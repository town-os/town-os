package packages

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

var (
	ErrInvalidOAuthSpec = errors.New("invalid oauth spec")
	// ErrOAuthURLNotAllowed is returned for a URL the system controller must not
	// call. The controller runs as root on the host, so a package that could
	// name any URL here would be able to use it to reach services on the host's
	// own network -- the package's own container cannot. Restricting these calls
	// to https on a public address keeps the flow to what it is for: talking to
	// an identity provider on the internet.
	ErrOAuthURLNotAllowed = errors.New("oauth URL not allowed")
)

// ValidateOAuthSpec checks the shape of an oauth question: that it declares a
// flow that could be run at all, and that a non-oauth question does not carry
// one. It deliberately says nothing about whether the flow's URLs may be
// *called*, because that is a property of the machine running the flow, not of
// the package -- see ValidateOAuthFlow. This is the check a package is held to
// when it is compiled and installed, which happens long after the flow itself
// ran and on a host that may legitimately allow private addresses.
func ValidateOAuthSpec(name string, q Question) error {
	if q.Type != Oauth {
		if q.OAuth != nil {
			return fmt.Errorf("%w: question %q declares oauth but is not type: oauth", ErrInvalidOAuthSpec, name)
		}
		return nil
	}

	if q.OAuth == nil {
		return fmt.Errorf("%w: question %q is type: oauth but declares no oauth flow", ErrInvalidOAuthSpec, name)
	}
	f := q.OAuth

	for field, value := range map[string]string{
		"start.url": f.Start.URL,
		"approve":   f.Approve,
		"poll.url":  f.Poll.URL,
		"token":     f.Token,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: question %q: %s is required", ErrInvalidOAuthSpec, name, field)
		}
	}

	for field, raw := range oauthURLs(f) {
		if err := validateOAuthURLShape(raw); err != nil {
			return fmt.Errorf("%w: question %q: %s: %w", ErrInvalidOAuthSpec, name, field, err)
		}
	}

	if f.Interval != "" {
		if _, err := ParseDuration(f.Interval); err != nil {
			return fmt.Errorf("%w: question %q: interval: %w", ErrInvalidOAuthSpec, name, err)
		}
	}
	if f.Timeout != "" {
		if _, err := ParseDuration(f.Timeout); err != nil {
			return fmt.Errorf("%w: question %q: timeout: %w", ErrInvalidOAuthSpec, name, err)
		}
	}
	return nil
}

// ValidateOAuthFlow is ValidateOAuthSpec plus the rule that every URL in the
// flow must be one the controller is willing to call: https, on a public
// address. It is the check made when a flow is about to be *run*, which is the
// only moment the answer depends on the host it is running on.
func ValidateOAuthFlow(name string, q Question) error {
	return ValidateOAuthFlowAllowing(name, q, false)
}

// ValidateOAuthFlowAllowing is ValidateOAuthFlow with the URL rules relaxed for
// tests, whose provider is an httptest server on loopback. allowPrivate is set
// only from ServerConfig, never from a package or a request.
func ValidateOAuthFlowAllowing(name string, q Question, allowPrivate bool) error {
	if err := ValidateOAuthSpec(name, q); err != nil {
		return err
	}
	if q.Type != Oauth {
		return nil
	}
	// The approve URL is opened in the operator's browser rather than fetched by
	// the controller, so the private-address rule is a poor fit for it -- but it
	// still must be https, so an approval page cannot be served in the clear.
	for field, raw := range oauthURLs(q.OAuth) {
		if err := ValidateOAuthURLAllowing(raw, allowPrivate); err != nil {
			return fmt.Errorf("%w: question %q: %s: %w", ErrInvalidOAuthSpec, name, field, err)
		}
	}
	return nil
}

func oauthURLs(f *OAuthFlow) map[string]string {
	return map[string]string{
		"start.url": f.Start.URL,
		"poll.url":  f.Poll.URL,
		"approve":   f.Approve,
	}
}

// validateOAuthURLShape checks what is knowable about a URL without deciding
// whether it may be called: that it parses, and that it does not template its
// host -- a templated host would leave the address unknowable until the flow
// ran, which is what the address rules exist to prevent.
func validateOAuthURLShape(raw string) error {
	if strings.Contains(authority(raw), "{{") {
		return fmt.Errorf("%w: %q templates its host", ErrOAuthURLNotAllowed, raw)
	}
	u, err := url.Parse(templatePlaceholder(raw))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrOAuthURLNotAllowed, err)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("%w: %q has no host", ErrOAuthURLNotAllowed, raw)
	}
	return nil
}

// ValidateOAuthURL checks a URL declared in a package's oauth flow. Templates
// ({{code}}, {{client_id}}) are not resolved yet, so the check is on the scheme
// and host as written; the resolved address is checked again at request time by
// CheckOAuthAddr, which is what actually stops a redirect or a DNS answer from
// landing on a private address.
func ValidateOAuthURL(raw string) error {
	return ValidateOAuthURLAllowing(raw, false)
}

// ValidateOAuthURLAllowing is ValidateOAuthURL with the https/public-address
// rules relaxed. See ValidateOAuthFlowAllowing.
func ValidateOAuthURLAllowing(raw string, allowPrivate bool) error {
	if err := validateOAuthURLShape(raw); err != nil {
		return err
	}
	u, err := url.Parse(templatePlaceholder(raw))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrOAuthURLNotAllowed, err)
	}
	if u.Scheme != "https" && (!allowPrivate || u.Scheme != "http") {
		return fmt.Errorf("%w: %q is not https", ErrOAuthURLNotAllowed, raw)
	}
	if allowPrivate {
		return nil
	}
	host := u.Hostname()
	// A literal IP is checked now; a name can only really be checked once
	// resolved, which CheckOAuthAddr does at dial time. "localhost" is the one
	// name that needs no resolver to condemn -- and it is the one a package
	// aiming the controller at the host would reach for first.
	if ip := net.ParseIP(host); ip != nil {
		return checkIP(ip)
	}
	if lower := strings.ToLower(host); lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return fmt.Errorf("%w: %q is not a public address", ErrOAuthURLNotAllowed, host)
	}
	return nil
}

// authority returns the scheme-and-host part of a URL as written, up to where
// the path, query, or fragment begins. Used to look for templates in the host
// without parsing, since a templated host is what makes parsing unreliable.
func authority(raw string) string {
	rest := raw
	if _, after, found := strings.Cut(raw, "://"); found {
		rest = after
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		return rest[:i]
	}
	return rest
}

// templatePlaceholder swaps {{name}} for a parseable literal so a URL carrying
// templates can still be scheme/host-checked before the flow runs.
func templatePlaceholder(raw string) string {
	var b strings.Builder
	for {
		start := strings.Index(raw, "{{")
		if start < 0 {
			b.WriteString(raw)
			return b.String()
		}
		end := strings.Index(raw[start:], "}}")
		if end < 0 {
			b.WriteString(raw)
			return b.String()
		}
		b.WriteString(raw[:start])
		b.WriteString("x")
		raw = raw[start+end+2:]
	}
}

// CheckOAuthAddr rejects an address the controller must not connect to. It is
// called for every address a flow's host resolves to, and again on every
// redirect: a DNS name that answers with 127.0.0.1, or a redirect to a link on
// the host's own network, is exactly what the scheme check alone would miss.
func CheckOAuthAddr(network, addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrOAuthURLNotAllowed, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("%w: %q is not an IP address", ErrOAuthURLNotAllowed, host)
	}
	return checkIP(ip)
}

func checkIP(ip net.IP) error {
	switch {
	case ip.IsLoopback(),
		ip.IsPrivate(),
		ip.IsLinkLocalUnicast(),
		ip.IsLinkLocalMulticast(),
		ip.IsInterfaceLocalMulticast(),
		ip.IsMulticast(),
		ip.IsUnspecified():
		return fmt.Errorf("%w: %s is not a public address", ErrOAuthURLNotAllowed, ip)
	}
	// 100.64.0.0/10 (carrier-grade NAT) is neither "private" nor public to Go,
	// and is routable inside a CGNAT/Tailscale network -- treat it as internal.
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return fmt.Errorf("%w: %s is not a public address", ErrOAuthURLNotAllowed, ip)
	}
	return nil
}

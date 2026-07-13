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
		if _, err := parseOAuthURL(raw); err != nil {
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

// parseOAuthURL parses a URL declared in a package's oauth flow and returns it
// with its host confirmed present.
//
// It hands the URL to net/url as written, templates and all, because net/url
// already says exactly the right thing about them: {{id}} in a path, query, or
// fragment parses fine and leaves the host intact, while a templated *host* --
// "https://{{host}}/token" -- is a parse error ("invalid character { in host
// name"). A templated host is the one form the address rules cannot survive,
// since the address would stay unknowable until the flow ran, so the parser's
// verdict is exactly the verdict we want.
func parseOAuthURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOAuthURLNotAllowed, err)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("%w: %q has no host", ErrOAuthURLNotAllowed, raw)
	}
	return u, nil
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
	u, err := parseOAuthURL(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "https" && (!allowPrivate || u.Scheme != "http") {
		return fmt.Errorf("%w: %q is not https", ErrOAuthURLNotAllowed, raw)
	}
	if allowPrivate {
		return nil
	}
	// A DNS name is NEVER rejected here. A name is not an address, and this
	// function has no resolver: which address it is dialed at is decided later, by
	// DNS, and is checked there -- CheckOAuthAddr runs in the dialer's Control hook
	// with the concrete IP. Judging names here would refuse plex.tv for the crime
	// of not being an IP address, while doing nothing about a name that answers
	// with 127.0.0.1 -- which is the whole attack the guard exists to stop.
	//
	// A literal IP is knowable now, and saying so early gives a package author a
	// better error than a failed dial.
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		return checkIP(ip)
	}
	return nil
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

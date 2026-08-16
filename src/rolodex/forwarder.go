package rolodex

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// Forwarder transports, matching the schemes rolodex's own parser accepts.
//
// These are the strings that go over the wire in SetForwarders, so they are the
// contract with `src/forwarder.rs` in ../rolodex-dns rather than a local
// convention. Nothing here re-implements rolodex's parsing for its own sake:
// Town OS validates a spec before pushing it so an operator's typo is refused
// where they can see the error, instead of arriving as a forwarder rolodex logs
// a warning about and silently drops.
const (
	// TransportDo53UDP is plaintext DNS over UDP, and the meaning of a bare
	// "ip:port" — which is what this list has always carried.
	TransportDo53UDP = "udp"
	// TransportDo53TCP is plaintext DNS over TCP (RFC 7766).
	TransportDo53TCP = "tcp"
	// TransportDoT is DNS-over-TLS (RFC 7858).
	TransportDoT = "tls"
	// TransportDoH is DNS-over-HTTPS (RFC 8484).
	TransportDoH = "https"
	// TransportDoQ is DNS-over-QUIC (RFC 9250).
	TransportDoQ = "quic"
)

// forwarderSchemes maps every accepted scheme onto its canonical transport.
// Both the protocol name and the common abbreviation are accepted because both
// are what people write, and rolodex accepts both.
var forwarderSchemes = map[string]string{
	"udp":   TransportDo53UDP,
	"do53":  TransportDo53UDP,
	"dns":   TransportDo53UDP,
	"tcp":   TransportDo53TCP,
	"tls":   TransportDoT,
	"dot":   TransportDoT,
	"https": TransportDoH,
	"doh":   TransportDoH,
	"quic":  TransportDoQ,
	"doq":   TransportDoQ,
}

// defaultForwarderPorts is the port assumed when a spec names none.
var defaultForwarderPorts = map[string]string{
	TransportDo53UDP: "53",
	TransportDo53TCP: "53",
	TransportDoT:     "853",
	TransportDoH:     "443",
	TransportDoQ:     "853",
}

// Forwarder is a parsed upstream forwarder spec.
type Forwarder struct {
	// Transport is one of the Transport* constants.
	Transport string
	// Addr is the "ip:port" actually dialed.
	Addr string
	// ServerName is the TLS name validated against the certificate, empty for
	// the plaintext transports.
	ServerName string
	// Path is the DoH request path, empty for everything else.
	Path string
	// Spec is the input, unchanged, so what is pushed is what was configured.
	Spec string
}

// Encrypted reports whether the forwarder's transport encrypts the query.
//
// This is what decides the tier rolodex files it under, so it is derived from
// the transport rather than being a field an entry can assert about itself.
func (f Forwarder) Encrypted() bool {
	switch f.Transport {
	case TransportDoT, TransportDoH, TransportDoQ:
		return true
	default:
		return false
	}
}

// ParseForwarder validates one forwarder spec.
//
// A bare "ip:port" is plaintext UDP, which is what every forwarder written
// before transports were nameable means and still means. A scheme selects
// anything else:
//
//	8.8.8.8:53                                    plaintext UDP
//	tcp://8.8.8.8:53                              plaintext TCP
//	tls://cloudflare-dns.com@1.1.1.1:853          DoT
//	https://cloudflare-dns.com@1.1.1.1/dns-query  DoH
//	quic://dns.adguard.com@94.140.14.14:853       DoQ
//
// The "name@ip" authority carries both halves an encrypted upstream needs — the
// address to dial and the name to validate its certificate against — so nothing
// has to resolve a hostname before it can reach a resolver. That bootstrapping
// property is why the address must be a literal: a forwarder named only by
// hostname could not be used by the resolver that would have to resolve it.
func ParseForwarder(spec string) (Forwarder, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Forwarder{}, errors.New("empty forwarder")
	}

	transport := TransportDo53UDP
	rest := spec
	if scheme, after, found := strings.Cut(spec, "://"); found {
		canonical, ok := forwarderSchemes[strings.ToLower(scheme)]
		if !ok {
			return Forwarder{}, fmt.Errorf(
				"forwarder %q: unsupported transport %q (use udp, tcp, tls/dot, https/doh or quic/doq)",
				spec, scheme)
		}
		transport, rest = canonical, after
	}
	if rest == "" {
		return Forwarder{}, fmt.Errorf("forwarder %q names no address", spec)
	}

	authority, path := rest, ""
	if before, after, found := strings.Cut(rest, "/"); found {
		authority, path = before, "/"+after
	}
	if path != "" && transport != TransportDoH {
		return Forwarder{}, fmt.Errorf("forwarder %q: a path is only meaningful for https/doh", spec)
	}

	serverName := ""
	if idx := strings.LastIndex(authority, "@"); idx >= 0 {
		serverName, authority = authority[:idx], authority[idx+1:]
		if serverName == "" {
			return Forwarder{}, fmt.Errorf("forwarder %q: empty server name before '@'", spec)
		}
	}
	if serverName != "" && !isEncryptedTransport(transport) {
		return Forwarder{}, fmt.Errorf(
			"forwarder %q: a server name is only meaningful for an encrypted transport", spec)
	}

	addr, err := normalizeForwarderAddr(authority, defaultForwarderPorts[transport])
	if err != nil {
		return Forwarder{}, fmt.Errorf("forwarder %q: %w", spec, err)
	}

	// An encrypted forwarder with no name validates against the address's own
	// IP SANs. That is a real check rather than a waiver: a certificate without
	// the SAN fails the handshake instead of being trusted.
	if isEncryptedTransport(transport) && serverName == "" {
		host, _, splitErr := net.SplitHostPort(addr)
		if splitErr != nil {
			return Forwarder{}, fmt.Errorf("forwarder %q: %w", spec, splitErr)
		}
		serverName = host
	}
	if transport == TransportDoH && path == "" {
		path = DefaultDoHPath
	}

	return Forwarder{
		Transport:  transport,
		Addr:       addr,
		ServerName: serverName,
		Path:       path,
		Spec:       spec,
	}, nil
}

// DefaultDoHPath is the request path assumed when a DoH forwarder names none.
const DefaultDoHPath = "/dns-query"

func isEncryptedTransport(transport string) bool {
	switch transport {
	case TransportDoT, TransportDoH, TransportDoQ:
		return true
	default:
		return false
	}
}

// normalizeForwarderAddr turns an authority into "ip:port", supplying
// defaultPort when none was given.
//
// The address must be a literal. Bracketed and bare IPv6 are both handled
// because the two spellings collide — "::1" is an address that contains colons,
// "[::1]:853" is an address and a port — so SplitHostPort alone cannot tell
// them apart and is tried second rather than first.
func normalizeForwarderAddr(authority, defaultPort string) (string, error) {
	if addr, err := netip.ParseAddrPort(authority); err == nil {
		return addr.String(), nil
	}
	bare := strings.TrimSuffix(strings.TrimPrefix(authority, "["), "]")
	if ip, err := netip.ParseAddr(bare); err == nil {
		return netip.AddrPortFrom(ip, portNumber(defaultPort)).String(), nil
	}
	return "", fmt.Errorf("%q is not an ip or ip:port", authority)
}

// portNumber converts a known-good default port string. The inputs are the
// package's own constants, so an unparseable one is a programming error rather
// than an operator's; it degrades to the DNS port instead of panicking.
func portNumber(port string) uint16 {
	switch port {
	case "53":
		return 53
	case "443":
		return 443
	case "853":
		return 853
	default:
		return 53
	}
}

// ValidateForwarders checks every spec, returning the first error.
//
// Validation is all-or-nothing on purpose: rolodex applies SetForwarders as a
// replacement, so a list accepted with one entry quietly dropped would leave the
// resolver configured with something the operator did not ask for and cannot
// see.
func ValidateForwarders(specs []string) error {
	for _, spec := range specs {
		if _, err := ParseForwarder(spec); err != nil {
			return err
		}
	}
	return nil
}

// SplitForwarderSpecs splits a comma-separated forwarder list, dropping empty
// entries and surrounding whitespace.
//
// Comma-separated rather than a JSON array because this is a settings value an
// operator types, and the list it replaces — DefaultForwarders — is short
// enough that quoting it would be the hardest part of setting it.
func SplitForwarderSpecs(value string) []string {
	var specs []string
	for part := range strings.SplitSeq(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			specs = append(specs, trimmed)
		}
	}
	return specs
}

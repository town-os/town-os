package wireguard

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// Validation for the values that end up in a wg-quick document.
//
// The document this package renders is executed by `wg-quick up` from a root
// systemd unit, and wg-quick's parser (parse_options in the wg-quick script)
// tracks its current section from the file's own content:
//
//	[[ $key == "["*        ]] && interface_section=0
//	[[ $key == "[Interface]" ]] && interface_section=1
//	...
//	PostUp) POST_UP+=( "$unstripped_value" ); continue ;;
//
// and later runs every collected hook through `(eval "$hook")` as root. So a
// value carrying a newline is not a formatting problem: it is a second
// [Interface] section away from arbitrary root command execution, and the peer
// fields are supplied by whoever calls POST /networks/peers/add — which admits
// a non-admin holding the wireguard grant.
//
// Two layers, deliberately:
//
//   - the exported validators below, applied at the API boundary, so a bad
//     value is refused with a clear error and never reaches the database; and
//   - the unconditional check inside RenderInterfaceConfig, so a row that got
//     in some other way (a database written by an older build, a direct
//     manager call) cannot be executed either. That second layer is what makes
//     an already-compromised box self-heal on its next reconcile instead of
//     re-running the injected hook at every boot.

var (
	// ErrUnsafeConfigValue is returned for a value that could alter the
	// structure of the rendered wg-quick document.
	ErrUnsafeConfigValue = errors.New("wireguard: value contains characters that are not allowed in a wg-quick config")
	// ErrInvalidKey is returned for a key that is not base64 of 32 bytes.
	ErrInvalidKey = errors.New("wireguard: key must be base64 of 32 bytes")
	// ErrInvalidEndpoint is returned for an endpoint that is not host:port.
	ErrInvalidEndpoint = errors.New("wireguard: endpoint must be host:port")
)

// KeyLen is the byte length of a Curve25519 key, which is what both halves of a
// WireGuard keypair are.
const KeyLen = 32

// safeConfigValue reports whether s can appear in a wg-quick document without
// changing its structure.
//
// A newline is the whole attack, but a carriage return is refused too: bash's
// `read -r` splits on \n and leaves the \r inside the value, so a CRLF payload
// still produces the extra logical lines while looking like one value to a
// naive check. NUL is refused because the file is written by Go and read by
// bash, which would truncate at it — so the bytes on disk and the bytes
// anything reviewing them sees would differ.
func safeConfigValue(s string) bool {
	return !strings.ContainsAny(s, "\n\r\x00")
}

// ValidateConfigValue rejects a value that could restructure the rendered
// document. It is the minimum every field must satisfy, whatever else it is.
func ValidateConfigValue(field, value string) error {
	if !safeConfigValue(value) {
		return fmt.Errorf("%w: %s", ErrUnsafeConfigValue, field)
	}
	return nil
}

// ValidateKey checks that a key is what WireGuard keys are: standard base64 of
// exactly 32 bytes.
//
// Strict rather than "no newlines", because a key that is not a key can only
// ever produce a peer that fails to handshake. Accepting a label like
// "LABPUBKEY" enrolls a device that can never connect, burns an overlay
// address, and makes the peer list describe something that does not exist —
// so this rejects a class of silent misconfiguration as well as the injection.
func ValidateKey(key string) error {
	if err := ValidateConfigValue("key", key); err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	if len(raw) != KeyLen {
		return fmt.Errorf("%w: got %d bytes", ErrInvalidKey, len(raw))
	}
	return nil
}

// ValidateEndpoint checks that an endpoint is a dialable host:port.
//
// The host may be an IP literal or a DNS name — a peer behind a dynamic address
// is legitimately named rather than numbered — but it must parse as one of
// those, and the port must be a port. An empty endpoint is valid and means the
// peer is not dialable from here, which is the normal case for a phone.
func ValidateEndpoint(endpoint string) error {
	if endpoint == "" {
		return nil
	}
	if err := ValidateConfigValue("endpoint", endpoint); err != nil {
		return err
	}

	// An IPv6 endpoint has to arrive bracketed, which is also what
	// formatEndpoint emits and what wg-quick expects.
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidEndpoint, err)
	}
	if host == "" {
		return fmt.Errorf("%w: empty host", ErrInvalidEndpoint)
	}
	if _, err := netip.ParseAddr(host); err != nil && !validDNSName(host) {
		return fmt.Errorf("%w: %q is neither an IP address nor a hostname", ErrInvalidEndpoint, host)
	}
	if !validPort(port) {
		return fmt.Errorf("%w: %q is not a port", ErrInvalidEndpoint, port)
	}
	return nil
}

// validPort reports whether s is a decimal port in 1..65535.
func validPort(s string) bool {
	if s == "" || len(s) > 5 {
		return false
	}
	n := 0
	for i := range len(s) {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + int(c-'0')
	}
	return n >= 1 && n <= 65535
}

// validDNSName reports whether s is a DNS name: dot-separated labels of
// letters, digits and dashes, no label starting or ending with a dash.
//
// No underscore -- it is not a hostname character, and an endpoint is a name
// something has to resolve and dial.
func validDNSName(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	s = strings.TrimSuffix(s, ".")
	for label := range strings.SplitSeq(s, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := range len(label) {
			c := label[i]
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-':
			default:
				return false
			}
		}
	}
	return true
}

// safePeer reports whether every field of a peer can be rendered without
// restructuring the document. Used by RenderInterfaceConfig to drop a peer it
// cannot render safely, rather than emitting it.
func safePeer(p PeerConfig) bool {
	return safeConfigValue(p.PublicKey) &&
		safeConfigValue(p.AllowedIPs) &&
		safeConfigValue(p.Endpoint)
}

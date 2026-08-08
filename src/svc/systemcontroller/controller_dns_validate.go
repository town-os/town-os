package systemcontroller

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// tldPattern matches valid DNS labels: lowercase alphanumeric, optionally
// containing hyphens (but not at the start or end).
var tldPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ValidateTLD validates that the given string is a valid TLD label for use
// with the local DNS server. It must be non-empty, at most 63 characters,
// lowercase alphanumeric with hyphens (not at start/end), and contain no dots.
func ValidateTLD(tld string) error {
	if tld == "" {
		return errors.New("TLD must not be empty")
	}

	if len(tld) > 63 {
		return fmt.Errorf("TLD must be at most 63 characters, got %d", len(tld))
	}

	if !tldPattern.MatchString(tld) {
		return fmt.Errorf("TLD %q is invalid: must be lowercase alphanumeric with optional hyphens, cannot start or end with a hyphen", tld)
	}

	return nil
}

// dnsLabelPattern matches a single DNS label: alphanumeric with optional
// internal hyphens, case-insensitive.
var dnsLabelPattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?$`)

// validateHostname validates a dotted DNS hostname (e.g. "zen.spamhaus.org"):
// non-empty, at most 253 characters, at least two dot-separated labels, each a
// valid DNS label of at most 63 characters. A trailing dot is tolerated.
func validateHostname(host string) error {
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return errors.New("must not be empty")
	}
	if len(host) > 253 {
		return fmt.Errorf("must be at most 253 characters, got %d", len(host))
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return errors.New("must be a fully-qualified domain (e.g. zen.spamhaus.org)")
	}
	for _, label := range labels {
		if len(label) > 63 {
			return fmt.Errorf("label %q must be at most 63 characters", label)
		}
		if !dnsLabelPattern.MatchString(label) {
			return fmt.Errorf("label %q is invalid: must be alphanumeric with optional internal hyphens", label)
		}
	}
	return nil
}

// ValidateRblZone validates an RBL/DNSBL provider zone (a DNS hostname such as
// "zen.spamhaus.org" or "dbl.spamhaus.org").
func ValidateRblZone(zone string) error {
	if err := validateHostname(zone); err != nil {
		return fmt.Errorf("RBL zone %q is invalid: %w", zone, err)
	}
	return nil
}

// RefusalCodesNone is the single-entry spelling of a provider's refusal-code
// list that means "this provider has no refusal codes" — switch the detection
// off rather than fall back to rolodex's built-in set. An empty list cannot
// carry that meaning, because an empty list is what every configuration written
// before refusal detection existed already has.
const RefusalCodesNone = "none"

// ValidateRefusalCodes validates and normalizes one provider's refusal-code
// list — the codes that provider answers with to mean "I refused your query"
// rather than "this name is listed".
//
// The distinction is the whole point of the field. A DNSxL answers both a
// listing and a complaint about the querier with an A record under 127.0.0.0/8,
// so only the address separates them: 127.0.0.2 is "listed", 127.255.255.254 is
// "you queried via a public resolver" and 127.255.255.255 is "you are over your
// query limit". Reading the latter two as listings makes the resolver NXDOMAIN
// every name checked against that provider — the blocklist stops being a
// blocklist and becomes an outage — which is exactly what happens to a
// household box that quietly exceeds Spamhaus's free-use limit.
//
// The three cases mirror rolodex's resolve_refusal_codes exactly, because this
// list is passed through to it verbatim and disagreeing about what an entry
// means would be worse than not validating at all:
//
//   - empty — rolodex substitutes its built-in set, so an existing
//     configuration gets the safe reading without being edited;
//   - exactly [RefusalCodesNone] — detection off, for a private blocklist whose
//     real listings collide with one of the built-in codes;
//   - anything else — exactly those codes, with the built-ins deliberately NOT
//     merged in, so an operator who spells the list out gets what they spelled.
//
// Each entry is an IPv4 address or "address/prefix"; a prefix because the
// providers document whole ranges (Spamhaus reserves all of 127.255.255.0/24
// for errors and adds codes to it over time, so enumerating today's three would
// silently start reading tomorrow's fourth as a listing). The returned codes are
// masked to their prefix, matching how rolodex stores and reports them back, so
// a round-trip through GET does not appear to rewrite the operator's input.
func ValidateRefusalCodes(codes []string) ([]string, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	// "none" is a whole-list statement, not a code, so it is meaningless
	// alongside real codes — a list that both disables detection and names
	// codes to detect has no reading to pick.
	if len(codes) == 1 && strings.EqualFold(strings.TrimSpace(codes[0]), RefusalCodesNone) {
		return []string{RefusalCodesNone}, nil
	}
	for _, code := range codes {
		if strings.EqualFold(strings.TrimSpace(code), RefusalCodesNone) {
			return nil, fmt.Errorf("refusal code %q disables refusal detection and must be the only entry", RefusalCodesNone)
		}
	}

	cleaned := make([]string, 0, len(codes))
	seen := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		normalized, err := normalizeRefusalCode(code)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[normalized]; dup {
			return nil, fmt.Errorf("duplicate refusal code %q", normalized)
		}
		seen[normalized] = struct{}{}
		cleaned = append(cleaned, normalized)
	}
	return cleaned, nil
}

// normalizeRefusalCode parses a single "127.0.0.1" or "127.255.255.0/24" entry
// and renders it back masked to its prefix, so 127.255.255.9/24 and
// 127.255.255.0/24 are the same code and read back identically.
func normalizeRefusalCode(code string) (string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", errors.New("refusal code must not be empty")
	}

	if addr, prefix, found := strings.Cut(code, "/"); found {
		bits, err := strconv.Atoi(strings.TrimSpace(prefix))
		if err != nil {
			return "", fmt.Errorf("refusal code %q is invalid: prefix must be a number", code)
		}
		if bits < 0 || bits > 32 {
			return "", fmt.Errorf("refusal code %q is invalid: prefix must be between 0 and 32", code)
		}
		parsed, err := parseRefusalAddr(addr, code)
		if err != nil {
			return "", err
		}
		masked := netip.PrefixFrom(parsed, bits).Masked()
		// A /32 renders bare, because that is how rolodex renders it back:
		// its RefusalCode Display writes the address alone when the prefix
		// covers every bit. Keeping the "/32" here would make a code read back
		// from GET differ from the one just submitted, so the screen would show
		// the box having rewritten the operator's input — and the UI decides
		// whether a provider still carries the untouched built-in set by
		// comparing against that list, whose entries are spelled bare.
		if bits == 32 {
			return masked.Addr().String(), nil
		}
		return masked.String(), nil
	}

	parsed, err := parseRefusalAddr(code, code)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

// parseRefusalAddr parses the address half of a refusal code. IPv4 only:
// a DNSxL answers in 127.0.0.0/8, and rolodex matches codes as IPv4, so an
// IPv6 literal here would be accepted and then never match anything.
func parseRefusalAddr(addr, code string) (netip.Addr, error) {
	parsed, err := netip.ParseAddr(strings.TrimSpace(addr))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("refusal code %q is invalid: must be an IPv4 address or address/prefix", code)
	}
	if !parsed.Is4() {
		return netip.Addr{}, fmt.Errorf("refusal code %q is invalid: must be IPv4, blocklists answer in 127.0.0.0/8", code)
	}
	return parsed, nil
}

// ValidateLocalRblName validates a local RBL blocklist entry name, which may be
// either a domain name (forward-blocked) or an IP address (reverse-blocked).
func ValidateLocalRblName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("local RBL entry name must not be empty")
	}
	if ip := net.ParseIP(name); ip != nil {
		return nil
	}
	if err := validateHostname(name); err != nil {
		return fmt.Errorf("local RBL entry %q is invalid: %w (must be a domain name or IP address)", name, err)
	}
	return nil
}

// ValidateDnsblAllowlistName validates a DNSBL allowlist entry name. Unlike
// ValidateLocalRblName this deliberately rejects IP literals: the allowlist
// exempts a name from the *name-based* blocklist step and matches the name plus
// every name beneath it, so an address is not something it could ever match.
func ValidateDnsblAllowlistName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("allowlist entry name must not be empty")
	}
	if ip := net.ParseIP(name); ip != nil {
		return fmt.Errorf("allowlist entry %q is invalid: must be a domain name, not an IP address", name)
	}
	if err := validateHostname(name); err != nil {
		return fmt.Errorf("allowlist entry %q is invalid: %w", name, err)
	}
	return nil
}

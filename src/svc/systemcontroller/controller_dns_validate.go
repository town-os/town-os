package systemcontroller

import (
	"errors"
	"fmt"
	"net"
	"regexp"
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

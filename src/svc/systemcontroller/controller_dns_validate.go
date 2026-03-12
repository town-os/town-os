package systemcontroller

import (
	"errors"
	"fmt"
	"regexp"
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

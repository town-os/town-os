// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package gfeh

import (
	"errors"
	"fmt"
	"strings"
)

// A label arriving from gfehd is untrusted input, and it is untrusted in three
// directions at once.
//
// GET /v1/names is answered by a daemon Town OS launched, from a config Town OS
// rendered, so it is tempting to treat what comes back as our own. It is not:
// the label is a *string on a wire*, and Town OS spends it in three places that
// each fail differently on a malformed one.
//
//  1. It becomes an ingress vhost. renderCaddyfile writes `https://<hostname> {`
//     with no quoting, so a label carrying a newline and a brace closes the block
//     and opens another. Caddy does not reject one bad vhost — it refuses the
//     whole config, which takes every package, page and partition on the box off
//     :443 at once.
//  2. It becomes a DNS record name, where a label with a space or a wildcard is
//     either refused by rolodex or, worse, accepted as something other than what
//     it reads as.
//  3. With the index page it becomes a *filesystem path* under the webroot, and
//     `filepath.Join` collapses "..", so `../../etc` is not a naming problem —
//     it is a write outside the tree, run by the control plane as root.
//
// One validator, applied where the label enters (gfehFQDN), covers all three.
// Checking at each spend instead would be three checks that have to agree, which
// is the shape the storage-prefix traversal bug already took once.

// LabelMaxLen is the DNS limit on a single label.
const LabelMaxLen = 63

// NameMaxLen is the DNS limit on a fully-qualified name.
const NameMaxLen = 253

// ErrEmptyLabel is returned for a label that is empty once trimmed.
var ErrEmptyLabel = errors.New("gfeh: empty hostname label")

// NormalizeLabel canonicalizes a label the way DNS reads it: trimmed of
// surrounding space and of a trailing root dot, and lowercased.
//
// Lowercasing is canonicalization rather than leniency. DNS comparison is
// case-insensitive, so S3.Gfeh and s3.gfeh are one name — but the label is also
// used as a map key, a directory name and a Caddyfile vhost, and those are all
// case-*sensitive*. Two spellings of one name would be two certificates, two
// directories and two vhosts Caddy would reject as a duplicate pair.
func NormalizeLabel(label string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(label), "."))
}

// ValidateLabel reports whether a label gfehd reported is safe to spend as a
// name, a route and a path. It expects an already-normalized value.
//
// A label may carry dots — every one gfeh reports does, being "<view>.gfeh" —
// so this validates each dot-separated component as a DNS label rather than the
// whole string as one. The character set is deliberately the strict LDH rule
// (letters, digits, hyphen) rather than "anything without a slash": a blocklist
// has to anticipate every character that matters to a Caddyfile, a zone file and
// a path, and those three lists are not the same one.
func ValidateLabel(label string) error {
	if label == "" {
		return ErrEmptyLabel
	}
	if len(label) > NameMaxLen {
		return fmt.Errorf("gfeh: hostname label %q is longer than %d characters", label, NameMaxLen)
	}
	for part := range strings.SplitSeq(label, ".") {
		if err := validateLabelPart(label, part); err != nil {
			return err
		}
	}
	return nil
}

// validateLabelPart checks one dot-separated component.
//
// The empty component is what rejects a leading dot, a trailing dot (the root
// dot is trimmed by NormalizeLabel before this runs, so one here is a second
// one), a doubled dot, and — the one that matters — "." and ".." as path
// components.
func validateLabelPart(label, part string) error {
	if part == "" {
		return fmt.Errorf("gfeh: hostname label %q has an empty component", label)
	}
	if len(part) > LabelMaxLen {
		return fmt.Errorf("gfeh: hostname label %q has a component longer than %d characters", label, LabelMaxLen)
	}
	// A leading or trailing hyphen is not a legal DNS label, and a leading one
	// is also how a string starts being read as a flag by something downstream.
	if strings.HasPrefix(part, "-") || strings.HasSuffix(part, "-") {
		return fmt.Errorf("gfeh: hostname label %q has a component starting or ending with a hyphen", label)
	}
	for _, r := range part {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return fmt.Errorf("gfeh: hostname label %q contains %q, which is not a letter, digit or hyphen", label, r)
		}
	}
	return nil
}

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"errors"
	"log/slog"
	"strings"

	"gitea.com/town-os/town-os/src/gfeh"
)

// Three things on this box get published under a network's TLD — a package, a
// page, and an object-storage partition — and each one's name has to be the
// same string in four places: its A record, its leaf certificate SAN, its DANE
// TLSA owner, and its ingress vhost. For a page it is a fifth: the on-disk
// subvolume and webroot symlink, because the pages Caddy roots on /srv/<host>.
//
// Each of the three composed that string itself, and they did not agree about
// what a legal name was. gfehFQDN normalized the label, validated every
// dot-separated component against the strict LDH rule, and refused a name that
// qualified past the 253-character DNS limit. packageFQDN was
// `pkg + "." + repo + "." + tld` with no validation and no length check at all.
// pageFQDN trimmed and handled the public-FQDN case, and otherwise also
// checked nothing — on the one name that additionally becomes a filesystem
// path, from a `domain` field an operator types into POST /pages/create.
//
// qualifyPublishedName is the one composer. It is gfehFQDN's rules, applied to
// all three, so the rigor that existed for the daemon that asked for it least
// (gfeh reports its own labels, and they are always "<view>.gfeh") now also
// covers the two whose names come from a repository and from a text field.
//
// It returns "" for a name it will not publish, which is the established
// contract on this path: every collector already drops an empty FQDN, so a
// malformed name contributes no record, no route, no certificate and no
// directory — rather than contributing a broken one to all four.

// qualifyPublishedName composes label under tld and returns "" if the result is
// not a name Town OS will publish.
//
// It is idempotent: a label that already ends in the TLD is returned unchanged
// rather than becoming blog.home.home. Length is checked on the *composed*
// name, not on the label alone, because a label within the limit can still
// qualify past it under a long TLD — and a name DNS will not carry is one the
// certificate and the vhost should not claim either.
//
// kind names the caller ("package", "page", "gfeh") and appears only in the
// warning, so an operator whose name was dropped can tell which subsystem
// dropped it.
func qualifyPublishedName(kind, label, tld string) string {
	label = validatePublishedName(kind, label)
	if label == "" || tld == "" {
		return ""
	}

	fqdn := label
	if label != tld && !strings.HasSuffix(label, "."+tld) {
		fqdn = label + "." + tld
	}

	if len(fqdn) > gfeh.NameMaxLen {
		slog.Error("refusing to publish a hostname that is too long once qualified",
			"kind", kind, "fqdn", fqdn, "limit", gfeh.NameMaxLen)
		return ""
	}
	return fqdn
}

// validatePublishedName normalizes a name and returns it, or "" if it is not
// one Town OS will publish. It does not qualify.
//
// It exists separately from qualifyPublishedName because a page's domain may
// legitimately be the operator's own public FQDN, which is served verbatim via
// ACME and must NOT be composed under the box's TLD — but still has to be a
// hostname. It becomes a Caddy vhost, a rolodex lookup, and a directory under
// pages-webroot/, and "it is the operator's domain" is a reason not to
// *qualify* it, never a reason not to check it.
//
// That distinction was the actual hole. isPublicFQDN reads anything containing
// a dot and not ending in the TLD as public, so `../escape.example.com`,
// `site.example.com/../../etc`, and `site.example.com other.example.com` all
// took the verbatim path and became filesystem paths and Caddyfile site
// addresses unexamined.
func validatePublishedName(kind, name string) string {
	name = gfeh.NormalizeLabel(name)
	if name == "" {
		return ""
	}
	if err := gfeh.ValidateLabel(name); err != nil {
		// Error, not Warn, and that is about visibility rather than severity
		// grading. LOG_LEVEL defaults to `error`, so a Warn here would be
		// invisible on a normal box — and the consequence of this branch is a
		// package or page that silently stops resolving, which is precisely
		// the class of failure that must not be discoverable only by turning
		// logging up.
		slog.Error("refusing to publish a hostname Town OS cannot serve safely",
			"kind", kind, "name", name, "error", err)
		return ""
	}
	return name
}

// ValidatePageDomain reports whether a page's `domain` is a hostname Town OS
// will serve. It is the API-level half of validatePublishedName: the handlers
// refuse the write outright so an operator is told, rather than accepting it
// and dropping the name later where only a log line records it.
//
// Exported because the pages handlers and their tests both need it.
func ValidatePageDomain(domain string) error {
	normalized := gfeh.NormalizeLabel(domain)
	if normalized == "" {
		return errEmptyPageDomain
	}
	if err := gfeh.ValidateLabel(normalized); err != nil {
		return err
	}
	if len(normalized) > gfeh.NameMaxLen {
		return errPageDomainTooLong
	}
	return nil
}

var (
	errEmptyPageDomain   = errors.New("page domain must not be empty")
	errPageDomainTooLong = errors.New("page domain is longer than DNS allows")
)

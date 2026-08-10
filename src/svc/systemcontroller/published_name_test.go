// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
)

// A published name is one string in four places — A record, leaf SAN, TLSA
// owner, ingress vhost — and for a page in a fifth, the on-disk subvolume and
// webroot symlink. Three publishers composed it separately and disagreed about
// what a legal name was: gfeh validated every component and capped the length,
// packages checked nothing at all, pages checked nothing beyond trimming.

func TestQualifyPublishedNameQualifiesAndIsIdempotent(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ label, tld, want string }{
		"bare label":        {"blog", "home", "blog.home"},
		"already qualified": {"blog.home", "home", "blog.home"},
		"gfeh view":         {"s3.gfeh", "office", "s3.gfeh.office"},
		"package shape":     {"gitea.default", "home", "gitea.default.home"},
		"label is the tld":  {"home", "home", "home"},
	} {
		if got := qualifyPublishedName("test", tc.label, tc.tld); got != tc.want {
			t.Errorf("%s: qualifyPublishedName(%q, %q) = %q, want %q", name, tc.label, tc.tld, got, tc.want)
		}
	}
}

// Lowercasing is canonicalization, not leniency: the name is also a map key, a
// directory name and a Caddyfile vhost, and those are case-sensitive. Two
// spellings would be two certificates, two directories, and a duplicate vhost
// pair that Caddy rejects outright — taking every other route down with it.
func TestQualifyPublishedNameNormalizes(t *testing.T) {
	t.Parallel()

	for _, label := range []string{"Blog", "BLOG", " blog ", "blog."} {
		if got := qualifyPublishedName("test", label, "home"); got != "blog.home" {
			t.Errorf("qualifyPublishedName(%q) = %q, want %q", label, got, "blog.home")
		}
	}
}

func TestQualifyPublishedNameRefusesUnpublishableLabels(t *testing.T) {
	t.Parallel()

	for name, label := range map[string]string{
		"empty":           "",
		"whitespace only": "   ",
		"slash":           "a/../b",
		"space inside":    "my blog",
		"caddy brace":     "blog{",
		"zone semicolon":  "blog;x",
		"leading dot":     ".blog",
		"empty component": "a..b",
		"underscore":      "my_blog",
		"colon":           "blog:8080",
		"newline":         "blog\nx",
		"leading hyphen":  "-blog",
		"wildcard":        "*.blog",
	} {
		if got := qualifyPublishedName("test", label, "home"); got != "" {
			t.Errorf("%s: qualifyPublishedName(%q) = %q, want \"\" (unpublishable)", name, label, got)
		}
	}
}

// Length is checked on the composed name, not the label: a label inside the
// limit can still qualify past it under a long TLD, and a name DNS will not
// carry is one the certificate and the vhost should not claim either.
func TestQualifyPublishedNameChecksComposedLength(t *testing.T) {
	t.Parallel()

	// The interesting case is a name that is legal on its own and illegal only
	// once qualified — which a per-component check and a check on the bare
	// label would both miss.
	//
	// 62+1+62+1+62+1+61 = 250: every component inside the 63-char per-label
	// limit, the whole thing inside the 253-char name limit, and 255 once
	// ".home" is appended.
	c62 := strings.Repeat("a", 62)
	long := c62 + "." + c62 + "." + c62 + "." + strings.Repeat("a", 61)
	if len(long) != 250 {
		t.Fatalf("fixture is %d chars, expected 250", len(long))
	}
	if got := qualifyPublishedName("test", long, "home"); got != "" {
		t.Errorf("a name that qualifies past the DNS limit was published as %q (len %d)", got, len(got))
	}

	// One component shorter still fits once qualified, so the check is not
	// simply refusing everything long.
	shorter := c62 + "." + c62 + "." + c62
	if got := qualifyPublishedName("test", shorter, "home"); got == "" {
		t.Error("a name comfortably within the limit was refused")
	}
}

func TestQualifyPublishedNameRefusesEmptyTLD(t *testing.T) {
	t.Parallel()

	if got := qualifyPublishedName("test", "blog", ""); got != "" {
		t.Errorf("qualifyPublishedName with no TLD = %q, want \"\"", got)
	}
}

// --- the three publishers now share those rules ---

func TestPackageFQDNIsValidated(t *testing.T) {
	t.Parallel()

	if got := packageFQDN("default", "gitea", "home"); got != "gitea.default.home" {
		t.Errorf("packageFQDN = %q, want gitea.default.home", got)
	}

	// Previously bare concatenation: this produced "my pkg.default.home",
	// which is a rolodex record no resolver carries, a SAN, and a Caddy vhost.
	if got := packageFQDN("default", "my pkg", "home"); got != "" {
		t.Errorf("packageFQDN with an illegal package name = %q, want \"\"", got)
	}
	if got := packageFQDN("re;po", "pkg", "home"); got != "" {
		t.Errorf("packageFQDN with an illegal repo name = %q, want \"\"", got)
	}
}

// A page's name is the one that also becomes a filesystem path, and its
// `domain` is a text field an operator types into POST /pages/create.
func TestPageHostnameIsValidated(t *testing.T) {
	t.Parallel()

	if got := pageHostname("blog", "home"); got != "blog.home" {
		t.Errorf("pageHostname = %q, want blog.home", got)
	}
	for _, domain := range []string{"../etc", "a/b", "blog x", "blog{"} {
		if got := pageHostname(domain, "home"); got != "" {
			t.Errorf("pageHostname(%q) = %q, want \"\" (unpublishable)", domain, got)
		}
	}
}

// A public FQDN is not qualified under the box's TLD — it is the operator's own
// domain, resolved by their own DNS and served via ACME. It is still checked.
func TestPageHostnamePassesPublicFQDNThroughUnqualified(t *testing.T) {
	t.Parallel()

	if got := pageHostname("blog.example.com", "home"); got != "blog.example.com" {
		t.Errorf("pageHostname(public) = %q, want blog.example.com", got)
	}
}

// isPublicFQDN reads anything containing a dot and not ending in the TLD as
// public, so the verbatim branch was where a domain reached filepath.Join
// unexamined. "It is the operator's domain" is a reason not to *qualify* it,
// never a reason not to check it: the string still becomes a Caddy vhost and a
// directory under pages-webroot/.
func TestPageHostnameValidatesEvenThePublicBranch(t *testing.T) {
	t.Parallel()

	for name, domain := range map[string]string{
		"parent traversal": "../escape.example.com",
		"embedded slash":   "site.example.com/../../etc",
		"absolute path":    "/etc/cron.d",
		"newline":          "site.example.com\nblog.example.com",
		"null byte":        "site.example.com\x00",
		// Caddy reads space-separated site addresses as several addresses for
		// one block, so this claims a hostname belonging to another service.
		"space": "site.example.com other.example.com",
	} {
		if got := pageHostname(domain, "home"); got != "" {
			t.Errorf("%s: pageHostname(%q) = %q, want \"\"", name, domain, got)
		}
	}
}

// The API-level half. A domain that will be dropped later must be refused now,
// so the operator is told instead of finding out from a log line.
func TestValidatePageDomain(t *testing.T) {
	t.Parallel()

	for name, domain := range map[string]string{
		"empty":                    "",
		"whitespace":               "   ",
		"parent traversal":         "../escape.example.com",
		"deep traversal":           "../../../../etc/cron.d",
		"absolute path":            "/etc/cron.d",
		"embedded slash":           "site.example.com/../../etc",
		"newline":                  "site.example.com\nblog.example.com",
		"null byte":                "site.example.com\x00",
		"space":                    "site.example.com other.example.com",
		"underscore":               "my_site.example.com",
		"leading underscore label": "_acme-challenge.example.com",
	} {
		if err := ValidatePageDomain(domain); err == nil {
			t.Errorf("%s: ValidatePageDomain(%q) accepted it", name, domain)
		}
	}

	// The counterpart: "reject everything that is not [a-z]" would satisfy the
	// table above, so the domains real pages use must still pass.
	for _, domain := range []string{
		"blog",
		"site.example.com",
		"my-site.example.com",
		"9front.example.com",
		"a.b.c.example.com",
		"renamed-site.example.com",
	} {
		if err := ValidatePageDomain(domain); err != nil {
			t.Errorf("ValidatePageDomain(%q) rejected an ordinary domain: %v", domain, err)
		}
	}
}

func TestGfehFQDNUnchangedByTheShare(t *testing.T) {
	t.Parallel()

	if got := gfehFQDN("s3.gfeh", "office"); got != "s3.gfeh.office" {
		t.Errorf("gfehFQDN = %q, want s3.gfeh.office", got)
	}
	// Idempotent, so a label that arrives qualified does not become
	// s3.gfeh.home.home.
	if got := gfehFQDN("s3.gfeh.home", "home"); got != "s3.gfeh.home" {
		t.Errorf("gfehFQDN(qualified) = %q, want s3.gfeh.home", got)
	}
	if got := gfehFQDN("s3 gfeh", "home"); got != "" {
		t.Errorf("gfehFQDN(illegal) = %q, want \"\"", got)
	}
}

// A page and a package qualify differently, and that is intended rather than
// an inconsistency the shared composer should have removed.
//
// A package name has no dots of its own, so pkg.repo.tld is always composed. A
// page's domain may be the operator's own FQDN, and isPublicFQDN classifies any
// dotted name not ending in the TLD as exactly that — so "gitea.default" is
// read as a public domain and served verbatim. Both go through the same
// validation; only the qualification differs.
func TestPageAndPackageQualifyDifferentlyOnPurpose(t *testing.T) {
	t.Parallel()

	if got := packageFQDN("default", "gitea", "home"); got != "gitea.default.home" {
		t.Errorf("packageFQDN = %q, want gitea.default.home", got)
	}
	// Dotted, so read as public and left alone — not qualified to
	// gitea.default.home.
	if got := pageHostname("gitea.default", "home"); got != "gitea.default" {
		t.Errorf("pageHostname(dotted) = %q, want gitea.default (verbatim)", got)
	}
	// A bare label is qualified, as a page's own name normally is.
	site := account.PageSite{Name: "blog"}
	if got := pageFQDN(nil, site, "home"); got != "blog.home" {
		t.Errorf("pageFQDN(bare) = %q, want blog.home", got)
	}
}

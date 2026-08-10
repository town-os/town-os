package account

import (
	"errors"
	"strings"
	"testing"
)

// A page's domain is not a label. It names the page's btrfs subvolume, its
// webroot symlink, the directory the static server roots on, its leaf
// certificate SAN, its DANE TLSA owner, and its ingress vhost — so it has to be
// a hostname, and a hostname is what ValidatePageDomain means.
//
// The rejected set below is in three groups, and they fail for different
// reasons worth keeping distinct:
//
//   - traversal and separators, which is the security case: filepath.Join
//     collapses "..", so a domain carrying one addresses a sibling of the pages
//     tree under the btrfs base (tls/, installed/, data/db/);
//   - Caddyfile and control characters, which restructure the rendered ingress
//     config or the file the page's own name is written into;
//   - characters that are simply not in a hostname — underscore above all,
//     which is where a "close enough" check usually stops.

func TestValidatePageDomainAccepts(t *testing.T) {
	t.Parallel()

	for _, domain := range []string{
		"blog",
		"blog.home",
		"site.example.com",
		"a.b.c.d.example.com",
		"xn--80ak6aa92e.com",
		"my-site.example.com",
		"9front.example.com",
		"site.example.com.", // a fully-qualified name may carry the root dot
		strings.Repeat("a", 63) + ".example.com",
	} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			if err := ValidatePageDomain(domain); err != nil {
				t.Errorf("ValidatePageDomain(%q) = %v, want accepted", domain, err)
			}
		})
	}
}

// TestValidatePageDomainRejectsUnderscore is its own test because underscore is
// the one that looks harmless.
//
// It is not a hostname character (RFC 1123), and more concretely it cannot
// appear in a certificate's dNSName SAN — so a page named with one resolves and
// then fails the TLS handshake, because the local CA cannot issue a leaf that
// covers it. A validator that lets it through produces a page that is published
// everywhere and served nowhere.
func TestValidatePageDomainRejectsUnderscore(t *testing.T) {
	t.Parallel()

	for _, domain := range []string{
		"my_site",
		"my_site.example.com",
		"site.my_zone.example.com",
		"_acme-challenge.example.com",
		"_dmarc.example.com",
	} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			err := ValidatePageDomain(domain)
			if err == nil {
				t.Fatalf("ValidatePageDomain(%q) was accepted; underscore is not a hostname character "+
					"and cannot appear in a certificate SAN", domain)
			}
			if !errors.Is(err, ErrPageInvalidDomain) {
				t.Errorf("ValidatePageDomain(%q) = %v, want ErrPageInvalidDomain", domain, err)
			}
		})
	}
}

func TestValidatePageDomainRejects(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		domain string
		// wantRequired marks the inputs that are "no domain at all" rather than
		// "not a hostname", which callers distinguish.
		wantRequired bool
	}{
		"empty":            {domain: "", wantRequired: true},
		"whitespace only":  {domain: "   ", wantRequired: true},
		"root dot only":    {domain: "."},
		"parent traversal": {domain: "../escape.example.com"},
		"deep traversal":   {domain: "../../../../etc/cron.d"},
		"absolute path":    {domain: "/etc/cron.d"},
		"embedded slash":   {domain: "site.example.com/../../etc"},
		"backslash":        {domain: `site\example.com`},
		"newline":          {domain: "site.example.com\nblog.example.com"},
		"carriage return":  {domain: "site.example.com\rblog.example.com"},
		"null byte":        {domain: "site.example.com\x00"},
		"space":            {domain: "site.example.com other.example.com"},
		"tab":              {domain: "site.example.com\tother"},
		"open brace":       {domain: "site.example.com {"},
		"comment":          {domain: "site.example.com # x"},
		"colon port":       {domain: "site.example.com:8080"},
		"empty label":      {domain: "site..example.com"},
		"leading dot":      {domain: ".example.com"},
		"leading dash":     {domain: "-site.example.com"},
		"trailing dash":    {domain: "site-.example.com"},
		"label too long":   {domain: strings.Repeat("a", 64) + ".example.com"},
		"name too long":    {domain: strings.Repeat("a.", 200) + "example.com"},
		"at sign":          {domain: "site@example.com"},
		"unicode":          {domain: "sité.example.com"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePageDomain(tc.domain)
			if err == nil {
				t.Fatalf("ValidatePageDomain(%q) was accepted", tc.domain)
			}
			if tc.wantRequired {
				if !errors.Is(err, ErrPageDomainRequired) {
					t.Errorf("ValidatePageDomain(%q) = %v, want ErrPageDomainRequired", tc.domain, err)
				}
				return
			}
			if !errors.Is(err, ErrPageInvalidDomain) {
				t.Errorf("ValidatePageDomain(%q) = %v, want ErrPageInvalidDomain", tc.domain, err)
			}
		})
	}
}

// The manager enforces it on both paths. Create was already checking something;
// Update was not, which is the gap that let a domain be edited into a value
// creation would have refused.
func TestPagesManagerRejectsUnderscoreDomainOnCreateAndUpdate(t *testing.T) {
	t.Parallel()
	mgr := initPagesTestDB(t)

	if _, err := mgr.Create(t.Context(), "bad", "", "", "my_site.example.com", PageSourceArchive, "", "", ""); err == nil {
		t.Error("Create accepted an underscore domain")
	}

	if _, err := mgr.Create(t.Context(), "good", "", "", "ok.example.com", PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("Create with a valid domain: %v", err)
	}
	bad := "my_site.example.com"
	if _, err := mgr.Update(t.Context(), "good", PageSiteUpdate{Domain: &bad}); err == nil {
		t.Error("Update accepted an underscore domain")
	}

	// The page keeps the domain it had, rather than being left half-edited.
	page, err := mgr.Get(t.Context(), "good")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if page.Domain != "ok.example.com" {
		t.Errorf("domain = %q after a rejected update, want %q", page.Domain, "ok.example.com")
	}
}

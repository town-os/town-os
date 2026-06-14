// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"testing"

	"gitea.com/town-os/town-os/src/account"
)

func TestPageHostname(t *testing.T) {
	cases := []struct {
		name   string
		domain string
		tld    string
		want   string
	}{
		{"bare label gets tld appended", "blog", "home", "blog.home"},
		{"already-qualified internal name kept", "blog.home", "home", "blog.home"},
		{"public fqdn kept verbatim", "blog.example.com", "home", "blog.example.com"},
		{"trailing dot trimmed", "blog.", "home", "blog.home"},
		{"empty domain yields empty", "", "home", ""},
		{"empty tld returns domain", "blog", "", "blog"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pageHostname(tc.domain, tc.tld); got != tc.want {
				t.Fatalf("pageHostname(%q, %q) = %q, want %q", tc.domain, tc.tld, got, tc.want)
			}
		})
	}
}

func TestPageIsPublic(t *testing.T) {
	cases := []struct {
		domain string
		tld    string
		want   bool
	}{
		{"blog", "home", false},          // bare label → internal
		{"blog.home", "home", false},     // ends in tld → internal
		{"blog.example.com", "home", true}, // real public FQDN
		{"localhost", "home", false},
	}
	for _, tc := range cases {
		if got := pageIsPublic(tc.domain, tc.tld); got != tc.want {
			t.Errorf("pageIsPublic(%q, %q) = %v, want %v", tc.domain, tc.tld, got, tc.want)
		}
	}
}

func TestPageDomainDefaultsToName(t *testing.T) {
	if got := pageDomain(account.PageSite{Name: "blog"}); got != "blog" {
		t.Fatalf("expected default domain to be the name, got %q", got)
	}
	if got := pageDomain(account.PageSite{Name: "blog", Domain: "diary"}); got != "diary" {
		t.Fatalf("expected explicit domain, got %q", got)
	}
}

// pagesTestManager is a minimal in-memory PagesManager for exercising the
// DNS-collection helpers without a database.
type pagesTestManager struct {
	pages []account.PageSite
}

func (m *pagesTestManager) Create(name, repoURL, branch, domain, sourceType, image, imageDirectory string) (*account.PageSite, error) {
	p := account.PageSite{Name: name, Domain: domain, SourceType: sourceType}
	m.pages = append(m.pages, p)
	return &p, nil
}
func (m *pagesTestManager) Get(name string) (*account.PageSite, error) {
	for i := range m.pages {
		if m.pages[i].Name == name {
			return &m.pages[i], nil
		}
	}
	return nil, account.ErrPageNotFound
}
func (m *pagesTestManager) Update(string, account.PageSiteUpdate) (*account.PageSite, error) {
	return nil, account.ErrPageNotFound
}
func (m *pagesTestManager) Remove(string) error          { return nil }
func (m *pagesTestManager) List() ([]account.PageSite, error) { return m.pages, nil }

func TestCollectPageHostnames(t *testing.T) {
	mgr := &pagesTestManager{pages: []account.PageSite{
		{Name: "blog"},                         // → blog.home (internal)
		{Name: "docs", Domain: "documentation"}, // → documentation.home (internal)
		{Name: "shop", Domain: "shop.example.com"}, // public → excluded
		{Name: "dup", Domain: "blog"},           // duplicate hostname → deduped
	}}

	got := collectPageHostnames(mgr, "home")

	want := map[string]bool{"blog.home": true, "documentation.home": true}
	if len(got) != len(want) {
		t.Fatalf("expected %d hostnames, got %d: %v", len(want), len(got), got)
	}
	for _, h := range got {
		if !want[h] {
			t.Errorf("unexpected hostname %q (public FQDNs must be excluded)", h)
		}
	}
}

func TestCollectPageHostnamesNilManager(t *testing.T) {
	if got := collectPageHostnames(nil, "home"); got != nil {
		t.Fatalf("expected nil for nil manager, got %v", got)
	}
}

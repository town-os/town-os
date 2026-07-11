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

func (m *pagesTestManager) Create(name, repoURL, branch, domain, sourceType, image, imageDirectory, network string) (*account.PageSite, error) {
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

// A page's TLD comes from its own network, not the global dns_tld — the
// page-side twin of networkTLDValue for packages.
func TestPageNetworkTLD(t *testing.T) {
	nm := account.InitMockNetworkManager()
	if _, err := nm.Create(&account.Network{
		Name: "fart", TLD: "fart", Subnet: "10.65.0.0/24", Address: "10.65.0.1/24",
		PublicKey: "PUB", ListenPort: 51820, Enabled: true,
	}); err != nil {
		t.Fatalf("create network: %v", err)
	}

	if got := pageNetworkTLD(nm, "fart", "home"); got != "fart" {
		t.Errorf("fart network: got %q, want fart", got)
	}
	// The default network, an empty network, an unknown network, and a nil
	// manager all fall back to the global dns_tld.
	for _, network := range []string{"", account.DefaultNetworkName, "nonexistent"} {
		if got := pageNetworkTLD(nm, network, "home"); got != "home" {
			t.Errorf("network %q: got %q, want the global home", network, got)
		}
	}
	if got := pageNetworkTLD(nil, "fart", "home"); got != "home" {
		t.Errorf("nil manager: got %q, want home", got)
	}
}

// pageFQDN is the one string the page's DNS record, leaf SAN, TLSA owner,
// ingress vhost AND on-disk subvolume/symlink must all agree on.
func TestPageFQDNUsesNetworkTLD(t *testing.T) {
	nm := account.InitMockNetworkManager()
	if _, err := nm.Create(&account.Network{
		Name: "fart", TLD: "fart", Subnet: "10.65.0.0/24", Address: "10.65.0.1/24",
		PublicKey: "PUB", ListenPort: 51820, Enabled: true,
	}); err != nil {
		t.Fatalf("create network: %v", err)
	}

	if got := pageFQDN(nm, account.PageSite{Name: "blog", Network: "fart"}, "home"); got != "blog.fart" {
		t.Errorf("fart page: got %q, want blog.fart", got)
	}
	if got := pageFQDN(nm, account.PageSite{Name: "blog"}, "home"); got != "blog.home" {
		t.Errorf("default-network page: got %q, want blog.home", got)
	}
	// A public FQDN is used verbatim regardless of network.
	if got := pageFQDN(nm, account.PageSite{Name: "shop", Domain: "shop.example.com", Network: "fart"}, "home"); got != "shop.example.com" {
		t.Errorf("public page: got %q, want shop.example.com", got)
	}
}

// The global home zone must contain ONLY default-network pages. A page on a
// WireGuard network lives under that network's TLD and is dual-homed by
// RebuildNetworkDNS — publishing it here too would resurrect the "resolves as
// .home" bug and make ReconcileDNS's remove pass fight the scoped registration.
func TestCollectPageHostnamesExcludesNetworkPages(t *testing.T) {
	mgr := &pagesTestManager{pages: []account.PageSite{
		{Name: "blog"},                     // default network → blog.home
		{Name: "wiki", Network: "home"},    // explicit default → wiki.home
		{Name: "secret", Network: "fart"},  // non-default → excluded from home
	}}

	got := collectPageHostnames(mgr, "home")

	want := map[string]bool{"blog.home": true, "wiki.home": true}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for _, h := range got {
		if !want[h] {
			t.Fatalf("unexpected hostname %q in the global home zone: %v", h, got)
		}
	}
}

// collectNetworkPageHostnames is the inverse: only the given network's pages,
// named under that network's TLD.
func TestCollectNetworkPageHostnames(t *testing.T) {
	mgr := &pagesTestManager{pages: []account.PageSite{
		{Name: "blog"},                          // default → excluded
		{Name: "secret", Network: "fart"},       // → secret.fart
		{Name: "other", Network: "fart2"},       // different network → excluded
		{Name: "shop", Domain: "shop.example.com", Network: "fart"}, // public → excluded
	}}

	got := collectNetworkPageHostnames(mgr, "fart", "fart")

	if len(got) != 1 || got[0] != "secret.fart" {
		t.Fatalf("expected [secret.fart], got %v", got)
	}
}

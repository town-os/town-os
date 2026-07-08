// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/rolodex"
	"github.com/labstack/echo/v5"
)

// dnsRecordsFixture wires a handler over a network manager holding the default
// "home" network plus a non-default "office" network, and a rolodex mock whose
// global (home-zone) and office-scoped records are seeded distinctly. This lets
// the DNS records API be exercised across every network — or filtered to one.
func dnsRecordsFixture(t *testing.T) *SystemControllerHandlers {
	t.Helper()
	nm := account.InitMockNetworkManager()
	home := &account.Network{Name: account.DefaultNetworkName, TLD: "home", Subnet: "10.64.0.0/24", Address: "10.64.0.1/24", PublicKey: "PUB", ListenPort: 51820, Enabled: true}
	office := &account.Network{Name: "office", TLD: "office", Subnet: "10.90.12.0/24", Address: "10.90.12.1/24", PublicKey: "PUB", ListenPort: 51821, Enabled: true}
	if _, err := nm.Create(home); err != nil {
		t.Fatalf("seed home network: %v", err)
	}
	if _, err := nm.Create(office); err != nil {
		t.Fatalf("seed office network: %v", err)
	}

	mc := &rolodex.MockClient{
		// Global (home-zone) records.
		Records: []*upstream.DnsRecord{
			{Name: "ns1.home.", RecordType: upstream.RecordTypeA, Value: "10.64.0.1", Ttl: 300},
			{Name: "gitea.repo-a.home.", RecordType: upstream.RecordTypeA, Value: "10.64.0.5", Ttl: 300},
		},
		// office-scoped records live only in the office network's zone.
		ScopedRecords: map[string][]*upstream.DnsRecord{
			"office": {
				{Name: "gitea.repo-a.office.", RecordType: upstream.RecordTypeA, Value: "10.90.12.5", Ttl: 300},
			},
		},
	}

	sb := &serverBase{ServerConfig: ServerConfig{NetworkMgr: nm, RolodexClient: mc}}
	return &SystemControllerHandlers{Controller: sb, ctx: context.Background()}
}

// listDNSRecordsView invokes the handler with an optional `?tld=` filter and
// decodes the JSON response.
func listDNSRecordsView(t *testing.T, s *SystemControllerHandlers, tld string) []DNSRecordView {
	t.Helper()
	e := echo.New()
	target := "/dns/records"
	if tld != "" {
		target += "?tld=" + tld
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	if err := s.listDNSRecords(e.NewContext(req, rec)); err != nil {
		t.Fatalf("listDNSRecords(tld=%q): %v", tld, err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var views []DNSRecordView
	if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
		t.Fatalf("decode: %v (body: %s)", err, rec.Body.String())
	}
	return views
}

func findView(views []DNSRecordView, name string) *DNSRecordView {
	for i := range views {
		if views[i].Name == name {
			return &views[i]
		}
	}
	return nil
}

// With no filter, the API returns records across every network: the global
// home zone and each network's scoped zone, each annotated with its network/TLD.
func TestListDNSRecordsGlobalReturnsAllNetworks(t *testing.T) {
	s := dnsRecordsFixture(t)

	views := listDNSRecordsView(t, s, "")
	if len(views) != 3 {
		t.Fatalf("expected 3 records across all networks, got %d: %+v", len(views), views)
	}

	home := findView(views, "gitea.repo-a.home.")
	if home == nil {
		t.Fatalf("missing global home record in %+v", views)
	}
	if home.Network != "" || home.TLD != "home" {
		t.Errorf("home record network/tld = %q/%q, want \"\"/home", home.Network, home.TLD)
	}

	office := findView(views, "gitea.repo-a.office.")
	if office == nil {
		t.Fatalf("missing office-scoped record in %+v", views)
	}
	if office.Network != "office" || office.TLD != "office" {
		t.Errorf("office record network/tld = %q/%q, want office/office", office.Network, office.TLD)
	}
}

// `?tld=office` restricts the result to the office network's scoped zone —
// nothing from the global home zone leaks in.
func TestListDNSRecordsFilterByNetworkTLD(t *testing.T) {
	s := dnsRecordsFixture(t)

	views := listDNSRecordsView(t, s, "office")
	if len(views) != 1 {
		t.Fatalf("expected only office records, got %d: %+v", len(views), views)
	}
	if views[0].Name != "gitea.repo-a.office." || views[0].Network != "office" || views[0].TLD != "office" {
		t.Fatalf("unexpected office record: %+v", views[0])
	}
	if findView(views, "gitea.repo-a.home.") != nil {
		t.Fatal("home-zone record must not appear when filtering by tld=office")
	}
}

// `?tld=home` restricts the result to the global home zone (the default
// network), with the scoped office record excluded.
func TestListDNSRecordsFilterByHomeTLD(t *testing.T) {
	s := dnsRecordsFixture(t)

	views := listDNSRecordsView(t, s, "home")
	if len(views) != 2 {
		t.Fatalf("expected 2 home-zone records, got %d: %+v", len(views), views)
	}
	for _, v := range views {
		if v.Network != "" || v.TLD != "home" {
			t.Errorf("home-zone record has network/tld %q/%q, want \"\"/home: %+v", v.Network, v.TLD, v)
		}
	}
	if findView(views, "gitea.repo-a.office.") != nil {
		t.Fatal("office-scoped record must not appear when filtering by tld=home")
	}
}

// The tld filter is case-insensitive.
func TestListDNSRecordsFilterCaseInsensitive(t *testing.T) {
	s := dnsRecordsFixture(t)

	views := listDNSRecordsView(t, s, "OFFICE")
	if len(views) != 1 || views[0].Network != "office" {
		t.Fatalf("case-insensitive tld filter failed: %+v", views)
	}
}

// With no network manager the API falls back to the global zone only, staying
// backward compatible with a single-TLD deployment.
func TestListDNSRecordsNoNetworkManagerFallsBackToGlobal(t *testing.T) {
	mc := &rolodex.MockClient{
		Records: []*upstream.DnsRecord{
			{Name: "gitea.repo-a.home.", RecordType: upstream.RecordTypeA, Value: "10.64.0.5", Ttl: 300},
		},
	}
	sb := &serverBase{ServerConfig: ServerConfig{RolodexClient: mc}}
	s := &SystemControllerHandlers{Controller: sb, ctx: context.Background()}

	views := listDNSRecordsView(t, s, "")
	if len(views) != 1 || views[0].Name != "gitea.repo-a.home." {
		t.Fatalf("expected the single global record, got %+v", views)
	}
}

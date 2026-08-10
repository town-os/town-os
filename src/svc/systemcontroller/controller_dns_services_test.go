package systemcontroller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"github.com/labstack/echo/v5"
)

func TestDNSExcludedServicesRoundtrip(t *testing.T) {
	mgr := &mockSettingsManager{values: map[string]string{}}

	if got := loadDNSExcludedServices(t.Context(), mgr); len(got) != 0 {
		t.Fatalf("expected empty set initially, got %v", got)
	}

	set := map[string]bool{"default/nginx": true, "extras/redis": true}
	if err := saveDNSExcludedServices(t.Context(), mgr, set); err != nil {
		t.Fatalf("saveDNSExcludedServices: %v", err)
	}

	got := loadDNSExcludedServices(t.Context(), mgr)
	if len(got) != 2 || !got["default/nginx"] || !got["extras/redis"] {
		t.Fatalf("roundtrip mismatch: %v", got)
	}

	// Stored as a sorted JSON array for deterministic, diff-friendly output.
	if want := `["default/nginx","extras/redis"]`; mgr.values[settingDNSExcludedServices] != want {
		t.Fatalf("stored value = %q, want %q", mgr.values[settingDNSExcludedServices], want)
	}
}

func TestSaveDNSExcludedServicesNilManager(t *testing.T) {
	if err := saveDNSExcludedServices(t.Context(), nil, map[string]bool{"a/b": true}); err == nil {
		t.Fatal("expected error with nil settings manager")
	}
}

func TestLoadDNSExcludedServicesNilAndInvalid(t *testing.T) {
	if got := loadDNSExcludedServices(t.Context(), nil); len(got) != 0 {
		t.Fatalf("nil manager should yield empty set, got %v", got)
	}
	mgr := &mockSettingsManager{values: map[string]string{settingDNSExcludedServices: "not json"}}
	if got := loadDNSExcludedServices(t.Context(), mgr); len(got) != 0 {
		t.Fatalf("invalid JSON should yield empty set, got %v", got)
	}
}

// A package installed into a non-default network must be listed with its FQDN
// under that network's TLD (gitea.default.fart), not the global home zone — the
// DNS "Services" tab renders this FQDN directly, so the old always-dns_tld
// behavior showed gitea.default.home for a package on the fart network. A
// default-network package stays under dns_tld (home).
func TestListDNSServicesUsesNetworkTLD(t *testing.T) {
	nm := seedNetwork(t) // fart network, TLD "fart"
	inst := packages.InitMockInstallManager()
	inst.Installed = []packages.PackageIdentity{
		{Repo: "default", Name: "nginx", Version: "1.0"},
		{Repo: "default", Name: "gitea", Version: "2.0"},
	}
	// gitea lives on the fart network; nginx stays in the default (home) zone.
	if err := inst.SaveNetwork("default", "gitea", "fart"); err != nil {
		t.Fatalf("save gitea network: %v", err)
	}

	sb := &serverBase{ServerConfig: ServerConfig{NetworkMgr: nm, Installer: inst}}
	s := &SystemControllerHandlers{Controller: sb, ctx: context.Background()}

	byName := map[string]DNSServiceEntry{}
	for _, e := range listDNSServicesView(t, s) {
		byName[e.Name] = e
	}

	if got := byName["gitea"].FQDN; got != "gitea.default.fart" {
		t.Errorf("gitea on the fart network: FQDN = %q, want gitea.default.fart", got)
	}
	if got := byName["nginx"].FQDN; got != "nginx.default.home" {
		t.Errorf("nginx on the default network: FQDN = %q, want nginx.default.home", got)
	}
}

// listDNSServicesView invokes the GET /dns/services handler and decodes the JSON.
func listDNSServicesView(t *testing.T, s *SystemControllerHandlers) []DNSServiceEntry {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/dns/services", nil)
	rec := httptest.NewRecorder()
	if err := s.listDNSServices(e.NewContext(req, rec)); err != nil {
		t.Fatalf("listDNSServices: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var entries []DNSServiceEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode: %v (body: %s)", err, rec.Body.String())
	}
	return entries
}

func TestFilterExcludedDNSInfo(t *testing.T) {
	pkgs := []rolodex.PackageDNSInfo{
		{Repo: "default", Name: "nginx"},
		{Repo: "default", Name: "gitea"},
		{Repo: "extras", Name: "redis"},
	}

	out := filterExcludedDNSInfo(pkgs, map[string]bool{"default/nginx": true})
	if len(out) != 2 {
		t.Fatalf("expected 2 after filtering, got %d", len(out))
	}
	for _, p := range out {
		if p.Name == "nginx" {
			t.Fatal("nginx should have been filtered out")
		}
	}

	// The input slice must not be mutated.
	if len(pkgs) != 3 {
		t.Fatalf("input slice mutated: len=%d", len(pkgs))
	}

	// Empty/nil excluded set returns all packages.
	if got := filterExcludedDNSInfo(pkgs, nil); len(got) != 3 {
		t.Fatalf("nil excluded should return all, got %d", len(got))
	}
}

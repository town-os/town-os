package systemcontroller

import (
	"testing"

	"gitea.com/town-os/town-os/src/rolodex"
)

func TestDNSExcludedServicesRoundtrip(t *testing.T) {
	mgr := &mockSettingsManager{values: map[string]string{}}

	if got := loadDNSExcludedServices(mgr); len(got) != 0 {
		t.Fatalf("expected empty set initially, got %v", got)
	}

	set := map[string]bool{"default/nginx": true, "extras/redis": true}
	if err := saveDNSExcludedServices(mgr, set); err != nil {
		t.Fatalf("saveDNSExcludedServices: %v", err)
	}

	got := loadDNSExcludedServices(mgr)
	if len(got) != 2 || !got["default/nginx"] || !got["extras/redis"] {
		t.Fatalf("roundtrip mismatch: %v", got)
	}

	// Stored as a sorted JSON array for deterministic, diff-friendly output.
	if want := `["default/nginx","extras/redis"]`; mgr.values[settingDNSExcludedServices] != want {
		t.Fatalf("stored value = %q, want %q", mgr.values[settingDNSExcludedServices], want)
	}
}

func TestSaveDNSExcludedServicesNilManager(t *testing.T) {
	if err := saveDNSExcludedServices(nil, map[string]bool{"a/b": true}); err == nil {
		t.Fatal("expected error with nil settings manager")
	}
}

func TestLoadDNSExcludedServicesNilAndInvalid(t *testing.T) {
	if got := loadDNSExcludedServices(nil); len(got) != 0 {
		t.Fatalf("nil manager should yield empty set, got %v", got)
	}
	mgr := &mockSettingsManager{values: map[string]string{settingDNSExcludedServices: "not json"}}
	if got := loadDNSExcludedServices(mgr); len(got) != 0 {
		t.Fatalf("invalid JSON should yield empty set, got %v", got)
	}
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

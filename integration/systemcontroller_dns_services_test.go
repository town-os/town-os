// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// initDNSServicesMockTest builds a test server with a mock installer, settings,
// and rolodex client for exercising the /dns/services publish toggle.
func initDNSServicesMockTest(t *testing.T, installed []packages.PackageIdentity) (*systemcontroller.SystemdClient, *rolodex.MockClient, *mockSettingsManager) {
	t.Helper()

	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	rc := &rolodex.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}
	inst := &packages.MockInstallManager{Installed: installed}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       mock,
		Systemd:       sd,
		RolodexClient: rc,
		SettingsMgr:   settings,
		Installer:     inst,
	})
	ts.SetInternalIP("10.0.0.5")
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}
	return c, rc, settings
}

func rolodexCalled(rc *rolodex.MockClient, method string) bool {
	for _, c := range rc.GetCalls() {
		if c.Method == method {
			return true
		}
	}
	return false
}

func TestDNSServicesListAndToggle(t *testing.T) {
	t.Parallel()
	c, rc, settings := initDNSServicesMockTest(t, []packages.PackageIdentity{
		{Repo: "default", Name: "nginx", Version: "1.0"},
		{Repo: "default", Name: "gitea", Version: "2.0"},
	})
	ctx := context.Background()

	entries, err := c.ListDNSServices(ctx)
	if err != nil {
		t.Fatalf("ListDNSServices: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 services, got %d", len(entries))
	}
	for _, e := range entries {
		if !e.Published {
			t.Errorf("service %s/%s should be published by default", e.Repo, e.Name)
		}
	}
	// Entries are sorted by repo then name: gitea before nginx.
	if entries[0].Name != "gitea" || entries[0].FQDN != "gitea.default.home" {
		t.Fatalf("unexpected first entry: %+v", entries[0])
	}

	// Unpublish nginx.
	if err := c.SetDNSService(ctx, "default", "nginx", false); err != nil {
		t.Fatalf("SetDNSService unpublish: %v", err)
	}
	if !strings.Contains(settings.values["dns_excluded_services"], "default/nginx") {
		t.Fatalf("excluded setting should contain default/nginx, got %q", settings.values["dns_excluded_services"])
	}
	if !rolodexCalled(rc, "RemoveRecord") {
		t.Error("expected rolodex RemoveRecord on unpublish")
	}

	entries, err = c.ListDNSServices(ctx)
	if err != nil {
		t.Fatalf("ListDNSServices after unpublish: %v", err)
	}
	for _, e := range entries {
		if e.Name == "nginx" && e.Published {
			t.Error("nginx should be unpublished")
		}
		if e.Name == "gitea" && !e.Published {
			t.Error("gitea should remain published")
		}
	}

	// Republish nginx.
	if err := c.SetDNSService(ctx, "default", "nginx", true); err != nil {
		t.Fatalf("SetDNSService republish: %v", err)
	}
	if strings.Contains(settings.values["dns_excluded_services"], "default/nginx") {
		t.Fatalf("excluded setting should no longer contain default/nginx, got %q", settings.values["dns_excluded_services"])
	}
	if !rolodexCalled(rc, "AddRecord") {
		t.Error("expected rolodex AddRecord on republish")
	}
}

func TestDNSServicesSetNotInstalled(t *testing.T) {
	t.Parallel()
	c, _, _ := initDNSServicesMockTest(t, nil)
	ctx := context.Background()

	if err := c.SetDNSService(ctx, "default", "ghost", false); err == nil {
		t.Fatal("expected error setting a non-installed service")
	}
}

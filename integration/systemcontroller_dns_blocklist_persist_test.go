// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// blocklistPersistEnv is everything a blocklist-persistence test needs to look
// at: the API client, the fake rolodex behind it, the settings the controller
// persists into, and the rolodex.yml a restarting rolodex would read.
type blocklistPersistEnv struct {
	client   *systemcontroller.SystemdClient
	rolodex  *rolodex.MockClient
	settings *mockSettingsManager
	dataDir  string
}

func (e blocklistPersistEnv) configFile(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.dataDir, "rolodex.yml")) //nolint:gosec // G304 -- path built from the test's own temp dir
	if err != nil {
		t.Fatalf("read rolodex.yml: %v", err)
	}
	return string(data)
}

func initBlocklistPersistTest(t *testing.T) blocklistPersistEnv {
	t.Helper()

	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	rc := &rolodex.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}

	dataDir := rolodexTempDir(t, "dns-blocklist-persist-*")
	rolMgr := rolodex.NewManager(rolodex.Config{
		Systemd:        sd,
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: filepath.Join(dataDir, "dns-test.sock"),
	})

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       mock,
		Systemd:       sd,
		Rolodex:       rolMgr,
		RolodexClient: rc,
		SettingsMgr:   settings,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return blocklistPersistEnv{client: c, rolodex: rc, settings: settings, dataDir: dataDir}
}

// Saving a blocklist must persist it and render it into rolodex.yml, not just
// push it into rolodex's memory. Rolodex keeps the provider lists in memory
// only — it seeds them from this file and persists nothing a gRPC call
// changes — so before this, every toggle an operator turned on turned itself
// back off at the next rolodex restart.
func TestDNSBlocklistConfigIsPersistedAndRendered(t *testing.T) {
	t.Parallel()
	env := initBlocklistPersistTest(t)
	ctx := context.Background()

	if err := env.client.SetDnsblConfig(ctx, true, []systemcontroller.RblProviderDTO{
		{Zone: "dbl.spamhaus.org", Enabled: true},
		{Zone: "multi.surbl.org", Enabled: false},
	}, 900); err != nil {
		t.Fatalf("SetDnsblConfig: %v", err)
	}
	if err := env.client.SetRblConfig(ctx, true, []systemcontroller.RblProviderDTO{
		{Zone: "zen.spamhaus.org", Enabled: true},
	}, 0); err != nil {
		t.Fatalf("SetRblConfig: %v", err)
	}

	if v := env.settings.values["dns_dnsbl_config"]; !strings.Contains(v, "dbl.spamhaus.org") {
		t.Fatalf("DNSBL config not persisted to settings: %q", v)
	}
	if v := env.settings.values["dns_rbl_config"]; !strings.Contains(v, "zen.spamhaus.org") {
		t.Fatalf("RBL config not persisted to settings: %q", v)
	}

	cfg := env.configFile(t)
	for _, want := range []string{
		"dnsbl:\n  enabled: true\n  refusal_cooldown_secs: 900\n",
		"    - zone: \"dbl.spamhaus.org\"\n      enabled: true\n",
		"    - zone: \"multi.surbl.org\"\n      enabled: false\n",
		"rbl:\n  enabled: true\n  providers:\n    - zone: \"zen.spamhaus.org\"\n      enabled: true\n",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("rolodex.yml missing %q:\n%s", want, cfg)
		}
	}
}

// The reported failure, end to end: rolodex comes back with its blocklists
// wiped, and the next DNS reconcile puts them back on.
func TestDNSBlocklistRestoredAfterRolodexLosesConfig(t *testing.T) {
	t.Parallel()
	env := initBlocklistPersistTest(t)
	ctx := context.Background()

	if err := env.client.SetDnsblConfig(ctx, true, []systemcontroller.RblProviderDTO{
		{Zone: "dbl.spamhaus.org", Enabled: true},
	}, 0); err != nil {
		t.Fatalf("SetDnsblConfig: %v", err)
	}
	if err := env.client.SetRblConfig(ctx, true, []systemcontroller.RblProviderDTO{
		{Zone: "zen.spamhaus.org", Enabled: true},
	}, 0); err != nil {
		t.Fatalf("SetRblConfig: %v", err)
	}

	// Rolodex restarts: the in-memory lists are gone.
	if err := env.rolodex.SetDnsblConfig(ctx, false, nil, 0); err != nil {
		t.Fatalf("wipe DNSBL: %v", err)
	}
	if err := env.rolodex.SetRblConfig(ctx, false, nil, 0); err != nil {
		t.Fatalf("wipe RBL: %v", err)
	}
	if got, err := env.client.GetDnsblConfig(ctx); err != nil {
		t.Fatalf("GetDnsblConfig: %v", err)
	} else if got.Enabled {
		t.Fatal("test setup did not actually wipe the DNSBL config")
	}

	if err := systemcontroller.ReconcileBlocklists(ctx, env.rolodex, env.settings); err != nil {
		t.Fatalf("ReconcileBlocklists: %v", err)
	}

	dnsbl, err := env.client.GetDnsblConfig(ctx)
	if err != nil {
		t.Fatalf("GetDnsblConfig: %v", err)
	}
	if !dnsbl.Enabled || len(dnsbl.Providers) != 1 || dnsbl.Providers[0].Zone != "dbl.spamhaus.org" {
		t.Fatalf("DNSBL not restored: %+v", dnsbl)
	}

	rbl, err := env.client.GetRblConfig(ctx)
	if err != nil {
		t.Fatalf("GetRblConfig: %v", err)
	}
	if !rbl.Enabled || len(rbl.Providers) != 1 || rbl.Providers[0].Zone != "zen.spamhaus.org" {
		t.Fatalf("RBL not restored: %+v", rbl)
	}
}

// Turning a blocklist off is just as much an instruction as turning it on: the
// restore must not resurrect a list the operator disabled.
func TestDNSBlocklistDisabledStateSurvivesReconcile(t *testing.T) {
	t.Parallel()
	env := initBlocklistPersistTest(t)
	ctx := context.Background()

	if err := env.client.SetRblConfig(ctx, true, []systemcontroller.RblProviderDTO{
		{Zone: "zen.spamhaus.org", Enabled: true},
	}, 0); err != nil {
		t.Fatalf("SetRblConfig (on): %v", err)
	}
	if err := env.client.SetRblConfig(ctx, false, []systemcontroller.RblProviderDTO{
		{Zone: "zen.spamhaus.org", Enabled: true},
	}, 0); err != nil {
		t.Fatalf("SetRblConfig (off): %v", err)
	}

	if err := systemcontroller.ReconcileBlocklists(ctx, env.rolodex, env.settings); err != nil {
		t.Fatalf("ReconcileBlocklists: %v", err)
	}

	rbl, err := env.client.GetRblConfig(ctx)
	if err != nil {
		t.Fatalf("GetRblConfig: %v", err)
	}
	if rbl.Enabled {
		t.Fatal("a blocklist the operator turned off must stay off")
	}
	if cfg := env.configFile(t); !strings.Contains(cfg, "rbl:\n  enabled: false\n") {
		t.Fatalf("rolodex.yml must render the disabled state:\n%s", cfg)
	}
}

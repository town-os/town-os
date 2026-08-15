// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// blocklistPersistEnv is everything a blocklist-persistence test needs to look
// at: the API client, the fake rolodex behind it, and the settings the
// controller persists into.
type blocklistPersistEnv struct {
	client   *systemcontroller.SystemdClient
	rolodex  *rolodex.MockClient
	settings *mockSettingsManager
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

	return blocklistPersistEnv{client: c, rolodex: rc, settings: settings}
}

// Saving a blocklist must persist it to SETTINGS, not just push it into
// rolodex's memory. Rolodex holds the provider lists in memory only and
// persists nothing a gRPC call changes, so the stored copy is the entire reason
// a toggle an operator turned on is still on after the next rolodex restart —
// ReconcileBlocklists (the test below) has nothing to restore from without it.
//
// This used to assert the same config rendered into rolodex.yml as well. Town OS
// no longer writes that file at all — it belongs to the install image, which
// renders only what cannot be programmed at runtime — so the rendering half was
// asserting behaviour that has been deliberately removed. What replaces it is
// the push itself: the config the operator saved is what the running server is
// holding when the call returns.
func TestDNSBlocklistConfigIsPersistedAndProgrammed(t *testing.T) {
	t.Parallel()
	env := initBlocklistPersistTest(t)
	ctx := context.Background()

	if err := env.client.SetDnsblConfig(ctx, true, []systemcontroller.BlocklistProviderDTO{
		{Zone: "dbl.spamhaus.org", Enabled: true},
		{Zone: "multi.surbl.org", Enabled: false},
	}, 900); err != nil {
		t.Fatalf("SetDnsblConfig: %v", err)
	}

	// Persisted: what a restart is repaired from.
	stored := env.settings.values["dns_dnsbl_config"]
	for _, want := range []string{"dbl.spamhaus.org", "multi.surbl.org", "900"} {
		if !strings.Contains(stored, want) {
			t.Fatalf("DNSBL config not persisted to settings (missing %q): %q", want, stored)
		}
	}

	// Programmed: what the box is filtering on right now.
	got, err := env.client.GetDnsblConfig(ctx)
	if err != nil {
		t.Fatalf("GetDnsblConfig: %v", err)
	}
	if !got.Enabled {
		t.Errorf("DNSBL reads back disabled: %+v", got)
	}
	if got.RefusalCooldownSecs != 900 {
		t.Errorf("refusal cooldown = %d, want 900", got.RefusalCooldownSecs)
	}
	if len(got.Providers) != 2 {
		t.Fatalf("providers = %d, want the two that were saved: %+v", len(got.Providers), got.Providers)
	}
	// The disabled one is programmed too, and programmed as disabled: a provider
	// dropped on the way out would come back on at the next reconcile.
	for _, want := range []struct {
		zone    string
		enabled bool
	}{{"dbl.spamhaus.org", true}, {"multi.surbl.org", false}} {
		var found bool
		for _, p := range got.Providers {
			if p.Zone != want.zone {
				continue
			}
			found = true
			if p.Enabled != want.enabled {
				t.Errorf("provider %s enabled = %v, want %v", want.zone, p.Enabled, want.enabled)
			}
		}
		if !found {
			t.Errorf("provider %s was not programmed: %+v", want.zone, got.Providers)
		}
	}
}

// The reported failure, end to end: rolodex comes back with its blocklists
// wiped, and the next DNS reconcile puts them back on.
func TestDNSBlocklistRestoredAfterRolodexLosesConfig(t *testing.T) {
	t.Parallel()
	env := initBlocklistPersistTest(t)
	ctx := context.Background()

	if err := env.client.SetDnsblConfig(ctx, true, []systemcontroller.BlocklistProviderDTO{
		{Zone: "dbl.spamhaus.org", Enabled: true},
	}, 0); err != nil {
		t.Fatalf("SetDnsblConfig: %v", err)
	}
	// Rolodex restarts: the in-memory lists are gone.
	if err := env.rolodex.SetDnsblConfig(ctx, false, nil, 0); err != nil {
		t.Fatalf("wipe DNSBL: %v", err)
	}
	if err := env.rolodex.SetDnsblConfig(ctx, false, nil, 0); err != nil {
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
}

// Turning a blocklist off is just as much an instruction as turning it on: the
// restore must not resurrect a list the operator disabled.
func TestDNSBlocklistDisabledStateSurvivesReconcile(t *testing.T) {
	t.Parallel()
	env := initBlocklistPersistTest(t)
	ctx := context.Background()

	if err := env.client.SetDnsblConfig(ctx, true, []systemcontroller.BlocklistProviderDTO{
		{Zone: "dbl.spamhaus.org", Enabled: true},
	}, 0); err != nil {
		t.Fatalf("SetDnsblConfig (on): %v", err)
	}
	if err := env.client.SetDnsblConfig(ctx, false, []systemcontroller.BlocklistProviderDTO{
		{Zone: "dbl.spamhaus.org", Enabled: true},
	}, 0); err != nil {
		t.Fatalf("SetDnsblConfig (off): %v", err)
	}

	if err := systemcontroller.ReconcileBlocklists(ctx, env.rolodex, env.settings); err != nil {
		t.Fatalf("ReconcileBlocklists: %v", err)
	}
}

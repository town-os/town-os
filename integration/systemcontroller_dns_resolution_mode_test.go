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

// initResolutionModeTest wires a real rolodex.Manager (writing into a temp data
// dir) behind the real HTTP settings API, with a mock systemd so we can see the
// restart it asks for.
func initResolutionModeTest(t *testing.T) (*systemcontroller.SystemdClient, *systemd.MockManager, *rolodex.Manager, string) {
	t.Helper()

	sd := systemd.InitMockManager()
	settings := &mockSettingsManager{values: map[string]string{
		"dns_tld":             "home",
		"dns_resolution_mode": rolodex.ResolutionModeAuto,
	}}

	dataDir := rolodexTempDir(t, "resolution-mode-*")
	rolMgr := rolodex.NewManager(rolodex.Config{
		Systemd:        sd,
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: filepath.Join(dataDir, "rolodex.sock"),
	})

	// Lay down the config the way boot does, so the test starts from the same
	// on-disk state a running box has.
	if _, err := rolMgr.RewriteConfig(); err != nil {
		t.Fatalf("RewriteConfig: %v", err)
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       storage.InitBtrFSMock(),
		Systemd:       sd,
		Rolodex:       rolMgr,
		RolodexClient: &rolodex.MockClient{},
		SettingsMgr:   settings,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, sd, rolMgr, dataDir
}

// TestIntegrationSetDNSResolutionModeRewritesConfigAndRestartsRolodex drives the
// real settings endpoint and asserts the operator-visible contract: flipping
// dns_resolution_mode must (a) land in rolodex.yml on disk and (b) restart
// rolodex, so the change takes effect NOW rather than at the next boot.
//
// The on-disk assertion is the load-bearing one. rolodex.yml written at the
// previous boot is always newer than the systemcontroller binary, which is
// exactly the condition WriteConfig treats as "user-modified, do not touch". A
// handler that called WriteConfig would return 200 and change nothing.
func TestIntegrationSetDNSResolutionModeRewritesConfigAndRestartsRolodex(t *testing.T) {
	t.Parallel()

	c, sd, rolMgr, dataDir := initResolutionModeTest(t)
	ctx := context.Background()

	assertRolodexMode(t, dataDir, rolodex.ResolutionModeAuto)

	if err := c.SetSetting(ctx, "dns_resolution_mode", rolodex.ResolutionModeForward); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	assertRolodexMode(t, dataDir, rolodex.ResolutionModeForward)

	if !rolodexRestartRequested(sd, rolMgr.UnitName()) {
		t.Fatalf("rolodex was not restarted; systemd calls: %+v", sd.Calls)
	}

	// And back again — the switch must not be one-way.
	if err := c.SetSetting(ctx, "dns_resolution_mode", rolodex.ResolutionModeRecursive); err != nil {
		t.Fatalf("SetSetting back to recursive: %v", err)
	}
	assertRolodexMode(t, dataDir, rolodex.ResolutionModeRecursive)

	if err := c.SetSetting(ctx, "dns_resolution_mode", rolodex.ResolutionModeAuto); err != nil {
		t.Fatalf("SetSetting back to auto: %v", err)
	}
	assertRolodexMode(t, dataDir, rolodex.ResolutionModeAuto)
}

// TestIntegrationSetDNSResolutionModeRejectsGarbage: an unparseable mode in
// rolodex.yml makes rolodex refuse to start, which takes DNS down for the entire
// box. The API must reject it before it ever reaches disk.
func TestIntegrationSetDNSResolutionModeRejectsGarbage(t *testing.T) {
	t.Parallel()

	c, _, _, dataDir := initResolutionModeTest(t)
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_resolution_mode", "iterative"); err == nil {
		t.Fatal("expected an invalid resolution mode to be rejected")
	}

	// The config on disk must be untouched — no half-applied change.
	assertRolodexMode(t, dataDir, rolodex.ResolutionModeAuto)
}

func rolodexRestartRequested(sd *systemd.MockManager, want string) bool {
	for _, call := range sd.Calls {
		if call.Method != "SetStatus" || len(call.Args) < 2 {
			continue
		}
		unit, ok := call.Args[0].(string)
		if !ok || unit != want {
			continue
		}
		action, ok := call.Args[1].(systemd.StatusAction)
		if ok && action == systemd.Restart {
			return true
		}
	}
	return false
}

func assertRolodexMode(t *testing.T, dataDir, want string) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dataDir, "rolodex.yml")) //nolint:gosec // test-controlled temp dir
	if err != nil {
		t.Fatalf("read rolodex.yml: %v", err)
	}
	if !strings.Contains(string(data), "  mode: "+want) {
		t.Fatalf("rolodex.yml does not select mode %q:\n%s", want, data)
	}
}

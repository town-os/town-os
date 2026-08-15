// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// initResolutionModeTest stands a controller up behind the real HTTP settings
// API with a fake rolodex server, so a test can see exactly what the handler
// programs — and a mock systemd, so it can see that it asks for no restart.
func initResolutionModeTest(t *testing.T) (*systemcontroller.SystemdClient, *systemd.MockManager, *rolodex.Manager, *rolodex.MockClient) {
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

	// No config file is laid down, because a running box has none that Town OS
	// wrote: rolodex.yml carries the install image's binds and metrics listener
	// and nothing this test touches.
	rolClient := &rolodex.MockClient{ResolutionMode: rolodex.ResolutionModeAuto}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       storage.InitBtrFSMock(),
		Systemd:       sd,
		Rolodex:       rolMgr,
		RolodexClient: rolClient,
		SettingsMgr:   settings,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, sd, rolMgr, rolClient
}

// TestIntegrationSetDNSResolutionModeProgramsRolodexWithoutRestarting drives the
// real settings endpoint and asserts the operator-visible contract: flipping
// dns_resolution_mode must reach the RUNNING rolodex immediately, and must not
// restart it.
//
// Both halves are load-bearing. This setting used to be applied by rewriting
// rolodex.yml and bouncing the unit, which meant every change to a dropdown
// took the box's DNS down for the length of a container restart — and, for a
// long time, silently did nothing at all, because the write was skipped and the
// restart re-read the same file.
func TestIntegrationSetDNSResolutionModeProgramsRolodexWithoutRestarting(t *testing.T) {
	t.Parallel()

	c, sd, rolMgr, rolClient := initResolutionModeTest(t)
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_resolution_mode", rolodex.ResolutionModeForward); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	assertProgrammedMode(t, rolClient, rolodex.ResolutionModeForward)

	if rolodexRestartRequested(sd, rolMgr.UnitName()) {
		t.Fatalf("rolodex was restarted to apply a runtime setting; systemd calls: %+v", sd.Calls)
	}

	// And back again — the switch must not be one-way.
	if err := c.SetSetting(ctx, "dns_resolution_mode", rolodex.ResolutionModeRecursive); err != nil {
		t.Fatalf("SetSetting back to recursive: %v", err)
	}
	assertProgrammedMode(t, rolClient, rolodex.ResolutionModeRecursive)

	if err := c.SetSetting(ctx, "dns_resolution_mode", rolodex.ResolutionModeAuto); err != nil {
		t.Fatalf("SetSetting back to auto: %v", err)
	}
	assertProgrammedMode(t, rolClient, rolodex.ResolutionModeAuto)
}

// TestIntegrationSetDNSResolutionModeRejectsGarbage: rolodex refuses an
// unrecognized mode outright rather than defaulting, so an unvalidated value
// from the API is a failed RPC and a box left in a mode nobody selected. The
// API must reject it before it is sent, and must leave the live mode alone.
func TestIntegrationSetDNSResolutionModeRejectsGarbage(t *testing.T) {
	t.Parallel()

	c, _, _, rolClient := initResolutionModeTest(t)
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_resolution_mode", "iterative"); err == nil {
		t.Fatal("expected an invalid resolution mode to be rejected")
	}

	// No half-applied change: the running server still holds what it had.
	assertProgrammedMode(t, rolClient, rolodex.ResolutionModeAuto)
	if rolClient.Called("SetResolutionMode") {
		t.Error("an invalid mode was sent to rolodex before validation")
	}
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

func assertProgrammedMode(t *testing.T, client *rolodex.MockClient, want string) {
	t.Helper()

	if got := client.ResolutionMode; got != want {
		t.Fatalf("rolodex is holding mode %q, want %q", got, want)
	}
}

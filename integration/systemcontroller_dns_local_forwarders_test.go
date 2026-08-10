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

// localForwardersEnv is the wiring one of these tests runs against.
type localForwardersEnv struct {
	client  *systemcontroller.SystemdClient
	systemd *systemd.MockManager
	rolodex *rolodex.Manager
	dataDir string
}

// initLocalForwardersTest wires a real rolodex.Manager (writing into a temp data
// dir) behind the real HTTP settings API, with a mock systemd so we can see the
// restart it asks for.
//
// Discovery is pointed at a fixture rather than the machine's own resolv.conf:
// the addresses a test asserts on must be a property of the test, not of
// whichever resolver the host running `make test-full` happens to have — and on
// a host whose /etc/resolv.conf holds only a loopback stub, real discovery
// would find nothing and the assertion would be about the wrong thing.
func initLocalForwardersTest(t *testing.T) localForwardersEnv {
	t.Helper()

	sd := systemd.InitMockManager()
	settings := &mockSettingsManager{values: map[string]string{
		"dns_tld":              "home",
		"dns_resolution_mode":  rolodex.ResolutionModeAuto,
		"dns_local_forwarders": "false",
	}}

	dataDir := rolodexTempDir(t, "local-forwarders-*")
	resolvPath := filepath.Join(dataDir, "resolv.conf")
	if err := os.WriteFile(resolvPath, []byte("nameserver 192.168.77.1\n"), 0600); err != nil {
		t.Fatalf("write resolv.conf fixture: %v", err)
	}

	rolMgr := rolodex.NewManager(rolodex.Config{
		Systemd:         sd,
		DataDir:         dataDir,
		Image:           rolodexTestImage(),
		UnixSocketPath:  filepath.Join(dataDir, "rolodex.sock"),
		ResolvConfPaths: []string{resolvPath},
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

	return localForwardersEnv{client: c, systemd: sd, rolodex: rolMgr, dataDir: dataDir}
}

// TestIntegrationSetDNSLocalForwardersRewritesConfigAndRestartsRolodex drives
// the real settings endpoint and asserts the operator-visible contract:
// switching to the local resolvers must (a) replace the forwarder list in
// rolodex.yml on disk and (b) restart rolodex, so a household stuck behind a
// network that blocks external DNS is resolving again NOW rather than after a
// reboot they have no reason to think would help.
//
// The on-disk assertion is the load-bearing one. rolodex.yml written at the
// previous boot is always newer than the systemcontroller binary, which is
// exactly the condition WriteConfig treats as "user-modified, do not touch". A
// handler that called WriteConfig would return 200 and change nothing.
func TestIntegrationSetDNSLocalForwardersRewritesConfigAndRestartsRolodex(t *testing.T) {
	t.Parallel()

	env := initLocalForwardersTest(t)
	c, sd, rolMgr, dataDir := env.client, env.systemd, env.rolodex, env.dataDir
	ctx := context.Background()

	assertRolodexForwarder(t, dataDir, rolodex.DefaultForwarders[0])

	if err := c.SetSetting(ctx, "dns_local_forwarders", "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	assertRolodexForwarder(t, dataDir, "192.168.77.1:53")
	assertRolodexLacksForwarder(t, dataDir, rolodex.DefaultForwarders[0])

	if !rolodexRestartRequested(sd, rolMgr.UnitName()) {
		t.Fatalf("rolodex was not restarted; systemd calls: %+v", sd.Calls)
	}

	// And back again — an operator who has left that network must be able to
	// stop handing it every name the household looks up.
	if err := c.SetSetting(ctx, "dns_local_forwarders", "false"); err != nil {
		t.Fatalf("SetSetting back to false: %v", err)
	}
	assertRolodexForwarder(t, dataDir, rolodex.DefaultForwarders[0])
	assertRolodexLacksForwarder(t, dataDir, "192.168.77.1:53")
}

// The mode decides whether the local tier is consulted; the forwarder list only
// decides what is in it. Turning one on must not move the other, or an operator
// fixing DNS on a filtered network would silently give up root recursion
// everywhere else.
func TestIntegrationSetDNSLocalForwardersLeavesTheResolutionModeAlone(t *testing.T) {
	t.Parallel()

	env := initLocalForwardersTest(t)
	c, dataDir := env.client, env.dataDir
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_local_forwarders", "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	assertRolodexMode(t, dataDir, rolodex.ResolutionModeAuto)
}

// A value that will not parse is read as off everywhere it is consumed, so
// storing one would look accepted and change nothing. Reject it at the API.
func TestIntegrationSetDNSLocalForwardersRejectsGarbage(t *testing.T) {
	t.Parallel()

	env := initLocalForwardersTest(t)
	c, dataDir := env.client, env.dataDir
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_local_forwarders", "sometimes"); err == nil {
		t.Fatal("expected an unparseable local-forwarders value to be rejected")
	}

	// The config on disk must be untouched — no half-applied change.
	assertRolodexForwarder(t, dataDir, rolodex.DefaultForwarders[0])
}

// GET /dns/status is what the settings screen renders, and it must report what
// rolodex.yml actually holds rather than echoing the setting back: the two
// disagree whenever discovery found nothing usable, which is the one case where
// the switch reads as on and nothing changed.
func TestIntegrationDNSStatusReportsTheEffectiveForwarders(t *testing.T) {
	t.Parallel()

	c := initLocalForwardersTest(t).client
	ctx := context.Background()

	status, err := c.DNSStatus(ctx)
	if err != nil {
		t.Fatalf("DNSStatus: %v", err)
	}
	if status.LocalForwarders {
		t.Fatal("DNSStatus reports local forwarders on before anything enabled them")
	}
	if strings.Join(status.Forwarders, ",") != strings.Join(rolodex.DefaultForwarders, ",") {
		t.Fatalf("DNSStatus forwarders = %v, want %v", status.Forwarders, rolodex.DefaultForwarders)
	}

	if err := c.SetSetting(ctx, "dns_local_forwarders", "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	status, err = c.DNSStatus(ctx)
	if err != nil {
		t.Fatalf("DNSStatus after enable: %v", err)
	}
	if !status.LocalForwarders {
		t.Fatal("DNSStatus still reports local forwarders off after enabling them")
	}
	if strings.Join(status.Forwarders, ",") != "192.168.77.1:53" {
		t.Fatalf("DNSStatus forwarders = %v, want [192.168.77.1:53]", status.Forwarders)
	}
}

func assertRolodexForwarder(t *testing.T, dataDir, want string) {
	t.Helper()

	config := readRolodexYAML(t, dataDir)
	if !strings.Contains(config, `- "`+want+`"`) {
		t.Fatalf("rolodex.yml does not forward to %q:\n%s", want, config)
	}
}

func assertRolodexLacksForwarder(t *testing.T, dataDir, unwanted string) {
	t.Helper()

	config := readRolodexYAML(t, dataDir)
	if strings.Contains(config, `- "`+unwanted+`"`) {
		t.Fatalf("rolodex.yml still forwards to %q:\n%s", unwanted, config)
	}
}

func readRolodexYAML(t *testing.T, dataDir string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dataDir, "rolodex.yml"))
	if err != nil {
		t.Fatalf("read rolodex.yml: %v", err)
	}
	return string(data)
}

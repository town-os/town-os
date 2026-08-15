// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// installWrittenRolodexConfig is a rolodex.yml as ../install's
// scripts/rolodex-config.sh lays it down: the host's real bind list, the DoH
// listener on loopback, and the encrypted transports. Every value here is
// something Town OS cannot derive — the routable address comes from enumerating
// the host's interfaces, which only the installer does.
const installWrittenRolodexConfig = `database_path: /data/rolodex.db
dns:
  bind:
    - udp: "127.0.0.2:53"
    - tcp: "127.0.0.2:53"
    - udp: "[::1]:53"
    - tcp: "[::1]:53"
    - udp: "192.168.122.50:53"
    - tcp: "192.168.122.50:53"
grpc:
  tcp_bind: ""
  unix_socket: /data/rolodex.sock
  shared_secret: ""
forwarders:
  - "192.168.122.1:53"
resolution:
  mode: auto
doh:
  bind: "127.0.0.2:4443"
  tls:
    auto_self_signed: true
metrics:
  bind: "127.0.0.2:9153"
`

// TestSettingsChangesNeverTouchTheInstallImagesConfig drives the REAL settings
// endpoints — the ones an operator's clicks land on — and asserts rolodex.yml
// comes out byte-identical.
//
// The unit test in src/rolodex proves the Manager writes no file. This proves
// the same thing one layer out, where the deployed failure actually happened: an
// operator toggled a DNSBL in the UI, the handler rewrote the whole config from
// a manager that can only render one hardcoded 127.0.0.2 bind, and the box's
// six binds became one. Everything resolving through 192.168.122.50 stopped, and
// nothing in any log said the write had happened.
//
// Driving the HTTP handlers rather than the Manager is the point. A future
// handler that reaches for a config writer directly — bypassing the Manager
// entirely — passes the unit test and fails here.
func TestSettingsChangesNeverTouchTheInstallImagesConfig(t *testing.T) {
	t.Parallel()

	sd := systemd.InitMockManager()
	settings := &mockSettingsManager{values: map[string]string{
		"dns_resolution_mode": rolodex.ResolutionModeAuto,
	}}

	dataDir := rolodexTempDir(t, "config-untouched-*")
	configPath := filepath.Join(dataDir, "rolodex.yml")
	if err := os.WriteFile(configPath, []byte(installWrittenRolodexConfig), 0o644); err != nil {
		t.Fatalf("plant the install image's config: %v", err)
	}
	before, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat planted config: %v", err)
	}

	rolMgr := rolodex.NewManager(rolodex.Config{
		Systemd:        sd,
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: filepath.Join(dataDir, "rolodex.sock"),
	})
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
	ctx := context.Background()

	// Every operator-facing mutation that used to rewrite this file. The
	// blocklist is first because it is the one that actually shipped the bug.
	if err := c.SetDnsblConfig(ctx, true, []systemcontroller.BlocklistProviderDTO{
		{Zone: "dbl.spamhaus.org", Enabled: true},
	}, 0); err != nil {
		t.Fatalf("SetDnsblConfig: %v", err)
	}
	if err := c.SetSetting(ctx, "dns_resolution_mode", rolodex.ResolutionModeForward); err != nil {
		t.Fatalf("SetSetting(dns_resolution_mode): %v", err)
	}
	if err := c.SetSetting(ctx, "dns_local_forwarders", "true"); err != nil {
		t.Fatalf("SetSetting(dns_local_forwarders): %v", err)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after the settings changes: %v", err)
	}
	if string(after) != installWrittenRolodexConfig {
		t.Errorf("the install image's rolodex.yml was rewritten by a settings change.\n--- want ---\n%s\n--- got ---\n%s",
			installWrittenRolodexConfig, after)
	}

	// mtime as well as content: a writer that happened to render identical bytes
	// still restarts rolodex on some paths, and a DNS outage for an unchanged
	// file is the worse half of the same bug.
	afterStat, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config after the settings changes: %v", err)
	}
	if !afterStat.ModTime().Equal(before.ModTime()) {
		t.Errorf("rolodex.yml was rewritten (mtime %s -> %s) even though its bytes are unchanged",
			before.ModTime(), afterStat.ModTime())
	}

	// The settings did have to go somewhere: if nothing was programmed, this
	// test would pass with the feature entirely broken.
	if got := rolClient.ResolutionMode; got != rolodex.ResolutionModeForward {
		t.Errorf("resolution mode never reached the running rolodex: %q", got)
	}
}

// TestNoTownOSPathCreatesARolodexConfig is the fresh-box counterpart: with no
// file to preserve, none may be created either.
//
// Separate because the two fail differently. A writer that merges into an
// existing file leaves the planted config alone above and still lays one down
// here — and a fresh box is exactly where "just render the defaults" looks
// harmless, right until it writes a single 127.0.0.2 bind onto a box the
// installer was about to configure properly.
func TestNoTownOSPathCreatesARolodexConfig(t *testing.T) {
	t.Parallel()

	sd := systemd.InitMockManager()
	settings := &mockSettingsManager{values: map[string]string{
		"dns_resolution_mode": rolodex.ResolutionModeAuto,
	}}
	dataDir := rolodexTempDir(t, "config-absent-*")

	rolMgr := rolodex.NewManager(rolodex.Config{
		Systemd:        sd,
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: filepath.Join(dataDir, "rolodex.sock"),
	})
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
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_resolution_mode", rolodex.ResolutionModeRecursive); err != nil {
		t.Fatalf("SetSetting(dns_resolution_mode): %v", err)
	}
	if err := c.SetDnsblConfig(ctx, false, nil, 0); err != nil {
		t.Fatalf("SetDnsblConfig: %v", err)
	}

	created := filepath.Join(dataDir, "rolodex.yml")
	switch _, err := os.Stat(created); {
	case err == nil:
		raw, readErr := os.ReadFile(created)
		if readErr != nil {
			t.Errorf("Town OS created a rolodex.yml (unreadable: %v); that file belongs to the install image", readErr)
			break
		}
		t.Errorf("Town OS created a rolodex.yml; that file belongs to the install image:\n%s", raw)
	case !os.IsNotExist(err):
		t.Fatalf("stat rolodex.yml: %v", err)
	}
}

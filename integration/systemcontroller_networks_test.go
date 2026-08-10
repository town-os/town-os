// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

func initNetworkDB(t *testing.T) *account.SQLiteNetworkManager {
	t.Helper()
	db, err := account.OpenDB(t.Context(), filepath.Join(t.TempDir(), "networks.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("db.Close: %v", cerr)
		}
	})
	nm, err := account.InitNetworkManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitNetworkManager: %v", err)
	}
	return nm
}

// TestReconcileNetworksSeedsHomeAndAppliesTransport drives the boot-time
// ReconcileNetworks entry point end-to-end across the sqlite network manager,
// the wireguard config renderer, and the systemd unit generator. It confirms
// the home network is present with its TLD reconciled against dns_tld, that a
// config file and unit are written for a non-default network, and that home
// gets neither.
//
// Home is NOT created here: account.InitNetworkManager seeds it alongside the
// tables, so it exists from the moment there is a database. All
// ensureDefaultNetwork does at boot is reconcile its TLD against the dns_tld
// setting (the account package has no settings manager, so the seeded row
// carries the bare default).
func TestReconcileNetworksSeedsHomeAndAppliesTransport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	nm := initNetworkDB(t)

	// Pre-seed a disabled "office" network so we can confirm disabled
	// networks are still materialized (config + unit) but not "remote-on".
	if _, err := nm.Create(t.Context(), &account.Network{
		Name:       "office",
		TLD:        "office",
		Subnet:     "10.90.5.0/24",
		Address:    "10.90.5.1/24",
		PublicKey:  "OFFICEPUB",
		PrivateKey: "OFFICEPRIV",
		ListenPort: 51821,
		Enabled:    false,
	}); err != nil {
		t.Fatalf("seed office: %v", err)
	}

	sd := systemd.InitMockManager()
	stateDir := t.TempDir()
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}

	systemcontroller.ReconcileNetworks(ctx, systemcontroller.ReconcileNetworksConfig{
		NetworkMgr:       nm,
		Systemd:          sd,
		NetworkStatePath: stateDir,
		SettingsMgr:      settings,
	})

	// The "home" network is present and its TLD matches the dns_tld setting.
	home, err := nm.Get(t.Context(), "home")
	if err != nil {
		t.Fatalf("home network not present: %v", err)
	}
	if home.TLD != "home" {
		t.Errorf("home TLD = %q, want %q", home.TLD, "home")
	}
	if !home.Enabled {
		t.Error("home network should be enabled by default")
	}
	// Home is DNS-only, so it carries NO transport fields at all: no overlay
	// subnet, no address, no keypair, no listen port. That is the truth rather
	// than a placeholder — a derived subnet and keys would be fields nothing
	// ever reads, and asserting they are populated would contradict the
	// no-config-file/no-unit assertions below.
	if home.Subnet != "" || home.Address != "" || home.PrivateKey != "" || home.PublicKey != "" || home.ListenPort != 0 {
		t.Errorf("home network must carry no transport fields, got %+v", home)
	}

	// A wg-quick config file and systemd unit are written for NON-DEFAULT networks
	// only. The default/home network is LAN-only and gets no WireGuard transport.
	iface := systemcontroller.NetworkInterfaceName("office")
	p := filepath.Join(stateDir, iface+".conf")
	data, rerr := os.ReadFile(p)
	if rerr != nil {
		t.Fatalf("expected config file for office at %s: %v", p, rerr)
	}
	if !strings.Contains(string(data), "[Interface]") || !strings.Contains(string(data), "PrivateKey = ") {
		t.Errorf("config for office malformed:\n%s", data)
	}
	officeUnit := systemd.NetworkUnitName("office")
	if _, ok := sd.InstalledUnits[officeUnit]; !ok {
		t.Errorf("expected unit %s to be installed", officeUnit)
	}
	if !strings.Contains(sd.InstalledUnits[officeUnit], "wg-quick up "+p) {
		t.Errorf("unit %s does not reference config path %s:\n%s", officeUnit, p, sd.InstalledUnits[officeUnit])
	}

	// The default/home network has NO WireGuard interface: no wg-quick config file
	// and no systemd unit. .home resolves only on the LAN, never over an overlay.
	if _, serr := os.Stat(filepath.Join(stateDir, systemcontroller.NetworkInterfaceName("home")+".conf")); !os.IsNotExist(serr) {
		t.Errorf("home network must not have a wg-quick config file, stat err = %v", serr)
	}
	if _, ok := sd.InstalledUnits[systemd.NetworkUnitName("home")]; ok {
		t.Error("home network must not install a WireGuard unit")
	}
}

// TestNetworkTransportTogglesUnitStatus confirms that toggling a network's
// enabled flag and re-reconciling flips the systemd unit action between
// start-equivalent (restart) and stop, which is how "remote access off" cuts
// the overlay while leaving the record and containers intact.
func TestNetworkTransportTogglesUnitStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	nm := initNetworkDB(t)
	if _, err := nm.Create(t.Context(), &account.Network{
		Name: "lab", TLD: "lab", Subnet: "10.90.7.0/24", Address: "10.90.7.1/24",
		PublicKey: "LABPUB", PrivateKey: "LABPRIV", ListenPort: 51822, Enabled: true,
	}); err != nil {
		t.Fatalf("seed lab: %v", err)
	}

	sd := systemd.InitMockManager()
	stateDir := t.TempDir()
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}
	cfg := systemcontroller.ReconcileNetworksConfig{
		NetworkMgr: nm, Systemd: sd, NetworkStatePath: stateDir, SettingsMgr: settings,
	}

	systemcontroller.ReconcileNetworks(ctx, cfg)
	unit := systemd.NetworkUnitName("lab")
	if last := lastStatusAction(sd, unit); last != string(systemd.Restart) {
		t.Fatalf("enabled network should be started (restart), got %q", last)
	}

	// Disable and re-reconcile → the unit is stopped.
	if err := nm.SetEnabled(t.Context(), "lab", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	sd.ClearCalls()
	systemcontroller.ReconcileNetworks(ctx, cfg)
	if last := lastStatusAction(sd, unit); last != string(systemd.Stop) {
		t.Fatalf("disabled network should be stopped, got %q", last)
	}
}

// TestNetworkReconcileRealSystemd runs ReconcileNetworks against a real
// systemd.NewManager() inside the test container and asserts that the generated
// network units are actually written to /etc/systemd/system, parsed, and loaded
// by systemd, and then removed on teardown — the real-systemd path the mock
// manager cannot cover.
//
// "lab" is seeded *disabled* on purpose: reconcile then issues a systemctl stop
// (a no-op on a never-started oneshot, so no wg-quick runs and no WireGuard
// interface is ever created) rather than bringing an interface up, which would
// require the wireguard kernel module + NET_ADMIN that the test container does
// not guarantee and which UninstallUnit would not tear back down. The
// enabled → start / disabled → stop action selection is covered separately by
// TestNetworkTransportTogglesUnitStatus against the mock manager.
//
// Home needs no such precaution and is deliberately not seeded here:
// account.InitNetworkManager already created it (a second Create would fail
// with ErrDuplicateNetwork), and applyNetworkTransport tears its transport down
// unconditionally regardless of the Enabled flag, so it can never auto-start an
// interface.
func TestNetworkReconcileRealSystemd(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	nm := initNetworkDB(t)

	if _, err := nm.Create(t.Context(), &account.Network{
		Name: "lab", TLD: "lab",
		Subnet: "10.91.3.0/24", Address: "10.91.3.1/24",
		PublicKey: "LABPUB", PrivateKey: "LABPRIV", ListenPort: 51931, Enabled: false,
	}); err != nil {
		t.Fatalf("seed lab: %v", err)
	}

	sd := systemd.NewManager()
	stateDir := t.TempDir()

	// Guarantee the units are removed even if an assertion fails mid-test, so no
	// unit file leaks into the shared /etc/systemd/system across parallel runs.
	for _, name := range []string{account.DefaultNetworkName, "lab"} {
		unit := systemd.NetworkUnitName(name)
		t.Cleanup(func() {
			cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			logCleanupf(t, systemd.NewManager().UninstallUnit(cctx, unit), "UninstallUnit %s", unit)
		})
	}

	systemcontroller.ReconcileNetworks(ctx, systemcontroller.ReconcileNetworksConfig{
		NetworkMgr:       nm,
		Systemd:          sd,
		NetworkStatePath: stateDir,
		SettingsMgr:      &mockSettingsManager{values: map[string]string{"dns_tld": "home"}},
	})

	// Only the non-default "lab" network gets WireGuard transport under real
	// systemd; the default/home network is LAN-only and gets no unit or config.
	{
		unit := systemd.NetworkUnitName("lab")
		unitPath := "/etc/systemd/system/" + unit

		// The unit file exists on disk and references the rendered wg config.
		content, err := os.ReadFile(unitPath)
		if err != nil {
			t.Fatalf("expected unit file %s on disk: %v", unitPath, err)
		}
		cfgPath := filepath.Join(stateDir, systemcontroller.NetworkInterfaceName("lab")+".conf")
		if !strings.Contains(string(content), "wg-quick up "+cfgPath) {
			t.Errorf("unit %s does not reference config %s:\n%s", unit, cfgPath, content)
		}

		// systemd actually loaded the unit (real dbus query, not a mock).
		states, err := sd.GetUnitStates(ctx, []string{unit})
		if err != nil {
			t.Fatalf("GetUnitStates %s: %v", unit, err)
		}
		if len(states) != 1 || states[0].LoadState != "loaded" {
			t.Fatalf("expected unit %s loaded, got %+v", unit, states)
		}
		// Disabled networks must not be active.
		if states[0].ActiveState == "active" {
			t.Errorf("disabled network lab should not be active, got %q", states[0].ActiveState)
		}

		// The wg-quick config file was rendered to the state dir.
		if _, err := os.Stat(cfgPath); err != nil {
			t.Errorf("expected config file %s: %v", cfgPath, err)
		}
	}

	// The default/home network has no WireGuard transport under real systemd: no
	// unit file on disk and no wg-quick config in the state dir.
	homeUnitPath := "/etc/systemd/system/" + systemd.NetworkUnitName(account.DefaultNetworkName)
	if _, err := os.Stat(homeUnitPath); !os.IsNotExist(err) {
		t.Errorf("default network must not install a unit file %s, stat err = %v", homeUnitPath, err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, systemcontroller.NetworkInterfaceName(account.DefaultNetworkName)+".conf")); !os.IsNotExist(err) {
		t.Errorf("default network must not have a wg-quick config file, stat err = %v", err)
	}
}

// lastStatusAction returns the most recent SetStatus action recorded for unit.
func lastStatusAction(sd *systemd.MockManager, unit string) string {
	action := ""
	for _, call := range sd.GetCalls() {
		if call.Method != "SetStatus" || len(call.Args) != 2 {
			continue
		}
		name, ok := call.Args[0].(string)
		if !ok || name != unit {
			continue
		}
		if act, ok := call.Args[1].(systemd.StatusAction); ok {
			action = string(act)
		}
	}
	return action
}

// initNetworkHTTPTest builds a test server backed by a real sqlite network
// manager, a mock systemd manager, and a real network-state directory, and
// returns a client plus the mock systemd manager and state dir so tests can
// assert on the units and config files the handlers produce.
func initNetworkHTTPTest(t *testing.T) (*systemcontroller.SystemdClient, *systemd.MockManager, string) {
	t.Helper()

	nm := initNetworkDB(t)
	sd := systemd.InitMockManager()
	stateDir := t.TempDir()

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		NetworkMgr:       nm,
		Systemd:          sd,
		NetworkStatePath: stateDir,
		SettingsMgr:      &mockSettingsManager{values: map[string]string{"dns_tld": "home"}},
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}
	return c, sd, stateDir
}

// TestNetworkHTTPLifecycle drives the /networks* endpoints end-to-end through
// the router and client: create → the wg config file and systemd unit appear
// and the interface is started; add peer → a device config comes back and the
// interface config gains the peer key; disable/enable → the unit stops/starts
// (this is exactly how "remote access off" cuts the overlay); remove peer and
// remove network → everything is torn down; and the default network is
// protected from removal.
// findNetworkView picks one network out of a list view by name. Lists carry the
// always-present home network alongside whatever the test made, so indexing
// into position 0 would silently assert about the wrong network.
func findNetworkView(nets []systemcontroller.NetworkView, name string) *systemcontroller.NetworkView {
	for i := range nets {
		if nets[i].Name == name {
			return &nets[i]
		}
	}
	return nil
}

func TestNetworkHTTPLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, sd, stateDir := initNetworkHTTPTest(t)

	// The home network always exists: account.InitNetworkManager seeds it with
	// the table, so a fresh server has it before anything is created.
	nets, err := c.ListNetworks(ctx)
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if len(nets) != 1 || nets[0].Name != account.DefaultNetworkName {
		t.Fatalf("expected only the home network initially, got %+v", nets)
	}

	// Create an enabled network.
	view, err := c.CreateNetwork(ctx, "office", "office")
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	if view.Name != "office" || !view.Enabled {
		t.Fatalf("unexpected created view: %+v", view)
	}
	// The private key must never cross the wire (account.Network tags it json:"-").
	if view.PrivateKey != "" {
		t.Errorf("private key leaked in API response: %q", view.PrivateKey)
	}
	if view.Subnet == "" || view.Address == "" || view.PublicKey == "" {
		t.Errorf("network not fully derived: %+v", view)
	}

	// A wg-quick config file was written and the unit installed + started.
	iface := systemcontroller.NetworkInterfaceName("office")
	cfgPath := filepath.Join(stateDir, iface+".conf")
	if _, serr := os.Stat(cfgPath); serr != nil {
		t.Fatalf("expected config file %s: %v", cfgPath, serr)
	}
	unitName := systemd.NetworkUnitName("office")
	if _, ok := sd.InstalledUnits[unitName]; !ok {
		t.Errorf("expected unit %s to be installed", unitName)
	}
	if act := lastStatusAction(sd, unitName); act != string(systemd.Restart) {
		t.Errorf("enabled network should be started (restart), got %q", act)
	}

	// List reflects the new network.
	nets, err = c.ListNetworks(ctx)
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if len(nets) != 2 || nets[0].Name != account.DefaultNetworkName || nets[1].Name != "office" {
		t.Fatalf("expected [home office], got %+v", nets)
	}

	// Add a peer with no supplied key → the server generates the keypair and
	// returns a ready-to-import device config plus the private key.
	res, err := c.AddNetworkPeer(ctx, systemcontroller.AddNetworkPeerRequest{Network: "office", Name: "laptop"})
	if err != nil {
		t.Fatalf("AddNetworkPeer: %v", err)
	}
	if res.PrivateKey == "" {
		t.Error("expected a server-generated private key for the device")
	}
	if res.Peer.PublicKey == "" || res.Peer.AllowedIP == "" {
		t.Errorf("peer not fully populated: %+v", res.Peer)
	}
	if !strings.Contains(res.Config, "[Interface]") || !strings.Contains(res.Config, "[Peer]") {
		t.Errorf("device config malformed:\n%s", res.Config)
	}

	// The re-rendered interface config now carries the peer's public key.
	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read interface config: %v", err)
	}
	if !strings.Contains(string(cfg), res.Peer.PublicKey) {
		t.Errorf("interface config missing peer public key:\n%s", cfg)
	}

	// ListNetworkPeers reflects the peer, and the list view's peer_count is 1.
	peers, err := c.ListNetworkPeers(ctx, "office")
	if err != nil {
		t.Fatalf("ListNetworkPeers: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
	nets, err = c.ListNetworks(ctx)
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if office := findNetworkView(nets, "office"); office == nil || office.PeerCount != 1 {
		t.Errorf("expected peer_count 1 on office, got %+v", nets)
	}

	// Disable → the unit is stopped (remote path down; record + container stay).
	sd.ClearCalls()
	if derr := c.DisableNetwork(ctx, "office"); derr != nil {
		t.Fatalf("DisableNetwork: %v", derr)
	}
	if act := lastStatusAction(sd, unitName); act != string(systemd.Stop) {
		t.Errorf("disabled network should be stopped, got %q", act)
	}

	// Enable again → the unit is started.
	sd.ClearCalls()
	if eerr := c.EnableNetwork(ctx, "office"); eerr != nil {
		t.Fatalf("EnableNetwork: %v", eerr)
	}
	if act := lastStatusAction(sd, unitName); act != string(systemd.Restart) {
		t.Errorf("re-enabled network should be started, got %q", act)
	}

	// Remove the peer → the network has no peers left.
	if rerr := c.RemoveNetworkPeer(ctx, "office", res.Peer.PublicKey); rerr != nil {
		t.Fatalf("RemoveNetworkPeer: %v", rerr)
	}
	if p, lerr := c.ListNetworkPeers(ctx, "office"); lerr != nil || len(p) != 0 {
		t.Errorf("expected 0 peers after removal, got (%d, %v)", len(p), lerr)
	}

	// The default network is protected: removal is rejected.
	if derr := c.RemoveNetwork(ctx, "home"); derr == nil {
		t.Error("expected an error removing the default network")
	}

	// Remove the office network → it disappears and its transport is torn down.
	if rerr := c.RemoveNetwork(ctx, "office"); rerr != nil {
		t.Fatalf("RemoveNetwork office: %v", rerr)
	}
	// Only the home network is left -- it is the one that cannot be removed.
	if n, lerr := c.ListNetworks(ctx); lerr != nil || len(n) != 1 || n[0].Name != account.DefaultNetworkName {
		t.Errorf("expected only the home network after removal, got (%+v, %v)", n, lerr)
	}
	if _, serr := os.Stat(cfgPath); !os.IsNotExist(serr) {
		t.Errorf("expected config file removed, stat err = %v", serr)
	}
	if _, ok := sd.InstalledUnits[unitName]; ok {
		t.Errorf("expected unit %s to be uninstalled", unitName)
	}
}

// TestInstallNetworkPersistenceSurvivesReinstall exercises the install-record
// network assignment: it is persisted on install and overwritten when the
// package is reinstalled onto a different network (the uninstall-without-purge
// → reinstall-elsewhere flow).
func TestInstallNetworkPersistenceSurvivesReinstall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m := packages.NewInstallManager(dir)

	// No assignment yet → default (empty).
	if got, err := m.LoadNetwork("default", "gitea"); err != nil || got != "" {
		t.Fatalf("LoadNetwork initial = (%q, %v), want (\"\", nil)", got, err)
	}

	if err := m.SaveNetwork("default", "gitea", "office"); err != nil {
		t.Fatalf("SaveNetwork office: %v", err)
	}
	if got, err := m.LoadNetwork("default", "gitea"); err != nil || got != "office" {
		t.Fatalf("LoadNetwork = (%q, %v), want office", got, err)
	}

	// Reinstall onto a different network.
	if err := m.SaveNetwork("default", "gitea", "lab"); err != nil {
		t.Fatalf("SaveNetwork lab: %v", err)
	}
	if got, err := m.LoadNetwork("default", "gitea"); err != nil || got != "lab" {
		t.Fatalf("LoadNetwork after reinstall = (%q, %v), want lab", got, err)
	}
}

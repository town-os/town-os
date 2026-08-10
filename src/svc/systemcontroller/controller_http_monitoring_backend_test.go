// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// initMonitoringBackendTestClient wires up a TestServer with the fields
// RefreshMonitoringBackend needs (systemd mock, btrfs base, network state
// path) so the HTTP setSetting handler can exercise the full restart flow
// when monitoring_backend changes.
func initMonitoringBackendTestClient(t *testing.T) (*SystemdClient, *systemd.MockManager) {
	t.Helper()
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}
	auditMgr, err := account.InitAuditManager(db)
	if err != nil {
		t.Fatalf("InitAuditManager: %v", err)
	}
	settingsMgr, err := account.InitSettingsManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitSettingsManager: %v", err)
	}

	btrfsBase := t.TempDir()
	st := storage.InitBtrFSFromController(btrfsBase, storage.InitBtrFSMockController())
	sd := systemd.InitMockManager()

	ts := InitTestServer(ServerConfig{
		Storage:                st,
		Systemd:                sd,
		AccountMgr:             mgr,
		SessionMgr:             sessMgr,
		AuditMgr:               auditMgr,
		SettingsMgr:            settingsMgr,
		BtrfsBasePath:          btrfsBase,
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       t.TempDir(),
		MonitoringBackend:      monitoring.BackendUPlot,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if _, err := c.CreateAccount(context.TODO(), "testadmin", "adminpass", "admin@test.com", "555-0000", "Test Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "testadmin", "adminpass")
	if err != nil {
		t.Fatalf("bootstrap Authenticate: %v", err)
	}
	c.Token = resp.Token

	return c, sd
}

// findRestart returns true if the mock systemd manager recorded a
// SetStatus(unit, Restart) call for the given unit name.
func findRestart(calls []systemd.MockCall, unit string) bool {
	for _, call := range calls {
		if call.Method != "SetStatus" || len(call.Args) < 2 {
			continue
		}
		name, ok := call.Args[0].(string)
		if !ok || name != unit {
			continue
		}
		if action, ok := call.Args[1].(systemd.StatusAction); ok && action == systemd.Restart {
			return true
		}
	}
	return false
}

func TestHTTPSettingsMonitoringBackendUPlotInstallsAndRestarts(t *testing.T) {
	c, sd := initMonitoringBackendTestClient(t)

	if err := c.SetSetting(context.TODO(), "monitoring_backend", monitoring.BackendUPlot); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	svcUnit := systemd.SystemServiceUnitName("monitoring-ui")
	content, ok := sd.InstalledUnits[svcUnit]
	if !ok {
		t.Fatalf("expected monitoring-ui service unit %q installed", svcUnit)
	}
	if !strings.Contains(content, "socat") {
		t.Fatalf("expected uPlot service unit to use socat, got:\n%s", content)
	}

	// The host-net monitoring UI is a plain system service (no NC): the
	// service unit must be restarted so the switch takes effect without a
	// reboot, and no -network unit should ever be installed.
	calls := sd.GetCalls()
	if !findRestart(calls, svcUnit) {
		t.Fatalf("expected Restart call on %q, got calls: %v", svcUnit, calls)
	}
	for name := range sd.InstalledUnits {
		if strings.Contains(name, "-network.service") {
			t.Fatalf("host-net monitoring-ui must not install an NC unit, got %q", name)
		}
	}

	// The persisted setting round-trips.
	val, err := c.GetSetting(context.TODO(), "monitoring_backend")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != monitoring.BackendUPlot {
		t.Fatalf("expected stored backend %q, got %q", monitoring.BackendUPlot, val)
	}
}

func TestHTTPSettingsMonitoringBackendSwitchStopsAndRestartsServices(t *testing.T) {
	c, sd := initMonitoringBackendTestClient(t)

	// First: set to uPlot. This installs the socat unit.
	if err := c.SetSetting(context.TODO(), "monitoring_backend", monitoring.BackendUPlot); err != nil {
		t.Fatalf("SetSetting uplot: %v", err)
	}

	svcUnit := systemd.SystemServiceUnitName("monitoring-ui")
	uplotContent, ok := sd.InstalledUnits[svcUnit]
	if !ok || !strings.Contains(uplotContent, "socat") {
		t.Fatalf("expected uPlot unit installed with socat, got:\n%s", uplotContent)
	}

	// Clear call history so we only see what the switch does.
	sd.ClearCalls()

	// Switch to Grafana.
	if err := c.SetSetting(context.TODO(), "monitoring_backend", monitoring.BackendGrafana); err != nil {
		t.Fatalf("SetSetting grafana: %v", err)
	}

	// The same unit name now holds the Grafana content (replacing the
	// prior socat unit). systemd's restart will then stop the running
	// socat container (via the old ExecStop, which is overwritten by
	// daemon-reload before restart) and bring up Grafana.
	grafanaContent, ok := sd.InstalledUnits[svcUnit]
	if !ok {
		t.Fatalf("expected monitoring-ui unit %q after switch", svcUnit)
	}
	if !strings.Contains(grafanaContent, monitoring.GrafanaImage) {
		t.Fatalf("expected Grafana image %q in unit after switch, got:\n%s", monitoring.GrafanaImage, grafanaContent)
	}
	if strings.Contains(grafanaContent, "socat TCP-LISTEN") {
		t.Fatalf("socat command should be gone after switch to Grafana, got:\n%s", grafanaContent)
	}

	// The service unit must be restarted after the switch (no NC unit exists
	// for the host-net monitoring UI).
	calls := sd.GetCalls()
	if !findRestart(calls, svcUnit) {
		t.Fatalf("expected Restart on %q after backend switch, got calls: %v", svcUnit, calls)
	}

	// Switch back to uPlot: Grafana unit should be replaced by socat.
	sd.ClearCalls()
	if err := c.SetSetting(context.TODO(), "monitoring_backend", monitoring.BackendUPlot); err != nil {
		t.Fatalf("SetSetting back to uplot: %v", err)
	}
	backContent := sd.InstalledUnits[svcUnit]
	if !strings.Contains(backContent, "socat") {
		t.Fatalf("expected socat unit after switching back, got:\n%s", backContent)
	}
	if strings.Contains(backContent, monitoring.GrafanaImage) {
		t.Fatalf("Grafana image should be gone after switch back, got:\n%s", backContent)
	}
	if !findRestart(sd.GetCalls(), svcUnit) {
		t.Fatalf("expected Restart on %q after second switch", svcUnit)
	}
}

func TestHTTPSettingsMonitoringBackendOtherKeyDoesNotTouchSystemd(t *testing.T) {
	c, sd := initMonitoringBackendTestClient(t)

	// Set an unrelated setting; the monitoring UI service must not be
	// installed or restarted.
	if err := c.SetSetting(context.TODO(), "default_quota", "100"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	svcUnit := systemd.SystemServiceUnitName("monitoring-ui")
	if _, ok := sd.InstalledUnits[svcUnit]; ok {
		t.Fatal("monitoring-ui should not be installed for unrelated settings")
	}
	if findRestart(sd.GetCalls(), svcUnit) {
		t.Fatal("monitoring-ui should not be restarted for unrelated settings")
	}
}

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"testing"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	"gitea.com/town-os/town-os/src/ui"
)

func initSystemServiceIntegrationTest(t *testing.T) (*systemcontroller.SystemdClient, *systemd.MockManager) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()

	// Mock systemd: the image is never pulled or run, so a neutral fake tag
	// is used. rc.latest must never be referenced in tests.
	uiMgr := ui.NewManager(ui.Config{
		Systemd: sd,
		Image:   "quay.io/town/ui:testtag",
	})

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:                    mock,
		Systemd:                    sd,
		MonitoringBackend:          monitoring.BackendUPlot,
		UI:                         uiMgr,
		SystemControllerImage:      "quay.io/town/town:testtag",
		SystemControllerListenAddr: ":5309",
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, sd
}

func TestSystemControllerListSystemServicesEmpty(t *testing.T) {
	t.Parallel()
	// Without monitoring configured.
	c := initSystemControllerTest(t)

	entries, err := c.ListSystemServices(context.TODO())
	if err != nil {
		t.Fatalf("ListSystemServices: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("expected 0 entries without monitoring, got %d", len(entries))
	}
}

func TestSystemControllerListSystemServicesPopulated(t *testing.T) {
	t.Parallel()
	c, _ := initSystemServiceIntegrationTest(t)

	entries, err := c.ListSystemServices(context.TODO())
	if err != nil {
		t.Fatalf("ListSystemServices: %v", err)
	}

	// node-exporter + prometheus + monitoring-ui (monitoring) + ui +
	// systemcontroller = 5 system services.
	if len(entries) != 5 {
		t.Fatalf("expected 5 system services, got %d", len(entries))
	}

	keys := map[string]bool{}
	for _, e := range entries {
		keys[e.Key] = true
	}
	for _, key := range []string{"node-exporter", "prometheus", "monitoring-ui", "ui", systemcontroller.SystemControllerServiceKey} {
		if !keys[key] {
			t.Fatalf("missing expected key %q", key)
		}
	}
}

func TestSystemControllerSetSystemServiceStatusAllActions(t *testing.T) {
	t.Parallel()
	c, sd := initSystemServiceIntegrationTest(t)

	for _, action := range []systemd.StatusAction{systemd.Start, systemd.Stop, systemd.Restart} {
		if err := c.SetSystemServiceStatus(context.TODO(), "node-exporter", action); err != nil {
			t.Fatalf("SetSystemServiceStatus(%s): %v", action, err)
		}
	}

	calls := sd.GetCalls()
	statusCalls := 0
	for _, call := range calls {
		if call.Method == "SetStatus" {
			statusCalls++
		}
	}

	if statusCalls != 3 {
		t.Fatalf("expected 3 SetStatus calls, got %d", statusCalls)
	}
}

func TestSystemControllerSetSystemServiceStatusRejectsEnableDisable(t *testing.T) {
	t.Parallel()
	c, _ := initSystemServiceIntegrationTest(t)

	err := c.SetSystemServiceStatus(context.TODO(), "node-exporter", systemd.Enable)
	if err == nil {
		t.Fatal("expected error for enable action")
	}

	err = c.SetSystemServiceStatus(context.TODO(), "node-exporter", systemd.Disable)
	if err == nil {
		t.Fatal("expected error for disable action")
	}
}

func TestSystemControllerSetSystemServiceStatusInvalidKey(t *testing.T) {
	t.Parallel()
	c, _ := initSystemServiceIntegrationTest(t)

	err := c.SetSystemServiceStatus(context.TODO(), "nonexistent-service", systemd.Start)
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestSystemControllerSystemServicesIsolatedFromPackageUnits(t *testing.T) {
	t.Parallel()
	c, sd := initSystemServiceIntegrationTest(t)

	// Add both package and system service units.
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
	}

	entries, err := c.ListSystemServices(context.TODO())
	if err != nil {
		t.Fatalf("ListSystemServices: %v", err)
	}

	// Should return 5 services (node-exporter + prometheus + monitoring-ui
	// + ui + systemcontroller), with status for node-exporter.
	if len(entries) != 5 {
		t.Fatalf("expected 5 system services, got %d", len(entries))
	}

	for _, e := range entries {
		if e.Key == "node-exporter" && e.ActiveState != "active" {
			t.Fatalf("expected node-exporter active, got %q", e.ActiveState)
		}
	}
}

// TestSystemControllerListSystemServicesIgnoresNCAndSocketUnits verifies that
// system-service-prefixed NC (`-network.service`) and socket units — which
// are generated alongside prometheus and monitoring-ui by the package unit
// path — are not surfaced as separate entries from ListSystemServices.
// The registered service list should always yield exactly 5 entries
// (node-exporter + prometheus + monitoring-ui + ui + systemcontroller)
// regardless of how many supporting NC/socket units exist on the system.
func TestSystemControllerListSystemServicesIgnoresNCAndSocketUnits(t *testing.T) {
	t.Parallel()
	c, sd := initSystemServiceIntegrationTest(t)

	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: "town-os-system--prometheus-network.service", ActiveState: "active"},
		{Name: "town-os-system--prometheus-9090-tcp.socket", ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("monitoring-ui"), ActiveState: "active"},
		{Name: "town-os-system--monitoring-ui-network.service", ActiveState: "active"},
		{Name: "town-os-system--monitoring-ui-5308-tcp.socket", ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("ui"), ActiveState: "active"},
		{Name: systemd.SystemControllerUnitName, ActiveState: "active"},
	}

	entries, err := c.ListSystemServices(context.TODO())
	if err != nil {
		t.Fatalf("ListSystemServices: %v", err)
	}

	if len(entries) != 5 {
		t.Fatalf("expected 5 entries (NC and socket units excluded), got %d", len(entries))
	}
	expected := map[string]bool{
		"node-exporter":                             true,
		"prometheus":                                true,
		"monitoring-ui":                             true,
		"ui":                                        true,
		systemcontroller.SystemControllerServiceKey: true,
	}
	for _, e := range entries {
		if !expected[e.Key] {
			t.Fatalf("unexpected entry key %q", e.Key)
		}
	}
}

// TestSystemControllerPingExcludesSystemServiceNCAndSocketUnits verifies that
// the ping endpoint's system_services count only includes main service units,
// not the NC (`-network.service`) and socket units generated alongside them
// by the package unit path. Prometheus and monitoring-ui each contribute one
// NC + one socket that must not be counted. The systemcontroller unit itself
// is counted via systemd.IsSystemServiceUnit.
func TestSystemControllerPingExcludesSystemServiceNCAndSocketUnits(t *testing.T) {
	t.Parallel()
	c, sd := initSystemServiceIntegrationTest(t)

	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: "town-os-system--prometheus-network.service", ActiveState: "active"},
		{Name: "town-os-system--prometheus-9090-tcp.socket", ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("monitoring-ui"), ActiveState: "failed"},
		{Name: "town-os-system--monitoring-ui-network.service", ActiveState: "active"},
		{Name: "town-os-system--monitoring-ui-5308-tcp.socket", ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("ui"), ActiveState: "active"},
		{Name: systemd.SystemControllerUnitName, ActiveState: "active"},
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if ping.SystemServices == nil {
		t.Fatal("expected system_services in ping response")
	}

	// 5 main services: node-exporter, prometheus, monitoring-ui, ui,
	// systemcontroller. NC and socket units should be excluded.
	if ping.SystemServices.Total != 5 {
		t.Fatalf("expected 5 total system services (NC and socket units excluded), got %d", ping.SystemServices.Total)
	}
	// 4 active (node-exporter, prometheus, ui, systemcontroller); monitoring-ui is failed.
	if ping.SystemServices.Active != 4 {
		t.Fatalf("expected 4 active system services, got %d", ping.SystemServices.Active)
	}
	if ping.SystemServices.Failed != 1 {
		t.Fatalf("expected 1 failed system service, got %d", ping.SystemServices.Failed)
	}
}

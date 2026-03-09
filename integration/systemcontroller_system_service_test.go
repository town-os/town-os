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
	monMgr := monitoring.NewManager(monitoring.Config{
		Systemd: sd,
		DataDir: t.TempDir(),
	})

	uiMgr := ui.NewManager(ui.Config{
		Systemd: sd,
		Image:   "quay.io/town/ui:rc.latest",
	})

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:    mock,
		Systemd:    sd,
		Monitoring: monMgr,
		UI:         uiMgr,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, sd
}

func TestSystemControllerListSystemServicesEmpty(t *testing.T) {
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
	c, _ := initSystemServiceIntegrationTest(t)

	entries, err := c.ListSystemServices(context.TODO())
	if err != nil {
		t.Fatalf("ListSystemServices: %v", err)
	}

	if len(entries) != 4 {
		t.Fatalf("expected 4 system services, got %d", len(entries))
	}

	keys := map[string]bool{}
	for _, e := range entries {
		keys[e.Key] = true
	}
	for _, key := range []string{"prometheus", "node-exporter", "grafana", "ui"} {
		if !keys[key] {
			t.Fatalf("missing expected key %q", key)
		}
	}
}

func TestSystemControllerSetSystemServiceStatusAllActions(t *testing.T) {
	c, sd := initSystemServiceIntegrationTest(t)

	for _, action := range []systemd.StatusAction{systemd.Start, systemd.Stop, systemd.Restart} {
		if err := c.SetSystemServiceStatus(context.TODO(), "prometheus", action); err != nil {
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
	c, _ := initSystemServiceIntegrationTest(t)

	err := c.SetSystemServiceStatus(context.TODO(), "prometheus", systemd.Enable)
	if err == nil {
		t.Fatal("expected error for enable action")
	}

	err = c.SetSystemServiceStatus(context.TODO(), "prometheus", systemd.Disable)
	if err == nil {
		t.Fatal("expected error for disable action")
	}
}

func TestSystemControllerSetSystemServiceStatusInvalidKey(t *testing.T) {
	c, _ := initSystemServiceIntegrationTest(t)

	err := c.SetSystemServiceStatus(context.TODO(), "nonexistent-service", systemd.Start)
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestSystemControllerSystemServicesIsolatedFromPackageUnits(t *testing.T) {
	c, sd := initSystemServiceIntegrationTest(t)

	// Add both package and system service units.
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("grafana"), ActiveState: "failed"},
	}

	entries, err := c.ListSystemServices(context.TODO())
	if err != nil {
		t.Fatalf("ListSystemServices: %v", err)
	}

	// Should return 4 services (all configured), with status for prometheus and grafana.
	if len(entries) != 4 {
		t.Fatalf("expected 4 system services, got %d", len(entries))
	}

	for _, e := range entries {
		switch e.Key {
		case "prometheus":
			if e.ActiveState != "active" {
				t.Fatalf("expected prometheus active, got %q", e.ActiveState)
			}
		case "grafana":
			if e.ActiveState != "failed" {
				t.Fatalf("expected grafana failed, got %q", e.ActiveState)
			}
		case "node-exporter":
			// No unit status available — should be empty.
			if e.ActiveState != "" {
				t.Fatalf("expected node-exporter empty state, got %q", e.ActiveState)
			}
		}
	}
}

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"testing"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

func initSystemServiceTestClient(t *testing.T) (*SystemdClient, *systemd.MockManager) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	monMgr := monitoring.NewManager(monitoring.Config{
		Systemd: sd,
		DataDir: t.TempDir(),
	})

	ts := InitTestServer(ServerConfig{Storage: mock, Systemd: sd, Monitoring: monMgr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, sd
}

func TestHTTPListSystemServicesWithoutMonitoring(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	entries, err := c.ListSystemServices(context.TODO())
	if err != nil {
		t.Fatalf("ListSystemServices: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("expected 0 entries without monitoring, got %d", len(entries))
	}
}

func TestHTTPListSystemServicesWithMonitoring(t *testing.T) {
	c, _ := initSystemServiceTestClient(t)

	entries, err := c.ListSystemServices(context.TODO())
	if err != nil {
		t.Fatalf("ListSystemServices: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	keys := map[string]bool{}
	for _, e := range entries {
		keys[e.Key] = true
		if e.Image == "" {
			t.Fatalf("expected non-empty image for key %q", e.Key)
		}
		if e.Port == "" {
			t.Fatalf("expected non-empty port for key %q", e.Key)
		}
		if e.DisplayName == "" {
			t.Fatalf("expected non-empty display name for key %q", e.Key)
		}
	}

	for _, key := range []string{"prometheus", "node-exporter", "grafana"} {
		if !keys[key] {
			t.Fatalf("expected key %q in entries", key)
		}
	}
}

func TestHTTPListSystemServicesReportsRunningState(t *testing.T) {
	c, sd := initSystemServiceTestClient(t)

	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active", SubState: "running"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active", SubState: "running"},
		{Name: systemd.SystemServiceUnitName("grafana"), ActiveState: "failed", SubState: "failed"},
	}

	entries, err := c.ListSystemServices(context.TODO())
	if err != nil {
		t.Fatalf("ListSystemServices: %v", err)
	}

	for _, e := range entries {
		switch e.Key {
		case "prometheus", "node-exporter":
			if e.ActiveState != "active" {
				t.Fatalf("expected %s active, got %q", e.Key, e.ActiveState)
			}
		case "grafana":
			if e.ActiveState != "failed" {
				t.Fatalf("expected grafana failed, got %q", e.ActiveState)
			}
		}
	}
}

func TestHTTPSetSystemServiceStatusStart(t *testing.T) {
	c, sd := initSystemServiceTestClient(t)

	if err := c.SetSystemServiceStatus(context.TODO(), "prometheus", systemd.Start); err != nil {
		t.Fatalf("SetSystemServiceStatus: %v", err)
	}

	calls := sd.GetCalls()
	found := false
	for _, call := range calls {
		if call.Method == "SetStatus" && len(call.Args) >= 2 {
			if call.Args[0] == systemd.SystemServiceUnitName("prometheus") && call.Args[1] == systemd.Start {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected SetStatus(prometheus, start) call")
	}
}

func TestHTTPSetSystemServiceStatusStop(t *testing.T) {
	c, _ := initSystemServiceTestClient(t)

	if err := c.SetSystemServiceStatus(context.TODO(), "grafana", systemd.Stop); err != nil {
		t.Fatalf("SetSystemServiceStatus: %v", err)
	}
}

func TestHTTPSetSystemServiceStatusRestart(t *testing.T) {
	c, _ := initSystemServiceTestClient(t)

	if err := c.SetSystemServiceStatus(context.TODO(), "node-exporter", systemd.Restart); err != nil {
		t.Fatalf("SetSystemServiceStatus: %v", err)
	}
}

func TestHTTPSetSystemServiceStatusEnableRejected(t *testing.T) {
	c, _ := initSystemServiceTestClient(t)

	err := c.SetSystemServiceStatus(context.TODO(), "prometheus", systemd.Enable)
	if err == nil {
		t.Fatal("expected error for enable action")
	}
}

func TestHTTPSetSystemServiceStatusInvalidKey(t *testing.T) {
	c, _ := initSystemServiceTestClient(t)

	err := c.SetSystemServiceStatus(context.TODO(), "nonexistent", systemd.Start)
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestHTTPPingIncludesSystemServiceCounts(t *testing.T) {
	c, sd := initSystemServiceTestClient(t)

	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("grafana"), ActiveState: "failed"},
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if ping.SystemServices == nil {
		t.Fatal("expected system_services in ping response")
	}

	if ping.SystemServices.Total != 3 {
		t.Fatalf("expected 3 total system services, got %d", ping.SystemServices.Total)
	}

	if ping.SystemServices.Active != 2 {
		t.Fatalf("expected 2 active system services, got %d", ping.SystemServices.Active)
	}

	if ping.SystemServices.Failed != 1 {
		t.Fatalf("expected 1 failed system service, got %d", ping.SystemServices.Failed)
	}
}

func TestMockClientListSystemServices(t *testing.T) {
	m := InitMockClient()
	m.SystemServices = []SystemServiceEntry{
		{Key: "prometheus", DisplayName: "Prometheus"},
	}

	entries, err := m.ListSystemServices(context.TODO())
	if err != nil {
		t.Fatalf("ListSystemServices: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].Key != "prometheus" {
		t.Fatalf("expected key prometheus, got %q", entries[0].Key)
	}
}

func TestMockClientListSystemServicesError(t *testing.T) {
	m := InitMockClient()
	m.ListSystemServicesErr = errors.New("injected")

	_, err := m.ListSystemServices(context.TODO())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMockClientSetSystemServiceStatus(t *testing.T) {
	m := InitMockClient()

	if err := m.SetSystemServiceStatus(context.TODO(), "prometheus", systemd.Start); err != nil {
		t.Fatalf("SetSystemServiceStatus: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "SetSystemServiceStatus" {
		t.Fatalf("expected method SetSystemServiceStatus, got %q", calls[0].Method)
	}
}

func TestMockClientSetSystemServiceStatusRejectsEnable(t *testing.T) {
	m := InitMockClient()

	err := m.SetSystemServiceStatus(context.TODO(), "prometheus", systemd.Enable)
	if err == nil {
		t.Fatal("expected error for enable action")
	}
}

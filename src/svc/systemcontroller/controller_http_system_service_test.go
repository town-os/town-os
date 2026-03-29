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

	ts := InitTestServer(ServerConfig{
		Storage:           mock,
		Systemd:           sd,
		MonitoringBackend: monitoring.BackendUPlot,
	})
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

	// Only node-exporter is a system service now; prometheus/grafana are packages.
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (node-exporter), got %d", len(entries))
	}

	if entries[0].Key != "node-exporter" {
		t.Fatalf("expected key node-exporter, got %q", entries[0].Key)
	}
	if entries[0].Image == "" {
		t.Fatal("expected non-empty image")
	}
	if entries[0].Port == "" {
		t.Fatal("expected non-empty port")
	}
	if entries[0].DisplayName == "" {
		t.Fatal("expected non-empty display name")
	}
}

func TestHTTPListSystemServicesReportsRunningState(t *testing.T) {
	c, sd := initSystemServiceTestClient(t)

	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active", SubState: "running"},
	}

	entries, err := c.ListSystemServices(context.TODO())
	if err != nil {
		t.Fatalf("ListSystemServices: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ActiveState != "active" {
		t.Fatalf("expected node-exporter active, got %q", entries[0].ActiveState)
	}
}

func TestHTTPSetSystemServiceStatusStart(t *testing.T) {
	c, sd := initSystemServiceTestClient(t)

	if err := c.SetSystemServiceStatus(context.TODO(), "node-exporter", systemd.Start); err != nil {
		t.Fatalf("SetSystemServiceStatus: %v", err)
	}

	calls := sd.GetCalls()
	found := false
	for _, call := range calls {
		if call.Method == "SetStatus" && len(call.Args) >= 2 {
			if call.Args[0] == systemd.SystemServiceUnitName("node-exporter") && call.Args[1] == systemd.Start {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected SetStatus(node-exporter, start) call")
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

	err := c.SetSystemServiceStatus(context.TODO(), "node-exporter", systemd.Enable)
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
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if ping.SystemServices == nil {
		t.Fatal("expected system_services in ping response")
	}

	if ping.SystemServices.Total != 1 {
		t.Fatalf("expected 1 total system services, got %d", ping.SystemServices.Total)
	}

	if ping.SystemServices.Active != 1 {
		t.Fatalf("expected 1 active system services, got %d", ping.SystemServices.Active)
	}
}

func TestMockClientListSystemServices(t *testing.T) {
	m := InitMockClient()
	m.SystemServices = []SystemServiceEntry{
		{Key: "node-exporter", DisplayName: "Node Exporter"},
	}

	entries, err := m.ListSystemServices(context.TODO())
	if err != nil {
		t.Fatalf("ListSystemServices: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].Key != "node-exporter" {
		t.Fatalf("expected key node-exporter, got %q", entries[0].Key)
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

	if err := m.SetSystemServiceStatus(context.TODO(), "node-exporter", systemd.Start); err != nil {
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

	err := m.SetSystemServiceStatus(context.TODO(), "node-exporter", systemd.Enable)
	if err == nil {
		t.Fatal("expected error for enable action")
	}
}

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

func TestHTTPMonitoringStatusWithoutMonitoring(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	resp, err := c.getClient(context.TODO(), "monitoring/status")
	if err != nil {
		t.Fatalf("getClient: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "disabled" {
		t.Fatalf("expected status=disabled, got %q", body["status"])
	}
}

func TestHTTPMonitoringStatusUPlotBackend(t *testing.T) {
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()

	// Simulate the monitoring package unit running.
	monUnitName := systemd.UnitName(
		monitoring.MonitoringRepo,
		monitoring.MonitoringPackageName,
		monitoring.MonitoringVersion,
	)
	sd.Units = []systemd.UnitStatus{
		{Name: monUnitName, ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
	}

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

	status, err := c.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	if status.Backend != monitoring.BackendUPlot {
		t.Fatalf("expected backend %q, got %q", monitoring.BackendUPlot, status.Backend)
	}
	if !status.Prometheus {
		t.Fatal("expected prometheus running")
	}
	if !status.NodeExporter {
		t.Fatal("expected node-exporter running")
	}
}

func TestHTTPMonitoringStatusGrafanaBackend(t *testing.T) {
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()

	monUnitName := systemd.UnitName(
		monitoring.MonitoringRepo,
		monitoring.MonitoringPackageName,
		monitoring.MonitoringVersion,
	)
	depUnitName := systemd.UnitName(
		monitoring.MonitoringRepo,
		monitoring.MonitoringPackageName+"--dep--prometheus",
		monitoring.MonitoringVersion,
	)
	sd.Units = []systemd.UnitStatus{
		{Name: monUnitName, ActiveState: "active"},
		{Name: depUnitName, ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
	}

	ts := InitTestServer(ServerConfig{
		Storage:           mock,
		Systemd:           sd,
		MonitoringBackend: monitoring.BackendGrafana,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	status, err := c.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	if status.Backend != monitoring.BackendGrafana {
		t.Fatalf("expected backend %q, got %q", monitoring.BackendGrafana, status.Backend)
	}
	if !status.Prometheus {
		t.Fatal("expected prometheus running")
	}
	if !status.Grafana {
		t.Fatal("expected grafana running")
	}
	if !status.NodeExporter {
		t.Fatal("expected node-exporter running")
	}
}

func TestMockClientMonitoringStatus(t *testing.T) {
	m := InitMockClient()

	status, err := m.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MockClient.MonitoringStatus: %v", err)
	}

	if status.Prometheus {
		t.Fatal("expected prometheus not running in default mock")
	}
}

func TestMockClientMonitoringStatusCustomResponse(t *testing.T) {
	m := InitMockClient()
	m.MonitoringStatusResp = &monitoring.MonitoringStatus{
		Backend:    monitoring.BackendUPlot,
		Prometheus: true,
	}

	status, err := m.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MockClient.MonitoringStatus: %v", err)
	}

	if !status.Prometheus {
		t.Fatal("expected prometheus running")
	}
	if status.Backend != monitoring.BackendUPlot {
		t.Fatalf("expected backend %q, got %q", monitoring.BackendUPlot, status.Backend)
	}
}

func TestMockClientMonitoringStatusErrorInjection(t *testing.T) {
	m := InitMockClient()
	m.MonitoringStatusErr = errors.New("injected error")

	_, err := m.MonitoringStatus(context.TODO())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMockClientMonitoringStatusCallLog(t *testing.T) {
	m := InitMockClient()

	if _, err := m.MonitoringStatus(context.TODO()); err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "MonitoringStatus" {
		t.Fatalf("expected method MonitoringStatus, got %q", calls[0].Method)
	}
}

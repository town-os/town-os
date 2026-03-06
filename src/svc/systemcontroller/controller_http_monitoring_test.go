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

func initMonitoringTestClient(t *testing.T) (*SystemdClient, *monitoring.Manager, *systemd.MockManager) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	monMgr := monitoring.NewManager(monitoring.Config{
		Systemd: sd,
		DataDir: t.TempDir(),
	})

	ts := InitTestServer(ServerConfig{Storage: mock, Monitoring: monMgr, Systemd: sd})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, monMgr, sd
}

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

func TestHTTPMonitoringStatusWithMonitoring(t *testing.T) {
	c, monMgr, sd := initMonitoringTestClient(t)

	ctx := context.Background()
	if err := monMgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer monMgr.Stop()

	// Pre-populate systemd mock with active units to simulate running state.
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("grafana"), ActiveState: "active"},
	}

	status, err := c.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	if !status.Prometheus.Running {
		t.Fatal("expected prometheus running")
	}
	if !status.NodeExporter.Running {
		t.Fatal("expected node-exporter running")
	}
	if !status.Grafana.Running {
		t.Fatal("expected grafana running")
	}
}

func TestHTTPMonitoringStatusReportsImages(t *testing.T) {
	c, _, _ := initMonitoringTestClient(t)

	status, err := c.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	if status.Prometheus.Image != monitoring.PrometheusImage {
		t.Fatalf("expected prometheus image %q, got %q", monitoring.PrometheusImage, status.Prometheus.Image)
	}
	if status.NodeExporter.Image != monitoring.NodeExporterImage {
		t.Fatalf("expected node-exporter image %q, got %q", monitoring.NodeExporterImage, status.NodeExporter.Image)
	}
	if status.Grafana.Image != monitoring.GrafanaImage {
		t.Fatalf("expected grafana image %q, got %q", monitoring.GrafanaImage, status.Grafana.Image)
	}
}

func TestHTTPMonitoringStatusReportsPorts(t *testing.T) {
	c, _, _ := initMonitoringTestClient(t)

	status, err := c.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	// Default ports.
	if status.Prometheus.Port != "9091" {
		t.Fatalf("expected prometheus port 9091, got %q", status.Prometheus.Port)
	}
	if status.NodeExporter.Port != "9101" {
		t.Fatalf("expected node-exporter port 9101, got %q", status.NodeExporter.Port)
	}
	if status.Grafana.Port != "3001" {
		t.Fatalf("expected grafana port 3001, got %q", status.Grafana.Port)
	}
}

func TestMockClientMonitoringStatus(t *testing.T) {
	m := InitMockClient()

	status, err := m.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MockClient.MonitoringStatus: %v", err)
	}

	// Default mock should return empty status.
	if status.Prometheus.Running {
		t.Fatal("expected prometheus not running in default mock")
	}
}

func TestMockClientMonitoringStatusCustomResponse(t *testing.T) {
	m := InitMockClient()
	m.MonitoringStatusResp = &monitoring.Status{
		Prometheus: monitoring.ContainerStatus{Running: true},
		Grafana:    monitoring.ContainerStatus{Running: true},
	}

	status, err := m.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MockClient.MonitoringStatus: %v", err)
	}

	if !status.Prometheus.Running {
		t.Fatal("expected prometheus running")
	}
	if !status.Grafana.Running {
		t.Fatal("expected grafana running")
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

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package monitoring

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/systemd"
)

func TestNewManager(t *testing.T) {
	sd := systemd.InitMockManager()
	m := NewManager(Config{
		Systemd: sd,
		DataDir: t.TempDir(),
	})
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestStartInstallsUnits(t *testing.T) {
	sd := systemd.InitMockManager()
	m := NewManager(Config{
		Systemd: sd,
		DataDir: t.TempDir(),
	})

	if err := m.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	calls := sd.GetCalls()

	// Should have InstallUnit + Enable + Start for each of the 3 services.
	installCalls := 0
	enableCalls := 0
	restartCalls := 0
	for _, c := range calls {
		switch c.Method {
		case "InstallUnit":
			installCalls++
		case "SetStatus":
			if len(c.Args) >= 2 {
				if c.Args[1] == systemd.Enable {
					enableCalls++
				}
				if c.Args[1] == systemd.Restart {
					restartCalls++
				}
			}
		}
	}

	if installCalls != 3 {
		t.Fatalf("expected 3 InstallUnit calls, got %d", installCalls)
	}
	if enableCalls != 3 {
		t.Fatalf("expected 3 Enable calls, got %d", enableCalls)
	}
	if restartCalls != 3 {
		t.Fatalf("expected 3 Restart calls, got %d", restartCalls)
	}

	// Verify units were installed with correct names.
	for _, key := range []string{"node-exporter", "prometheus", "grafana"} {
		unitName := systemd.SystemServiceUnitName(key)
		if !sd.InstalledUnits[unitName] {
			t.Fatalf("expected unit %s to be installed", unitName)
		}
	}
}

func TestStatusReportsRunningState(t *testing.T) {
	sd := systemd.InitMockManager()
	m := NewManager(Config{
		Systemd: sd,
		DataDir: t.TempDir(),
	})

	ctx := context.Background()

	// Before start, nothing is running (no units in mock).
	status := m.Status(ctx)
	if status.Prometheus.Running {
		t.Fatal("expected prometheus not running before start")
	}
	if status.NodeExporter.Running {
		t.Fatal("expected node-exporter not running before start")
	}
	if status.Grafana.Running {
		t.Fatal("expected grafana not running before start")
	}

	// Pre-populate systemd mock with active units.
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("grafana"), ActiveState: "active"},
	}

	status = m.Status(ctx)
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

func TestStatusReportsCorrectImages(t *testing.T) {
	sd := systemd.InitMockManager()
	m := NewManager(Config{
		Systemd: sd,
		DataDir: t.TempDir(),
	})

	status := m.Status(context.Background())

	if status.Prometheus.Image != PrometheusImage {
		t.Fatalf("expected prometheus image %q, got %q", PrometheusImage, status.Prometheus.Image)
	}
	if status.NodeExporter.Image != NodeExporterImage {
		t.Fatalf("expected node-exporter image %q, got %q", NodeExporterImage, status.NodeExporter.Image)
	}
	if status.Grafana.Image != GrafanaImage {
		t.Fatalf("expected grafana image %q, got %q", GrafanaImage, status.Grafana.Image)
	}
}

func TestWriteConfigs(t *testing.T) {
	dataDir := t.TempDir()
	sd := systemd.InitMockManager()
	m := NewManager(Config{
		Systemd: sd,
		DataDir: dataDir,
	})

	if err := m.writeConfigs(); err != nil {
		t.Fatalf("writeConfigs: %v", err)
	}

	// Verify prometheus.yml was created.
	promConfig, err := os.ReadFile(filepath.Join(dataDir, "prometheus.yml"))
	if err != nil {
		t.Fatalf("read prometheus.yml: %v", err)
	}
	if !strings.Contains(string(promConfig), "node-exporter") {
		t.Fatal("prometheus.yml should contain node-exporter scrape target")
	}

	// Verify grafana provisioning datasource.
	dsConfig, err := os.ReadFile(filepath.Join(dataDir, "grafana-provisioning", "datasources", "prometheus.yml"))
	if err != nil {
		t.Fatalf("read datasource config: %v", err)
	}
	if !strings.Contains(string(dsConfig), "prometheus") {
		t.Fatal("datasource config should reference prometheus")
	}

	// Verify grafana dashboard provisioning.
	dashConfig, err := os.ReadFile(filepath.Join(dataDir, "grafana-provisioning", "dashboards", "default.yml"))
	if err != nil {
		t.Fatalf("read dashboard config: %v", err)
	}
	if !strings.Contains(string(dashConfig), "dashboard-json") {
		t.Fatal("dashboard config should reference dashboard-json directory")
	}

	// Verify dashboard JSON was created.
	dashJSON, err := os.ReadFile(filepath.Join(dataDir, "grafana-provisioning", "dashboard-json", "node-exporter.json"))
	if err != nil {
		t.Fatalf("read dashboard JSON: %v", err)
	}
	if !strings.Contains(string(dashJSON), "System Overview") {
		t.Fatal("dashboard JSON should contain System Overview title")
	}

	// Verify Town OS Overview dashboard was created.
	overviewJSON, err := os.ReadFile(filepath.Join(dataDir, "grafana-provisioning", "dashboard-json", "town-os-overview.json"))
	if err != nil {
		t.Fatalf("read town-os-overview.json: %v", err)
	}
	if !strings.Contains(string(overviewJSON), "Town OS Overview") {
		t.Fatal("town-os-overview.json should contain Town OS Overview title")
	}
}

func TestCustomPorts(t *testing.T) {
	sd := systemd.InitMockManager()
	// Pre-populate active units so Status reports running.
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("grafana"), ActiveState: "active"},
	}
	m := NewManager(Config{
		Systemd:          sd,
		DataDir:          t.TempDir(),
		PrometheusPort:   "19090",
		NodeExporterPort: "19100",
		GrafanaPort:      "13000",
	})

	status := m.Status(context.Background())
	if status.Prometheus.Port != "19090" {
		t.Fatalf("expected prometheus port 19090, got %s", status.Prometheus.Port)
	}
	if status.NodeExporter.Port != "19100" {
		t.Fatalf("expected node-exporter port 19100, got %s", status.NodeExporter.Port)
	}
	if status.Grafana.Port != "13000" {
		t.Fatalf("expected grafana port 13000, got %s", status.Grafana.Port)
	}
}

func TestStartWithInstallError(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.InstallUnitErr = os.ErrPermission
	m := NewManager(Config{
		Systemd: sd,
		DataDir: t.TempDir(),
	})

	err := m.Start(context.Background())
	if err == nil {
		t.Fatal("expected error when InstallUnit fails")
	}
}

func TestSystemServices(t *testing.T) {
	sd := systemd.InitMockManager()
	m := NewManager(Config{
		Systemd:          sd,
		DataDir:          t.TempDir(),
		PrometheusPort:   "19090",
		NodeExporterPort: "19100",
		GrafanaPort:      "13000",
	})

	svcs := m.SystemServices()
	if len(svcs) != 3 {
		t.Fatalf("expected 3 system services, got %d", len(svcs))
	}

	expected := []struct {
		key         string
		displayName string
		image       string
		port        string
	}{
		{"prometheus", "Prometheus", PrometheusImage, "19090"},
		{"node-exporter", "Node Exporter", NodeExporterImage, "19100"},
		{"grafana", "Grafana", GrafanaImage, "13000"},
	}

	for i, e := range expected {
		if svcs[i].Key != e.key {
			t.Fatalf("service %d: expected key %q, got %q", i, e.key, svcs[i].Key)
		}
		if svcs[i].DisplayName != e.displayName {
			t.Fatalf("service %d: expected display name %q, got %q", i, e.displayName, svcs[i].DisplayName)
		}
		if svcs[i].Image != e.image {
			t.Fatalf("service %d: expected image %q, got %q", i, e.image, svcs[i].Image)
		}
		if svcs[i].Port != e.port {
			t.Fatalf("service %d: expected port %q, got %q", i, e.port, svcs[i].Port)
		}
		expectedUnit := systemd.SystemServiceUnitName(e.key)
		if svcs[i].UnitName != expectedUnit {
			t.Fatalf("service %d: expected unit name %q, got %q", i, expectedUnit, svcs[i].UnitName)
		}
	}
}

func TestPrometheusConfigIncludesNodeExporterTarget(t *testing.T) {
	dataDir := t.TempDir()
	if err := writePrometheusConfig(dataDir, "9091", "9101"); err != nil {
		t.Fatalf("writePrometheusConfig: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dataDir, "prometheus.yml"))
	if err != nil {
		t.Fatalf("read prometheus.yml: %v", err)
	}

	if !strings.Contains(string(data), "localhost:9101") {
		t.Fatal("prometheus config should scrape node-exporter on port 9101")
	}
}

func TestGrafanaProvisioningIncludesPrometheus(t *testing.T) {
	dataDir := t.TempDir()
	if err := writeGrafanaProvisioning(dataDir, "9091"); err != nil {
		t.Fatalf("writeGrafanaProvisioning: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dataDir, "grafana-provisioning", "datasources", "prometheus.yml"))
	if err != nil {
		t.Fatalf("read datasource config: %v", err)
	}

	if !strings.Contains(string(data), "localhost:9091") {
		t.Fatal("grafana datasource should point to prometheus on port 9091")
	}
}

func TestStartWritesConfigsToDataDir(t *testing.T) {
	dataDir := t.TempDir()
	sd := systemd.InitMockManager()
	m := NewManager(Config{
		Systemd: sd,
		DataDir: dataDir,
	})

	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// prometheus.yml should exist.
	if _, err := os.Stat(filepath.Join(dataDir, "prometheus.yml")); err != nil {
		t.Fatalf("expected prometheus.yml: %v", err)
	}

	// Grafana provisioning should exist.
	if _, err := os.Stat(filepath.Join(dataDir, "grafana-provisioning", "datasources", "prometheus.yml")); err != nil {
		t.Fatalf("expected grafana datasource config: %v", err)
	}
}

func TestDefaultPortValues(t *testing.T) {
	cfg := Config{}
	if cfg.prometheusHostPort() != "9091" {
		t.Fatalf("expected default prometheus port 9091, got %s", cfg.prometheusHostPort())
	}
	if cfg.nodeExporterHostPort() != "9101" {
		t.Fatalf("expected default node-exporter port 9101, got %s", cfg.nodeExporterHostPort())
	}
	if cfg.grafanaHostPort() != "3001" {
		t.Fatalf("expected default grafana port 3001, got %s", cfg.grafanaHostPort())
	}
}

func TestStatusPartialFailure(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "failed"},
		{Name: systemd.SystemServiceUnitName("grafana"), ActiveState: "inactive"},
	}
	m := NewManager(Config{
		Systemd: sd,
		DataDir: t.TempDir(),
	})

	status := m.Status(context.Background())
	if !status.Prometheus.Running {
		t.Fatal("expected prometheus running")
	}
	if status.NodeExporter.Running {
		t.Fatal("expected node-exporter not running (failed)")
	}
	if status.Grafana.Running {
		t.Fatal("expected grafana not running (inactive)")
	}
}

func TestStopIsNoOp(t *testing.T) {
	sd := systemd.InitMockManager()
	m := NewManager(Config{
		Systemd: sd,
		DataDir: t.TempDir(),
	})

	// Stop should not panic or produce errors.
	m.Stop()

	calls := sd.GetCalls()
	if len(calls) != 0 {
		t.Fatalf("expected 0 systemd calls from Stop, got %d", len(calls))
	}
}

func TestStatusContainerNames(t *testing.T) {
	sd := systemd.InitMockManager()
	m := NewManager(Config{
		Systemd: sd,
		DataDir: t.TempDir(),
	})

	status := m.Status(context.Background())

	if status.Prometheus.Name != systemd.SystemServiceUnitPrefix+"prometheus" {
		t.Fatalf("expected prometheus container name %q, got %q", systemd.SystemServiceUnitPrefix+"prometheus", status.Prometheus.Name)
	}
	if status.NodeExporter.Name != systemd.SystemServiceUnitPrefix+"node-exporter" {
		t.Fatalf("expected node-exporter container name %q, got %q", systemd.SystemServiceUnitPrefix+"node-exporter", status.NodeExporter.Name)
	}
	if status.Grafana.Name != systemd.SystemServiceUnitPrefix+"grafana" {
		t.Fatalf("expected grafana container name %q, got %q", systemd.SystemServiceUnitPrefix+"grafana", status.Grafana.Name)
	}
}

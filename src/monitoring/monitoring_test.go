package monitoring

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNewManager(t *testing.T) {
	runner := InitMockRunner()
	m := NewManager(Config{
		Runner:  runner,
		DataDir: t.TempDir(),
	})
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestStartCreatesContainers(t *testing.T) {
	runner := InitMockRunner()
	m := NewManager(Config{
		Runner:  runner,
		DataDir: t.TempDir(),
	})

	if err := m.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	calls := runner.GetCalls()
	// Expect: 3 Stop calls (pre-cleanup) + 3 Run calls.
	runCalls := 0
	for _, c := range calls {
		if c.Method == "Run" {
			runCalls++
		}
	}
	if runCalls != 3 {
		t.Fatalf("expected 3 Run calls, got %d", runCalls)
	}

	// All three containers should be "running" in the mock.
	for _, name := range []string{
		containerPrefix + "node-exporter",
		containerPrefix + "prometheus",
		containerPrefix + "grafana",
	} {
		if !runner.Running[name] {
			t.Fatalf("expected container %s to be running", name)
		}
	}
}

func TestStopRemovesContainers(t *testing.T) {
	runner := InitMockRunner()
	m := NewManager(Config{
		Runner:  runner,
		DataDir: t.TempDir(),
	})

	if err := m.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	m.Stop()

	// After Stop, all containers should not be running.
	for _, name := range []string{
		containerPrefix + "node-exporter",
		containerPrefix + "prometheus",
		containerPrefix + "grafana",
	} {
		if runner.Running[name] {
			t.Fatalf("expected container %s to be stopped after Stop()", name)
		}
	}
}

func TestStatusReportsRunningState(t *testing.T) {
	runner := InitMockRunner()
	m := NewManager(Config{
		Runner:  runner,
		DataDir: t.TempDir(),
	})

	ctx := context.Background()

	// Before start, nothing is running.
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

	// Start the stack.
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	status = m.Status(ctx)
	if !status.Prometheus.Running {
		t.Fatal("expected prometheus running after start")
	}
	if !status.NodeExporter.Running {
		t.Fatal("expected node-exporter running after start")
	}
	if !status.Grafana.Running {
		t.Fatal("expected grafana running after start")
	}
}

func TestStatusReportsCorrectImages(t *testing.T) {
	runner := InitMockRunner()
	m := NewManager(Config{
		Runner:  runner,
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

func TestGrafanaURL(t *testing.T) {
	runner := InitMockRunner()
	m := NewManager(Config{
		Runner:     runner,
		DataDir:    t.TempDir(),
		GrafanaPort: "4000",
	})

	url := m.GrafanaURL()
	if url != "http://127.0.0.1:4000" {
		t.Fatalf("expected http://127.0.0.1:4000, got %s", url)
	}
}

func TestGrafanaURLDefault(t *testing.T) {
	runner := InitMockRunner()
	m := NewManager(Config{
		Runner:  runner,
		DataDir: t.TempDir(),
	})

	url := m.GrafanaURL()
	if url != "http://127.0.0.1:3001" {
		t.Fatalf("expected http://127.0.0.1:3001, got %s", url)
	}
}

func TestWriteConfigs(t *testing.T) {
	dataDir := t.TempDir()
	runner := InitMockRunner()
	m := NewManager(Config{
		Runner:  runner,
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
}

func TestStartWithRunError(t *testing.T) {
	runner := InitMockRunner()
	runner.RunErr = os.ErrPermission
	m := NewManager(Config{
		Runner:  runner,
		DataDir: t.TempDir(),
	})

	err := m.Start(context.Background())
	if err == nil {
		t.Fatal("expected error when Run fails")
	}
}

func TestCustomPorts(t *testing.T) {
	runner := InitMockRunner()
	m := NewManager(Config{
		Runner:           runner,
		DataDir:          t.TempDir(),
		PrometheusPort:   "19090",
		NodeExporterPort: "19100",
		GrafanaPort:      "13000",
	})

	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	status := m.Status(ctx)
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

func TestPrometheusConfigIncludesNodeExporterTarget(t *testing.T) {
	dataDir := t.TempDir()
	if err := writePrometheusConfig(dataDir, "9101"); err != nil {
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

func TestContainerRunArgs(t *testing.T) {
	runner := InitMockRunner()
	m := NewManager(Config{
		Runner:  runner,
		DataDir: t.TempDir(),
	})

	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	calls := runner.GetCalls()

	// Find the Grafana Run call and check for embedding env var.
	for _, c := range calls {
		if c.Method != "Run" {
			continue
		}
		args, ok := c.Args[0].([]string)
		if !ok {
			continue
		}
		if !slices.Contains(args, containerPrefix+"grafana") {
			continue
		}

		// Verify Grafana has anonymous auth and embedding enabled.
		argsStr := strings.Join(args, " ")
		if !strings.Contains(argsStr, "GF_AUTH_ANONYMOUS_ENABLED=true") {
			t.Fatal("grafana should have anonymous auth enabled")
		}
		if !strings.Contains(argsStr, "GF_SECURITY_ALLOW_EMBEDDING=true") {
			t.Fatal("grafana should have embedding enabled")
		}
	}
}

func TestMockRunnerRecordsCalls(t *testing.T) {
	runner := InitMockRunner()
	ctx := context.Background()

	if err := runner.Run(ctx, []string{"--name", "test", "nginx"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := runner.Stop(ctx, "test"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := runner.IsRunning(ctx, "test"); err != nil {
		t.Fatalf("IsRunning: %v", err)
	}

	calls := runner.GetCalls()
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}
	if calls[0].Method != "Run" {
		t.Fatalf("expected Run, got %s", calls[0].Method)
	}
	if calls[1].Method != "Stop" {
		t.Fatalf("expected Stop, got %s", calls[1].Method)
	}
	if calls[2].Method != "IsRunning" {
		t.Fatalf("expected IsRunning, got %s", calls[2].Method)
	}
}

func TestMockRunnerTracksRunningState(t *testing.T) {
	runner := InitMockRunner()
	ctx := context.Background()

	// Initially not running.
	running, err := runner.IsRunning(ctx, "test")
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if running {
		t.Fatal("expected not running initially")
	}

	// After Run, should be running.
	if err := runner.Run(ctx, []string{"--name", "test", "nginx"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	running, err = runner.IsRunning(ctx, "test")
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if !running {
		t.Fatal("expected running after Run")
	}

	// After Stop, should not be running.
	if err := runner.Stop(ctx, "test"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	running, err = runner.IsRunning(ctx, "test")
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if running {
		t.Fatal("expected not running after Stop")
	}
}

func TestStartWritesConfigsToDataDir(t *testing.T) {
	dataDir := t.TempDir()
	runner := InitMockRunner()
	m := NewManager(Config{
		Runner:  runner,
		DataDir: dataDir,
	})

	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

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

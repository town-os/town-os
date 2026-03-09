// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/systemd"
)

func TestMonitoringStatusDisabledByDefault(t *testing.T) {
	c := initSystemControllerTest(t)

	status, err := c.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	if status.Prometheus.Running {
		t.Fatal("expected prometheus not running when monitoring disabled")
	}
	if status.Prometheus.Image != "" {
		t.Fatalf("expected empty prometheus image, got %q", status.Prometheus.Image)
	}
	if status.NodeExporter.Running {
		t.Fatal("expected node-exporter not running when monitoring disabled")
	}
	if status.NodeExporter.Image != "" {
		t.Fatalf("expected empty node-exporter image, got %q", status.NodeExporter.Image)
	}
	if status.Grafana.Running {
		t.Fatal("expected grafana not running when monitoring disabled")
	}
	if status.Grafana.Image != "" {
		t.Fatalf("expected empty grafana image, got %q", status.Grafana.Image)
	}
}

func TestMonitoringStatusBeforeStart(t *testing.T) {
	c, _, _ := initSystemControllerMonitoringTest(t)

	status, err := c.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	if status.Prometheus.Running {
		t.Fatal("expected prometheus not running before Start")
	}
	if status.Prometheus.Image == "" {
		t.Fatal("expected prometheus image to be populated")
	}
	if status.Prometheus.Port != "9091" {
		t.Fatalf("expected prometheus port 9091, got %q", status.Prometheus.Port)
	}

	if status.NodeExporter.Running {
		t.Fatal("expected node-exporter not running before Start")
	}
	if status.NodeExporter.Image == "" {
		t.Fatal("expected node-exporter image to be populated")
	}
	if status.NodeExporter.Port != "9101" {
		t.Fatalf("expected node-exporter port 9101, got %q", status.NodeExporter.Port)
	}

	if status.Grafana.Running {
		t.Fatal("expected grafana not running before Start")
	}
	if status.Grafana.Image == "" {
		t.Fatal("expected grafana image to be populated")
	}
	if status.Grafana.Port != "3001" {
		t.Fatalf("expected grafana port 3001, got %q", status.Grafana.Port)
	}
}

func TestMonitoringStatusAfterStart(t *testing.T) {
	c, monMgr, sd := initSystemControllerMonitoringTest(t)

	if err := monMgr.Start(context.Background()); err != nil {
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
		t.Fatal("expected prometheus running after Start")
	}
	if !status.NodeExporter.Running {
		t.Fatal("expected node-exporter running after Start")
	}
	if !status.Grafana.Running {
		t.Fatal("expected grafana running after Start")
	}
}

func TestMonitoringStatusDecodesFullStruct(t *testing.T) {
	c, monMgr, sd := initSystemControllerMonitoringTest(t)

	if err := monMgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer monMgr.Stop()

	// Pre-populate systemd mock with active units.
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("grafana"), ActiveState: "active"},
	}

	status, err := c.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	// Prometheus
	expectedPromName := systemd.SystemServiceContainerName("prometheus")
	if status.Prometheus.Name != expectedPromName {
		t.Fatalf("expected prometheus name %q, got %q", expectedPromName, status.Prometheus.Name)
	}
	if status.Prometheus.Image != monitoring.PrometheusImage {
		t.Fatalf("expected prometheus image %q, got %q", monitoring.PrometheusImage, status.Prometheus.Image)
	}
	if status.Prometheus.Port != "9091" {
		t.Fatalf("expected prometheus port 9091, got %q", status.Prometheus.Port)
	}
	if !status.Prometheus.Running {
		t.Fatal("expected prometheus running")
	}

	// Node Exporter
	expectedNEName := systemd.SystemServiceContainerName("node-exporter")
	if status.NodeExporter.Name != expectedNEName {
		t.Fatalf("expected node-exporter name %q, got %q", expectedNEName, status.NodeExporter.Name)
	}
	if status.NodeExporter.Image != monitoring.NodeExporterImage {
		t.Fatalf("expected node-exporter image %q, got %q", monitoring.NodeExporterImage, status.NodeExporter.Image)
	}
	if status.NodeExporter.Port != "9101" {
		t.Fatalf("expected node-exporter port 9101, got %q", status.NodeExporter.Port)
	}
	if !status.NodeExporter.Running {
		t.Fatal("expected node-exporter running")
	}

	// Grafana
	expectedGrafName := systemd.SystemServiceContainerName("grafana")
	if status.Grafana.Name != expectedGrafName {
		t.Fatalf("expected grafana name %q, got %q", expectedGrafName, status.Grafana.Name)
	}
	if status.Grafana.Image != monitoring.GrafanaImage {
		t.Fatalf("expected grafana image %q, got %q", monitoring.GrafanaImage, status.Grafana.Image)
	}
	if status.Grafana.Port != "3001" {
		t.Fatalf("expected grafana port 3001, got %q", status.Grafana.Port)
	}
	if !status.Grafana.Running {
		t.Fatal("expected grafana running")
	}
}

func TestMonitoringContainersRealStartAndAccessible(t *testing.T) {
	promPort := findFreePort(t)
	nePort := findFreePort(t)
	grafPort := findFreePort(t)

	dataDir := t.TempDir()
	sd := systemd.NewManager()

	mgr := monitoring.NewManager(monitoring.Config{
		Systemd:          sd,
		DataDir:          dataDir,
		PrometheusPort:   promPort,
		NodeExporterPort: nePort,
		GrafanaPort:      grafPort,
	})

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		// Stop and remove the systemd units installed by Start.
		for _, key := range []string{"node-exporter", "prometheus", "grafana"} {
			unitName := systemd.SystemServiceUnitName(key)
			_ = sd.SetStatus(ctx, unitName, systemd.Stop)
			_ = sd.UninstallUnit(ctx, unitName)
		}
	})

	// Wait for systemd to bring the containers up (Start installs the
	// units, but activation is asynchronous).
	var status monitoring.Status
	statusDeadline := time.Now().Add(time.Minute)
	for time.Now().Before(statusDeadline) {
		status = mgr.Status(ctx)
		if status.Prometheus.Running && status.NodeExporter.Running && status.Grafana.Running {
			break
		}
		time.Sleep(time.Second)
	}
	if !status.Prometheus.Running {
		t.Fatal("expected prometheus running after Start")
	}
	if !status.NodeExporter.Running {
		t.Fatal("expected node-exporter running after Start")
	}
	if !status.Grafana.Running {
		t.Fatal("expected grafana running after Start")
	}

	endpoints := []struct {
		name string
		url  string
	}{
		{"prometheus", fmt.Sprintf("http://localhost:%s/-/healthy", promPort)},
		{"node-exporter", fmt.Sprintf("http://localhost:%s/metrics", nePort)},
		{"grafana", fmt.Sprintf("http://localhost:%s/api/health", grafPort)},
	}

	client := &http.Client{Timeout: 5 * time.Second}
	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			deadline := time.Now().Add(time.Minute)
			for time.Now().Before(deadline) {
				req, reqErr := http.NewRequestWithContext(context.Background(), http.MethodGet, ep.url, nil)
				if reqErr != nil {
					t.Fatalf("NewRequest: %v", reqErr)
				}
				resp, err := client.Do(req)
				if err == nil {
					if closeErr := resp.Body.Close(); closeErr != nil {
						t.Errorf("resp.Body.Close: %v", closeErr)
					}
					if resp.StatusCode == http.StatusOK {
						return
					}
				}
				time.Sleep(500 * time.Millisecond)
			}
			t.Fatalf("%s did not become healthy at %s within 60s", ep.name, ep.url)
		})
	}
}

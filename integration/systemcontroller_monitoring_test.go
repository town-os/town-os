package integration_test

import (
	"context"
	"net/http"
	"testing"

	"gitea.com/town-os/town-os/src/monitoring"
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
	c, _ := initSystemControllerMonitoringTest(t)

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
	c, monMgr := initSystemControllerMonitoringTest(t)

	if err := monMgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer monMgr.Stop()

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
	c, monMgr := initSystemControllerMonitoringTest(t)

	if err := monMgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer monMgr.Stop()

	status, err := c.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	// Prometheus
	if status.Prometheus.Name != "town-os-monitoring-prometheus" {
		t.Fatalf("expected prometheus name %q, got %q", "town-os-monitoring-prometheus", status.Prometheus.Name)
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
	if status.NodeExporter.Name != "town-os-monitoring-node-exporter" {
		t.Fatalf("expected node-exporter name %q, got %q", "town-os-monitoring-node-exporter", status.NodeExporter.Name)
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
	if status.Grafana.Name != "town-os-monitoring-grafana" {
		t.Fatalf("expected grafana name %q, got %q", "town-os-monitoring-grafana", status.Grafana.Name)
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

func TestMonitoringGrafanaProxyDisabledReturns503(t *testing.T) {
	c := initSystemControllerTest(t)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, c.BaseURL+"/monitoring/grafana/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("GET /monitoring/grafana/: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when monitoring disabled, got %d", resp.StatusCode)
	}
}

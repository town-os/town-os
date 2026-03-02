package monitoring

import (
	"fmt"
	"os"
	"path/filepath"
)

// writePrometheusConfig creates a prometheus.yml in the given data directory
// that scrapes the local Node Exporter and Prometheus itself.
func writePrometheusConfig(dataDir, nodeExporterPort string) error {
	config := fmt.Sprintf(`global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: "prometheus"
    static_configs:
      - targets: ["localhost:9091"]

  - job_name: "node-exporter"
    static_configs:
      - targets: ["localhost:%s"]
`, nodeExporterPort)

	return os.WriteFile(filepath.Join(dataDir, "prometheus.yml"), []byte(config), 0600)
}

// writeGrafanaProvisioning creates Grafana provisioning files that
// auto-configure Prometheus as a data source and install a default
// Node Exporter dashboard.
func writeGrafanaProvisioning(dataDir, prometheusPort string) error {
	provDir := filepath.Join(dataDir, "grafana-provisioning")
	dsDir := filepath.Join(provDir, "datasources")
	dashDir := filepath.Join(provDir, "dashboards")
	dashJSONDir := filepath.Join(provDir, "dashboard-json")

	for _, d := range []string{dsDir, dashDir, dashJSONDir} {
		if err := os.MkdirAll(d, 0750); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}

	// Datasource provisioning: auto-configure Prometheus.
	dsConfig := fmt.Sprintf(`apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://localhost:%s
    isDefault: true
    editable: false
`, prometheusPort)

	if err := os.WriteFile(filepath.Join(dsDir, "prometheus.yml"), []byte(dsConfig), 0600); err != nil {
		return fmt.Errorf("write datasource config: %w", err)
	}

	// Dashboard provisioning: point Grafana at the dashboard JSON directory.
	dashConfig := `apiVersion: 1
providers:
  - name: "default"
    orgId: 1
    folder: ""
    type: file
    disableDeletion: false
    editable: true
    options:
      path: /etc/grafana/provisioning/dashboard-json
      foldersFromFilesStructure: false
`

	if err := os.WriteFile(filepath.Join(dashDir, "default.yml"), []byte(dashConfig), 0600); err != nil {
		return fmt.Errorf("write dashboard provisioner config: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dashJSONDir, "node-exporter.json"), []byte(nodeExporterDashboard), 0600); err != nil {
		return fmt.Errorf("write node exporter dashboard: %w", err)
	}

	return nil
}

// nodeExporterDashboard is a minimal Grafana dashboard JSON that displays
// key Node Exporter metrics: CPU usage, memory usage, disk usage, and
// network I/O.
const nodeExporterDashboard = `{
  "annotations": { "list": [] },
  "editable": true,
  "fiscalYearStartMonth": 0,
  "graphTooltip": 0,
  "id": null,
  "links": [],
  "panels": [
    {
      "datasource": { "type": "prometheus", "uid": "" },
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "palette-classic" },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "drawStyle": "line",
            "fillOpacity": 10,
            "gradientMode": "none",
            "hideFrom": { "legend": false, "tooltip": false, "viz": false },
            "insertNulls": false,
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "pointSize": 5,
            "scaleDistribution": { "type": "linear" },
            "showPoints": "never",
            "spanNulls": false,
            "stacking": { "group": "A", "mode": "none" },
            "thresholdsStyle": { "mode": "off" }
          },
          "unit": "percentunit",
          "min": 0,
          "max": 1
        }
      },
      "gridPos": { "h": 8, "w": 12, "x": 0, "y": 0 },
      "id": 1,
      "options": { "legend": { "calcs": ["mean", "lastNotNull"], "displayMode": "table", "placement": "bottom" }, "tooltip": { "mode": "multi" } },
      "title": "CPU Usage",
      "type": "timeseries",
      "targets": [
        {
          "expr": "1 - avg(rate(node_cpu_seconds_total{mode=\"idle\"}[5m]))",
          "legendFormat": "CPU Usage",
          "refId": "A"
        }
      ]
    },
    {
      "datasource": { "type": "prometheus", "uid": "" },
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "palette-classic" },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "drawStyle": "line",
            "fillOpacity": 10,
            "gradientMode": "none",
            "hideFrom": { "legend": false, "tooltip": false, "viz": false },
            "insertNulls": false,
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "pointSize": 5,
            "scaleDistribution": { "type": "linear" },
            "showPoints": "never",
            "spanNulls": false,
            "stacking": { "group": "A", "mode": "none" },
            "thresholdsStyle": { "mode": "off" }
          },
          "unit": "percentunit",
          "min": 0,
          "max": 1
        }
      },
      "gridPos": { "h": 8, "w": 12, "x": 12, "y": 0 },
      "id": 2,
      "options": { "legend": { "calcs": ["mean", "lastNotNull"], "displayMode": "table", "placement": "bottom" }, "tooltip": { "mode": "multi" } },
      "title": "Memory Usage",
      "type": "timeseries",
      "targets": [
        {
          "expr": "1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)",
          "legendFormat": "Memory Usage",
          "refId": "A"
        }
      ]
    },
    {
      "datasource": { "type": "prometheus", "uid": "" },
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "palette-classic" },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "drawStyle": "line",
            "fillOpacity": 10,
            "gradientMode": "none",
            "hideFrom": { "legend": false, "tooltip": false, "viz": false },
            "insertNulls": false,
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "pointSize": 5,
            "scaleDistribution": { "type": "linear" },
            "showPoints": "never",
            "spanNulls": false,
            "stacking": { "group": "A", "mode": "none" },
            "thresholdsStyle": { "mode": "off" }
          },
          "unit": "percentunit",
          "min": 0,
          "max": 1
        }
      },
      "gridPos": { "h": 8, "w": 12, "x": 0, "y": 8 },
      "id": 3,
      "options": { "legend": { "calcs": ["mean", "lastNotNull"], "displayMode": "table", "placement": "bottom" }, "tooltip": { "mode": "multi" } },
      "title": "Disk Usage",
      "type": "timeseries",
      "targets": [
        {
          "expr": "1 - (node_filesystem_avail_bytes{mountpoint=\"/\"} / node_filesystem_size_bytes{mountpoint=\"/\"})",
          "legendFormat": "Disk Usage (/)",
          "refId": "A"
        }
      ]
    },
    {
      "datasource": { "type": "prometheus", "uid": "" },
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "palette-classic" },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "drawStyle": "line",
            "fillOpacity": 10,
            "gradientMode": "none",
            "hideFrom": { "legend": false, "tooltip": false, "viz": false },
            "insertNulls": false,
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "pointSize": 5,
            "scaleDistribution": { "type": "linear" },
            "showPoints": "never",
            "spanNulls": false,
            "stacking": { "group": "A", "mode": "none" },
            "thresholdsStyle": { "mode": "off" }
          },
          "unit": "Bps"
        }
      },
      "gridPos": { "h": 8, "w": 12, "x": 12, "y": 8 },
      "id": 4,
      "options": { "legend": { "calcs": ["mean", "lastNotNull"], "displayMode": "table", "placement": "bottom" }, "tooltip": { "mode": "multi" } },
      "title": "Network I/O",
      "type": "timeseries",
      "targets": [
        {
          "expr": "rate(node_network_receive_bytes_total{device!=\"lo\"}[5m])",
          "legendFormat": "{{device}} RX",
          "refId": "A"
        },
        {
          "expr": "rate(node_network_transmit_bytes_total{device!=\"lo\"}[5m])",
          "legendFormat": "{{device}} TX",
          "refId": "B"
        }
      ]
    }
  ],
  "schemaVersion": 39,
  "tags": ["node-exporter", "system"],
  "templating": { "list": [] },
  "time": { "from": "now-1h", "to": "now" },
  "timepicker": {},
  "timezone": "",
  "title": "System Overview",
  "uid": "town-os-system-overview",
  "version": 1,
  "refresh": "30s"
}`

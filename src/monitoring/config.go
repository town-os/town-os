package monitoring

import (
	"fmt"
	"os"
	"path/filepath"
)

// writePrometheusConfig creates a prometheus.yml in the given data directory
// that scrapes the local Node Exporter and Prometheus itself.
func writePrometheusConfig(dataDir, prometheusPort, nodeExporterPort string) error {
	config := fmt.Sprintf(`global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: "prometheus"
    static_configs:
      - targets: ["localhost:%s"]

  - job_name: "node-exporter"
    static_configs:
      - targets: ["localhost:%s"]
`, prometheusPort, nodeExporterPort)

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
  - name: "Town OS Status"
    orgId: 1
    folder: ""
    type: file
    disableDeletion: true
    editable: false
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

	if err := os.WriteFile(filepath.Join(dashJSONDir, "town-os-overview.json"), []byte(townOSOverviewDashboard), 0600); err != nil {
		return fmt.Errorf("write town-os overview dashboard: %w", err)
	}

	return nil
}

// townOSOverviewDashboard is a Grafana dashboard with Disk I/O (/town-os),
// external Network usage, CPU breakdown, and Memory totals.
const townOSOverviewDashboard = `{
  "annotations": {},
  "editable": false,
  "fiscalYearStartMonth": 0,
  "graphTooltip": 0,
  "links": [],
  "liveNow": false,
  "panels": [
    {
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "palette-classic" },
          "custom": {
            "axisBorderShow": false, "drawStyle": "line",
            "fillOpacity": 10, "gradientMode": "none",
            "lineInterpolation": "smooth", "lineWidth": 1,
            "pointSize": 5, "showPoints": "never", "spanNulls": false,
            "stacking": { "group": "A", "mode": "none" },
            "thresholdsStyle": { "mode": "off" }
          },
          "thresholds": { "mode": "absolute", "steps": [{ "color": "green", "value": 0 }] },
          "unit": "Bps"
        },
        "overrides": []
      },
      "gridPos": { "h": 8, "w": 12, "x": 0, "y": 0 },
      "id": 1,
      "options": {
        "legend": { "calcs": ["mean","lastNotNull"], "displayMode": "table", "placement": "bottom", "showLegend": true },
        "tooltip": { "mode": "multi", "sort": "desc" }
      },
      "targets": [
        { "datasource": "Prometheus", "expr": "rate(node_disk_read_bytes_total{device=~\"sd.*|nvme.*|vd.*\"}[$__rate_interval]) * on(device) group_left node_filesystem_size_bytes{mountpoint=\"/town-os\"} / node_filesystem_size_bytes{mountpoint=\"/town-os\"}", "legendFormat": "Read", "refId": "A" },
        { "datasource": "Prometheus", "expr": "rate(node_disk_written_bytes_total{device=~\"sd.*|nvme.*|vd.*\"}[$__rate_interval]) * on(device) group_left node_filesystem_size_bytes{mountpoint=\"/town-os\"} / node_filesystem_size_bytes{mountpoint=\"/town-os\"}", "legendFormat": "Write", "refId": "B" }
      ],
      "title": "Disk I/O (/town-os)",
      "transparent": true,
      "type": "timeseries"
    },
    {
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "palette-classic" },
          "custom": {
            "axisBorderShow": false, "drawStyle": "line",
            "fillOpacity": 10, "gradientMode": "none",
            "lineInterpolation": "smooth", "lineWidth": 1,
            "pointSize": 5, "showPoints": "never", "spanNulls": false,
            "stacking": { "group": "A", "mode": "none" },
            "thresholdsStyle": { "mode": "off" }
          },
          "thresholds": { "mode": "absolute", "steps": [{ "color": "green", "value": 0 }] },
          "unit": "bps"
        },
        "overrides": []
      },
      "gridPos": { "h": 8, "w": 12, "x": 12, "y": 0 },
      "id": 2,
      "options": {
        "legend": { "calcs": ["mean","lastNotNull"], "displayMode": "table", "placement": "bottom", "showLegend": true },
        "tooltip": { "mode": "multi", "sort": "desc" }
      },
      "targets": [
        { "datasource": "Prometheus", "expr": "rate(node_network_receive_bytes_total{device!~\"lo|veth.*|podman.*|cni.*|tailscale.*|br-.*|docker.*\"}[$__rate_interval]) * 8", "legendFormat": "{{device}} Rx", "refId": "A" },
        { "datasource": "Prometheus", "expr": "rate(node_network_transmit_bytes_total{device!~\"lo|veth.*|podman.*|cni.*|tailscale.*|br-.*|docker.*\"}[$__rate_interval]) * 8", "legendFormat": "{{device}} Tx", "refId": "B" }
      ],
      "title": "Network (External)",
      "transparent": true,
      "type": "timeseries"
    },
    {
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "palette-classic" },
          "custom": {
            "axisBorderShow": false, "drawStyle": "line",
            "fillOpacity": 20, "gradientMode": "none",
            "lineInterpolation": "smooth", "lineWidth": 1,
            "pointSize": 5, "showPoints": "never", "spanNulls": false,
            "stacking": { "group": "A", "mode": "normal" },
            "thresholdsStyle": { "mode": "off" }
          },
          "max": 100, "min": 0,
          "thresholds": { "mode": "absolute", "steps": [{ "color": "green", "value": 0 }, { "color": "red", "value": 90 }] },
          "unit": "percent"
        },
        "overrides": [
          {
            "matcher": { "id": "byName", "options": "Total" },
            "properties": [
              { "id": "custom.stacking", "value": { "group": "B", "mode": "none" } },
              { "id": "custom.fillOpacity", "value": 0 },
              { "id": "custom.lineWidth", "value": 2 },
              { "id": "color", "value": { "fixedColor": "white", "mode": "fixed" } }
            ]
          }
        ]
      },
      "gridPos": { "h": 8, "w": 12, "x": 0, "y": 8 },
      "id": 3,
      "options": {
        "legend": { "calcs": ["mean","lastNotNull"], "displayMode": "table", "placement": "bottom", "showLegend": true },
        "tooltip": { "mode": "multi", "sort": "desc" }
      },
      "targets": [
        { "datasource": "Prometheus", "expr": "sum by (mode) (rate(node_cpu_seconds_total{mode=~\"user|system|iowait|irq|softirq|steal|nice\"}[$__rate_interval])) * 100 / scalar(count(node_cpu_seconds_total{mode=\"user\"}))", "legendFormat": "{{mode}}", "refId": "A" },
        { "datasource": "Prometheus", "expr": "(1 - sum(rate(node_cpu_seconds_total{mode=\"idle\"}[$__rate_interval])) / scalar(count(node_cpu_seconds_total{mode=\"user\"}))) * 100", "legendFormat": "Total", "refId": "B" }
      ],
      "title": "CPU Usage",
      "transparent": true,
      "type": "timeseries"
    },
    {
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "palette-classic" },
          "custom": {
            "axisBorderShow": false, "drawStyle": "line",
            "fillOpacity": 10, "gradientMode": "none",
            "lineInterpolation": "smooth", "lineWidth": 1,
            "pointSize": 5, "showPoints": "never", "spanNulls": false,
            "stacking": { "group": "A", "mode": "none" },
            "thresholdsStyle": { "mode": "off" }
          },
          "thresholds": { "mode": "absolute", "steps": [{ "color": "green", "value": 0 }] },
          "unit": "bytes"
        },
        "overrides": [
          {
            "matcher": { "id": "byName", "options": "Total" },
            "properties": [
              { "id": "custom.fillOpacity", "value": 0 },
              { "id": "custom.lineStyle", "value": { "dash": [10,10], "fill": "dash" } },
              { "id": "color", "value": { "fixedColor": "white", "mode": "fixed" } }
            ]
          }
        ]
      },
      "gridPos": { "h": 8, "w": 12, "x": 12, "y": 8 },
      "id": 4,
      "options": {
        "legend": { "calcs": ["mean","lastNotNull"], "displayMode": "table", "placement": "bottom", "showLegend": true },
        "tooltip": { "mode": "multi", "sort": "desc" }
      },
      "targets": [
        { "datasource": "Prometheus", "expr": "node_memory_MemTotal_bytes", "legendFormat": "Total", "refId": "A" },
        { "datasource": "Prometheus", "expr": "node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes", "legendFormat": "Used", "refId": "B" },
        { "datasource": "Prometheus", "expr": "node_memory_MemAvailable_bytes", "legendFormat": "Available", "refId": "C" }
      ],
      "title": "Memory Usage",
      "transparent": true,
      "type": "timeseries"
    }
  ],
  "schemaVersion": 42,
  "tags": [],
  "templating": { "list": [] },
  "time": { "from": "now-6h", "to": "now" },
  "timepicker": { "hidden": false, "refresh_intervals": ["5s","10s","30s","1m","5m","15m","30m","1h","2h","1d"] },
  "timezone": "browser",
  "title": "Town OS Overview",
  "uid": "town-os-overview",
  "version": 2,
  "id": null
}`

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
      "transparent": true,
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
      "transparent": true,
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
      "transparent": true,
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
      "transparent": true,
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

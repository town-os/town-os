package monitoring

// GrafanaDatasourceYAML returns the Grafana datasource provisioning YAML
// that auto-configures Prometheus as the default data source.
func GrafanaDatasourceYAML(prometheusHost string) string {
	return `apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://` + prometheusHost + `:9090
    isDefault: true
    editable: false
`
}

// GrafanaDashboardProviderYAML returns the dashboard provisioner config.
const GrafanaDashboardProviderYAML = `apiVersion: 1
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

// TownOSOverviewDashboard is the default Grafana dashboard loaded by the
// monitoring UI iframe. It has four panels: Disk I/O for the `/town-os`
// mount, external Network throughput (excluding virtual interfaces),
// CPU breakdown stacked by mode with a Total overlay, and Memory Usage.
// All panels are transparent so they blend with the iframe background.
// The dashboard uid (`town-os-overview`) is stable across restarts and
// referenced by the MonitoringDashboard route in the web UI.
const TownOSOverviewDashboard = `{
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

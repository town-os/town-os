package monitoring

import (
	"fmt"
	"strings"
)

// GrafanaDatasourceUID is the pinned uid of the provisioned Prometheus
// datasource. Dashboard panel targets reference it via the object-form
// datasource ref ({"type":"prometheus","uid":GrafanaDatasourceUID}); the
// Grafana 13+ frontend cannot resolve legacy string-form refs like
// "Prometheus" in panel targets, so pinning a stable uid here and in the
// dashboard JSON is the single source of truth that keeps panel queries
// wired to the datasource across restarts and reprovisioning.
const GrafanaDatasourceUID = "townos-prometheus"

// GrafanaDatasourceYAML returns the Grafana datasource provisioning YAML
// that auto-configures Prometheus as the default data source.
func GrafanaDatasourceYAML(prometheusHost, prometheusPort string) string {
	if prometheusPort == "" {
		prometheusPort = PrometheusPort
	}
	return `apiVersion: 1
datasources:
  - name: Prometheus
    uid: ` + GrafanaDatasourceUID + `
    type: prometheus
    access: proxy
    url: http://` + prometheusHost + `:` + prometheusPort + `
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

// NoBtrfsDevicesSentinel is substituted into the Disk I/O device regex
// when the controller could not discover any block devices backing the
// btrfs filesystem. It is a string that no real kernel device name can
// match, so the panel renders "No data" rather than silently summing
// every disk on the host.
const NoBtrfsDevicesSentinel = "__no_btrfs_devices__"

// DiskDeviceRegex builds the PromQL device-label regex for the Disk I/O
// panel from the list of kernel device basenames backing /town-os. An
// empty list resolves to NoBtrfsDevicesSentinel.
func DiskDeviceRegex(diskDevices []string) string {
	if len(diskDevices) == 0 {
		return NoBtrfsDevicesSentinel
	}
	return strings.Join(diskDevices, "|")
}

// TownOSOverviewDashboard returns the default Grafana dashboard loaded
// by the monitoring UI iframe. It has four panels: Disk I/O summed
// across the block devices that back the btrfs filesystem at /town-os,
// external Network throughput (excluding virtual interfaces), CPU
// breakdown stacked by mode with a Total overlay, and Memory Usage.
// All panels are transparent so they blend with the iframe background.
// The dashboard uid (`town-os-overview`) is stable across restarts and
// referenced by the MonitoringDashboard route in the web UI.
//
// diskDevices is the list of kernel device basenames (e.g., "sda3",
// "nvme0n1p3") that node_exporter reports for `node_disk_*` metrics.
// An empty/nil slice produces a regex that matches nothing, leaving
// the Disk I/O panel empty rather than misleadingly summing unrelated
// devices.
func TownOSOverviewDashboard(diskDevices []string) string {
	regex := DiskDeviceRegex(diskDevices)
	readExpr := fmt.Sprintf("sum(rate(node_disk_read_bytes_total{device=~\\\"%s\\\"}[$__rate_interval]))", regex)
	writeExpr := fmt.Sprintf("sum(rate(node_disk_written_bytes_total{device=~\\\"%s\\\"}[$__rate_interval]))", regex)
	return fmt.Sprintf(townOSOverviewDashboardTemplate, readExpr, writeExpr)
}

// townOSOverviewDashboardTemplate is the dashboard JSON with two %s
// placeholders for the Disk I/O read and write target expressions.
const townOSOverviewDashboardTemplate = `{
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
        "legend": { "calcs": ["lastNotNull"], "displayMode": "list", "placement": "bottom", "showLegend": true },
        "tooltip": { "mode": "multi", "sort": "desc" }
      },
      "targets": [
        { "datasource": { "type": "prometheus", "uid": "townos-prometheus" }, "expr": "%s", "legendFormat": "Read", "refId": "A" },
        { "datasource": { "type": "prometheus", "uid": "townos-prometheus" }, "expr": "%s", "legendFormat": "Write", "refId": "B" }
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
        "legend": { "calcs": ["lastNotNull"], "displayMode": "list", "placement": "bottom", "showLegend": true },
        "tooltip": { "mode": "multi", "sort": "desc" }
      },
      "targets": [
        { "datasource": { "type": "prometheus", "uid": "townos-prometheus" }, "expr": "(rate(node_network_receive_bytes_total{device!~\"lo|veth.*|podman.*|cni.*|tailscale.*|br-.*|docker.*\"}[$__rate_interval]) and on (device) (node_network_up == 1)) * 8", "legendFormat": "{{device}} Rx", "refId": "A" },
        { "datasource": { "type": "prometheus", "uid": "townos-prometheus" }, "expr": "(rate(node_network_transmit_bytes_total{device!~\"lo|veth.*|podman.*|cni.*|tailscale.*|br-.*|docker.*\"}[$__rate_interval]) and on (device) (node_network_up == 1)) * 8", "legendFormat": "{{device}} Tx", "refId": "B" }
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
        "legend": { "calcs": ["lastNotNull"], "displayMode": "list", "placement": "bottom", "showLegend": true },
        "tooltip": { "mode": "multi", "sort": "desc" }
      },
      "targets": [
        { "datasource": { "type": "prometheus", "uid": "townos-prometheus" }, "expr": "sum by (mode) (rate(node_cpu_seconds_total{mode=~\"user|system|iowait|irq|softirq|steal|nice\"}[$__rate_interval])) * 100 / scalar(count(node_cpu_seconds_total{mode=\"user\"}))", "legendFormat": "{{mode}}", "refId": "A" },
        { "datasource": { "type": "prometheus", "uid": "townos-prometheus" }, "expr": "(1 - sum(rate(node_cpu_seconds_total{mode=\"idle\"}[$__rate_interval])) / scalar(count(node_cpu_seconds_total{mode=\"user\"}))) * 100", "legendFormat": "Total", "refId": "B" }
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
        "legend": { "calcs": ["lastNotNull"], "displayMode": "list", "placement": "bottom", "showLegend": true },
        "tooltip": { "mode": "multi", "sort": "desc" }
      },
      "targets": [
        { "datasource": { "type": "prometheus", "uid": "townos-prometheus" }, "expr": "node_memory_MemTotal_bytes", "legendFormat": "Total", "refId": "A" },
        { "datasource": { "type": "prometheus", "uid": "townos-prometheus" }, "expr": "node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes", "legendFormat": "Used", "refId": "B" },
        { "datasource": { "type": "prometheus", "uid": "townos-prometheus" }, "expr": "node_memory_MemAvailable_bytes", "legendFormat": "Available", "refId": "C" }
      ],
      "title": "Memory Usage",
      "transparent": true,
      "type": "timeseries"
    }
  ],
  "schemaVersion": 42,
  "tags": [],
  "templating": { "list": [] },
  "refresh": "30s",
  "time": { "from": "now-6h", "to": "now" },
  "timepicker": { "hidden": false, "refresh_intervals": ["5s","10s","30s","1m","5m","15m","30m","1h","2h","1d"] },
  "timezone": "browser",
  "title": "Town OS Overview",
  "uid": "town-os-overview",
  "version": 2,
  "id": null
}`

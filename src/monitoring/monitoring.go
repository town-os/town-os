// Package monitoring manages the monitoring stack (Prometheus, Node Exporter,
// and optionally Grafana) as a regular Town OS package with a network
// controller. Node Exporter runs as a system service (host networking);
// Prometheus/Grafana run as package containers.
package monitoring

const (
	// PrometheusImage is the container image reference for Prometheus.
	PrometheusImage = "quay.io/prometheus/prometheus:latest"
	// NodeExporterImage is the container image reference for Node Exporter.
	NodeExporterImage = "quay.io/prometheus/node-exporter:latest"
	// GrafanaImage is the container image reference for Grafana.
	GrafanaImage = "docker.io/grafana/grafana:latest"
	// DefaultSocatImage is the fallback container image for the uPlot socat
	// forwarder when no NC image is available.
	DefaultSocatImage = "localhost/town-os-networkcontroller:local"

	// PrometheusPort is the default internal port used by Prometheus.
	PrometheusPort = "9090"
	// NodeExporterPort is the default host port for Node Exporter.
	NodeExporterPort = "9100"
	// GrafanaPort is the default internal port for Grafana.
	GrafanaPort = "3000"
	// MonitoringExternalPort is the external port exposed for the monitoring dashboard.
	MonitoringExternalPort = "5308"

	// BackendUPlot selects the lightweight uPlot frontend (default).
	BackendUPlot = "uplot"
	// BackendGrafana selects the Grafana dashboard frontend.
	BackendGrafana = "grafana"
)

// SystemService describes a system service managed outside the package system.
type SystemService struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	Image       string `json:"image"`
	Port        string `json:"port"`
	UnitName    string `json:"unit_name"`
}

// MonitoringStatus represents the state of the monitoring stack for the API.
// MonitoringUI reflects whether the monitoring-ui unit (socat in uPlot mode,
// Grafana in grafana mode) is active; the UI polls this after changing the
// backend to confirm the restart completed.
type MonitoringStatus struct {
	Backend      string   `json:"backend"`
	Prometheus   bool     `json:"prometheus"`
	NodeExporter bool     `json:"node_exporter"`
	MonitoringUI bool     `json:"monitoring_ui"`
	Grafana      bool     `json:"grafana,omitempty"`
	// DiskDevices lists the kernel device basenames (e.g. "sda3",
	// "nvme0n1p3") backing the btrfs filesystem mounted at /town-os.
	// The frontend uses these to build the Disk I/O panel's PromQL
	// query. Empty when discovery failed; the dashboard falls back to a
	// sentinel regex that matches nothing.
	DiskDevices []string `json:"disk_devices,omitempty"`
}

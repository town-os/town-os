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
	// SocatImage is the container image for the uPlot socat forwarder.
	SocatImage = "docker.io/library/alpine:latest"

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
type MonitoringStatus struct {
	Backend      string `json:"backend"`
	Prometheus   bool   `json:"prometheus"`
	NodeExporter bool   `json:"node_exporter"`
	Grafana      bool   `json:"grafana,omitempty"`
}

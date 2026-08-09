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

// Ports are the host ports the three monitoring system services bind, plus the
// address of the one external service Prometheus scrapes (rolodex).
//
// All three run --net host, so they bind in whatever network namespace the
// systemcontroller itself is in. On a real box that is the host and the
// defaults are what everything (the UI, the Grafana datasource, the Prometheus
// scrape config) expects. Inside the integration harness it is *also* the host
// namespace — the test container runs --net host too — so a test box and a
// `make dev` box would fight over 9100/9090/5308 and crash-loop each other
// under Restart=always. The harness therefore assigns every field an ephemeral
// port, exactly as it already does for TOWN_OS_LISTEN — IRON RULE.
//
// The zero value means "use the documented defaults", so a caller that does
// not care about ports passes Ports{} and gets today's behavior unchanged.
type Ports struct {
	// NodeExporter is the loopback port node-exporter serves metrics on
	// (default NodeExporterPort). Only Prometheus ever connects to it.
	NodeExporter string
	// Prometheus is the loopback port Prometheus serves its HTTP API on
	// (default PrometheusPort). Reached from the browser only through the
	// monitoring UI forwarder, never directly.
	Prometheus string
	// External is the single LAN-facing monitoring port (default
	// MonitoringExternalPort): socat in uPlot mode, Grafana in grafana mode.
	External string
	// RolodexMetrics is the "host:port" address rolodex serves its Prometheus
	// endpoint on, and is the one field here that is an address rather than a
	// port this stack binds: rolodex is a separate system service that chooses
	// its own listener, so monitoring is told where to scrape rather than
	// deciding it. rolodex.Manager.MetricsAddr() is the value to pass, which is
	// the same string it writes into rolodex.yml — that is what keeps the
	// target from drifting from the listener.
	//
	// Empty means no rolodex scrape job, which is what every test that builds a
	// bare Ports{} gets, so the generated config is unchanged for them.
	RolodexMetrics string
	// ControllerMetrics is the "host:port" address the system controller serves
	// its Prometheus endpoint on. Like RolodexMetrics this is an address rather
	// than a port this stack binds, and for the same reason: the controller's
	// listener is chosen by -listen/TOWN_OS_LISTEN, so monitoring is told where
	// to scrape rather than deciding it. That also means no new port to
	// relocate for concurrent test-full and dev runs — the endpoint rides the
	// listener the harness already moves.
	//
	// Empty omits the job.
	ControllerMetrics string
	// ControllerMetricsScheme is "https" when the controller's own listener is
	// TLS-terminated (TOWN_OS_TLS), "" or "http" otherwise. It exists because
	// that certificate is issued by the box's local CA, which Prometheus has no
	// reason to trust, so the https job must also skip verification — see
	// WritePrometheusConfig.
	ControllerMetricsScheme string
}

// withDefaults returns a copy with every empty field filled in with its
// documented default. Every consumer in this package calls it, so the
// defaulting lives in exactly one place rather than at each use site.
func (p Ports) withDefaults() Ports {
	if p.NodeExporter == "" {
		p.NodeExporter = NodeExporterPort
	}
	if p.Prometheus == "" {
		p.Prometheus = PrometheusPort
	}
	if p.External == "" {
		p.External = MonitoringExternalPort
	}
	return p
}

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

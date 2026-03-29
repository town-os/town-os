package monitoring

import (
	"fmt"
	"os"
	"path/filepath"

	"gitea.com/town-os/town-os/src/packages"
)

const (
	// MonitoringRepo is the synthetic repository name for built-in packages.
	MonitoringRepo = "_system"
	// MonitoringPackageName is the package name for the monitoring stack.
	MonitoringPackageName = "monitoring"
	// MonitoringVersion is the current version of the monitoring package.
	MonitoringVersion = "1.0"
	// MonitoringExternalPort is the external port exposed for the monitoring dashboard.
	MonitoringExternalPort = "5308"

	// BackendUPlot selects the lightweight uPlot frontend (default).
	BackendUPlot = "uplot"
	// BackendGrafana selects the Grafana dashboard frontend.
	BackendGrafana = "grafana"
)

// MonitoringPackageIdentity returns the package identity for the monitoring stack.
func MonitoringPackageIdentity() packages.PackageIdentity {
	return packages.PackageIdentity{
		Repo:    MonitoringRepo,
		Name:    MonitoringPackageName,
		Version: MonitoringVersion,
	}
}

// GenerateManifest returns the YAML manifest for the monitoring package based
// on the backend setting. In uPlot mode, Prometheus is the primary container
// and the UI queries its API directly on port 5308. In Grafana mode, Grafana
// is the primary container on port 5308 with Prometheus running alongside.
//
// Node Exporter runs on the host network as a system service (port 9100),
// so Prometheus reaches it via host.containers.internal (podman's host
// gateway inside the container network).
func GenerateManifest(backend, nodeExporterPort string) string {
	if backend == "" {
		backend = BackendUPlot
	}
	if nodeExporterPort == "" {
		nodeExporterPort = NodeExporterPort
	}

	switch backend {
	case BackendGrafana:
		return grafanaManifest(nodeExporterPort)
	default:
		return uplotManifest(nodeExporterPort)
	}
}

// uplotManifest returns the YAML for the uPlot backend: Prometheus only,
// exposed on port 5308 via the network controller.
func uplotManifest(nodeExporterPort string) string {
	return fmt.Sprintf(`image: %s
description: "System monitoring (Prometheus + Node Exporter)"
command:
  - "--config.file=/etc/prometheus/prometheus.yml"
  - "--storage.tsdb.path=/prometheus"
  - "--storage.tsdb.retention.time=30d"
  - "--web.listen-address=:9090"
environment: {}
network:
  internal:
    "%s": "9090"
volumes:
  config:
    mountpoint: /etc/prometheus
  data:
    mountpoint: /prometheus
templates:
  prometheus-config:
    volume: config
    path: prometheus.yml
    content: |
      global:
        scrape_interval: 15s
        evaluation_interval: 15s
      scrape_configs:
        - job_name: "prometheus"
          static_configs:
            - targets: ["localhost:9090"]
        - job_name: "node-exporter"
          static_configs:
            - targets: ["host.containers.internal:%s"]
`, PrometheusImage, MonitoringExternalPort, nodeExporterPort)
}

// grafanaManifest returns the YAML for the Grafana backend: Grafana exposed
// on port 5308, with Prometheus running on the same container network. The
// Grafana provisioning files are generated separately by WriteGrafanaConfig
// because the dashboard JSON contains Go template-conflicting syntax.
func grafanaManifest(nodeExporterPort string) string {
	return fmt.Sprintf(`image: %s
description: "System monitoring (Grafana + Prometheus + Node Exporter)"
command: []
environment:
  GF_AUTH_ANONYMOUS_ENABLED: "true"
  GF_AUTH_ANONYMOUS_ORG_ROLE: "Viewer"
  GF_SECURITY_ALLOW_EMBEDDING: "true"
  GF_USERS_DEFAULT_THEME: "light"
  GF_SERVER_ENABLE_GZIP: "true"
  GF_SERVER_HTTP_PORT: "3000"
network:
  internal:
    "%s": "3000"
volumes:
  provisioning:
    mountpoint: /etc/grafana/provisioning
  data:
    mountpoint: /var/lib/grafana
  prom-config:
    mountpoint: /prom-config
  prom-data:
    mountpoint: /prom-data
`, GrafanaImage, MonitoringExternalPort)
}

// EnsureMonitoringPackage writes the monitoring package YAML to the _system
// repository directory on disk. The file is always overwritten to ensure the
// manifest matches the running systemcontroller version.
func EnsureMonitoringPackage(repoBase, backend, nodeExporterPort string) error {
	if backend == "" {
		backend = BackendUPlot
	}

	pkgDir := filepath.Join(repoBase, MonitoringRepo, packages.PackagesDir, MonitoringPackageName)
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		return fmt.Errorf("create monitoring package dir: %w", err)
	}

	manifest := GenerateManifest(backend, nodeExporterPort)
	pkgFile := filepath.Join(pkgDir, MonitoringVersion+".yaml")
	if err := os.WriteFile(pkgFile, []byte(manifest), 0600); err != nil {
		return fmt.Errorf("write monitoring package: %w", err)
	}

	return nil
}

// InstallMonitoringPackage registers the monitoring package as installed
// if it is not already. Returns true if the package was newly installed.
func InstallMonitoringPackage(inst packages.Installer) (bool, error) {
	_, installed, err := inst.GetInstalledVersion(MonitoringRepo, MonitoringPackageName)
	if err != nil {
		return false, fmt.Errorf("check monitoring installed: %w", err)
	}
	if installed {
		return false, nil
	}

	if err := inst.Install(MonitoringRepo, MonitoringPackageName, MonitoringPackageName, MonitoringVersion, packages.Responses{}); err != nil {
		return false, fmt.Errorf("install monitoring package: %w", err)
	}
	return true, nil
}

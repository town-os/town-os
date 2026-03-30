package monitoring

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gitea.com/town-os/town-os/src/systemd"
)

// MonitoringUIUnitConfig returns the systemd system service configuration
// for the monitoring dashboard on port 5308. In uPlot mode this is a socat
// forwarder to Prometheus on port 9090; in Grafana mode it runs the full
// Grafana container. The socatImage parameter specifies the container image
// for the uPlot forwarder (typically the NC image which has socat pre-installed).
func MonitoringUIUnitConfig(backend, btrfsBase, socatImage string) systemd.SystemServiceUnitConfig {
	if backend == "" || backend == BackendUPlot {
		return uplotUIUnitConfig(socatImage)
	}
	return grafanaUIUnitConfig(btrfsBase)
}

// uplotUIUnitConfig returns a socat forwarder that exposes port 5308 and
// forwards to Prometheus on port 9090 via the host network. The image should
// have socat pre-installed (e.g. the NC image built at startup).
func uplotUIUnitConfig(socatImage string) systemd.SystemServiceUnitConfig {
	if socatImage == "" {
		socatImage = DefaultSocatImage
	}
	return systemd.SystemServiceUnitConfig{
		Key:         "monitoring-ui",
		Description: "Monitoring UI (uPlot)",
		Image:       socatImage,
		PullNever:   true,
		Args: []string{
			"--net", "host",
		},
		Command: []string{
			"socat", "TCP-LISTEN:" + MonitoringExternalPort + ",fork,reuseaddr", "TCP:127.0.0.1:" + PrometheusPort,
		},
	}
}

// grafanaUIUnitConfig returns the Grafana container configuration with port
// 5308 mapped to Grafana's internal port 3000.
func grafanaUIUnitConfig(btrfsBase string) systemd.SystemServiceUnitConfig {
	provisioningDir := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning")
	dataDir := filepath.Join(btrfsBase, "monitoring", "grafana-data")
	return systemd.SystemServiceUnitConfig{
		Key:         "monitoring-ui",
		Description: "Monitoring UI (Grafana)",
		Image:       GrafanaImage,
		Args: []string{
			"-p", MonitoringExternalPort + ":" + GrafanaPort,
			"-e", "GF_AUTH_ANONYMOUS_ENABLED=true",
			"-e", "GF_AUTH_ANONYMOUS_ORG_ROLE=Viewer",
			"-e", "GF_SECURITY_ALLOW_EMBEDDING=true",
			"-e", "GF_USERS_DEFAULT_THEME=light",
			"-e", "GF_SERVER_ENABLE_GZIP=true",
			"-e", "GF_SERVER_HTTP_PORT=" + GrafanaPort,
			"-v", provisioningDir + ":/etc/grafana/provisioning",
			"-v", dataDir + ":/var/lib/grafana",
		},
		VolumeDirs: []string{provisioningDir, dataDir},
	}
}

// WriteGrafanaProvisioningFiles writes the Grafana provisioning configuration
// (datasource and dashboard provider) to the monitoring directory.
func WriteGrafanaProvisioningFiles(btrfsBase string) error {
	provDir := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning")

	// Datasource directory.
	dsDir := filepath.Join(provDir, "datasources")
	if err := os.MkdirAll(dsDir, 0755); err != nil { //nolint:gosec // must be readable by container process
		return fmt.Errorf("create grafana datasources dir: %w", err)
	}
	dsYAML := GrafanaDatasourceYAML("host.containers.internal")
	if err := os.WriteFile(filepath.Join(dsDir, "prometheus.yml"), []byte(dsYAML), 0644); err != nil { //nolint:gosec // must be readable by container process
		return fmt.Errorf("write grafana datasource: %w", err)
	}

	// Dashboard provider directory.
	dashDir := filepath.Join(provDir, "dashboards")
	if err := os.MkdirAll(dashDir, 0755); err != nil { //nolint:gosec // must be readable by container process
		return fmt.Errorf("create grafana dashboards dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dashDir, "default.yml"), []byte(GrafanaDashboardProviderYAML), 0644); err != nil { //nolint:gosec // must be readable by container process
		return fmt.Errorf("write grafana dashboard provider: %w", err)
	}

	// Dashboard JSON directory (referenced by provider).
	jsonDir := filepath.Join(provDir, "dashboard-json")
	if err := os.MkdirAll(jsonDir, 0755); err != nil { //nolint:gosec // must be readable by container process
		return fmt.Errorf("create grafana dashboard-json dir: %w", err)
	}

	return nil
}

// StartMonitoringUI writes any required configuration, then installs, enables,
// and starts the monitoring UI system service (socat or Grafana). The socatImage
// parameter specifies the container image for the uPlot forwarder.
func StartMonitoringUI(ctx context.Context, sd systemd.Manager, backend, btrfsBase, socatImage string) error {
	if backend == BackendGrafana {
		if err := WriteGrafanaProvisioningFiles(btrfsBase); err != nil {
			return fmt.Errorf("write grafana provisioning: %w", err)
		}
	}

	cfg := MonitoringUIUnitConfig(backend, btrfsBase, socatImage)
	uf := systemd.GenerateSystemServiceUnit(cfg)

	if err := sd.InstallUnit(ctx, uf.Name, uf.Content); err != nil {
		return fmt.Errorf("install monitoring-ui unit: %w", err)
	}
	if err := sd.SetStatus(ctx, uf.Name, systemd.Enable); err != nil {
		return fmt.Errorf("enable monitoring-ui: %w", err)
	}
	if err := sd.SetStatus(ctx, uf.Name, systemd.Restart); err != nil {
		return fmt.Errorf("start monitoring-ui: %w", err)
	}

	return nil
}

// MonitoringUISystemService returns metadata for the monitoring UI system
// service, used by the system services API. The image varies by backend.
func MonitoringUISystemService(backend, socatImage string) SystemService {
	image := socatImage
	if image == "" {
		image = DefaultSocatImage
	}
	if backend == BackendGrafana {
		image = GrafanaImage
	}
	return SystemService{
		Key:         "monitoring-ui",
		DisplayName: "Monitoring UI",
		Image:       image,
		Port:        MonitoringExternalPort,
		UnitName:    systemd.SystemServiceUnitName("monitoring-ui"),
	}
}

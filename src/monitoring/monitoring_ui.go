package monitoring

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

// UPlotPackageConfig returns the PackageUnitConfig for the uPlot socat
// forwarder that exposes port 5308 and forwards to Prometheus on port 9090.
// The NC image is used as both the socat container (it has socat
// pre-installed) and the network controller image. Passing an empty
// ncImage falls back to DefaultSocatImage.
func UPlotPackageConfig(ncImage, networkStatePath string) systemd.PackageUnitConfig {
	if ncImage == "" {
		ncImage = DefaultSocatImage
	}
	return systemd.PackageUnitConfig{
		SystemServiceKey:       "monitoring-ui",
		Description:            "Monitoring UI (uPlot)",
		Image:                  ncImage,
		PullNever:              true,
		External:               packages.PortMap{5308: 5308},
		NetworkControllerImage: ncImage,
		NetworkStatePath:       networkStatePath,
		Command: []string{
			"socat", "TCP-LISTEN:" + MonitoringExternalPort + ",fork,reuseaddr", "TCP:host.containers.internal:" + PrometheusPort,
		},
		RestartAlways:          true,
		StartLimitIntervalZero: true,
	}
}

// GrafanaPackageConfig returns the PackageUnitConfig for Grafana so it gets a
// proper NC, socket units, and private podman network. Only used when the
// monitoring backend is "grafana".
func GrafanaPackageConfig(btrfsBase, ncImage, networkStatePath string) systemd.PackageUnitConfig {
	provisioningDir := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning")
	dataDir := filepath.Join(btrfsBase, "monitoring", "grafana-data")

	return systemd.PackageUnitConfig{
		SystemServiceKey:       "monitoring-ui",
		Description:            "Monitoring UI (Grafana)",
		Image:                  GrafanaImage,
		External:               packages.PortMap{5308: 3000},
		NetworkControllerImage: ncImage,
		NetworkStatePath:       networkStatePath,
		Environment: map[string]string{
			"GF_AUTH_ANONYMOUS_ENABLED":    "true",
			"GF_AUTH_ANONYMOUS_ORG_ROLE":   "Viewer",
			"GF_SECURITY_ALLOW_EMBEDDING": "true",
			"GF_USERS_DEFAULT_THEME":       "light",
			"GF_SERVER_ENABLE_GZIP":        "true",
			"GF_SERVER_HTTP_PORT":          GrafanaPort,
		},
		HostVolumeMounts: []systemd.HostVolumeMount{
			{HostPath: provisioningDir, ContainerPath: "/etc/grafana/provisioning"},
			{HostPath: dataDir, ContainerPath: "/var/lib/grafana"},
		},
		MkdirPaths:             []string{provisioningDir, dataDir},
		RestartAlways:          true,
		StartLimitIntervalZero: true,
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

// StartMonitoringUI installs and starts the monitoring UI service. Both uPlot
// and Grafana modes generate full package units (NC, sockets, private network).
func StartMonitoringUI(ctx context.Context, sd systemd.Manager, backend, btrfsBase, ncImage, networkStatePath string) error {
	var cfg systemd.PackageUnitConfig

	if backend == BackendGrafana {
		if err := WriteGrafanaProvisioningFiles(btrfsBase); err != nil {
			return fmt.Errorf("write grafana provisioning: %w", err)
		}
		cfg = GrafanaPackageConfig(btrfsBase, ncImage, networkStatePath)
	} else {
		cfg = UPlotPackageConfig(ncImage, networkStatePath)
	}

	units := systemd.GeneratePackageUnits(cfg)

	if err := writeMonitoringNetworkState(cfg); err != nil {
		return fmt.Errorf("write monitoring-ui network state: %w", err)
	}

	return installAndStartPackageUnits(ctx, sd, units)
}

// MonitoringUISystemService returns metadata for the monitoring UI system
// service, used by the system services API. The image varies by backend:
// Grafana in grafana mode, or the NC image (which has socat pre-installed)
// in uPlot mode.
func MonitoringUISystemService(backend, ncImage string) SystemService {
	image := ncImage
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

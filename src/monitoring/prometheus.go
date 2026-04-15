package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

// prometheusUID / prometheusGID are the user and group IDs that the
// Prometheus container runs as (upstream image uses "nobody"). The
// bind-mounted data directory must be owned by this uid:gid pair or
// Prometheus aborts at startup with a permission error.
const (
	prometheusUID uint32 = 65534
	prometheusGID uint32 = 65534
)

// prometheusUID32 / prometheusGID32 are mutable copies for addressing
// via HostVolumeMount.UID / HostVolumeMount.GID.
var (
	prometheusUID32 = prometheusUID
	prometheusGID32 = prometheusGID
)

// PrometheusPackageConfig returns the PackageUnitConfig for Prometheus so it
// gets a proper NC, socket units, and private podman network — exactly like a
// regular package. When uPlot is the monitoring backend, port 5308 is exposed
// as a second external port mapping to internal 9090 so the browser can reach
// Prometheus directly without a separate socat forwarder.
func PrometheusPackageConfig(btrfsBase, ncImage, networkStatePath string) systemd.PackageUnitConfig {
	configDir := filepath.Join(btrfsBase, "monitoring", "prometheus-config")
	dataDir := filepath.Join(btrfsBase, "monitoring", "prometheus-data")

	return systemd.PackageUnitConfig{
		SystemServiceKey:       "prometheus",
		Description:            "Prometheus",
		Image:                  PrometheusImage,
		External:               packages.PortMap{9090: 9090},
		NetworkControllerImage: ncImage,
		NetworkStatePath:       networkStatePath,
		Command: []string{
			"--config.file=/etc/prometheus/prometheus.yml",
			"--storage.tsdb.path=/prometheus",
			"--storage.tsdb.retention.time=30d",
			"--web.listen-address=:" + PrometheusPort,
		},
		HostVolumeMounts: []systemd.HostVolumeMount{
			{HostPath: configDir, ContainerPath: "/etc/prometheus", Options: "ro"},
			{HostPath: dataDir, ContainerPath: "/prometheus", UID: &prometheusUID32, GID: &prometheusGID32},
		},
		MkdirPaths:             []string{configDir, dataDir},
		RestartAlways:          true,
		StartLimitIntervalZero: true,
	}
}

// WritePrometheusConfig writes the prometheus.yml configuration file to the
// monitoring config directory under the btrfs base path.
func WritePrometheusConfig(btrfsBase, nodeExporterPort string) error {
	if nodeExporterPort == "" {
		nodeExporterPort = NodeExporterPort
	}
	configDir := filepath.Join(btrfsBase, "monitoring", "prometheus-config")
	if err := os.MkdirAll(configDir, 0755); err != nil { //nolint:gosec // config dir must be readable by container process
		return fmt.Errorf("create prometheus config dir: %w", err)
	}
	config := fmt.Sprintf(`global:
  scrape_interval: 15s
  evaluation_interval: 15s
scrape_configs:
  - job_name: "prometheus"
    static_configs:
      - targets: ["localhost:9090"]
  - job_name: "node-exporter"
    static_configs:
      - targets: ["host.containers.internal:%s"]
`, nodeExporterPort)
	if err := os.WriteFile(filepath.Join(configDir, "prometheus.yml"), []byte(config), 0644); err != nil { //nolint:gosec // config must be readable by container process
		return fmt.Errorf("write prometheus.yml: %w", err)
	}
	return nil
}

// StartPrometheus writes the prometheus config, generates package units (with
// NC and sockets), writes the NC state file, and installs/starts everything.
func StartPrometheus(ctx context.Context, sd systemd.Manager, btrfsBase, nodeExporterPort, ncImage, networkStatePath string) error {
	if err := WritePrometheusConfig(btrfsBase, nodeExporterPort); err != nil {
		return fmt.Errorf("write prometheus config: %w", err)
	}

	cfg := PrometheusPackageConfig(btrfsBase, ncImage, networkStatePath)
	units := systemd.GeneratePackageUnits(cfg)

	// Write the NC state file so the network controller knows which ports
	// to forward via socat.
	if err := writeMonitoringNetworkState(cfg); err != nil {
		return fmt.Errorf("write prometheus network state: %w", err)
	}

	return installAndStartPackageUnits(ctx, sd, units)
}

// PrometheusSystemService returns metadata for the Prometheus system
// service, used by the system services API.
func PrometheusSystemService() SystemService {
	return SystemService{
		Key:         "prometheus",
		DisplayName: "Prometheus",
		Image:       PrometheusImage,
		Port:        PrometheusPort,
		UnitName:    systemd.SystemServiceUnitName("prometheus"),
	}
}

// installAndStartPackageUnits installs all unit files, enables them, and
// starts them (NC first, then service). Shared by monitoring Start functions.
func installAndStartPackageUnits(ctx context.Context, sd systemd.Manager, units systemd.PackageUnits) error {
	// Install all units.
	if err := sd.InstallUnit(ctx, units.Service.Name, units.Service.Content); err != nil {
		return fmt.Errorf("install service unit: %w", err)
	}
	for _, sock := range units.Sockets {
		if err := sd.InstallUnit(ctx, sock.Name, sock.Content); err != nil {
			return fmt.Errorf("install socket %s: %w", sock.Name, err)
		}
	}
	if units.NetworkController != nil {
		if err := sd.InstallUnit(ctx, units.NetworkController.Name, units.NetworkController.Content); err != nil {
			return fmt.Errorf("install NC unit: %w", err)
		}
	}

	// Enable all units.
	for _, sock := range units.Sockets {
		if err := sd.SetStatus(ctx, sock.Name, systemd.Enable); err != nil {
			return fmt.Errorf("enable socket %s: %w", sock.Name, err)
		}
	}
	if units.NetworkController != nil {
		if err := sd.SetStatus(ctx, units.NetworkController.Name, systemd.Enable); err != nil {
			return fmt.Errorf("enable NC: %w", err)
		}
	}
	if err := sd.SetStatus(ctx, units.Service.Name, systemd.Enable); err != nil {
		return fmt.Errorf("enable service: %w", err)
	}

	// Start NC first, then service (NC uses Type=notify so systemd waits
	// for container readiness before starting the service).
	if units.NetworkController != nil {
		if err := sd.SetStatus(ctx, units.NetworkController.Name, systemd.Restart); err != nil {
			return fmt.Errorf("start NC: %w", err)
		}
	}
	if err := sd.SetStatus(ctx, units.Service.Name, systemd.Restart); err != nil {
		return fmt.Errorf("start service: %w", err)
	}

	return nil
}

// writeMonitoringNetworkState writes the per-service NC state file for a
// monitoring service configured via PackageUnitConfig.
func writeMonitoringNetworkState(cfg systemd.PackageUnitConfig) error {
	state := networkcontroller.PackageNetworkState{
		Repo:          "system",
		Package:       cfg.SystemServiceKey,
		Version:       "latest",
		ContainerName: systemd.SystemServiceContainerName(cfg.SystemServiceKey),
	}

	for ext, internal := range cfg.External {
		state.Ports = append(state.Ports, networkcontroller.PortConfig{
			ExternalPort: ext,
			InternalPort: internal,
			Forward:      true,
		})
	}

	sort.Slice(state.Ports, func(i, j int) bool {
		return state.Ports[i].ExternalPort < state.Ports[j].ExternalPort
	})

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal monitoring network state: %w", err)
	}

	if err := os.MkdirAll(cfg.NetworkStatePath, 0700); err != nil {
		return fmt.Errorf("create network state dir: %w", err)
	}

	filePath := fmt.Sprintf("%s/system-%s.json", cfg.NetworkStatePath, cfg.SystemServiceKey)
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("write monitoring network state: %w", err)
	}

	return nil
}

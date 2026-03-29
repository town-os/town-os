package monitoring

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gitea.com/town-os/town-os/src/systemd"
)

// PrometheusUnitConfig returns the systemd system service configuration
// for Prometheus. It runs with port 9090 mapped to the host and bind-mounts
// config and data directories from the monitoring data path.
func PrometheusUnitConfig(btrfsBase string) systemd.SystemServiceUnitConfig {
	configDir := filepath.Join(btrfsBase, "monitoring", "prometheus-config")
	dataDir := filepath.Join(btrfsBase, "monitoring", "prometheus-data")
	return systemd.SystemServiceUnitConfig{
		Key:         "prometheus",
		Description: "Prometheus",
		Image:       PrometheusImage,
		Args: []string{
			"-p", PrometheusPort + ":" + PrometheusPort,
			"-v", configDir + ":/etc/prometheus:ro",
			"-v", dataDir + ":/prometheus",
		},
		Command: []string{
			"--config.file=/etc/prometheus/prometheus.yml",
			"--storage.tsdb.path=/prometheus",
			"--storage.tsdb.retention.time=30d",
			"--web.listen-address=:" + PrometheusPort,
		},
		VolumeDirs: []string{configDir, dataDir},
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

// StartPrometheus writes the prometheus config, installs, enables, and starts
// the Prometheus system service.
func StartPrometheus(ctx context.Context, sd systemd.Manager, btrfsBase, nodeExporterPort string) error {
	if err := WritePrometheusConfig(btrfsBase, nodeExporterPort); err != nil {
		return fmt.Errorf("write prometheus config: %w", err)
	}
	cfg := PrometheusUnitConfig(btrfsBase)
	uf := systemd.GenerateSystemServiceUnit(cfg)

	if err := sd.InstallUnit(ctx, uf.Name, uf.Content); err != nil {
		return fmt.Errorf("install prometheus unit: %w", err)
	}
	if err := sd.SetStatus(ctx, uf.Name, systemd.Enable); err != nil {
		return fmt.Errorf("enable prometheus: %w", err)
	}
	if err := sd.SetStatus(ctx, uf.Name, systemd.Restart); err != nil {
		return fmt.Errorf("start prometheus: %w", err)
	}

	return nil
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

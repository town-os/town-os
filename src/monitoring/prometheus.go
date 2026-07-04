package monitoring

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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

// PrometheusUnitConfig returns the system-service unit config for Prometheus.
// Prometheus runs in the HOST network namespace (--net host) so it can scrape
// node-exporter over the loopback with no cross-podman-network hairpin, and it
// binds 127.0.0.1:9090 so it is private to the host — the browser reaches its
// data only through the monitoring UI forwarder on :5308, never :9090 directly.
func PrometheusUnitConfig(btrfsBase string) systemd.SystemServiceUnitConfig {
	configDir := filepath.Join(btrfsBase, "monitoring", "prometheus-config")
	dataDir := filepath.Join(btrfsBase, "monitoring", "prometheus-data")

	return systemd.SystemServiceUnitConfig{
		Key:         "prometheus",
		Description: "Prometheus",
		Image:       PrometheusImage,
		Args: []string{
			"--net", "host",
			"-v", configDir + ":/etc/prometheus:ro",
			"-v", dataDir + ":/prometheus",
		},
		Command: []string{
			"--config.file=/etc/prometheus/prometheus.yml",
			"--storage.tsdb.path=/prometheus",
			"--storage.tsdb.retention.time=30d",
			"--web.listen-address=127.0.0.1:" + PrometheusPort,
		},
		VolumeDirs: []string{configDir, dataDir},
		// Prometheus's data dir must be owned by its uid:gid (bind mounts pass
		// host ownership straight through). Non-recursive: the container creates
		// its own children as uid 65534, so only the top level needs fixing.
		ExecStartPre: []string{
			fmt.Sprintf("/bin/chown %d:%d %s", prometheusUID, prometheusGID, dataDir),
		},
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
	// Both targets are on the host loopback: Prometheus and node-exporter run
	// --net host, so localhost reaches each other with no podman-network hop.
	config := fmt.Sprintf(`global:
  scrape_interval: 15s
  evaluation_interval: 15s
scrape_configs:
  - job_name: "prometheus"
    static_configs:
      - targets: ["localhost:9090"]
  - job_name: "node-exporter"
    static_configs:
      - targets: ["localhost:%s"]
`, nodeExporterPort)
	if err := os.WriteFile(filepath.Join(configDir, "prometheus.yml"), []byte(config), 0644); err != nil { //nolint:gosec // config must be readable by container process
		return fmt.Errorf("write prometheus.yml: %w", err)
	}
	return nil
}

// StartPrometheus writes the prometheus config and installs/starts the
// host-networked Prometheus system-service unit.
func StartPrometheus(ctx context.Context, sd systemd.Manager, btrfsBase, nodeExporterPort string) error {
	if err := WritePrometheusConfig(btrfsBase, nodeExporterPort); err != nil {
		return fmt.Errorf("write prometheus config: %w", err)
	}
	uf := systemd.GenerateSystemServiceUnit(PrometheusUnitConfig(btrfsBase))
	return installAndStartSystemServiceUnit(ctx, sd, uf)
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

// installAndStartSystemServiceUnit installs, enables, and (re)starts a single
// system-service unit. Shared by the monitoring Start functions now that all
// three services (Prometheus, node-exporter, monitoring UI) run as plain
// host-networked system services with no network controller or socket units.
func installAndStartSystemServiceUnit(ctx context.Context, sd systemd.Manager, uf systemd.UnitFile) error {
	if err := sd.InstallUnit(ctx, uf.Name, uf.Content); err != nil {
		return fmt.Errorf("install unit %s: %w", uf.Name, err)
	}
	if err := sd.SetStatus(ctx, uf.Name, systemd.Enable); err != nil {
		return fmt.Errorf("enable unit %s: %w", uf.Name, err)
	}
	if err := sd.SetStatus(ctx, uf.Name, systemd.Restart); err != nil {
		return fmt.Errorf("start unit %s: %w", uf.Name, err)
	}
	return nil
}

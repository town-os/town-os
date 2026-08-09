package monitoring

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitea.com/town-os/town-os/src/systemd"
)

// RolodexJobName is the Prometheus job label carried by every rolodex series.
// It is a constant because it is what any dashboard or alert selecting rolodex
// metrics has to name; changing it orphans every one of them.
const RolodexJobName = "rolodex"

// ControllerJobName is the Prometheus job label carried by every system
// controller series, and is a constant for the same reason RolodexJobName is.
const ControllerJobName = "systemcontroller"

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
// binds its port on 127.0.0.1 so it is private to the host — the browser
// reaches its data only through the monitoring UI forwarder, never directly.
func PrometheusUnitConfig(btrfsBase string, ports Ports) systemd.SystemServiceUnitConfig {
	ports = ports.withDefaults()
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
			"--web.listen-address=127.0.0.1:" + ports.Prometheus,
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
func WritePrometheusConfig(btrfsBase string, ports Ports) error {
	ports = ports.withDefaults()
	configDir := filepath.Join(btrfsBase, "monitoring", "prometheus-config")
	if err := os.MkdirAll(configDir, 0755); err != nil { //nolint:gosec // config dir must be readable by container process
		return fmt.Errorf("create prometheus config dir: %w", err)
	}
	// Every target is on the host loopback: Prometheus, node-exporter, and
	// rolodex all run --net host, so they reach each other with no
	// podman-network hop. rolodex answers on DNSLoopback (127.0.0.2) rather
	// than localhost, which is why its target is passed in as a full address.
	var b strings.Builder
	fmt.Fprintf(&b, `global:
  scrape_interval: 15s
  evaluation_interval: 15s
scrape_configs:
  - job_name: "prometheus"
    static_configs:
      - targets: ["localhost:%s"]
  - job_name: "node-exporter"
    static_configs:
      - targets: ["localhost:%s"]
`, ports.Prometheus, ports.NodeExporter)
	// Omitted rather than pointed at a default when unset: a job aimed at a
	// guessed address would sit permanently down and read as a broken rolodex
	// instead of an unconfigured one.
	if ports.RolodexMetrics != "" {
		fmt.Fprintf(&b, `  - job_name: %q
    static_configs:
      - targets: [%q]
`, RolodexJobName, ports.RolodexMetrics)
	}
	// Same omit-rather-than-guess rule as rolodex above.
	if ports.ControllerMetrics != "" {
		fmt.Fprintf(&b, `  - job_name: %q
`, ControllerJobName)
		if ports.ControllerMetricsScheme == "https" {
			// The controller's leaf is issued by the box's own CA, which this
			// Prometheus has no reason to trust and no clean way to be handed
			// (the CA file lives on a btrfs path the container does not mount).
			// Skipping verification is sound for this target specifically: it is
			// a loopback scrape inside the host namespace, so there is no
			// network path for anything else to answer as it.
			b.WriteString("    scheme: https\n    tls_config:\n      insecure_skip_verify: true\n")
		}
		fmt.Fprintf(&b, `    static_configs:
      - targets: [%q]
`, ports.ControllerMetrics)
	}
	if err := os.WriteFile(filepath.Join(configDir, "prometheus.yml"), []byte(b.String()), 0644); err != nil { //nolint:gosec // config must be readable by container process
		return fmt.Errorf("write prometheus.yml: %w", err)
	}
	return nil
}

// StartPrometheus writes the prometheus config and installs/starts the
// host-networked Prometheus system-service unit.
func StartPrometheus(ctx context.Context, sd systemd.Manager, btrfsBase string, ports Ports) error {
	if err := WritePrometheusConfig(btrfsBase, ports); err != nil {
		return fmt.Errorf("write prometheus config: %w", err)
	}
	uf := systemd.GenerateSystemServiceUnit(PrometheusUnitConfig(btrfsBase, ports))
	return installAndStartSystemServiceUnit(ctx, sd, uf)
}

// PrometheusSystemService returns metadata for the Prometheus system
// service, used by the system services API.
func PrometheusSystemService(ports Ports) SystemService {
	return SystemService{
		Key:         "prometheus",
		DisplayName: "Prometheus",
		Image:       PrometheusImage,
		Port:        ports.withDefaults().Prometheus,
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

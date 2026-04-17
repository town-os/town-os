package monitoring

import (
	"context"
	"fmt"

	"gitea.com/town-os/town-os/src/systemd"
)

// DiskstatsDeviceExclude is the regex passed to node_exporter's
// `--collector.diskstats.device-exclude` flag. The upstream default
// (`^(ram|loop|fd|(h|s|v|xv)d[a-z]|nvme\d+n\d+p)\d+$`) filters out
// partitions (e.g. `sda3`, `nvme0n1p3`) and loop devices, which is
// exactly where Town OS's btrfs filesystem actually lives — so the
// Disk I/O dashboard ends up querying metrics that were never emitted
// and renders as an empty chart. We narrow the exclusion to just the
// genuinely noisy pseudo-devices (`ram*`, `fd*`) and let every real
// block device through. The PromQL queries still scope to the btrfs
// device list, so the extra series cost a little scrape bandwidth and
// nothing else.
const DiskstatsDeviceExclude = `^(ram|fd)\d+$`

// NodeExporterUnitConfig returns the systemd system service configuration
// for Node Exporter. It runs with host networking, PID namespace, and
// SYS_TIME capability to collect host-level metrics.
func NodeExporterUnitConfig(port string) systemd.SystemServiceUnitConfig {
	if port == "" {
		port = NodeExporterPort
	}
	return systemd.SystemServiceUnitConfig{
		Key:         "node-exporter",
		Description: "Node Exporter",
		Image:       NodeExporterImage,
		Args: []string{
			"--net", "host",
			"--pid", "host",
			"--cap-add", "SYS_TIME",
			"-v", "/:/host:ro,rslave",
		},
		Command: []string{
			"--path.rootfs=/host",
			"--web.listen-address=:" + port,
			"--collector.diskstats.device-exclude=" + DiskstatsDeviceExclude,
		},
	}
}

// StartNodeExporter installs and starts the Node Exporter system service.
// Node Exporter requires host networking and cannot run inside a podman
// network, so it remains a system service rather than a package.
func StartNodeExporter(ctx context.Context, sd systemd.Manager, port string) error {
	cfg := NodeExporterUnitConfig(port)
	uf := systemd.GenerateSystemServiceUnit(cfg)

	if err := sd.InstallUnit(ctx, uf.Name, uf.Content); err != nil {
		return fmt.Errorf("install node-exporter unit: %w", err)
	}
	if err := sd.SetStatus(ctx, uf.Name, systemd.Enable); err != nil {
		return fmt.Errorf("enable node-exporter: %w", err)
	}
	if err := sd.SetStatus(ctx, uf.Name, systemd.Restart); err != nil {
		return fmt.Errorf("start node-exporter: %w", err)
	}

	return nil
}

// NodeExporterSystemService returns metadata for the Node Exporter system
// service, used by the system services API.
func NodeExporterSystemService(port string) SystemService {
	if port == "" {
		port = NodeExporterPort
	}
	return SystemService{
		Key:         "node-exporter",
		DisplayName: "Node Exporter",
		Image:       NodeExporterImage,
		Port:        port,
		UnitName:    systemd.SystemServiceUnitName("node-exporter"),
	}
}

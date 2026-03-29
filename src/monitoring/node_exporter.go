package monitoring

import (
	"context"
	"fmt"

	"gitea.com/town-os/town-os/src/systemd"
)

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

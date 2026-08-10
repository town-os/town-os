package monitoring

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// grafanaUID and grafanaGID are the user and group ids that the Grafana
// container runs as. The bind-mounted data directory must be owned by
// this uid:gid pair or Grafana aborts at startup with
// "GF_PATHS_DATA='/var/lib/grafana' is not writable".
const (
	grafanaUID = 472
	grafanaGID = 472
)

// UPlotUnitConfig returns the system-service unit config for the uPlot socat
// forwarder. It runs in the HOST network namespace so it can reach the
// loopback-only Prometheus with no cross-podman-network hairpin. socat binds
// the single LAN-exposed monitoring port (where the browser queries the
// Prometheus API) and forwards to Prometheus.
// The NC image is reused as the socat container (it has socat pre-installed);
// an empty ncImage falls back to DefaultSocatImage.
func UPlotUnitConfig(ncImage string, ports Ports) systemd.SystemServiceUnitConfig {
	if ncImage == "" {
		ncImage = DefaultSocatImage
	}
	ports = ports.withDefaults()
	return systemd.SystemServiceUnitConfig{
		Key:         "monitoring-ui",
		Description: "Monitoring UI (uPlot)",
		Image:       ncImage,
		PullNever:   true,
		Args:        []string{"--net", "host"},
		Command: []string{
			"socat", "TCP-LISTEN:" + ports.External + ",fork,reuseaddr", "TCP:127.0.0.1:" + ports.Prometheus,
		},
	}
}

// GrafanaUnitConfig returns the system-service unit config for Grafana. Only
// used when the monitoring backend is "grafana". Like the uPlot forwarder it
// runs in the HOST network namespace: Grafana listens directly on the single
// LAN-exposed monitoring port (GF_SERVER_HTTP_PORT) and its datasource reaches
// the loopback-only Prometheus on 127.0.0.1.
func GrafanaUnitConfig(btrfsBase string, ports Ports) systemd.SystemServiceUnitConfig {
	ports = ports.withDefaults()
	provisioningDir := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning")
	dataDir := filepath.Join(btrfsBase, "monitoring", "grafana-data")

	return systemd.SystemServiceUnitConfig{
		Key:         "monitoring-ui",
		Description: "Monitoring UI (Grafana)",
		Image:       GrafanaImage,
		Args: []string{
			"--net", "host",
			"-e", "GF_AUTH_ANONYMOUS_ENABLED=true",
			"-e", "GF_AUTH_ANONYMOUS_ORG_ROLE=Viewer",
			"-e", "GF_SECURITY_ALLOW_EMBEDDING=true",
			"-e", "GF_USERS_DEFAULT_THEME=light",
			"-e", "GF_SERVER_ENABLE_GZIP=true",
			// Grafana binds the external port directly (host netns), replacing
			// the old -p 5308:3000 publish now that there is no podman network.
			"-e", "GF_SERVER_HTTP_PORT=" + ports.External,
			// Provisioning files are read-only from Grafana's perspective;
			// 0755/0644 perms set in WriteGrafanaProvisioningFiles let uid
			// 472 read them without owning them, so no chown is needed.
			"-v", provisioningDir + ":/etc/grafana/provisioning",
			"-v", dataDir + ":/var/lib/grafana",
		},
		VolumeDirs: []string{provisioningDir, dataDir},
		// Grafana writes plugins, sessions, and grafana.db into
		// /var/lib/grafana, so it must own the top of this bind-mount. The
		// chown is non-recursive: Grafana creates subdirectories itself as
		// uid 472, so they stay correctly owned without any walk.
		ExecStartPre: []string{
			fmt.Sprintf("/bin/chown %d:%d %s", grafanaUID, grafanaGID, dataDir),
		},
	}
}

// grafanaStoragePaths returns the (relative name, absolute path) pairs
// for Grafana's data and provisioning directories under btrfsBase. Kept
// as a function (not a var) so tests and callers stay in sync.
func grafanaStoragePaths(btrfsBase string) [][2]string {
	return [][2]string{
		{filepath.Join("monitoring", "grafana-data"), filepath.Join(btrfsBase, "monitoring", "grafana-data")},
		{filepath.Join("monitoring", "grafana-provisioning"), filepath.Join(btrfsBase, "monitoring", "grafana-provisioning")},
	}
}

// EnsureGrafanaStorage ensures Grafana's data and provisioning directories
// exist on disk. It first tries to create each directory as a btrfs
// subvolume (idempotent: existing paths are left alone), then falls
// back to a plain directory when subvolume creation fails — for
// example when the parent "monitoring" directory already exists as a
// non-btrfs directory (which it does on the live system, because
// StartPrometheus creates it via ExecStartPre mkdir before
// StartMonitoringUI runs).
//
// Ownership is not set here. The generated monitoring-ui systemd unit emits a
// single non-recursive ExecStartPre=/bin/chown of the grafana-data mount to
// uid:gid 472 (see GrafanaUnitConfig), and that is the single source of
// ownership for the bind-mounted data path. This function only has to make
// sure the paths exist.
func EnsureGrafanaStorage(st storage.Storage, btrfsBase string) error {
	for _, p := range grafanaStoragePaths(btrfsBase) {
		name, dir := p[0], p[1]
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			// Prefer a btrfs subvolume. Fall back to a plain directory
			// if that fails for any reason (parent not btrfs, parent
			// already exists as plain dir, nil storage in tests).
			subvolErr := errors.New("storage unavailable")
			if st != nil {
				subvolErr = st.CreateFilesystem(storage.Filesystem{Name: name})
			}
			if subvolErr != nil {
				slog.Debug("grafana subvolume creation failed, falling back to plain directory", "path", dir, "error", subvolErr)
				if mkErr := os.MkdirAll(dir, 0755); mkErr != nil { //nolint:gosec // must be traversable by the grafana container
					return fmt.Errorf("create grafana dir %s: %w", dir, mkErr)
				}
			}
		} else if err != nil {
			return fmt.Errorf("stat %s: %w", dir, err)
		}
	}
	return nil
}

// WriteGrafanaProvisioningFiles writes the Grafana provisioning configuration
// (datasource and dashboard provider) to the monitoring directory. The
// Disk I/O panel of the generated dashboard is parameterised on
// diskDevices, the kernel device basenames backing the btrfs filesystem
// at /town-os; pass nil to render the panel with a sentinel regex that
// matches nothing.
func WriteGrafanaProvisioningFiles(btrfsBase string, diskDevices []string, ports Ports) error {
	ports = ports.withDefaults()
	provDir := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning")

	// Datasource directory.
	dsDir := filepath.Join(provDir, "datasources")
	if err := os.MkdirAll(dsDir, 0755); err != nil { //nolint:gosec // must be readable by container process
		return fmt.Errorf("create grafana datasources dir: %w", err)
	}
	// Grafana runs --net host, so it reaches the loopback-only Prometheus
	// on 127.0.0.1 (not the old host.containers.internal gateway hop).
	dsYAML := GrafanaDatasourceYAML("127.0.0.1", ports.Prometheus)
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

	// Dashboard JSON directory (referenced by provider). The provider
	// points at the directory, so every file in it is picked up: adding a
	// dashboard is an entry in GrafanaDashboards and nothing here.
	jsonDir := filepath.Join(provDir, "dashboard-json")
	if err := os.MkdirAll(jsonDir, 0755); err != nil { //nolint:gosec // must be readable by container process
		return fmt.Errorf("create grafana dashboard-json dir: %w", err)
	}
	for _, dash := range GrafanaDashboards(diskDevices) {
		if err := os.WriteFile(filepath.Join(jsonDir, dash.Filename), []byte(dash.JSON), 0644); err != nil { //nolint:gosec // must be readable by container process
			return fmt.Errorf("write grafana dashboard %s: %w", dash.Filename, err)
		}
	}

	return nil
}

// StartMonitoringUI installs and starts the monitoring UI service as a plain
// host-networked system service (no NC, no socket units, no private network).
// In Grafana mode, the data and provisioning directories are created (btrfs
// subvolume where possible, plain directory fallback otherwise) and the
// provisioning files are written; ownership of the data tree is then set by the
// non-recursive ExecStartPre chown emitted by the generated unit.
func StartMonitoringUI(ctx context.Context, sd systemd.Manager, st storage.Storage, backend, btrfsBase, ncImage string, diskDevices []string, ports Ports) error {
	var cfg systemd.SystemServiceUnitConfig

	if backend == BackendGrafana {
		if err := EnsureGrafanaStorage(st, btrfsBase); err != nil {
			return fmt.Errorf("ensure grafana storage: %w", err)
		}
		if err := WriteGrafanaProvisioningFiles(btrfsBase, diskDevices, ports); err != nil {
			return fmt.Errorf("write grafana provisioning: %w", err)
		}
		cfg = GrafanaUnitConfig(btrfsBase, ports)
	} else {
		cfg = UPlotUnitConfig(ncImage, ports)
	}

	return installAndStartSystemServiceUnit(ctx, sd, systemd.GenerateSystemServiceUnit(cfg))
}

// MonitoringUISystemService returns metadata for the monitoring UI system
// service, used by the system services API. The image varies by backend:
// Grafana in grafana mode, or the NC image (which has socat pre-installed)
// in uPlot mode.
func MonitoringUISystemService(backend, ncImage string, ports Ports) SystemService {
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
		Port:        ports.withDefaults().External,
		UnitName:    systemd.SystemServiceUnitName("monitoring-ui"),
	}
}

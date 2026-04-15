package monitoring

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gitea.com/town-os/town-os/src/packages"
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

// grafanaUID32 / grafanaGID32 are pointer-friendly copies for embedding
// in HostVolumeMount.UID / HostVolumeMount.GID, which the unit generator
// turns into an ExecStartPre chown for the bind-mounted host path.
var (
	grafanaUID32 uint32 = grafanaUID
	grafanaGID32 uint32 = grafanaGID
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
			// Provisioning files are read-only from Grafana's
			// perspective; 0755/0644 perms set in
			// WriteGrafanaProvisioningFiles let uid 472 read them
			// without owning them, so no chown is needed.
			{HostPath: provisioningDir, ContainerPath: "/etc/grafana/provisioning"},
			// Grafana writes plugins, sessions, and grafana.db
			// into /var/lib/grafana, so it must own the top of
			// this bind-mount. The chown is non-recursive:
			// Grafana creates subdirectories itself as uid 472,
			// so they stay correctly owned without any walk.
			{HostPath: dataDir, ContainerPath: "/var/lib/grafana", UID: &grafanaUID32, GID: &grafanaGID32},
		},
		MkdirPaths:             []string{provisioningDir, dataDir},
		RestartAlways:          true,
		StartLimitIntervalZero: true,
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
// Ownership is not set here. The generated monitoring-ui systemd unit
// emits an ExecStartPre=/bin/chown -R for every HostVolumeMount whose
// UID/GID fields are set (see GrafanaPackageConfig), and that is the
// single declarative source of ownership for the bind-mounted host
// paths. This function only has to make sure the paths exist.
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
func WriteGrafanaProvisioningFiles(btrfsBase string, diskDevices []string) error {
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
	if err := os.WriteFile(filepath.Join(jsonDir, "town-os-overview.json"), []byte(TownOSOverviewDashboard(diskDevices)), 0644); err != nil { //nolint:gosec // must be readable by container process
		return fmt.Errorf("write town-os overview dashboard: %w", err)
	}

	return nil
}

// StartMonitoringUI installs and starts the monitoring UI service. Both uPlot
// and Grafana modes generate full package units (NC, sockets, private network).
// In Grafana mode, the data and provisioning directories are created (btrfs
// subvolume where possible, plain directory fallback otherwise) and the
// provisioning files are written; ownership of both trees is then set by the
// ExecStartPre chowns emitted by the generated unit from the HostVolumeMount
// UID/GID fields.
func StartMonitoringUI(ctx context.Context, sd systemd.Manager, st storage.Storage, backend, btrfsBase, ncImage, networkStatePath string, diskDevices []string) error {
	var cfg systemd.PackageUnitConfig

	if backend == BackendGrafana {
		if err := EnsureGrafanaStorage(st, btrfsBase); err != nil {
			return fmt.Errorf("ensure grafana storage: %w", err)
		}
		if err := WriteGrafanaProvisioningFiles(btrfsBase, diskDevices); err != nil {
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

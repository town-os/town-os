package monitoring

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

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

// grafanaUIDStr / grafanaGIDStr render the ids as strings for embedding
// in systemd unit ExecStartPre shell commands.
var (
	grafanaUIDStr = strconv.Itoa(grafanaUID)
	grafanaGIDStr = strconv.Itoa(grafanaGID)
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
		MkdirPaths: []string{provisioningDir, dataDir},
		ExecStartPreExtra: []string{
			"/bin/chown -R " + grafanaUIDStr + ":" + grafanaGIDStr + " " + dataDir,
			"/bin/chown -R " + grafanaUIDStr + ":" + grafanaGIDStr + " " + provisioningDir,
		},
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
// exist and are owned by the Grafana uid:gid. It first tries to create
// each directory as a btrfs subvolume (idempotent: existing paths are
// left alone), then falls back to a plain directory when subvolume
// creation fails — for example when the parent "monitoring" directory
// already exists as a non-btrfs directory (which it does on the live
// system, because StartPrometheus creates it via ExecStartPre mkdir
// before StartMonitoringUI runs). Finally, every entry under the path
// is chowned to the Grafana uid:gid so that Grafana (uid 472) can
// create its plugins/, logs/, and sessions/ subdirectories at startup.
//
// This is the self-heal path: regardless of how a previous boot left
// the directories (wrong owner, plain dir, stale subvolume), running
// this function and then restarting the unit brings Grafana back up.
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
		if err := chownGrafanaTree(dir); err != nil {
			return fmt.Errorf("chown grafana dir %s: %w", dir, err)
		}
	}
	return nil
}

// chownGrafanaTree recursively sets uid:gid=472:472 on every entry
// under path. The function is best-effort: per-entry chown failures
// are logged and skipped so a test context (running as non-root)
// does not fail the surrounding EnsureGrafanaStorage call. In
// production the systemcontroller runs as root inside its container
// and the chown succeeds; the ExecStartPre chown in the generated
// unit is the authoritative fix that runs on every service start.
func chownGrafanaTree(path string) error {
	walkErr := filepath.WalkDir(path, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		//nolint:gosec // G122 -- path comes from filepath.WalkDir of a
		// systemcontroller-owned directory (monitoring/grafana-*), not
		// from user input; the chown target is the Grafana uid which
		// cannot be abused via symlink TOCTOU at this privilege level.
		if chownErr := os.Lchown(p, grafanaUID, grafanaGID); chownErr != nil {
			slog.Debug("chown grafana entry", "path", p, "error", chownErr)
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, os.ErrNotExist) {
		return walkErr
	}
	return nil
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
	if err := os.WriteFile(filepath.Join(jsonDir, "town-os-overview.json"), []byte(TownOSOverviewDashboard), 0644); err != nil { //nolint:gosec // must be readable by container process
		return fmt.Errorf("write town-os overview dashboard: %w", err)
	}

	return nil
}

// StartMonitoringUI installs and starts the monitoring UI service. Both uPlot
// and Grafana modes generate full package units (NC, sockets, private network).
// In Grafana mode, Grafana's data and provisioning directories are ensured
// (btrfs subvolume where possible, plain directory fallback otherwise) and
// proactively chowned to the Grafana uid before the unit is installed, so
// a previously-broken boot (wrong owner, restart loop) self-heals as soon
// as the new systemcontroller runs.
func StartMonitoringUI(ctx context.Context, sd systemd.Manager, st storage.Storage, backend, btrfsBase, ncImage, networkStatePath string) error {
	var cfg systemd.PackageUnitConfig

	if backend == BackendGrafana {
		if err := EnsureGrafanaStorage(st, btrfsBase); err != nil {
			return fmt.Errorf("ensure grafana storage: %w", err)
		}
		if err := WriteGrafanaProvisioningFiles(btrfsBase); err != nil {
			return fmt.Errorf("write grafana provisioning: %w", err)
		}
		// Re-chown after writing provisioning files (os.WriteFile
		// resets ownership to the systemcontroller's uid).
		if err := chownGrafanaTree(filepath.Join(btrfsBase, "monitoring", "grafana-provisioning")); err != nil {
			return fmt.Errorf("chown grafana provisioning: %w", err)
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

package systemcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// ReconcileConfig holds the dependencies needed to reconcile installed packages
// on startup. Each field mirrors what the system controller uses at runtime.
type ReconcileConfig struct {
	Installer                packages.Installer
	RepositoryRoot           *packages.RepositoryRoot
	Storage                  storage.Storage
	Systemd                  systemd.Manager
	SettingsMgr              account.SettingsManager
	BtrfsBasePath            string
	NetworkControllerBinPath string
	NetworkStatePath         string
	NetworkMode              string
}

// reconcileDefaultQuota returns the system-wide default quota in bytes from the
// settings manager. Returns 0 if unconfigured.
func reconcileDefaultQuota(mgr account.SettingsManager) uint64 {
	if mgr == nil {
		return 0
	}

	val, err := mgr.Get("default_quota")
	if err != nil {
		return 0
	}

	q, err := strconv.ParseUint(val, 10, 64)
	if err != nil {
		return 0
	}

	return q
}

// Reconcile iterates all installed packages and reconstructs their runtime
// state: compiles each package with its persisted responses, ensures storage
// volumes exist, and installs/starts the corresponding systemd unit.
// Disabled packages have their unit installed but are not started.
//
// This is idempotent and safe to call on every startup. It allows the
// system controller to recover installed-package state after a container
// restart where the filesystem (repos, installed symlinks, responses) is
// persisted but systemd units are not.
func Reconcile(ctx context.Context, cfg ReconcileConfig) error {
	// Ensure root subvolumes exist for volume management.
	if cfg.Storage != nil {
		for _, root := range []string{PackagesVolumePrefix, UninstalledVolumePrefix} {
			if err := cfg.Storage.CreateFilesystem(storage.Filesystem{Name: root}); err != nil {
				slog.Debug(fmt.Sprintf("reconcile: create root volume %s: %v", root, err))
			}
		}
	}

	installed, err := cfg.Installer.ListInstalled()
	if err != nil {
		return fmt.Errorf("reconcile: list installed: %w", err)
	}

	if len(installed) == 0 {
		return nil
	}

	// Group by repo/name, pick only the latest version of each package.
	// Since only one systemd unit per repo/package-name can be active, we
	// reconcile only the highest version per repo+name pair.
	type repoNameKey struct{ repo, name string }
	byRepoName := map[repoNameKey]packages.PackageIdentity{}
	for _, identity := range installed {
		pi, err := packages.ParsePackageIdentity(identity)
		if err != nil {
			slog.Error(fmt.Sprintf("reconcile: parse %q: %v", identity, err))
			continue
		}

		key := repoNameKey{pi.Repo, pi.Name}
		existing, ok := byRepoName[key]
		if !ok || packages.CompareVersions(pi.Version, existing.Version) > 0 {
			byRepoName[key] = pi
		}
	}

	defQuota := reconcileDefaultQuota(cfg.SettingsMgr)

	for _, pi := range byRepoName {
		identity := pi.String()
		if err := reconcilePackage(ctx, cfg, pi, defQuota); err != nil {
			slog.Error(fmt.Sprintf("reconcile: %s: %v", identity, err))
			continue
		}

		slog.Info(fmt.Sprintf("reconcile: restored %s", identity))
	}

	return nil
}

// reconcilePackage restores a single installed package: compiles it with its
// persisted responses, ensures volumes, and installs+starts the systemd units.
func reconcilePackage(ctx context.Context, cfg ReconcileConfig, pi packages.PackageIdentity, defQuota uint64) error {
	repoName := pi.Repo

	ip, err := cfg.RepositoryRoot.LoadPackage(repoName, pi.Name, pi.Version)
	if err != nil {
		return fmt.Errorf("load package: %w", err)
	}

	responses, err := cfg.Installer.GetResponses(repoName, pi.Name, pi.Version)
	if err != nil {
		return fmt.Errorf("get responses: %w", err)
	}

	compiled, err := ip.Compile(responses)
	if err != nil {
		return fmt.Errorf("compile: %w", err)
	}

	if cfg.Storage != nil {
		for volName, vol := range compiled.Volumes {
			quota := vol.Quota
			if quota == 0 {
				quota = defQuota
			}
			fsName := fmt.Sprintf("%s/%s/%s/%s/%s", PackagesVolumePrefix, repoName, pi.Name, pi.Version, volName)
			if err := cfg.Storage.CreateFilesystem(storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
				if err := cfg.Storage.ModifyFilesystem(fsName, storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
					return fmt.Errorf("storage volume %s: %w", fsName, err)
				}
			}
		}
	}

	// Write per-package network state file.
	if cfg.NetworkStatePath != "" && len(compiled.Network.External) > 0 {
		if err := reconcileWriteNetworkState(cfg, repoName, pi.Name, pi.Version, compiled); err != nil {
			return fmt.Errorf("write network state: %w", err)
		}
	}

	if cfg.Systemd != nil {
		unitCfg := systemd.PackageUnitConfig{
			RepoName:                 repoName,
			PkgName:                  pi.Name,
			Version:                  pi.Version,
			Image:                    compiled.Image,
			Command:                  compiled.Command,
			Environment:              compiled.Environment,
			External:                 compiled.Network.External,
			Internal:                 compiled.Network.Internal,
			Volumes:                  compiled.Volumes,
			BtrfsBase:                cfg.BtrfsBasePath,
			NetworkControllerBinPath: cfg.NetworkControllerBinPath,
			NetworkStatePath:         cfg.NetworkStatePath,
			NetworkMode:              cfg.NetworkMode,
		}
		units := systemd.GeneratePackageUnits(unitCfg)

		// Install all unit files.
		if err := cfg.Systemd.InstallUnit(ctx, units.Service.Name, units.Service.Content); err != nil {
			return fmt.Errorf("install service unit: %w", err)
		}
		for _, sock := range units.Sockets {
			if err := cfg.Systemd.InstallUnit(ctx, sock.Name, sock.Content); err != nil {
				return fmt.Errorf("install socket unit %s: %w", sock.Name, err)
			}
		}
		if units.NetworkController != nil {
			if err := cfg.Systemd.InstallUnit(ctx, units.NetworkController.Name, units.NetworkController.Content); err != nil {
				return fmt.Errorf("install network controller unit: %w", err)
			}
		}

		// Enable socket and network controller units.
		for _, sock := range units.Sockets {
			if err := cfg.Systemd.SetStatus(ctx, sock.Name, systemd.Enable); err != nil {
				return fmt.Errorf("enable socket %s: %w", sock.Name, err)
			}
		}
		if units.NetworkController != nil {
			if err := cfg.Systemd.SetStatus(ctx, units.NetworkController.Name, systemd.Enable); err != nil {
				return fmt.Errorf("enable network controller: %w", err)
			}
		}

		disabled, err := cfg.Installer.IsDisabled(repoName, pi.Name)
		if err != nil {
			return fmt.Errorf("check disabled: %w", err)
		}
		if !disabled {
			if err := cfg.Systemd.SetStatus(ctx, units.Service.Name, systemd.Start); err != nil {
				return fmt.Errorf("start unit: %w", err)
			}
		}
	}

	return nil
}

// reconcileWriteNetworkState writes the per-package network state file during
// reconciliation.
func reconcileWriteNetworkState(cfg ReconcileConfig, repoName, pkgName, version string, compiled *packages.Package) error {
	state := networkcontroller.PackageNetworkState{
		Repo:        repoName,
		Package:     pkgName,
		Version:     version,
		NetworkMode: cfg.NetworkMode,
	}

	for ext, int_ := range compiled.Network.External {
		forward := cfg.NetworkMode == "host" && ext != int_
		state.Ports = append(state.Ports, networkcontroller.PortConfig{
			ExternalPort: ext,
			InternalPort: int_,
			UPnP:         true,
			Forward:      forward,
		})
	}

	sort.Slice(state.Ports, func(i, j int) bool {
		return state.Ports[i].ExternalPort < state.Ports[j].ExternalPort
	})

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal network state: %w", err)
	}

	if err := os.MkdirAll(cfg.NetworkStatePath, 0755); err != nil {
		return fmt.Errorf("create network state dir: %w", err)
	}

	filePath := fmt.Sprintf("%s/%s-%s-%s.json", cfg.NetworkStatePath, repoName, pkgName, version)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("write network state: %w", err)
	}

	return nil
}

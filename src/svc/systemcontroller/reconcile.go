package systemcontroller

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// ReconcileConfig holds the dependencies needed to reconcile installed packages
// on startup. Each field mirrors what the system controller uses at runtime.
type ReconcileConfig struct {
	Installer      packages.Installer
	RepositoryRoot *packages.RepositoryRoot
	Storage        storage.Storage
	Systemd        systemd.Manager
	SettingsMgr    account.SettingsManager
	BtrfsBasePath  string
	UPnPBinPath    string
	NetworkMode    string
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

	if cfg.Systemd != nil {
		unitCfg := systemd.PackageUnitConfig{
			RepoName:    repoName,
			PkgName:     pi.Name,
			Version:     pi.Version,
			Image:       compiled.Image,
			Command:     compiled.Command,
			Environment: compiled.Environment,
			External:    compiled.Network.External,
			Internal:    compiled.Network.Internal,
			Volumes:     compiled.Volumes,
			BtrfsBase:   cfg.BtrfsBasePath,
			UPnPBinPath: cfg.UPnPBinPath,
			NetworkMode: cfg.NetworkMode,
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
		for _, fwd := range units.Forwarders {
			if err := cfg.Systemd.InstallUnit(ctx, fwd.Name, fwd.Content); err != nil {
				return fmt.Errorf("install forwarder unit %s: %w", fwd.Name, err)
			}
		}
		if units.UPnPService != nil {
			if err := cfg.Systemd.InstallUnit(ctx, units.UPnPService.Name, units.UPnPService.Content); err != nil {
				return fmt.Errorf("install upnp service unit: %w", err)
			}
		}
		if units.UPnPTimer != nil {
			if err := cfg.Systemd.InstallUnit(ctx, units.UPnPTimer.Name, units.UPnPTimer.Content); err != nil {
				return fmt.Errorf("install upnp timer unit: %w", err)
			}
		}

		// Enable socket, forwarder, and timer units.
		for _, sock := range units.Sockets {
			if err := cfg.Systemd.SetStatus(ctx, sock.Name, systemd.Enable); err != nil {
				return fmt.Errorf("enable socket %s: %w", sock.Name, err)
			}
		}
		for _, fwd := range units.Forwarders {
			if err := cfg.Systemd.SetStatus(ctx, fwd.Name, systemd.Enable); err != nil {
				return fmt.Errorf("enable forwarder %s: %w", fwd.Name, err)
			}
		}
		if units.UPnPTimer != nil {
			if err := cfg.Systemd.SetStatus(ctx, units.UPnPTimer.Name, systemd.Enable); err != nil {
				return fmt.Errorf("enable upnp timer: %w", err)
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

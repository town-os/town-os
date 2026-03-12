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
	PagesManager             account.PagesManager
	BtrfsBasePath            string
	NetworkControllerBinPath string
	NetworkStatePath         string
	NetworkMode              string
	CaddyImage               string
	CaddyPort                string
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
		for _, root := range []string{PackagesVolumePrefix, UninstalledVolumePrefix, ArchivesSubvolume, PagesVolumePrefix, VMImagesSubvolume, UserVolumePrefix} {
			if err := cfg.Storage.CreateFilesystem(storage.Filesystem{Name: root}); err != nil {
				slog.Debug(fmt.Sprintf("reconcile: create root volume %s: %v", root, err))
			}
		}
	}

	installed, err := cfg.Installer.ListInstalled()
	if err != nil {
		return fmt.Errorf("reconcile: list installed: %w", err)
	}

	if len(installed) > 0 {
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

			slog.Info("reconcile: restored " + identity)
		}
	}

	// Reconcile pages: ensure subvolumes, symlinks, and the Caddy unit.
	if cfg.PagesManager != nil {
		if err := reconcilePages(ctx, cfg); err != nil {
			slog.Error(fmt.Sprintf("reconcile: pages: %v", err))
		}
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

		// Auto-archive: if the package defines archives, extract data into
		// empty volumes during reconciliation.
		if len(ip.Archives) > 0 {
			for _, archive := range ip.Archives {
				volPath := fmt.Sprintf("%s/%s/%s/%s/%s", PackagesVolumePrefix, repoName, pi.Name, pi.Version, archive.Volume)
				targetPath := fmt.Sprintf("%s/%s", cfg.BtrfsBasePath, volPath)
				entries, err := os.ReadDir(targetPath)
				if err != nil || len(entries) > 0 {
					continue
				}
				if err := reconcileExtractFromImage(ctx, archive.Image, archive.Directory, targetPath); err != nil {
					slog.Debug(fmt.Sprintf("reconcile auto-archive %s -> %s: %v", archive.Image, archive.Volume, err))
				}
			}
		}

		// Proton app extraction: extract from app_image into empty volumes.
		if compiled.Proton != nil {
			volPath := fmt.Sprintf("%s/%s/%s/%s/%s", PackagesVolumePrefix, repoName, pi.Name, pi.Version, compiled.Proton.Volume)
			targetPath := fmt.Sprintf("%s/%s", cfg.BtrfsBasePath, volPath)
			entries, err := os.ReadDir(targetPath)
			if err == nil && len(entries) == 0 {
				if err := reconcileExtractFromImage(ctx, compiled.Proton.AppImage, compiled.Proton.AppDirectory, targetPath); err != nil {
					slog.Debug(fmt.Sprintf("reconcile proton app-extract %s -> %s: %v", compiled.Proton.AppImage, compiled.Proton.Volume, err))
				}
			}
		}

		// Git seed: clone git repositories into empty volumes.
		for volName, vol := range compiled.Volumes {
			if vol.Git == "" {
				continue
			}
			volPath := fmt.Sprintf("%s/%s/%s/%s/%s", PackagesVolumePrefix, repoName, pi.Name, pi.Version, volName)
			targetPath := fmt.Sprintf("%s/%s", cfg.BtrfsBasePath, volPath)
			entries, err := os.ReadDir(targetPath)
			if err != nil || len(entries) > 0 {
				continue
			}
			if err := gitCloneIntoPath(ctx, vol.Git, targetPath); err != nil {
				slog.Debug(fmt.Sprintf("reconcile git-seed %s -> %s: %v", vol.Git, volName, err))
			}
		}

		// Apply file templates after volume seeding.
		if len(compiled.Templates) > 0 {
			reconcileApplyTemplates(cfg, compiled, responses, pi, ip.Description)
		}
	}

	// Write per-package network state file.
	needsNetworkState := len(compiled.Network.External) > 0
	if !needsNetworkState && cfg.NetworkMode == "host" {
		for intHost, intContainer := range compiled.Network.Internal {
			if intHost != intContainer {
				needsNetworkState = true
				break
			}
		}
	}
	if cfg.NetworkStatePath != "" && needsNetworkState {
		if err := reconcileWriteNetworkState(cfg, repoName, pi.Name, pi.Version, compiled); err != nil {
			return fmt.Errorf("write network state: %w", err)
		}
	}

	if cfg.Systemd != nil {
		image := compiled.Image
		if image == "" && compiled.Proton != nil {
			image = reconcileProtonImage(cfg.SettingsMgr)
		}
		unitCfg := systemd.PackageUnitConfig{
			RepoName:                 repoName,
			PkgName:                  pi.Name,
			Version:                  pi.Version,
			Image:                    image,
			Command:                  compiled.Command,
			Environment:              compiled.Environment,
			External:                 compiled.Network.External,
			Internal:                 compiled.Network.Internal,
			Volumes:                  compiled.Volumes,
			BtrfsBase:                cfg.BtrfsBasePath,
			NetworkControllerBinPath: cfg.NetworkControllerBinPath,
			NetworkStatePath:         cfg.NetworkStatePath,
			NetworkMode:              cfg.NetworkMode,
			Runtime:                  compiled.Runtime,
			VM:                       compiled.VM,
		}
		if compiled.Runtime == packages.RuntimeVM && compiled.VM != nil {
			unitCfg.VMImagePath = resolveVMImagePath(cfg.BtrfsBasePath, compiled.VM.Image)
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

// reconcileProtonImage returns the system-wide proton runner image from the
// settings manager. Returns an empty string if not configured.
func reconcileProtonImage(mgr account.SettingsManager) string {
	if mgr == nil {
		return ""
	}

	val, err := mgr.Get("proton_image")
	if err != nil {
		return ""
	}

	return val
}

// reconcileWriteNetworkState writes the per-package network state file during
// reconciliation. Dependencies (names containing --dep--) always have UPnP
// disabled to keep their ports local-only.
func reconcileWriteNetworkState(cfg ReconcileConfig, repoName, pkgName, version string, compiled *packages.Package) error {
	isDep := packages.IsDependency(pkgName)

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
			UPnP:         !isDep,
			Forward:      forward,
		})
	}

	for intHost, intContainer := range compiled.Network.Internal {
		if cfg.NetworkMode == "host" && intHost != intContainer {
			state.Ports = append(state.Ports, networkcontroller.PortConfig{
				ExternalPort: intHost,
				InternalPort: intContainer,
				UPnP:         false,
				Forward:      true,
			})
		}
	}

	sort.Slice(state.Ports, func(i, j int) bool {
		return state.Ports[i].ExternalPort < state.Ports[j].ExternalPort
	})

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal network state: %w", err)
	}

	if err := os.MkdirAll(cfg.NetworkStatePath, 0700); err != nil {
		return fmt.Errorf("create network state dir: %w", err)
	}

	filePath := fmt.Sprintf("%s/%s-%s-%s.json", cfg.NetworkStatePath, repoName, pkgName, version)
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("write network state: %w", err)
	}

	return nil
}

// reconcilePages ensures all existing pages have btrfs subvolumes, symlinks,
// and that the Caddy web server unit is installed and started.
func reconcilePages(ctx context.Context, cfg ReconcileConfig) error {
	if err := EnsurePagesWebroot(cfg.BtrfsBasePath); err != nil {
		return fmt.Errorf("ensure pages webroot: %w", err)
	}

	pages, err := cfg.PagesManager.List()
	if err != nil {
		return fmt.Errorf("list pages: %w", err)
	}

	for _, page := range pages {
		// Ensure subvolume exists.
		if cfg.Storage != nil {
			fsName := PagesVolumePrefix + "/" + page.Name
			if err := cfg.Storage.CreateFilesystem(storage.Filesystem{Name: fsName}); err != nil {
				slog.Debug(fmt.Sprintf("reconcile: pages subvolume %s: %v", page.Name, err))
			}
		}

		// Ensure symlink exists.
		if err := EnsurePageSymlink(cfg.BtrfsBasePath, page.Name); err != nil {
			slog.Debug(fmt.Sprintf("reconcile: pages symlink %s: %v", page.Name, err))
		}
	}

	// Write Caddyfile and install the Caddy unit.
	caddyPort := cfg.CaddyPort
	if caddyPort == "" {
		caddyPort = DefaultCaddyPort
	}

	if _, err := WriteCaddyfile(cfg.BtrfsBasePath, caddyPort); err != nil {
		return fmt.Errorf("write Caddyfile: %w", err)
	}

	if cfg.Systemd != nil {
		caddyImage := cfg.CaddyImage
		if caddyImage == "" {
			caddyImage = DefaultCaddyImage
		}

		unit := GeneratePagesUnit(PagesUnitConfig{
			BtrfsBasePath: cfg.BtrfsBasePath,
			CaddyImage:    caddyImage,
			CaddyPort:     caddyPort,
		})

		if err := cfg.Systemd.InstallUnit(ctx, unit.Name, unit.Content); err != nil {
			return fmt.Errorf("install pages unit: %w", err)
		}

		if err := cfg.Systemd.SetStatus(ctx, unit.Name, systemd.Start); err != nil {
			return fmt.Errorf("start pages unit: %w", err)
		}
	}

	return nil
}

// reconcileApplyTemplates renders Go text/template files into volume
// directories during reconciliation. Uses the same logic as the install
// flow: existing files are not overwritten.
func reconcileApplyTemplates(cfg ReconcileConfig, compiled *packages.Package, responses packages.Responses, pi packages.PackageIdentity, description string) {
	data := packages.TemplateData{
		Responses: responses,
		Package: packages.TemplatePackageInfo{
			Name:        pi.Name,
			Version:     pi.Version,
			Repo:        pi.Repo,
			Image:       compiled.Image,
			Description: description,
		},
		System: packages.TemplateSystemInfo{},
	}

	hostname, err := os.Hostname()
	if err == nil {
		data.System.Hostname = hostname
	}

	if err := packages.ApplyPackageTemplates(compiled.Templates, data, func(volName string) string {
		return fmt.Sprintf("%s/%s/%s/%s/%s/%s", cfg.BtrfsBasePath, PackagesVolumePrefix, pi.Repo, pi.Name, pi.Version, volName)
	}); err != nil {
		slog.Debug(fmt.Sprintf("reconcile apply templates %s: %v", pi.String(), err))
	}
}

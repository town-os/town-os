package systemcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"sort"
	"strconv"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// ReconcileConfig holds the dependencies needed to reconcile installed packages
// on startup. Each field mirrors what the system controller uses at runtime.
type ReconcileConfig struct {
	Installer              packages.Installer
	RepositoryRoot         *packages.RepositoryRoot
	Storage                storage.Storage
	Systemd                systemd.Manager
	SettingsMgr            account.SettingsManager
	PagesManager           account.PagesManager
	BtrfsBasePath          string
	NetworkControllerImage string
	NetworkStatePath       string
	CaddyImage             string
	CaddyPort              string
	ExternalIP             string
	InternalIP             string
	VersionChanged         bool // true when the systemcontroller version differs from last run

	// PostUpdateExec executes a shell command inside a running container.
	// Called after version-change restarts for packages with post_update commands.
	// The function receives the container name and the shell command string.
	// nil means post-update execution is disabled.
	PostUpdateExec func(ctx context.Context, containerName string, command string) error
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

		// Batch-load all dependency records upfront so the per-package
		// loop does not issue N separate I/O calls.
		allDeps := map[repoNameKey]map[string]packages.DependencyRecord{}
		for _, pi := range byRepoName {
			if packages.IsDependency(pi.Name) {
				continue
			}
			deps, loadErr := cfg.Installer.LoadDependencies(pi.Repo, pi.Name)
			if loadErr == nil && len(deps) > 0 {
				allDeps[repoNameKey{pi.Repo, pi.Name}] = deps
			}
		}

		var changedUnits reconcileChangedUnits

		for _, pi := range byRepoName {
			identity := pi.String()

			var netInfo reconcilePackageNetworkInfo
			if packages.IsDependency(pi.Name) {
				// Dependency: find the parent's network to join.
				parentName := packages.ParentName(pi.Name)
				parentKey := repoNameKey{pi.Repo, parentName}
				if parentPI, ok := byRepoName[parentKey]; ok {
					netInfo.ParentNetwork = systemd.NetworkName(pi.Repo, parentName, parentPI.Version)
					netInfo.ParentUnitName = systemd.UnitName(pi.Repo, parentName, parentPI.Version)
					netInfo.ParentNCUnitName = systemd.NetworkControllerUnitName(pi.Repo, parentName, parentPI.Version)
				}
			} else if deps := allDeps[repoNameKey{pi.Repo, pi.Name}]; len(deps) > 0 {
				// Parent: collect dependency unit names for ordering.
				for _, rec := range deps {
					netInfo.DependencyUnitNames = append(netInfo.DependencyUnitNames, systemd.UnitName(rec.Repo, rec.EffectiveName, rec.Version))
				}
				sort.Strings(netInfo.DependencyUnitNames)
			}

			// Pass dependency records so parent packages can rebuild
			// TOWNOS_DEP_* env vars and resolve @dep_KEY_host@ templates.
			var depRecs map[string]packages.DependencyRecord
			if !packages.IsDependency(pi.Name) {
				depRecs = allDeps[repoNameKey{pi.Repo, pi.Name}]
			}

			if err := reconcilePackage(ctx, cfg, pi, defQuota, netInfo, depRecs, &changedUnits); err != nil {
				slog.Error(fmt.Sprintf("reconcile: %s: %v", identity, err))
				continue
			}

			slog.Info("reconcile: restored " + identity)
		}

		// When the systemcontroller version changed, restart all units whose
		// content differs from what was on disk. Order: NC first (owns
		// networks), then dependencies, then parent/standalone services.
		if cfg.VersionChanged {
			for _, name := range changedUnits.nc {
				slog.Info("reconcile: restarting NC " + name)
				if err := cfg.Systemd.SetStatus(ctx, name, systemd.Restart); err != nil {
					slog.Error(fmt.Sprintf("reconcile: restart NC %s: %v", name, err))
				}
			}
			for _, name := range changedUnits.deps {
				slog.Info("reconcile: restarting dependency " + name)
				if err := cfg.Systemd.SetStatus(ctx, name, systemd.Restart); err != nil {
					slog.Error(fmt.Sprintf("reconcile: restart dep %s: %v", name, err))
				}
			}
			for _, name := range changedUnits.services {
				slog.Info("reconcile: restarting service " + name)
				if err := cfg.Systemd.SetStatus(ctx, name, systemd.Restart); err != nil {
					slog.Error(fmt.Sprintf("reconcile: restart service %s: %v", name, err))
				}
			}

			// Execute post-update commands for packages that had changed units.
			if cfg.PostUpdateExec != nil {
				for _, pu := range changedUnits.postUpdates {
					for _, cmd := range pu.commands {
						slog.Info(fmt.Sprintf("reconcile: post-update exec %s: %s", pu.containerName, cmd))
						if err := cfg.PostUpdateExec(ctx, pu.containerName, cmd); err != nil {
							slog.Error(fmt.Sprintf("reconcile: post-update exec %s failed: %v", pu.containerName, err))
						}
					}
				}
			}
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

// reconcilePackageNetworkInfo carries shared-network and systemd ordering
// information computed by the Reconcile loop and passed to reconcilePackage.
type reconcilePackageNetworkInfo struct {
	// ParentNetwork is the podman network for deps to join (empty for parents).
	ParentNetwork string
	// ParentUnitName is the parent systemd unit name (empty for parents).
	ParentUnitName string
	// ParentNCUnitName is the parent's NC unit name (empty for parents).
	// Dependencies add After= for this so the network exists before they start.
	ParentNCUnitName string
	// DependencyUnitNames lists dep service unit names (empty for deps).
	DependencyUnitNames []string
}

// reconcilePackage restores a single installed package: compiles it with its
// persisted responses, ensures volumes, and installs+starts the systemd units.
// depRecs holds dependency records for parent packages (nil for dependencies).
func reconcilePackage(ctx context.Context, cfg ReconcileConfig, pi packages.PackageIdentity, defQuota uint64, netInfo reconcilePackageNetworkInfo, depRecs map[string]packages.DependencyRecord, changedUnits *reconcileChangedUnits) error {
	repoName := pi.Repo

	ip, err := cfg.RepositoryRoot.LoadPackage(repoName, pi.Name, pi.Version)
	if err != nil && packages.IsDependency(pi.Name) {
		// Dependencies are installed under an effective name that differs
		// from the source package name. Fall back to the installed directory
		// where the YAML is stored as a hard link.
		ip, err = cfg.RepositoryRoot.LoadInstalledPackage(repoName, pi.Name, pi.Version)
	}
	if err != nil {
		return fmt.Errorf("load package: %w", err)
	}

	responses, err := cfg.Installer.GetResponses(repoName, pi.Name, pi.Version)
	if err != nil {
		return fmt.Errorf("get responses: %w", err)
	}

	tld := reconcileDNSTLD(cfg.SettingsMgr)
	compiled, err := ip.CompileWithContext(responses, packages.CompileContext{
		ExternalHost: cfg.ExternalIP,
		InternalHost: cfg.InternalIP,
		PackageDNS:   pi.Name + "." + repoName + "." + tld,
	})
	if err != nil {
		return fmt.Errorf("compile: %w", err)
	}

	// Rebuild dependency environment variables for parent packages so the
	// generated unit includes TOWNOS_DEP_* vars and @dep_KEY_host@ templates
	// are resolved. Without this, reconcile would drop dep env vars.
	if len(depRecs) > 0 {
		depEnvVars := buildDepEnvVarsFromRecords(depRecs, cfg.RepositoryRoot, cfg.Installer, cfg.SettingsMgr, cfg.ExternalIP, cfg.InternalIP)
		if len(depEnvVars) > 0 {
			if compiled.Environment == nil {
				compiled.Environment = map[string]string{}
			}
			maps.Copy(compiled.Environment, depEnvVars)
			applyDepTemplates(compiled.Environment, depEnvVars)
		}
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

	// Write per-package network state file. Only parent/standalone packages
	// need a state file — dependencies share the parent's NC.
	isDep := netInfo.ParentNCUnitName != ""
	needsNetworkState := !isDep && (len(compiled.Network.External) > 0 || len(compiled.Network.Internal) > 0)
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
			RepoName:               repoName,
			PkgName:                pi.Name,
			Version:                pi.Version,
			Description:            ip.Description,
			Image:                  image,
			Command:                compiled.Command,
			Environment:            compiled.Environment,
			External:               compiled.Network.External,
			Internal:               compiled.Network.Internal,
			Volumes:                compiled.Volumes,
			BtrfsBase:              cfg.BtrfsBasePath,
			NetworkControllerImage: cfg.NetworkControllerImage,
			NetworkStatePath:       cfg.NetworkStatePath,
			Runtime:                compiled.Runtime,
			VM:                     compiled.VM,
		}
		if compiled.Runtime == packages.RuntimeVM && compiled.VM != nil {
			unitCfg.VMImagePath = resolveVMImagePath(cfg.BtrfsBasePath, compiled.VM.Image)
		}

		// Apply shared-network and systemd ordering info.
		unitCfg.ParentNetwork = netInfo.ParentNetwork
		unitCfg.ParentUnitName = netInfo.ParentUnitName
		unitCfg.ParentNCUnitName = netInfo.ParentNCUnitName
		unitCfg.DependencyUnitNames = netInfo.DependencyUnitNames

		units := systemd.GeneratePackageUnits(unitCfg)

		// Install all unit files, tracking which ones changed.
		changed := installUnitIfChanged(ctx, cfg.Systemd, units.Service.Name, units.Service.Content)
		for _, sock := range units.Sockets {
			installUnitIfChanged(ctx, cfg.Systemd, sock.Name, sock.Content)
		}
		if units.NetworkController != nil {
			if installUnitIfChanged(ctx, cfg.Systemd, units.NetworkController.Name, units.NetworkController.Content) {
				changedUnits.nc = append(changedUnits.nc, units.NetworkController.Name)
			}
		}
		if changed {
			if packages.IsDependency(pi.Name) {
				changedUnits.deps = append(changedUnits.deps, units.Service.Name)
			} else {
				changedUnits.services = append(changedUnits.services, units.Service.Name)
			}

			// Track post-update commands for container packages with changed units.
			if len(compiled.PostUpdate) > 0 && compiled.Runtime == packages.RuntimeContainer {
				changedUnits.postUpdates = append(changedUnits.postUpdates, reconcilePostUpdate{
					containerName: systemd.ContainerName(repoName, pi.Name, pi.Version),
					commands:      compiled.PostUpdate,
				})
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
			// Start the NC before the service to avoid races where the
			// service's ExecStartPre waits for the NC container.
			if units.NetworkController != nil {
				if err := cfg.Systemd.SetStatus(ctx, units.NetworkController.Name, systemd.Start); err != nil {
					slog.Warn("network controller failed to start during reconcile", "unit", units.NetworkController.Name, "error", err)
				}
			}
			if err := cfg.Systemd.SetStatus(ctx, units.Service.Name, systemd.Start); err != nil {
				return fmt.Errorf("start unit: %w", err)
			}
		}
	}

	return nil
}

// reconcilePostUpdate tracks a container and its post-update commands.
type reconcilePostUpdate struct {
	containerName string
	commands      []string
}

// reconcileChangedUnits tracks units whose content changed during reconcile.
type reconcileChangedUnits struct {
	nc          []string              // network controller units (restart first)
	deps        []string              // dependency service units (restart second)
	services    []string              // parent/standalone service units (restart last)
	postUpdates []reconcilePostUpdate // post-update commands for changed container packages
}

// installUnitIfChanged installs a unit file and returns true if the content
// differs from what was previously on disk.
func installUnitIfChanged(ctx context.Context, sd systemd.Manager, name, content string) bool {
	existing, err := sd.ReadUnit(name)
	changed := err != nil || existing != content
	if err := sd.InstallUnit(ctx, name, content); err != nil {
		slog.Error(fmt.Sprintf("reconcile: install unit %s: %v", name, err))
	}
	return changed
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

// reconcileDNSTLD returns the current TLD from settings, defaulting to "home".
func reconcileDNSTLD(mgr account.SettingsManager) string {
	if mgr == nil {
		return "home"
	}

	val, err := mgr.Get("dns_tld")
	if err != nil || val == "" {
		return "home"
	}

	return val
}

// collectInstalledDNSInfo returns DNS info for all installed packages using
// the given installer and repository root.
func collectInstalledDNSInfo(inst packages.Installer, rr *packages.RepositoryRoot) []rolodex.PackageDNSInfo {
	if inst == nil {
		return nil
	}

	installed, err := inst.ListInstalled()
	if err != nil {
		return nil
	}

	var pkgs []rolodex.PackageDNSInfo
	for _, pkg := range installed {
		pi, err := packages.ParsePackageIdentity(pkg)
		if err != nil {
			continue
		}

		var domains []string
		if rr != nil {
			ip, err := rr.LoadPackage(pi.Repo, pi.Name, pi.Version)
			if err == nil {
				domains = ip.Network.Domains
			}
		}

		pkgs = append(pkgs, rolodex.PackageDNSInfo{
			Repo:    pi.Repo,
			Name:    pi.Name,
			Domains: domains,
		})
	}

	return pkgs
}

// ReconcileDNSConfig holds the dependencies needed to reconcile DNS state
// on startup.
type ReconcileDNSConfig struct {
	Client         rolodex.Client
	Installer      packages.Installer
	RepositoryRoot *packages.RepositoryRoot
	SettingsMgr    account.SettingsManager
	InternalIP     string
}

// ReconcileDNS sets up the TLD zone and registers DNS records for all
// installed packages. Errors for individual packages are logged and skipped.
func ReconcileDNS(ctx context.Context, cfg ReconcileDNSConfig) error {
	tld := reconcileDNSTLD(cfg.SettingsMgr)

	if err := rolodex.SetupTLD(ctx, cfg.Client, tld, cfg.InternalIP, ""); err != nil {
		return fmt.Errorf("setup TLD %s: %w", tld, err)
	}

	pkgs := collectInstalledDNSInfo(cfg.Installer, cfg.RepositoryRoot)
	for _, pkg := range pkgs {
		if err := rolodex.RegisterPackageDNS(ctx, cfg.Client, pkg.Repo, pkg.Name, tld, cfg.InternalIP, "", pkg.Domains); err != nil {
			slog.Debug(fmt.Sprintf("reconcile DNS %s/%s: %v", pkg.Repo, pkg.Name, err))
			continue
		}
	}

	return nil
}

// reconcileWriteNetworkState writes the per-package network state file during
// reconciliation. Dependencies (names containing --dep--) always have UPnP
// disabled to keep their ports local-only.
func reconcileWriteNetworkState(cfg ReconcileConfig, repoName, pkgName, version string, compiled *packages.Package) error {
	isDep := packages.IsDependency(pkgName)

	state := networkcontroller.PackageNetworkState{
		Repo:          repoName,
		Package:       pkgName,
		Version:       version,
		ContainerName: systemd.ContainerName(repoName, pkgName, version),
	}

	// All ports get Forward=true — the NC handles all host port exposure
	// via socat from the host to the package container's private network IP.
	for ext, int_ := range compiled.Network.External {
		state.Ports = append(state.Ports, networkcontroller.PortConfig{
			ExternalPort: ext,
			InternalPort: int_,
			UPnP:         !isDep,
			Forward:      true,
		})
	}

	for intHost, intContainer := range compiled.Network.Internal {
		state.Ports = append(state.Ports, networkcontroller.PortConfig{
			ExternalPort: intHost,
			InternalPort: intContainer,
			UPnP:         false,
			Forward:      true,
		})
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

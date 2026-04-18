package systemcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
	upstream "gitea.com/town-os/rolodex-dns/go"
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

	// TLSCA is the local CA used to mint per-package leaf certs for HTTP
	// endpoints. nil disables TLS termination entirely; the reconciler
	// still writes state files and generates units, they just don't carry
	// any TLS=true ports.
	TLSCA *townostls.CA

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
		for _, root := range []string{PackagesVolumePrefix, UninstalledVolumePrefix, ArchivesSubvolume, PagesVolumePrefix, VMImagesSubvolume, UserVolumePrefix, TLSSubvolume} {
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

		// Phase 1: install and enable every package's units in parallel.
		// Slow operations (volume seeding, image extraction, git clone,
		// unit install) dominate boot time, and each package is
		// independent until its systemd Start fires. Pending start
		// records are collected for phase 2.
		const maxConcurrent = 4
		sem := make(chan struct{}, maxConcurrent)
		var wg sync.WaitGroup
		var startsMu sync.Mutex
		var pendingStarts []*pendingPackageStart

		for _, pi := range byRepoName {
			wg.Add(1)
			go func(pi packages.PackageIdentity) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

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
					netInfo.NetworkAlias = packages.DepKey(pi.Name)
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

				pending, err := reconcilePackage(ctx, cfg, pi, defQuota, netInfo, depRecs, &changedUnits)
				if err != nil {
					slog.Error(fmt.Sprintf("reconcile: %s: %v", identity, err))
					return
				}
				if pending != nil {
					startsMu.Lock()
					pendingStarts = append(pendingStarts, pending)
					startsMu.Unlock()
				}

				slog.Info("reconcile: restored " + identity)
			}(pi)
		}
		wg.Wait()

		// Phase 2: start every package's NC + service unit in parallel.
		// All unit files are now on disk, so systemd's After=/Wants=
		// directives will serialize the actual boot order between
		// dependent packages — concurrent Start calls are safe.
		var startWG sync.WaitGroup
		for _, ps := range pendingStarts {
			startWG.Add(1)
			go func(ps *pendingPackageStart) {
				defer startWG.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				// Start the NC before the service to avoid races where
				// the service's ExecStartPre waits for the NC container.
				if ps.ncUnit != "" {
					if err := cfg.Systemd.SetStatus(ctx, ps.ncUnit, systemd.Start); err != nil {
						slog.Warn("network controller failed to start during reconcile", "unit", ps.ncUnit, "error", err)
					}
				}
				if err := cfg.Systemd.SetStatus(ctx, ps.serviceUnit, systemd.Start); err != nil {
					slog.Error(fmt.Sprintf("reconcile: start %s: %v", ps.serviceUnit, err))
				}
			}(ps)
		}
		startWG.Wait()

		// When the systemcontroller version changed, restart ALL package
		// units — not just those whose unit content changed. Container
		// images may have been updated even when the systemd unit text is
		// identical. Order: NC first (owns networks), then dependencies,
		// then parent/standalone services.
		if cfg.VersionChanged {
			for _, name := range changedUnits.allNc {
				slog.Info("reconcile: restarting NC " + name)
				if err := cfg.Systemd.SetStatus(ctx, name, systemd.Restart); err != nil {
					slog.Error(fmt.Sprintf("reconcile: restart NC %s: %v", name, err))
				}
			}
			for _, name := range changedUnits.allDeps {
				slog.Info("reconcile: restarting dependency " + name)
				if err := cfg.Systemd.SetStatus(ctx, name, systemd.Restart); err != nil {
					slog.Error(fmt.Sprintf("reconcile: restart dep %s: %v", name, err))
				}
			}
			for _, name := range changedUnits.allServices {
				slog.Info("reconcile: restarting service " + name)
				if err := cfg.Systemd.SetStatus(ctx, name, systemd.Restart); err != nil {
					slog.Error(fmt.Sprintf("reconcile: restart service %s: %v", name, err))
				}
			}

			// Execute post-update commands for all container packages.
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
	// NetworkAlias is the short dep-key hostname this dep's container
	// should respond to on the shared network (empty for parents).
	NetworkAlias string
}

// reconcilePackage restores a single installed package: compiles it with
// its persisted responses, ensures volumes, and installs+enables (but
// does not start) the systemd units. It returns a pendingPackageStart
// describing what to start in phase 2 of the reconcile loop, or nil when
// there is nothing to start (package disabled or no systemd manager).
// depRecs holds dependency records for parent packages (nil for deps).
func reconcilePackage(ctx context.Context, cfg ReconcileConfig, pi packages.PackageIdentity, defQuota uint64, netInfo reconcilePackageNetworkInfo, depRecs map[string]packages.DependencyRecord, changedUnits *reconcileChangedUnits) (*pendingPackageStart, error) {
	repoName := pi.Repo

	ip, err := cfg.RepositoryRoot.LoadPackage(repoName, pi.Name, pi.Version)
	if err != nil && packages.IsDependency(pi.Name) {
		// Dependencies are installed under an effective name that differs
		// from the source package name. Fall back to the installed directory
		// where the YAML is stored as a hard link.
		ip, err = cfg.RepositoryRoot.LoadInstalledPackage(repoName, pi.Name, pi.Version)
	}
	if err != nil {
		return nil, fmt.Errorf("load package: %w", err)
	}

	responses, err := cfg.Installer.GetResponses(repoName, pi.Name, pi.Version)
	if err != nil {
		return nil, fmt.Errorf("get responses: %w", err)
	}

	tld := reconcileDNSTLD(cfg.SettingsMgr)
	compiled, err := ip.CompileWithContext(responses, packages.CompileContext{
		ExternalHost: cfg.ExternalIP,
		InternalHost: cfg.InternalIP,
		PackageDNS:   pi.Name + "." + repoName + "." + tld,
	})
	if err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}

	// Rebuild dependency environment variables for parent packages so the
	// generated unit includes TOWNOS_DEP_* vars and @dep_KEY_host@ templates
	// are resolved. Without this, reconcile would drop dep env vars. The
	// parallel depMap is plumbed into reconcileApplyTemplates below so any
	// missing file templates re-render with .Dep populated.
	var depMap map[string]packages.TemplateDep
	var depEnvVars map[string]string
	if len(depRecs) > 0 {
		envVars, deps := buildDepEnvVarsFromRecords(depRecs, cfg.RepositoryRoot, cfg.Installer, cfg.SettingsMgr, cfg.ExternalIP, cfg.InternalIP)
		depMap = deps
		depEnvVars = envVars
		if len(depEnvVars) > 0 {
			if compiled.Environment == nil {
				compiled.Environment = map[string]string{}
			}
			maps.Copy(compiled.Environment, depEnvVars)
		}
	}
	// applyDepTemplates also collapses `@@` escapes, so run it even when
	// there are no dep env vars — otherwise Environment values that use
	// `@@` would render with the literal `@@` in the final unit.
	if len(compiled.Environment) > 0 {
		applyDepTemplates(compiled.Environment, depEnvVars)
	}

	if cfg.Storage != nil {
		for volName, vol := range compiled.Volumes {
			quota := vol.Quota
			if quota == 0 {
				quota = defQuota
			}
			fsName := packageVolumePath(repoName, pi.Name, pi.Version, volName)
			if err := cfg.Storage.CreateFilesystem(storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
				if err := cfg.Storage.ModifyFilesystem(fsName, storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
					return nil, fmt.Errorf("storage volume %s: %w", fsName, err)
				}
			}
		}

		// Auto-archive: if the package defines archives, extract data into
		// empty volumes during reconciliation.
		if len(ip.Archives) > 0 {
			for _, archive := range ip.Archives {
				volPath := packageVolumePath(repoName, pi.Name, pi.Version, archive.Volume)
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

		// Proton app extraction (no-op when proton is not built in).
		reconcileProtonApp(ctx, compiled, cfg.BtrfsBasePath, repoName, pi.Name, pi.Version)

		// Git seed: clone git repositories into empty volumes.
		for volName, vol := range compiled.Volumes {
			if vol.Git == "" {
				continue
			}
			volPath := packageVolumePath(repoName, pi.Name, pi.Version, volName)
			targetPath := fmt.Sprintf("%s/%s", cfg.BtrfsBasePath, volPath)
			entries, err := os.ReadDir(targetPath)
			if err != nil || len(entries) > 0 {
				continue
			}
			if err := gitCloneIntoPath(ctx, vol.Git, targetPath); err != nil {
				slog.Debug(fmt.Sprintf("reconcile git-seed %s -> %s: %v", vol.Git, volName, err))
			}
		}

		// Apply file templates after volume seeding. depMap (built above
		// from dep records) is threaded in so any missing file templates
		// re-render with .Dep populated just like install-time.
		if len(compiled.Templates) > 0 {
			reconcileApplyTemplates(cfg, compiled, responses, pi, ip.Description, depMap)
		}
	}

	// Write per-package network state file. Only parent/standalone packages
	// need a state file — dependencies share the parent's NC.
	isDep := netInfo.ParentNCUnitName != ""
	needsNetworkState := !isDep && (len(compiled.Network.External) > 0 || len(compiled.Network.Internal) > 0)
	if cfg.NetworkStatePath != "" && needsNetworkState {
		if err := reconcileWriteNetworkState(cfg, repoName, pi.Name, pi.Version, compiled, ip.Supplies); err != nil {
			return nil, fmt.Errorf("write network state: %w", err)
		}
	}

	if cfg.Systemd == nil {
		return nil, nil //nolint:nilnil // nothing to start when systemd manager is absent
	}

	image := resolveReconcileImage(compiled, cfg.SettingsMgr)
	unitCfg := systemd.PackageUnitConfig{
		RepoName:               repoName,
		PkgName:                pi.Name,
		Version:                pi.Version,
		Description:            ip.Description,
		Image:                  image,
		Entrypoint:             compiled.Entrypoint,
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
		TLSDir:                 hostTLSBase(cfg.BtrfsBasePath),
	}
	if compiled.Runtime == packages.RuntimeVM && compiled.VM != nil {
		unitCfg.VMImagePath = resolveVMImagePath(cfg.BtrfsBasePath, compiled.VM.Image)
	}

	// Apply shared-network and systemd ordering info.
	unitCfg.ParentNetwork = netInfo.ParentNetwork
	unitCfg.ParentUnitName = netInfo.ParentUnitName
	unitCfg.ParentNCUnitName = netInfo.ParentNCUnitName
	unitCfg.DependencyUnitNames = netInfo.DependencyUnitNames
	if netInfo.NetworkAlias != "" {
		unitCfg.NetworkAliases = []string{netInfo.NetworkAlias}
	}

	units := systemd.GeneratePackageUnits(unitCfg)

	// Install all unit files, tracking which ones changed. File I/O
	// happens outside the mutex; only the slice appends need to be
	// serialized.
	serviceChanged := installUnitIfChanged(ctx, cfg.Systemd, units.Service.Name, units.Service.Content)
	for _, sock := range units.Sockets {
		installUnitIfChanged(ctx, cfg.Systemd, sock.Name, sock.Content)
	}
	var ncName string
	var ncChanged bool
	if units.NetworkController != nil {
		ncName = units.NetworkController.Name
		ncChanged = installUnitIfChanged(ctx, cfg.Systemd, ncName, units.NetworkController.Content)
	}

	isDepUnit := packages.IsDependency(pi.Name)

	changedUnits.mu.Lock()
	if ncName != "" {
		changedUnits.allNc = append(changedUnits.allNc, ncName)
		if ncChanged {
			changedUnits.nc = append(changedUnits.nc, ncName)
		}
	}
	if isDepUnit {
		changedUnits.allDeps = append(changedUnits.allDeps, units.Service.Name)
	} else {
		changedUnits.allServices = append(changedUnits.allServices, units.Service.Name)
	}
	if serviceChanged {
		if isDepUnit {
			changedUnits.deps = append(changedUnits.deps, units.Service.Name)
		} else {
			changedUnits.services = append(changedUnits.services, units.Service.Name)
		}
	}
	if len(compiled.PostUpdate) > 0 && compiled.Runtime == packages.RuntimeContainer {
		changedUnits.postUpdates = append(changedUnits.postUpdates, reconcilePostUpdate{
			containerName: systemd.ContainerName(repoName, pi.Name, pi.Version),
			commands:      compiled.PostUpdate,
		})
	}
	changedUnits.mu.Unlock()

	// Enable socket and network controller units.
	for _, sock := range units.Sockets {
		if err := cfg.Systemd.SetStatus(ctx, sock.Name, systemd.Enable); err != nil {
			return nil, fmt.Errorf("enable socket %s: %w", sock.Name, err)
		}
	}
	if ncName != "" {
		if err := cfg.Systemd.SetStatus(ctx, ncName, systemd.Enable); err != nil {
			return nil, fmt.Errorf("enable network controller: %w", err)
		}
	}

	disabled, err := cfg.Installer.IsDisabled(repoName, pi.Name)
	if err != nil {
		return nil, fmt.Errorf("check disabled: %w", err)
	}
	if disabled {
		return nil, nil //nolint:nilnil // disabled packages have units installed but nothing to start
	}
	// Start is deferred to phase 2 of the reconcile loop so that all
	// unit files are on disk before any Start call fires — otherwise
	// a parent package starting concurrently with its dependencies
	// would race on After=/Wants= ordering.
	return &pendingPackageStart{
		serviceUnit: units.Service.Name,
		ncUnit:      ncName,
	}, nil
}

// reconcilePostUpdate tracks a container and its post-update commands.
type reconcilePostUpdate struct {
	containerName string
	commands      []string
}

// reconcileChangedUnits tracks units whose content changed during reconcile.
// The mutex serializes the append sections in reconcilePackage so the
// top-level reconcile loop can run packages in parallel.
type reconcileChangedUnits struct {
	mu          sync.Mutex
	nc          []string              // changed network controller units
	deps        []string              // changed dependency service units
	services    []string              // changed parent/standalone service units
	postUpdates []reconcilePostUpdate // post-update commands for changed container packages

	// allNc, allDeps, allServices track every unit installed during
	// reconcile so that a version-change restart can restart ALL packages,
	// not just those whose unit content changed.
	allNc       []string
	allDeps     []string
	allServices []string
}

// pendingPackageStart is a unit-start job deferred until after every
// package's units have been installed. Splitting install from start is
// what makes the parallel reconcile loop safe: when package A depends on
// package B, systemd's After=/Wants= ordering blocks A's start until B
// is up — but only if B's unit file is on disk first. Installing all
// units in phase 1 and starting them in phase 2 guarantees that.
type pendingPackageStart struct {
	serviceUnit string // empty means no service unit (skip)
	ncUnit      string // empty means no NC unit
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

	pkgs := make([]rolodex.PackageDNSInfo, 0, len(installed))
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

// RebuildDNS tears the authoritative zone down and rebuilds it from
// installed-package records. Use this ONLY for events where a clean slate
// is the whole point: systemcontroller startup (rolodex persists records
// across restarts but the systemcontroller is the source of truth, so any
// drift accumulated while it was down must be discarded) and a confirmed
// local-IP change (every A record's value is wrong and needs to flip).
// Hourly reconciliation and install/uninstall paths must not go through
// here — those need to stay non-disruptive and use ReconcileDNS / the
// per-package helpers instead.
func RebuildDNS(ctx context.Context, cfg ReconcileDNSConfig) error {
	tld := reconcileDNSTLD(cfg.SettingsMgr)

	// Teardown first so AddRecord (which appends, not upserts) does not
	// create duplicate SOA/NS/A records on each rebuild.
	if err := rolodex.TeardownTLD(ctx, cfg.Client, tld); err != nil {
		slog.Debug(fmt.Sprintf("rebuild DNS teardown %s: %v", tld, err))
		// Non-fatal: zone may not exist yet on first boot.
	}

	if err := rolodex.SetupTLD(ctx, cfg.Client, tld, cfg.InternalIP, ""); err != nil {
		return fmt.Errorf("setup TLD %s: %w", tld, err)
	}

	for _, pkg := range collectInstalledDNSInfo(cfg.Installer, cfg.RepositoryRoot) {
		if err := rolodex.RegisterPackageDNS(ctx, cfg.Client, pkg.Repo, pkg.Name, tld, cfg.InternalIP, "", pkg.Domains); err != nil {
			slog.Debug(fmt.Sprintf("rebuild DNS %s/%s: %v", pkg.Repo, pkg.Name, err))
			continue
		}
	}

	return nil
}

// ReconcileDNS converges the authoritative zone toward the desired state
// in-place: add package A records that are missing, remove A records that
// no longer correspond to an installed package, leave everything else
// untouched. Used on the hourly drift-repair poller and whenever the
// systemcontroller wants to push DB state without disrupting live
// resolutions. A teardown-then-rebuild here (see RebuildDNS for the one
// case where that's still the right call) dropped every .home record in
// Go-map-random order on each restart — any client that caught an
// NXDOMAIN during that sub-second gap then hid the package for up to the
// SOA's minimum TTL (3600s here). The diff approach means a restart-era
// reconcile with no package changes touches zero records in rolodex, so
// live resolutions stay live.
//
// Errors on individual records are logged and skipped so a transient
// rolodex hiccup on one record never aborts the whole reconcile.
func ReconcileDNS(ctx context.Context, cfg ReconcileDNSConfig) error {
	tld := reconcileDNSTLD(cfg.SettingsMgr)
	zone := tld + "."

	// Ensure the authoritative zone and its SOA/NS/ns1 records exist.
	// SetupTLD is a no-op if the zone is already present because AddRecord
	// errors out on duplicates — so we only call SetupTLD on first boot
	// (or a rolodex re-init), not on every systemcontroller restart.
	zones, err := cfg.Client.ListAuthoritativeZones(ctx)
	if err != nil {
		return fmt.Errorf("list authoritative zones: %w", err)
	}
	if !slices.Contains(zones, zone) {
		if err := rolodex.SetupTLD(ctx, cfg.Client, tld, cfg.InternalIP, ""); err != nil {
			return fmt.Errorf("setup TLD %s: %w", tld, err)
		}
	}

	// Build the desired set of package A records.
	type recKey struct{ name, value string }
	desired := map[recKey]struct{}{}
	pkgs := collectInstalledDNSInfo(cfg.Installer, cfg.RepositoryRoot)
	if cfg.InternalIP != "" {
		for _, pkg := range pkgs {
			baseName := pkg.Name + "." + pkg.Repo + "." + zone
			desired[recKey{name: baseName, value: cfg.InternalIP}] = struct{}{}
			for _, d := range pkg.Domains {
				desired[recKey{name: d + "." + baseName, value: cfg.InternalIP}] = struct{}{}
			}
		}
	}

	// List current records and index only the A records we own.
	// ns1.<zone> is owned by SetupTLD — leave it alone. SOA/NS records
	// likewise stay put so we never stop answering as the zone owner.
	current, err := cfg.Client.ListRecords(ctx, nil)
	if err != nil {
		return fmt.Errorf("list records: %w", err)
	}
	ns1Name := "ns1." + zone
	currentByKey := map[recKey]*upstream.DnsRecord{}
	for _, r := range current {
		if !strings.HasSuffix(r.Name, zone) || r.RecordType != upstream.RecordTypeA || r.Name == ns1Name {
			continue
		}
		currentByKey[recKey{name: r.Name, value: r.Value}] = r
	}

	// Add records in desired-but-not-current first, so every query during
	// the reconcile sees at least the correct answer (possibly alongside
	// a soon-to-be-removed duplicate).
	aType := upstream.RecordTypeA
	for k := range desired {
		if _, ok := currentByKey[k]; ok {
			continue
		}
		if err := cfg.Client.AddRecord(ctx, &upstream.DnsRecord{
			Name:       k.name,
			RecordType: aType,
			Value:      k.value,
			Ttl:        300,
		}); err != nil {
			slog.Debug(fmt.Sprintf("reconcile DNS add %s: %v", k.name, err))
		}
	}

	// Remove records in current-but-not-desired.
	for k, r := range currentByKey {
		if _, ok := desired[k]; ok {
			continue
		}
		if _, err := cfg.Client.RemoveRecord(ctx, r.Name, &upstream.RemoveRecordOptions{
			RecordType: &aType,
			Value:      r.Value,
		}); err != nil {
			slog.Debug(fmt.Sprintf("reconcile DNS remove %s: %v", r.Name, err))
		}
	}

	return nil
}

// reconcileWriteNetworkState writes the per-package network state file during
// reconciliation. Dependencies (names containing --dep--) always have UPnP
// disabled to keep their ports local-only.
//
// supplies is the raw `supplies:` list from the package YAML. When it
// contains "http" and cfg.TLSCA is set, a leaf cert is issued for the
// package and every external port is marked TLS=true in the state file.
func reconcileWriteNetworkState(cfg ReconcileConfig, repoName, pkgName, version string, compiled *packages.Package, supplies []string) error {
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

	if suppliesHTTP(supplies) && hasHTTPPort(&state, compiled) {
		packageDNS := pkgName + "." + repoName + "." + reconcileDNSTLD(cfg.SettingsMgr)
		certPath, err := issueLeafForPackage(
			cfg.TLSCA,
			cfg.BtrfsBasePath,
			repoName, pkgName, version,
			compiled, packageDNS,
		)
		if err != nil {
			return fmt.Errorf("issue tls leaf: %w", err)
		}
		if certPath != "" {
			applyTLSToPorts(&state, certPath, compiled)
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
func reconcileApplyTemplates(cfg ReconcileConfig, compiled *packages.Package, responses packages.Responses, pi packages.PackageIdentity, description string, deps map[string]packages.TemplateDep) {
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
		Dep:    deps,
	}

	hostname, err := os.Hostname()
	if err == nil {
		data.System.Hostname = hostname
	}

	if err := packages.ApplyPackageTemplates(compiled.Templates, data, func(volName string) string {
		return fmt.Sprintf("%s/%s", cfg.BtrfsBasePath, packageVolumePath(pi.Repo, pi.Name, pi.Version, volName))
	}); err != nil {
		slog.Debug(fmt.Sprintf("reconcile apply templates %s: %v", pi.String(), err))
	}
}

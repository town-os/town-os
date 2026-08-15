package systemcontroller

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// ReconcileConfig holds the dependencies needed to reconcile installed packages
// on startup. Each field mirrors what the system controller uses at runtime.
type ReconcileConfig struct {
	Installer              packages.Installer
	RepositoryRoot         *packages.RepositoryRoot
	Storage                storage.Storage
	Systemd                systemd.Manager
	SettingsMgr            account.SettingsManager
	// NetworkMgr resolves a package's install network to its DNS TLD so
	// reconcile recompiles @PACKAGE_DNS@ under the same TLD the package was
	// installed with (e.g. a package on the "fart" network keeps
	// gitea.default.fart in its unit env, not gitea.default.home). nil falls
	// back to the global dns_tld for every package.
	NetworkMgr             account.NetworkManager
	PagesManager           account.PagesManager
	BtrfsBasePath          string
	NetworkControllerImage string
	NetworkStatePath       string
	CaddyImage             string
	ExternalIP             string
	InternalIP             string
	// IngressClient programs the shared :443 ingress with the desired route
	// set (packages + pages). nil disables ingress programming (tests / boot
	// before the ingress is up); the reconcile is then a no-op for routing.
	IngressClient  ingress.Client
	VersionChanged bool // true when the systemcontroller version differs from last run

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

	// Git is the client used to seed git-backed package volumes and pages into
	// btrfs. nil falls back to a default go-git client (see gitClient); tests
	// inject a mock.
	Git git.Client

	// Gfeh is the set of running object-storage partitions, so the route set
	// this reconcile programs is the same full set boot programs. Omitting it
	// here would make every package install silently drop every gfeh vhost
	// until the next boot -- SetRoutes is declarative and replaces everything.
	Gfeh GfehRegistry
}

// gitClient returns the configured git client, defaulting to a go-git client
// when unset so production callers and older tests that don't set Git keep
// working.
func (c ReconcileConfig) gitClient() git.Client {
	if c.Git != nil {
		return c.Git
	}
	return &git.GoGitClient{}
}

// reconcileDefaultQuota returns the system-wide default quota in bytes from the
// settings manager. Returns 0 if unconfigured.
func reconcileDefaultQuota(ctx context.Context, mgr account.SettingsManager) uint64 {
	if mgr == nil {
		return 0
	}

	val, err := mgr.Get(ctx, "default_quota")
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
		for _, root := range []string{PackagesVolumePrefix, UninstalledVolumePrefix, ArchivesSubvolume, PagesVolumePrefix, VMImagesSubvolume, UserVolumePrefix, TLSSubvolume, GfehVolumePrefix} {
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

		defQuota := reconcileDefaultQuota(ctx, cfg.SettingsMgr)

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

		// Precompute each parent's compiled Dependencies block once
		// (sequentially, before the parallel per-package loop). The
		// block carries Expose/Consume specs that drive shared-volume
		// mounts on both parent and dep units. Compiling here and
		// reusing the result avoids races between dep and parent
		// goroutines that would otherwise both want to load the parent.
		parentDepBlocks := map[repoNameKey]map[string]packages.InputPackageDependency{}
		tldForCompile := reconcileDNSTLD(ctx, cfg.SettingsMgr)
		for parentKey := range allDeps {
			pi, ok := byRepoName[parentKey]
			if !ok {
				continue
			}
			ip, err := cfg.RepositoryRoot.LoadPackage(pi.Repo, pi.Name, pi.Version)
			if err != nil {
				slog.Debug(fmt.Sprintf("reconcile: load parent %s/%s@%s for shared volumes: %v", pi.Repo, pi.Name, pi.Version, err))
				continue
			}
			responses, err := cfg.Installer.GetResponses(pi.Repo, pi.Name, pi.Version)
			if err != nil {
				slog.Debug(fmt.Sprintf("reconcile: parent responses %s/%s@%s: %v", pi.Repo, pi.Name, pi.Version, err))
				continue
			}
			parentCompiled, err := ip.CompileWithContext(responses, packages.CompileContext{
				ExternalHost: cfg.ExternalIP,
				InternalHost: cfg.InternalIP,
				PackageDNS:   pi.Name + "." + pi.Repo + "." + tldForCompile,
			})
			if err != nil {
				slog.Debug(fmt.Sprintf("reconcile: compile parent %s/%s@%s: %v", pi.Repo, pi.Name, pi.Version, err))
				continue
			}
			if len(parentCompiled.Dependencies) > 0 {
				parentDepBlocks[parentKey] = parentCompiled.Dependencies
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
					// Pull this dep's consume[] block from the parent's
					// compiled Dependencies map (precomputed above).
					// SiblingDepRecs covers all of the parent's deps so
					// the dep's reconcile can resolve consume.From keys.
					if block, ok := parentDepBlocks[parentKey]; ok {
						myKey := packages.DepKey(pi.Name)
						if entry, ok := block[myKey]; ok && len(entry.Consume) > 0 {
							netInfo.MyConsume = entry.Consume
							netInfo.SiblingDepRecs = allDeps[parentKey]
						}
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

	// Page provisioning (subvolumes + symlinks + git content) only when pages
	// are enabled.
	if cfg.PagesManager != nil {
		if err := reconcilePages(ctx, cfg); err != nil {
			slog.Error(fmt.Sprintf("reconcile: pages: %v", err))
		}
	}

	// Program the shared :443 ingress with the full route set (HTTP packages +
	// pages). This runs independent of the pages feature — packages need the
	// ingress even when no pages exist.
	if cfg.IngressClient != nil {
		tld := reconcileDNSTLD(ctx, cfg.SettingsMgr)
		if err := RebuildIngress(ctx, cfg.IngressClient, cfg.PagesManager, cfg.NetworkMgr, cfg.Installer,
			cfg.Gfeh, cfg.TLSCA, cfg.BtrfsBasePath, cfg.NetworkStatePath, tld, cfg.InternalIP); err != nil {
			slog.Error(fmt.Sprintf("reconcile: ingress: %v", err))
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
	// MyConsume is this dep's compiled consume[] block taken from the
	// parent's YAML — used to rebuild HostVolumeMounts on the dep so a
	// reconcile after a reboot still bind-mounts sibling volumes. Nil
	// for parents and for deps without a consume: block.
	MyConsume []packages.InputDepConsume
	// SiblingDepRecs is the parent's full set of persisted dep records
	// (keyed by dep key). Used to resolve consume.From → host path for
	// deps. Nil for parents and for deps that don't consume from siblings.
	SiblingDepRecs map[string]packages.DependencyRecord
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

	// Resolve @PACKAGE_DNS@ under the package's install network TLD, not the
	// global dns_tld, so a package on a non-default network keeps its
	// network-scoped FQDN across reconciles. A package on the "fart" network
	// whose env references @PACKAGE_DNS@ (e.g. gitea's ROOT_URL/DOMAIN) must
	// keep gitea.default.fart — recompiling it as gitea.default.home here would
	// rewrite the unit and restart the service with a broken URL. LoadNetwork
	// returns "" for default-network packages, which falls back to dns_tld.
	network, _ := cfg.Installer.LoadNetwork(repoName, pi.Name)
	tld := networkTLDValue(ctx, cfg.NetworkMgr, cfg.SettingsMgr, network)
	compiled, err := ip.CompileWithContext(responses, packages.CompileContext{
		ExternalHost: cfg.ExternalIP,
		InternalHost: cfg.InternalIP,
		PackageDNS:   packageFQDN(repoName, pi.Name, tld),
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
		envVars, deps := buildDepEnvVarsFromRecords(ctx, depRecs, cfg.RepositoryRoot, cfg.Installer, cfg.SettingsMgr, cfg.ExternalIP, cfg.InternalIP)
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
			// Package volumes have no branch concept; clone the remote default.
			if err := seedGitIfEmpty(ctx, cfg.gitClient(), vol.Git, targetPath, ""); err != nil {
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
	// need a state file — dependencies share the parent's NC. Reuse the
	// install-network tld resolved above (line ~418) so the leaf SAN and the
	// state file's FQDN stay under the package's own network TLD across boots.
	isDep := netInfo.ParentNCUnitName != ""
	needsNetworkState := !isDep && (len(compiled.Network.External) > 0 || len(compiled.Network.Internal) > 0)
	if cfg.NetworkStatePath != "" && needsNetworkState {
		overlay := networkOverlayIPValue(ctx, cfg.NetworkMgr, network)
		if err := reconcileWriteNetworkState(cfg, repoName, pi.Name, pi.Version, tld, overlay, compiled, ip.Supplies); err != nil {
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
		DirectPorts:            compiled.Network.DirectPorts,
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
	// Standalone HTTP packages are fronted by the shared :443 ingress, so their
	// HTTP ports are not host-published here; dependencies are never fronted.
	if !isDep {
		unitCfg.IngressPorts = ingressHostPorts(compiled, ip.Supplies)
	}

	// Apply shared-network and systemd ordering info.
	unitCfg.ParentNetwork = netInfo.ParentNetwork
	unitCfg.ParentUnitName = netInfo.ParentUnitName
	unitCfg.ParentNCUnitName = netInfo.ParentNCUnitName
	unitCfg.DependencyUnitNames = netInfo.DependencyUnitNames
	if netInfo.NetworkAlias != "" {
		unitCfg.NetworkAliases = []string{netInfo.NetworkAlias}
	}

	// Resolve shared-volume bind mounts from the parent's compiled
	// Dependencies block. For parents we walk Expose: entries; for deps
	// we walk this dep's MyConsume slice (passed in via netInfo).
	// loadProducerPkg loads + compiles a producer dep so its volumes
	// can be checked for `shareable: true` before emitting a mount.
	loadProducerPkg := func(rec packages.DependencyRecord) (*packages.Package, error) {
		ip, err := cfg.RepositoryRoot.LoadPackage(rec.Repo, rec.Package, rec.Version)
		if err != nil {
			ip, err = cfg.RepositoryRoot.LoadInstalledPackage(rec.Repo, rec.EffectiveName, rec.Version)
			if err != nil {
				return nil, err
			}
		}
		responses, err := cfg.Installer.GetResponses(rec.Repo, rec.EffectiveName, rec.Version)
		if err != nil {
			return nil, err
		}
		return ip.CompileWithContext(responses, packages.CompileContext{
			ExternalHost: cfg.ExternalIP,
			InternalHost: cfg.InternalIP,
			PackageDNS:   rec.EffectiveName + "." + rec.Repo + "." + reconcileDNSTLD(ctx, cfg.SettingsMgr),
		})
	}

	if !packages.IsDependency(pi.Name) && len(compiled.Dependencies) > 0 && len(depRecs) > 0 {
		exposeMounts, err := resolveExposeMounts(cfg.BtrfsBasePath, compiled.Dependencies, depRecs, loadProducerPkg)
		if err != nil {
			return nil, fmt.Errorf("resolve expose mounts: %w", err)
		}
		unitCfg.HostVolumeMounts = append(unitCfg.HostVolumeMounts, exposeMounts...)
	}
	if packages.IsDependency(pi.Name) && len(netInfo.MyConsume) > 0 && len(netInfo.SiblingDepRecs) > 0 {
		consumeMounts, err := resolveConsumeMounts(cfg.BtrfsBasePath, packages.DepKey(pi.Name), netInfo.MyConsume, netInfo.SiblingDepRecs, loadProducerPkg)
		if err != nil {
			return nil, fmt.Errorf("resolve consume mounts: %w", err)
		}
		unitCfg.HostVolumeMounts = append(unitCfg.HostVolumeMounts, consumeMounts...)
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
func reconcileDNSTLD(ctx context.Context, mgr account.SettingsManager) string {
	if mgr == nil {
		return "home"
	}

	val, err := mgr.Get(ctx, "dns_tld")
	if err != nil || val == "" {
		return "home"
	}

	return val
}

// collectInstalledDNSInfo returns DNS info for all installed packages using
// the given installer and repository root.
func collectInstalledDNSInfo(inst packages.Installer, rr *packages.RepositoryRoot, tld string) []rolodex.PackageDNSInfo {
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

		// Packages installed into a non-default network live in that network's
		// scoped TLD zone, not the global home zone. Excluding them here keeps the
		// global reconcile from (re)creating <name>.<repo>.home for them, and lets
		// its remove-pass delete any stale global record left behind by the old
		// always-home install path.
		if network, nerr := inst.LoadNetwork(pi.Repo, pi.Name); nerr == nil &&
			network != "" && network != account.DefaultNetworkName {
			continue
		}

		var domains []string
		if rr != nil {
			ip, err := rr.LoadPackage(pi.Repo, pi.Name, pi.Version)
			if err == nil {
				// Public FQDNs are resolved by the user's real DNS, not
				// rolodex, so they must not become internal subdomains.
				domains = internalDomains(ip.Network.Domains, tld)
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
	PagesManager   account.PagesManager
	// NetworkMgr enumerates non-default networks so RebuildNetworkDNS can
	// establish each one's global authoritative TLD zone and register its
	// packages' LAN-facing records. nil disables the non-default-network DNS
	// rebuild (the global home zone is unaffected).
	NetworkMgr account.NetworkManager
	InternalIP string
	// InternalIPv6 is the host's global IPv6 address (from the same interface
	// as InternalIP), used to publish AAAA records alongside the A records.
	// Empty when the host has no globally routable IPv6 — AAAA is then skipped
	// and behavior is identical to the IPv4-only path.
	InternalIPv6 string
	// NetworkStatePath and BtrfsBasePath let RebuildDNS recompute and
	// republish DANE TLSA records after a zone teardown (TeardownTLD wipes
	// every record, TLSA included). Both are read-only here; empty values
	// disable TLSA republishing.
	NetworkStatePath string
	BtrfsBasePath    string
	// Gfeh is the set of running object-storage partitions. Each is asked for
	// its names on every rebuild, because both reconcilers destroy foreign
	// state -- gfeh contributes hostnames rather than registering them. nil
	// means object storage is not configured and contributes nothing.
	Gfeh GfehRegistry
	// TLSCA is the box's own CA, used here for one leaf: the certificate
	// rolodex's DoT and DoQ listeners present. nil leaves them on the generated
	// self-signed certificate they start with — encrypted DNS still works, it is
	// just unverifiable and never hot-reloaded.
	TLSCA *townostls.CA
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
	tld := reconcileDNSTLD(ctx, cfg.SettingsMgr)

	// Re-assert the blocklist provider lists. Rolodex keeps them in memory
	// only, so a restart it performed without us — and a boot is exactly that
	// — comes back with every configured blocklist switched off unless
	// something puts them back.
	if err := ReconcileBlocklists(ctx, cfg.Client, cfg.SettingsMgr); err != nil {
		slog.Debug(fmt.Sprintf("rebuild DNS blocklists: %v", err))
	}

	// Teardown first so AddRecord (which appends, not upserts) does not
	// create duplicate SOA/NS/A records on each rebuild.
	if err := rolodex.TeardownTLD(ctx, cfg.Client, tld); err != nil {
		slog.Debug(fmt.Sprintf("rebuild DNS teardown %s: %v", tld, err))
		// Non-fatal: zone may not exist yet on first boot.
	}

	if err := rolodex.SetupTLD(ctx, cfg.Client, tld, cfg.InternalIP, cfg.InternalIPv6); err != nil {
		return fmt.Errorf("setup TLD %s: %w", tld, err)
	}

	excluded := loadDNSExcludedServices(ctx, cfg.SettingsMgr)
	for _, pkg := range filterExcludedDNSInfo(collectInstalledDNSInfo(cfg.Installer, cfg.RepositoryRoot, tld), excluded) {
		if err := rolodex.RegisterPackageDNS(ctx, cfg.Client, pkg.Repo, pkg.Name, tld, cfg.InternalIP, cfg.InternalIPv6, pkg.Domains); err != nil {
			slog.Debug(fmt.Sprintf("rebuild DNS %s/%s: %v", pkg.Repo, pkg.Name, err))
			continue
		}
	}

	// Pages share the zone and resolve to the host IP, keyed by their internal
	// hostname. Public-FQDN pages are excluded (resolved by the user's own DNS).
	// An AAAA is added alongside the A when the host has a global IPv6.
	for _, host := range collectPageHostnames(ctx, cfg.PagesManager, tld) {
		if cfg.InternalIP != "" {
			if err := cfg.Client.AddRecord(ctx, &upstream.DnsRecord{
				Name:       host + ".",
				RecordType: upstream.RecordTypeA,
				Value:      cfg.InternalIP,
				Ttl:        300,
			}); err != nil {
				slog.Debug(fmt.Sprintf("rebuild DNS page %s: %v", host, err))
			}
		}
		if cfg.InternalIPv6 != "" {
			if err := cfg.Client.AddRecord(ctx, &upstream.DnsRecord{
				Name:       host + ".",
				RecordType: upstream.RecordTypeAAAA,
				Value:      cfg.InternalIPv6,
				Ttl:        300,
			}); err != nil {
				slog.Debug(fmt.Sprintf("rebuild DNS page AAAA %s: %v", host, err))
			}
		}
	}

	// Encrypted DNS: the DoH endpoint's address records, the DDR designation
	// that advertises them, the DoT/DoQ leaf and its DANE pins. One call
	// because all four are named after the same `dns.<tld>` and a caller that
	// updates some of them publishes a resolver clients cannot verify — see
	// publishEncryptedDNS.
	publishEncryptedDNS(ctx, cfg.Client, cfg.TLSCA, cfg.BtrfsBasePath, tld, cfg.InternalIP, cfg.InternalIPv6)

	// Object storage on the default network shares this zone. Its names come
	// from the partition itself (GET /v1/names) rather than from anything
	// derived here, which is the whole reason gfeh needs no DNS client.
	gfehSites := collectGfehSites(ctx, cfg.Gfeh, cfg.NetworkMgr, tld, "")
	publishGfehGlobalRecords(ctx, cfg, gfehSites)

	// TeardownTLD wiped every record including DANE TLSA, so re-pin the leaf
	// for each installed package's terminated ports and each internal page.
	entries := collectInstalledTLSA(cfg, tld)
	entries = append(entries, collectPageTLSA(ctx, cfg.PagesManager, cfg.BtrfsBasePath, tld)...)
	entries = append(entries, collectGfehTLSA(gfehSites, cfg.BtrfsBasePath)...)
	if len(entries) > 0 {
		if err := rolodex.RegisterPackageTLSA(ctx, cfg.Client, entries); err != nil {
			slog.Debug(fmt.Sprintf("rebuild TLSA: %v", err))
		}
	}

	return nil
}

// RebuildNetworkDNS re-registers each non-default network package's LAN-facing
// A/AAAA record under its network TLD, at the box's internal IP. This is the LAN
// half of the dual-home: overlay peers reach the package via its scoped record
// (the overlay IP), while loopback/LAN clients reach it via this global record
// (the LAN IP). No global authoritative zone is published — a bare global A
// record resolves on the LAN, and rolodex's LAN->owning-scope fallback (the
// network TLD is owned by the scope applyNetworkTransport creates) both makes
// the global record authoritative for LAN sources and returns an authoritative
// NXDOMAIN for unmatched names instead of leaking the private TLD upstream. The
// scoped/overlay records and the scoped zone apex live in separate rolodex
// tables and are owned by applyNetworkTransport; this function never touches
// them. Idempotent across reboots: each package's prior global records are
// removed before re-adding, since AddRecord errors on duplicates. No-op when no
// network manager or rolodex client is configured.
func RebuildNetworkDNS(ctx context.Context, cfg ReconcileDNSConfig) error {
	if cfg.NetworkMgr == nil || cfg.Client == nil {
		return nil
	}

	nets, err := cfg.NetworkMgr.List(ctx)
	if err != nil {
		return fmt.Errorf("list networks: %w", err)
	}

	excluded := loadDNSExcludedServices(ctx, cfg.SettingsMgr)
	for i := range nets {
		n := nets[i]
		if n.Name == account.DefaultNetworkName || n.TLD == "" {
			continue
		}

		for _, pkg := range filterExcludedDNSInfo(collectNetworkDNSInfo(cfg.Installer, cfg.RepositoryRoot, n.Name, n.TLD), excluded) {
			// Drop any prior global records for this package so a reboot
			// re-registers cleanly (RemoveRecord is a no-op when absent).
			if err := rolodex.UnregisterPackageDNS(ctx, cfg.Client, pkg.Repo, pkg.Name, n.TLD, pkg.Domains); err != nil {
				slog.Debug(fmt.Sprintf("rebuild network DNS clear %s/%s: %v", pkg.Repo, pkg.Name, err))
			}
			if err := rolodex.RegisterPackageDNS(ctx, cfg.Client, pkg.Repo, pkg.Name, n.TLD, cfg.InternalIP, cfg.InternalIPv6, pkg.Domains); err != nil {
				slog.Debug(fmt.Sprintf("rebuild network DNS %s/%s: %v", pkg.Repo, pkg.Name, err))
			}
		}

		// Pages on this network are dual-homed exactly like its packages: a scoped
		// A at the overlay address for WireGuard peers, and a global A at the LAN
		// address for loopback/LAN clients. Pages have no install record, so they
		// are keyed off PageSite.Network rather than Installer.LoadNetwork.
		overlay := networkOverlayIPValue(ctx, cfg.NetworkMgr, n.Name)
		for _, host := range collectNetworkPageHostnames(ctx, cfg.PagesManager, n.Name, n.TLD) {
			if overlay != "" {
				if err := cfg.Client.AddScopedRecord(ctx, n.Name, &upstream.DnsRecord{
					Name: host + ".", RecordType: upstream.RecordTypeA, Value: overlay, Ttl: 300,
				}); err != nil {
					slog.Debug(fmt.Sprintf("rebuild network page scoped %s: %v", host, err))
				}
			}
			if cfg.InternalIP != "" {
				if err := cfg.Client.AddRecord(ctx, &upstream.DnsRecord{
					Name: host + ".", RecordType: upstream.RecordTypeA, Value: cfg.InternalIP, Ttl: 300,
				}); err != nil {
					slog.Debug(fmt.Sprintf("rebuild network page %s: %v", host, err))
				}
			}
			if cfg.InternalIPv6 != "" {
				if err := cfg.Client.AddRecord(ctx, &upstream.DnsRecord{
					Name: host + ".", RecordType: upstream.RecordTypeAAAA, Value: cfg.InternalIPv6, Ttl: 300,
				}); err != nil {
					slog.Debug(fmt.Sprintf("rebuild network page AAAA %s: %v", host, err))
				}
			}
		}

		// Dual-home the DANE TLSA exactly like the A records above: a GLOBAL pin
		// (served to LAN sources, which resolve the network TLD through rolodex's
		// LAN->owning-scope fallback) and a SCOPED pin (served to WireGuard peers,
		// whose queries are answered from the scope and never see global records).
		// Install only ever wrote the scoped half (publishPackageTLSA), and nothing
		// republished either half after a restart — so a LAN client had no TLSA at
		// all for a network package, and a reboot dropped the scoped one.
		// Object storage on this network is dual-homed exactly like its pages:
		// a scoped A at the overlay address for WireGuard peers, and a global A
		// at the LAN address for loopback and LAN clients. Each side gets an
		// address it can actually route to.
		gfehSites := collectGfehSites(ctx, cfg.Gfeh, cfg.NetworkMgr, n.TLD, n.Name)
		publishGfehNetworkRecords(ctx, cfg, n.Name, overlay, gfehSites)

		entries := collectNetworkTLSA(cfg, n.Name, n.TLD)
		entries = append(entries, collectNetworkPageTLSA(ctx, cfg.PagesManager, cfg.BtrfsBasePath, n.Name, n.TLD)...)
		entries = append(entries, collectGfehTLSA(gfehSites, cfg.BtrfsBasePath)...)
		if len(entries) > 0 {
			if err := rolodex.RegisterPackageTLSA(ctx, cfg.Client, entries); err != nil {
				slog.Debug(fmt.Sprintf("rebuild network TLSA %s: %v", n.Name, err))
			}
			if err := rolodex.RegisterScopedPackageTLSA(ctx, cfg.Client, n.Name, entries); err != nil {
				slog.Debug(fmt.Sprintf("rebuild scoped network TLSA %s: %v", n.Name, err))
			}
		}
	}

	return nil
}

// collectNetworkTLSA returns DANE TLSA entries for the installed packages
// assigned to the given non-default network, pinned under that network's TLD.
// It is the inverse filter of collectInstalledTLSA, which skips exactly these
// packages so the global home zone never carries a stale pin under a network's
// TLD.
func collectNetworkTLSA(cfg ReconcileDNSConfig, network, tld string) []rolodex.TLSAEntry {
	if cfg.Installer == nil || cfg.NetworkStatePath == "" || cfg.BtrfsBasePath == "" {
		return nil
	}
	installed, err := cfg.Installer.ListInstalled()
	if err != nil {
		return nil
	}
	var entries []rolodex.TLSAEntry
	for _, pkg := range installed {
		pi, err := packages.ParsePackageIdentity(pkg)
		if err != nil {
			continue
		}
		net, nerr := cfg.Installer.LoadNetwork(pi.Repo, pi.Name)
		if nerr != nil || net != network {
			continue
		}
		var domains []string
		if cfg.RepositoryRoot != nil {
			if ip, lerr := cfg.RepositoryRoot.LoadPackage(pi.Repo, pi.Name, pi.Version); lerr == nil {
				domains = ip.Network.Domains
			}
		}
		pkgEntries, err := buildTLSAEntries(cfg.NetworkStatePath, cfg.BtrfsBasePath, pi.Repo, pi.Name, pi.Version, tld, domains)
		if err != nil {
			slog.Debug(fmt.Sprintf("collect network TLSA %s/%s: %v", pi.Repo, pi.Name, err))
			continue
		}
		entries = append(entries, pkgEntries...)
	}
	return entries
}

// collectNetworkDNSInfo returns LAN-facing DNS info for installed packages
// assigned to the given non-default network — the inverse of the exclusion in
// collectInstalledDNSInfo, which drops those same packages from the global home
// zone so they only appear under their network TLD here.
func collectNetworkDNSInfo(inst packages.Installer, rr *packages.RepositoryRoot, network, tld string) []rolodex.PackageDNSInfo {
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

		net, nerr := inst.LoadNetwork(pi.Repo, pi.Name)
		if nerr != nil || net != network {
			continue
		}

		var domains []string
		if rr != nil {
			if ip, lerr := rr.LoadPackage(pi.Repo, pi.Name, pi.Version); lerr == nil {
				domains = internalDomains(ip.Network.Domains, tld)
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

// collectInstalledTLSA computes the DANE TLSA entries for every installed
// package by reading its network state file and pinning the issued leaf. Used
// by RebuildDNS to re-pin certs after a zone teardown. Returns nil when state
// path / btrfs base are unset.
func collectInstalledTLSA(cfg ReconcileDNSConfig, tld string) []rolodex.TLSAEntry {
	if cfg.Installer == nil || cfg.NetworkStatePath == "" || cfg.BtrfsBasePath == "" {
		return nil
	}
	installed, err := cfg.Installer.ListInstalled()
	if err != nil {
		return nil
	}
	var entries []rolodex.TLSAEntry
	for _, pkg := range installed {
		pi, err := packages.ParsePackageIdentity(pkg)
		if err != nil {
			continue
		}
		// Non-default-network packages publish their DANE TLSA scoped to that
		// network (see publishPackageTLSA), not in the global home zone. Skip them
		// here so the global rebuild does not add a stale, partition-hidden TLSA
		// under the network's TLD.
		if network, nerr := cfg.Installer.LoadNetwork(pi.Repo, pi.Name); nerr == nil &&
			network != "" && network != account.DefaultNetworkName {
			continue
		}
		var domains []string
		if cfg.RepositoryRoot != nil {
			if ip, lerr := cfg.RepositoryRoot.LoadPackage(pi.Repo, pi.Name, pi.Version); lerr == nil {
				domains = ip.Network.Domains
			}
		}
		pkgEntries, err := buildTLSAEntries(cfg.NetworkStatePath, cfg.BtrfsBasePath, pi.Repo, pi.Name, pi.Version, tld, domains)
		if err != nil {
			slog.Debug(fmt.Sprintf("collect TLSA %s/%s: %v", pi.Repo, pi.Name, err))
			continue
		}
		entries = append(entries, pkgEntries...)
	}
	return entries
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
	tld := reconcileDNSTLD(ctx, cfg.SettingsMgr)
	zone := tld + "."

	// Blocklists are diffed the same way the records below are, and for the
	// same reason: rolodex can be restarted by anything at any time, and it
	// remembers no provider list across one. Normally two reads and no writes.
	if err := ReconcileBlocklists(ctx, cfg.Client, cfg.SettingsMgr); err != nil {
		slog.Debug(fmt.Sprintf("reconcile DNS blocklists: %v", err))
	}

	// Ensure the authoritative zone and its SOA/NS/ns1 records exist.
	// SetupTLD is a no-op if the zone is already present because AddRecord
	// errors out on duplicates — so we only call SetupTLD on first boot
	// (or a rolodex re-init), not on every systemcontroller restart.
	zones, err := cfg.Client.ListAuthoritativeZones(ctx)
	if err != nil {
		return fmt.Errorf("list authoritative zones: %w", err)
	}
	if !slices.Contains(zones, zone) {
		if err := rolodex.SetupTLD(ctx, cfg.Client, tld, cfg.InternalIP, cfg.InternalIPv6); err != nil {
			return fmt.Errorf("setup TLD %s: %w", tld, err)
		}
	}

	// Build the desired set of package address records. Each name gets an A
	// record (the host IPv4) and, when the host has a global IPv6, a parallel
	// AAAA record. The record type is part of the key so A and AAAA for the
	// same name are reconciled independently — losing IPv6 removes only the
	// AAAA, and vice versa.
	type recKey struct {
		name  string
		value string
		rtype upstream.RecordType
	}
	desired := map[recKey]struct{}{}
	addDesired := func(name string) {
		if cfg.InternalIP != "" {
			desired[recKey{name: name, value: cfg.InternalIP, rtype: upstream.RecordTypeA}] = struct{}{}
		}
		if cfg.InternalIPv6 != "" {
			desired[recKey{name: name, value: cfg.InternalIPv6, rtype: upstream.RecordTypeAAAA}] = struct{}{}
		}
	}
	pkgs := filterExcludedDNSInfo(collectInstalledDNSInfo(cfg.Installer, cfg.RepositoryRoot, tld), loadDNSExcludedServices(ctx, cfg.SettingsMgr))
	for _, pkg := range pkgs {
		baseName := pkg.Name + "." + pkg.Repo + "." + zone
		addDesired(baseName)
		for _, d := range pkg.Domains {
			addDesired(d + "." + baseName)
		}
	}
	// Pages resolve to the same host, keyed by their assigned internal
	// hostname (e.g. blog.<tld>). They live in the same zone as packages,
	// so they must be in the desired set or the remove pass below would
	// delete them as orphan records.
	for _, host := range collectPageHostnames(ctx, cfg.PagesManager, tld) {
		addDesired(host + ".")
	}
	// Object storage, for exactly the same reason -- and this is the easiest
	// fold-in to miss, because omitting it does not break anything at boot.
	// The records get published, everything works, and then this hourly pass
	// deletes them as orphans an hour later.
	for _, site := range collectGfehSites(ctx, cfg.Gfeh, cfg.NetworkMgr, tld, "") {
		addDesired(site.FQDN + ".")
	}
	// The DoH endpoint, which RebuildDNS publishes. It has to be in the desired
	// set for the same reason object storage does, and the consequence is worse
	// here: this pass would delete it as an orphan an hour after boot, leaving a
	// vhost and a leaf for a name that stopped resolving.
	if name := dohRecordName(tld); name != "" {
		addDesired(name)
	}

	// List current records and index only the A/AAAA records we own.
	// ns1.<zone> is owned by SetupTLD — leave it alone. SOA/NS records
	// likewise stay put so we never stop answering as the zone owner.
	current, err := cfg.Client.ListRecords(ctx, nil)
	if err != nil {
		return fmt.Errorf("list records: %w", err)
	}
	ns1Name := "ns1." + zone
	currentByKey := map[recKey]*upstream.DnsRecord{}
	for _, r := range current {
		if !strings.HasSuffix(r.Name, zone) || r.Name == ns1Name {
			continue
		}
		if r.RecordType != upstream.RecordTypeA && r.RecordType != upstream.RecordTypeAAAA {
			continue
		}
		currentByKey[recKey{name: r.Name, value: r.Value, rtype: r.RecordType}] = r
	}

	// Add records in desired-but-not-current first, so every query during
	// the reconcile sees at least the correct answer (possibly alongside
	// a soon-to-be-removed duplicate).
	for k := range desired {
		if _, ok := currentByKey[k]; ok {
			continue
		}
		if err := cfg.Client.AddRecord(ctx, &upstream.DnsRecord{
			Name:       k.name,
			RecordType: k.rtype,
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
		rtype := r.RecordType
		if _, err := cfg.Client.RemoveRecord(ctx, r.Name, &upstream.RemoveRecordOptions{
			RecordType: &rtype,
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
//
// tld and overlayIP are the caller's *install-network* values (resolved once in
// reconcilePackage), NOT the global dns_tld. Recomputing the global TLD here is
// exactly the bug this signature exists to prevent: it re-issued a
// fart-network package's leaf with SAN <pkg>.<repo>.home on every boot,
// silently clobbering the correct <pkg>.<repo>.fart SAN written at install and
// leaving the ingress serving a name the cert was not valid for.
func reconcileWriteNetworkState(cfg ReconcileConfig, repoName, pkgName, version, tld, overlayIP string, compiled *packages.Package, supplies []string) error {
	state := buildPackageNetworkState(repoName, pkgName, version, compiled)

	if err := applyPackageTLS(
		&state, cfg.TLSCA, cfg.BtrfsBasePath,
		repoName, pkgName, version, cfg.InternalIP, tld, overlayIP,
		compiled, supplies,
	); err != nil {
		return err
	}

	return writeNetworkStateFile(cfg.NetworkStatePath, repoName, pkgName, version, &state)
}

// reconcilePages ensures all existing pages have btrfs subvolumes and webroot
// symlinks. The shared :443 ingress that fronts them is programmed separately
// over gRPC (see the ingress block in Reconcile and RebuildIngress).
func reconcilePages(ctx context.Context, cfg ReconcileConfig) error {
	if err := EnsurePagesWebroot(cfg.BtrfsBasePath); err != nil {
		return fmt.Errorf("ensure pages webroot: %w", err)
	}

	pages, err := cfg.PagesManager.List(ctx)
	if err != nil {
		return fmt.Errorf("list pages: %w", err)
	}

	// Page storage is keyed by the served FQDN — which is named under the page's
	// OWN network TLD, not the global dns_tld. Getting this wrong is not cosmetic:
	// `valid` drives pruneStalePageSymlinks below, so naming a fart-network page
	// as blog.home here would both miss its real blog.fart directory AND prune the
	// live symlink the ingress is serving from.
	globalTLD := reconcileDNSTLD(ctx, cfg.SettingsMgr)
	valid := make(map[string]struct{}, len(pages))
	for _, page := range pages {
		dir := pageFQDN(ctx, cfg.NetworkMgr, page, globalTLD)
		if dir == "" {
			continue
		}
		valid[dir] = struct{}{}

		// Ensure subvolume exists.
		if cfg.Storage != nil {
			fsName := PagesVolumePrefix + "/" + dir
			if err := cfg.Storage.CreateFilesystem(storage.Filesystem{Name: fsName}); err != nil {
				slog.Debug(fmt.Sprintf("reconcile: pages subvolume %s: %v", dir, err))
			}
		}

		// Ensure symlink exists.
		if err := EnsurePageSymlink(cfg.BtrfsBasePath, dir); err != nil {
			slog.Debug(fmt.Sprintf("reconcile: pages symlink %s: %v", dir, err))
		}

		// Seed git-backed page content into the (empty) subvolume, using the
		// same idempotent git->storage seeder as package volumes. This is what
		// makes page content converge on btrfs and self-heal after a boot-time
		// DNS failure: a populated site is skipped, an empty one (fresh, or left
		// empty by a failed create-time clone) is (re)cloned on this reconcile.
		if page.SourceType == account.PageSourceGit && page.RepoURL != "" {
			targetPath := fmt.Sprintf("%s/%s/%s", cfg.BtrfsBasePath, PagesVolumePrefix, dir)
			if err := seedGitIfEmpty(ctx, cfg.gitClient(), page.RepoURL, targetPath, page.Branch); err != nil {
				slog.Debug(fmt.Sprintf("reconcile: pages git-seed %s: %v", dir, err))
			}
		}
	}

	// The webroot is shared with object storage: each partition's generated
	// index page is linked in beside the pages (see gfeh_index.go). Those links
	// are not pages, so without this the prune below deletes every one of them
	// on the first reconcile and the index names 404 until the ingress is next
	// rebuilt.
	//
	// Derived from the network set rather than by asking the daemons, so a
	// partition that is merely slow to start does not have its index pruned out
	// from under it — what may be deleted has to be decidable from state Town OS
	// owns.
	for host := range gfehIndexHostnames(ctx, cfg.Gfeh, cfg.NetworkMgr, globalTLD) {
		valid[host] = struct{}{}
	}

	// Prune webroot symlinks left behind by a TLD change or removed page so the
	// static server never resolves a stale hostname to old content.
	pruneStalePageSymlinks(cfg.BtrfsBasePath, valid)

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

package systemcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sort"
	"strconv"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

type InstallRequest struct {
	Repo              string             `json:"repo"`
	Name              string             `json:"name"`
	Version           string             `json:"version"`
	Responses         packages.Responses `json:"responses"`
	ReuseVolumes      bool               `json:"reuse_volumes"`
	ImportFromVersion string             `json:"import_from_version,omitempty"`
	SkipResponseReuse bool               `json:"skip_response_reuse,omitempty"`
	Instance          string             `json:"instance,omitempty"`
}

type UninstallRequest struct {
	Repo         string `json:"repo"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	PurgeVolumes bool   `json:"purge_volumes"`
	Instance     string `json:"instance,omitempty"`
}

type InstallPreviewRequest struct {
	Repo    string `json:"repo"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type VolumePreview struct {
	Name       string `json:"name"`
	Mountpoint string `json:"mountpoint"`
	Quota      string `json:"quota,omitempty"`
	Migrated   bool   `json:"migrated"`
	Fresh      bool   `json:"fresh"`
}

type PortPreview struct {
	External uint16 `json:"external"`
	Internal uint16 `json:"internal"`
}

type InstallPreview struct {
	Repo             string                   `json:"repo"`
	Name             string                   `json:"name"`
	Version          string                   `json:"version"`
	Description      string                   `json:"description,omitempty"`
	Image            string                   `json:"image"`
	Runtime          string                   `json:"runtime"`
	Volumes          []VolumePreview          `json:"volumes"`
	ExternalPorts    []PortPreview            `json:"external_ports"`
	InternalPorts    []PortPreview            `json:"internal_ports"`
	UpgradingFrom    string                   `json:"upgrading_from,omitempty"`
	HasQuestions     bool                     `json:"has_questions"`
	DiskUsage        *storage.DiskUsage       `json:"disk_usage,omitempty"`
	TotalQuota       uint64                   `json:"total_quota"`
	QuotaExceedsDisk bool                     `json:"quota_exceeds_disk"`
	Summary          string                   `json:"summary"`
	VM               *packages.InputPackageVM `json:"vm,omitempty"`
}

// defaultQuota returns the system-wide default quota in bytes.
// If no settings manager is configured or the value is missing/invalid, it
// returns 0 (no quota).
func (s *SystemControllerHandlers) defaultQuota() uint64 {
	mgr := s.Controller.GetSettingsManager()
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

// installPackageUnits installs all systemd unit files for a package, enables
// socket and network controller units, and starts the main service.
func (s *SystemControllerHandlers) installPackageUnits(ctx context.Context, sd systemd.Manager, units systemd.PackageUnits) error {
	// Install all unit files.
	if err := sd.InstallUnit(ctx, units.Service.Name, units.Service.Content); err != nil {
		return fmt.Errorf("install service unit: %w", err)
	}
	for _, sock := range units.Sockets {
		if err := sd.InstallUnit(ctx, sock.Name, sock.Content); err != nil {
			return fmt.Errorf("install socket unit %s: %w", sock.Name, err)
		}
	}
	if units.NetworkController != nil {
		if err := sd.InstallUnit(ctx, units.NetworkController.Name, units.NetworkController.Content); err != nil {
			return fmt.Errorf("install network controller unit: %w", err)
		}
	}

	// Enable socket and network controller units.
	for _, sock := range units.Sockets {
		if err := sd.SetStatus(ctx, sock.Name, systemd.Enable); err != nil {
			return fmt.Errorf("enable socket %s: %w", sock.Name, err)
		}
	}
	if units.NetworkController != nil {
		if err := sd.SetStatus(ctx, units.NetworkController.Name, systemd.Enable); err != nil {
			return fmt.Errorf("enable network controller: %w", err)
		}
	}

	// Explicitly start the NC before the service to avoid races where
	// systemd's Wants= pull-in hasn't processed by the time the service's
	// ExecStartPre waits for the NC container to be running.
	if units.NetworkController != nil {
		if err := sd.SetStatus(ctx, units.NetworkController.Name, systemd.Start); err != nil {
			slog.Warn("network controller failed to start after install", "unit", units.NetworkController.Name, "error", err)
		}
	}

	// Start the main service. A start failure is logged but does not fail the
	// install — the package is fully installed (volumes, unit files, network
	// state) and the user can see the failed service in the UI and retry.
	if err := sd.SetStatus(ctx, units.Service.Name, systemd.Start); err != nil {
		slog.Warn("service failed to start after install", "unit", units.Service.Name, "error", err)
	}

	return nil
}

// uninstallPackageUnits stops, disables, and uninstalls all systemd units for
// a package by scanning installed unit files.
func (s *SystemControllerHandlers) uninstallPackageUnits(ctx context.Context, sd systemd.Manager, repoName, pkgName, version string) error {
	unitNames, err := sd.ListPackageUnitFiles(ctx, repoName, pkgName, version)
	if err != nil {
		return fmt.Errorf("list package unit files: %w", err)
	}

	for _, name := range unitNames {
		if err := sd.SetStatus(ctx, name, systemd.Stop); err != nil {
			slog.Debug(fmt.Sprintf("stop unit %s: %v", name, err)) //#nosec G706 -- name from trusted ListPackageUnitFiles
		}
		if err := sd.SetStatus(ctx, name, systemd.Disable); err != nil {
			slog.Debug(fmt.Sprintf("disable unit %s: %v", name, err)) //#nosec G706 -- name from trusted ListPackageUnitFiles
		}
		if err := sd.UninstallUnit(ctx, name); err != nil {
			slog.Debug(fmt.Sprintf("uninstall unit %s: %v", name, err)) //#nosec G706 -- name from trusted ListPackageUnitFiles
		}
	}
	return nil
}

// packageUnitConfig builds a PackageUnitConfig from a compiled package and
// backend configuration. When the package uses proton and has no explicit
// image URL, the proton_image system setting is used.
func (s *SystemControllerHandlers) packageUnitConfig(repoName, pkgName, version, description string, compiled *packages.Package, supplies []string) systemd.PackageUnitConfig {
	image := s.resolveProtonImage(compiled)
	cfg := systemd.PackageUnitConfig{
		RepoName:               repoName,
		PkgName:                pkgName,
		Version:                version,
		Description:            description,
		Image:                  image,
		Entrypoint:             compiled.Entrypoint,
		Command:                compiled.Command,
		Environment:            compiled.Environment,
		External:               compiled.Network.External,
		Internal:               compiled.Network.Internal,
		DirectPorts:            compiled.Network.DirectPorts,
		IngressPorts:           ingressHostPorts(compiled, supplies),
		Volumes:                compiled.Volumes,
		BtrfsBase:              s.Controller.GetBtrfsBasePath(),
		NetworkControllerImage: s.Controller.GetNetworkControllerImage(),
		NetworkStatePath:       s.Controller.GetNetworkStatePath(),
		Runtime:                compiled.Runtime,
		VM:                     compiled.VM,
		TLSDir:                 hostTLSBase(s.Controller.GetBtrfsBasePath()),
	}
	if compiled.Runtime == packages.RuntimeVM && compiled.VM != nil {
		cfg.VMImagePath = resolveVMImagePath(s.Controller.GetBtrfsBasePath(), compiled.VM.Image)
	}
	return cfg
}

// findActiveVersion returns the currently installed version for a package,
// or "" if not installed.
func findActiveVersion(inst packages.Installer, repoName, effectiveName string) (string, error) {
	version, found, err := inst.GetInstalledVersion(repoName, effectiveName)
	if err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}
	return version, nil
}

func (s *SystemControllerHandlers) installPackage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := InstallRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()
	repoName := req.Repo
	if repoName == "" {
		var err error
		repoName, err = rr.FindRepoForPackage(req.Name, req.Version)
		if err != nil {
			return err
		}
	}

	parentName := req.Name
	effectiveName := req.Name
	if req.Instance != "" {
		effectiveName = fmt.Sprintf("%s-%s", parentName, req.Instance)
	}

	ip, err := rr.LoadPackage(repoName, parentName, req.Version)
	if err != nil {
		return err
	}

	unlock := s.lockPackage(repoName, effectiveName)
	defer unlock()

	inst := s.Controller.GetInstaller()
	ctx := c.Request().Context()

	activeVersion, err := findActiveVersion(inst, repoName, effectiveName)
	if err != nil {
		return err
	}

	// Carry forward responses from the active version during upgrades.
	if activeVersion != "" && activeVersion != req.Version {
		oldResponses, err := inst.GetResponses(repoName, effectiveName, activeVersion)
		if err == nil {
			mergeResponses(&req.Responses, oldResponses, ip.Questions)
		}
	}

	// Load last responses when reusing volumes from a previous uninstall.
	if req.ReuseVolumes && !req.SkipResponseReuse {
		lastResp, err := inst.LoadLastResponses(repoName, effectiveName)
		if err == nil && len(lastResp) > 0 {
			mergeResponses(&req.Responses, lastResp, ip.Questions)
		}
	}

	if err := s.autoGenerateResponses(&req.Responses, ip.Questions, effectiveName); err != nil {
		return err
	}

	compiled, err := ip.CompileWithContext(req.Responses, packages.CompileContext{
		ExternalHost: s.Controller.GetExternalIP(),
		InternalHost: s.Controller.GetInternalIP(),
		PackageDNS:   effectiveName + "." + repoName + "." + s.getDNSTLDValue(),
	})
	if err != nil {
		return err
	}

	// Begin streaming progress events. After this point, errors are sent
	// as SSE error events (the HTTP status is already 200).
	pw := NewProgressWriter(c)

	if activeVersion != "" {
		pw.Step("preparing_upgrade")
		if err := s.prepareActiveVersion(ctx, rr, inst, repoName, parentName, effectiveName, activeVersion, req.Version, req.ImportFromVersion, compiled); err != nil {
			pw.Err(err)
			return nil
		}
	}

	pw.Step("provisioning_volumes")
	if err := s.provisionVolumes(repoName, effectiveName, req.Version, req.ImportFromVersion, req.ReuseVolumes, compiled); err != nil {
		pw.Err(err)
		return nil
	}

	pw.Step("seeding_data")
	s.seedVolumeData(ctx, &ip, compiled, repoName, parentName, effectiveName, req.Version)

	// Install dependencies before the parent's file templates render so
	// they can reference dep host/ports via {{.Dep.key.Host}} /
	// {{index .Dep.key.Ports "sql"}}. depMap is passed to applyPackageTemplates
	// below; for packages with no deps the map is nil and .Dep renders empty.
	var depMap map[string]packages.TemplateDep
	// depRecordsForExpose / depCompiledForExpose feed the parent's
	// Expose: shared-volume resolver below; both are populated when
	// the parent declares any deps.
	var depRecordsForExpose map[string]packages.DependencyRecord
	var depCompiledForExpose map[string]*packages.Package
	if len(compiled.Dependencies) > 0 {
		pw.Step("installing_dependencies")
		// Compute parent NC unit name so deps can wait for the network.
		var parentNCUnitName string
		if len(compiled.Network.External) > 0 || len(compiled.Network.Internal) > 0 {
			parentNCUnitName = systemd.NetworkControllerUnitName(repoName, effectiveName, req.Version)
		}
		depRecords, depEnvVars, deps, depCompiled, err := s.installDependencies(ctx, repoName, effectiveName, req.Version, parentNCUnitName, compiled.Dependencies)
		if err != nil {
			pw.Err(fmt.Errorf("install dependencies: %w", err))
			return nil
		}
		depMap = deps
		depRecordsForExpose = depRecords
		depCompiledForExpose = depCompiled

		// Inject dependency connection environment variables into the parent
		// and resolve @dep_KEY_host@ / @dep_KEY_port_N@ template variables.
		// applyDepTemplates runs ApplyTemplates over every env value, which
		// also collapses `@@` escapes — so we always call it even when
		// depEnvVars is empty, otherwise @@ would survive into the unit.
		if len(depEnvVars) > 0 {
			if compiled.Environment == nil {
				compiled.Environment = map[string]string{}
			}
			maps.Copy(compiled.Environment, depEnvVars)
		}
		if len(compiled.Environment) > 0 {
			applyDepTemplates(compiled.Environment, depEnvVars)
		}

		// Save dependency records for uninstall.
		if len(depRecords) > 0 {
			if err := inst.SaveDependencies(repoName, effectiveName, depRecords); err != nil {
				pw.Err(fmt.Errorf("save dependencies: %w", err))
				return nil
			}
		}
	}

	// Apply file templates after volume seeding AND after dep install so
	// file templates can substitute .Dep.KEY.Host / .Dep.KEY.Ports values.
	if len(compiled.Templates) > 0 {
		pw.Step("applying_templates")
		s.applyPackageTemplates(compiled, req.Responses, repoName, effectiveName, req.Version, ip.Description, depMap)
	}

	pw.Step("saving_install")
	if err := inst.Install(repoName, effectiveName, parentName, req.Version, req.Responses); err != nil {
		pw.Err(err)
		return nil
	}

	if err := inst.ClearLastResponses(repoName, effectiveName); err != nil {
		slog.Debug(fmt.Sprintf("clear last responses %s/%s: %v", repoName, effectiveName, err))
	}

	if activeVersion != "" && activeVersion != req.Version {
		if err := inst.Uninstall(repoName, effectiveName, activeVersion); err != nil {
			slog.Debug(fmt.Sprintf("remove old install record %s/%s@%s: %v", repoName, effectiveName, activeVersion, err))
		}
	}

	if req.Instance != "" {
		children, err := inst.LoadChildren(repoName, parentName)
		if err != nil {
			pw.Err(err)
			return nil
		}
		if !slices.Contains(children, req.Instance) {
			children = append(children, req.Instance)
			if err := inst.SaveChildren(repoName, parentName, children); err != nil {
				slog.Debug(fmt.Sprintf("save children %s/%s: %v", repoName, parentName, err))
			}
		}
	}

	// For VM packages, download and cache the VM image if it is a URL.
	if compiled.Runtime == packages.RuntimeVM && compiled.VM != nil {
		pw.Step("downloading_vm_image")
		if err := s.ensureVMImage(ctx, compiled.VM.Image); err != nil {
			pw.Err(fmt.Errorf("ensure vm image: %w", err))
			return nil
		}
	}

	if sd := s.Controller.GetSystemdManager(); sd != nil {
		pw.Step("installing_services")
		cfg := s.packageUnitConfig(repoName, effectiveName, req.Version, ip.Description, compiled, ip.Supplies)

		// Set dependency unit names on the parent so systemd orders them.
		depRecordsForUnits, err := inst.LoadDependencies(repoName, effectiveName)
		if err == nil && len(depRecordsForUnits) > 0 {
			for _, rec := range depRecordsForUnits {
				cfg.DependencyUnitNames = append(cfg.DependencyUnitNames, systemd.UnitName(rec.Repo, rec.EffectiveName, rec.Version))
			}
			sort.Strings(cfg.DependencyUnitNames)
		}

		// Resolve Expose: shared volume mounts the parent imports from
		// its just-installed deps. depRecordsForExpose / depCompiledForExpose
		// are populated above when len(compiled.Dependencies) > 0.
		if len(depRecordsForExpose) > 0 {
			exposeMounts, err := resolveExposeMounts(s.Controller.GetBtrfsBasePath(), compiled.Dependencies, depRecordsForExpose, func(rec packages.DependencyRecord) (*packages.Package, error) {
				key := packages.DepKey(rec.EffectiveName)
				if pkg, ok := depCompiledForExpose[key]; ok {
					return pkg, nil
				}
				return nil, fmt.Errorf("compiled package not cached for %s", rec.EffectiveName)
			})
			if err != nil {
				pw.Err(fmt.Errorf("resolve expose mounts: %w", err))
				return nil
			}
			cfg.HostVolumeMounts = append(cfg.HostVolumeMounts, exposeMounts...)
		}

		units := systemd.GeneratePackageUnits(cfg)
		if err := s.writePackageNetworkState(repoName, effectiveName, req.Version, compiled, ip.Supplies); err != nil {
			pw.Err(err)
			return nil
		}
		if err := s.installPackageUnits(ctx, sd, units); err != nil {
			pw.Err(err)
			return nil
		}
	}

	pw.Step("registering_dns")
	s.registerPackageDNS(ctx, repoName, effectiveName, compiled.Network.Domains)
	// Pin the proxy's leaf via DANE for any terminated TLS ports.
	s.publishPackageTLSA(ctx, repoName, effectiveName, req.Version, compiled.Network.Domains)

	// Re-render the shared :443 ingress so an HTTP package's reverse_proxy vhost
	// is served immediately (its leaf and DNS were set up above). Only HTTP
	// packages touch the ingress; others leave it untouched.
	if len(ingressHostPorts(compiled, ip.Supplies)) > 0 {
		s.refreshPages(ctx)
	}

	pw.Done()
	return nil
}

func (s *SystemControllerHandlers) uninstallPackage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := UninstallRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	// When Instance is set, the effective name becomes parentName-instance.
	parentName := req.Name
	effectiveName := req.Name
	if req.Instance != "" {
		effectiveName = fmt.Sprintf("%s-%s", parentName, req.Instance)
	}

	unlock := s.lockPackage(req.Repo, effectiveName)
	defer unlock()

	ctx := c.Request().Context()
	inst := s.Controller.GetInstaller()

	pw := NewProgressWriter(c)

	pw.Step("unregistering_dns")
	s.unregisterPackageDNS(ctx, req.Repo, parentName, effectiveName, req.Version)
	s.unpublishPackageTLSA(ctx, req.Repo, effectiveName)

	if sd := s.Controller.GetSystemdManager(); sd != nil {
		pw.Step("stopping_services")
		if err := s.uninstallPackageUnits(ctx, sd, req.Repo, effectiveName, req.Version); err != nil {
			pw.Err(err)
			return nil
		}
	}

	pw.Step("removing_network")
	s.removePackageNetworkState(req.Repo, effectiveName, req.Version)

	pw.Step("saving_responses")
	lastResp, err := inst.GetResponses(req.Repo, effectiveName, req.Version)
	if err == nil && len(lastResp) > 0 {
		if err := inst.SaveLastResponses(req.Repo, effectiveName, lastResp); err != nil {
			slog.Debug(fmt.Sprintf("save last responses %s/%s: %v", req.Repo, effectiveName, err))
		}
	}

	pw.Step("removing_install")
	if err := inst.SetDisabled(req.Repo, effectiveName, false); err != nil {
		pw.Err(err)
		return nil
	}
	if err := inst.Uninstall(req.Repo, effectiveName, req.Version); err != nil {
		pw.Err(err)
		return nil
	}

	// Remove child from parent's children list when uninstalling an instance.
	if req.Instance != "" {
		children, err := inst.LoadChildren(req.Repo, parentName)
		if err != nil {
			pw.Err(err)
			return nil
		}
		for i, ch := range children {
			if ch == req.Instance {
				children = append(children[:i], children[i+1:]...)
				break
			}
		}
		if err := inst.SaveChildren(req.Repo, parentName, children); err != nil {
			slog.Debug(fmt.Sprintf("save children %s/%s: %v", req.Repo, parentName, err))
		}
	}

	pw.Step("uninstalling_dependencies")
	s.uninstallDependencies(ctx, req.Repo, effectiveName)

	// Volume handling after uninstall. Dep volumes live nested under the
	// parent's on-disk tree, so a single top-level purge (or rename to
	// the uninstalled/ mirror) atomically handles every dep subtree in
	// one operation. The uninstallDependencies cascade above never
	// touched volumes — only metadata.
	pw.Step("cleaning_volumes")
	if req.PurgeVolumes {
		if err := s.purgePackageVolumes(req.Repo, effectiveName); err != nil {
			pw.Err(err)
			return nil
		}
	} else if st := s.Controller.GetStorage(); st != nil {
		_, otherVersionInstalled, err := inst.GetInstalledVersion(req.Repo, effectiveName)
		if err != nil {
			pw.Err(err)
			return nil
		}

		if !otherVersionInstalled {
			storage := packages.StoragePath(effectiveName)
			src := fmt.Sprintf("%s/%s/%s", PackagesVolumePrefix, req.Repo, storage)
			dst := fmt.Sprintf("%s/%s/%s", UninstalledVolumePrefix, req.Repo, storage)
			if err := st.RenameFilesystem(src, dst); err != nil {
				slog.Debug(fmt.Sprintf("preserve volumes: rename %s -> %s: %v", src, dst, err))
			}
		}
	}

	// Re-render the shared :443 ingress so the uninstalled package's vhost is
	// dropped (its DNS and state were already removed above) — but only if the
	// ingress is already in use, so removing a non-HTTP package never spins it
	// up. applyPages no-ops on systemd when the rendered config is unchanged.
	if ingressCaddyfileExists(s.Controller.GetBtrfsBasePath()) {
		s.refreshPages(ctx)
	}

	pw.Done()
	return nil
}

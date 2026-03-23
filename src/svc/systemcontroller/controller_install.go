package systemcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
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

	// Start the main service.
	if err := sd.SetStatus(ctx, units.Service.Name, systemd.Start); err != nil {
		return fmt.Errorf("start service: %w", err)
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
func (s *SystemControllerHandlers) packageUnitConfig(repoName, pkgName, version, description string, compiled *packages.Package) systemd.PackageUnitConfig {
	image := compiled.Image
	if image == "" && compiled.Proton != nil {
		image = s.protonImage()
	}
	cfg := systemd.PackageUnitConfig{
		RepoName:               repoName,
		PkgName:                pkgName,
		Version:                version,
		Description:            description,
		Image:                  image,
		Command:                compiled.Command,
		Environment:            compiled.Environment,
		External:               compiled.Network.External,
		Internal:               compiled.Network.Internal,
		Volumes:                compiled.Volumes,
		BtrfsBase:              s.Controller.GetBtrfsBasePath(),
		NetworkControllerImage: s.Controller.GetNetworkControllerImage(),
		NetworkStatePath:       s.Controller.GetNetworkStatePath(),
		Runtime:                compiled.Runtime,
		VM:                     compiled.VM,
	}
	if compiled.Runtime == packages.RuntimeVM && compiled.VM != nil {
		cfg.VMImagePath = resolveVMImagePath(s.Controller.GetBtrfsBasePath(), compiled.VM.Image)
	}
	return cfg
}

// protonImage returns the system-wide Proton runner container image from
// the proton_image setting. Returns an empty string if not configured.
func (s *SystemControllerHandlers) protonImage() string {
	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return ""
	}

	val, err := mgr.Get("proton_image")
	if err != nil {
		return ""
	}

	return val
}

// findActiveVersion returns the currently installed version for a package,
// or "" if not installed.
func findActiveVersion(inst packages.Installer, repoName, effectiveName string) (string, error) {
	installed, err := inst.ListInstalled()
	if err != nil {
		return "", err
	}
	for _, pkg := range installed {
		pi, err := packages.ParsePackageIdentity(pkg)
		if err != nil {
			continue
		}
		if pi.Repo == repoName && pi.Name == effectiveName {
			return pi.Version, nil
		}
	}
	return "", nil
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

	if activeVersion != "" {
		if err := s.prepareActiveVersion(ctx, rr, inst, repoName, parentName, effectiveName, activeVersion, req.Version, req.ImportFromVersion, compiled); err != nil {
			return err
		}
	}

	if err := s.provisionVolumes(repoName, effectiveName, req.Version, req.ImportFromVersion, req.ReuseVolumes, compiled); err != nil {
		return err
	}

	s.seedVolumeData(ctx, &ip, compiled, repoName, parentName, effectiveName, req.Version)

	// Apply file templates after volume seeding but before service boot.
	if len(compiled.Templates) > 0 {
		s.applyPackageTemplates(compiled, req.Responses, repoName, effectiveName, req.Version, ip.Description)
	}

	// Install dependencies before the parent package.
	if len(compiled.Dependencies) > 0 {
		depRecords, depEnvVars, err := s.installDependencies(ctx, repoName, effectiveName, compiled.Dependencies)
		if err != nil {
			return fmt.Errorf("install dependencies: %w", err)
		}

		// Inject dependency connection environment variables into the parent.
		if len(depEnvVars) > 0 {
			if compiled.Environment == nil {
				compiled.Environment = map[string]string{}
			}
			maps.Copy(compiled.Environment, depEnvVars)
		}

		// Save dependency records for uninstall.
		if len(depRecords) > 0 {
			if err := inst.SaveDependencies(repoName, effectiveName, depRecords); err != nil {
				return fmt.Errorf("save dependencies: %w", err)
			}
		}
	}

	if err := inst.Install(repoName, effectiveName, req.Version, req.Responses); err != nil {
		return err
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
			return err
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
		if err := s.ensureVMImage(ctx, compiled.VM.Image); err != nil {
			return fmt.Errorf("ensure vm image: %w", err)
		}
	}

	if sd := s.Controller.GetSystemdManager(); sd != nil {
		cfg := s.packageUnitConfig(repoName, effectiveName, req.Version, ip.Description, compiled)
		units := systemd.GeneratePackageUnits(cfg)
		if err := s.writePackageNetworkState(repoName, effectiveName, req.Version, compiled); err != nil {
			return err
		}
		if err := s.installPackageUnits(ctx, sd, units); err != nil {
			return err
		}
	}

	s.registerPackageDNS(ctx, repoName, effectiveName, compiled.Network.Domains)

	c.Response().WriteHeader(200)
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

	// Unregister DNS before removing units.
	s.unregisterPackageDNS(ctx, req.Repo, parentName, effectiveName, req.Version)

	if sd := s.Controller.GetSystemdManager(); sd != nil {
		if err := s.uninstallPackageUnits(ctx, sd, req.Repo, effectiveName, req.Version); err != nil {
			return err
		}
	}

	s.removePackageNetworkState(req.Repo, effectiveName, req.Version)

	// Save last responses before uninstall for potential reuse.
	lastResp, err := inst.GetResponses(req.Repo, effectiveName, req.Version)
	if err == nil && len(lastResp) > 0 {
		if err := inst.SaveLastResponses(req.Repo, effectiveName, lastResp); err != nil {
			slog.Debug(fmt.Sprintf("save last responses %s/%s: %v", req.Repo, effectiveName, err))
		}
	}

	if err := inst.SetDisabled(req.Repo, effectiveName, false); err != nil {
		return err
	}
	if err := inst.Uninstall(req.Repo, effectiveName, req.Version); err != nil {
		return err
	}

	// Remove child from parent's children list when uninstalling an instance.
	if req.Instance != "" {
		children, err := inst.LoadChildren(req.Repo, parentName)
		if err != nil {
			return err
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

	// Cascade-uninstall dependencies after the parent.
	s.uninstallDependencies(ctx, req.Repo, effectiveName, req.PurgeVolumes)

	// Volume handling after uninstall.
	if req.PurgeVolumes {
		if err := s.purgePackageVolumes(req.Repo, effectiveName); err != nil {
			return err
		}
	} else if st := s.Controller.GetStorage(); st != nil {
		// Check if any other versions remain installed for this repo/name.
		installed, err := inst.ListInstalled()
		if err != nil {
			return err
		}

		otherVersionInstalled := false
		for _, pkg := range installed {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			if pi.Repo == req.Repo && pi.Name == effectiveName {
				otherVersionInstalled = true
				break
			}
		}

		if !otherVersionInstalled {
			// Move installed/<repo>/<name> → uninstalled/<repo>/<name>.
			src := fmt.Sprintf("%s/%s/%s", PackagesVolumePrefix, req.Repo, effectiveName)
			dst := fmt.Sprintf("%s/%s/%s", UninstalledVolumePrefix, req.Repo, effectiveName)
			if err := st.RenameFilesystem(src, dst); err != nil {
				slog.Debug(fmt.Sprintf("preserve volumes: rename %s -> %s: %v", src, dst, err))
			}
		}
	}

	c.Response().WriteHeader(200)
	return nil
}

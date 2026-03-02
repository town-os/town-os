package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"gitea.com/town-os/town-os/src/i18n"
	"gitea.com/town-os/town-os/src/networkcontroller"
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
	Repo             string             `json:"repo"`
	Name             string             `json:"name"`
	Version          string             `json:"version"`
	Description      string             `json:"description,omitempty"`
	Image            string             `json:"image"`
	Volumes          []VolumePreview    `json:"volumes"`
	ExternalPorts    []PortPreview      `json:"external_ports"`
	InternalPorts    []PortPreview      `json:"internal_ports"`
	UpgradingFrom    string             `json:"upgrading_from,omitempty"`
	HasQuestions     bool               `json:"has_questions"`
	DiskUsage        *storage.DiskUsage `json:"disk_usage,omitempty"`
	TotalQuota       uint64             `json:"total_quota"`
	QuotaExceedsDisk bool               `json:"quota_exceeds_disk"`
	Summary          string             `json:"summary"`
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
// backend configuration.
func (s *SystemControllerHandlers) packageUnitConfig(repoName, pkgName, version, description string, compiled *packages.Package) systemd.PackageUnitConfig {
	return systemd.PackageUnitConfig{
		RepoName:                 repoName,
		PkgName:                  pkgName,
		Version:                  version,
		Description:              description,
		Image:                    compiled.Image,
		Command:                  compiled.Command,
		Environment:              compiled.Environment,
		External:                 compiled.Network.External,
		Internal:                 compiled.Network.Internal,
		Volumes:                  compiled.Volumes,
		BtrfsBase:                s.Controller.GetBtrfsBasePath(),
		NetworkControllerBinPath: s.Controller.GetNetworkControllerBinPath(),
		NetworkStatePath:         s.Controller.GetNetworkStatePath(),
		NetworkMode:              s.Controller.GetNetworkMode(),
	}
}

// writePackageNetworkState writes the per-package JSON state file consumed by
// the networkcontroller daemon.
func (s *SystemControllerHandlers) writePackageNetworkState(repoName, pkgName, version string, compiled *packages.Package) error {
	statePath := s.Controller.GetNetworkStatePath()
	if statePath == "" {
		return nil
	}

	state := networkcontroller.PackageNetworkState{
		Repo:        repoName,
		Package:     pkgName,
		Version:     version,
		NetworkMode: s.Controller.GetNetworkMode(),
	}

	for ext, int_ := range compiled.Network.External {
		forward := s.Controller.GetNetworkMode() == "host" && ext != int_
		state.Ports = append(state.Ports, networkcontroller.PortConfig{
			ExternalPort: ext,
			InternalPort: int_,
			UPnP:         true,
			Forward:      forward,
		})
	}

	for intHost, intContainer := range compiled.Network.Internal {
		if s.Controller.GetNetworkMode() == "host" && intHost != intContainer {
			state.Ports = append(state.Ports, networkcontroller.PortConfig{
				ExternalPort: intHost,
				InternalPort: intContainer,
				UPnP:         false,
				Forward:      true,
			})
		}
	}

	if len(state.Ports) == 0 {
		return nil
	}

	// Sort for deterministic output.
	sort.Slice(state.Ports, func(i, j int) bool {
		return state.Ports[i].ExternalPort < state.Ports[j].ExternalPort
	})

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal network state: %w", err)
	}

	filePath := fmt.Sprintf("%s/%s-%s-%s.json", statePath, repoName, pkgName, version)
	if err := os.MkdirAll(statePath, 0700); err != nil {
		return fmt.Errorf("create network state dir: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("write network state: %w", err)
	}

	return nil
}

// removePackageNetworkState removes the per-package network state file.
func (s *SystemControllerHandlers) removePackageNetworkState(repoName, pkgName, version string) {
	statePath := s.Controller.GetNetworkStatePath()
	if statePath == "" {
		return
	}

	filePath := fmt.Sprintf("%s/%s-%s-%s.json", statePath, repoName, pkgName, version)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		slog.Debug(fmt.Sprintf("remove network state %s: %v", filePath, err))
	}
}

func (s *SystemControllerHandlers) installPreview(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := InstallPreviewRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()
	if rr == nil {
		return errors.New(i18n.T(s.getLocale(), i18n.MsgInstallNoRepoRoot))
	}

	repoName, err := rr.FindRepoForPackage(req.Name, req.Version)
	if err != nil {
		return err
	}
	if req.Repo != "" && req.Repo != repoName {
		repoName = req.Repo
	}

	ip, err := rr.LoadPackage(repoName, req.Name, req.Version)
	if err != nil {
		return err
	}

	// Find currently installed version.
	inst := s.Controller.GetInstaller()
	var activeVersion string
	if inst != nil {
		installed, err := inst.ListInstalled()
		if err != nil {
			return err
		}
		for _, pkg := range installed {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			if pi.Repo == repoName && pi.Name == req.Name {
				activeVersion = pi.Version
				break
			}
		}
	}

	// Load old package volumes for migration detection.
	var oldVolumes map[string]packages.InputPackageVolume
	if activeVersion != "" && activeVersion != req.Version {
		oldIP, err := rr.LoadPackage(repoName, req.Name, activeVersion)
		if err == nil {
			oldVolumes = oldIP.Volumes
		}
	}

	// Build volume previews.
	var volumes []VolumePreview
	var totalQuota uint64
	for volName, vol := range ip.Volumes {
		migrated := false
		fresh := true
		if oldVolumes != nil {
			if _, exists := oldVolumes[volName]; exists {
				migrated = true
				fresh = false
			}
		}
		volumes = append(volumes, VolumePreview{
			Name:       volName,
			Mountpoint: vol.Mountpoint,
			Quota:      vol.Quota,
			Migrated:   migrated,
			Fresh:      fresh,
		})
		if vol.Quota != "" {
			q, err := packages.ParseBytes(vol.Quota)
			if err == nil {
				totalQuota += q
			}
		}
	}

	// Sort volumes by name for deterministic output.
	sort.Slice(volumes, func(i, j int) bool {
		return volumes[i].Name < volumes[j].Name
	})

	// Build port previews.
	var externalPorts []PortPreview
	for ext, intStr := range ip.Network.External {
		extPort, err := strconv.ParseUint(ext, 10, 16)
		if err != nil {
			continue
		}
		intPort, err := strconv.ParseUint(intStr, 10, 16)
		if err != nil {
			continue
		}
		externalPorts = append(externalPorts, PortPreview{
			External: uint16(extPort),
			Internal: uint16(intPort),
		})
	}
	sort.Slice(externalPorts, func(i, j int) bool {
		return externalPorts[i].External < externalPorts[j].External
	})

	var internalPorts []PortPreview
	for ext, intStr := range ip.Network.Internal {
		extPort, err := strconv.ParseUint(ext, 10, 16)
		if err != nil {
			continue
		}
		intPort, err := strconv.ParseUint(intStr, 10, 16)
		if err != nil {
			continue
		}
		internalPorts = append(internalPorts, PortPreview{
			External: uint16(extPort),
			Internal: uint16(intPort),
		})
	}
	sort.Slice(internalPorts, func(i, j int) bool {
		return internalPorts[i].External < internalPorts[j].External
	})

	preview := InstallPreview{
		Repo:          repoName,
		Name:          req.Name,
		Version:       req.Version,
		Description:   ip.Description,
		Image:         ip.Image,
		Volumes:       volumes,
		ExternalPorts: externalPorts,
		InternalPorts: internalPorts,
		HasQuestions:  len(ip.Questions) > 0,
		TotalQuota:    totalQuota,
	}

	if activeVersion != "" && activeVersion != req.Version {
		preview.UpgradingFrom = activeVersion
	}

	// Disk usage and quota warning.
	if st := s.Controller.GetStorage(); st != nil {
		du, err := st.DiskUsage()
		if err == nil {
			preview.DiskUsage = &du
			reserved := du.Total * 5 / 100
			var effectiveAvailable uint64
			if du.Available > reserved {
				effectiveAvailable = du.Available - reserved
			}
			if totalQuota > 0 && totalQuota > effectiveAvailable {
				preview.QuotaExceedsDisk = true
			}
		}
	}

	if preview.Volumes == nil {
		preview.Volumes = []VolumePreview{}
	}
	if preview.ExternalPorts == nil {
		preview.ExternalPorts = []PortPreview{}
	}
	if preview.InternalPorts == nil {
		preview.InternalPorts = []PortPreview{}
	}

	preview.Summary = buildInstallSummary(&preview, s.getLocale())

	return c.JSON(200, preview)
}

// buildInstallSummary generates a human-readable summary of the install operation.
// The locale parameter determines the language used for the summary text.
func buildInstallSummary(p *InstallPreview, locale string) string {
	var parts []string

	if p.UpgradingFrom != "" {
		parts = append(parts, i18n.T(locale, i18n.MsgInstallSummaryUpgrade, p.Name, p.UpgradingFrom, p.Version))
	} else {
		parts = append(parts, i18n.T(locale, i18n.MsgInstallSummaryInstall, p.Name, p.Version))
	}

	parts = append(parts, i18n.T(locale, i18n.MsgInstallSummaryImage, p.Image))

	if len(p.Volumes) > 0 {
		fresh := 0
		migrated := 0
		for _, v := range p.Volumes {
			if v.Migrated {
				migrated++
			}
			if v.Fresh {
				fresh++
			}
		}
		volParts := []string{i18n.T(locale, i18n.MsgInstallSummaryVolumes, len(p.Volumes))}
		if fresh > 0 {
			volParts = append(volParts, i18n.T(locale, i18n.MsgInstallSummaryNewVols, fresh))
		}
		if migrated > 0 {
			volParts = append(volParts, i18n.T(locale, i18n.MsgInstallSummaryMigrated, migrated))
		}
		parts = append(parts, strings.Join(volParts, ", "))
	} else {
		parts = append(parts, i18n.T(locale, i18n.MsgInstallSummaryNoVols))
	}

	if len(p.ExternalPorts) > 0 {
		portStrs := make([]string, len(p.ExternalPorts))
		for i, port := range p.ExternalPorts {
			portStrs[i] = fmt.Sprintf("%d->%d", port.External, port.Internal)
		}
		parts = append(parts, i18n.T(locale, i18n.MsgInstallSummaryPorts, strings.Join(portStrs, ", ")))
	}

	if p.HasQuestions {
		parts = append(parts, i18n.T(locale, i18n.MsgInstallSummaryConfig))
	}

	return strings.Join(parts, ". ") + "."
}

// mergeResponses carries forward responses from a previous version or last
// uninstall into the request, filling only keys that the user did not provide
// and that exist in the new package's questions.
func mergeResponses(dst *packages.Responses, src packages.Responses, questions map[string]packages.Question) {
	for key, val := range src {
		if _, exists := questions[key]; !exists {
			continue
		}
		if _, provided := (*dst)[key]; provided {
			continue
		}
		if *dst == nil {
			*dst = packages.Responses{}
		}
		(*dst)[key] = val
	}
}

// autoGenerateResponses fills empty or "auto" response values with generated
// ports, hostnames, secrets, or defaults.
func (s *SystemControllerHandlers) autoGenerateResponses(responses *packages.Responses, questions map[string]packages.Question, effectiveName string) error {
	inst := s.Controller.GetInstaller()
	excludedPorts := map[uint16]bool{}
	if inst != nil {
		allInstalled, err := inst.ListInstalled()
		if err != nil {
			return err
		}
		for _, pkg := range allInstalled {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			resp, err := inst.GetResponses(pi.Repo, pi.Name, pi.Version)
			if err != nil {
				continue
			}
			for _, v := range resp {
				if p, err := strconv.ParseUint(v, 10, 16); err == nil && p > 0 {
					excludedPorts[uint16(p)] = true
				}
			}
		}
	}

	for name, q := range questions {
		resp := (*responses)[name]
		if resp != "" && resp != "auto" {
			continue
		}

		switch q.Type {
		case packages.Port:
			port, err := packages.FindAvailablePort(excludedPorts)
			if err != nil {
				continue
			}
			if *responses == nil {
				*responses = packages.Responses{}
			}
			(*responses)[name] = strconv.FormatUint(uint64(port), 10)
			excludedPorts[port] = true
		case packages.Hostname:
			if *responses == nil {
				*responses = packages.Responses{}
			}
			(*responses)[name] = packages.GenerateHostname(effectiveName)
		case packages.Secret:
			secret, err := packages.GenerateSecret()
			if err != nil {
				continue
			}
			if *responses == nil {
				*responses = packages.Responses{}
			}
			(*responses)[name] = secret
		default:
			if (resp == "auto" || resp == "") && q.Default != "" {
				if *responses == nil {
					*responses = packages.Responses{}
				}
				(*responses)[name] = q.Default
			}
		}
	}
	return nil
}

// prepareActiveVersion stops running units for the active version and
// migrates volumes when upgrading.
func (s *SystemControllerHandlers) prepareActiveVersion(ctx context.Context, rr *packages.RepositoryRoot, inst packages.Installer, repoName, parentName, effectiveName, activeVersion, newVersion, importFromVersion string, compiled *packages.Package) error {
	// Stop and remove all systemd units for the currently active version.
	if sd := s.Controller.GetSystemdManager(); sd != nil {
		if err := s.uninstallPackageUnits(ctx, sd, repoName, effectiveName, activeVersion); err != nil {
			return err
		}
	}

	if activeVersion == newVersion {
		// Same version reinstall: remove the install record (but not volumes).
		return inst.Uninstall(repoName, effectiveName, newVersion)
	}

	// Different version (upgrade): move matching volumes.
	if st := s.Controller.GetStorage(); st != nil && importFromVersion == "" {
		oldIP, loadErr := rr.LoadPackage(repoName, parentName, activeVersion)
		if loadErr == nil {
			for volName := range compiled.Volumes {
				if _, exists := oldIP.Volumes[volName]; exists {
					src := packageVolumePath(repoName, effectiveName, activeVersion, volName)
					dst := packageVolumePath(repoName, effectiveName, newVersion, volName)
					if err := st.RenameFilesystem(src, dst); err != nil {
						slog.Debug(fmt.Sprintf("upgrade move volume %s -> %s: %v", src, dst, err))
					}
				}
			}
		} else {
			slog.Debug(fmt.Sprintf("upgrade: load old package %s/%s@%s: %v", repoName, parentName, activeVersion, loadErr))
		}
	}
	return nil
}

// provisionVolumes creates, reuses, or imports storage volumes for the package.
func (s *SystemControllerHandlers) provisionVolumes(repoName, effectiveName, version, importFromVersion string, reuseVolumes bool, compiled *packages.Package) error {
	st := s.Controller.GetStorage()
	if st == nil {
		return nil
	}

	if reuseVolumes {
		src := fmt.Sprintf("%s/%s/%s", UninstalledVolumePrefix, repoName, effectiveName)
		dst := fmt.Sprintf("%s/%s/%s", PackagesVolumePrefix, repoName, effectiveName)
		if err := st.RenameFilesystem(src, dst); err != nil {
			slog.Debug(fmt.Sprintf("reuse volumes: rename %s -> %s: %v", src, dst, err))
		}
	}

	defQuota := s.defaultQuota()
	for volName, vol := range compiled.Volumes {
		quota := vol.Quota
		if quota == 0 {
			quota = defQuota
		}
		fsName := packageVolumePath(repoName, effectiveName, version, volName)

		if importFromVersion != "" {
			srcVol := packageVolumePath(repoName, effectiveName, importFromVersion, volName)
			if err := st.SnapshotFilesystem(srcVol, fsName); err != nil {
				slog.Debug(fmt.Sprintf("import snapshot %s -> %s: %v", srcVol, fsName, err))
				if err := st.CreateFilesystem(storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
					if err := st.ModifyFilesystem(fsName, storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
						return err
					}
				}
			} else {
				if err := st.ModifyFilesystem(fsName, storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
					slog.Debug(fmt.Sprintf("adjust quota on snapshot %s: %v", fsName, err))
				}
			}
		} else {
			if err := st.CreateFilesystem(storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
				if err := st.ModifyFilesystem(fsName, storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// seedVolumeData populates volumes with auto-archives, git seeds, and
// git_sources after they are created.
func (s *SystemControllerHandlers) seedVolumeData(ctx context.Context, ip *packages.InputPackage, compiled *packages.Package, repoName, parentName, effectiveName, version string) {
	if len(ip.Archives) > 0 {
		for _, archive := range ip.Archives {
			volPath := packageVolumePath(repoName, parentName, version, archive.Volume)
			if err := s.extractFromContainerImage(ctx, archive.Image, archive.Directory, volPath); err != nil {
				slog.Debug(fmt.Sprintf("auto-archive %s -> %s: %v", archive.Image, archive.Volume, err))
			}
		}
	}

	for volName, vol := range compiled.Volumes {
		if vol.Git == "" {
			continue
		}
		volPath := packageVolumePath(repoName, effectiveName, version, volName)
		targetPath := filepath.Join(s.Controller.GetBtrfsBasePath(), volPath)
		entries, err := os.ReadDir(targetPath)
		if err != nil || len(entries) > 0 {
			continue
		}
		if err := gitCloneIntoPath(ctx, vol.Git, targetPath); err != nil {
			slog.Debug(fmt.Sprintf("git-seed %s -> %s: %v", vol.Git, volName, err))
		}
	}

	if len(ip.GitSources) > 0 {
		cloner := s.Controller.GetGitCloner()
		for _, gs := range ip.GitSources {
			volPath := packageVolumePath(repoName, effectiveName, version, gs.Volume)
			targetDir := filepath.Join(s.Controller.GetBtrfsBasePath(), volPath)
			if err := cloner.Clone(targetDir, gs.URL, gs.Branch); err != nil {
				slog.Debug(fmt.Sprintf("git-source clone %s -> %s: %v", gs.URL, gs.Volume, err))
			}
		}
	}
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

	if sd := s.Controller.GetSystemdManager(); sd != nil {
		cfg := s.packageUnitConfig(repoName, effectiveName, req.Version, ip.Description, compiled)
		units := systemd.GeneratePackageUnits(cfg)
		if err := s.writePackageNetworkState(repoName, req.Name, req.Version, compiled); err != nil {
			return err
		}
		if err := s.installPackageUnits(ctx, sd, units); err != nil {
			return err
		}
	}

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

	ctx := c.Request().Context()
	inst := s.Controller.GetInstaller()

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

func (s *SystemControllerHandlers) purgePackageVolumes(repo, name string) error {
	st := s.Controller.GetStorage()
	if st == nil {
		return nil
	}

	// Purge from installed/<repo>/<name>/.
	if err := s.purgeVolumePrefix(st, packagePrefix(repo, name)); err != nil {
		return err
	}

	// Remove the installed/<repo>/<name> parent subvolume itself.
	parentPath := fmt.Sprintf("%s/%s/%s", PackagesVolumePrefix, repo, name)
	if err := st.RemoveFilesystem(parentPath); err != nil {
		slog.Debug(fmt.Sprintf("purge parent volume %s: %v", parentPath, err))
	}

	// Also purge from uninstalled/<repo>/<name>/.
	uninstPrefix := fmt.Sprintf("%s/%s/%s/", UninstalledVolumePrefix, repo, name)
	if err := s.purgeVolumePrefix(st, uninstPrefix); err != nil {
		return err
	}

	uninstParent := fmt.Sprintf("%s/%s/%s", UninstalledVolumePrefix, repo, name)
	if err := st.RemoveFilesystem(uninstParent); err != nil {
		slog.Debug(fmt.Sprintf("purge uninstalled parent volume %s: %v", uninstParent, err))
	}

	return nil
}

func (s *SystemControllerHandlers) purgeVolumePrefix(st storage.Storage, prefix string) error {
	filesystems, err := st.ListFilesystems(prefix)
	if err != nil {
		return err
	}

	// Sort deepest-first so child subvolumes are removed before parents.
	sort.Slice(filesystems, func(i, j int) bool {
		return strings.Count(filesystems[i].Name, "/") > strings.Count(filesystems[j].Name, "/")
	})

	for _, fs := range filesystems {
		if err := st.RemoveFilesystem(fs.Name); err != nil {
			return err
		}
	}

	return nil
}

func (s *SystemControllerHandlers) purgeVolumes(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if err := s.purgePackageVolumes(req.Repo, req.Name); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

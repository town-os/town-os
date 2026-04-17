package systemcontroller

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gitea.com/town-os/town-os/src/i18n"
	"gitea.com/town-os/town-os/src/packages"
	"github.com/labstack/echo/v5"
)

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

	// Look up the currently installed version directly.
	inst := s.Controller.GetInstaller()
	var activeVersion string
	if inst != nil {
		version, ok, err := inst.GetInstalledVersion(repoName, req.Name)
		if err != nil {
			return err
		}
		if ok {
			activeVersion = version
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

	previewImage := s.resolvePreviewProtonImage(ip.Image.URL, &ip)

	preview := InstallPreview{
		Repo:          repoName,
		Name:          req.Name,
		Version:       req.Version,
		Description:   ip.Description,
		Image:         previewImage,
		Runtime:       string(ip.RuntimeType()),
		Volumes:       volumes,
		ExternalPorts: externalPorts,
		InternalPorts: internalPorts,
		HasQuestions:  len(ip.Questions) > 0,
		TotalQuota:    totalQuota,
		VM:            ip.VM,
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

	if p.Runtime == string(packages.RuntimeVM) && p.VM != nil {
		parts = append(parts, i18n.T(locale, i18n.MsgInstallSummaryVMImage, p.VM.Image))
	} else {
		parts = append(parts, i18n.T(locale, i18n.MsgInstallSummaryImage, p.Image))
	}

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

// applyPackageTemplates renders Go text/template files into the appropriate
// volume directories. Templates are applied after volume seeding (archives,
// git clones) but before the service is started. Existing files are not
// overwritten, ensuring user-uploaded data is preserved.
func (s *SystemControllerHandlers) applyPackageTemplates(compiled *packages.Package, responses packages.Responses, repoName, effectiveName, version, description string) {
	btrfsBase := s.Controller.GetBtrfsBasePath()
	data := packages.TemplateData{
		Responses: responses,
		Package: packages.TemplatePackageInfo{
			Name:        effectiveName,
			Version:     version,
			Repo:        repoName,
			Image:       compiled.Image,
			Description: description,
		},
		System: packages.TemplateSystemInfo{
			ExternalIP: s.Controller.GetExternalIP(),
			InternalIP: s.Controller.GetInternalIP(),
		},
	}

	hostname, err := os.Hostname()
	if err == nil {
		data.System.Hostname = hostname
	}

	if err := packages.ApplyPackageTemplates(compiled.Templates, data, func(volName string) string {
		return filepath.Join(btrfsBase, packageVolumePath(repoName, effectiveName, version, volName))
	}); err != nil {
		slog.Debug(fmt.Sprintf("apply templates %s/%s@%s: %v", repoName, effectiveName, version, err))
	}
}

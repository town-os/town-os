package systemcontroller

import (
	"encoding/json"
	"fmt"
	"strings"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

type PackageNameRequest struct {
	Repo string `json:"repo"`
	Name string `json:"name"`
}

type PackageIdentityRequest struct {
	Repo    string `json:"repo"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type PackageListEntry struct {
	Repo             string   `json:"repo"`
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	Description      string   `json:"description,omitempty"`
	Supplies         []string `json:"supplies,omitempty"`
	Runtime          string   `json:"runtime"`
	Installed        bool     `json:"installed"`
	InstalledVersion string   `json:"installed_version,omitempty"`
	Featured         bool     `json:"featured,omitempty"`
	Changed          bool     `json:"changed,omitempty"`
}

type PackageToggleRequest struct {
	Repo string `json:"repo"`
	Name string `json:"name"`
}

type GetResponsesRequest struct {
	Repo    string `json:"repo"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ListChildrenRequest struct {
	Repo string `json:"repo"`
	Name string `json:"name"`
}

type InstalledInfoResponse struct {
	Questions map[string]packages.Question `json:"questions"`
	Responses packages.Responses           `json:"responses"`
	Notes     map[string]string            `json:"notes"`
	NoteTypes map[string]packages.NoteType `json:"note_types,omitempty"`
}

type UninstalledVolumesResponse struct {
	HasUninstalledVolumes bool     `json:"has_uninstalled_volumes"`
	UninstalledVersions   []string `json:"uninstalled_versions,omitempty"`
	InstalledVersions     []string `json:"installed_versions,omitempty"`
}

// FeaturedPackageEntry represents a featured package with its description and
// install status for use in the featured repositories card.
type FeaturedPackageEntry struct {
	Repo             string `json:"repo"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	Description      string `json:"description,omitempty"`
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installed_version,omitempty"`
	ServiceStatus    string `json:"service_status,omitempty"`
}

// FeaturedRepoGroup groups featured packages by repository.
type FeaturedRepoGroup struct {
	Repo     string                 `json:"repo"`
	Packages []FeaturedPackageEntry `json:"packages"`
}

// --- Package handlers ---

func (s *SystemControllerHandlers) listPackages(c *echo.Context) error {
	rr := s.Controller.GetRepositoryRoot()

	pkgStrings, err := rr.ListPackages()
	if err != nil {
		return err
	}

	// Build a map of installed repo/name keys to their installed version.
	// Dependency sub-packages (parent--dep--child) are intentionally
	// excluded so they never appear as separate tiles in the packages UI;
	// they belong to the parent's lifecycle.
	installedVersions := map[string]string{}
	inst := s.Controller.GetInstaller()
	if inst != nil {
		installed, err := inst.ListInstalled()
		if err != nil {
			return err
		}
		installed = filterDependencyInstalls(installed)

		for _, pkg := range installed {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			key := fmt.Sprintf("%s/%s", pi.Repo, pi.Name)
			installedVersions[key] = pi.Version
		}

		// Merge installed packages that may not be the latest version.
		known := map[string]bool{}
		for _, pkg := range pkgStrings {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			key := fmt.Sprintf("%s/%s", pi.Repo, pi.Name)
			known[key] = true
		}

		for _, pkg := range installed {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			key := fmt.Sprintf("%s/%s", pi.Repo, pi.Name)
			if !known[key] {
				pkgStrings = append(pkgStrings, pkg)
				known[key] = true
			}
		}
	}

	// Build a set of featured packages per repo.
	featuredSet := map[string]bool{}
	groups, grpErr := rr.ListPackagesByRepo()
	if grpErr == nil {
		for _, g := range groups {
			for _, f := range g.Featured {
				featuredSet[fmt.Sprintf("%s/%s", g.Repo, f)] = true
			}
		}
	}

	// Build structured entries with description/supplies from manifest.
	entries := make([]PackageListEntry, 0, len(pkgStrings))
	for _, pkg := range pkgStrings {
		pi, err := packages.ParsePackageIdentity(pkg)
		if err != nil {
			continue
		}

		key := fmt.Sprintf("%s/%s", pi.Repo, pi.Name)
		instVer, isInstalled := installedVersions[key]
		entry := PackageListEntry{
			Repo:             pi.Repo,
			Name:             pi.Name,
			Version:          pi.Version,
			Installed:        isInstalled,
			InstalledVersion: instVer,
			Featured:         featuredSet[key],
		}

		// Try to load manifest for description/supplies.
		ip, loadErr := rr.LoadPackage(pi.Repo, pi.Name, pi.Version)
		if loadErr == nil {
			entry.Description = ip.Description
			entry.Supplies = ip.Supplies
			entry.Runtime = string(ip.RuntimeType())
		}

		// Check if installed package file has changed.
		if isInstalled && inst != nil {
			changed, err := inst.IsPackageChanged(pi.Repo, pi.Name, instVer)
			if err == nil && changed {
				entry.Changed = true
			}
		}

		entries = append(entries, entry)
	}

	p := readListParams(c)

	if p.InstalledOnly {
		filtered := make([]PackageListEntry, 0, len(entries))
		for _, e := range entries {
			if e.Installed {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	if p.FeaturedOnly {
		filtered := make([]PackageListEntry, 0, len(entries))
		for _, e := range entries {
			if e.Featured {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	entries = filterSearch(entries, p.Search)
	sortSlice(entries, p.SortBy, p.SortOrder)

	return c.JSON(200, paginate(entries, p.Limit, p.Offset))
}

func (s *SystemControllerHandlers) listPackagesByRepo(c *echo.Context) error {
	rr := s.Controller.GetRepositoryRoot()

	groups, err := rr.ListPackagesByRepo()
	if err != nil {
		return err
	}

	p := readListParams(c)
	if p.Search != "" {
		searchLower := strings.ToLower(p.Search)
		var filtered []packages.RepoPackageGroup
		for _, g := range groups {
			var matching []packages.PackageIdentity
			for _, pkg := range g.Packages {
				if strings.Contains(strings.ToLower(pkg.Name), searchLower) {
					matching = append(matching, pkg)
				}
			}
			if len(matching) > 0 {
				filtered = append(filtered, packages.RepoPackageGroup{Repo: g.Repo, Packages: matching, Featured: g.Featured})
			}
		}
		groups = filtered
	}

	if p.FeaturedOnly {
		var filtered []packages.RepoPackageGroup
		for _, g := range groups {
			featuredSet := map[string]bool{}
			for _, f := range g.Featured {
				featuredSet[f] = true
			}
			var matching []packages.PackageIdentity
			for _, pkg := range g.Packages {
				if featuredSet[pkg.Name] {
					matching = append(matching, pkg)
				}
			}
			if len(matching) > 0 {
				filtered = append(filtered, packages.RepoPackageGroup{Repo: g.Repo, Packages: matching, Featured: g.Featured})
			}
		}
		groups = filtered
	}

	return c.JSON(200, groups)
}

func (s *SystemControllerHandlers) listFeaturedPackages(c *echo.Context) error {
	rr := s.Controller.GetRepositoryRoot()

	groups, err := rr.ListPackagesByRepo()
	if err != nil {
		return err
	}

	// Build installed version lookup.
	installedVersions := map[string]string{}
	if inst := s.Controller.GetInstaller(); inst != nil {
		installed, err := inst.ListInstalled()
		if err != nil {
			return err
		}
		for _, pkg := range installed {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			installedVersions[fmt.Sprintf("%s/%s", pi.Repo, pi.Name)] = pi.Version
		}
	}

	// Build unit status lookup for installed packages.
	unitStatusMap := map[string]string{}
	if sd := s.Controller.GetSystemdManager(); sd != nil {
		units, unitsErr := sd.ListUnits(c.Request().Context())
		if unitsErr == nil {
			for _, u := range units {
				if systemd.IsPackageServiceUnit(u.Name) {
					unitStatusMap[u.Name] = u.ActiveState
				}
			}
		}
	}

	var result []FeaturedRepoGroup
	for _, g := range groups {
		if len(g.Featured) == 0 {
			continue
		}

		featuredSet := map[string]bool{}
		for _, f := range g.Featured {
			featuredSet[f] = true
		}

		var entries []FeaturedPackageEntry
		for _, pkg := range g.Packages {
			if !featuredSet[pkg.Name] {
				continue
			}

			entry := FeaturedPackageEntry{
				Repo:    pkg.Repo,
				Name:    pkg.Name,
				Version: pkg.Version,
			}

			key := fmt.Sprintf("%s/%s", pkg.Repo, pkg.Name)
			if instVer, ok := installedVersions[key]; ok {
				entry.Installed = true
				entry.InstalledVersion = instVer

				unitName := systemd.UnitName(pkg.Repo, pkg.Name, instVer)
				if status, ok := unitStatusMap[unitName]; ok {
					entry.ServiceStatus = status
				}
			}

			ip, loadErr := rr.LoadPackage(pkg.Repo, pkg.Name, pkg.Version)
			if loadErr == nil {
				entry.Description = ip.Description
			}

			entries = append(entries, entry)
		}

		if len(entries) > 0 {
			result = append(result, FeaturedRepoGroup{
				Repo:     g.Repo,
				Packages: entries,
			})
		}
	}

	if result == nil {
		result = []FeaturedRepoGroup{}
	}

	return c.JSON(200, result)
}

func (s *SystemControllerHandlers) listPackageVersions(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()

	versions, err := rr.ListPackageVersions(req.Name)
	if err != nil {
		return err
	}

	return c.JSON(200, versions)
}

func (s *SystemControllerHandlers) getPackageQuestions(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()

	questions, err := rr.GetPackageQuestions(req.Name)
	if err != nil {
		return err
	}

	return c.JSON(200, questions)
}

func (s *SystemControllerHandlers) getPackageQuestionsByIdentity(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageIdentityRequest{}

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

	ip, err := rr.LoadPackage(repoName, req.Name, req.Version)
	if err != nil {
		return err
	}

	return c.JSON(200, ip.Questions)
}

package systemcontroller

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
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

// --- Package handlers ---

func (s *SystemControllerHandlers) listPackages(c *echo.Context) error {
	rr := s.Controller.GetRepositoryRoot()

	pkgStrings, err := rr.ListPackages()
	if err != nil {
		return err
	}

	// Build a map of installed repo/name keys to their installed version.
	installedVersions := map[string]string{}
	inst := s.Controller.GetInstaller()
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
				filtered = append(filtered, packages.RepoPackageGroup{Repo: g.Repo, Packages: matching})
			}
		}
		groups = filtered
	}

	return c.JSON(200, groups)
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

func (s *SystemControllerHandlers) listTimezones(c *echo.Context) error {
	return c.JSON(200, packages.ListTimezones())
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

func (s *SystemControllerHandlers) listInstalled(c *echo.Context) error {
	inst := s.Controller.GetInstaller()

	pkgs, err := inst.ListInstalled()
	if err != nil {
		return err
	}

	p := readListParams(c)
	pkgs = filterSearch(pkgs, p.Search)
	sort.Strings(pkgs)
	if strings.EqualFold(p.SortOrder, "desc") {
		sort.Sort(sort.Reverse(sort.StringSlice(pkgs)))
	}

	return c.JSON(200, paginate(pkgs, p.Limit, p.Offset))
}

func (s *SystemControllerHandlers) getResponses(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := GetResponsesRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	resp, err := inst.GetResponses(req.Repo, req.Name, req.Version)
	if err != nil {
		if errors.Is(err, packages.ErrNotInstalled) {
			return c.JSON(200, packages.Responses{})
		}
		return err
	}

	return c.JSON(200, resp)
}

func (s *SystemControllerHandlers) getInstalledInfo(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageIdentityRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	responses, err := inst.GetResponses(req.Repo, req.Name, req.Version)
	if err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()

	ip, err := rr.LoadPackage(req.Repo, req.Name, req.Version)
	if err != nil {
		return err
	}

	notes, err := ip.CompileNotes(responses)
	if err != nil {
		return err
	}

	var noteTypes map[string]packages.NoteType
	for k, note := range ip.Notes {
		if note.Type != "" {
			if noteTypes == nil {
				noteTypes = make(map[string]packages.NoteType)
			}
			noteTypes[k] = note.Type
		}
	}

	return c.JSON(200, InstalledInfoResponse{
		Questions: ip.Questions,
		Responses: responses,
		Notes:     notes,
		NoteTypes: noteTypes,
	})
}

func (s *SystemControllerHandlers) listChildren(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := ListChildrenRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	children, err := inst.LoadChildren(req.Repo, req.Name)
	if err != nil {
		return err
	}
	if children == nil {
		children = []string{}
	}

	return c.JSON(200, children)
}

func (s *SystemControllerHandlers) listUninstalledVolumes(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	resp := UninstalledVolumesResponse{}

	st := s.Controller.GetStorage()
	if st != nil {
		// Check uninstalled/<repo>/<name>/ for existing volume trees.
		uninstPrefix := fmt.Sprintf("%s/%s/%s/", UninstalledVolumePrefix, req.Repo, req.Name)
		filesystems, err := st.ListFilesystems(uninstPrefix)
		if err != nil {
			return err
		}

		if len(filesystems) > 0 {
			resp.HasUninstalledVolumes = true
			// Extract unique versions from uninstalled/<repo>/<name>/<version>/...
			versionSet := map[string]bool{}
			for _, fs := range filesystems {
				rest := strings.TrimPrefix(fs.Name, uninstPrefix)
				parts := strings.SplitN(rest, "/", 2)
				if len(parts) > 0 && parts[0] != "" {
					versionSet[parts[0]] = true
				}
			}
			for v := range versionSet {
				resp.UninstalledVersions = append(resp.UninstalledVersions, v)
			}
			sort.Strings(resp.UninstalledVersions)
		}

		// Extract installed versions from installed/<repo>/<name>/<version>/...
		instPrefix := packagePrefix(req.Repo, req.Name)
		instFS, err := st.ListFilesystems(instPrefix)
		if err != nil {
			return err
		}

		instVersionSet := map[string]bool{}
		for _, fs := range instFS {
			rest := strings.TrimPrefix(fs.Name, instPrefix)
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) > 0 && parts[0] != "" {
				instVersionSet[parts[0]] = true
			}
		}
		for v := range instVersionSet {
			resp.InstalledVersions = append(resp.InstalledVersions, v)
		}
		sort.Strings(resp.InstalledVersions)
	}

	return c.JSON(200, resp)
}

func (s *SystemControllerHandlers) purgeUninstalledVolumes(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	st := s.Controller.GetStorage()
	if st == nil {
		c.Response().WriteHeader(200)
		return nil
	}

	uninstPrefix := fmt.Sprintf("%s/%s/%s/", UninstalledVolumePrefix, req.Repo, req.Name)
	if err := s.purgeVolumePrefix(st, uninstPrefix); err != nil {
		return err
	}

	uninstParent := fmt.Sprintf("%s/%s/%s", UninstalledVolumePrefix, req.Repo, req.Name)
	if err := st.RemoveFilesystem(uninstParent); err != nil {
		slog.Debug(fmt.Sprintf("purge uninstalled parent %s: %v", uninstParent, err))
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) disablePackage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageToggleRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	if err := inst.SetDisabled(req.Repo, req.Name, true); err != nil {
		return err
	}

	// Find the installed version(s) and stop the service.
	if sd := s.Controller.GetSystemdManager(); sd != nil {
		installed, _ := inst.ListInstalled()
		for _, pkg := range installed {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			if pi.Repo == req.Repo && pi.Name == req.Name {
				unitName := systemd.UnitName(req.Repo, req.Name, pi.Version)
				if err := sd.SetStatus(c.Request().Context(), unitName, systemd.Stop); err != nil {
					return err
				}
			}
		}
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) enablePackage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageToggleRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	if err := inst.SetDisabled(req.Repo, req.Name, false); err != nil {
		return err
	}

	// Find the installed version(s) and start the service.
	if sd := s.Controller.GetSystemdManager(); sd != nil {
		installed, _ := inst.ListInstalled()
		for _, pkg := range installed {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			if pi.Repo == req.Repo && pi.Name == req.Name {
				unitName := systemd.UnitName(req.Repo, req.Name, pi.Version)
				if err := sd.SetStatus(c.Request().Context(), unitName, systemd.Start); err != nil {
					return err
				}
			}
		}
	}

	c.Response().WriteHeader(200)
	return nil
}

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

func (s *SystemControllerHandlers) getLastResponses(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	resp, err := inst.LoadLastResponses(req.Repo, req.Name)
	if err != nil {
		return c.JSON(200, packages.Responses{})
	}

	return c.JSON(200, resp)
}

func (s *SystemControllerHandlers) clearLastResponses(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	if err := inst.ClearLastResponses(req.Repo, req.Name); err != nil {
		return err
	}

	return c.JSON(200, struct{}{})
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

	ctx := packages.CompileContext{
		ExternalHost: s.Controller.GetExternalIP(),
		InternalHost: s.Controller.GetInternalIP(),
		PackageDNS:   req.Name + "." + req.Repo + "." + s.getDNSTLDValue(),
	}
	notes, err := ip.CompileNotesWithContext(responses, ctx)
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
		installed, err := inst.ListInstalled()
		if err != nil {
			return err
		}
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
		installed, err := inst.ListInstalled()
		if err != nil {
			return err
		}
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

package systemcontroller

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

	"gitea.com/town-os/town-os/src/i18n"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

type RebuildGitRequest struct {
	Repo    string `json:"repo"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (s *SystemControllerHandlers) rebuildGit(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := RebuildGitRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	locale := s.getLocale()
	if req.Repo == "" || req.Name == "" || req.Version == "" {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgRebuildFieldsRequired))
	}

	unlock := s.lockPackage(req.Repo, req.Name)
	defer unlock()

	inst := s.Controller.GetInstaller()
	rr := s.Controller.GetRepositoryRoot()
	if rr == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, i18n.T(locale, i18n.MsgRebuildRepoNotConfigured))
	}

	ip, err := rr.LoadPackage(req.Repo, req.Name, req.Version)
	if err != nil {
		return fmt.Errorf("load package %s/%s@%s: %w", req.Repo, req.Name, req.Version, err)
	}

	responses, err := inst.GetResponses(req.Repo, req.Name, req.Version)
	if err != nil {
		return fmt.Errorf("get responses: %w", err)
	}

	tld := s.getDNSTLDValue()
	compiled, err := ip.CompileWithContext(responses, packages.CompileContext{
		ExternalHost: s.Controller.GetExternalIP(),
		InternalHost: s.Controller.GetInternalIP(),
		PackageDNS:   req.Name + "." + req.Repo + "." + tld,
	})
	if err != nil {
		return fmt.Errorf("compile package: %w", err)
	}

	g := s.Controller.GetGitClient()
	if g == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, i18n.T(locale, i18n.MsgRebuildGitNotConfigured))
	}

	ctx := c.Request().Context()
	basePath := s.Controller.GetBtrfsBasePath()

	for volName, vol := range compiled.Volumes {
		if vol.Git == "" {
			continue
		}

		dest := filepath.Join(basePath, packageVolumePath(req.Repo, req.Name, req.Version, volName))
		if err := g.Pull(ctx, dest); err != nil {
			return fmt.Errorf("pull git volume %s: %w", volName, err)
		}
	}

	// Restart the dependent service.
	sd := s.Controller.GetSystemdManager()
	if sd != nil {
		unitNames, err := sd.ListPackageUnitFiles(ctx, req.Repo, req.Name, req.Version)
		if err != nil {
			slog.Debug(fmt.Sprintf("list unit files for %s/%s@%s: %v", req.Repo, req.Name, req.Version, err))
		}
		for _, name := range unitNames {
			if err := sd.SetStatus(ctx, name, systemd.Restart); err != nil {
				return fmt.Errorf("restart service %s: %w", name, err)
			}
		}
	}

	return c.JSON(http.StatusOK, struct{}{})
}

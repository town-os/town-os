package systemcontroller

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"gitea.com/town-os/town-os/src/i18n"
	"gitea.com/town-os/town-os/src/packages"
	"github.com/labstack/echo/v5"
)

func (s *SystemControllerHandlers) packageManifest(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageIdentityRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	locale := s.getLocale()
	if req.Repo == "" || req.Name == "" || req.Version == "" {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgManifestFieldsRequired))
	}

	rr := s.Controller.GetRepositoryRoot()
	if rr == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, i18n.T(locale, i18n.MsgInstallNoRepoRoot))
	}

	fn, err := packages.SafePath(rr.BaseDir, req.Repo, packages.PackagesDir, req.Name, req.Version+".yaml")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	data, err := os.ReadFile(fn) //nolint:gosec // G304 -- fn from SafePath
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return echo.NewHTTPError(http.StatusNotFound, i18n.T(locale, i18n.MsgManifestNotFound, req.Repo, req.Name, req.Version))
		}
		return err
	}

	c.Response().Header().Set("Content-Type", "text/x-yaml; charset=utf-8")
	_, err = c.Response().Write(data)
	return err
}

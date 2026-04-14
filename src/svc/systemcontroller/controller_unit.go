package systemcontroller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/i18n"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

type UnitListEntry struct {
	systemd.UnitStatus

	// PackageIdentifier is the raw flat form "<repo>/<effectiveName>@<version>".
	// For dependency packages the effective name contains "--dep--", e.g.
	// "core/gitea--dep--postgres@1.0". This is the form callers feed back
	// to other APIs (`/packages/installed/info`, `/packages/uninstall`, …)
	// that expect the effective name, so it stays stable across the
	// pretty-name refactor.
	PackageIdentifier string `json:"package_identifier,omitempty"`
	// DisplayIdentifier is the user-facing pretty form
	// "<repo>/<PrettyName(effectiveName)>@<version>". For standalone
	// packages DisplayIdentifier == PackageIdentifier. For deps the
	// "--dep--" separators are replaced with "/" so the identifier
	// reflects the nested on-disk structure, e.g.
	// "core/gitea/postgres@1.0".
	DisplayIdentifier  string `json:"display_identifier,omitempty"`
	PackageDescription string `json:"package_description,omitempty"`
	// IsDependency is true for unit entries whose package was installed
	// as a dependency of another package. Clients use this instead of
	// string-matching "--dep--" to decide whether to roll a unit up
	// under its parent in dashboard-style views.
	IsDependency bool `json:"is_dependency,omitempty"`
	NCFailed     bool `json:"nc_failed,omitempty"`
	NCActive     bool `json:"nc_active,omitempty"`
}

type SetStatusRequest struct {
	Name   string               `json:"name"`
	Action systemd.StatusAction `json:"action"`
}

// --- Systemd handlers ---

func (s *SystemControllerHandlers) listUnits(c *echo.Context) error {
	units, err := s.Controller.GetSystemdManager().ListUnits(c.Request().Context())
	if err != nil {
		return err
	}

	// Build lookup maps: filter main service units and index NC units.
	filtered := make([]systemd.UnitStatus, 0, len(units))
	ncUnitMap := map[string]systemd.UnitStatus{}
	for _, u := range units {
		if systemd.IsPackageServiceUnit(u.Name) {
			filtered = append(filtered, u)
		}
		if strings.HasSuffix(u.Name, "-network.service") && strings.HasPrefix(u.Name, systemd.PackageUnitPrefix) {
			ncUnitMap[u.Name] = u
		}
	}

	// Build unit name → package identity/description map. Each installed
	// entry produces both the raw flat identifier (used for API
	// round-tripping) and the pretty display identifier (used for UI
	// rendering) so downstream consumers don't have to duplicate the
	// translation logic.
	identityMap := map[string]string{}
	displayMap := map[string]string{}
	isDepMap := map[string]bool{}
	descriptionMap := map[string]string{}
	if inst := s.Controller.GetInstaller(); inst != nil {
		installed, listErr := inst.ListInstalled()
		if listErr == nil {
			rr := s.Controller.GetRepositoryRoot()
			for _, pkg := range installed {
				pi, parseErr := packages.ParsePackageIdentity(pkg)
				if parseErr != nil {
					continue
				}
				unitName := systemd.UnitName(pi.Repo, pi.Name, pi.Version)
				identityMap[unitName] = fmt.Sprintf("%s/%s@%s", pi.Repo, pi.Name, pi.Version)
				displayMap[unitName] = fmt.Sprintf("%s/%s@%s", pi.Repo, packages.PrettyName(pi.Name), pi.Version)
				isDepMap[unitName] = packages.IsDependency(pi.Name)

				// Load only the specific package YAML instead of all packages.
				if rr != nil {
					ip, loadErr := rr.LoadPackage(pi.Repo, pi.Name, pi.Version)
					if loadErr == nil {
						descriptionMap[unitName] = ip.Description
					}
				}
			}
		}
	}

	// Enrich with package identity, description, and NC failure status.
	// Skip units that have no matching installed package identity.
	entries := make([]UnitListEntry, 0, len(filtered))
	for _, u := range filtered {
		pkgID, ok := identityMap[u.Name]
		if !ok {
			continue
		}

		entry := UnitListEntry{
			UnitStatus:         u,
			PackageIdentifier:  pkgID,
			DisplayIdentifier:  displayMap[u.Name],
			IsDependency:       isDepMap[u.Name],
			PackageDescription: descriptionMap[u.Name],
		}

		// Check the corresponding network controller unit status.
		ncName := strings.TrimSuffix(u.Name, ".service") + "-network.service"
		if ncUnit, ok := ncUnitMap[ncName]; ok {
			if ncUnit.ActiveState == "failed" {
				entry.NCFailed = true
				if entry.ActiveState != "failed" {
					entry.ActiveState = "failed"
				}
			}
			if ncUnit.ActiveState == "active" {
				entry.NCActive = true
			}
		}

		entries = append(entries, entry)
	}

	p := readListParams(c)
	entries = filterSearch(entries, p.Search)
	sortSlice(entries, p.SortBy, p.SortOrder)

	return c.JSON(200, paginate(entries, p.Limit, p.Offset))
}

func (s *SystemControllerHandlers) setUnitStatus(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := SetStatusRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	locale := s.getLocale()
	if req.Action == systemd.Enable || req.Action == systemd.Disable {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgUnitEnableDisableNotAllowed))
	}

	if req.Action == systemd.Stop && req.Name == systemd.SystemControllerUnitName {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgUnitCannotStopController))
	}

	if err := s.Controller.GetSystemdManager().SetStatus(c.Request().Context(), req.Name, req.Action); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) logReplay(c *echo.Context) error {
	unit := c.QueryParam("unit")

	ch, err := s.Controller.GetSystemdManager().LogReplay(c.Request().Context(), unit)
	if err != nil {
		return err
	}

	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().WriteHeader(200)

	flusher, ok := c.Response().(http.Flusher)
	ctx := c.Request().Context()
	heartbeat := time.NewTicker(time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case entry, open := <-ch:
			if !open {
				return nil
			}
			if _, err := fmt.Fprint(c.Response(), "data: "); err != nil {
				return err
			}
			if err := json.NewEncoder(c.Response()).Encode(entry); err != nil {
				return err
			}
			if _, err := fmt.Fprint(c.Response(), "\n"); err != nil {
				return err
			}
			if ok {
				flusher.Flush()
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(c.Response(), ":\n"); err != nil {
				return err
			}
			if ok {
				flusher.Flush()
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *SystemControllerHandlers) logTail(c *echo.Context) error {
	unit := c.QueryParam("unit")

	locale := s.getLocale()
	lines := 100
	if v := c.QueryParam("lines"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s: %w", i18n.T(locale, i18n.MsgUnitInvalidLines), err)
		}
		lines = n
	}

	params := systemd.LogTailParams{
		Unit:         unit,
		Lines:        lines,
		BeforeCursor: c.QueryParam("before"),
		AfterCursor:  c.QueryParam("after"),
		Grep:         c.QueryParam("grep"),
	}

	if v := c.QueryParam("since"); v != "" {
		sinceUnix, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("%s: %w", i18n.T(locale, i18n.MsgUnitInvalidSince), err)
		}
		params.Since = time.Unix(sinceUnix, 0)
	}

	if v := c.QueryParam("until"); v != "" {
		untilUnix, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("%s: %w", i18n.T(locale, i18n.MsgUnitInvalidUntil), err)
		}
		params.Until = time.Unix(untilUnix, 0)
	}

	if v := c.QueryParam("priority"); v != "" {
		pri, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s: %w", i18n.T(locale, i18n.MsgUnitInvalidPriority), err)
		}
		params.Priority = pri
	}

	result, err := s.Controller.GetSystemdManager().LogTail(c.Request().Context(), params)
	if err != nil {
		return err
	}

	return c.JSON(200, result)
}

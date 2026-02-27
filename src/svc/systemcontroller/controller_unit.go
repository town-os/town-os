package systemcontroller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

type UnitListEntry struct {
	systemd.UnitStatus

	PackageIdentifier  string `json:"package_identifier,omitempty"`
	PackageDescription string `json:"package_description,omitempty"`
	NCFailed           bool   `json:"nc_failed,omitempty"`
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

	// Build unit name → package identity/description map.
	identityMap := map[string]string{}
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
			PackageDescription: descriptionMap[u.Name],
		}

		// Check if the corresponding network controller unit has failed.
		ncName := strings.TrimSuffix(u.Name, ".service") + "-network.service"
		if ncUnit, ok := ncUnitMap[ncName]; ok && ncUnit.ActiveState == "failed" {
			entry.NCFailed = true
			if entry.ActiveState != "failed" {
				entry.ActiveState = "failed"
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

	if req.Action == systemd.Enable || req.Action == systemd.Disable {
		return echo.NewHTTPError(http.StatusBadRequest, "enable/disable not allowed")
	}

	if req.Action == systemd.Stop && req.Name == "town-os-systemcontroller.service" {
		return echo.NewHTTPError(http.StatusBadRequest, "cannot stop systemcontroller")
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

	lines := 100
	if v := c.QueryParam("lines"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid lines parameter: %w", err)
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
			return fmt.Errorf("invalid since parameter: %w", err)
		}
		params.Since = time.Unix(sinceUnix, 0)
	}

	if v := c.QueryParam("until"); v != "" {
		untilUnix, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid until parameter: %w", err)
		}
		params.Until = time.Unix(untilUnix, 0)
	}

	if v := c.QueryParam("priority"); v != "" {
		pri, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid priority parameter: %w", err)
		}
		params.Priority = pri
	}

	result, err := s.Controller.GetSystemdManager().LogTail(c.Request().Context(), params)
	if err != nil {
		return err
	}

	return c.JSON(200, result)
}

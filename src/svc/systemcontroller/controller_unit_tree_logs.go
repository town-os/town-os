// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
)

// treeLogReplay streams the historical journal for every systemd unit in a
// package's dependency tree as a single SSE stream. The request identifies
// the root package by (repo, name, version); the handler walks the
// persisted dependency records, builds a unit list, and passes it to the
// underlying journal reader — which OR-combines the unit matches so
// entries from the parent and each dependency arrive interleaved in
// chronological order.
//
// Empty trees (an unknown root with no install record) still receive an
// open SSE stream with no entries rather than a 404, so the UI can keep
// its generic journal viewer for both single-unit and tree flows without
// special-casing missing packages.
func (s *SystemControllerHandlers) treeLogReplay(c *echo.Context) error {
	repo := c.QueryParam("repo")
	name := c.QueryParam("name")
	version := c.QueryParam("version")
	if repo == "" || name == "" || version == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "repo, name, and version are required")
	}

	units, err := s.collectTreeUnits(repo, name, version)
	if err != nil {
		return err
	}

	ch, err := s.Controller.GetSystemdManager().LogReplay(c.Request().Context(), units...)
	if err != nil {
		return err
	}

	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().WriteHeader(http.StatusOK)

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

// treeLogTail returns a page of journal entries covering every systemd
// unit in the package's dependency tree. Filters (lines, grep, since,
// until, priority) and cursors are identical to the single-unit
// /systemd/logs/tail endpoint; the only difference is the unit set is
// resolved from (repo, name, version) instead of an explicit name.
//
// Each cursor returned here is a regular sdjournal cursor, so the client
// can page backwards through a tree's combined history exactly the way it
// pages through a single-unit history.
func (s *SystemControllerHandlers) treeLogTail(c *echo.Context) error {
	repo := c.QueryParam("repo")
	name := c.QueryParam("name")
	version := c.QueryParam("version")
	if repo == "" || name == "" || version == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "repo, name, and version are required")
	}

	params, err := s.parseLogTailQuery(c)
	if err != nil {
		return err
	}

	units, err := s.collectTreeUnits(repo, name, version)
	if err != nil {
		return err
	}
	params.Units = units

	result, err := s.Controller.GetSystemdManager().LogTail(c.Request().Context(), params)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, result)
}

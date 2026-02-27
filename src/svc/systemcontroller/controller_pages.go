package systemcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"
)

// Request/response types for Pages API.

type CreatePageRequest struct {
	Name    string `json:"name"`
	RepoURL string `json:"repo_url"`
	Branch  string `json:"branch,omitempty"`
	Domain  string `json:"domain,omitempty"`
}

type UpdatePageRequest struct {
	Name    string `json:"name"`
	RepoURL string `json:"repo_url,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Domain  string `json:"domain,omitempty"`
}

type RemovePageRequest struct {
	Name string `json:"name"`
}

type RebuildPageRequest struct {
	Name string `json:"name"`
}

func (s *SystemControllerHandlers) createPage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := CreatePageRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	if req.Name == "" || req.RepoURL == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name and repo_url are required")
	}

	branch := req.Branch
	if branch == "" {
		branch = "main"
	}
	domain := req.Domain
	if domain == "" {
		domain = req.Name
	}

	page := Page{
		Name:    req.Name,
		RepoURL: req.RepoURL,
		Branch:  branch,
		Domain:  domain,
		Status:  "pending",
	}

	ps := s.Controller.GetPagesStore()
	if err := ps.Create(page); err != nil {
		return err
	}

	created, err := ps.Get(req.Name)
	if err != nil {
		return err
	}

	// Clone asynchronously.
	g := s.Controller.GetGitClient()
	if g != nil {
		go func() {
			basePath := s.Controller.GetBtrfsBasePath()
			dest := basePath + "/pages"

			if err := g.Clone(context.Background(), dest, req.RepoURL, req.Name); err != nil {
				slog.Error(fmt.Sprintf("pages clone %s: %v", req.Name, err))
				_ = ps.Update(req.Name, map[string]string{"status": "error"})
				return
			}
			_ = ps.Update(req.Name, map[string]string{"status": "active"})
		}()
	}

	return c.JSON(http.StatusOK, created)
}

func (s *SystemControllerHandlers) updatePage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := UpdatePageRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}

	updates := map[string]string{}
	if req.RepoURL != "" {
		updates["repo_url"] = req.RepoURL
	}
	if req.Branch != "" {
		updates["branch"] = req.Branch
	}
	if req.Domain != "" {
		updates["domain"] = req.Domain
	}

	ps := s.Controller.GetPagesStore()
	if err := ps.Update(req.Name, updates); err != nil {
		return err
	}

	page, err := ps.Get(req.Name)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, page)
}

func (s *SystemControllerHandlers) removePage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := RemovePageRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}

	ps := s.Controller.GetPagesStore()
	if err := ps.Remove(req.Name); err != nil {
		return err
	}

	// Remove the cloned repository directory.
	basePath := s.Controller.GetBtrfsBasePath()
	dir := filepath.Join(basePath, "pages", req.Name)
	if err := os.RemoveAll(dir); err != nil {
		slog.Debug(fmt.Sprintf("remove page directory %s: %v", dir, err))
	}

	return c.JSON(http.StatusOK, struct{}{})
}

func (s *SystemControllerHandlers) rebuildPage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := RebuildPageRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}

	ps := s.Controller.GetPagesStore()
	page, err := ps.Get(req.Name)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "page not found")
	}

	g := s.Controller.GetGitClient()
	if g == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "git client not configured")
	}

	basePath := s.Controller.GetBtrfsBasePath()
	dest := filepath.Join(basePath, "pages", page.Name)

	// If the .git directory is missing, perform a fresh clone instead of pulling.
	gitDir := filepath.Join(dest, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		parentDir := filepath.Join(basePath, "pages")
		if err := g.Clone(c.Request().Context(), parentDir, page.RepoURL, page.Name); err != nil {
			_ = ps.Update(page.Name, map[string]string{"status": "error"})
			return fmt.Errorf("rebuild page (clone) %s: %w", page.Name, err)
		}
	} else if err := g.Pull(c.Request().Context(), dest); err != nil {
		_ = ps.Update(page.Name, map[string]string{"status": "error"})
		return fmt.Errorf("rebuild page %s: %w", page.Name, err)
	}

	_ = ps.Update(page.Name, map[string]string{
		"status":     "active",
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	})

	return c.JSON(http.StatusOK, struct{}{})
}

func (s *SystemControllerHandlers) listPages(c *echo.Context) error {
	ps := s.Controller.GetPagesStore()
	if ps == nil {
		return c.JSON(http.StatusOK, PagesPage{})
	}

	opts := PagesListOptions{
		SortBy:    c.QueryParam("sort_by"),
		SortOrder: c.QueryParam("sort_order"),
		Search:    c.QueryParam("search"),
	}

	if v := c.QueryParam("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			opts.Limit = n
		}
	}
	if v := c.QueryParam("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			opts.Offset = n
		}
	}

	result, err := ps.List(opts)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

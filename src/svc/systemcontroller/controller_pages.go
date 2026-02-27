package systemcontroller

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"gitea.com/town-os/town-os/src/account"
	"github.com/labstack/echo/v5"
)

// --- Pages handlers ---

type CreatePageRequest struct {
	Name    string `json:"name"`
	RepoURL string `json:"repo_url"`
	Branch  string `json:"branch"`
	Domain  string `json:"domain"`
}

type UpdatePageRequest struct {
	Name   string                 `json:"name"`
	Fields account.PageSiteUpdate `json:"fields"`
}

type PageNameRequest struct {
	Name string `json:"name"`
}

func (s *SystemControllerHandlers) createPage(c *echo.Context) error {
	mgr := s.Controller.GetPagesManager()
	if mgr == nil {
		return errors.New("pages not configured")
	}

	de := json.NewDecoder(c.Request().Body)
	req := CreatePageRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	// Default domain to subdomain based on name if not provided.
	if req.Domain == "" {
		req.Domain = req.Name
	}

	page, err := mgr.Create(req.Name, req.RepoURL, req.Branch, req.Domain)
	if err != nil {
		return err
	}

	// Clone the git repository in the background.
	cloner := s.Controller.GetGitCloner()
	pagesDir := s.pagesBaseDir()
	if cloner != nil && pagesDir != "" {
		targetDir := fmt.Sprintf("%s/%s", pagesDir, req.Name)
		cloneErr := cloner.Clone(targetDir, req.RepoURL, req.Branch)

		status := "active"
		if cloneErr != nil {
			slog.Debug(fmt.Sprintf("pages clone %s: %v", req.Name, cloneErr))
			status = "error"
		}

		if _, err := mgr.Update(req.Name, account.PageSiteUpdate{Status: &status}); err != nil {
			slog.Debug(fmt.Sprintf("pages update status %s: %v", req.Name, err))
		}

		// Re-fetch the page to return the updated status.
		page, _ = mgr.Get(req.Name)
	}

	return c.JSON(200, page)
}

func (s *SystemControllerHandlers) updatePage(c *echo.Context) error {
	mgr := s.Controller.GetPagesManager()
	if mgr == nil {
		return errors.New("pages not configured")
	}

	de := json.NewDecoder(c.Request().Body)
	req := UpdatePageRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	page, err := mgr.Update(req.Name, req.Fields)
	if err != nil {
		return err
	}

	return c.JSON(200, page)
}

func (s *SystemControllerHandlers) removePage(c *echo.Context) error {
	mgr := s.Controller.GetPagesManager()
	if mgr == nil {
		return errors.New("pages not configured")
	}

	de := json.NewDecoder(c.Request().Body)
	req := PageNameRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	if err := mgr.Remove(req.Name); err != nil {
		return err
	}

	// Remove the cloned directory.
	pagesDir := s.pagesBaseDir()
	if pagesDir != "" {
		targetDir := fmt.Sprintf("%s/%s", pagesDir, req.Name)
		if err := os.RemoveAll(targetDir); err != nil {
			slog.Debug(fmt.Sprintf("pages remove dir %s: %v", targetDir, err))
		}
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listPages(c *echo.Context) error {
	mgr := s.Controller.GetPagesManager()
	if mgr == nil {
		return errors.New("pages not configured")
	}

	pages, err := mgr.List()
	if err != nil {
		return err
	}

	p := readListParams(c)
	pages = filterSearch(pages, p.Search)
	sortSlice(pages, p.SortBy, p.SortOrder)

	return c.JSON(200, paginate(pages, p.Limit, p.Offset))
}

func (s *SystemControllerHandlers) rebuildPage(c *echo.Context) error {
	mgr := s.Controller.GetPagesManager()
	if mgr == nil {
		return errors.New("pages not configured")
	}

	de := json.NewDecoder(c.Request().Body)
	req := PageNameRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	page, err := mgr.Get(req.Name)
	if err != nil {
		return err
	}

	cloner := s.Controller.GetGitCloner()
	pagesDir := s.pagesBaseDir()
	if cloner == nil || pagesDir == "" {
		return errors.New("git cloner or pages directory not configured")
	}

	targetDir := fmt.Sprintf("%s/%s", pagesDir, page.Name)

	// Check if the directory exists and has a .git directory.
	gitDir := targetDir + "/.git"
	if _, err := os.Stat(gitDir); err != nil {
		// Not cloned yet; do a fresh clone.
		if err := cloner.Clone(targetDir, page.RepoURL, page.Branch); err != nil {
			status := "error"
			if _, uerr := mgr.Update(page.Name, account.PageSiteUpdate{Status: &status}); uerr != nil {
				slog.Debug(fmt.Sprintf("pages update status %s: %v", page.Name, uerr))
			}
			return fmt.Errorf("pages clone %s: %w", page.Name, err)
		}
	} else {
		if err := cloner.Update(targetDir, page.Branch); err != nil {
			status := "error"
			if _, uerr := mgr.Update(page.Name, account.PageSiteUpdate{Status: &status}); uerr != nil {
				slog.Debug(fmt.Sprintf("pages update status %s: %v", page.Name, uerr))
			}
			return fmt.Errorf("pages update %s: %w", page.Name, err)
		}
	}

	status := "active"
	updated, err := mgr.Update(page.Name, account.PageSiteUpdate{Status: &status})
	if err != nil {
		return err
	}

	return c.JSON(200, updated)
}

// pagesBaseDir returns the base directory for page site clones.
// It is stored alongside the btrfs base path under a "pages" subdirectory.
func (s *SystemControllerHandlers) pagesBaseDir() string {
	base := s.Controller.GetBtrfsBasePath()
	if base == "" {
		return ""
	}
	return base + "/pages"
}

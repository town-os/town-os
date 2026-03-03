package systemcontroller

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/storage"
	"github.com/labstack/echo/v5"
)

// --- Pages handlers ---

type CreatePageRequest struct {
	Name           string `json:"name"`
	RepoURL        string `json:"repo_url"`
	Branch         string `json:"branch"`
	Domain         string `json:"domain"`
	SourceType     string `json:"source_type"`
	Image          string `json:"image"`
	ImageDirectory string `json:"image_directory"`
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

	// Default source type to archive.
	if req.SourceType == "" {
		req.SourceType = account.PageSourceArchive
	}

	page, err := mgr.Create(req.Name, req.RepoURL, req.Branch, req.Domain, req.SourceType, req.Image, req.ImageDirectory)
	if err != nil {
		return err
	}

	// Create the btrfs subvolume for this page's content.
	st := s.Controller.GetStorage()
	if st != nil {
		fsName := PagesVolumePrefix + "/" + req.Name
		if err := st.CreateFilesystem(storage.Filesystem{Name: fsName}); err != nil {
			// Rollback: remove the page from the DB.
			if rerr := mgr.Remove(req.Name); rerr != nil {
				slog.Debug(fmt.Sprintf("pages rollback remove %s: %v", req.Name, rerr))
			}
			return fmt.Errorf("create pages subvolume: %w", err)
		}
	}

	btrfsBase := s.Controller.GetBtrfsBasePath()
	if btrfsBase != "" {
		if err := EnsurePageSymlink(btrfsBase, req.Name); err != nil {
			slog.Debug(fmt.Sprintf("pages symlink %s: %v", req.Name, err))
		}
	}

	subvolPath := s.pagesSubvolumePath(req.Name)

	switch req.SourceType {
	case account.PageSourceGit:
		// Clone the git repository asynchronously in the background.
		gitClient := s.Controller.GetGitClient()
		pagesDir := filepath.Join(btrfsBase, PagesVolumePrefix)
		if gitClient != nil && btrfsBase != "" {
			go func() {
				cloneErr := gitClient.Clone(context.Background(), pagesDir, req.RepoURL, req.Name)

				status := "active"
				if cloneErr != nil {
					slog.Debug(fmt.Sprintf("pages clone %s: %v", req.Name, cloneErr))
					status = "error"
				}

				if _, err := mgr.Update(req.Name, account.PageSiteUpdate{Status: &status}); err != nil {
					slog.Debug(fmt.Sprintf("pages update status %s: %v", req.Name, err))
				}
			}()
		}
	case account.PageSourceContainerImage:
		// Extract from container image asynchronously.
		if subvolPath != "" {
			go func() {
				extractErr := reconcileExtractFromImage(context.Background(), req.Image, req.ImageDirectory, subvolPath)

				status := "active"
				if extractErr != nil {
					slog.Debug(fmt.Sprintf("pages extract image %s: %v", req.Name, extractErr))
					status = "error"
				}

				if _, err := mgr.Update(req.Name, account.PageSiteUpdate{Status: &status}); err != nil {
					slog.Debug(fmt.Sprintf("pages update status %s: %v", req.Name, err))
				}
			}()
		}
	case account.PageSourceArchive:
		// Archive pages remain pending until the user uploads an archive
		// via POST /pages/upload.
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

	btrfsBase := s.Controller.GetBtrfsBasePath()
	if btrfsBase != "" {
		if err := RemovePageSymlink(btrfsBase, req.Name); err != nil {
			slog.Debug(fmt.Sprintf("pages remove symlink %s: %v", req.Name, err))
		}
	}

	st := s.Controller.GetStorage()
	if st != nil {
		fsName := PagesVolumePrefix + "/" + req.Name
		if err := st.RemoveFilesystem(fsName); err != nil {
			slog.Debug(fmt.Sprintf("pages remove subvolume %s: %v", req.Name, err))
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

	btrfsBase := s.Controller.GetBtrfsBasePath()
	if btrfsBase == "" {
		return errors.New("pages directory not configured")
	}

	targetDir := s.pagesSubvolumePath(page.Name)
	pagesDir := filepath.Join(btrfsBase, PagesVolumePrefix)

	switch page.SourceType {
	case account.PageSourceGit, "":
		gitClient := s.Controller.GetGitClient()
		if gitClient == nil {
			return errors.New("git client not configured")
		}

		// Check if the directory exists and has a .git directory.
		gitDir := targetDir + "/.git"
		if _, err := os.Stat(gitDir); err != nil {
			// Not cloned yet; do a fresh clone.
			if err := gitClient.Clone(c.Request().Context(), pagesDir, page.RepoURL, page.Name); err != nil {
				status := "error"
				if _, uerr := mgr.Update(page.Name, account.PageSiteUpdate{Status: &status}); uerr != nil {
					slog.Debug(fmt.Sprintf("pages update status %s: %v", page.Name, uerr))
				}
				return fmt.Errorf("pages clone %s: %w", page.Name, err)
			}
		} else {
			if err := gitClient.Pull(c.Request().Context(), targetDir); err != nil {
				status := "error"
				if _, uerr := mgr.Update(page.Name, account.PageSiteUpdate{Status: &status}); uerr != nil {
					slog.Debug(fmt.Sprintf("pages update status %s: %v", page.Name, uerr))
				}
				return fmt.Errorf("pages pull %s: %w", page.Name, err)
			}
		}

	case account.PageSourceContainerImage:
		if err := reconcileExtractFromImage(c.Request().Context(), page.Image, page.ImageDirectory, targetDir); err != nil {
			status := "error"
			if _, uerr := mgr.Update(page.Name, account.PageSiteUpdate{Status: &status}); uerr != nil {
				slog.Debug(fmt.Sprintf("pages update status %s: %v", page.Name, uerr))
			}
			return fmt.Errorf("pages extract image %s: %w", page.Name, err)
		}

	case account.PageSourceArchive:
		// Archive pages must be rebuilt by uploading a new archive via
		// POST /pages/upload. This endpoint only refreshes the status.
		return echo.NewHTTPError(http.StatusBadRequest, "archive pages must be rebuilt by uploading a new archive via /pages/upload")
	}

	status := "active"
	updated, err := mgr.Update(page.Name, account.PageSiteUpdate{Status: &status})
	if err != nil {
		return err
	}

	return c.JSON(200, updated)
}

// uploadPageArchive handles multipart form upload of an archive file and
// unpacks it into the pages directory for the named page. Only valid for
// pages with source_type "archive".
func (s *SystemControllerHandlers) uploadPageArchive(c *echo.Context) error {
	mgr := s.Controller.GetPagesManager()
	if mgr == nil {
		return errors.New("pages not configured")
	}

	name := c.FormValue("name")
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name field is required")
	}

	page, err := mgr.Get(name)
	if err != nil {
		return err
	}

	if page.SourceType != account.PageSourceArchive {
		return echo.NewHTTPError(http.StatusBadRequest, "upload is only allowed for archive-type pages")
	}

	// Check Content-Length against max size.
	if c.Request().ContentLength > 0 {
		maxSize := s.maxArchiveSize()
		if c.Request().ContentLength > maxSize {
			return echo.NewHTTPError(http.StatusForbidden, ErrArchiveTooLarge.Error())
		}
	}

	file, header, err := c.Request().FormFile("archive")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("archive file required: %v", err))
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Debug(fmt.Sprintf("close upload file: %v", err))
		}
	}()

	targetDir := s.pagesSubvolumePath(name)
	if targetDir == "" {
		return errors.New("pages directory not configured")
	}

	ctx := c.Request().Context()
	br := bufio.NewReader(file)

	// Detect archive format.
	format, _, fmtErr := detectArchiveFormat(br)
	if fmtErr != nil {
		status := "error"
		if _, uerr := mgr.Update(name, account.PageSiteUpdate{Status: &status}); uerr != nil {
			slog.Debug(fmt.Sprintf("pages update status %s: %v", name, uerr))
		}
		if errors.Is(fmtErr, ErrUnsupportedArchive) {
			return echo.NewHTTPError(http.StatusBadRequest, fmtErr.Error())
		}
		return fmtErr
	}

	if _, extErr := archiveFormat(header.Filename); extErr != nil {
		status := "error"
		if _, uerr := mgr.Update(name, account.PageSiteUpdate{Status: &status}); uerr != nil {
			slog.Debug(fmt.Sprintf("pages update status %s: %v", name, uerr))
		}
		return echo.NewHTTPError(http.StatusBadRequest, extErr.Error())
	}

	// Enforce size limit.
	maxSize := s.maxArchiveSize()
	cr := &countingReader{r: io.LimitReader(br, maxSize+1)}

	timeout := s.archiveUnpackTimeout()
	unpackCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	decompCmd := decompressCommand(unpackCtx, format)

	var unpackErr error
	if decompCmd != nil {
		unpackErr = s.unpackWithValidation(unpackCtx, cr, decompCmd, targetDir, maxSize)
	} else {
		unpackErr = s.unpackPlainTar(unpackCtx, cr, targetDir, maxSize)
	}

	if unpackErr != nil {
		status := "error"
		if _, uerr := mgr.Update(name, account.PageSiteUpdate{Status: &status}); uerr != nil {
			slog.Debug(fmt.Sprintf("pages update status %s: %v", name, uerr))
		}
		if errors.Is(unpackErr, ErrArchiveTooLarge) {
			return echo.NewHTTPError(http.StatusForbidden, unpackErr.Error())
		}
		if errors.Is(unpackErr, ErrUnsupportedArchive) || errors.Is(unpackErr, ErrInvalidTar) {
			return echo.NewHTTPError(http.StatusBadRequest, unpackErr.Error())
		}
		return unpackErr
	}

	status := "active"
	updated, err := mgr.Update(name, account.PageSiteUpdate{Status: &status})
	if err != nil {
		return err
	}

	return c.JSON(200, updated)
}

// pagesSubvolumePath returns the filesystem path for a page's btrfs subvolume.
func (s *SystemControllerHandlers) pagesSubvolumePath(name string) string {
	base := s.Controller.GetBtrfsBasePath()
	if base == "" {
		return ""
	}
	return filepath.Join(base, PagesVolumePrefix, name)
}

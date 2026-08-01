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
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/i18n"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

// extractFromContainerImage pulls a container image and copies a directory
// from it into the target subvolume.
func (s *SystemControllerHandlers) extractFromContainerImage(ctx context.Context, image, directory, targetSubvol string) error {
	basePath := s.Controller.GetBtrfsBasePath()
	targetPath, err := safeSubvolumePath(basePath, targetSubvol)
	if err != nil {
		return err
	}

	return reconcileExtractFromImage(ctx, image, directory, targetPath)
}

// uploadArchive handles multipart form upload of an archive file and unpacks
// it into the specified subvolume.
func (s *SystemControllerHandlers) uploadArchive(c *echo.Context) error {
	locale := s.getLocale()
	subvolume := c.FormValue("subvolume")
	if subvolume == "" {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgArchiveSubvolumeRequired))
	}

	if isGfehSubvolume(subvolume) {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgArchiveGfehRefused))
	}

	subpath := c.FormValue("subpath")
	stopService := c.FormValue("stop_service")

	if err := validateServiceName(stopService); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
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
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgArchiveFileRequired, err))
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Debug(fmt.Sprintf("close upload file: %v", err))
		}
	}()

	ctx := c.Request().Context()
	sd := s.Controller.GetSystemdManager()

	// Stop service if requested.
	if stopService != "" && sd != nil {
		if err := sd.SetStatus(ctx, stopService, systemd.Stop); err != nil {
			return fmt.Errorf("stop service %s: %w", stopService, err)
		}
		defer func() {
			if err := sd.SetStatus(ctx, stopService, systemd.Start); err != nil {
				slog.Debug(fmt.Sprintf("restart service %s: %v", stopService, err))
			}
		}()
	}

	subvolume = resolveArchiveSubvolume(subvolume)

	br := bufio.NewReader(file)
	if err := s.streamUnpackToSubvolume(ctx, br, header.Filename, subvolume, subpath); err != nil {
		if errors.Is(err, ErrArchiveTooLarge) {
			return echo.NewHTTPError(http.StatusForbidden, err.Error())
		}
		if errors.Is(err, ErrUnsupportedArchive) || errors.Is(err, ErrInvalidTar) {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return err
	}

	return c.JSON(200, ArchiveUploadResponse{NeedsRestart: true, Message: i18n.T(locale, i18n.MsgArchiveUnpackSuccess)})
}

// downloadArchive creates a compressed tar archive of the specified subvolume
// and streams it directly to the client with no temp files. The compression
// format can be selected via the "format" field (default: tar.gz).
func (s *SystemControllerHandlers) downloadArchive(c *echo.Context) error {
	locale := s.getLocale()
	de := json.NewDecoder(c.Request().Body)
	req := DownloadArchiveRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	if req.Subvolume == "" {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgArchiveSubvolumeRequired))
	}

	if isGfehSubvolume(req.Subvolume) {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgArchiveGfehRefused))
	}

	// Default to tar.gz if not specified.
	format := req.Format
	if format == "" {
		format = "tar.gz"
	}
	if !validDownloadFormat(format) {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgArchiveUnsupportedFormat, format))
	}

	if err := validateServiceName(req.StopService); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if err := validateArchivePaths(req.Paths); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	ctx := c.Request().Context()
	sd := s.Controller.GetSystemdManager()

	// Stop service if requested.
	if req.StopService != "" && sd != nil {
		if err := sd.SetStatus(ctx, req.StopService, systemd.Stop); err != nil {
			return fmt.Errorf("stop service %s: %w", req.StopService, err)
		}
		defer func() {
			if err := sd.SetStatus(ctx, req.StopService, systemd.Start); err != nil {
				slog.Debug(fmt.Sprintf("restart service %s: %v", req.StopService, err))
			}
		}()
	}

	basePath := s.Controller.GetBtrfsBasePath()
	subvolPath, err := safeSubvolumePath(basePath, resolveArchiveSubvolume(req.Subvolume))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Build tar command rooted at the subvolume, archiving specific paths or everything.
	prog := compressProgramArg(format)
	var args []string
	if prog != "" {
		args = []string{"--use-compress-program=" + prog, "-cf", "-", "-C", subvolPath}
	} else {
		args = []string{"-cf", "-", "-C", subvolPath}
	}
	if len(req.Paths) > 0 {
		args = append(args, "--")
		args = append(args, req.Paths...)
	} else {
		args = append(args, ".")
	}

	cmd := exec.CommandContext(ctx, "tar", args...) //nolint:gosec // G204 -- args constructed from validated inputs
	cmd.Stdout = c.Response()

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	c.Response().Header().Set("Content-Type", downloadContentType(format))
	c.Response().Header().Set("Content-Disposition", "attachment; filename="+downloadFilename(req.Filename, format))
	c.Response().WriteHeader(200)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start archive: %w", err)
	}

	stderrOut, err := io.ReadAll(stderrPipe)
	if err != nil {
		slog.Debug("read tar stderr", "error", err)
	}

	if err := cmd.Wait(); err != nil {
		slog.Debug(fmt.Sprintf("archive tar: %v: %s", err, string(stderrOut)))
	}

	return nil
}

// gitCloneIntoPath clones a git repository into targetPath using the given
// client. The clone is run with a 5-minute timeout. This is the single
// git->storage clone primitive shared by page and package seeding during
// install and reconcile, mirroring reconcileExtractFromImage. The client is
// injectable so callers can mock it in tests. A non-empty branch checks out
// that branch (single-branch); an empty branch clones the remote default.
func gitCloneIntoPath(ctx context.Context, client git.Client, gitURL, targetPath, branch string) error {
	cloneCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	parent := filepath.Dir(targetPath)
	name := filepath.Base(targetPath)
	return client.CloneBranch(cloneCtx, parent, gitURL, name, branch)
}

// seedGitIfEmpty clones gitURL into targetPath only when targetPath exists and
// is empty. It is the idempotent seeder shared by package and page
// reconcile/install: safe to call every reconcile (a populated directory is
// skipped, so there is no re-clone and no overwrite) and self-healing (an empty
// directory left behind by a failed earlier clone is retried on the next
// reconcile). A non-empty branch checks out that branch; an empty branch (the
// package-volume case, which has no branch concept) clones the remote default.
// Returns any clone error.
func seedGitIfEmpty(ctx context.Context, client git.Client, gitURL, targetPath, branch string) error {
	entries, err := os.ReadDir(targetPath)
	if os.IsNotExist(err) {
		// Missing directory: nothing to seed.
		return nil
	}
	if err != nil {
		return fmt.Errorf("read seed target %s: %w", targetPath, err)
	}
	if len(entries) > 0 {
		// Already populated: nothing to seed.
		return nil
	}
	return gitCloneIntoPath(ctx, client, gitURL, targetPath, branch)
}

// reconcileExtractFromImage is a standalone function for extracting data from
// a container image into a target path, used during both install and reconcile.
// Every podman invocation inherits CONTAINER_HOST from the systemcontroller
// process environment (set at startup) so pull/create/rm/cp act on the host's
// image store and container state via /run/podman/podman.sock.
func reconcileExtractFromImage(ctx context.Context, image, directory, targetPath string) error {
	// Pull the image.
	pullCmd := exec.CommandContext(ctx, "podman", "pull", image) //nolint:gosec // G204 -- image from validated package config
	if output, err := pullCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("podman pull %s: %w: %s", image, err, string(output))
	}

	// Create a temporary container.
	createCmd := exec.CommandContext(ctx, "podman", "create", image) //nolint:gosec // G204 -- image from validated package config
	output, err := createCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman create %s: %w: %s", image, err, string(output))
	}
	containerID := strings.TrimSpace(string(output))
	defer func() {
		rmCmd := exec.CommandContext(ctx, "podman", "rm", "-f", containerID) //nolint:gosec // G204 -- containerID from podman create output
		if out, err := rmCmd.CombinedOutput(); err != nil {
			slog.Debug(fmt.Sprintf("podman rm %s: %v: %s", containerID, err, string(out)))
		}
	}()

	// Copy from container to target path.
	src := fmt.Sprintf("%s:%s", containerID, directory)
	targetPath = filepath.Clean(targetPath)
	cpCmd := exec.CommandContext(ctx, "podman", "cp", src, targetPath) //nolint:gosec // G204 -- src/targetPath from validated inputs
	if output, err := cpCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("podman cp %s -> %s: %w: %s", src, targetPath, err, string(output))
	}

	return nil
}

package systemcontroller

import (
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
	"strconv"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

const (
	ArchivesSubvolume    = "archives"
	DefaultMaxArchiveSize = 20 * 1024 * 1024  // 20 MB
	DefaultUnpackTimeout  = 120               // seconds
)

var (
	ErrArchiveTooLarge   = errors.New("archive exceeds maximum allowed size")
	ErrUnsupportedArchive = errors.New("unsupported archive format")
	ErrUnpackTimeout     = errors.New("archive unpack timed out")
	ErrPathTraversal     = errors.New("archive contains path traversal")
)

// archiveFormat detects the archive format from the filename extension.
func archiveFormat(filename string) (string, error) {
	lower := strings.ToLower(filename)

	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return "tar.gz", nil
	case strings.HasSuffix(lower, ".tar.bz2"), strings.HasSuffix(lower, ".tbz2"):
		return "tar.bz2", nil
	case strings.HasSuffix(lower, ".tar.xz"), strings.HasSuffix(lower, ".txz"):
		return "tar.xz", nil
	case strings.HasSuffix(lower, ".tar"):
		return "tar", nil
	case strings.HasSuffix(lower, ".zip"):
		return "zip", nil
	case strings.HasSuffix(lower, ".7z"):
		return "7z", nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedArchive, filename)
	}
}

// unpackCommand returns an exec.Cmd that unpacks archivePath into destDir
// using the appropriate tool for the given format.
func unpackCommand(ctx context.Context, archivePath, destDir, format string) *exec.Cmd {
	switch format {
	case "tar.gz":
		return exec.CommandContext(ctx, "tar", "--use-compress-program=pigz", "-xf", archivePath, "-C", destDir)
	case "tar.bz2":
		return exec.CommandContext(ctx, "tar", "--use-compress-program=lbzip2", "-xf", archivePath, "-C", destDir)
	case "tar.xz":
		return exec.CommandContext(ctx, "tar", "--use-compress-program=xz", "-xf", archivePath, "-C", destDir)
	case "tar":
		return exec.CommandContext(ctx, "tar", "-xf", archivePath, "-C", destDir)
	case "zip":
		return exec.CommandContext(ctx, "unzip", "-o", archivePath, "-d", destDir)
	case "7z":
		return exec.CommandContext(ctx, "7z", "x", "-y", fmt.Sprintf("-o%s", destDir), archivePath)
	default:
		return exec.CommandContext(ctx, "false")
	}
}

// validateUnpackedPaths walks the destination directory and ensures all files
// and resolved symlinks remain within the root directory.
func validateUnpackedPaths(destDir string) error {
	root, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}

	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return fmt.Errorf("%w: cannot resolve symlink %s", ErrPathTraversal, path)
			}
			absResolved, err := filepath.Abs(resolved)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(absResolved, root+"/") && absResolved != root {
				return fmt.Errorf("%w: symlink %s escapes root (resolves to %s)", ErrPathTraversal, path, absResolved)
			}
		}

		return nil
	})
}

// maxArchiveSize returns the configured maximum archive size in bytes.
func (s *SystemControllerHandlers) maxArchiveSize() int64 {
	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return DefaultMaxArchiveSize
	}

	val, err := mgr.Get("max_archive_size")
	if err != nil {
		return DefaultMaxArchiveSize
	}

	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return DefaultMaxArchiveSize
	}

	return n
}

// archiveUnpackTimeout returns the configured unpack timeout.
func (s *SystemControllerHandlers) archiveUnpackTimeout() time.Duration {
	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return time.Duration(DefaultUnpackTimeout) * time.Second
	}

	val, err := mgr.Get("archive_unpack_timeout")
	if err != nil {
		return time.Duration(DefaultUnpackTimeout) * time.Second
	}

	n, err := strconv.Atoi(val)
	if err != nil {
		return time.Duration(DefaultUnpackTimeout) * time.Second
	}

	return time.Duration(n) * time.Second
}

// unpackArchiveToSubvolume writes an archive to a staging area and unpacks it
// into the target subvolume. The staging area is cleaned up on completion.
func (s *SystemControllerHandlers) unpackArchiveToSubvolume(ctx context.Context, archiveReader io.Reader, filename, targetSubvol string) error {
	format, err := archiveFormat(filename)
	if err != nil {
		return err
	}

	st := s.Controller.GetStorage()
	if st == nil {
		return fmt.Errorf("storage not available")
	}

	// Create a staging subvolume for the archive file.
	stagingName := fmt.Sprintf("%s/staging-%d", ArchivesSubvolume, time.Now().UnixNano())
	if err := st.CreateFilesystem(storage.Filesystem{Name: stagingName}); err != nil {
		return fmt.Errorf("create staging subvolume: %w", err)
	}
	defer func() {
		if err := st.RemoveFilesystem(stagingName); err != nil {
			slog.Debug(fmt.Sprintf("cleanup staging subvolume %s: %v", stagingName, err))
		}
	}()

	basePath := s.Controller.GetBtrfsBasePath()
	stagingPath := filepath.Join(basePath, stagingName)
	archivePath := filepath.Join(stagingPath, filepath.Base(filename))

	// Write archive to staging, enforcing size limit.
	maxSize := s.maxArchiveSize()
	limited := io.LimitReader(archiveReader, maxSize+1)

	f, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("create archive file: %w", err)
	}

	n, err := io.Copy(f, limited)
	if closeErr := f.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write archive: %w", err)
	}
	if n > maxSize {
		return ErrArchiveTooLarge
	}

	// Unpack with timeout.
	timeout := s.archiveUnpackTimeout()
	unpackCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	targetPath := filepath.Join(basePath, targetSubvol)
	cmd := unpackCommand(unpackCtx, archivePath, targetPath, format)
	output, err := cmd.CombinedOutput()
	if unpackCtx.Err() == context.DeadlineExceeded {
		return ErrUnpackTimeout
	}
	if err != nil {
		return fmt.Errorf("unpack archive: %w: %s", err, string(output))
	}

	// Validate unpacked paths.
	if err := validateUnpackedPaths(targetPath); err != nil {
		return err
	}

	return nil
}

// extractFromContainerImage pulls a container image and copies a directory
// from it into the target subvolume.
func (s *SystemControllerHandlers) extractFromContainerImage(ctx context.Context, image, directory, targetSubvol string) error {
	basePath := s.Controller.GetBtrfsBasePath()
	targetPath := filepath.Join(basePath, targetSubvol)

	return reconcileExtractFromImage(ctx, image, directory, targetPath)
}

// uploadArchive handles multipart form upload of an archive file and unpacks
// it into the specified subvolume.
func (s *SystemControllerHandlers) uploadArchive(c *echo.Context) error {
	subvolume := c.FormValue("subvolume")
	if subvolume == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "subvolume field is required")
	}

	if isReservedFilesystem(subvolume) {
		return storage.ErrReservedFilesystem
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

	ctx := c.Request().Context()
	if err := s.unpackArchiveToSubvolume(ctx, file, header.Filename, subvolume); err != nil {
		if errors.Is(err, ErrArchiveTooLarge) {
			return echo.NewHTTPError(http.StatusForbidden, err.Error())
		}
		if errors.Is(err, ErrUnsupportedArchive) {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return err
	}

	return c.JSON(200, ArchiveUploadResponse{NeedsRestart: true, Message: "archive unpacked successfully"})
}

// downloadArchive creates a 7z archive of the specified subvolumes and streams
// it back to the client.
func (s *SystemControllerHandlers) downloadArchive(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := DownloadArchiveRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	if len(req.Subvolumes) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "at least one subvolume is required")
	}

	for _, sv := range req.Subvolumes {
		if isReservedFilesystem(sv) {
			return storage.ErrReservedFilesystem
		}
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

	st := s.Controller.GetStorage()
	if st == nil {
		return fmt.Errorf("storage not available")
	}

	// Create staging for the archive.
	stagingName := fmt.Sprintf("%s/download-%d", ArchivesSubvolume, time.Now().UnixNano())
	if err := st.CreateFilesystem(storage.Filesystem{Name: stagingName}); err != nil {
		return fmt.Errorf("create staging subvolume: %w", err)
	}
	defer func() {
		if err := st.RemoveFilesystem(stagingName); err != nil {
			slog.Debug(fmt.Sprintf("cleanup download staging %s: %v", stagingName, err))
		}
	}()

	basePath := s.Controller.GetBtrfsBasePath()
	archivePath := filepath.Join(basePath, stagingName, "download.7z")

	// Build 7z command with all subvolume paths.
	args := []string{"a", "-y", archivePath}
	for _, sv := range req.Subvolumes {
		args = append(args, filepath.Join(basePath, sv))
	}

	cmd := exec.CommandContext(ctx, "7z", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("create 7z archive: %w: %s", err, string(output))
	}

	c.Response().Header().Set("Content-Type", "application/x-7z-compressed")
	c.Response().Header().Set("Content-Disposition", "attachment; filename=download.7z")

	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.Debug(fmt.Sprintf("close archive file: %v", err))
		}
	}()

	c.Response().WriteHeader(200)
	if _, err := io.Copy(c.Response(), f); err != nil {
		return fmt.Errorf("stream archive: %w", err)
	}

	return nil
}

// reconcileExtractFromImage is a standalone function for extracting data from
// a container image into a target path, used during both install and reconcile.
func reconcileExtractFromImage(ctx context.Context, image, directory, targetPath string) error {
	// Pull the image.
	pullCmd := exec.CommandContext(ctx, "podman", "pull", image)
	if output, err := pullCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("podman pull %s: %w: %s", image, err, string(output))
	}

	// Create a temporary container.
	createCmd := exec.CommandContext(ctx, "podman", "create", image)
	output, err := createCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman create %s: %w: %s", image, err, string(output))
	}
	containerID := strings.TrimSpace(string(output))
	defer func() {
		rmCmd := exec.CommandContext(ctx, "podman", "rm", "-f", containerID)
		if out, err := rmCmd.CombinedOutput(); err != nil {
			slog.Debug(fmt.Sprintf("podman rm %s: %v: %s", containerID, err, string(out)))
		}
	}()

	// Copy from container to target path.
	src := fmt.Sprintf("%s:%s", containerID, directory)
	cpCmd := exec.CommandContext(ctx, "podman", "cp", src, targetPath)
	if output, err := cpCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("podman cp %s -> %s: %w: %s", src, targetPath, err, string(output))
	}

	return nil
}

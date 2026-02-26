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
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedArchive, filename)
	}
}

// streamUnpackCommand returns an exec.Cmd that reads a tar archive from stdin
// and unpacks it into destDir using the appropriate decompressor for the given format.
func streamUnpackCommand(ctx context.Context, destDir, format string) *exec.Cmd {
	switch format {
	case "tar.gz":
		return exec.CommandContext(ctx, "tar", "--use-compress-program=pigz", "-xf", "-", "-C", destDir)
	case "tar.bz2":
		return exec.CommandContext(ctx, "tar", "--use-compress-program=lbzip2", "-xf", "-", "-C", destDir)
	case "tar.xz":
		return exec.CommandContext(ctx, "tar", "--use-compress-program=xz", "-xf", "-", "-C", destDir)
	case "tar":
		return exec.CommandContext(ctx, "tar", "-xf", "-", "-C", destDir)
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

// countingReader wraps a reader and tracks total bytes read.
type countingReader struct {
	r io.Reader
	n int64
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.n += int64(n)
	return n, err
}

// streamUnpackToSubvolume streams an archive directly from the reader into
// the target subvolume via tar's stdin, with no temp files or staging.
func (s *SystemControllerHandlers) streamUnpackToSubvolume(ctx context.Context, archiveReader io.Reader, filename, targetSubvol string) error {
	format, err := archiveFormat(filename)
	if err != nil {
		return err
	}

	basePath := s.Controller.GetBtrfsBasePath()
	targetPath := filepath.Join(basePath, targetSubvol)

	// Enforce size limit: LimitReader caps at maxSize+1 so we can detect overflow.
	maxSize := s.maxArchiveSize()
	cr := &countingReader{r: io.LimitReader(archiveReader, maxSize+1)}

	// Unpack with timeout.
	timeout := s.archiveUnpackTimeout()
	unpackCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := streamUnpackCommand(unpackCtx, targetPath, format)
	cmd.Stdin = cr

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start unpack: %w", err)
	}

	stderrOut, readErr := io.ReadAll(stderrPipe)

	if err := cmd.Wait(); err != nil {
		if unpackCtx.Err() == context.DeadlineExceeded {
			return ErrUnpackTimeout
		}
		// Check if the archive exceeded the size limit.
		if cr.n > maxSize {
			return ErrArchiveTooLarge
		}
		return fmt.Errorf("unpack archive: %w: %s", err, string(stderrOut))
	}

	if cr.n > maxSize {
		return ErrArchiveTooLarge
	}

	if readErr != nil {
		return fmt.Errorf("read stderr: %w", readErr)
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
	if err := s.streamUnpackToSubvolume(ctx, file, header.Filename, subvolume); err != nil {
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

// downloadArchive creates a tar.gz archive of the specified subvolumes and
// streams it directly to the client with no temp files.
func (s *SystemControllerHandlers) downloadArchive(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := DownloadArchiveRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	if req.Subvolume == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "subvolume is required")
	}

	if isReservedFilesystem(req.Subvolume) {
		return storage.ErrReservedFilesystem
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
	subvolPath := filepath.Join(basePath, req.Subvolume)

	// Build tar command rooted at the subvolume, archiving specific paths or everything.
	args := []string{"--use-compress-program=pigz", "-cf", "-", "-C", subvolPath}
	if len(req.Paths) > 0 {
		args = append(args, req.Paths...)
	} else {
		args = append(args, ".")
	}

	cmd := exec.CommandContext(ctx, "tar", args...)
	cmd.Stdout = c.Response()

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	c.Response().Header().Set("Content-Type", "application/gzip")
	c.Response().Header().Set("Content-Disposition", "attachment; filename=download.tar.gz")
	c.Response().WriteHeader(200)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start archive: %w", err)
	}

	stderrOut, _ := io.ReadAll(stderrPipe)

	if err := cmd.Wait(); err != nil {
		slog.Debug(fmt.Sprintf("archive tar: %v: %s", err, string(stderrOut)))
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

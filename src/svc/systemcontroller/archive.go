package systemcontroller

import (
	"archive/tar"
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
	"strconv"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

const (
	ArchivesSubvolume    = "archives"
	DefaultMaxArchiveSize = 20 * 1024 * 1024  // 20 MB
	DefaultUnpackTimeout  = 120               // seconds

	// magicPeekSize is the number of bytes needed to detect all supported
	// compression formats by their magic bytes.
	magicPeekSize = 6
)

var (
	ErrArchiveTooLarge    = errors.New("archive exceeds maximum allowed size")
	ErrUnsupportedArchive = errors.New("unsupported archive format")
	ErrUnpackTimeout      = errors.New("archive unpack timed out")
	ErrPathTraversal      = errors.New("archive contains path traversal")
	ErrInvalidTar         = errors.New("archive does not contain a valid tar stream")
)

type ArchiveUploadResponse struct {
	NeedsRestart bool   `json:"needs_restart"`
	Message      string `json:"message"`
}

type DownloadArchiveRequest struct {
	Subvolume   string   `json:"subvolume"`
	Paths       []string `json:"paths,omitempty"`
	StopService string   `json:"stop_service,omitempty"`
	Format      string   `json:"format,omitempty"`
}

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

// detectArchiveFormat identifies the compression format of an archive by
// inspecting the magic bytes at the start of the stream. It returns the
// format string and a new reader that replays the peeked bytes followed
// by the rest of the stream.
func detectArchiveFormat(r io.Reader) (string, io.Reader, error) {
	br := bufio.NewReaderSize(r, magicPeekSize)
	header, err := br.Peek(magicPeekSize)
	if err != nil && len(header) < 2 {
		return "", nil, fmt.Errorf("%w: unable to read magic bytes", ErrUnsupportedArchive)
	}

	format := matchMagicBytes(header)
	if format == "" {
		return "", nil, fmt.Errorf("%w: unrecognized magic bytes", ErrUnsupportedArchive)
	}

	return format, br, nil
}

// matchMagicBytes returns the archive format string for the given header
// bytes, or "" if the format is not recognized.
func matchMagicBytes(header []byte) string {
	if len(header) < 2 {
		return ""
	}

	// gzip: 0x1f 0x8b
	if header[0] == 0x1f && header[1] == 0x8b {
		return "tar.gz"
	}

	// bzip2: "BZ" followed by 'h' (0x42 0x5a 0x68)
	if len(header) >= 3 && header[0] == 0x42 && header[1] == 0x5a && header[2] == 0x68 {
		return "tar.bz2"
	}

	// xz: 0xfd "7zXZ" 0x00 (6 bytes: fd 37 7a 58 5a 00)
	if len(header) >= 6 &&
		header[0] == 0xfd && header[1] == 0x37 && header[2] == 0x7a &&
		header[3] == 0x58 && header[4] == 0x5a && header[5] == 0x00 {
		return "tar.xz"
	}

	return ""
}

// decompressCommand returns an exec.Cmd that decompresses stdin to stdout
// for the given format. This is used for tar validation.
func decompressCommand(ctx context.Context, format string) *exec.Cmd {
	switch format {
	case "tar.gz":
		return exec.CommandContext(ctx, "pigz", "-dc")
	case "tar.bz2":
		return exec.CommandContext(ctx, "lbzip2", "-dc")
	case "tar.xz":
		return exec.CommandContext(ctx, "xz", "-dc")
	default:
		return nil
	}
}

// compressProgramArg returns the --use-compress-program value for tar, or
// empty string for plain tar.
func compressProgramArg(format string) string {
	switch format {
	case "tar.gz":
		return "pigz"
	case "tar.bz2":
		return "lbzip2"
	case "tar.xz":
		return "xz"
	default:
		return ""
	}
}

// downloadContentType returns the HTTP Content-Type for a download format.
func downloadContentType(format string) string {
	switch format {
	case "tar.bz2":
		return "application/x-bzip2"
	case "tar.xz":
		return "application/x-xz"
	default:
		return "application/gzip"
	}
}

// downloadFilename returns a default filename for the given download format.
func downloadFilename(format string) string {
	switch format {
	case "tar.bz2":
		return "download.tar.bz2"
	case "tar.xz":
		return "download.tar.xz"
	default:
		return "download.tar.gz"
	}
}

// validDownloadFormat returns true if the given format string is a
// supported download compression format.
func validDownloadFormat(format string) bool {
	switch format {
	case "tar.gz", "tar.bz2", "tar.xz", "":
		return true
	default:
		return false
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

// validateTarStream reads from a decompressed tar stream and validates that
// it contains valid tar headers. It is designed to run in a goroutine,
// consuming data written by a decompressor. If the tar stream is invalid,
// the returned error channel receives ErrInvalidTar and the reader side of
// the pipe is closed to signal the decompressor to stop.
func validateTarStream(ctx context.Context, r io.Reader) <-chan error {
	ch := make(chan error, 1)
	go func() {
		defer close(ch)
		tr := tar.NewReader(r)
		for {
			if ctx.Err() != nil {
				return
			}
			_, err := tr.Next()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				ch <- fmt.Errorf("%w: %w", ErrInvalidTar, err)
				return
			}
			// Discard file bodies so the reader progresses.
			// Size is bounded by the countingReader limit wrapping the original stream.
			if _, err := io.Copy(io.Discard, tr); err != nil { //nolint:gosec // size bounded by countingReader
				ch <- fmt.Errorf("%w: %w", ErrInvalidTar, err)
				return
			}
		}
	}()
	return ch
}

// streamUnpackToSubvolume streams an archive directly from the reader into
// the target subvolume via tar's stdin, with no temp files or staging.
// If subpath is non-empty, the archive is unpacked into that subdirectory
// within the subvolume (created with MkdirAll if it does not exist).
func (s *SystemControllerHandlers) streamUnpackToSubvolume(ctx context.Context, archiveReader *bufio.Reader, filename, targetSubvol, subpath string) error {
	// Detect format from magic bytes first.
	format, magicReader, err := detectArchiveFormat(archiveReader)
	if err != nil {
		return err
	}
	_ = magicReader // archiveReader is already a *bufio.Reader that replays peeked bytes

	// Also validate the extension independently.
	if _, extErr := archiveFormat(filename); extErr != nil {
		return extErr
	}

	basePath := s.Controller.GetBtrfsBasePath()
	targetPath := filepath.Join(basePath, targetSubvol)
	if subpath != "" {
		targetPath = filepath.Join(targetPath, subpath)
		if err := os.MkdirAll(targetPath, 0755); err != nil { //nolint:gosec // admin-only endpoint
			return fmt.Errorf("create subpath directory: %w", err)
		}
	}

	// Enforce size limit: LimitReader caps at maxSize+1 so we can detect overflow.
	maxSize := s.maxArchiveSize()
	cr := &countingReader{r: io.LimitReader(archiveReader, maxSize+1)}

	// Unpack with timeout.
	timeout := s.archiveUnpackTimeout()
	unpackCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Start a decompressor whose stdout we can tee for validation.
	decompCmd := decompressCommand(unpackCtx, format)

	if decompCmd != nil {
		return s.unpackWithValidation(unpackCtx, cr, decompCmd, targetPath, maxSize)
	}

	// Plain tar: no decompressor needed; the stream IS tar, so we validate
	// by teeing the raw reader to archive/tar while unpacking.
	return s.unpackPlainTar(unpackCtx, cr, targetPath, maxSize)
}

// unpackWithValidation decompresses the stream, tees the decompressed output
// to archive/tar for validation, and pipes the other half to tar -xf.
func (s *SystemControllerHandlers) unpackWithValidation(ctx context.Context, cr *countingReader, decompCmd *exec.Cmd, targetPath string, maxSize int64) error {
	decompCmd.Stdin = cr

	decompStdout, err := decompCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("decompress stdout pipe: %w", err)
	}

	decompStderr, err := decompCmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("decompress stderr pipe: %w", err)
	}

	if err := decompCmd.Start(); err != nil {
		return fmt.Errorf("start decompressor: %w", err)
	}

	// Tee the decompressed output: one side validates tar, the other unpacks.
	validPR, validPW := io.Pipe()
	teeReader := io.TeeReader(decompStdout, validPW)

	validCh := validateTarStream(ctx, validPR)

	// Start the unpack command reading from the tee.
	unpackCmd := exec.CommandContext(ctx, "tar", "-xf", "-", "-C", targetPath)
	unpackCmd.Stdin = teeReader

	unpackStderr, err := unpackCmd.StderrPipe()
	if err != nil {
		_ = validPW.Close()
		return fmt.Errorf("unpack stderr pipe: %w", err)
	}

	if err := unpackCmd.Start(); err != nil {
		_ = validPW.Close()
		return fmt.Errorf("start unpack: %w", err)
	}

	// Wait for the unpack to finish (it drives the tee reader).
	unpackStderrOut, _ := io.ReadAll(unpackStderr)
	unpackErr := unpackCmd.Wait()

	// Close the validation pipe writer so the validator sees EOF.
	_ = validPW.Close()

	// Collect validation result.
	var validErr error
	for e := range validCh {
		if e != nil {
			validErr = e
		}
	}

	decompStderrOut, _ := io.ReadAll(decompStderr)
	decompErr := decompCmd.Wait()

	// Check for validation failure first.
	if validErr != nil {
		return validErr
	}

	if ctx.Err() == context.DeadlineExceeded {
		return ErrUnpackTimeout
	}

	if cr.n > maxSize {
		return ErrArchiveTooLarge
	}

	if decompErr != nil {
		return fmt.Errorf("decompressor: %w: %s", decompErr, string(decompStderrOut))
	}

	if unpackErr != nil {
		return fmt.Errorf("unpack archive: %w: %s", unpackErr, string(unpackStderrOut))
	}

	return validateUnpackedPaths(targetPath)
}

// unpackPlainTar handles uncompressed tar archives, teeing the stream for
// validation.
func (s *SystemControllerHandlers) unpackPlainTar(ctx context.Context, cr *countingReader, targetPath string, maxSize int64) error {
	validPR, validPW := io.Pipe()
	teeReader := io.TeeReader(cr, validPW)

	validCh := validateTarStream(ctx, validPR)

	unpackCmd := exec.CommandContext(ctx, "tar", "-xf", "-", "-C", targetPath)
	unpackCmd.Stdin = teeReader

	unpackStderr, err := unpackCmd.StderrPipe()
	if err != nil {
		_ = validPW.Close()
		return fmt.Errorf("unpack stderr pipe: %w", err)
	}

	if err := unpackCmd.Start(); err != nil {
		_ = validPW.Close()
		return fmt.Errorf("start unpack: %w", err)
	}

	unpackStderrOut, _ := io.ReadAll(unpackStderr)
	unpackErr := unpackCmd.Wait()

	_ = validPW.Close()

	var validErr error
	for e := range validCh {
		if e != nil {
			validErr = e
		}
	}

	if validErr != nil {
		return validErr
	}

	if ctx.Err() == context.DeadlineExceeded {
		return ErrUnpackTimeout
	}

	if cr.n > maxSize {
		return ErrArchiveTooLarge
	}

	if unpackErr != nil {
		return fmt.Errorf("unpack archive: %w: %s", unpackErr, string(unpackStderrOut))
	}

	return validateUnpackedPaths(targetPath)
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

	subpath := c.FormValue("subpath")
	stopService := c.FormValue("stop_service")

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

	return c.JSON(200, ArchiveUploadResponse{NeedsRestart: true, Message: "archive unpacked successfully"})
}

// downloadArchive creates a compressed tar archive of the specified subvolume
// and streams it directly to the client with no temp files. The compression
// format can be selected via the "format" field (default: tar.gz).
func (s *SystemControllerHandlers) downloadArchive(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := DownloadArchiveRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	if req.Subvolume == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "subvolume is required")
	}

	// Default to tar.gz if not specified.
	format := req.Format
	if format == "" {
		format = "tar.gz"
	}
	if !validDownloadFormat(format) {
		return echo.NewHTTPError(http.StatusBadRequest, "unsupported download format: "+format)
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
	subvolPath := filepath.Clean(filepath.Join(basePath, req.Subvolume))

	// Build tar command rooted at the subvolume, archiving specific paths or everything.
	prog := compressProgramArg(format)
	var args []string
	if prog != "" {
		args = []string{"--use-compress-program=" + prog, "-cf", "-", "-C", subvolPath}
	} else {
		args = []string{"-cf", "-", "-C", subvolPath}
	}
	if len(req.Paths) > 0 {
		args = append(args, req.Paths...)
	} else {
		args = append(args, ".")
	}

	cmd := exec.CommandContext(ctx, "tar", args...) //nolint:gosec // args constructed from controlled values above
	cmd.Stdout = c.Response()

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	c.Response().Header().Set("Content-Type", downloadContentType(format))
	c.Response().Header().Set("Content-Disposition", "attachment; filename="+downloadFilename(format))
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

// gitCloneIntoPath clones a git repository into targetPath. The clone is run
// with a 5-minute timeout. This is used for git-based volume seed data during
// install and reconcile, mirroring reconcileExtractFromImage.
func gitCloneIntoPath(ctx context.Context, gitURL, targetPath string) error {
	cloneCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	g := &git.GoGitClient{}
	parent := filepath.Dir(targetPath)
	name := filepath.Base(targetPath)
	return g.Clone(cloneCtx, parent, gitURL, name)
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
		rmCmd := exec.CommandContext(ctx, "podman", "rm", "-f", containerID) //nolint:gosec // containerID from podman create output
		if out, err := rmCmd.CombinedOutput(); err != nil {
			slog.Debug(fmt.Sprintf("podman rm %s: %v: %s", containerID, err, string(out)))
		}
	}()

	// Copy from container to target path.
	src := fmt.Sprintf("%s:%s", containerID, directory)
	targetPath = filepath.Clean(targetPath)
	cpCmd := exec.CommandContext(ctx, "podman", "cp", src, targetPath) //nolint:gosec // targetPath cleaned with filepath.Clean above
	if output, err := cpCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("podman cp %s -> %s: %w: %s", src, targetPath, err, string(output))
	}

	return nil
}

package systemcontroller

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	ArchivesSubvolume    = "archives"
	DefaultMaxArchiveSize = 1024 * 1024 * 1024 // 1 GB
	DefaultUnpackTimeout  = 600               // seconds (10 min)

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
	ErrInvalidArchivePath = errors.New("invalid archive path")
	ErrSubvolumeTraversal = errors.New("path escapes subvolume base directory")
	ErrInvalidServiceName = errors.New("invalid systemd service name")
)

// serviceNameRegexp matches valid systemd unit names: alphanumerics, dashes,
// underscores, dots, and @ (for template instances), ending with a unit type
// suffix like .service, .timer, .mount, etc.
var serviceNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9@._-]+\.(service|timer|mount|socket|target|slice|scope)$`)

// validateArchivePaths checks that none of the user-supplied paths contain
// directory traversal sequences or start with a dash (which could be
// interpreted as tar flags).
func validateArchivePaths(paths []string) error {
	for _, p := range paths {
		if strings.HasPrefix(p, "-") {
			return fmt.Errorf("%w: path must not start with dash: %q", ErrInvalidArchivePath, p)
		}
		cleaned := filepath.Clean(p)
		if strings.Contains(cleaned, "..") {
			return fmt.Errorf("%w: path must not contain traversal: %q", ErrInvalidArchivePath, p)
		}
	}
	return nil
}

// safeSubvolumePath joins components onto basePath, cleans the result, and
// verifies that it stays within basePath. Returns ErrSubvolumeTraversal if the
// resolved path escapes the base directory.
func safeSubvolumePath(basePath string, components ...string) (string, error) {
	parts := append([]string{basePath}, components...)
	joined := filepath.Clean(filepath.Join(parts...))
	base := filepath.Clean(basePath)
	if joined != base && !strings.HasPrefix(joined, base+"/") {
		return "", fmt.Errorf("%w: %q is outside %q", ErrSubvolumeTraversal, joined, base)
	}
	return joined, nil
}

// validateServiceName checks that a systemd unit name matches the expected
// format. Empty names are allowed (meaning no service operation).
func validateServiceName(name string) error {
	if name == "" {
		return nil
	}
	if !serviceNameRegexp.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidServiceName, name)
	}
	return nil
}

type ArchiveUploadResponse struct {
	NeedsRestart bool   `json:"needs_restart"`
	Message      string `json:"message"`
}

type DownloadArchiveRequest struct {
	Subvolume   string   `json:"subvolume"`
	Paths       []string `json:"paths,omitempty"`
	StopService string   `json:"stop_service,omitempty"`
	Format      string   `json:"format,omitempty"`
	Filename    string   `json:"filename,omitempty"`
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
		return exec.CommandContext(ctx, "pigz", "-dc") //nolint:gosec // G702 -- constant args
	case "tar.bz2":
		return exec.CommandContext(ctx, "lbzip2", "-dc") //nolint:gosec // G702 -- constant args
	case "tar.xz":
		return exec.CommandContext(ctx, "xz", "-dc") //nolint:gosec // G702 -- constant args
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

// downloadExtension returns the file extension for the given download format.
func downloadExtension(format string) string {
	switch format {
	case "tar.bz2":
		return ".tar.bz2"
	case "tar.xz":
		return ".tar.xz"
	default:
		return ".tar.gz"
	}
}

// sanitizeFilename strips path separators and control characters from a
// user-supplied filename, returning only the base name component. An empty
// result means the input was invalid.
func sanitizeFilename(name string) string {
	// Take only the base name to prevent path traversal.
	name = filepath.Base(name)
	if name == "." || name == ".." || name == "/" {
		return ""
	}

	// Strip any remaining control characters.
	var b strings.Builder
	for _, r := range name {
		if r >= 32 && r != 127 {
			b.WriteRune(r)
		}
	}

	return b.String()
}

// downloadFilename returns the filename for the Content-Disposition header.
// When a custom name is provided it is sanitized and the appropriate archive
// extension is appended. Otherwise a default "download" name is used.
func downloadFilename(customName, format string) string {
	ext := downloadExtension(format)
	if customName == "" {
		return "download" + ext
	}

	// Remove any existing archive extension so it isn't doubled.
	safe := sanitizeFilename(customName)
	if safe == "" {
		return "download" + ext
	}

	for _, suffix := range []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tgz", ".tbz2", ".txz"} {
		if strings.HasSuffix(strings.ToLower(safe), suffix) {
			safe = safe[:len(safe)-len(suffix)]
			break
		}
	}

	if safe == "" {
		return "download" + ext
	}

	return safe + ext
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

	root = filepath.Clean(root)

	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error { //nolint:gosec // root is sanitized via filepath.Abs and Clean above
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

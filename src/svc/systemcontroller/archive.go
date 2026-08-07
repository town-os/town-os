package systemcontroller

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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

// tarExtractFlags are the flags every unpack runs with.
//
// The controller runs as root, and GNU tar extracting as root honours the uid,
// gid and mode recorded in the archive unless told not to. Without these an
// uploaded tarball chooses who owns the files it lands and whether any of them
// is setuid-root — and the target is a btrfs subvolume that package volumes
// bind-mount into package containers, so those bits are read by something other
// than this process.
//
//   - --no-same-owner: extracted files belong to the controller, not to
//     whatever uid the archive names. An archive cannot hand a file to the uid
//     a container happens to run as.
//   - --no-same-permissions: the mode is filtered through the umask, which
//     drops setuid and setgid. Ordinary permission bits (including the
//     executable bit) survive, so this does not break a shipped script.
//
// Both are GNU tar's default for a NON-root extraction; this makes the
// privileged path behave like the unprivileged one, which is the behaviour the
// rest of the archive code was written against.
func tarExtractFlags() []string {
	return []string{"-xf", "-", "--no-same-owner", "--no-same-permissions"}
}

// validateUnpackedPaths walks the destination directory and ensures all files
// and resolved symlinks remain within the root directory.
//
// An escaping symlink is REMOVED as it is found, not merely reported. This runs
// after `tar` has finished — it has to, since the check is about where symlinks
// resolve to once they all exist — so by the time it objects, the link is
// already on disk. Its callers return the error without unwinding the
// extraction, which left the volume carrying exactly the thing this refuses,
// ready for whatever bind-mounts it next. Removing the link and then reporting
// leaves the volume in a state consistent with the rejection.
func validateUnpackedPaths(destDir string) error {
	root, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}

	root = filepath.Clean(root)

	var escapes []string
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error { //nolint:gosec // root is sanitized via filepath.Abs and Clean above
		if err != nil {
			return err
		}

		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}

		reason, escaped := symlinkEscape(root, path)
		if escaped {
			escapes = append(escapes, reason)
			removeEscapingLink(path)
		}
		return nil
	})
	if walkErr != nil {
		return walkErr
	}
	if len(escapes) > 0 {
		return fmt.Errorf("%w: removed %d symlink(s) escaping root: %s",
			ErrPathTraversal, len(escapes), strings.Join(escapes, ", "))
	}
	return nil
}

// symlinkEscape reports whether a symlink leaves root, and describes how.
//
// A link that cannot be resolved counts as escaping: it points outside the tree
// at something absent, which is no safer than pointing outside at something
// present, and the resolution can start succeeding later without the archive
// changing.
func symlinkEscape(root, path string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path + " (unresolvable)", true
	}
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return path + " (unresolvable)", true
	}
	if strings.HasPrefix(absResolved, root+"/") || absResolved == root {
		return "", false
	}
	return path + " -> " + absResolved, true
}

// removeEscapingLink unlinks a rejected symlink, logging rather than failing:
// the caller is already returning an error, and a link that could not be
// removed must not mask the reason the upload was refused.
func removeEscapingLink(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		slog.Error("could not remove escaping symlink from a rejected archive", "path", path, "error", err)
	}
}

package systemcontroller

import (
	"archive/tar"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"time"
)

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
		// ALWAYS drain r to EOF before returning. The caller tees the *entire*
		// unpack stream into r's pipe (io.TeeReader -> validPW), including the
		// tar record padding and any bytes after the logical end-of-archive
		// (real `tar czf` pads every archive to a 10 KiB record). archive/tar
		// stops reading at the two zero blocks, so without this drain the bytes
		// after them are never read: the unbuffered io.Pipe writer blocks
		// forever, the unpack tar's stdin copier blocks, and unpackCmd.Wait()
		// deadlocks -- hanging the whole page-provisioning request. Draining on
		// every exit path (EOF, invalid tar, ctx cancel) keeps the writer
		// unblocked so the unpack can finish. The defer runs before close(ch)
		// (LIFO), and EOF arrives once the caller closes validPW after Wait().
		defer func() { _, _ = io.Copy(io.Discard, r) }()
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
			if _, err := io.Copy(io.Discard, tr); err != nil { //nolint:gosec // G110 -- size bounded by countingReader
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
	// archiveReader is a *bufio.Reader that replays peeked bytes, so the
	// returned reader from detectArchiveFormat is not needed separately.
	format, _, err := detectArchiveFormat(archiveReader)
	if err != nil {
		return err
	}

	// Also validate the extension independently.
	if _, extErr := archiveFormat(filename); extErr != nil {
		return extErr
	}

	basePath := s.Controller.GetBtrfsBasePath()
	targetPath, err := safeSubvolumePath(basePath, targetSubvol)
	if err != nil {
		return err
	}
	if subpath != "" {
		targetPath, err = safeSubvolumePath(basePath, targetSubvol, subpath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(targetPath, 0755); err != nil { //nolint:gosec // G301 -- web-serving directory
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
	unpackCmd := exec.CommandContext(ctx, "tar", append(tarExtractFlags(), "-C", targetPath)...) //nolint:gosec // G204 -- targetPath validated by safeSubvolumePath
	unpackCmd.Stdin = teeReader

	unpackStderr, err := unpackCmd.StderrPipe()
	if err != nil {
		if cerr := validPW.Close(); cerr != nil {
			slog.Debug("close validation pipe writer", "error", cerr)
		}
		return fmt.Errorf("unpack stderr pipe: %w", err)
	}

	if err := unpackCmd.Start(); err != nil {
		if cerr := validPW.Close(); cerr != nil {
			slog.Debug("close validation pipe writer", "error", cerr)
		}
		return fmt.Errorf("start unpack: %w", err)
	}

	// Wait for the unpack to finish (it drives the tee reader).
	unpackStderrOut, err := io.ReadAll(unpackStderr)
	if err != nil {
		slog.Debug("read unpack stderr", "error", err)
	}
	unpackErr := unpackCmd.Wait()

	// Close the validation pipe writer so the validator sees EOF.
	if cerr := validPW.Close(); cerr != nil {
		slog.Debug("close validation pipe writer", "error", cerr)
	}

	// Collect validation result.
	var validErr error
	for e := range validCh {
		if e != nil {
			validErr = e
		}
	}

	decompStderrOut, err := io.ReadAll(decompStderr)
	if err != nil {
		slog.Debug("read decompress stderr", "error", err)
	}
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

	unpackCmd := exec.CommandContext(ctx, "tar", append(tarExtractFlags(), "-C", targetPath)...) //nolint:gosec // G204 -- targetPath validated by safeSubvolumePath
	unpackCmd.Stdin = teeReader

	unpackStderr, err := unpackCmd.StderrPipe()
	if err != nil {
		if cerr := validPW.Close(); cerr != nil {
			slog.Debug("close validation pipe writer", "error", cerr)
		}
		return fmt.Errorf("unpack stderr pipe: %w", err)
	}

	if err := unpackCmd.Start(); err != nil {
		if cerr := validPW.Close(); cerr != nil {
			slog.Debug("close validation pipe writer", "error", cerr)
		}
		return fmt.Errorf("start unpack: %w", err)
	}

	unpackStderrOut, err := io.ReadAll(unpackStderr)
	if err != nil {
		slog.Debug("read unpack stderr", "error", err)
	}
	unpackErr := unpackCmd.Wait()

	if cerr := validPW.Close(); cerr != nil {
		slog.Debug("close validation pipe writer", "error", cerr)
	}

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

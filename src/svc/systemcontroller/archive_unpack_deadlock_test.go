package systemcontroller

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// These integration tests exercise the real archive-unpack pipeline (pigz + tar
// subprocesses) end to end. They are regression tests for a page-provisioning
// deadlock: validateTarStream tees the *entire* unpack stream into an unbuffered
// io.Pipe, but archive/tar stops reading at the two zero-blocks that mark the
// logical end of archive. Real tarballs carry trailing bytes after that point
// (GNU `tar czf` pads every archive to a 10 KiB record), so the validator used
// to return while bytes were still queued for it -- the io.Pipe writer then
// blocked forever, the unpack tar's stdin copier blocked, and unpackCmd.Wait()
// never returned. The fix drains the reader on every exit path.
//
// Each test runs the unpack under a wall-clock guard so the bug surfaces as a
// fast failure rather than hanging the whole suite: the deadlock ignores the
// unpack context timeout, so without the guard it would hang for the full
// 10-minute DefaultUnpackTimeout and beyond.

const deadlockGuard = 30 * time.Second

// requireBins skips the test unless every named binary is on PATH, since these
// drive the real pigz/tar subprocesses.
func requireBins(t *testing.T, bins ...string) {
	t.Helper()
	for _, b := range bins {
		if _, err := exec.LookPath(b); err != nil {
			t.Skipf("integration test requires %q on PATH: %v", b, err)
		}
	}
}

// tarWithTrailingPadding builds a valid tar of files, then appends `pad` zero
// bytes AFTER archive/tar's logical end-of-archive (its two zero blocks). This
// reproduces the trailing record padding that real `tar czf` always emits and
// that the validator must drain.
func tarWithTrailingPadding(t *testing.T, files map[string]string, pad int) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatalf("tar header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("tar body %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil { // writes the two zero blocks (logical EOF)
		t.Fatalf("tar close: %v", err)
	}
	buf.Write(make([]byte, pad)) // trailing bytes after logical EOF
	return buf.Bytes()
}

func gzipBytes(t *testing.T, b []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	gw := gzip.NewWriter(&out)
	if _, err := gw.Write(b); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return out.Bytes()
}

// guard runs fn and fails the test (instead of letting the suite hang) if it
// does not return within deadlockGuard.
func guard(t *testing.T, what string, fn func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(deadlockGuard):
		t.Fatalf("%s did not return within %s: validateTarStream pipe deadlock "+
			"(the io.Pipe writer blocks when the validator stops before draining trailing tar padding)",
			what, deadlockGuard)
		return nil
	}
}

func newArchiveHandlers(t *testing.T) (*SystemControllerHandlers, string) {
	t.Helper()
	base := t.TempDir()
	target := filepath.Join(base, "page")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	return &SystemControllerHandlers{Controller: &archiveTestBackend{btrfsBase: base}}, target
}

func assertExtracted(t *testing.T, target string, files map[string]string) {
	t.Helper()
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(target, name))
		if err != nil {
			t.Fatalf("read extracted %s: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("extracted %s = %q, want %q", name, got, want)
		}
	}
}

func TestStreamUnpackTarGzTrailingPaddingNoDeadlock(t *testing.T) {
	requireBins(t, "tar", "pigz")

	files := map[string]string{
		"index.html": "<html><body>hi from scarlett</body></html>\n",
		"CNAME":      "scarlett.home\n",
	}
	// 10 KiB of trailing zeros mimics `tar czf` record padding after logical EOF.
	gz := gzipBytes(t, tarWithTrailingPadding(t, files, 10*1024))

	s, target := newArchiveHandlers(t)
	err := guard(t, "streamUnpackToSubvolume(tar.gz)", func() error {
		return s.streamUnpackToSubvolume(
			context.Background(), bufio.NewReader(bytes.NewReader(gz)), "site.tar.gz", "page", "")
	})
	if err != nil {
		t.Fatalf("unpack tar.gz with trailing padding: %v", err)
	}
	assertExtracted(t, target, files)
}

// Plain (uncompressed) tar exercises the unpackPlainTar path, which has the same
// tee+validate structure as the gzip path. The magic-byte gate in
// streamUnpackToSubvolume rejects header-less plain tar, so this drives
// unpackPlainTar directly. WITHOUT the validateTarStream drain fix this
// deadlocks and the guard fails the test; WITH it the files extract cleanly.
func TestUnpackPlainTarTrailingPaddingNoDeadlock(t *testing.T) {
	requireBins(t, "tar")

	files := map[string]string{
		"index.html": "<html>plain</html>\n",
		"CNAME":      "plain.home\n",
	}
	raw := tarWithTrailingPadding(t, files, 10*1024) // plain tar + trailing padding

	s, target := newArchiveHandlers(t)
	cr := &countingReader{r: io.LimitReader(bytes.NewReader(raw), DefaultMaxArchiveSize+1)}
	err := guard(t, "unpackPlainTar", func() error {
		return s.unpackPlainTar(context.Background(), cr, target, DefaultMaxArchiveSize)
	})
	if err != nil {
		t.Fatalf("unpackPlainTar with trailing padding: %v", err)
	}
	assertExtracted(t, target, files)
}

func TestStreamUnpackTarGzManyFilesNoDeadlock(t *testing.T) {
	requireBins(t, "tar", "pigz")

	files := map[string]string{}
	for i := range 64 {
		files[filepath.Join("dir", "file"+itoa(i)+".txt")] = "content-" + itoa(i) + "\n"
	}
	gz := gzipBytes(t, tarWithTrailingPadding(t, files, 10*1024))

	s, target := newArchiveHandlers(t)
	err := guard(t, "streamUnpackToSubvolume(many)", func() error {
		return s.streamUnpackToSubvolume(
			context.Background(), bufio.NewReader(bytes.NewReader(gz)), "many.tar.gz", "page", "")
	})
	if err != nil {
		t.Fatalf("unpack many-file tar.gz: %v", err)
	}
	assertExtracted(t, target, map[string]string{
		filepath.Join("dir", "file7.txt"): "content-7\n",
	})
}

// An invalid tar (valid gzip wrapping non-tar bytes) must return ErrInvalidTar
// promptly -- the error path must also drain the reader so it cannot deadlock.
func TestStreamUnpackTarGzInvalidTarNoDeadlock(t *testing.T) {
	requireBins(t, "tar", "pigz")

	gz := gzipBytes(t, bytes.Repeat([]byte("this is definitely not a tar archive\n"), 512))

	s, _ := newArchiveHandlers(t)
	err := guard(t, "streamUnpackToSubvolume(invalid)", func() error {
		return s.streamUnpackToSubvolume(
			context.Background(), bufio.NewReader(bytes.NewReader(gz)), "bad.tar.gz", "page", "")
	})
	if !errors.Is(err, ErrInvalidTar) {
		t.Fatalf("invalid archive: got err %v, want ErrInvalidTar", err)
	}
}

// itoa is a tiny dependency-free int-to-string for test data.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

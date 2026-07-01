package systemcontroller

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

// Regression tests for a page-provisioning deadlock, written as PURE Go so they
// have no external dependencies (no pigz/tar subprocess) and run safely on the
// build host.
//
// The bug was entirely inside validateTarStream. The unpack pipeline tees the
// *entire* archive stream into an unbuffered io.Pipe (io.TeeReader -> validPW)
// while another reader (tar's stdin) drives it; validateTarStream consumes the
// pipe's read end. archive/tar stops reading at the two zero blocks that mark
// the logical end of archive, but real tarballs carry trailing bytes after that
// (GNU `tar czf` pads every archive to a 10 KiB record). validateTarStream used
// to return at logical EOF *without draining the rest of r*, so the leftover
// bytes had no reader: the io.Pipe writer blocked forever, the unpack's stdin
// copier blocked, and unpackCmd.Wait() never returned -- hanging the whole
// page-create request (the GUI sat at "Provisioning..." indefinitely).
//
// These tests pin the fixed contract directly: validateTarStream MUST drain r to
// EOF on every exit path (valid EOF, invalid tar, context cancel), so a paired
// io.Pipe writer always unblocks. They reproduce the exact failure mode (an
// io.Pipe writer that blocks when the validator stops early) without any
// subprocess, and fail via a short timeout if the drain is removed.

const drainTimeout = 5 * time.Second

// validTarBytes builds a valid tar of files and returns the raw bytes. tar.Writer
// emits the two zero blocks at Close; archive/tar's Reader stops there.
func validTarBytes(t *testing.T, files map[string]string) []byte {
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
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	return buf.Bytes()
}

// withTrailingZeros returns a fresh slice holding src followed by n zero bytes,
// mimicking the record padding that real `tar czf` appends after end-of-archive.
func withTrailingZeros(src []byte, n int) []byte {
	padded := make([]byte, len(src)+n)
	copy(padded, src)
	return padded
}

// assertValidatorDrains feeds `payload` to validateTarStream through an io.Pipe,
// exactly mirroring how the unpack pipeline tees the stream into validPW, and
// asserts BOTH that the validator returns the expected error AND that the writer
// fully drains (its blocking pw.Write completes). If validateTarStream returns
// without draining, the trailing bytes leave pw.Write blocked forever and the
// writer-drain wait times out -- reproducing the production deadlock with no
// subprocess. `wantErr` is matched with errors.Is (nil means "no error").
func assertValidatorDrains(t *testing.T, payload []byte, wantErr error) {
	t.Helper()

	pr, pw := io.Pipe()
	ch := validateTarStream(context.Background(), pr)

	writerDone := make(chan error, 1)
	go func() {
		// One Write of the whole payload. io.Pipe Write blocks until every byte
		// is consumed by reads, so it only returns once validateTarStream has
		// drained the trailing padding. Close mirrors the caller closing validPW
		// after the unpack finishes, giving the validator's drain its EOF.
		_, werr := pw.Write(payload)
		_ = pw.Close()
		writerDone <- werr
	}()

	// 1) validateTarStream must return (its channel closes) with the right verdict.
	select {
	case gotErr := <-ch:
		if wantErr == nil && gotErr != nil {
			t.Fatalf("validateTarStream: unexpected error %v", gotErr)
		}
		if wantErr != nil && !errors.Is(gotErr, wantErr) {
			t.Fatalf("validateTarStream: got err %v, want %v", gotErr, wantErr)
		}
	case <-time.After(drainTimeout):
		t.Fatalf("validateTarStream did not return within %s", drainTimeout)
	}

	// 2) The writer must have fully drained. If validateTarStream stopped before
	// reading the trailing bytes, pw.Write is still blocked -> deadlock.
	select {
	case werr := <-writerDone:
		if werr != nil {
			t.Fatalf("pipe writer: %v", werr)
		}
	case <-time.After(drainTimeout):
		t.Fatalf("io.Pipe writer still blocked after %s: validateTarStream did not drain r "+
			"(trailing tar padding left unread -> unpack stdin copier deadlock)", drainTimeout)
	}
}

func TestValidateTarStreamDrainsTrailingPadding(t *testing.T) {
	tarBytes := validTarBytes(t, map[string]string{
		"index.html": "<html><body>hi from scarlett</body></html>\n",
		"CNAME":      "scarlett.home\n",
	})
	// Append 10 KiB of trailing zeros after the logical end-of-archive, mimicking
	// the record padding that real `tar czf` always emits.
	padded := withTrailingZeros(tarBytes, 10*1024)
	assertValidatorDrains(t, padded, nil)
}

func TestValidateTarStreamDrainsManyFiles(t *testing.T) {
	files := map[string]string{}
	for i := range 64 {
		files["dir/file"+itoa(i)+".txt"] = "content-" + itoa(i) + "\n"
	}
	padded := withTrailingZeros(validTarBytes(t, files), 10*1024)
	assertValidatorDrains(t, padded, nil)
}

// The error path must also drain: an invalid tar followed by trailing bytes must
// return ErrInvalidTar AND leave no blocked writer.
func TestValidateTarStreamDrainsOnInvalidTar(t *testing.T) {
	payload := bytes.Repeat([]byte("this is definitely not a tar archive\n"), 512)
	assertValidatorDrains(t, payload, ErrInvalidTar)
}

// Context cancel must also drain so a cancelled provisioning request can't wedge
// the io.Pipe writer either.
func TestValidateTarStreamDrainsOnCancel(t *testing.T) {
	tarBytes := validTarBytes(t, map[string]string{"index.html": "<html>x</html>\n"})
	padded := withTrailingZeros(tarBytes, 10*1024)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the validator starts

	pr, pw := io.Pipe()
	ch := validateTarStream(ctx, pr)

	writerDone := make(chan error, 1)
	go func() {
		_, werr := pw.Write(padded)
		_ = pw.Close()
		writerDone <- werr
	}()

	select {
	case <-ch:
	case <-time.After(drainTimeout):
		t.Fatalf("validateTarStream did not return on cancel within %s", drainTimeout)
	}
	select {
	case <-writerDone:
	case <-time.After(drainTimeout):
		t.Fatalf("io.Pipe writer still blocked after cancel: validateTarStream did not drain r")
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

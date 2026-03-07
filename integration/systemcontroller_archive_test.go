package integration_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// makeTarGz builds a tar.gz archive in memory from a map of filename -> content.
func makeTarGz(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar write header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("tar write body %s: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return &buf
}

// extractTarGz reads a tar.gz stream and returns a map of filename -> content.
func extractTarGz(t *testing.T, r io.Reader) map[string]string {
	t.Helper()
	gr, err := gzip.NewReader(r)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer func() {
		if err := gr.Close(); err != nil {
			t.Errorf("gzip close: %v", err)
		}
	}()

	result := make(map[string]string)
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("tar read %s: %v", hdr.Name, err)
		}
		result[hdr.Name] = string(data)
	}
	return result
}

func mapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestArchiveUploadAndDownload(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()
	subvol := "archive-test-upload"

	if err := c.CreateFilesystem(ctx, storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(ctx, subvol); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", subvol, err)
		}
	})

	// Build and upload a tar.gz containing hello.txt.
	archive := makeTarGz(t, map[string]string{"hello.txt": "hello world"})
	resp, err := c.UploadArchive(ctx, subvol, archive, "test.tar.gz", "", "")
	if err != nil {
		t.Fatalf("UploadArchive: %v", err)
	}
	if resp.Message == "" {
		t.Fatal("expected non-empty upload response message")
	}

	// Verify the file was unpacked on disk.
	got, err := os.ReadFile(filepath.Join("/data/btrfs", subvol, "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile hello.txt: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("expected %q, got %q", "hello world", string(got))
	}

	// Download the archive and verify contents.
	rc, err := c.DownloadArchive(ctx, subvol, nil, "", "", "")
	if err != nil {
		t.Fatalf("DownloadArchive: %v", err)
	}
	defer func() {
		if err := rc.Close(); err != nil {
			t.Errorf("close download body: %v", err)
		}
	}()

	files := extractTarGz(t, rc)
	content, ok := files["hello.txt"]
	if !ok {
		// tar may prefix with "./"
		content, ok = files["./hello.txt"]
	}
	if !ok {
		t.Fatalf("hello.txt not found in downloaded archive, got keys: %v", mapKeys(files))
	}
	if content != "hello world" {
		t.Fatalf("downloaded hello.txt: expected %q, got %q", "hello world", content)
	}
}

func TestArchiveDownloadWithPaths(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()
	subvol := "archive-test-paths"

	if err := c.CreateFilesystem(ctx, storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(ctx, subvol); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", subvol, err)
		}
	})

	// Upload archive with two files.
	archive := makeTarGz(t, map[string]string{
		"a.txt": "content-a",
		"b.txt": "content-b",
	})
	if _, err := c.UploadArchive(ctx, subvol, archive, "test.tar.gz", "", ""); err != nil {
		t.Fatalf("UploadArchive: %v", err)
	}

	// Download with only a.txt requested.
	rc, err := c.DownloadArchive(ctx, subvol, []string{"a.txt"}, "", "", "")
	if err != nil {
		t.Fatalf("DownloadArchive: %v", err)
	}
	defer func() {
		if err := rc.Close(); err != nil {
			t.Errorf("close download body: %v", err)
		}
	}()

	files := extractTarGz(t, rc)
	if _, ok := files["a.txt"]; !ok {
		t.Fatalf("expected a.txt in archive, got keys: %v", mapKeys(files))
	}
	if _, ok := files["b.txt"]; ok {
		t.Fatal("b.txt should not be in the filtered archive")
	}
}

func TestArchiveUploadUnsupportedFormat(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()
	subvol := "archive-test-unsupported"

	if err := c.CreateFilesystem(ctx, storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(ctx, subvol); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", subvol, err)
		}
	})

	_, err := c.UploadArchive(ctx, subvol, strings.NewReader("not a real archive"), "test.zip", "", "")
	if err == nil {
		t.Fatal("expected error for unsupported format, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported error, got: %v", err)
	}
}

func TestArchiveUploadWithSubpath(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()
	subvol := "archive-test-subpath"

	if err := c.CreateFilesystem(ctx, storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(ctx, subvol); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", subvol, err)
		}
	})

	archive := makeTarGz(t, map[string]string{"nested.txt": "nested content"})
	if _, err := c.UploadArchive(ctx, subvol, archive, "test.tar.gz", "deep/sub", ""); err != nil {
		t.Fatalf("UploadArchive with subpath: %v", err)
	}

	got, err := os.ReadFile(filepath.Join("/data/btrfs", subvol, "deep/sub/nested.txt"))
	if err != nil {
		t.Fatalf("ReadFile nested.txt: %v", err)
	}
	if string(got) != "nested content" {
		t.Fatalf("expected %q, got %q", "nested content", string(got))
	}
}

func TestArchiveUploadDownloadInstalledVolume(t *testing.T) {
	c, btr := initSystemControllerTestWithStorageAndBtrfsBase(t)
	ctx := context.TODO()
	subvol := "installed/testrepo/testpkg/1.0/data"

	if err := btr.CreateFilesystem(storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := btr.RemoveFilesystem(subvol); err != nil {
			t.Errorf("RemoveFilesystem: %v", err)
		}
		if err := btr.RemoveFilesystem("installed/testrepo/testpkg/1.0"); err != nil {
			t.Errorf("RemoveFilesystem: %v", err)
		}
		if err := btr.RemoveFilesystem("installed/testrepo/testpkg"); err != nil {
			t.Errorf("RemoveFilesystem: %v", err)
		}
		if err := btr.RemoveFilesystem("installed/testrepo"); err != nil {
			t.Errorf("RemoveFilesystem: %v", err)
		}
	})

	archive := makeTarGz(t, map[string]string{"pkg-data.txt": "package data"})
	if _, err := c.UploadArchive(ctx, subvol, archive, "test.tar.gz", "", ""); err != nil {
		t.Fatalf("UploadArchive on installed volume: %v", err)
	}

	got, err := os.ReadFile(filepath.Join("/data/btrfs", subvol, "pkg-data.txt"))
	if err != nil {
		t.Fatalf("ReadFile pkg-data.txt: %v", err)
	}
	if string(got) != "package data" {
		t.Fatalf("expected %q, got %q", "package data", string(got))
	}

	rc, err := c.DownloadArchive(ctx, subvol, nil, "", "", "")
	if err != nil {
		t.Fatalf("DownloadArchive on installed volume: %v", err)
	}
	defer func() {
		if err := rc.Close(); err != nil {
			t.Errorf("close download body: %v", err)
		}
	}()

	files := extractTarGz(t, rc)
	content, ok := files["pkg-data.txt"]
	if !ok {
		content, ok = files["./pkg-data.txt"]
	}
	if !ok {
		t.Fatalf("pkg-data.txt not found in downloaded archive, got keys: %v", mapKeys(files))
	}
	if content != "package data" {
		t.Fatalf("downloaded pkg-data.txt: expected %q, got %q", "package data", content)
	}
}

func TestModifyInstalledVolumeQuota(t *testing.T) {
	c, btr := initSystemControllerTestWithStorageAndBtrfsBase(t)
	ctx := context.TODO()
	subvol := "installed/testrepo/quotapkg/1.0/data"

	if err := btr.CreateFilesystem(storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := btr.RemoveFilesystem(subvol); err != nil {
			t.Errorf("RemoveFilesystem: %v", err)
		}
		if err := btr.RemoveFilesystem("installed/testrepo/quotapkg/1.0"); err != nil {
			t.Errorf("RemoveFilesystem: %v", err)
		}
		if err := btr.RemoveFilesystem("installed/testrepo/quotapkg"); err != nil {
			t.Errorf("RemoveFilesystem: %v", err)
		}
		if err := btr.RemoveFilesystem("installed/testrepo"); err != nil {
			t.Errorf("RemoveFilesystem: %v", err)
		}
	})

	if err := c.ModifyFilesystem(ctx, subvol, storage.Filesystem{
		Name:  subvol,
		Quota: 1073741824,
	}); err != nil {
		t.Fatalf("ModifyFilesystem quota on installed volume: %v", err)
	}

	result, err := c.ListFilesystems(ctx, "", "installed", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	found := false
	for _, f := range result.Entries {
		if f.Name == "quotapkg/1.0/data" {
			found = true
			if f.Quota != 1073741824 {
				t.Fatalf("expected quota 1073741824, got %d", f.Quota)
			}
			break
		}
	}
	if !found {
		t.Fatal("installed volume not found in listing after quota modification")
	}
}

func TestArchiveDownloadInstalledSubvolume(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()

	// Installed volumes are now allowed for archive operations.
	// This will fail with a tar error (directory doesn't exist) rather
	// than a reserved filesystem error.
	_, err := c.DownloadArchive(ctx, "installed/repo/pkg/1.0/data", nil, "", "", "")
	if err == nil {
		// If the directory happens to exist, the download would succeed.
		return
	}
	if strings.Contains(err.Error(), "reserved") {
		t.Fatalf("installed volumes should not be rejected as reserved: %v", err)
	}
}

func TestArchiveDownloadWithBzip2Format(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()
	subvol := "archive-test-bz2-dl"

	if err := c.CreateFilesystem(ctx, storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(ctx, subvol); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", subvol, err)
		}
	})

	// Upload a file first.
	archive := makeTarGz(t, map[string]string{"hello.txt": "hello bz2"})
	if _, err := c.UploadArchive(ctx, subvol, archive, "test.tar.gz", "", ""); err != nil {
		t.Fatalf("UploadArchive: %v", err)
	}

	// Download as bzip2.
	rc, err := c.DownloadArchive(ctx, subvol, nil, "", "tar.bz2", "")
	if err != nil {
		t.Fatalf("DownloadArchive bzip2: %v", err)
	}
	defer func() {
		if err := rc.Close(); err != nil {
			t.Errorf("rc.Close: %v", err)
		}
	}()

	// Verify the bzip2 magic bytes.
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(data) < 3 {
		t.Fatal("downloaded archive is too small")
	}
	if data[0] != 0x42 || data[1] != 0x5a || data[2] != 0x68 {
		t.Fatalf("expected bzip2 magic bytes, got %x %x %x", data[0], data[1], data[2])
	}
}

func TestArchiveDownloadWithXZFormat(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()
	subvol := "archive-test-xz-dl"

	if err := c.CreateFilesystem(ctx, storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(ctx, subvol); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", subvol, err)
		}
	})

	// Upload a file first.
	archive := makeTarGz(t, map[string]string{"hello.txt": "hello xz"})
	if _, err := c.UploadArchive(ctx, subvol, archive, "test.tar.gz", "", ""); err != nil {
		t.Fatalf("UploadArchive: %v", err)
	}

	// Download as xz.
	rc, err := c.DownloadArchive(ctx, subvol, nil, "", "tar.xz", "")
	if err != nil {
		t.Fatalf("DownloadArchive xz: %v", err)
	}
	defer func() {
		if err := rc.Close(); err != nil {
			t.Errorf("rc.Close: %v", err)
		}
	}()

	// Verify the xz magic bytes.
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(data) < 6 {
		t.Fatal("downloaded archive is too small")
	}
	if data[0] != 0xfd || data[1] != 0x37 || data[2] != 0x7a || data[3] != 0x58 || data[4] != 0x5a || data[5] != 0x00 {
		t.Fatalf("expected xz magic bytes, got %x %x %x %x %x %x", data[0], data[1], data[2], data[3], data[4], data[5])
	}
}

func TestArchiveDownloadWithFilename(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()
	subvol := "archive-test-filename"

	if err := c.CreateFilesystem(ctx, storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(ctx, subvol); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", subvol, err)
		}
	})

	archive := makeTarGz(t, map[string]string{"data.txt": "test data"})
	if _, err := c.UploadArchive(ctx, subvol, archive, "test.tar.gz", "", ""); err != nil {
		t.Fatalf("UploadArchive: %v", err)
	}

	rc, err := c.DownloadArchive(ctx, subvol, nil, "", "", "my-backup")
	if err != nil {
		t.Fatalf("DownloadArchive with filename: %v", err)
	}
	defer func() {
		if err := rc.Close(); err != nil {
			t.Errorf("rc.Close: %v", err)
		}
	}()

	files := extractTarGz(t, rc)
	if _, ok := files["data.txt"]; !ok {
		if _, ok = files["./data.txt"]; !ok {
			t.Fatalf("data.txt not found in downloaded archive, got keys: %v", mapKeys(files))
		}
	}
}

func TestArchiveDownloadInvalidFormat(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()

	_, err := c.DownloadArchive(ctx, "anything", nil, "", "tar.zst", "")
	if err == nil {
		t.Fatal("expected error for unsupported download format")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported error, got: %v", err)
	}
}

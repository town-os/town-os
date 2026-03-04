package systemcontroller

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
)

func initVMImagesTestClient(t *testing.T) (*SystemdClient, string) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	btrfsBase := t.TempDir()
	ts := InitTestServer(ServerConfig{Storage: mock, BtrfsBasePath: btrfsBase})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, btrfsBase
}

func TestListVMImagesEmpty(t *testing.T) {
	c, _ := initVMImagesTestClient(t)
	images, err := c.ListVMImages(context.Background())
	if err != nil {
		t.Fatalf("ListVMImages: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("expected empty list, got %d items", len(images))
	}
}

func TestListVMImagesWithFiles(t *testing.T) {
	c, btrfsBase := initVMImagesTestClient(t)

	// Create the vm-images directory and add some files.
	dir := filepath.Join(btrfsBase, VMImagesSubvolume)
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "debian.raw"), []byte("fake-image-data"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "alpine.raw"), []byte("smaller"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	images, err := c.ListVMImages(context.Background())
	if err != nil {
		t.Fatalf("ListVMImages: %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}

	// Verify images are returned with correct sizes.
	found := map[string]int64{}
	for _, img := range images {
		found[img.Name] = img.Size
	}
	if found["debian.raw"] != int64(len("fake-image-data")) {
		t.Fatalf("expected debian.raw size %d, got %d", len("fake-image-data"), found["debian.raw"])
	}
	if found["alpine.raw"] != int64(len("smaller")) {
		t.Fatalf("expected alpine.raw size %d, got %d", len("smaller"), found["alpine.raw"])
	}
}

func TestListVMImagesSkipsDirectories(t *testing.T) {
	c, btrfsBase := initVMImagesTestClient(t)

	dir := filepath.Join(btrfsBase, VMImagesSubvolume)
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "only-file.raw"), []byte("data"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	images, err := c.ListVMImages(context.Background())
	if err != nil {
		t.Fatalf("ListVMImages: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("expected 1 image (skipping directories), got %d", len(images))
	}
	if images[0].Name != "only-file.raw" {
		t.Fatalf("expected only-file.raw, got %s", images[0].Name)
	}
}

func TestDeleteVMImage(t *testing.T) {
	c, btrfsBase := initVMImagesTestClient(t)

	dir := filepath.Join(btrfsBase, VMImagesSubvolume)
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	imgPath := filepath.Join(dir, "to-delete.raw")
	if err := os.WriteFile(imgPath, []byte("delete-me"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := c.DeleteVMImage(context.Background(), "to-delete.raw")
	if err != nil {
		t.Fatalf("DeleteVMImage: %v", err)
	}

	// Verify file was removed.
	if _, err := os.Stat(imgPath); !os.IsNotExist(err) {
		t.Fatal("expected file to be deleted")
	}
}

func TestDeleteVMImageNotFound(t *testing.T) {
	c, btrfsBase := initVMImagesTestClient(t)

	// Ensure the directory exists so the error is "not found" rather than "no dir".
	dir := filepath.Join(btrfsBase, VMImagesSubvolume)
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	err := c.DeleteVMImage(context.Background(), "nonexistent.raw")
	if err == nil {
		t.Fatal("expected error for nonexistent image")
	}
}

func TestDeleteVMImageSanitizesPath(t *testing.T) {
	c, btrfsBase := initVMImagesTestClient(t)

	// Ensure the vm-images directory exists.
	dir := filepath.Join(btrfsBase, VMImagesSubvolume)
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Attempt path traversal — filepath.Base should strip the traversal.
	// The sanitized name "passwd" won't exist, so we expect an error.
	err := c.DeleteVMImage(context.Background(), "../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for traversal path")
	}
}

func TestResolveVMImagePathURLNoExtension(t *testing.T) {
	path := resolveVMImagePath("/data/btrfs", "https://example.com/images/myvm")
	expected := "/data/btrfs/vm-images/myvm.raw"
	if path != expected {
		t.Fatalf("expected %s, got %s", expected, path)
	}
}

func TestResolveVMImagePathLocalNoExtension(t *testing.T) {
	path := resolveVMImagePath("/data/btrfs", "myimage")
	expected := "/data/btrfs/vm-images/myimage.raw"
	if path != expected {
		t.Fatalf("expected %s, got %s", expected, path)
	}
}

func TestResolveVMImagePathURLWithMultipleDots(t *testing.T) {
	path := resolveVMImagePath("/data/btrfs", "https://example.com/my.special.image.qcow2")
	expected := "/data/btrfs/vm-images/my.special.image.raw"
	if path != expected {
		t.Fatalf("expected %s, got %s", expected, path)
	}
}

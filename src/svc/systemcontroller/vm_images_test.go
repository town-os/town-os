package systemcontroller

import (
	"testing"
)

func TestResolveVMImagePath(t *testing.T) {
	t.Run("URL to cached raw path", func(t *testing.T) {
		path := resolveVMImagePath("/data/btrfs", "https://example.com/debian-12.qcow2")
		expected := "/data/btrfs/vm-images/debian-12.raw"
		if path != expected {
			t.Fatalf("expected %s, got %s", expected, path)
		}
	})

	t.Run("URL with raw extension", func(t *testing.T) {
		path := resolveVMImagePath("/data/btrfs", "https://example.com/debian-12.raw")
		expected := "/data/btrfs/vm-images/debian-12.raw"
		if path != expected {
			t.Fatalf("expected %s, got %s", expected, path)
		}
	})

	t.Run("local filename with extension", func(t *testing.T) {
		path := resolveVMImagePath("/data/btrfs", "debian-12.qcow2")
		expected := "/data/btrfs/vm-images/debian-12.raw"
		if path != expected {
			t.Fatalf("expected %s, got %s", expected, path)
		}
	})

	t.Run("local filename already raw", func(t *testing.T) {
		path := resolveVMImagePath("/data/btrfs", "debian-12.raw")
		expected := "/data/btrfs/vm-images/debian-12.raw"
		if path != expected {
			t.Fatalf("expected %s, got %s", expected, path)
		}
	})

	t.Run("http URL", func(t *testing.T) {
		path := resolveVMImagePath("/data/btrfs", "http://mirror.local/images/vm.img")
		expected := "/data/btrfs/vm-images/vm.raw"
		if path != expected {
			t.Fatalf("expected %s, got %s", expected, path)
		}
	})

	t.Run("plain name no extension", func(t *testing.T) {
		path := resolveVMImagePath("/data/btrfs", "myimage")
		expected := "/data/btrfs/vm-images/myimage.raw"
		if path != expected {
			t.Fatalf("expected %s, got %s", expected, path)
		}
	})
}

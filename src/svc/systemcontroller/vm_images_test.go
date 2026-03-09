// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"testing"
)

func TestResolveVMImagePath(t *testing.T) {
	t.Run("URL to cached raw path", func(t *testing.T) {
		path := resolveVMImagePath("/town-os", "https://example.com/debian-12.qcow2")
		expected := "/town-os/vm-images/debian-12.raw"
		if path != expected {
			t.Fatalf("expected %s, got %s", expected, path)
		}
	})

	t.Run("URL with raw extension", func(t *testing.T) {
		path := resolveVMImagePath("/town-os", "https://example.com/debian-12.raw")
		expected := "/town-os/vm-images/debian-12.raw"
		if path != expected {
			t.Fatalf("expected %s, got %s", expected, path)
		}
	})

	t.Run("local filename with extension", func(t *testing.T) {
		path := resolveVMImagePath("/town-os", "debian-12.qcow2")
		expected := "/town-os/vm-images/debian-12.raw"
		if path != expected {
			t.Fatalf("expected %s, got %s", expected, path)
		}
	})

	t.Run("local filename already raw", func(t *testing.T) {
		path := resolveVMImagePath("/town-os", "debian-12.raw")
		expected := "/town-os/vm-images/debian-12.raw"
		if path != expected {
			t.Fatalf("expected %s, got %s", expected, path)
		}
	})

	t.Run("http URL", func(t *testing.T) {
		path := resolveVMImagePath("/town-os", "http://mirror.local/images/vm.img")
		expected := "/town-os/vm-images/vm.raw"
		if path != expected {
			t.Fatalf("expected %s, got %s", expected, path)
		}
	})

	t.Run("plain name no extension", func(t *testing.T) {
		path := resolveVMImagePath("/town-os", "myimage")
		expected := "/town-os/vm-images/myimage.raw"
		if path != expected {
			t.Fatalf("expected %s, got %s", expected, path)
		}
	})
}

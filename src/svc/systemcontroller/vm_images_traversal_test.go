// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"path/filepath"
	"strings"
	"testing"
)

// resolveVMImagePath turns a package's `vm.image` into a path under the
// vm-images subvolume:
//
//	return filepath.Join(basePath, VMImagesSubvolume, vmImage)
//
// It applies filepath.Base to neither branch, unlike uploadVMImage and
// deleteVMImage, which both sanitize the name they were handed. So a package
// YAML naming "../../data/db/system.db" resolves outside the subvolume, and the
// resolved path is what the generated QEMU unit hands to
// `-drive file=...,format=raw` -- i.e. what the VM gets as a block device, and
// what qemu-img writes over when the image is converted.
//
// The value comes from a package manifest in a configured repository, not from
// an HTTP request, so exploiting it means controlling a repo the operator
// added. That is a real trust boundary (a repository is a code-supply channel)
// but a weak one, which is why this is defence in depth: the fix is one call to
// filepath.Base, matching what the two sibling handlers already do.
//
// These tests assert the SECURE behaviour and fail against the current code.

func TestResolveVMImagePathStaysInsideSubvolume(t *testing.T) {
	t.Parallel()

	base := "/town-os"
	prefix := filepath.Join(base, VMImagesSubvolume) + string(filepath.Separator)

	for name, image := range map[string]string{
		"parent traversal":        "../escape.raw",
		"deep traversal":          "../../data/db/system.db",
		"traversal to CA key":     "../tls/ca.key",
		"absolute path":           "/etc/shadow",
		"traversal via extension": "../../tls/ca.raw",
		"nested traversal":        "sub/../../escape.raw",
		"url with traversal":      "https://example.com/a/../../../../etc/shadow",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := resolveVMImagePath(base, image)
			if !strings.HasPrefix(got, prefix) {
				t.Errorf("resolveVMImagePath(%q, %q) = %q, which is outside %q; "+
					"the result is handed to qemu as -drive file= and to qemu-img as a write target",
					base, image, got, prefix)
			}
		})
	}
}

// The counterpart: ordinary names must still resolve where they always did,
// including the .raw suffixing and the URL-derived cache filename.
func TestResolveVMImagePathOrdinaryNamesUnchanged(t *testing.T) {
	t.Parallel()

	base := "/town-os"
	dir := filepath.Join(base, VMImagesSubvolume)

	for name, tc := range map[string]struct{ image, want string }{
		"local raw":       {image: "debian.raw", want: filepath.Join(dir, "debian.raw")},
		"local other ext": {image: "debian.qcow2", want: filepath.Join(dir, "debian.raw")},
		"local no ext":    {image: "debian", want: filepath.Join(dir, "debian.raw")},
		"https url":       {image: "https://example.com/images/debian-13.qcow2", want: filepath.Join(dir, "debian-13.raw")},
		"http url":        {image: "http://example.com/images/alpine.img", want: filepath.Join(dir, "alpine.raw")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := resolveVMImagePath(base, tc.image); got != tc.want {
				t.Errorf("resolveVMImagePath(%q, %q) = %q, want %q", base, tc.image, got, tc.want)
			}
		})
	}
}

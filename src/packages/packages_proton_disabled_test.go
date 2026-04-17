//go:build !proton

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"errors"
	"testing"
)

func TestProtonDisabledRejectsManifest(t *testing.T) {
	input := InputPackage{
		Image: InputPackageImage{Type: ImageTypeOCI},
		Proton: &InputPackageProton{
			AppImage:     "mycompany/windows-app:1.0",
			AppDirectory: "/app",
			Volume:       "app",
			Exe:          "/app/myapp.exe",
		},
		Environment: map[string]string{},
		Network:     InputPackageNetwork{},
		Volumes:     map[string]InputPackageVolume{"app": {Mountpoint: "/app"}},
		Questions:   map[string]Question{},
	}
	_, err := input.Compile(Responses{})
	if err == nil {
		t.Fatal("expected ErrProtonNotEnabled when compiling a proton manifest without the build tag")
	}
	if !errors.Is(err, ErrProtonNotEnabled) {
		t.Fatalf("expected ErrProtonNotEnabled, got %v", err)
	}
}

func TestProtonEnabledFlagDisabled(t *testing.T) {
	if ProtonEnabled() {
		t.Fatal("ProtonEnabled() should be false without the `proton` build tag")
	}
}

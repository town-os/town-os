//go:build proton

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"errors"
	"testing"
)

func TestCompileProtonCommandGeneration(t *testing.T) {
	t.Run("generates proton run command", func(t *testing.T) {
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
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(p.Command) != 3 {
			t.Fatalf("expected 3 command args, got %d: %v", len(p.Command), p.Command)
		}
		if p.Command[0] != "proton" || p.Command[1] != "run" || p.Command[2] != "/app/myapp.exe" {
			t.Fatalf("expected [proton run /app/myapp.exe], got %v", p.Command)
		}
	})

	t.Run("generates proton run command with args", func(t *testing.T) {
		input := InputPackage{
			Image: InputPackageImage{Type: ImageTypeOCI},
			Proton: &InputPackageProton{
				AppImage:     "mycompany/windows-app:1.0",
				AppDirectory: "/app",
				Volume:       "app",
				Exe:          "/app/myapp.exe",
				Args:         []string{"-fullscreen", "-config", "/app/config.ini"},
			},
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"app": {Mountpoint: "/app"}},
			Questions:   map[string]Question{},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(p.Command) != 6 {
			t.Fatalf("expected 6 command args, got %d: %v", len(p.Command), p.Command)
		}
		expected := []string{"proton", "run", "/app/myapp.exe", "-fullscreen", "-config", "/app/config.ini"}
		for i, v := range expected {
			if p.Command[i] != v {
				t.Fatalf("command[%d] = %q, want %q", i, p.Command[i], v)
			}
		}
	})

	t.Run("proton populates Package.Proton", func(t *testing.T) {
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
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Proton == nil {
			t.Fatal("expected non-nil Proton")
		}
		if p.Proton.AppImage != "docker.io/mycompany/windows-app:1.0" {
			t.Fatalf("expected normalized app image, got %s", p.Proton.AppImage)
		}
		if p.Proton.AppDirectory != "/app" {
			t.Fatalf("expected /app, got %s", p.Proton.AppDirectory)
		}
		if p.Proton.Volume != "app" {
			t.Fatalf("expected app volume, got %s", p.Proton.Volume)
		}
		if p.Proton.Exe != "/app/myapp.exe" {
			t.Fatalf("expected /app/myapp.exe, got %s", p.Proton.Exe)
		}
	})

	t.Run("proton with image url uses that url", func(t *testing.T) {
		input := InputPackage{
			Image: InputPackageImage{Type: ImageTypeOCI, URL: "my-custom-proton:latest"},
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
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Image != "docker.io/library/my-custom-proton:latest" {
			t.Fatalf("expected normalized custom proton image, got %s", p.Image)
		}
	})

	t.Run("proton without image url leaves image empty", func(t *testing.T) {
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
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Image != "" {
			t.Fatalf("expected empty image, got %s", p.Image)
		}
	})

	t.Run("rejects both command and proton", func(t *testing.T) {
		input := InputPackage{
			Image:   InputPackageImage{Type: ImageTypeOCI},
			Command: []string{"custom", "command"},
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
			t.Fatal("expected error for both command and proton")
		}
		if !errors.Is(err, ErrInvalidProtonSpec) {
			t.Fatalf("expected ErrInvalidProtonSpec, got %v", err)
		}
	})

	t.Run("proton template substitution", func(t *testing.T) {
		input := InputPackage{
			Image: InputPackageImage{Type: ImageTypeOCI},
			Proton: &InputPackageProton{
				AppImage:     "mycompany/@appname@:1.0",
				AppDirectory: "/app",
				Volume:       "app",
				Exe:          "/app/@appname@.exe",
			},
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"app": {Mountpoint: "/app"}},
			Questions:   map[string]Question{"appname": {Query: "App name?"}},
		}
		p, err := input.Compile(Responses{"appname": "myapp"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Proton.Exe != "/app/myapp.exe" {
			t.Fatalf("expected /app/myapp.exe, got %s", p.Proton.Exe)
		}
		if p.Command[2] != "/app/myapp.exe" {
			t.Fatalf("expected /app/myapp.exe in command, got %s", p.Command[2])
		}
	})

	t.Run("no proton leaves Package.Proton nil", func(t *testing.T) {
		input := InputPackage{
			Image:       InputPackageImage{URL: "nginx:latest"},
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Proton != nil {
			t.Fatal("expected nil Proton")
		}
	})
}

func TestProtonEnabledFlag(t *testing.T) {
	if !ProtonEnabled() {
		t.Fatal("ProtonEnabled() should be true with the `proton` build tag")
	}
}

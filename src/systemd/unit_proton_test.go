//go:build proton

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemd

import (
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

func TestGeneratePackageUnitsProtonCommand(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:    "test-repo",
		PkgName:     "winapp",
		Version:     "1.0",
		Image:       "ghcr.io/town-os/proton-runner:latest",
		Command:     []string{"proton", "run", "/app/myapp.exe"},
		Environment: map[string]string{"STEAM_COMPAT_DATA_PATH": "/proton-data"},
		External:    packages.PortMap{},
		Internal:    packages.PortMap{},
		Volumes: map[string]packages.PackageVolume{
			"app":        {Mountpoint: "/app"},
			"compatdata": {Mountpoint: "/proton-data"},
		},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if !strings.Contains(svc, "ghcr.io/town-os/proton-runner:latest") {
		t.Fatalf("service missing proton runner image, got:\n%s", svc)
	}
	if !strings.Contains(svc, "proton") {
		t.Fatalf("service missing proton command, got:\n%s", svc)
	}
	if !strings.Contains(svc, "run") {
		t.Fatalf("service missing run subcommand, got:\n%s", svc)
	}
	if !strings.Contains(svc, "/app/myapp.exe") {
		t.Fatalf("service missing exe path, got:\n%s", svc)
	}
	if !strings.Contains(svc, "-e STEAM_COMPAT_DATA_PATH=/proton-data") {
		t.Fatalf("service missing STEAM_COMPAT_DATA_PATH env var, got:\n%s", svc)
	}
	if !strings.Contains(svc, "/town-os/installed/test-repo/winapp/1.0/app:/app:rw,z") {
		t.Fatalf("service missing app volume mount, got:\n%s", svc)
	}
	if !strings.Contains(svc, "/town-os/installed/test-repo/winapp/1.0/compatdata:/proton-data:rw,z") {
		t.Fatalf("service missing compatdata volume mount, got:\n%s", svc)
	}

	// Proton command args should appear after the image name.
	_, afterImage, found := strings.Cut(svc, "ghcr.io/town-os/proton-runner:latest")
	if !found {
		t.Fatalf("image name not found in service, got:\n%s", svc)
	}
	if !strings.Contains(afterImage, "\n  proton") {
		t.Fatalf("proton command should appear after image name, got:\n%s", svc)
	}

	// No ports, so no sockets or network controller.
	if len(units.Sockets) != 0 {
		t.Fatalf("expected 0 socket units, got %d", len(units.Sockets))
	}
	if units.NetworkController != nil {
		t.Fatal("expected no network controller unit")
	}
}

func TestGeneratePackageUnitsProtonWithArgs(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:    "test-repo",
		PkgName:     "game",
		Version:     "2.0",
		Image:       "ghcr.io/town-os/proton-runner:latest",
		Command:     []string{"proton", "run", "/app/game.exe", "-fullscreen", "-config", "/app/settings.ini"},
		Environment: map[string]string{"STEAM_COMPAT_DATA_PATH": "/proton-data"},
		External:    packages.PortMap{},
		Internal:    packages.PortMap{},
		Volumes: map[string]packages.PackageVolume{
			"app":        {Mountpoint: "/app"},
			"compatdata": {Mountpoint: "/proton-data"},
		},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if !strings.Contains(svc, "/app/game.exe") {
		t.Fatalf("service missing exe path, got:\n%s", svc)
	}
	if !strings.Contains(svc, "-fullscreen") {
		t.Fatalf("service missing -fullscreen arg, got:\n%s", svc)
	}
	if !strings.Contains(svc, "-config") {
		t.Fatalf("service missing -config arg, got:\n%s", svc)
	}
	if !strings.Contains(svc, "/app/settings.ini") {
		t.Fatalf("service missing settings.ini arg, got:\n%s", svc)
	}
}

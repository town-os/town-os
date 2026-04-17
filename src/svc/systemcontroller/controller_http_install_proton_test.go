//go:build proton

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

func TestHTTPInstallProtonPackage(t *testing.T) {
	c, inst, sd := initProtonInstallTestClient(t)

	if err := c.InstallPackage(context.TODO(), "winapp", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify the mock installer received the Install call.
	var foundInstall bool
	for _, call := range inst.GetCalls() {
		if call.Method == "Install" {
			nameArg, ok := call.Args[1].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if nameArg == "winapp" {
				foundInstall = true
			}
		}
	}
	if !foundInstall {
		t.Fatal("expected Install call for winapp")
	}

	// Verify systemd units were created with proton runner image and command.
	calls := sd.GetCalls()
	var foundUnit bool
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			content, ok := call.Args[1].(string)
			if !ok {
				continue
			}
			if strings.Contains(content, "ghcr.io/town-os/proton-runner:latest") &&
				strings.Contains(content, "proton") &&
				strings.Contains(content, "/app/myapp.exe") {
				foundUnit = true
			}
		}
	}
	if !foundUnit {
		t.Fatal("expected systemd unit with proton runner image and command")
	}
}

func TestHTTPInstallProtonPackageCreatesSystemdUnits(t *testing.T) {
	c, _, sd := initProtonInstallTestClient(t)

	if err := c.InstallPackage(context.TODO(), "winapp", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	calls := sd.GetCalls()
	// No ports: InstallUnit(service) + Start(service) = 2
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls (no ports), got %d: %v", len(calls), calls)
	}

	if calls[0].Method != "InstallUnit" {
		t.Fatalf("expected InstallUnit, got %s", calls[0].Method)
	}
	unitName, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if unitName != "town-os-package--repo-a-winapp-1.0.service" {
		t.Fatalf("expected unit name town-os-package--repo-a-winapp-1.0.service, got %s", unitName)
	}

	if calls[1].Method != "SetStatus" || calls[1].Args[1] != systemd.Start {
		t.Fatalf("expected SetStatus Start, got %s %v", calls[1].Method, calls[1].Args)
	}
}

func TestHTTPInstallPreviewProtonPackage(t *testing.T) {
	c, _, _ := initProtonInstallTestClient(t)

	preview, err := c.InstallPreview(context.TODO(), "repo-a", "winapp", "1.0")
	if err != nil {
		t.Fatalf("InstallPreview: %v", err)
	}

	if preview.Name != "winapp" {
		t.Fatalf("expected name %q, got %q", "winapp", preview.Name)
	}
	if preview.Image != "ghcr.io/town-os/proton-runner:latest" {
		t.Fatalf("expected proton image %q, got %q", "ghcr.io/town-os/proton-runner:latest", preview.Image)
	}
	if len(preview.Volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(preview.Volumes))
	}
}

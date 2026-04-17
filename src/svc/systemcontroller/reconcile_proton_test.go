//go:build proton

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

func TestReconcileProtonPackage(t *testing.T) {
	// Proton package without explicit image URL: the reconciler should use
	// the proton_image setting. The proton app extraction will fail because
	// podman is not available in test, but the reconcile should still install
	// the systemd unit.
	pkgYAML := `image:
  type: oci
proton:
  app_image: mycompany/windows-app:1.0
  app_directory: /app
  volume: app
  exe: /app/myapp.exe
volumes:
  app:
    mountpoint: /app
  compatdata:
    mountpoint: /proton-data
environment:
  STEAM_COMPAT_DATA_PATH: /proton-data
`
	rr, inst := setupReconcileRepo(t, map[string]string{
		"winapp/1.0": pkgYAML,
	})
	sd := systemd.InitMockManager()

	// Use a mock settings manager to provide the proton_image setting.
	settings := &mockSettingsManager{
		values: map[string]string{
			"proton_image": "ghcr.io/town-os/proton-runner:latest",
		},
	}

	err := inst.Install("repo-a", "winapp", "winapp", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
		SettingsMgr:    settings,
		BtrfsBasePath:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// InstallUnit + Start = 2
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls, got %d: %v", len(calls), calls)
	}

	// Verify the unit content contains the proton image and command.
	unitContent, ok := calls[0].Args[1].(string)
	if !ok {
		t.Fatal("expected string arg for unit content")
	}
	if !strings.Contains(unitContent, "ghcr.io/town-os/proton-runner:latest") {
		t.Fatalf("expected proton image in unit content, got:\n%s", unitContent)
	}
	if !strings.Contains(unitContent, "proton") {
		t.Fatalf("expected proton command in unit content, got:\n%s", unitContent)
	}
}

func TestReconcileProtonPackageNilSettings(t *testing.T) {
	// When no settings manager is provided, the proton image should be empty.
	// The reconcile should still succeed (unit installs with empty image).
	pkgYAML := `image:
  type: oci
proton:
  app_image: mycompany/windows-app:1.0
  app_directory: /app
  volume: app
  exe: /app/myapp.exe
volumes:
  app:
    mountpoint: /app
`
	rr, inst := setupReconcileRepo(t, map[string]string{
		"winapp/1.0": pkgYAML,
	})
	sd := systemd.InitMockManager()

	err := inst.Install("repo-a", "winapp", "winapp", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	// No SettingsMgr — reconcileProtonImage returns "".
	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// InstallUnit + Start = 2
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls, got %d: %v", len(calls), calls)
	}
}

func TestReconcileProtonPackageDisabled(t *testing.T) {
	// Disabled proton package should install unit but not start it.
	pkgYAML := `image:
  type: oci
proton:
  app_image: mycompany/windows-app:1.0
  app_directory: /app
  volume: app
  exe: /app/myapp.exe
volumes:
  app:
    mountpoint: /app
`
	rr, inst := setupReconcileRepo(t, map[string]string{
		"winapp/1.0": pkgYAML,
	})
	sd := systemd.InitMockManager()

	settings := &mockSettingsManager{
		values: map[string]string{
			"proton_image": "ghcr.io/town-os/proton-runner:latest",
		},
	}

	err := inst.Install("repo-a", "winapp", "winapp", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err = inst.SetDisabled("repo-a", "winapp", true)
	if err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
		SettingsMgr:    settings,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// Only InstallUnit, no Start.
	if len(calls) != 1 {
		t.Fatalf("expected 1 systemd call (InstallUnit only), got %d: %v", len(calls), calls)
	}
	if calls[0].Method != "InstallUnit" {
		t.Fatalf("expected InstallUnit, got %s", calls[0].Method)
	}
}

func TestReconcileProtonPackageWithStorage(t *testing.T) {
	// Proton package with storage should create volumes and attempt app extraction.
	pkgYAML := `image:
  type: oci
proton:
  app_image: mycompany/windows-app:1.0
  app_directory: /app
  volume: app
  exe: /app/myapp.exe
volumes:
  app:
    mountpoint: /app
  compatdata:
    mountpoint: /proton-data
`
	rr, inst := setupReconcileRepo(t, map[string]string{
		"winapp/1.0": pkgYAML,
	})
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()
	controller, ok := mock.Controller.(*storage.MockBtrFSController)
	if !ok {
		t.Fatal("expected *storage.MockBtrFSController")
	}

	settings := &mockSettingsManager{
		values: map[string]string{
			"proton_image": "ghcr.io/town-os/proton-runner:latest",
		},
	}

	err := inst.Install("repo-a", "winapp", "winapp", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		SettingsMgr:    settings,
		BtrfsBasePath:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify volumes were created: 4 root + intermediate dirs + 2 package volumes.
	fs := controller.GetFilesystems()
	fsNames := map[string]bool{}
	for _, f := range fs {
		fsNames[f.Name] = true
	}
	if !fsNames["installed/repo-a/winapp/1.0/app"] {
		t.Fatal("expected app volume")
	}
	if !fsNames["installed/repo-a/winapp/1.0/compatdata"] {
		t.Fatal("expected compatdata volume")
	}

	// Systemd should have InstallUnit + Start.
	calls := sd.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls, got %d: %v", len(calls), calls)
	}
}

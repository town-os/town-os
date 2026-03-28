// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// initVMInstallTest creates a test server with a local file-based repo
// containing VM packages for testing VM install flows. It creates a proper
// bare git repo so AddRepository succeeds.
func initVMInstallTest(t *testing.T) (*systemcontroller.SystemdClient, *systemd.MockManager) {
	t.Helper()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		BtrfsBasePath:  btrfsBase,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	// Create a bare git repo containing VM packages.
	localBareRepo := filepath.Join(t.TempDir(), "vm-repo.git")
	localWork := filepath.Join(t.TempDir(), "vm-work")
	for _, args := range [][]string{
		{"init", "--bare", localBareRepo},
		{"clone", localBareRepo, localWork},
	} {
		cmd := exec.CommandContext(context.TODO(), "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	// Create debian-vm package.
	debianVMDir := filepath.Join(localWork, packages.PackagesDir, "debian-vm")
	if err := os.MkdirAll(debianVMDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	vmPkgYAML := `vm:
  image: debian.raw
  memory: 2gb
  cpus: 2
network:
  external:
    8022: 22
  internal: {}
`
	if err := os.WriteFile(filepath.Join(debianVMDir, "1.0.yaml"), []byte(vmPkgYAML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create headless-vm package.
	headlessDir := filepath.Join(localWork, packages.PackagesDir, "headless-vm")
	if err := os.MkdirAll(headlessDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	headlessPkgYAML := `vm:
  image: headless.raw
  memory: 1gb
  cpus: 1
`
	if err := os.WriteFile(filepath.Join(headlessDir, "1.0.yaml"), []byte(headlessPkgYAML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	for _, args := range [][]string{
		{"-C", localWork, "add", "."},
		{"-C", localWork, "-c", "user.name=test", "-c", "user.email=test@test", "-c", "commit.gpgsign=false", "commit", "-m", "init"},
		{"-C", localWork, "push", "origin", "HEAD:main"},
	} {
		cmd := exec.CommandContext(context.TODO(), "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	// Ensure bare repo HEAD points to main.
	cmd := exec.CommandContext(context.TODO(), "git", "-C", localBareRepo, "symbolic-ref", "HEAD", "refs/heads/main")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git symbolic-ref HEAD: %v: %s", err, out)
	}

	if err := c.AddRepository(context.TODO(), "vm-repo", "file://"+localBareRepo, "", ""); err != nil {
		t.Fatalf("AddRepository: %v", err)
	}

	return c, sd
}

func TestVMInstallCreatesQEMUUnit(t *testing.T) {
	t.Parallel()
	c, sd := initVMInstallTest(t)

	if err := c.InstallPackage(context.TODO(), "debian-vm", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage debian-vm@1.0: %v", err)
	}

	calls := sd.GetCalls()
	// VM with 1 external port:
	//   3 InstallUnit (service, socket, networkcontroller) +
	//   2 Enable (socket, networkcontroller) + 1 Start (service) = 6
	if len(calls) != 6 {
		t.Fatalf("expected 6 systemd calls, got %d", len(calls))
	}

	// First InstallUnit should be the service.
	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	unitName, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed for unit name")
	}
	if unitName != "town-os-package--vm-repo-debian-vm-1.0.service" {
		t.Fatalf("expected unit name town-os-package--vm-repo-debian-vm-1.0.service, got %s", unitName)
	}

	// Verify the service content uses qemu, not podman.
	svcContent, ok := calls[0].Args[1].(string)
	if !ok {
		t.Fatal("type assertion failed for unit content")
	}
	if !strings.Contains(svcContent, "qemu-system-x86_64") {
		t.Fatal("VM service should reference qemu-system-x86_64")
	}
	if strings.Contains(svcContent, "podman") {
		t.Fatal("VM service should not reference podman")
	}
	if !strings.Contains(svcContent, "-m 2048") {
		t.Fatalf("VM service missing -m 2048")
	}
	if !strings.Contains(svcContent, "-smp 2") {
		t.Fatalf("VM service missing -smp 2")
	}
	if !strings.Contains(svcContent, "hostfwd=tcp::8022-:22") {
		t.Fatalf("VM service missing port forwarding 8022:22")
	}

	// Last call should be Start.
	lastCall := calls[len(calls)-1]
	if lastCall.Method != "SetStatus" {
		t.Fatalf("last call: expected SetStatus, got %q", lastCall.Method)
	}
	lastAction, ok := lastCall.Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed for status action")
	}
	if lastAction != systemd.Start {
		t.Fatalf("last call: expected Start, got %v", lastCall.Args[1])
	}
}

func TestVMInstallNoPortsMinimalUnits(t *testing.T) {
	t.Parallel()
	c, sd := initVMInstallTest(t)

	if err := c.InstallPackage(context.TODO(), "headless-vm", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage headless-vm@1.0: %v", err)
	}

	calls := sd.GetCalls()
	// VM with no ports: InstallUnit(service) + Start(service) = 2
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls, got %d", len(calls))
	}

	svcContent, ok := calls[0].Args[1].(string)
	if !ok {
		t.Fatal("type assertion failed for unit content")
	}
	if !strings.Contains(svcContent, "qemu-system-x86_64") {
		t.Fatal("VM service should reference qemu-system-x86_64")
	}
	if !strings.Contains(svcContent, "-m 1024") {
		t.Fatalf("VM service missing -m 1024")
	}
	if !strings.Contains(svcContent, "-smp 1") {
		t.Fatalf("VM service missing -smp 1")
	}
}

func TestVMInstallPreview(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}

	// Add repo directly so we don't need git.
	repoName := "vm-repo"
	repoURL, _ := url.Parse("https://example.com/vm-repo.git")
	if err := rr.Add(packages.Repository{Name: repoName, URL: *repoURL}); err != nil {
		t.Fatalf("Add repo: %v", err)
	}

	// Create a VM package file directly in the repo.
	pkgDir := filepath.Join(dir, repoName, packages.PackagesDir, "debian-vm")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	vmPkgYAML := `vm:
  image: debian.raw
  memory: 2gb
  cpus: 2
network:
  external:
    8022: 22
  internal: {}
`
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(vmPkgYAML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		BtrfsBasePath:  t.TempDir(),
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	preview, err := c.InstallPreview(context.TODO(), repoName, "debian-vm", "1.0")
	if err != nil {
		t.Fatalf("InstallPreview: %v", err)
	}

	if preview.Runtime != string(packages.RuntimeVM) {
		t.Fatalf("expected runtime %q, got %q", packages.RuntimeVM, preview.Runtime)
	}
	if preview.VM == nil {
		t.Fatal("expected VM info in preview")
	}
	if preview.VM.Image != "debian.raw" {
		t.Fatalf("expected VM image %q, got %q", "debian.raw", preview.VM.Image)
	}
	if preview.VM.Memory != "2gb" {
		t.Fatalf("expected VM memory %q, got %q", "2gb", preview.VM.Memory)
	}
	if preview.VM.CPUs != 2 {
		t.Fatalf("expected VM CPUs 2, got %d", preview.VM.CPUs)
	}
}

func TestVMInstallAndUninstall(t *testing.T) {
	t.Parallel()
	c, sd := initVMInstallTest(t)

	// Install.
	if err := c.InstallPackage(context.TODO(), "debian-vm", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after install: %v", err)
	}
	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(pkgs.Entries))
	}
	if pkgs.Entries[0] != "vm-repo/debian-vm@1.0" {
		t.Fatalf("expected vm-repo/debian-vm@1.0, got %s", pkgs.Entries[0])
	}

	// Uninstall.
	if err := c.UninstallPackage(context.TODO(), "vm-repo", "debian-vm", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	pkgs, err = c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after uninstall: %v", err)
	}
	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 installed after uninstall, got %d", len(pkgs.Entries))
	}

	// Install: 6 calls (3 InstallUnit + 2 Enable + 1 Start)
	// Uninstall: ListPackageUnitFiles + 3*(Stop+Disable+Uninstall) = 10
	// Total: 16
	calls := sd.GetCalls()
	if len(calls) != 16 {
		t.Fatalf("expected 16 systemd calls total, got %d", len(calls))
	}
}

func TestVMListImages(t *testing.T) {
	t.Parallel()
	btrfsBase := t.TempDir()
	mock := storage.InitBtrFSMock()

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       mock,
		BtrfsBasePath: btrfsBase,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	// Initially empty.
	images, err := c.ListVMImages(context.TODO())
	if err != nil {
		t.Fatalf("ListVMImages: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("expected 0 images, got %d", len(images))
	}

	// Create a fake image file in the vm-images directory.
	vmDir := filepath.Join(btrfsBase, "vm-images")
	if err := os.MkdirAll(vmDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vmDir, "test.raw"), []byte("fake-image-data"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Should now see the image.
	images, err = c.ListVMImages(context.TODO())
	if err != nil {
		t.Fatalf("ListVMImages: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].Name != "test.raw" {
		t.Fatalf("expected image name %q, got %q", "test.raw", images[0].Name)
	}
	if images[0].Size != 15 {
		t.Fatalf("expected image size 15, got %d", images[0].Size)
	}
}

func TestVMDeleteImage(t *testing.T) {
	t.Parallel()
	btrfsBase := t.TempDir()
	mock := storage.InitBtrFSMock()

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       mock,
		BtrfsBasePath: btrfsBase,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	// Create a fake image file.
	vmDir := filepath.Join(btrfsBase, "vm-images")
	if err := os.MkdirAll(vmDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	imgPath := filepath.Join(vmDir, "delete-me.raw")
	if err := os.WriteFile(imgPath, []byte("data"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Delete the image.
	if err := c.DeleteVMImage(context.TODO(), "delete-me.raw"); err != nil {
		t.Fatalf("DeleteVMImage: %v", err)
	}

	// Verify it was removed.
	if _, err := os.Stat(imgPath); !os.IsNotExist(err) {
		t.Fatal("expected image file to be deleted")
	}

	// Should be empty now.
	images, err := c.ListVMImages(context.TODO())
	if err != nil {
		t.Fatalf("ListVMImages: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("expected 0 images after delete, got %d", len(images))
	}
}

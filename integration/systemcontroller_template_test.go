// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// initTemplateInstallTest creates a test server with a local package containing
// templates. Returns the client, install manager, btrfs base path, and
// repository root.
func initTemplateInstallTest(t *testing.T) (
	*systemcontroller.SystemdClient,
	packages.Installer,
	string,
	*packages.RepositoryRoot,
) {
	t.Helper()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal empty repository list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories file: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("failed to load repository root: %v", err)
	}

	// Add a local repository entry.
	u, err := url.Parse("file://" + dir)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{{Name: "local", URL: *u}}

	// Write a package with templates.
	pkgYAML := `image: nginx:1.0
environment:
  NGINX_HOST: "@hostname@"
network:
  external: {}
  internal: {}
volumes:
  config:
    mountpoint: /etc/nginx
  data:
    mountpoint: /var/data
questions:
  hostname:
    query: "What hostname?"
    type: hostname
description: "Test web server"
templates:
  nginx_conf:
    volume: config
    path: nginx.conf
    content: "server_name {{.Responses.hostname}};"
  readme:
    volume: data
    path: info.txt
    content: "{{.Package.Name}} v{{.Package.Version}}"
  sysinfo:
    volume: data
    path: system.txt
    content: "host={{.System.Hostname}} desc={{.Package.Description}}"
`
	pkgDir := filepath.Join(dir, "local", packages.PackagesDir, "nginx")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	btrfsBase := t.TempDir()
	sd := systemd.InitMockManager()
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
		t.Fatalf("could not create client: %v", err)
	}

	return c, inst, btrfsBase, rr
}

func TestIntegrationInstallWithTemplatesWritesFiles(t *testing.T) {
	t.Parallel()
	c, _, btrfsBase, _ := initTemplateInstallTest(t)

	responses := packages.Responses{"hostname": "example"}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify template files were written to the volume directories.
	nginxConf := filepath.Join(btrfsBase, systemcontroller.PackagesVolumePrefix, "local", "nginx", "1.0", "config", "nginx.conf")
	content, err := os.ReadFile(nginxConf)
	if err != nil {
		t.Fatalf("expected nginx.conf to be written: %v", err)
	}
	if string(content) != "server_name example;" {
		t.Fatalf("expected 'server_name example;', got %q", string(content))
	}

	readme := filepath.Join(btrfsBase, systemcontroller.PackagesVolumePrefix, "local", "nginx", "1.0", "data", "info.txt")
	content, err = os.ReadFile(readme)
	if err != nil {
		t.Fatalf("expected info.txt to be written: %v", err)
	}
	if string(content) != "nginx v1.0" {
		t.Fatalf("expected 'nginx v1.0', got %q", string(content))
	}

	// Verify system info and description are available in templates.
	sysinfo := filepath.Join(btrfsBase, systemcontroller.PackagesVolumePrefix, "local", "nginx", "1.0", "data", "system.txt")
	content, err = os.ReadFile(sysinfo)
	if err != nil {
		t.Fatalf("expected system.txt to be written: %v", err)
	}
	expectedHostname, _ := os.Hostname()
	expectedSysinfo := "host=" + expectedHostname + " desc=Test web server"
	if string(content) != expectedSysinfo {
		t.Fatalf("expected %q, got %q", expectedSysinfo, string(content))
	}
}

func TestIntegrationInstallTemplatesDoNotOverwrite(t *testing.T) {
	t.Parallel()
	c, _, btrfsBase, _ := initTemplateInstallTest(t)

	// Pre-create the target file with existing content.
	configDir := filepath.Join(btrfsBase, systemcontroller.PackagesVolumePrefix, "local", "nginx", "1.0", "config")
	if err := os.MkdirAll(configDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	existing := "user-modified config"
	if err := os.WriteFile(filepath.Join(configDir, "nginx.conf"), []byte(existing), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	responses := packages.Responses{"hostname": "example"}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Existing file should not be overwritten.
	content, err := os.ReadFile(filepath.Join(configDir, "nginx.conf"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != existing {
		t.Fatalf("expected existing content preserved, got %q", string(content))
	}

	// The other template (no pre-existing file) should still be written.
	readme := filepath.Join(btrfsBase, systemcontroller.PackagesVolumePrefix, "local", "nginx", "1.0", "data", "info.txt")
	content, err = os.ReadFile(readme)
	if err != nil {
		t.Fatalf("expected info.txt: %v", err)
	}
	if string(content) != "nginx v1.0" {
		t.Fatalf("expected 'nginx v1.0', got %q", string(content))
	}
}

func TestIntegrationReconcileWithTemplatesWritesFiles(t *testing.T) {
	t.Parallel()
	c, inst, btrfsBase, rr := initTemplateInstallTest(t)

	responses := packages.Responses{"hostname": "reconcile-host"}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Remove template files to simulate a volume rebuild.
	configDir := filepath.Join(btrfsBase, systemcontroller.PackagesVolumePrefix, "local", "nginx", "1.0", "config")
	if err := os.Remove(filepath.Join(configDir, "nginx.conf")); err != nil {
		t.Fatalf("remove nginx.conf: %v", err)
	}
	dataDir := filepath.Join(btrfsBase, systemcontroller.PackagesVolumePrefix, "local", "nginx", "1.0", "data")
	if err := os.Remove(filepath.Join(dataDir, "info.txt")); err != nil {
		t.Fatalf("remove info.txt: %v", err)
	}
	if err := os.Remove(filepath.Join(dataDir, "system.txt")); err != nil {
		t.Fatalf("remove system.txt: %v", err)
	}

	// Run reconciliation.
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		BtrfsBasePath:  btrfsBase,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify template files were re-written during reconciliation.
	content, err := os.ReadFile(filepath.Join(configDir, "nginx.conf"))
	if err != nil {
		t.Fatalf("expected nginx.conf after reconcile: %v", err)
	}
	if string(content) != "server_name reconcile-host;" {
		t.Fatalf("expected 'server_name reconcile-host;', got %q", string(content))
	}

	content, err = os.ReadFile(filepath.Join(dataDir, "info.txt"))
	if err != nil {
		t.Fatalf("expected info.txt after reconcile: %v", err)
	}
	if string(content) != "nginx v1.0" {
		t.Fatalf("expected 'nginx v1.0', got %q", string(content))
	}
}

func TestIntegrationReconcileTemplatesPreserveExistingFiles(t *testing.T) {
	t.Parallel()
	c, inst, btrfsBase, rr := initTemplateInstallTest(t)

	responses := packages.Responses{"hostname": "myhost"}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Template files already exist from the install. Reconcile should not
	// overwrite them.
	configDir := filepath.Join(btrfsBase, systemcontroller.PackagesVolumePrefix, "local", "nginx", "1.0", "config")
	existingContent, err := os.ReadFile(filepath.Join(configDir, "nginx.conf"))
	if err != nil {
		t.Fatalf("ReadFile before reconcile: %v", err)
	}

	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	err = systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		BtrfsBasePath:  btrfsBase,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify the existing file was not overwritten.
	content, err := os.ReadFile(filepath.Join(configDir, "nginx.conf"))
	if err != nil {
		t.Fatalf("ReadFile after reconcile: %v", err)
	}
	if string(content) != string(existingContent) {
		t.Fatalf("expected content unchanged after reconcile, got %q", string(content))
	}
}

func TestIntegrationInstallWithoutTemplatesNoFiles(t *testing.T) {
	t.Parallel()
	// Package without templates should install normally.
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

	u, err := url.Parse("file://" + dir)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{{Name: "local", URL: *u}}

	pkgYAML := `image: nginx:1.0
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/data
questions: {}
`
	pkgDir := filepath.Join(dir, "local", packages.PackagesDir, "nginx")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	btrfsBase := t.TempDir()
	sd := systemd.InitMockManager()
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
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify no template files exist in the volume dir.
	dataDir := filepath.Join(btrfsBase, systemcontroller.PackagesVolumePrefix, "local", "nginx", "1.0", "data")
	entries, err := os.ReadDir(dataDir)
	if err == nil && len(entries) > 0 {
		t.Fatalf("expected no template files, got %d entries", len(entries))
	}
}

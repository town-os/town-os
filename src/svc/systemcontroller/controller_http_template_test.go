// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
)

func TestHTTPInstallPackageWithTemplates(t *testing.T) {
	c, _, btrfsBase := initInstallWithTemplatesTestClient(t)

	responses := packages.Responses{"hostname": "example"}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify template files were written.
	nginxConf := filepath.Join(btrfsBase, PackagesVolumePrefix, "repo-a", "nginx", "1.0", "config", "nginx.conf")
	content, err := os.ReadFile(nginxConf)
	if err != nil {
		t.Fatalf("expected nginx.conf to be written: %v", err)
	}
	if string(content) != "server_name example;" {
		t.Fatalf("expected 'server_name example.com;', got %q", string(content))
	}

	readme := filepath.Join(btrfsBase, PackagesVolumePrefix, "repo-a", "nginx", "1.0", "data", "info.txt")
	content, err = os.ReadFile(readme)
	if err != nil {
		t.Fatalf("expected info.txt to be written: %v", err)
	}
	if string(content) != "nginx v1.0" {
		t.Fatalf("expected 'nginx v1.0', got %q", string(content))
	}
}

func TestHTTPInstallPackageTemplatesDoNotOverwrite(t *testing.T) {
	c, _, btrfsBase := initInstallWithTemplatesTestClient(t)

	// Pre-create the target file with existing content.
	configDir := filepath.Join(btrfsBase, PackagesVolumePrefix, "repo-a", "nginx", "1.0", "config")
	if err := os.MkdirAll(configDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	existing := "existing nginx config"
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
	readme := filepath.Join(btrfsBase, PackagesVolumePrefix, "repo-a", "nginx", "1.0", "data", "info.txt")
	content, err = os.ReadFile(readme)
	if err != nil {
		t.Fatalf("expected info.txt to be written: %v", err)
	}
	if string(content) != "nginx v1.0" {
		t.Fatalf("expected 'nginx v1.0', got %q", string(content))
	}
}

func TestHTTPInstallPackageWithoutTemplatesNoFiles(t *testing.T) {
	// Packages without templates should install normally with no template files.
	c, _ := initInstallWithVolumesTestClient(t)

	responses := packages.Responses{"hostname": "example"}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}
}

func TestHTTPInstallPackageTemplateNestedPath(t *testing.T) {
	// Template with a nested path (subdirectories) should create parent dirs.
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}
	pkgYAML := `image: nginx:1.0
network:
  external: {}
  internal: {}
volumes:
  config:
    mountpoint: /etc/nginx
questions: {}
templates:
  deep:
    volume: config
    path: etc/app/config.yaml
    content: "value: hello"
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", pkgYAML)

	btrfsBase := t.TempDir()
	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst, BtrfsBasePath: btrfsBase})
	t.Cleanup(ts.Close)

	cl, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := cl.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	deepFile := filepath.Join(btrfsBase, PackagesVolumePrefix, "repo-a", "nginx", "1.0", "config", "etc", "app", "config.yaml")
	content, err := os.ReadFile(deepFile)
	if err != nil {
		t.Fatalf("expected nested template file: %v", err)
	}
	if string(content) != "value: hello" {
		t.Fatalf("expected 'value: hello', got %q", string(content))
	}
}

func TestHTTPInstallPackageTemplateUsesResponseSubstitution(t *testing.T) {
	// Template volume and path fields go through @foo@ substitution.
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}
	// Template path uses @filename@ substitution.
	pkgYAML := `image: nginx:1.0
network:
  external: {}
  internal: {}
volumes:
  config:
    mountpoint: /etc/nginx
questions:
  filename:
    query: "Config file name?"
templates:
  conf:
    volume: config
    path: "@filename@"
    content: "generated config"
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", pkgYAML)

	btrfsBase := t.TempDir()
	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst, BtrfsBasePath: btrfsBase})
	t.Cleanup(ts.Close)

	cl, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	responses := packages.Responses{"filename": "custom.conf"}
	if err := cl.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	customConf := filepath.Join(btrfsBase, PackagesVolumePrefix, "repo-a", "nginx", "1.0", "config", "custom.conf")
	content, err := os.ReadFile(customConf)
	if err != nil {
		t.Fatalf("expected custom.conf to be written: %v", err)
	}
	if string(content) != "generated config" {
		t.Fatalf("expected 'generated config', got %q", string(content))
	}
}

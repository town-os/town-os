// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// TestIntegrationDepTemplateDataRendered installs a parent whose file
// template references its dependency's container name and a named port
// via .Dep.KEY.Host and `index .Dep.KEY.Ports "sql"`, and asserts both
// landed in the rendered file on disk.
//
// This is the end-to-end check that ordering in controller_install.go is
// correct (deps must install before the parent's templates render) AND
// that the systemcontroller wires its depMap into TemplateData.Dep. A
// regression in either place would surface as "{{.Dep.db.Host}}" etc.
// left literal in the rendered file, or as execution errors.
func TestIntegrationDepTemplateDataRendered(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories: %v", err)
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

	// Dep: exposes container port 5432 named "sql".
	depYAML := `image: alpine:3.20
description: "test db"
network:
  internal:
    sql: "5432"
volumes: {}
questions: {}
`
	depDir := filepath.Join(dir, "local", packages.PackagesDir, "tpl-db")
	if err := os.MkdirAll(depDir, 0750); err != nil {
		t.Fatalf("MkdirAll dep: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "1.0.yaml"), []byte(depYAML), 0600); err != nil {
		t.Fatalf("WriteFile dep: %v", err)
	}

	// Parent: template content exercises every access pattern we want
	// packagers to be able to rely on: direct dep Host access, `index`
	// on Ports by semantic name, and `index` on Ports by numeric form.
	parentYAML := `image: alpine:3.20
description: "test parent"
network:
  external:
    "@port@": "80"
  internal: {}
volumes:
  data:
    mountpoint: /data
questions:
  port:
    query: "External port?"
    type: port
dependencies:
  db:
    package: tpl-db
    version: "1.0"
templates:
  cfg:
    volume: data
    path: app.yaml
    content: |
      host: {{.Dep.db.Host}}
      port_named: {{index .Dep.db.Ports "sql"}}
      port_numeric: {{index .Dep.db.Ports "5432"}}
`
	parentDir := filepath.Join(dir, "local", packages.PackagesDir, "tpl-parent")
	if err := os.MkdirAll(parentDir, 0750); err != nil {
		t.Fatalf("MkdirAll parent: %v", err)
	}
	if err := os.WriteFile(filepath.Join(parentDir, "1.0.yaml"), []byte(parentYAML), 0600); err != nil {
		t.Fatalf("WriteFile parent: %v", err)
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

	if err := c.InstallPackage(context.TODO(), "tpl-parent", "1.0", packages.Responses{
		"port": "9091",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Dep container name follows systemd.ContainerName. For a dep of
	// parent "tpl-parent" keyed "db", the effective name is
	// "tpl-parent--dep--db" — the same string the systemcontroller
	// uses for the podman container and that packages on the shared
	// network resolve via podman DNS.
	wantHost := systemd.ContainerName("local", "tpl-parent--dep--db", "1.0")

	rendered := filepath.Join(btrfsBase, "installed", "local", "tpl-parent", "1.0", "data", "app.yaml")
	content, err := os.ReadFile(rendered)
	if err != nil {
		t.Fatalf("ReadFile rendered template %q: %v", rendered, err)
	}
	body := string(content)

	if !strings.Contains(body, "host: "+wantHost) {
		t.Fatalf("rendered template missing dep host %q; content:\n%s", wantHost, body)
	}
	if !strings.Contains(body, "port_named: 5432") {
		t.Fatalf("rendered template missing named-port resolution; content:\n%s", body)
	}
	if !strings.Contains(body, "port_numeric: 5432") {
		t.Fatalf("rendered template missing numeric-port resolution; content:\n%s", body)
	}
	// Any literal "{{" in the rendered file would mean the template
	// engine skipped execution, or an unclosed action leaked through.
	if strings.Contains(body, "{{") || strings.Contains(body, "<no value>") {
		t.Fatalf("rendered template has unresolved template markers:\n%s", body)
	}
}

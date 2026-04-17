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

// initNamedPortsTest provisions a temporary local repository with a
// parent package and a dependency. The dependency declares a semantic
// port name "sql" next to its numeric port 5432. The parent references
// the dep's port using both numeric (`@dep_db_port_5432@`) and named
// (`@dep_db_port_sql@`) forms in its environment, so a single install
// exercises both code paths end-to-end.
func initNamedPortsTest(t *testing.T) (*systemcontroller.SystemdClient, *systemd.MockManager) {
	t.Helper()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal empty repo list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories file: %v", err)
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

	// Dep: declares container port 5432 with the semantic name "sql".
	// A second entry with a numeric key "6379" verifies that named and
	// numeric forms coexist in the same map without interfering.
	depYAML := `image: alpine:3.20
description: "test db"
environment:
  DB_PORT: "@dbport@"
network:
  internal:
    sql: "5432"
    "6379": "6379"
volumes: {}
questions:
  dbport:
    query: "Port?"
    type: port
    default: "5432"
`
	depDir := filepath.Join(dir, "local", packages.PackagesDir, "named-db")
	if err := os.MkdirAll(depDir, 0750); err != nil {
		t.Fatalf("MkdirAll dep: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "1.0.yaml"), []byte(depYAML), 0600); err != nil {
		t.Fatalf("WriteFile dep: %v", err)
	}

	// Parent: references the dep's port by BOTH numeric and named form.
	parentYAML := `image: alpine:3.20
description: "test parent"
environment:
  DB_HOST: "@dep_db_host@"
  DB_PORT_NUM: "@dep_db_port_5432@"
  DB_PORT_NAMED: "@dep_db_port_sql@"
  CACHE_PORT: "@dep_db_port_6379@"
network:
  external:
    "@port@": "80"
  internal: {}
volumes: {}
questions:
  port:
    query: "External port?"
    type: port
dependencies:
  db:
    package: named-db
    version: "1.0"
`
	parentDir := filepath.Join(dir, "local", packages.PackagesDir, "named-parent")
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
	return c, sd
}

// TestIntegrationNamedPortsResolveInParentUnit installs a parent whose
// environment references a dependency's port via both numeric and named
// templates, and asserts the generated service unit contains both
// TOWNOS_DEP_DB_PORT_5432 and TOWNOS_DEP_DB_PORT_SQL env vars and that
// every template reference in the parent resolved to a concrete value
// (no unresolved `@dep_*@` markers remain).
func TestIntegrationNamedPortsResolveInParentUnit(t *testing.T) {
	t.Parallel()
	c, sd := initNamedPortsTest(t)

	if err := c.InstallPackage(context.TODO(), "named-parent", "1.0", packages.Responses{
		"port": "9090",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	parentUnitName := systemd.UnitName("local", "named-parent", "1.0")
	parentContent := findInstalledUnitContent(t, sd, parentUnitName)
	if parentContent == "" {
		t.Fatalf("expected parent unit %s to be installed", parentUnitName)
	}

	// Both numeric and named env vars must be emitted for the named
	// port. The numeric form is retained for back-compat.
	if !strings.Contains(parentContent, "TOWNOS_DEP_DB_PORT_5432=5432") {
		t.Fatalf("missing numeric dep port env var; unit:\n%s", parentContent)
	}
	if !strings.Contains(parentContent, "TOWNOS_DEP_DB_PORT_SQL=5432") {
		t.Fatalf("missing named dep port env var TOWNOS_DEP_DB_PORT_SQL=5432; unit:\n%s", parentContent)
	}
	// The unnamed entry (numeric key 6379) emits the numeric form only.
	if !strings.Contains(parentContent, "TOWNOS_DEP_DB_PORT_6379=6379") {
		t.Fatalf("missing unnamed-port env var TOWNOS_DEP_DB_PORT_6379; unit:\n%s", parentContent)
	}

	// The parent's environment values that reference the dep must have
	// been resolved at unit-generation time. DB_PORT_NAMED references
	// @dep_db_port_sql@ which only resolves when named-port support is
	// wired into both compile and runtime env emission; a regression in
	// the regex or env emission would surface here.
	if !strings.Contains(parentContent, "-e DB_PORT_NAMED=5432") {
		t.Fatalf("DB_PORT_NAMED should have been resolved to 5432; unit:\n%s", parentContent)
	}
	if !strings.Contains(parentContent, "-e DB_PORT_NUM=5432") {
		t.Fatalf("DB_PORT_NUM should have been resolved to 5432; unit:\n%s", parentContent)
	}
	if !strings.Contains(parentContent, "-e CACHE_PORT=6379") {
		t.Fatalf("CACHE_PORT should have been resolved to 6379; unit:\n%s", parentContent)
	}

	// Any leftover "@dep_" in the generated unit indicates that a
	// template reference failed to resolve.
	if strings.Contains(parentContent, "@dep_") {
		t.Fatalf("unresolved @dep_*@ marker in parent unit:\n%s", parentContent)
	}
}

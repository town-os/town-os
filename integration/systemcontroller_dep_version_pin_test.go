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

// A dependency declared without `version:` is resolved by
// RepositoryRoot.LatestPackage, which scans EVERY configured repository and
// takes the highest version string it finds anywhere. A package name that
// exists in more than one repo therefore resolves to whichever repo happens to
// carry the higher version -- not to the parent's own repo -- and the parent's
// `responses:` block is then compiled against a package whose questions it was
// never written for.
//
// This is not hypothetical: mattermost declares `db: {package: postgres}` with
// responses for default/postgres@1.0 (password/user/database/port), and on a
// box that also carries the extras repo the dep resolves to
// extras/postgres@16.0 (port/user/password/dbname). Install dies with
// `dependency "db": compile: 2 response validation error(s)` -- `database` is
// an unknown response key and `dbname` is missing -- and the two-error count is
// all the operator ever sees.
//
// The fixture below reproduces that shape with two local repositories:
// "alpha" carries the parent and shared-db@1.0; "beta" carries an unrelated
// shared-db@16.0 with an incompatible question set.
const (
	depPinParentRepo  = "alpha"
	depPinForeignRepo = "beta"
)

// depPinLocalDB is the dep the parents are written against. It lives in the
// same repo as the parents and answers to `database`.
const depPinLocalDB = `image: alpine:3.20
description: "shared-db, alpha edition"
environment:
  DB_USER: "@user@"
  DB_PASSWORD: "@password@"
  DB_NAME: "@database@"
network:
  internal:
    sql: "@port@"
volumes: {}
questions:
  password:
    query: "Password?"
    type: secret
    default: auto
  user:
    query: "User?"
  database:
    query: "Database?"
  port:
    query: "Port?"
    type: port
    default: "5432"
`

// depPinForeignDB is an unrelated package that happens to share the name and
// carries a higher version. It calls the database question `dbname`, so the
// parents' responses do not fit it: `database` is unknown and `dbname` is
// missing (no type and no default, so nothing auto-generates it).
const depPinForeignDB = `image: alpine:3.20
description: "shared-db, beta edition"
environment:
  DB_USER: "@user@"
  DB_PASSWORD: "@password@"
  DB_NAME: "@dbname@"
network:
  internal:
    sql: "5432"
volumes: {}
questions:
  port:
    query: "Port?"
    type: port
  user:
    query: "User?"
  password:
    query: "Password?"
  dbname:
    query: "Database name?"
`

// depPinParentYAML builds a parent whose dep differs only in how (or whether)
// it is pinned. Everything else is held identical so a failure is attributable
// to the pin alone.
func depPinParentYAML(depPin string) string {
	return `image: alpine:3.20
description: "parent"
environment:
  DB_HOST: "@dep_db_host@"
  DB_PORT: "@dep_db_port_sql@"
network:
  external:
    "@port@": "80"
  internal: {}
volumes: {}
questions:
  port:
    query: "External port?"
    type: port
  dbpass:
    query: "DB password?"
    type: secret
    default: auto
dependencies:
  db:
    package: shared-db
` + depPin + `    responses:
      password: "@dbpass@"
      user: "app"
      database: "appdb"
      port: "5432"
`
}

// writeDepPinPackage writes one package YAML into a repo checkout inside the
// repository root's base directory.
func writeDepPinPackage(t *testing.T, base, repo, name, version, body string) {
	t.Helper()

	dir := filepath.Join(base, repo, packages.PackagesDir, name)
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatalf("MkdirAll %s/%s: %v", repo, name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, version+".yaml"), []byte(body), 0600); err != nil {
		t.Fatalf("WriteFile %s/%s@%s: %v", repo, name, version, err)
	}
}

// initDepVersionPinTest provisions two local repositories carrying the same
// package name at different versions, plus three parents in the first repo
// that pin their dependency three different ways.
func initDepVersionPinTest(t *testing.T) (*systemcontroller.SystemdClient, *systemd.MockManager, *packages.RepositoryRoot) {
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
	rr.Items = []packages.Repository{
		{Name: depPinParentRepo, URL: *u},
		{Name: depPinForeignRepo, URL: *u},
	}

	writeDepPinPackage(t, dir, depPinParentRepo, "shared-db", "1.0", depPinLocalDB)
	writeDepPinPackage(t, dir, depPinForeignRepo, "shared-db", "16.0", depPinForeignDB)

	// Pinned on version only -- the correct form. `repo:` is deliberately
	// left unset: an unset repo means "the parent's own repo", which is
	// right whatever the operator named that repository, whereas naming a
	// repo in package YAML breaks on any box that added it under a
	// different name.
	writeDepPinPackage(t, dir, depPinParentRepo, "pinned-parent", "1.0",
		depPinParentYAML("    version: \"1.0\"\n"))

	// Pinned on nothing -- the mattermost shape.
	writeDepPinPackage(t, dir, depPinParentRepo, "unpinned-parent", "1.0",
		depPinParentYAML(""))

	// Pinned on repo only. This does NOT help: the version is still
	// resolved globally, so the resolver asks the named repo for a version
	// only the other repo has.
	writeDepPinPackage(t, dir, depPinParentRepo, "repo-pinned-parent", "1.0",
		depPinParentYAML("    repo: "+depPinParentRepo+"\n"))

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		BtrfsBasePath:  t.TempDir(),
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return c, sd, rr
}

// TestIntegrationDepVersionResolvesAcrossAllRepositories states the root cause
// on its own: an unpinned dependency version is resolved by scanning every
// configured repository, so the parent's repo has no say in which package it
// gets.
func TestIntegrationDepVersionResolvesAcrossAllRepositories(t *testing.T) {
	t.Parallel()
	_, _, rr := initDepVersionPinTest(t)

	_, version, err := rr.LatestPackage("shared-db")
	if err != nil {
		t.Fatalf("LatestPackage shared-db: %v", err)
	}
	if version != "16.0" {
		t.Fatalf("expected LatestPackage to pick 16.0 across repos, got %q", version)
	}

	repo, err := rr.FindRepoForPackage("shared-db", version)
	if err != nil {
		t.Fatalf("FindRepoForPackage shared-db@%s: %v", version, err)
	}
	if repo != depPinForeignRepo {
		t.Fatalf("expected shared-db@%s to resolve to repo %q, got %q", version, depPinForeignRepo, repo)
	}
}

// TestIntegrationUnpinnedDepCompilesAgainstForeignRepo is the regression: with
// no `version:`, the parent's responses are compiled against the other repo's
// package and the install dies in dependency compile. Nothing is installed.
func TestIntegrationUnpinnedDepCompilesAgainstForeignRepo(t *testing.T) {
	t.Parallel()
	c, sd, _ := initDepVersionPinTest(t)

	err := c.InstallPackage(context.TODO(), "unpinned-parent", "1.0", packages.Responses{
		"port":   "9091",
		"dbpass": "secret123",
	}, false, "", false)
	if err == nil {
		t.Fatal("expected install to fail: the dep resolves to the foreign repo's shared-db@16.0")
	}
	if !strings.Contains(err.Error(), `dependency "db"`) {
		t.Fatalf("expected the error to name the failing dependency, got: %v", err)
	}
	// `database` is unknown to shared-db@16.0 and `dbname` is missing from
	// the responses -- exactly two, and the count is what an operator sees.
	if !strings.Contains(err.Error(), "2 response validation error(s)") {
		t.Fatalf("expected two response validation errors, got: %v", err)
	}

	depEffName := packages.DependencyName("unpinned-parent", "db")
	for _, version := range []string{"1.0", "16.0"} {
		for _, repo := range []string{depPinParentRepo, depPinForeignRepo} {
			unit := systemd.UnitName(repo, depEffName, version)
			if content := findInstalledUnitContent(t, sd, unit); content != "" {
				t.Fatalf("expected no dep unit to be installed, found %s", unit)
			}
		}
	}
}

// TestIntegrationVersionPinnedDepUsesParentRepo is the fix: pinning only the
// version keeps the dep in the parent's own repo, so the responses land on the
// package they were written for.
func TestIntegrationVersionPinnedDepUsesParentRepo(t *testing.T) {
	t.Parallel()
	c, sd, _ := initDepVersionPinTest(t)

	if err := c.InstallPackage(context.TODO(), "pinned-parent", "1.0", packages.Responses{
		"port":   "9092",
		"dbpass": "secret123",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage pinned-parent@1.0: %v", err)
	}

	depEffName := packages.DependencyName("pinned-parent", "db")
	depUnit := systemd.UnitName(depPinParentRepo, depEffName, "1.0")
	depContent := findInstalledUnitContent(t, sd, depUnit)
	if depContent == "" {
		t.Fatalf("expected dep unit %s to be installed", depUnit)
	}

	// The foreign repo's shared-db has no `database` question at all, so
	// DB_NAME=appdb can only have come from the parent's own repo.
	if !strings.Contains(depContent, "-e DB_NAME=appdb") {
		t.Fatalf("expected dep unit to carry DB_NAME=appdb from %s/shared-db@1.0, got:\n%s",
			depPinParentRepo, depContent)
	}

	// And the foreign copy must not have been installed alongside it.
	foreignUnit := systemd.UnitName(depPinForeignRepo, depEffName, "16.0")
	if content := findInstalledUnitContent(t, sd, foreignUnit); content != "" {
		t.Fatalf("expected no unit for the foreign shared-db, found %s", foreignUnit)
	}

	parentUnit := systemd.UnitName(depPinParentRepo, "pinned-parent", "1.0")
	parentContent := findInstalledUnitContent(t, sd, parentUnit)
	if parentContent == "" {
		t.Fatalf("expected parent unit %s to be installed", parentUnit)
	}
	if !strings.Contains(parentContent, "TOWNOS_DEP_DB_PORT_SQL=5432") {
		t.Fatalf("expected parent unit to resolve the dep's named port, got:\n%s", parentContent)
	}
}

// TestIntegrationDepRepoPinWithoutVersionStillResolvesGlobally pins the trap
// that makes `repo:` look like the fix when it is not: the version is resolved
// before the repo is consulted, so a repo pin alone asks the parent's repo for
// a version only the other repo carries and the load fails outright.
func TestIntegrationDepRepoPinWithoutVersionStillResolvesGlobally(t *testing.T) {
	t.Parallel()
	c, _, _ := initDepVersionPinTest(t)

	err := c.InstallPackage(context.TODO(), "repo-pinned-parent", "1.0", packages.Responses{
		"port":   "9093",
		"dbpass": "secret123",
	}, false, "", false)
	if err == nil {
		t.Fatal("expected install to fail: repo is pinned but the version still resolves to 16.0")
	}
	if !strings.Contains(err.Error(), `dependency "db"`) {
		t.Fatalf("expected the error to name the failing dependency, got: %v", err)
	}
	if !strings.Contains(err.Error(), "16.0") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected a load failure for shared-db@16.0 in the pinned repo, got: %v", err)
	}
}

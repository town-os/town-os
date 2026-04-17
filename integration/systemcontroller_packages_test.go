// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// --- ListPackages integration tests ---

func TestSystemControllerListPackagesEmpty(t *testing.T) {
	t.Parallel()
	c := initSystemControllerRepoTest(t)

	pkgs, err := c.ListPackages(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}

	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 packages, got %d", len(pkgs.Entries))
	}
}

func TestSystemControllerListPackagesSingleRepo(t *testing.T) {
	t.Parallel()
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	pkgs, err := c.ListPackages(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}

	if len(pkgs.Entries) != 4 {
		t.Fatalf("expected 4 packages, got %d", len(pkgs.Entries))
	}

	// Results are sorted, latest version only.
	if pkgs.Entries[0].Repo != "core" || pkgs.Entries[0].Name != "app-with-cache" || pkgs.Entries[0].Version != "1.0" {
		t.Fatalf("expected core/app-with-cache@1.0, got %s/%s@%s", pkgs.Entries[0].Repo, pkgs.Entries[0].Name, pkgs.Entries[0].Version)
	}
	if pkgs.Entries[1].Repo != "core" || pkgs.Entries[1].Name != "demo-nginx" || pkgs.Entries[1].Version != "1.0" {
		t.Fatalf("expected core/demo-nginx@1.0, got %s/%s@%s", pkgs.Entries[1].Repo, pkgs.Entries[1].Name, pkgs.Entries[1].Version)
	}
	if pkgs.Entries[2].Repo != "core" || pkgs.Entries[2].Name != "nginx" || pkgs.Entries[2].Version != "2.0" {
		t.Fatalf("expected core/nginx@2.0, got %s/%s@%s", pkgs.Entries[2].Repo, pkgs.Entries[2].Name, pkgs.Entries[2].Version)
	}
	if pkgs.Entries[3].Repo != "core" || pkgs.Entries[3].Name != "redis" || pkgs.Entries[3].Version != "7.0" {
		t.Fatalf("expected core/redis@7.0, got %s/%s@%s", pkgs.Entries[3].Repo, pkgs.Entries[3].Name, pkgs.Entries[3].Version)
	}

	// Verify fields are consistent with PackageIdentity.
	for _, p := range pkgs.Entries {
		identity := fmt.Sprintf("%s/%s@%s", p.Repo, p.Name, p.Version)
		pi, err := packages.ParsePackageIdentity(identity)
		if err != nil {
			t.Fatalf("invalid package identity %q: %v", identity, err)
		}
		if pi.String() != identity {
			t.Fatalf("round-trip mismatch: %q != %q", pi.String(), identity)
		}
	}
}

func TestSystemControllerListPackagesMultipleRepos(t *testing.T) {
	t.Parallel()
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	pkgs, err := c.ListPackages(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}

	if len(pkgs.Entries) != 7 {
		t.Fatalf("expected 7 packages, got %d", len(pkgs.Entries))
	}

	// Verify all expected packages present.
	pkgSet := map[string]bool{}
	for _, p := range pkgs.Entries {
		pkgSet[fmt.Sprintf("%s/%s@%s", p.Repo, p.Name, p.Version)] = true
	}

	for _, want := range []string{"core/app-with-cache@1.0", "core/demo-nginx@1.0", "core/nginx@2.0", "core/redis@7.0", "extras/mosquitto@2.0", "extras/postgres@16.0", "extras/underscore-questions@1.0"} {
		if !pkgSet[want] {
			t.Fatalf("expected %s in package list", want)
		}
	}

	// Verify sorted order by name when sort is requested.
	sortedPkgs, err := c.ListPackages(context.TODO(), systemcontroller.ListParams{SortBy: "name", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListPackages sorted: %v", err)
	}
	for i := 1; i < len(sortedPkgs.Entries); i++ {
		if sortedPkgs.Entries[i-1].Name > sortedPkgs.Entries[i].Name {
			t.Fatalf("packages not sorted: %q > %q at index %d", sortedPkgs.Entries[i-1].Name, sortedPkgs.Entries[i].Name, i)
		}
	}
}

func TestSystemControllerListPackagesAfterRemoveRepo(t *testing.T) {
	t.Parallel()
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	// Remove extras.
	if err := c.RemoveRepository(context.TODO(), "extras"); err != nil {
		t.Fatalf("RemoveRepository extras: %v", err)
	}

	pkgs, err := c.ListPackages(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListPackages after remove: %v", err)
	}

	// Only core packages should remain.
	if len(pkgs.Entries) != 4 {
		t.Fatalf("expected 4 packages after removing extras, got %d", len(pkgs.Entries))
	}

	if pkgs.Entries[0].Repo != "core" || pkgs.Entries[0].Name != "app-with-cache" || pkgs.Entries[0].Version != "1.0" {
		t.Fatalf("expected core/app-with-cache@1.0, got %s/%s@%s", pkgs.Entries[0].Repo, pkgs.Entries[0].Name, pkgs.Entries[0].Version)
	}
	if pkgs.Entries[1].Repo != "core" || pkgs.Entries[1].Name != "demo-nginx" || pkgs.Entries[1].Version != "1.0" {
		t.Fatalf("expected core/demo-nginx@1.0, got %s/%s@%s", pkgs.Entries[1].Repo, pkgs.Entries[1].Name, pkgs.Entries[1].Version)
	}
	if pkgs.Entries[2].Repo != "core" || pkgs.Entries[2].Name != "nginx" || pkgs.Entries[2].Version != "2.0" {
		t.Fatalf("expected core/nginx@2.0, got %s/%s@%s", pkgs.Entries[2].Repo, pkgs.Entries[2].Name, pkgs.Entries[2].Version)
	}
	if pkgs.Entries[3].Repo != "core" || pkgs.Entries[3].Name != "redis" || pkgs.Entries[3].Version != "7.0" {
		t.Fatalf("expected core/redis@7.0, got %s/%s@%s", pkgs.Entries[3].Repo, pkgs.Entries[3].Name, pkgs.Entries[3].Version)
	}
}

// --- GetPackageQuestions integration tests ---

func TestSystemControllerGetPackageQuestions(t *testing.T) {
	t.Parallel()
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	questions, err := c.GetPackageQuestions(context.TODO(), "nginx")
	if err != nil {
		t.Fatalf("GetPackageQuestions nginx: %v", err)
	}

	// nginx 2.0 (latest) should have hostname and port questions
	if len(questions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(questions))
	}

	hostname, ok := questions["hostname"]
	if !ok {
		t.Fatal("expected hostname question")
	}
	if hostname.Query == "" {
		t.Fatal("expected non-empty hostname query")
	}
	if hostname.Type != packages.Hostname {
		t.Fatalf("expected hostname type %q, got %q", packages.Hostname, hostname.Type)
	}

	port, ok := questions["port"]
	if !ok {
		t.Fatal("expected port question")
	}
	if port.Type != packages.Port {
		t.Fatalf("expected port type %q, got %q", packages.Port, port.Type)
	}
}

func TestSystemControllerGetPackageQuestionsMultipleRepos(t *testing.T) {
	t.Parallel()
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	// postgres from extras
	pgQ, err := c.GetPackageQuestions(context.TODO(), "postgres")
	if err != nil {
		t.Fatalf("GetPackageQuestions postgres: %v", err)
	}

	for _, key := range []string{"user", "password", "dbname"} {
		if _, ok := pgQ[key]; !ok {
			t.Fatalf("expected postgres %s question", key)
		}
	}
}

func TestSystemControllerGetPackageQuestionsNotFound(t *testing.T) {
	t.Parallel()
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	_, err := c.GetPackageQuestions(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
}

func TestSystemControllerListPackagesInstalledOnlyFilter(t *testing.T) {
	t.Parallel()
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install only nginx (core repo also has other packages).
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "test", "port": "9090"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// List all packages: should contain more than 1.
	all, err := c.ListPackages(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListPackages all: %v", err)
	}
	if len(all.Entries) < 2 {
		t.Fatalf("expected at least 2 packages, got %d", len(all.Entries))
	}

	// List with installed_only: should only return installed packages.
	installed, err := c.ListPackages(context.TODO(), systemcontroller.ListParams{InstalledOnly: true})
	if err != nil {
		t.Fatalf("ListPackages installed_only: %v", err)
	}
	if len(installed.Entries) != 1 {
		t.Fatalf("expected 1 installed package, got %d", len(installed.Entries))
	}
	if installed.Entries[0].Name != "nginx" {
		t.Fatalf("expected 'nginx', got %q", installed.Entries[0].Name)
	}
	if !installed.Entries[0].Installed {
		t.Fatal("expected installed=true")
	}
	if installed.TotalCount != 1 {
		t.Fatalf("expected total_count=1, got %d", installed.TotalCount)
	}
}

// --- Package Manifest integration tests ---

func TestSystemControllerPackageManifest(t *testing.T) {
	t.Parallel()
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	manifest, err := c.GetPackageManifest(context.TODO(), "core", "nginx", "2.0")
	if err != nil {
		t.Fatalf("GetPackageManifest: %v", err)
	}

	if manifest == "" {
		t.Fatal("expected non-empty manifest")
	}

	// The nginx package YAML should contain the image field.
	if !strings.Contains(manifest, "image") {
		t.Fatalf("expected manifest to contain 'image', got: %s", manifest)
	}
}

func TestSystemControllerPackageManifestNotFound(t *testing.T) {
	t.Parallel()
	c := initSystemControllerRepoTest(t)

	_, err := c.GetPackageManifest(context.TODO(), "nonexistent", "fake", "0.0")
	if err == nil {
		t.Fatal("expected error for non-existent package manifest")
	}
}

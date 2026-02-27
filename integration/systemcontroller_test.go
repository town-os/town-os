package integration_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

func scRepoCredentials() (string, string) {
	return os.Getenv(packages.EnvRepoUsername), os.Getenv(packages.EnvRepoPassword)
}

func addRepoWithCreds(c *systemcontroller.SystemdClient, name, rawURL string) error {
	user, pass := scRepoCredentials()
	return c.AddRepository(context.TODO(), name, rawURL, user, pass)
}

func initSystemControllerTest(t *testing.T) *systemcontroller.SystemdClient {
	t.Helper()
	c, _ := initSystemControllerTestWithStorage(t)
	return c
}

func initSystemControllerTestWithStorage(t *testing.T) (*systemcontroller.SystemdClient, *storage.BtrFS) {
	t.Helper()

	btr := storage.InitBtrFS("/data/btrfs")
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: btr})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, btr
}

func TestSystemControllerCreateAndList(t *testing.T) {
	c := initSystemControllerTest(t)

	baseResult, err := c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing before create: %v", err)
	}
	baseCount := len(baseResult.Entries)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-create-list"}); err != nil {
		t.Fatalf("error creating filesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-create-list"); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", "sc-create-list", err)
		}
	})

	listResult, err := c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing after create: %v", err)
	}

	if len(listResult.Entries) != baseCount+1 {
		t.Fatalf("expected %d filesystems after create, got %d", baseCount+1, len(listResult.Entries))
	}

	listResult, err = c.ListFilesystems(context.TODO(), "sc-create-list", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing with exact prefix: %v", err)
	}

	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem under test path, got %d", len(listResult.Entries))
	}
}

func TestSystemControllerRemove(t *testing.T) {
	c := initSystemControllerTest(t)

	baseResult, err := c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing before create: %v", err)
	}
	baseCount := len(baseResult.Entries)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-remove"}); err != nil {
		t.Fatalf("error creating filesystem: %v", err)
	}

	if err := c.RemoveFilesystem(context.TODO(), "sc-remove"); err != nil {
		t.Fatalf("error removing filesystem: %v", err)
	}

	listResult, err := c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing after remove: %v", err)
	}

	if len(listResult.Entries) != baseCount {
		t.Fatalf("expected %d filesystems after remove, got %d", baseCount, len(listResult.Entries))
	}
}

func TestSystemControllerMultipleFilesystems(t *testing.T) {
	c := initSystemControllerTest(t)

	baseResult, err := c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing before create: %v", err)
	}
	baseCount := len(baseResult.Entries)

	names := []string{"sc-multi-a", "sc-multi-b", "sc-multi-c"}
	for _, name := range names {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("error creating %q: %v", name, err)
		}
		// sc-multi-b is explicitly removed in the test body below.
		if name != "sc-multi-b" {
			name := name
			t.Cleanup(func() {
				if err := c.RemoveFilesystem(context.TODO(), name); err != nil {
					t.Errorf("cleanup RemoveFilesystem(%q): %v", name, err)
				}
			})
		}
	}

	listResult, err := c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing after creates: %v", err)
	}

	if len(listResult.Entries) != baseCount+len(names) {
		t.Fatalf("expected %d filesystems, got %d", baseCount+len(names), len(listResult.Entries))
	}

	// Remove one and verify count
	if err := c.RemoveFilesystem(context.TODO(), "sc-multi-b"); err != nil {
		t.Fatalf("error removing sc-multi-b: %v", err)
	}

	listResult, err = c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing after partial remove: %v", err)
	}

	if len(listResult.Entries) != baseCount+len(names)-1 {
		t.Fatalf("expected %d filesystems after partial remove, got %d", baseCount+len(names)-1, len(listResult.Entries))
	}
}

func TestSystemControllerListPrefix(t *testing.T) {
	c := initSystemControllerTest(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-pfx-a"}); err != nil {
		t.Fatalf("error creating sc-pfx-a: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-pfx-a"); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", "sc-pfx-a", err)
		}
	})

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-pfx-b"}); err != nil {
		t.Fatalf("error creating sc-pfx-b: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-pfx-b"); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", "sc-pfx-b", err)
		}
	})

	listResult, err := c.ListFilesystems(context.TODO(), "sc-pfx-a", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing with prefix: %v", err)
	}

	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem for prefix sc-pfx-a, got %d", len(listResult.Entries))
	}
}

func TestSystemControllerModifyFilesystem(t *testing.T) {
	c := initSystemControllerTest(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-modify"}); err != nil {
		t.Fatalf("error creating filesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-modify"); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", "sc-modify", err)
		}
	})

	if err := c.ModifyFilesystem(context.TODO(), "sc-modify", storage.Filesystem{Name: "sc-modify", Quota: 1024}); err != nil {
		t.Fatalf("ModifyFilesystem: %v", err)
	}
}

func TestSystemControllerModifyPackageVolumeRenameRejected(t *testing.T) {
	c, btr := initSystemControllerTestWithStorage(t)

	volPath := "installed/repo/pkg/1.0/data"
	if err := btr.CreateFilesystem(storage.Filesystem{Name: volPath}); err != nil {
		t.Fatalf("CreateFilesystem(%q): %v", volPath, err)
	}
	t.Cleanup(func() {
		if err := btr.RemoveFilesystem(volPath); err != nil {
			t.Logf("cleanup RemoveFilesystem(%q): %v", volPath, err)
		}
	})

	err := c.ModifyFilesystem(context.TODO(), volPath, storage.Filesystem{
		Name: "installed/repo/pkg/1.0/renamed",
	})
	if err == nil {
		t.Fatal("expected error when renaming installed package volume")
	}
}

func TestSystemControllerModifyPackageVolumeQuotaAllowed(t *testing.T) {
	c, btr := initSystemControllerTestWithStorage(t)

	volPath := "installed/repo/pkg/1.0/data"
	if err := btr.CreateFilesystem(storage.Filesystem{Name: volPath}); err != nil {
		t.Fatalf("CreateFilesystem(%q): %v", volPath, err)
	}
	t.Cleanup(func() {
		if err := btr.RemoveFilesystem(volPath); err != nil {
			t.Logf("cleanup RemoveFilesystem(%q): %v", volPath, err)
		}
	})

	if err := c.ModifyFilesystem(context.TODO(), volPath, storage.Filesystem{
		Name:  volPath,
		Quota: 4096,
	}); err != nil {
		t.Fatalf("ModifyFilesystem quota on installed volume: %v", err)
	}
}

func TestSystemControllerFullLifecycle(t *testing.T) {
	c := initSystemControllerTest(t)

	baseResult, err := c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems before create: %v", err)
	}
	baseCount := len(baseResult.Entries)

	// Create
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-lifecycle"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Verify exists
	listResult, err := c.ListFilesystems(context.TODO(), "sc-lifecycle", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems to verify creation: %v", err)
	}
	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(listResult.Entries))
	}

	// Remove
	if err := c.RemoveFilesystem(context.TODO(), "sc-lifecycle"); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	// Verify gone
	listResult, err = c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems to verify removal: %v", err)
	}
	if len(listResult.Entries) != baseCount {
		t.Fatalf("expected %d filesystems after remove, got %d", baseCount, len(listResult.Entries))
	}
}

// --- Repository integration tests ---

func initSystemControllerRepoTest(t *testing.T) *systemcontroller.SystemdClient {
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

	mock := storage.InitBtrFSMock()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c
}

func TestSystemControllerAddAndListRepository(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("error adding repository: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing repositories: %v", err)
	}

	if len(repos.Entries) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(repos.Entries))
	}

	if repos.Entries[0].Name != "core" {
		t.Fatalf("expected name %q, got %q", "core", repos.Entries[0].Name)
	}

	if repos.Entries[0].URL != testCoreURLString() {
		t.Fatalf("expected URL %q, got %q", testCoreURLString(), repos.Entries[0].URL)
	}
}

func TestSystemControllerRemoveRepository(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("error adding repository: %v", err)
	}

	if err := c.RemoveRepository(context.TODO(), "core"); err != nil {
		t.Fatalf("error removing repository: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing after remove: %v", err)
	}

	if len(repos.Entries) != 0 {
		t.Fatalf("expected 0 repositories after remove, got %d", len(repos.Entries))
	}
}

func TestSystemControllerAddMultipleRepositories(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("error adding core: %v", err)
	}

	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("error adding extras: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing repositories: %v", err)
	}

	if len(repos.Entries) != 2 {
		t.Fatalf("expected 2 repositories, got %d", len(repos.Entries))
	}

	names := map[string]bool{}
	for _, r := range repos.Entries {
		names[r.Name] = true
	}

	if !names["core"] {
		t.Fatal("expected core in list")
	}
	if !names["extras"] {
		t.Fatal("expected extras in list")
	}
}

func TestSystemControllerListRepositoriesEmpty(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}

	if len(repos.Entries) != 0 {
		t.Fatalf("expected 0 repositories, got %d", len(repos.Entries))
	}
}

func TestSystemControllerListRepositoriesAfterRemove(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	if err := c.RemoveRepository(context.TODO(), "core"); err != nil {
		t.Fatalf("RemoveRepository core: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories after remove: %v", err)
	}

	if len(repos.Entries) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(repos.Entries))
	}

	if repos.Entries[0].Name != "extras" {
		t.Fatalf("expected extras to remain, got %q", repos.Entries[0].Name)
	}

	if repos.Entries[0].URL != testExtrasURLString() {
		t.Fatalf("expected URL %q, got %q", testExtrasURLString(), repos.Entries[0].URL)
	}
}

func TestSystemControllerRemoveNonexistentRepository(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	err := c.RemoveRepository(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error removing nonexistent repository")
	}
}

func TestSystemControllerAddRepositoryBadClone(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	err := c.AddRepository(context.TODO(), "", "https://github.com/town-os/does-not-exist.git", "", "")
	if err == nil {
		t.Fatal("expected error for inaccessible repository")
	}

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}

	if len(repos.Entries) != 0 {
		t.Fatalf("expected 0 repositories after failed add, got %d", len(repos.Entries))
	}
}

func TestSystemControllerAddRepositoryPartialCredentials(t *testing.T) {
	t.Run("username without password", func(t *testing.T) {
		c := initSystemControllerRepoTest(t)

		err := c.AddRepository(context.TODO(), "", testCoreURLString(), "user", "")
		if err == nil {
			t.Fatal("expected error for username without password")
		}
	})

	t.Run("password without username", func(t *testing.T) {
		c := initSystemControllerRepoTest(t)

		err := c.AddRepository(context.TODO(), "", testCoreURLString(), "", "pass")
		if err == nil {
			t.Fatal("expected error for password without username")
		}
	})
}

func TestSystemControllerAddRepositoryWithCredentials(t *testing.T) {
	user, pass := scRepoCredentials()
	if user == "" {
		t.Skip("skipping: TOWN_OS_REPO_USERNAME not set")
	}

	c := initSystemControllerRepoTest(t)

	if err := c.AddRepository(context.TODO(), "", testCoreURLString(), user, pass); err != nil {
		t.Fatalf("AddRepository with credentials: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}

	if len(repos.Entries) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(repos.Entries))
	}

	if repos.Entries[0].Username != user {
		t.Fatalf("expected username %q, got %q", user, repos.Entries[0].Username)
	}
}

func TestSystemControllerAddRepositoryWithoutCredentials(t *testing.T) {
	user, _ := scRepoCredentials()
	if user != "" {
		t.Skip("skipping: TOWN_OS_REPO_USERNAME is set")
	}

	c := initSystemControllerRepoTest(t)

	if err := c.AddRepository(context.TODO(), "", testCoreURLString(), "", ""); err != nil {
		t.Fatalf("AddRepository without credentials: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}

	if len(repos.Entries) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(repos.Entries))
	}

	if repos.Entries[0].Username != "" {
		t.Fatalf("expected empty username, got %q", repos.Entries[0].Username)
	}
}

// --- ListPackages integration tests ---

func TestSystemControllerListPackagesEmpty(t *testing.T) {
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
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	pkgs, err := c.ListPackages(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}

	if len(pkgs.Entries) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(pkgs.Entries))
	}

	// Results are sorted, latest version only.
	if pkgs.Entries[0].Repo != "core" || pkgs.Entries[0].Name != "demo-nginx" || pkgs.Entries[0].Version != "1.0" {
		t.Fatalf("expected core/demo-nginx@1.0, got %s/%s@%s", pkgs.Entries[0].Repo, pkgs.Entries[0].Name, pkgs.Entries[0].Version)
	}
	if pkgs.Entries[1].Repo != "core" || pkgs.Entries[1].Name != "nginx" || pkgs.Entries[1].Version != "2.0" {
		t.Fatalf("expected core/nginx@2.0, got %s/%s@%s", pkgs.Entries[1].Repo, pkgs.Entries[1].Name, pkgs.Entries[1].Version)
	}
	if pkgs.Entries[2].Repo != "core" || pkgs.Entries[2].Name != "redis" || pkgs.Entries[2].Version != "7.0" {
		t.Fatalf("expected core/redis@7.0, got %s/%s@%s", pkgs.Entries[2].Repo, pkgs.Entries[2].Name, pkgs.Entries[2].Version)
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

	if len(pkgs.Entries) != 5 {
		t.Fatalf("expected 5 packages, got %d", len(pkgs.Entries))
	}

	// Verify all expected packages present.
	pkgSet := map[string]bool{}
	for _, p := range pkgs.Entries {
		pkgSet[fmt.Sprintf("%s/%s@%s", p.Repo, p.Name, p.Version)] = true
	}

	for _, want := range []string{"core/demo-nginx@1.0", "core/nginx@2.0", "core/redis@7.0", "extras/mosquitto@2.0", "extras/postgres@16.0"} {
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
	if len(pkgs.Entries) != 3 {
		t.Fatalf("expected 3 packages after removing extras, got %d", len(pkgs.Entries))
	}

	if pkgs.Entries[0].Repo != "core" || pkgs.Entries[0].Name != "demo-nginx" || pkgs.Entries[0].Version != "1.0" {
		t.Fatalf("expected core/demo-nginx@1.0, got %s/%s@%s", pkgs.Entries[0].Repo, pkgs.Entries[0].Name, pkgs.Entries[0].Version)
	}
	if pkgs.Entries[1].Repo != "core" || pkgs.Entries[1].Name != "nginx" || pkgs.Entries[1].Version != "2.0" {
		t.Fatalf("expected core/nginx@2.0, got %s/%s@%s", pkgs.Entries[1].Repo, pkgs.Entries[1].Name, pkgs.Entries[1].Version)
	}
	if pkgs.Entries[2].Repo != "core" || pkgs.Entries[2].Name != "redis" || pkgs.Entries[2].Version != "7.0" {
		t.Fatalf("expected core/redis@7.0, got %s/%s@%s", pkgs.Entries[2].Repo, pkgs.Entries[2].Name, pkgs.Entries[2].Version)
	}
}

// --- GetPackageQuestions integration tests ---

func TestSystemControllerGetPackageQuestions(t *testing.T) {
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
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	_, err := c.GetPackageQuestions(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
}

// --- Install integration tests ---

func initSystemControllerInstallTest(t *testing.T) (*systemcontroller.SystemdClient, *packages.RepositoryRoot) {
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

	inst := packages.NewInstallManager(dir)

	mock := storage.InitBtrFSMock()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, rr
}

func initSystemControllerInstallSystemdTest(t *testing.T) (*systemcontroller.SystemdClient, *systemd.MockManager) {
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

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst, Systemd: sd})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, sd
}

func TestSystemControllerInstallAndListInstalled(t *testing.T) {
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(pkgs.Entries))
	}
	if pkgs.Entries[0] != "core/nginx@1.0" {
		t.Fatalf("expected core/nginx@1.0, got %s", pkgs.Entries[0])
	}
}

func TestSystemControllerInstallAndGetResponses(t *testing.T) {
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	responses := packages.Responses{"hostname": "example", "port": "8080"}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	got, err := c.GetResponses(context.TODO(), "core", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}

	if got["hostname"] != "example" {
		t.Fatalf("expected hostname %q, got %q", "example", got["hostname"])
	}
	if got["port"] != "8080" {
		t.Fatalf("expected port %q, got %q", "8080", got["port"])
	}
}

func TestSystemControllerInstallFullLifecycle(t *testing.T) {
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Start empty.
	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled (initial): %v", err)
	}
	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 installed initially, got %d", len(pkgs.Entries))
	}

	// Install with responses.
	responses := packages.Responses{"hostname": "webserver", "port": "9090"}
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@2.0: %v", err)
	}

	// Verify installed.
	pkgs, err = c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after install: %v", err)
	}
	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(pkgs.Entries))
	}

	// Verify responses.
	got, err := c.GetResponses(context.TODO(), "core", "nginx", "2.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}
	if got["hostname"] != "webserver" {
		t.Fatalf("expected hostname %q, got %q", "webserver", got["hostname"])
	}

	// Uninstall.
	if err := c.UninstallPackage(context.TODO(), "core", "nginx", "2.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	// Verify uninstalled.
	pkgs, err = c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after uninstall: %v", err)
	}
	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 installed after uninstall, got %d", len(pkgs.Entries))
	}

	// Verify responses empty after uninstall.
	resp, err := c.GetResponses(context.TODO(), "core", "nginx", "2.0")
	if err != nil {
		t.Fatalf("GetResponses after uninstall: %v", err)
	}
	if len(resp) != 0 {
		t.Fatalf("expected empty responses after uninstall, got %v", resp)
	}
}

func TestSystemControllerInstallMultiplePackages(t *testing.T) {
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "alpha", "port": "80"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}
	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{"port": "6379", "password": "secret", "maxmemory": "100mb"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(pkgs.Entries) != 2 {
		t.Fatalf("expected 2 installed, got %d", len(pkgs.Entries))
	}

	// Each package has its own responses.
	nginxResp, err := c.GetResponses(context.TODO(), "core", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses nginx@1.0: %v", err)
	}
	if nginxResp["hostname"] != "alpha" {
		t.Fatalf("expected nginx hostname %q, got %q", "alpha", nginxResp["hostname"])
	}

	redisResp, err := c.GetResponses(context.TODO(), "core", "redis", "7.0")
	if err != nil {
		t.Fatalf("GetResponses redis@7.0: %v", err)
	}
	if redisResp["password"] != "secret" {
		t.Fatalf("expected redis password %q, got %q", "secret", redisResp["password"])
	}
}

func TestSystemControllerRepositoryFullLifecycle(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	// Start empty
	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories at start: %v", err)
	}
	if len(repos.Entries) != 0 {
		t.Fatalf("expected empty list, got %d", len(repos.Entries))
	}

	// Add two repos
	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("add core failed: %v", err)
	}
	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("add extras failed: %v", err)
	}

	// Verify both present
	repos, err = c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories after adding repos: %v", err)
	}
	if len(repos.Entries) != 2 {
		t.Fatalf("expected 2 repositories, got %d", len(repos.Entries))
	}

	// Remove one
	if err := c.RemoveRepository(context.TODO(), "core"); err != nil {
		t.Fatalf("remove core failed: %v", err)
	}

	// Verify only extras remains
	repos, err = c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories after removing core: %v", err)
	}
	if len(repos.Entries) != 1 {
		t.Fatalf("expected 1 repository after remove, got %d", len(repos.Entries))
	}
	if repos.Entries[0].Name != "extras" {
		t.Fatalf("expected extras to remain, got %q", repos.Entries[0].Name)
	}

	// Remove the last one
	if err := c.RemoveRepository(context.TODO(), "extras"); err != nil {
		t.Fatalf("remove extras failed: %v", err)
	}

	// Verify empty
	repos, err = c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories at end: %v", err)
	}
	if len(repos.Entries) != 0 {
		t.Fatalf("expected 0 repositories at end, got %d", len(repos.Entries))
	}
}

// --- Install + Systemd integration tests ---

func TestSystemControllerInstallCreatesSystemdUnit(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// nginx has 1 external port (8080->80):
	//   3 InstallUnit (service, socket, networkcontroller) +
	//   2 Enable (socket, networkcontroller) + 1 Start (service) = 6
	calls := sd.GetCalls()
	if len(calls) != 6 {
		t.Fatalf("expected 6 systemd calls, got %d", len(calls))
	}

	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	unitArg, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if unitArg != "town-os-package--core-nginx-1.0.service" {
		t.Fatalf("call 0: expected unit %q, got %v", "town-os-package--core-nginx-1.0.service", calls[0].Args[0])
	}

	// Last call should be Start for the service.
	lastCall := calls[len(calls)-1]
	if lastCall.Method != "SetStatus" {
		t.Fatalf("last call: expected SetStatus, got %q", lastCall.Method)
	}
	lastAction, ok := lastCall.Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if lastAction != systemd.Start {
		t.Fatalf("last call: expected Start, got %v", lastCall.Args[1])
	}
}

func TestSystemControllerUninstallRemovesSystemdUnit(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	if err := c.UninstallPackage(context.TODO(), "core", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage nginx@1.0: %v", err)
	}

	// Install (6) + Uninstall: ListPackageUnitFiles + 3 units * (Stop+Disable+Uninstall) = 10 → total 16
	// (3 units: service, socket, networkcontroller)
	calls := sd.GetCalls()
	if len(calls) != 16 {
		t.Fatalf("expected 16 systemd calls, got %d", len(calls))
	}

	// Install phase: first call is InstallUnit for service.
	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}

	// Install phase: last install call (index 5) is Start.
	call5Action, ok := calls[5].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if call5Action != systemd.Start {
		t.Fatalf("call 5: expected Start, got %v", calls[5].Args[1])
	}

	// Uninstall phase starts at index 6: ListPackageUnitFiles, then Stop, Disable, UninstallUnit per unit.
	if calls[6].Method != "ListPackageUnitFiles" {
		t.Fatalf("call 6: expected ListPackageUnitFiles, got %q", calls[6].Method)
	}
	if calls[7].Method != "SetStatus" {
		t.Fatalf("call 7: expected SetStatus, got %q", calls[7].Method)
	}
	stopAction, ok := calls[7].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if stopAction != systemd.Stop {
		t.Fatalf("call 7: expected Stop, got %v", calls[7].Args[1])
	}
}

func TestSystemControllerInstallUninstallFullLifecycle(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install nginx@1.0
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Verify listed as installed
	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after install: %v", err)
	}
	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(pkgs.Entries))
	}
	if pkgs.Entries[0] != "core/nginx@1.0" {
		t.Fatalf("expected core/nginx@1.0, got %s", pkgs.Entries[0])
	}

	// Verify 6 systemd calls from install (nginx has 1 ext port).
	calls := sd.GetCalls()
	if len(calls) != 6 {
		t.Fatalf("expected 6 systemd calls after install, got %d", len(calls))
	}

	// Uninstall
	if err := c.UninstallPackage(context.TODO(), "core", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage nginx@1.0: %v", err)
	}

	// Verify uninstalled
	pkgs, err = c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after uninstall: %v", err)
	}
	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 installed after uninstall, got %d", len(pkgs.Entries))
	}

	// Install (6) + Uninstall: ListPackageUnitFiles + 3 units * 3 ops = 10 → total 16
	// (3 units: service, socket, networkcontroller)
	calls = sd.GetCalls()
	if len(calls) != 16 {
		t.Fatalf("expected 16 systemd calls total, got %d", len(calls))
	}
}

func TestSystemControllerInstallMultiplePackagesSystemdUnits(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{"port": "6379", "password": "secret", "maxmemory": "100mb"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// nginx (1 ext port): 3 InstallUnit + 2 Enable + 1 Start = 6
	// redis (1 int port): 2 InstallUnit + 1 Enable + 1 Start = 4
	// Total = 10
	calls := sd.GetCalls()
	if len(calls) != 10 {
		t.Fatalf("expected 10 systemd calls, got %d", len(calls))
	}

	// First call is InstallUnit for nginx service.
	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	unitArg, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if unitArg != "town-os-package--core-nginx-1.0.service" {
		t.Fatalf("call 0: expected unit %q, got %q", "town-os-package--core-nginx-1.0.service", calls[0].Args[0])
	}

	// Redis service starts at index 6.
	if calls[6].Method != "InstallUnit" {
		t.Fatalf("call 6: expected InstallUnit, got %q", calls[6].Method)
	}
	unitArg2, ok := calls[6].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if unitArg2 != "town-os-package--core-redis-7.0.service" {
		t.Fatalf("call 6: expected unit %q, got %q", "town-os-package--core-redis-7.0.service", calls[6].Args[0])
	}
}

// --- Install + Real Systemd integration tests ---

func initSystemControllerInstallRealSystemdTest(t *testing.T) *systemcontroller.SystemdClient {
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

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.NewManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst, Systemd: sd})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c
}

func TestSystemControllerInstallWithRealSystemd(t *testing.T) {
	c := initSystemControllerInstallRealSystemdTest(t)

	unitName := systemd.UnitName("core", "nginx", "1.0")
	unitPath := "/etc/systemd/system/" + unitName

	// Cleanup: unconditionally stop/disable/remove all units to prevent leaks.
	t.Cleanup(func() {
		cleanup := systemd.NewManager()
		ctx := context.Background()
		allUnits := systemd.PackageUnitNames("core", "nginx", "1.0", "", packages.PortMap{8080: 80}, packages.PortMap{})
		for _, name := range allUnits {
			_ = cleanup.SetStatus(ctx, name, systemd.Stop)
			_ = cleanup.SetStatus(ctx, name, systemd.Disable)
			_ = cleanup.UninstallUnit(ctx, name)
		}
	})

	// Add core repo.
	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install nginx@1.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Verify listed as installed.
	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after install: %v", err)
	}
	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(pkgs.Entries))
	}
	if pkgs.Entries[0] != "core/nginx@1.0" {
		t.Fatalf("expected core/nginx@1.0, got %s", pkgs.Entries[0])
	}

	// Verify the unit is active via ListUnits.
	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{Search: "town-os-package--core-nginx"})
	if err != nil {
		t.Fatalf("ListUnits after install: %v", err)
	}

	var found bool
	for _, u := range units.Entries {
		if u.Name == unitName {
			found = true
			// The unit may be active, deactivating, or failed depending on
			// timing (no real container image exists to keep it running).
			// Any of these states confirms the unit was installed and started.
			switch u.ActiveState {
			case "active", "deactivating", "failed":
				// OK
			default:
				t.Fatalf("expected unit %q ActiveState active/deactivating/failed, got %q", unitName, u.ActiveState)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected unit %q in ListUnits output", unitName)
	}

	// Uninstall nginx@1.0.
	if err := c.UninstallPackage(context.TODO(), "core", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage nginx@1.0: %v", err)
	}

	// Verify no longer installed.
	pkgs, err = c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after uninstall: %v", err)
	}
	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 installed after uninstall, got %d", len(pkgs.Entries))
	}

	// Verify unit file no longer exists on disk.
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("expected unit file %q to be removed, got err: %v", unitPath, err)
	}
}

// --- Real container integration tests ---

// initSystemControllerRealContainerTest sets up a test server with real BtrFS
// storage, real systemd, and proper paths so that podman containers can
// actually be launched.
func initSystemControllerRealContainerTest(t *testing.T) *systemcontroller.SystemdClient {
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

	inst := packages.NewInstallManager(dir)
	btr := storage.InitBtrFS("/data/btrfs")
	sd := systemd.NewManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        btr,
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		BtrfsBasePath:  "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         "/var/run/town-os",
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c
}

// cleanupContainerUnits unconditionally stops, disables, and removes all
// systemd units for a package plus the podman container itself.
func cleanupContainerUnits(repo, pkgName, version string, external, internal packages.PortMap) {
	cleanup := systemd.NewManager()
	ctx := context.Background()
	allUnits := systemd.PackageUnitNames(repo, pkgName, version, "", external, internal)
	for _, name := range allUnits {
		_ = cleanup.SetStatus(ctx, name, systemd.Stop)
		_ = cleanup.SetStatus(ctx, name, systemd.Disable)
		_ = cleanup.UninstallUnit(ctx, name)
	}
	containerName := systemd.ContainerName(repo, pkgName, version)
	_ = exec.CommandContext(context.TODO(), "podman", "stop", "-t", "10", containerName).Run()
	_ = exec.CommandContext(context.TODO(), "podman", "rm", "-f", containerName).Run()
}

// waitForContainer polls podman inspect until the container reaches "running"
// state or the timeout expires.
func waitForContainer(t *testing.T, repo, pkgName, version string, timeout time.Duration) {
	t.Helper()
	containerName := systemd.ContainerName(repo, pkgName, version)
	unitName := systemd.UnitName(repo, pkgName, version)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := exec.CommandContext(context.TODO(), "podman", "inspect", "--format", "{{.State.Status}}", containerName).Output()
		if err == nil && strings.TrimSpace(string(out)) == "running" {
			return
		}
		time.Sleep(2 * time.Second)
	}
	// Log the systemd journal for debugging before failing.
	journal, _ := exec.CommandContext(context.TODO(), "journalctl", "-u", unitName, "--no-pager", "-n", "50").Output()
	t.Fatalf("container %q did not reach running state within %v\njournal:\n%s", containerName, timeout, string(journal))
}

func TestSystemControllerRealContainerLifecycle(t *testing.T) {
	c := initSystemControllerRealContainerTest(t)

	// Add core repo.
	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install redis@7.0 (port is auto-assigned by FindAvailablePort).
	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// Discover the dynamically assigned port.
	responses, err := c.GetResponses(context.TODO(), "core", "redis", "7.0")
	if err != nil {
		t.Fatalf("GetResponses redis@7.0: %v", err)
	}
	assignedPort := responses["port"]
	if assignedPort == "" {
		t.Fatal("expected non-empty port in responses after install")
	}
	portNum, err := strconv.ParseUint(assignedPort, 10, 16)
	if err != nil {
		t.Fatalf("parse assigned port %q: %v", assignedPort, err)
	}

	t.Cleanup(func() {
		cleanupContainerUnits("core", "redis", "7.0", packages.PortMap{}, packages.PortMap{uint16(portNum): 6379})
	})

	// Wait for the container to start (includes image pull).
	waitForContainer(t, "core", "redis", "7.0", 180*time.Second)

	// Verify the systemd unit is active.
	containerName := systemd.ContainerName("core", "redis", "7.0")
	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{Search: containerName})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	unitName := systemd.UnitName("core", "redis", "7.0")
	var found bool
	for _, u := range units.Entries {
		if u.Name == unitName {
			found = true
			if u.ActiveState != "active" {
				t.Fatalf("expected unit %q ActiveState active, got %q", unitName, u.ActiveState)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected unit %q in ListUnits output", unitName)
	}

	// Verify the assigned port is accessible via TCP from the host.
	// Retry with short dials because nested podman port forwarding can be slow.
	dialAddr := "127.0.0.1:" + assignedPort
	var conn net.Conn
	dialDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(dialDeadline) {
		conn, err = (&net.Dialer{Timeout: 5 * time.Second}).DialContext(context.TODO(), "tcp", dialAddr)
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		logs, _ := exec.CommandContext(context.TODO(), "podman", "logs", "--tail", "20", containerName).CombinedOutput()
		t.Fatalf("TCP connect to redis on port %s failed: %v\ncontainer logs:\n%s", assignedPort, err, string(logs))
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close redis connection: %v", err)
	}

	// Verify podman container is listed.
	out, err := exec.CommandContext(context.TODO(), "podman", "ps", "--filter", "name="+containerName, "--format", "{{.Names}}").Output()
	if err != nil {
		t.Fatalf("podman ps: %v", err)
	}
	if !strings.Contains(string(out), containerName) {
		t.Fatalf("expected %s in podman ps output, got: %s", containerName, string(out))
	}

	// Uninstall redis@7.0.
	if err := c.UninstallPackage(context.TODO(), "core", "redis", "7.0", false); err != nil {
		t.Fatalf("UninstallPackage redis@7.0: %v", err)
	}

	// Wait for the container to stop.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		inspectOut, inspectErr := exec.CommandContext(context.TODO(), "podman", "inspect", "--format", "{{.State.Status}}", containerName).Output()
		if inspectErr != nil || strings.TrimSpace(string(inspectOut)) != "running" {
			break
		}
		time.Sleep(time.Second)
	}

	// Verify the unit file is gone.
	unitPath := "/etc/systemd/system/" + unitName
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("expected unit file %q removed after uninstall, got err: %v", unitPath, err)
	}

	// Verify the container is no longer running.
	out, err = exec.CommandContext(context.TODO(), "podman", "ps", "--filter", "name="+containerName, "--format", "{{.Names}}").Output()
	if err == nil && strings.Contains(string(out), containerName) {
		t.Fatalf("expected %s not in podman ps after uninstall", containerName)
	}

	// Verify the assigned port is no longer accessible.
	postConn, postErr := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(context.TODO(), "tcp", dialAddr)
	if postErr == nil {
		_ = postConn.Close()
		t.Fatalf("expected port %s to be unreachable after uninstall", assignedPort)
	}
}

func TestSystemControllerRealContainerReinstall(t *testing.T) {
	c := initSystemControllerRealContainerTest(t)

	// Add core repo.
	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install redis@7.0 (port is auto-assigned by FindAvailablePort).
	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	waitForContainer(t, "core", "redis", "7.0", 180*time.Second)

	// Discover the dynamically assigned port.
	responses, err := c.GetResponses(context.TODO(), "core", "redis", "7.0")
	if err != nil {
		t.Fatalf("GetResponses redis@7.0: %v", err)
	}
	assignedPort := responses["port"]
	if assignedPort == "" {
		t.Fatal("expected non-empty port in responses after install")
	}
	portNum, err := strconv.ParseUint(assignedPort, 10, 16)
	if err != nil {
		t.Fatalf("parse assigned port %q: %v", assignedPort, err)
	}

	t.Cleanup(func() {
		cleanupContainerUnits("core", "redis", "7.0", packages.PortMap{}, packages.PortMap{uint16(portNum): 6379})
	})

	// Reinstall with the same version (same-version reinstall path).
	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"password":  "newpass",
		"maxmemory": "200mb",
	}, false, "", false); err != nil {
		t.Fatalf("Reinstall redis@7.0: %v", err)
	}

	// Wait for the new container to come up.
	waitForContainer(t, "core", "redis", "7.0", 180*time.Second)

	// Discover the port after reinstall (may have changed).
	responses, err = c.GetResponses(context.TODO(), "core", "redis", "7.0")
	if err != nil {
		t.Fatalf("GetResponses after reinstall: %v", err)
	}
	reinstallPort := responses["port"]
	if reinstallPort == "" {
		t.Fatal("expected non-empty port in responses after reinstall")
	}

	// Verify installed list still shows redis@7.0.
	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	var foundRedis bool
	for _, entry := range pkgs.Entries {
		if entry == "core/redis@7.0" {
			foundRedis = true
		}
	}
	if !foundRedis {
		t.Fatalf("expected core/redis@7.0 in installed list, got: %v", pkgs.Entries)
	}

	// Verify the assigned port is accessible via TCP after reinstall.
	// Retry because the container may be "running" before Redis binds the port.
	dialAddr := "127.0.0.1:" + reinstallPort
	var conn net.Conn
	tcpDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(tcpDeadline) {
		conn, err = (&net.Dialer{Timeout: 5 * time.Second}).DialContext(context.TODO(), "tcp", dialAddr)
		if err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		logs, _ := exec.CommandContext(context.TODO(), "podman", "logs", "--tail", "20", systemd.ContainerName("core", "redis", "7.0")).CombinedOutput()
		t.Fatalf("TCP connect to redis on port %s after reinstall failed: %v\ncontainer logs:\n%s", reinstallPort, err, string(logs))
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close redis connection: %v", err)
	}

	// Uninstall.
	if err := c.UninstallPackage(context.TODO(), "core", "redis", "7.0", false); err != nil {
		t.Fatalf("UninstallPackage redis@7.0: %v", err)
	}
}

// --- Systemd integration tests ---

func initRealSystemdTest(t *testing.T) *systemcontroller.SystemdClient {
	t.Helper()

	sd := systemd.NewManager()
	mock := storage.InitBtrFSMock()
	inst := packages.InitMockInstallManager()
	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "test", Version: "1.0"},
	}
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: mock, Systemd: sd, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c
}

func initSystemControllerSystemdTest(t *testing.T, sd *systemd.MockManager, installed ...packages.PackageIdentity) *systemcontroller.SystemdClient {
	t.Helper()

	btr := storage.InitBtrFS("/data/btrfs")
	inst := packages.InitMockInstallManager()
	inst.Installed = installed
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: btr, Installer: inst, Systemd: sd})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c
}

func TestSystemControllerSystemdListUnitsEmpty(t *testing.T) {
	sd := systemd.InitMockManager()
	c := initSystemControllerSystemdTest(t, sd)

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 0 {
		t.Fatalf("expected 0 units, got %d", len(units.Entries))
	}
}

func TestSystemControllerSystemdListUnitsPopulated(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", Description: "The NGINX HTTP Server", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "town-os-package--repo-redis-2.0.service", Description: "Redis", LoadState: "loaded", ActiveState: "inactive", SubState: "dead"},
		{Name: "town-os-package--repo-postgres-16.0.service", Description: "PostgreSQL", LoadState: "loaded", ActiveState: "active", SubState: "running"},
	}
	c := initSystemControllerSystemdTest(t, sd,
		packages.PackageIdentity{Repo: "repo", Name: "nginx", Version: "1.0"},
		packages.PackageIdentity{Repo: "repo", Name: "redis", Version: "2.0"},
		packages.PackageIdentity{Repo: "repo", Name: "postgres", Version: "16.0"},
	)

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 3 {
		t.Fatalf("expected 3 units, got %d", len(units.Entries))
	}

	if units.Entries[0].Name != "town-os-package--repo-nginx-1.0.service" {
		t.Fatalf("expected first unit town-os-package--repo-nginx-1.0.service, got %q", units.Entries[0].Name)
	}
	if units.Entries[0].Description != "The NGINX HTTP Server" {
		t.Fatalf("expected description %q, got %q", "The NGINX HTTP Server", units.Entries[0].Description)
	}
	if units.Entries[0].ActiveState != "active" {
		t.Fatalf("expected active state %q, got %q", "active", units.Entries[0].ActiveState)
	}
	if units.Entries[0].SubState != "running" {
		t.Fatalf("expected sub state %q, got %q", "running", units.Entries[0].SubState)
	}

	if units.Entries[1].Name != "town-os-package--repo-redis-2.0.service" {
		t.Fatalf("expected second unit town-os-package--repo-redis-2.0.service, got %q", units.Entries[1].Name)
	}
	if units.Entries[1].ActiveState != "inactive" {
		t.Fatalf("expected inactive state for redis, got %q", units.Entries[1].ActiveState)
	}

	if units.Entries[2].Name != "town-os-package--repo-postgres-16.0.service" {
		t.Fatalf("expected third unit town-os-package--repo-postgres-16.0.service, got %q", units.Entries[2].Name)
	}
}

func TestSystemControllerSystemdListUnitsPreservesAllFields(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-test-1.0.service", Description: "Test Unit", LoadState: "loaded", ActiveState: "activating", SubState: "start-pre"},
	}
	c := initSystemControllerSystemdTest(t, sd,
		packages.PackageIdentity{Repo: "repo", Name: "test", Version: "1.0"},
	)

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units.Entries))
	}

	u := units.Entries[0]
	if u.Name != "town-os-package--repo-test-1.0.service" {
		t.Fatalf("Name: expected %q, got %q", "town-os-package--repo-test-1.0.service", u.Name)
	}
	if u.Description != "Test Unit" {
		t.Fatalf("Description: expected %q, got %q", "Test Unit", u.Description)
	}
	if u.LoadState != "loaded" {
		t.Fatalf("LoadState: expected %q, got %q", "loaded", u.LoadState)
	}
	if u.ActiveState != "activating" {
		t.Fatalf("ActiveState: expected %q, got %q", "activating", u.ActiveState)
	}
	if u.SubState != "start-pre" {
		t.Fatalf("SubState: expected %q, got %q", "start-pre", u.SubState)
	}
}

func TestSystemControllerSystemdSetUnitStatusAllActions(t *testing.T) {
	for _, action := range []systemd.StatusAction{systemd.Start, systemd.Stop, systemd.Restart} {
		t.Run(string(action), func(t *testing.T) {
			sd := systemd.InitMockManager()
			c := initSystemControllerSystemdTest(t, sd)

			if err := c.SetUnitStatus(context.TODO(), "nginx.service", action); err != nil {
				t.Fatalf("SetUnitStatus(%q, %q): %v", "nginx.service", action, err)
			}

			calls := sd.GetCalls()
			if len(calls) != 1 {
				t.Fatalf("expected 1 call, got %d", len(calls))
			}
			if calls[0].Method != "SetStatus" {
				t.Fatalf("expected SetStatus call, got %q", calls[0].Method)
			}

			unit, ok := calls[0].Args[0].(string)
			if !ok {
				t.Fatalf("expected string arg, got %T", calls[0].Args[0])
			}
			if unit != "nginx.service" {
				t.Fatalf("expected unit %q, got %q", "nginx.service", unit)
			}

			gotAction, ok := calls[0].Args[1].(systemd.StatusAction)
			if !ok {
				t.Fatalf("expected StatusAction arg, got %T", calls[0].Args[1])
			}
			if gotAction != action {
				t.Fatalf("expected action %q, got %q", action, gotAction)
			}
		})
	}

	// Enable and disable must be rejected.
	for _, action := range []systemd.StatusAction{systemd.Enable, systemd.Disable} {
		t.Run(string(action), func(t *testing.T) {
			sd := systemd.InitMockManager()
			c := initSystemControllerSystemdTest(t, sd)

			err := c.SetUnitStatus(context.TODO(), "nginx.service", action)
			if err == nil {
				t.Fatalf("expected error for %q action", action)
			}
		})
	}
}

func TestSystemControllerSystemdSetUnitStatusInvalidAction(t *testing.T) {
	sd := systemd.InitMockManager()
	c := initSystemControllerSystemdTest(t, sd)

	err := c.SetUnitStatus(context.TODO(), "nginx.service", systemd.StatusAction("bogus"))
	if err == nil {
		t.Fatal("expected error for invalid action")
	}
}

func TestSystemControllerSystemdLogReplay(t *testing.T) {
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	sd := systemd.InitMockManager()
	sd.Entries = []systemd.JournalEntry{
		{Message: "Starting nginx...", RealtimeTimestamp: ts, SystemdUnit: "nginx.service", Priority: "6"},
		{Message: "Started nginx.", RealtimeTimestamp: ts.Add(time.Second), SystemdUnit: "nginx.service", Priority: "6"},
		{Message: "Listening on :80", RealtimeTimestamp: ts.Add(2 * time.Second), SystemdUnit: "nginx.service", Priority: "6"},
	}
	c := initSystemControllerSystemdTest(t, sd)

	ch, err := c.LogReplay(context.TODO(), "nginx.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	var entries []systemd.JournalEntry
	for e := range ch {
		entries = append(entries, e)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	if entries[0].Message != "Starting nginx..." {
		t.Fatalf("expected first message %q, got %q", "Starting nginx...", entries[0].Message)
	}
	if entries[1].Message != "Started nginx." {
		t.Fatalf("expected second message %q, got %q", "Started nginx.", entries[1].Message)
	}
	if entries[2].Message != "Listening on :80" {
		t.Fatalf("expected third message %q, got %q", "Listening on :80", entries[2].Message)
	}
}

func TestSystemControllerSystemdLogReplayPreservesFields(t *testing.T) {
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	sd := systemd.InitMockManager()
	sd.Entries = []systemd.JournalEntry{
		{
			Message:           "test message",
			RealtimeTimestamp: ts,
			Priority:          "4",
			SystemdUnit:       "test.service",
			Hostname:          "testhost",
			PID:               "1234",
			UID:               "1000",
			Comm:              "nginx",
			SyslogIdentifier:  "nginx",
			Transport:         "journal",
		},
	}
	c := initSystemControllerSystemdTest(t, sd)

	ch, err := c.LogReplay(context.TODO(), "test.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	entry := <-ch
	// Drain the channel.
	for range ch {
	}

	if entry.Message != "test message" {
		t.Fatalf("Message: expected %q, got %q", "test message", entry.Message)
	}
	if !entry.RealtimeTimestamp.Equal(ts) {
		t.Fatalf("RealtimeTimestamp: expected %v, got %v", ts, entry.RealtimeTimestamp)
	}
	if entry.Priority != "4" {
		t.Fatalf("Priority: expected %q, got %q", "4", entry.Priority)
	}
	if entry.SystemdUnit != "test.service" {
		t.Fatalf("SystemdUnit: expected %q, got %q", "test.service", entry.SystemdUnit)
	}
	if entry.Hostname != "testhost" {
		t.Fatalf("Hostname: expected %q, got %q", "testhost", entry.Hostname)
	}
	if entry.PID != "1234" {
		t.Fatalf("PID: expected %q, got %q", "1234", entry.PID)
	}
	if entry.UID != "1000" {
		t.Fatalf("UID: expected %q, got %q", "1000", entry.UID)
	}
	if entry.Comm != "nginx" {
		t.Fatalf("Comm: expected %q, got %q", "nginx", entry.Comm)
	}
	if entry.SyslogIdentifier != "nginx" {
		t.Fatalf("SyslogIdentifier: expected %q, got %q", "nginx", entry.SyslogIdentifier)
	}
	if entry.Transport != "journal" {
		t.Fatalf("Transport: expected %q, got %q", "journal", entry.Transport)
	}
}

func TestSystemControllerSystemdLogReplayEmpty(t *testing.T) {
	sd := systemd.InitMockManager()
	c := initSystemControllerSystemdTest(t, sd)

	ch, err := c.LogReplay(context.TODO(), "nginx.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	var entries []systemd.JournalEntry
	for e := range ch {
		entries = append(entries, e)
	}

	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestSystemControllerSystemdLogReplayManyEntries(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Entries = make([]systemd.JournalEntry, 100)
	for i := range sd.Entries {
		sd.Entries[i] = systemd.JournalEntry{
			Message:     time.Now().Format(time.RFC3339Nano),
			SystemdUnit: "bulk.service",
			Priority:    "6",
		}
	}
	c := initSystemControllerSystemdTest(t, sd)

	ch, err := c.LogReplay(context.TODO(), "bulk.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	count := 0
	for range ch {
		count++
	}

	if count != 100 {
		t.Fatalf("expected 100 entries, got %d", count)
	}
}

func TestSystemControllerSystemdLogReplayCallLog(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Entries = []systemd.JournalEntry{
		{Message: "hello"},
	}
	c := initSystemControllerSystemdTest(t, sd)

	ch, err := c.LogReplay(context.TODO(), "nginx.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}
	for range ch {
	}

	calls := sd.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "LogReplay" {
		t.Fatalf("expected LogReplay call, got %q", calls[0].Method)
	}
	if len(calls[0].Args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(calls[0].Args))
	}
	unit, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatalf("expected string arg, got %T", calls[0].Args[0])
	}
	if unit != "nginx.service" {
		t.Fatalf("expected unit %q, got %q", "nginx.service", unit)
	}
}

func TestSystemControllerSystemdLogTailPriority(t *testing.T) {
	sd := systemd.InitMockManager()
	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "debug msg", Priority: "7", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c2", Message: "info msg", Priority: "6", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "error msg", Priority: "3", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "critical msg", Priority: "2", RealtimeTimestamp: now.Add(-time.Second)},
	}
	c := initSystemControllerSystemdTest(t, sd)

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Priority: 3})
	if err != nil {
		t.Fatalf("LogTail with priority: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries with priority <= 3, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "error msg" {
		t.Fatalf("expected first entry %q, got %q", "error msg", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "critical msg" {
		t.Fatalf("expected second entry %q, got %q", "critical msg", result.Entries[1].Message)
	}
}

func TestSystemControllerSystemdLogTailPriorityNoFilter(t *testing.T) {
	sd := systemd.InitMockManager()
	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "debug msg", Priority: "7", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c2", Message: "error msg", Priority: "3", RealtimeTimestamp: now.Add(-time.Second)},
	}
	c := initSystemControllerSystemdTest(t, sd)

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100})
	if err != nil {
		t.Fatalf("LogTail without priority: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries without priority filter, got %d", len(result.Entries))
	}
}

func TestSystemControllerSystemdLogTailPriorityWithGrep(t *testing.T) {
	sd := systemd.InitMockManager()
	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "error: disk full", Priority: "3", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c2", Message: "info: disk ok", Priority: "6", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c3", Message: "error: memory low", Priority: "3", RealtimeTimestamp: now.Add(-time.Second)},
	}
	c := initSystemControllerSystemdTest(t, sd)

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Priority: 3, Grep: "disk"})
	if err != nil {
		t.Fatalf("LogTail with priority+grep: %v", err)
	}

	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "error: disk full" {
		t.Fatalf("expected entry %q, got %q", "error: disk full", result.Entries[0].Message)
	}
}

func TestSystemControllerSystemdLogTailPriorityEmptyPriority(t *testing.T) {
	sd := systemd.InitMockManager()
	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "no priority", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c2", Message: "has priority", Priority: "3", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c3", Message: "high priority", Priority: "7", RealtimeTimestamp: now},
	}
	c := initSystemControllerSystemdTest(t, sd)

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Priority: 3})
	if err != nil {
		t.Fatalf("LogTail with priority: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries (empty + pri 3), got %d", len(result.Entries))
	}
}

func TestSystemControllerSystemdFullLifecycle(t *testing.T) {
	sd := systemd.InitMockManager()
	c := initSystemControllerSystemdTest(t, sd,
		packages.PackageIdentity{Repo: "repo", Name: "nginx", Version: "1.0"},
	)

	// Start with no units.
	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits (initial): %v", err)
	}
	if len(units.Entries) != 0 {
		t.Fatalf("expected 0 units initially, got %d", len(units.Entries))
	}

	// Populate units.
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", LoadState: "loaded", ActiveState: "inactive", SubState: "dead"},
	}

	units, err = c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits (after populate): %v", err)
	}
	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units.Entries))
	}
	if units.Entries[0].ActiveState != "inactive" {
		t.Fatalf("expected inactive, got %q", units.Entries[0].ActiveState)
	}

	// Start.
	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-nginx-1.0.service", systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Populate log entries and replay.
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	sd.Entries = []systemd.JournalEntry{
		{Message: "Starting nginx...", RealtimeTimestamp: ts, SystemdUnit: "town-os-package--repo-nginx-1.0.service", Priority: "6"},
		{Message: "Started nginx.", RealtimeTimestamp: ts.Add(time.Second), SystemdUnit: "town-os-package--repo-nginx-1.0.service", Priority: "6"},
	}

	ch, err := c.LogReplay(context.TODO(), "town-os-package--repo-nginx-1.0.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	var entries []systemd.JournalEntry
	for e := range ch {
		entries = append(entries, e)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}
	if entries[0].Message != "Starting nginx..." {
		t.Fatalf("expected first message %q, got %q", "Starting nginx...", entries[0].Message)
	}

	// Stop.
	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-nginx-1.0.service", systemd.Stop); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Verify full call log.
	calls := sd.GetCalls()
	if len(calls) != 5 {
		t.Fatalf("expected 5 calls, got %d", len(calls))
	}

	expected := []string{"ListUnits", "ListUnits", "SetStatus", "LogReplay", "SetStatus"}
	for i, want := range expected {
		if calls[i].Method != want {
			t.Fatalf("call %d: expected method %q, got %q", i, want, calls[i].Method)
		}
	}
}

func TestSystemControllerSystemdListUnitsCallLog(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service"},
	}
	c := initSystemControllerSystemdTest(t, sd)

	if _, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{}); err != nil {
		t.Fatalf("ListUnits: %v", err)
	}
	if _, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{}); err != nil {
		t.Fatalf("ListUnits (second): %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	for _, call := range calls {
		if call.Method != "ListUnits" {
			t.Fatalf("expected ListUnits call, got %q", call.Method)
		}
	}
}

func TestSystemControllerSystemdSetUnitStatusCallLog(t *testing.T) {
	sd := systemd.InitMockManager()
	c := initSystemControllerSystemdTest(t, sd)

	if err := c.SetUnitStatus(context.TODO(), "nginx.service", systemd.Restart); err != nil {
		t.Fatalf("SetUnitStatus: %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "SetStatus" {
		t.Fatalf("expected SetStatus, got %q", calls[0].Method)
	}
	if len(calls[0].Args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(calls[0].Args))
	}
	unitStr, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if unitStr != "nginx.service" {
		t.Fatalf("expected unit %q, got %v", "nginx.service", calls[0].Args[0])
	}
	action, ok := calls[0].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if action != systemd.Restart {
		t.Fatalf("expected action %q, got %v", systemd.Restart, calls[0].Args[1])
	}
}

func TestSystemControllerSystemdSetUnitStatusMultipleUnits(t *testing.T) {
	sd := systemd.InitMockManager()
	c := initSystemControllerSystemdTest(t, sd)

	if err := c.SetUnitStatus(context.TODO(), "nginx.service", systemd.Start); err != nil {
		t.Fatalf("Start nginx: %v", err)
	}
	if err := c.SetUnitStatus(context.TODO(), "redis.service", systemd.Stop); err != nil {
		t.Fatalf("Stop redis: %v", err)
	}
	if err := c.SetUnitStatus(context.TODO(), "postgres.service", systemd.Restart); err != nil {
		t.Fatalf("Restart postgres: %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}

	type expectedCall struct {
		unit   string
		action systemd.StatusAction
	}
	want := []expectedCall{
		{"nginx.service", systemd.Start},
		{"redis.service", systemd.Stop},
		{"postgres.service", systemd.Restart},
	}
	for i, w := range want {
		unitName, ok := calls[i].Args[0].(string)
		if !ok {
			t.Fatal("type assertion failed")
		}
		if unitName != w.unit {
			t.Fatalf("call %d: expected unit %q, got %v", i, w.unit, calls[i].Args[0])
		}
		actionVal, ok := calls[i].Args[1].(systemd.StatusAction)
		if !ok {
			t.Fatal("type assertion failed")
		}
		if actionVal != w.action {
			t.Fatalf("call %d: expected action %q, got %v", i, w.action, calls[i].Args[1])
		}
	}
}

func TestSystemControllerPingUnitCountsFiltersTownOS(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--core-nginx-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--core-redis-7.0.service", ActiveState: "active"},
		{Name: "town-os-package--extras-postgres-16.0.service", ActiveState: "failed"},
		{Name: "sshd.service", ActiveState: "active"},
		{Name: "systemd-journald.service", ActiveState: "active"},
	}
	c := initSystemControllerSystemdTest(t, sd,
		packages.PackageIdentity{Repo: "core", Name: "nginx", Version: "1.0"},
		packages.PackageIdentity{Repo: "core", Name: "redis", Version: "7.0"},
		packages.PackageIdentity{Repo: "extras", Name: "postgres", Version: "16.0"},
	)

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if ping.Units == nil {
		t.Fatal("expected units in ping response")
	}

	if ping.Units.Total != 3 {
		t.Fatalf("expected 3 total town-os units, got %d", ping.Units.Total)
	}

	if ping.Units.Active != 2 {
		t.Fatalf("expected 2 active town-os units, got %d", ping.Units.Active)
	}

	if ping.Units.Failed != 1 {
		t.Fatalf("expected 1 failed town-os unit, got %d", ping.Units.Failed)
	}
}

func TestSystemControllerListUnitsFiltersNonPackageUnits(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-nginx-1.0-network.service", ActiveState: "active"},
		{Name: "town-os-package--repo-nginx-1.0-8080-tcp.socket", ActiveState: "active"},
		{Name: "sshd.service", ActiveState: "active"},
		{Name: "systemd-journald.service", ActiveState: "active"},
		{Name: "town-os-systemcontroller.service", ActiveState: "active"},
	}
	c := initSystemControllerSystemdTest(t, sd,
		packages.PackageIdentity{Repo: "repo", Name: "nginx", Version: "1.0"},
	)

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit (only main package service), got %d", len(units.Entries))
	}
	if units.Entries[0].Name != "town-os-package--repo-nginx-1.0.service" {
		t.Fatalf("expected town-os-package--repo-nginx-1.0.service, got %s", units.Entries[0].Name)
	}
}

func TestSystemControllerListUnitsDescriptionEnrichment(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-redis-7.0.service", ActiveState: "active"},
	}

	btr := storage.InitBtrFS("/data/btrfs")
	inst := packages.InitMockInstallManager()
	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "nginx", Version: "1.0"},
		{Repo: "repo", Name: "redis", Version: "7.0"},
	}

	dir := t.TempDir()
	rr := &packages.RepositoryRoot{
		BaseDir: dir,
		Items:   []packages.Repository{{Name: "repo"}},
		Git:     &git.GoGitClient{Home: dir},
	}

	// Write package files with descriptions.
	pkgDir := filepath.Join(dir, "repo", packages.PackagesDir, "nginx")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte("image: nginx:1.0\ndescription: A fast web server\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	redisPkgDir := filepath.Join(dir, "repo", packages.PackagesDir, "redis")
	if err := os.MkdirAll(redisPkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(redisPkgDir, "7.0.yaml"), []byte("image: redis:7.0\ndescription: In-memory data store\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        btr,
		Installer:      inst,
		Systemd:        sd,
		RepositoryRoot: rr,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units.Entries))
	}

	descMap := map[string]string{}
	for _, e := range units.Entries {
		descMap[e.PackageIdentifier] = e.PackageDescription
	}

	if descMap["repo/nginx@1.0"] != "A fast web server" {
		t.Fatalf("expected nginx description %q, got %q", "A fast web server", descMap["repo/nginx@1.0"])
	}
	if descMap["repo/redis@7.0"] != "In-memory data store" {
		t.Fatalf("expected redis description %q, got %q", "In-memory data store", descMap["repo/redis@7.0"])
	}
}

func TestSystemControllerListUnitsNoDescriptionWithoutRepoRoot(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
	}
	c := initSystemControllerSystemdTest(t, sd,
		packages.PackageIdentity{Repo: "repo", Name: "nginx", Version: "1.0"},
	)

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units.Entries))
	}
	if units.Entries[0].PackageDescription != "" {
		t.Fatalf("expected empty description without repo root, got %q", units.Entries[0].PackageDescription)
	}
}

func TestSystemControllerPingUnitCountsExcludesUninstalled(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--core-nginx-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--core-redis-7.0.service", ActiveState: "active"},
		{Name: "town-os-package--extras-postgres-16.0.service", ActiveState: "failed"},
	}
	// Only nginx is installed; redis and postgres units are leftover.
	c := initSystemControllerSystemdTest(t, sd,
		packages.PackageIdentity{Repo: "core", Name: "nginx", Version: "1.0"},
	)

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if ping.Units == nil {
		t.Fatal("expected units in ping response")
	}

	if ping.Units.Total != 1 {
		t.Fatalf("expected 1 total unit (only installed), got %d", ping.Units.Total)
	}

	if ping.Units.Active != 1 {
		t.Fatalf("expected 1 active unit, got %d", ping.Units.Active)
	}

	if ping.Units.Failed != 0 {
		t.Fatalf("expected 0 failed units, got %d", ping.Units.Failed)
	}
}

// --- Subvolume and quota integration tests ---

func TestSystemControllerCreateWithQuota(t *testing.T) {
	c := initSystemControllerTest(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-quota", Quota: 1048576}); err != nil {
		t.Fatalf("CreateFilesystem with quota: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-quota"); err != nil {
			t.Errorf("cleanup RemoveFilesystem: %v", err)
		}
	})

	listResult, err := c.ListFilesystems(context.TODO(), "sc-quota", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}
	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(listResult.Entries))
	}
	if listResult.Entries[0].Quota != 1048576 {
		t.Fatalf("expected quota %d, got %d", 1048576, listResult.Entries[0].Quota)
	}
}

func TestSystemControllerModifyQuota(t *testing.T) {
	c := initSystemControllerTest(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-modq"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-modq"); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})

	// Set quota.
	if err := c.ModifyFilesystem(context.TODO(), "sc-modq", storage.Filesystem{Name: "sc-modq", Quota: 2097152}); err != nil {
		t.Fatalf("ModifyFilesystem set quota: %v", err)
	}

	listResult, err := c.ListFilesystems(context.TODO(), "sc-modq", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems after set: %v", err)
	}
	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(listResult.Entries))
	}
	if listResult.Entries[0].Quota != 2097152 {
		t.Fatalf("expected quota %d, got %d", 2097152, listResult.Entries[0].Quota)
	}

	// Clear quota.
	if err := c.ModifyFilesystem(context.TODO(), "sc-modq", storage.Filesystem{Name: "sc-modq", Quota: 0}); err != nil {
		t.Fatalf("ModifyFilesystem clear quota: %v", err)
	}

	listResult, err = c.ListFilesystems(context.TODO(), "sc-modq", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems after clear: %v", err)
	}
	if listResult.Entries[0].Quota != 0 {
		t.Fatalf("expected quota 0 after clear, got %d", listResult.Entries[0].Quota)
	}
}

func TestSystemControllerNestedSubvolumes(t *testing.T) {
	c := initSystemControllerTest(t)

	names := []string{"sc-nest/parent", "sc-nest/parent/child", "sc-nest/parent/child/deep"}
	for _, name := range names {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}
	// Clean up deepest first, including the auto-created intermediate.
	allNames := append([]string{"sc-nest"}, names...)
	t.Cleanup(func() {
		for i := len(allNames) - 1; i >= 0; i-- {
			if err := c.RemoveFilesystem(context.TODO(), allNames[i]); err != nil {
				t.Errorf("cleanup RemoveFilesystem(%q): %v", allNames[i], err)
			}
		}
	})

	// All three should appear when listing with the parent prefix.
	listResult, err := c.ListFilesystems(context.TODO(), "sc-nest/", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems with parent prefix: %v", err)
	}
	if len(listResult.Entries) != 3 {
		t.Fatalf("expected 3 filesystems under sc-nest/, got %d", len(listResult.Entries))
	}

	// Exact match should return only the leaf.
	listResult, err = c.ListFilesystems(context.TODO(), "sc-nest/parent/child/deep", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems exact: %v", err)
	}
	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem for exact match, got %d", len(listResult.Entries))
	}
	if listResult.Entries[0].Name != "sc-nest/parent/child/deep" {
		t.Fatalf("expected %q, got %q", "sc-nest/parent/child/deep", listResult.Entries[0].Name)
	}
}

func TestSystemControllerNestedSubvolumeQuotaOnLeaf(t *testing.T) {
	c := initSystemControllerTest(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-leafq/parent"}); err != nil {
		t.Fatalf("CreateFilesystem parent: %v", err)
	}
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-leafq/parent/child", Quota: 4194304}); err != nil {
		t.Fatalf("CreateFilesystem child with quota: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-leafq/parent/child"); err != nil {
			t.Errorf("cleanup child: %v", err)
		}
		if err := c.RemoveFilesystem(context.TODO(), "sc-leafq/parent"); err != nil {
			t.Errorf("cleanup parent: %v", err)
		}
		if err := c.RemoveFilesystem(context.TODO(), "sc-leafq"); err != nil {
			t.Errorf("cleanup sc-leafq: %v", err)
		}
	})

	// Child should have its quota.
	listResult, err := c.ListFilesystems(context.TODO(), "sc-leafq/parent/child", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems child: %v", err)
	}
	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1, got %d", len(listResult.Entries))
	}
	if listResult.Entries[0].Quota != 4194304 {
		t.Fatalf("expected child quota %d, got %d", 4194304, listResult.Entries[0].Quota)
	}

	// Parent should have no quota.
	listResult, err = c.ListFilesystems(context.TODO(), "sc-leafq/parent", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems parent: %v", err)
	}
	for _, fs := range listResult.Entries {
		if fs.Name == "sc-leafq/parent" && fs.Quota != 0 {
			t.Fatalf("expected parent quota 0, got %d", fs.Quota)
		}
	}
}

func TestSystemControllerQuotaUpdatePreservesName(t *testing.T) {
	c := initSystemControllerTest(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-qname", Quota: 1024}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-qname"); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})

	// Modify quota without changing name.
	if err := c.ModifyFilesystem(context.TODO(), "sc-qname", storage.Filesystem{Name: "sc-qname", Quota: 8192}); err != nil {
		t.Fatalf("ModifyFilesystem: %v", err)
	}

	listResult, err := c.ListFilesystems(context.TODO(), "sc-qname", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}
	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(listResult.Entries))
	}
	if listResult.Entries[0].Name != "sc-qname" {
		t.Fatalf("expected name %q, got %q", "sc-qname", listResult.Entries[0].Name)
	}
	if listResult.Entries[0].Quota != 8192 {
		t.Fatalf("expected quota %d, got %d", 8192, listResult.Entries[0].Quota)
	}
}

func TestSystemControllerCreateMultipleNestedWithQuotas(t *testing.T) {
	c := initSystemControllerTest(t)

	type fs struct {
		name  string
		quota uint64
	}
	filesystems := []fs{
		{"sc-mnq/data", 0},
		{"sc-mnq/data/logs", 1048576},
		{"sc-mnq/data/cache", 2097152},
		{"sc-mnq/data/media", 4194304},
	}

	for _, f := range filesystems {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: f.name, Quota: f.quota}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", f.name, err)
		}
	}
	// Clean up deepest first, including the auto-created intermediate.
	t.Cleanup(func() {
		for i := len(filesystems) - 1; i >= 0; i-- {
			if err := c.RemoveFilesystem(context.TODO(), filesystems[i].name); err != nil {
				t.Errorf("cleanup %q: %v", filesystems[i].name, err)
			}
		}
		if err := c.RemoveFilesystem(context.TODO(), "sc-mnq"); err != nil {
			t.Errorf("cleanup sc-mnq: %v", err)
		}
	})

	// List all children.
	listResult, err := c.ListFilesystems(context.TODO(), "sc-mnq/data", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}
	if len(listResult.Entries) != 4 {
		t.Fatalf("expected 4 filesystems, got %d", len(listResult.Entries))
	}

	// Verify individual quotas.
	quotaMap := map[string]uint64{}
	for _, f := range listResult.Entries {
		quotaMap[f.Name] = f.Quota
	}
	for _, want := range filesystems {
		got, ok := quotaMap[want.name]
		if !ok {
			t.Fatalf("missing filesystem %q in list", want.name)
		}
		if got != want.quota {
			t.Fatalf("filesystem %q: expected quota %d, got %d", want.name, want.quota, got)
		}
	}
}

// --- Purge volume integration tests ---

func TestSystemControllerPurgeVolumes(t *testing.T) {
	c, btr := initSystemControllerTestWithStorage(t)

	// Simulate package "test-repo/sc-purge" with two child volumes via direct storage.
	children := []string{"installed/test-repo/sc-purge/data", "installed/test-repo/sc-purge/logs"}
	for _, name := range children {
		if err := btr.CreateFilesystem(storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	// Safety net: if purge fails, clean up manually.
	t.Cleanup(func() {
		for i := len(children) - 1; i >= 0; i-- {
			_ = btr.RemoveFilesystem(children[i])
		}
		_ = btr.RemoveFilesystem("installed/test-repo/sc-purge")
	})

	// Verify children exist via direct storage.
	list, err := btr.ListFilesystems("installed/test-repo/sc-purge/")
	if err != nil {
		t.Fatalf("ListFilesystems before purge: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 filesystems before purge, got %d", len(list))
	}

	// Purge all volumes for "test-repo/sc-purge".
	if err := c.PurgeVolumes(context.TODO(), "test-repo", "sc-purge"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	// Verify all children are gone.
	list, err = btr.ListFilesystems("installed/test-repo/sc-purge/")
	if err != nil {
		t.Fatalf("ListFilesystems after purge: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 filesystems after purge, got %d", len(list))
	}

	// Verify the parent intermediate is also gone.
	list, err = btr.ListFilesystems("installed/test-repo/sc-purge")
	if err != nil {
		t.Fatalf("ListFilesystems parent after purge: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected parent to be purged, got %d filesystems", len(list))
	}
}

func TestSystemControllerPurgeVolumesSimilarPrefix(t *testing.T) {
	c, btr := initSystemControllerTestWithStorage(t)

	// Create two packages with similar prefixes via direct storage.
	if err := btr.CreateFilesystem(storage.Filesystem{Name: "installed/test-repo/sc-pfx/data"}); err != nil {
		t.Fatalf("CreateFilesystem test-repo/sc-pfx/data: %v", err)
	}
	if err := btr.CreateFilesystem(storage.Filesystem{Name: "installed/test-repo/sc-pfx2/data"}); err != nil {
		t.Fatalf("CreateFilesystem test-repo/sc-pfx2/data: %v", err)
	}
	t.Cleanup(func() {
		_ = btr.RemoveFilesystem("installed/test-repo/sc-pfx/data")
		_ = btr.RemoveFilesystem("installed/test-repo/sc-pfx")
		_ = btr.RemoveFilesystem("installed/test-repo/sc-pfx2/data")
		_ = btr.RemoveFilesystem("installed/test-repo/sc-pfx2")
	})

	// Purge "sc-pfx" only.
	if err := c.PurgeVolumes(context.TODO(), "test-repo", "sc-pfx"); err != nil {
		t.Fatalf("PurgeVolumes sc-pfx: %v", err)
	}

	// sc-pfx should be gone.
	list, err := btr.ListFilesystems("installed/test-repo/sc-pfx/")
	if err != nil {
		t.Fatalf("ListFilesystems installed/test-repo/sc-pfx/: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 filesystems for installed/test-repo/sc-pfx/, got %d", len(list))
	}

	// sc-pfx2 should survive.
	list, err = btr.ListFilesystems("installed/test-repo/sc-pfx2/")
	if err != nil {
		t.Fatalf("ListFilesystems installed/test-repo/sc-pfx2/: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 filesystem for installed/test-repo/sc-pfx2/, got %d", len(list))
	}
}

func TestSystemControllerPurgeVolumesDeepNesting(t *testing.T) {
	c, btr := initSystemControllerTestWithStorage(t)

	names := []string{
		"installed/test-repo/sc-dpurge/a",
		"installed/test-repo/sc-dpurge/a/b",
		"installed/test-repo/sc-dpurge/a/b/c",
		"installed/test-repo/sc-dpurge/a/b/c/d",
	}
	for _, name := range names {
		if err := btr.CreateFilesystem(storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}
	t.Cleanup(func() {
		for i := len(names) - 1; i >= 0; i-- {
			_ = btr.RemoveFilesystem(names[i])
		}
		_ = btr.RemoveFilesystem("installed/test-repo/sc-dpurge")
	})

	if err := c.PurgeVolumes(context.TODO(), "test-repo", "sc-dpurge"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	list, err := btr.ListFilesystems("installed/test-repo/sc-dpurge")
	if err != nil {
		t.Fatalf("ListFilesystems after purge: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 filesystems after purge, got %d", len(list))
	}
}

func TestSystemControllerPurgeVolumesNonexistent(t *testing.T) {
	c := initSystemControllerTest(t)

	// Create a filesystem to verify it's not affected.
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-surv"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		_ = c.RemoveFilesystem(context.TODO(), "sc-surv")
	})

	// Purge a package that doesn't exist — should succeed.
	if err := c.PurgeVolumes(context.TODO(), "test-repo", "sc-nonexistent"); err != nil {
		t.Fatalf("PurgeVolumes nonexistent: %v", err)
	}

	// The unrelated filesystem should still exist.
	listResult, err := c.ListFilesystems(context.TODO(), "sc-surv", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}
	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem to survive, got %d", len(listResult.Entries))
	}
}

func TestSystemControllerPurgeVolumesWithQuotas(t *testing.T) {
	c, btr := initSystemControllerTestWithStorage(t)

	children := []storage.Filesystem{
		{Name: "installed/test-repo/sc-pq/data", Quota: 1048576},
		{Name: "installed/test-repo/sc-pq/logs", Quota: 2097152},
	}
	for _, f := range children {
		if err := btr.CreateFilesystem(f); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", f.Name, err)
		}
	}
	t.Cleanup(func() {
		for i := len(children) - 1; i >= 0; i-- {
			_ = btr.RemoveFilesystem(children[i].Name)
		}
		_ = btr.RemoveFilesystem("installed/test-repo/sc-pq")
	})

	// Verify quotas exist before purge via direct storage.
	list, err := btr.ListFilesystems("installed/test-repo/sc-pq/")
	if err != nil {
		t.Fatalf("ListFilesystems before purge: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
	for _, fs := range list {
		if fs.Quota == 0 {
			t.Fatalf("expected non-zero quota for %q before purge", fs.Name)
		}
	}

	if err := c.PurgeVolumes(context.TODO(), "test-repo", "sc-pq"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	list, err = btr.ListFilesystems("installed/test-repo/sc-pq")
	if err != nil {
		t.Fatalf("ListFilesystems after purge: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 after purge, got %d", len(list))
	}
}

func TestSystemControllerPurgeVolumesMultipleChildren(t *testing.T) {
	c, btr := initSystemControllerTestWithStorage(t)

	names := []string{
		"installed/test-repo/sc-pmulti/alpha",
		"installed/test-repo/sc-pmulti/bravo",
		"installed/test-repo/sc-pmulti/charlie",
		"installed/test-repo/sc-pmulti/delta",
		"installed/test-repo/sc-pmulti/echo",
	}
	for _, name := range names {
		if err := btr.CreateFilesystem(storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}
	t.Cleanup(func() {
		for i := len(names) - 1; i >= 0; i-- {
			_ = btr.RemoveFilesystem(names[i])
		}
		_ = btr.RemoveFilesystem("installed/test-repo/sc-pmulti")
	})

	if err := c.PurgeVolumes(context.TODO(), "test-repo", "sc-pmulti"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	list, err := btr.ListFilesystems("installed/test-repo/sc-pmulti")
	if err != nil {
		t.Fatalf("ListFilesystems after purge: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 after purge, got %d", len(list))
	}
}

// --- Settings integration tests (admin and user) ---

func initSystemControllerSettingsTest(t *testing.T) *systemcontroller.SystemdClient {
	t.Helper()

	dir := t.TempDir()
	db, err := account.OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	auditMgr, err := account.InitAuditManager(db)
	if err != nil {
		t.Fatalf("InitAuditManager: %v", err)
	}

	settingsMgr, err := account.InitSettingsManager(db)
	if err != nil {
		t.Fatalf("InitSettingsManager: %v", err)
	}

	btr := storage.InitBtrFS("/data/btrfs")
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:     btr,
		AccountMgr:  mgr,
		SessionMgr:  sessMgr,
		AuditMgr:    auditMgr,
		SettingsMgr: settingsMgr,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Bootstrap: create admin account and authenticate.
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "adminpass")
	if err != nil {
		t.Fatalf("bootstrap Authenticate: %v", err)
	}
	c.Token = resp.Token

	return c
}

func TestSettingsDefaultsOnInit(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	// Defaults should be present without any explicit Set calls.
	val, err := c.GetSetting(context.TODO(), "default_quota")
	if err != nil {
		t.Fatalf("GetSetting default_quota: %v", err)
	}
	if val != account.DefaultSettings["default_quota"] {
		t.Fatalf("expected default %q, got %q", account.DefaultSettings["default_quota"], val)
	}

	settings, err := c.GetSettings(context.TODO())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	for k, want := range account.DefaultSettings {
		got, ok := settings[k]
		if !ok {
			t.Fatalf("expected default key %q in list", k)
		}
		if got != want {
			t.Fatalf("default %q: expected %q, got %q", k, want, got)
		}
	}
}

func TestSettingsAdminCanSetAndGet(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	if err := c.SetSetting(context.TODO(), "default_quota", "1073741824"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "default_quota")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "1073741824" {
		t.Fatalf("expected %q, got %q", "1073741824", val)
	}
}

func TestSettingsAdminCanSetHumanReadable(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	if err := c.SetSetting(context.TODO(), "default_quota", "500GB"); err != nil {
		t.Fatalf("SetSetting 500GB: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "default_quota")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "536870912000" {
		t.Fatalf("expected %q, got %q", "536870912000", val)
	}
}

func TestSettingsNonAdminRejected(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	// Create a non-admin user and switch to their token.
	if _, err := c.CreateAccount(context.TODO(), "user", "userpass", "user@test.com", "555-1111", "User", false); err != nil {
		t.Fatalf("CreateAccount user: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "user", "userpass")
	if err != nil {
		t.Fatalf("Authenticate user: %v", err)
	}
	c.Token = resp.Token

	// All settings endpoints should reject non-admin.
	if _, err := c.GetSettings(context.TODO()); err == nil {
		t.Fatal("expected error for non-admin GetSettings")
	}
	if _, err := c.GetSetting(context.TODO(), "default_quota"); err == nil {
		t.Fatal("expected error for non-admin GetSetting")
	}
	if err := c.SetSetting(context.TODO(), "default_quota", "0"); err == nil {
		t.Fatal("expected error for non-admin SetSetting")
	}
}

func TestSettingsAdminOverrideAndList(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	// Set a custom key.
	if err := c.SetSetting(context.TODO(), "motd", "hello world"); err != nil {
		t.Fatalf("SetSetting motd: %v", err)
	}

	// Override the default quota.
	if err := c.SetSetting(context.TODO(), "default_quota", "0"); err != nil {
		t.Fatalf("SetSetting default_quota: %v", err)
	}

	settings, err := c.GetSettings(context.TODO())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}

	if settings["motd"] != "hello world" {
		t.Fatalf("expected motd %q, got %q", "hello world", settings["motd"])
	}
	if settings["default_quota"] != "0" {
		t.Fatalf("expected default_quota %q, got %q", "0", settings["default_quota"])
	}
}

// --- Pages integration tests ---

func initSystemControllerPagesTest(t *testing.T) *systemcontroller.SystemdClient {
	t.Helper()

	dir := t.TempDir()
	db, err := account.OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	auditMgr, err := account.InitAuditManager(db)
	if err != nil {
		t.Fatalf("InitAuditManager: %v", err)
	}

	pagesMgr, err := account.InitPagesManager(db)
	if err != nil {
		t.Fatalf("InitPagesManager: %v", err)
	}

	mock := storage.InitBtrFSMock()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       mock,
		AccountMgr:    mgr,
		SessionMgr:    sessMgr,
		AuditMgr:      auditMgr,
		PagesMgr:      pagesMgr,
		BtrfsBasePath: dir,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Bootstrap: create admin account and authenticate.
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "adminpass")
	if err != nil {
		t.Fatalf("bootstrap Authenticate: %v", err)
	}
	c.Token = resp.Token

	return c
}

func TestPagesCreateAndList(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	// Empty list.
	pages, err := c.ListPages(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 0 {
		t.Fatalf("expected 0 pages, got %d", len(pages.Entries))
	}

	// Create a page.
	page, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if page.Name != "my-site" {
		t.Errorf("expected name %q, got %q", "my-site", page.Name)
	}
	if page.RepoURL != "https://github.com/user/site.git" {
		t.Errorf("expected repo_url %q, got %q", "https://github.com/user/site.git", page.RepoURL)
	}
	if page.Branch != "main" {
		t.Errorf("expected branch %q, got %q", "main", page.Branch)
	}
	if page.Domain != "site.example.com" {
		t.Errorf("expected domain %q, got %q", "site.example.com", page.Domain)
	}

	// List should now have one entry.
	pages, err = c.ListPages(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages.Entries))
	}
}

func TestPagesCreateDefaultDomain(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	page, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if page.Domain != "my-site" {
		t.Errorf("expected default domain %q, got %q", "my-site", page.Domain)
	}
}

func TestPagesCreateDuplicate(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	_, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/other.git", "main", "other.example.com")
	if err == nil {
		t.Fatal("expected error for duplicate page name")
	}
}

func TestPagesUpdate(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	newDomain := "new.example.com"
	updated, err := c.UpdatePage(context.TODO(), "my-site", account.PageSiteUpdate{Domain: &newDomain})
	if err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}
	if updated.Domain != newDomain {
		t.Errorf("expected domain %q, got %q", newDomain, updated.Domain)
	}
}

func TestPagesUpdateNotFound(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	newDomain := "new.example.com"
	_, err := c.UpdatePage(context.TODO(), "nonexistent", account.PageSiteUpdate{Domain: &newDomain})
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestPagesRemove(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if err := c.RemovePage(context.TODO(), "my-site"); err != nil {
		t.Fatalf("RemovePage: %v", err)
	}

	pages, err := c.ListPages(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 0 {
		t.Errorf("expected 0 pages after remove, got %d", len(pages.Entries))
	}
}

func TestPagesRemoveNotFound(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	err := c.RemovePage(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestPagesListSearch(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "alpha-site", "https://github.com/user/alpha.git", "main", "alpha.example.com"); err != nil {
		t.Fatalf("CreatePage alpha-site: %v", err)
	}
	if _, err := c.CreatePage(context.TODO(), "beta-site", "https://github.com/user/beta.git", "develop", "beta.example.com"); err != nil {
		t.Fatalf("CreatePage beta-site: %v", err)
	}

	pages, err := c.ListPages(context.TODO(), systemcontroller.ListParams{Search: "alpha"})
	if err != nil {
		t.Fatalf("ListPages with search: %v", err)
	}
	if len(pages.Entries) != 1 {
		t.Fatalf("expected 1 page matching search, got %d", len(pages.Entries))
	}
	if pages.Entries[0].Name != "alpha-site" {
		t.Errorf("expected %q, got %q", "alpha-site", pages.Entries[0].Name)
	}
}

func TestPagesRequireAuth(t *testing.T) {
	c := initSystemControllerPagesTest(t)
	c.Token = "" // Clear auth token.

	_, err := c.ListPages(context.TODO(), systemcontroller.ListParams{})
	if err == nil {
		t.Fatal("expected auth error for ListPages without token")
	}
}

// --- Reconciliation tests ---

// initReconcileTest creates a test server with all components wired up and
// returns them individually so tests can exercise Reconcile directly.
func initReconcileTest(t *testing.T) (
	*systemcontroller.SystemdClient,
	*packages.RepositoryRoot,
	packages.Installer,
	*systemd.MockManager,
	storage.Storage,
) {
	t.Helper()
	return initReconcileTestWithNetworkMode(t, "")
}

func initReconcileTestWithNetworkMode(t *testing.T, networkMode string) (
	*systemcontroller.SystemdClient,
	*packages.RepositoryRoot,
	packages.Installer,
	*systemd.MockManager,
	storage.Storage,
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

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		NetworkMode:    networkMode,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, rr, inst, sd, mock
}

func TestReconcileAfterInstall(t *testing.T) {
	c, rr, inst, sd, mock := initReconcileTest(t)

	// Add a repository and install a package via the API.
	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Verify the install created systemd units.
	// nginx 1.0 has 1 external port (8080->80) and 1 volume:
	//   InstallUnit(service) + InstallUnit(socket) + InstallUnit(networkcontroller) +
	//   Enable(socket) + Enable(networkcontroller) + Start(service) = 6
	installCalls := sd.GetCalls()
	if len(installCalls) != 6 {
		t.Fatalf("expected 6 systemd calls from install, got %d", len(installCalls))
	}

	// Simulate a container restart: clear the mock systemd state.
	// The installed state (symlinks + responses) persists but units are gone.
	sd.Calls = nil

	// Run reconciliation.
	err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify reconciliation re-created all units (same 6 calls).
	calls := sd.GetCalls()
	if len(calls) != 6 {
		t.Fatalf("expected 6 systemd calls from reconcile, got %d: %v", len(calls), calls)
	}

	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	unitName, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if unitName != "town-os-package--core-nginx-1.0.service" {
		t.Fatalf("expected unit town-os-package--core-nginx-1.0.service, got %s", unitName)
	}

	// Last call should be Start for the service.
	lastCall := calls[len(calls)-1]
	lastAction, ok := lastCall.Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if lastCall.Method != "SetStatus" || lastAction != systemd.Start {
		t.Fatalf("last call: expected SetStatus Start, got %s %v", lastCall.Method, lastCall.Args)
	}

	// Verify installed state is intact.
	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(pkgs.Entries) != 1 || pkgs.Entries[0] != "core/nginx@1.0" {
		t.Fatalf("expected [core/nginx@1.0], got %v", pkgs.Entries)
	}
}

func TestReconcileMultiplePackagesAfterInstall(t *testing.T) {
	c, rr, inst, sd, mock := initReconcileTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{"port": "6379", "password": "secret", "maxmemory": "100mb"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// Clear mock systemd calls (simulate restart).
	sd.Calls = nil

	err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// nginx (1 ext port): 3 InstallUnit + 2 Enable + 1 Start = 6
	// redis (1 int port): 2 InstallUnit + 1 Enable + 1 Start = 4
	// Total = 10
	if len(calls) != 10 {
		t.Fatalf("expected 10 systemd calls, got %d", len(calls))
	}
}

func TestReconcilePreservesResponses(t *testing.T) {
	c, rr, inst, sd, mock := initReconcileTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	responses := packages.Responses{"hostname": "myhost", "port": "9090"}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	sd.Calls = nil

	err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify responses are still accessible via the API.
	resp, err := c.GetResponses(context.TODO(), "core", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}
	if resp["hostname"] != "myhost" {
		t.Fatalf("expected hostname myhost, got %s", resp["hostname"])
	}
	if resp["port"] != "9090" {
		t.Fatalf("expected port 9090, got %s", resp["port"])
	}
}

// --- Command and NetworkMode integration tests ---

func initSystemControllerInstallSystemdTestWithNetworkMode(t *testing.T, networkMode string) (*systemcontroller.SystemdClient, *systemd.MockManager) {
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

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		NetworkMode:    networkMode,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, sd
}

func TestSystemControllerInstallRedisCommandInUnit(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTestWithNetworkMode(t, "")

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"port":      "6379",
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// Find the InstallUnit call for the service unit and inspect its content.
	calls := sd.GetCalls()
	var serviceContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-redis-7.0.service" {
				sc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				serviceContent = sc
				break
			}
		}
	}

	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-package--core-redis-7.0.service")
	}

	// Verify command args appear after the image name.
	if !strings.Contains(serviceContent, "redis-server") {
		t.Fatalf("service unit missing command arg 'redis-server', got:\n%s", serviceContent)
	}
	if !strings.Contains(serviceContent, "--bind") {
		t.Fatalf("service unit missing command arg '--bind', got:\n%s", serviceContent)
	}
	if !strings.Contains(serviceContent, "0.0.0.0") {
		t.Fatalf("service unit missing command arg '0.0.0.0', got:\n%s", serviceContent)
	}

	// Verify -p port mapping is present (default bridge mode).
	if !strings.Contains(serviceContent, "-p 6379:6379") {
		t.Fatalf("service unit missing '-p 6379:6379' in bridge mode, got:\n%s", serviceContent)
	}

	// Verify --net host is NOT present.
	if strings.Contains(serviceContent, "--net host") {
		t.Fatal("service unit should not have --net host in bridge mode")
	}
}

func TestSystemControllerInstallWithHostNetworkMode(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTestWithNetworkMode(t, "host")

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"port":      "6379",
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	calls := sd.GetCalls()

	// Find the service unit content.
	var serviceContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-redis-7.0.service" {
				sc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				serviceContent = sc
				break
			}
		}
	}

	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-package--core-redis-7.0.service")
	}

	// Verify --net host is present.
	if !strings.Contains(serviceContent, "--net host") {
		t.Fatalf("service unit missing '--net host' in host mode, got:\n%s", serviceContent)
	}

	// Verify -p is NOT present.
	if strings.Contains(serviceContent, "-p 6379:6379") {
		t.Fatal("service unit should not have -p mappings in host mode")
	}

	// Verify command args still appear.
	if !strings.Contains(serviceContent, "redis-server") {
		t.Fatalf("service unit missing command arg 'redis-server' in host mode, got:\n%s", serviceContent)
	}
	if !strings.Contains(serviceContent, "--bind") {
		t.Fatalf("service unit missing command arg '--bind' in host mode, got:\n%s", serviceContent)
	}

	// Redis 6379→6379 (same port, internal only): no network controller should be installed.
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if strings.Contains(name, "network") {
				t.Fatalf("unexpected network controller unit installed for internal-only port: %s", name)
			}
		}
	}
}

func TestSystemControllerInstallNginxBridgeMode(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTestWithNetworkMode(t, "")

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Find the service unit content.
	calls := sd.GetCalls()
	var serviceContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0.service" {
				sc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				serviceContent = sc
				break
			}
		}
	}

	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-package--core-nginx-1.0.service")
	}

	// Nginx has no command field, so command args should not appear after the image.
	// The image line should end the ExecStart (no extra args).
	if !strings.Contains(serviceContent, "-p 8080:80") {
		t.Fatalf("service unit missing '-p 8080:80', got:\n%s", serviceContent)
	}
	if strings.Contains(serviceContent, "--net host") {
		t.Fatal("service unit should not have --net host in bridge mode")
	}
}

func TestSystemControllerInstallNginxHostMode(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTestWithNetworkMode(t, "host")

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	calls := sd.GetCalls()

	// Find the service unit content.
	var serviceContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0.service" {
				sc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				serviceContent = sc
				break
			}
		}
	}

	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-package--core-nginx-1.0.service")
	}

	// Verify --net host is present and -p is not.
	if !strings.Contains(serviceContent, "--net host") {
		t.Fatalf("service unit missing '--net host' in host mode, got:\n%s", serviceContent)
	}
	if strings.Contains(serviceContent, "-p 8080:80") {
		t.Fatal("service unit should not have -p mappings in host mode")
	}

	// Verify networkcontroller unit was installed.
	var ncContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0-network.service" {
				nc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				ncContent = nc
				break
			}
		}
	}
	if ncContent == "" {
		t.Fatal("expected InstallUnit call for network controller unit town-os-package--core-nginx-1.0-network.service")
	}
	if !strings.Contains(ncContent, "BindsTo=town-os-package--core-nginx-1.0.service") {
		t.Fatalf("network controller missing BindsTo, got:\n%s", ncContent)
	}
	if !strings.Contains(ncContent, "After=town-os-package--core-nginx-1.0.service") {
		t.Fatalf("network controller missing After, got:\n%s", ncContent)
	}
}

func TestSystemControllerInstallNginxHostModeNetworkController(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTestWithNetworkMode(t, "host")

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	calls := sd.GetCalls()

	// Verify network controller unit was installed.
	var ncContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0-network.service" {
				nc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				ncContent = nc
				break
			}
		}
	}
	if ncContent == "" {
		t.Fatal("expected InstallUnit call for town-os-package--core-nginx-1.0-network.service")
	}
	if !strings.Contains(ncContent, "Description=Town OS Network Controller: core/nginx@1.0") {
		t.Fatalf("network controller missing description, got:\n%s", ncContent)
	}
	if !strings.Contains(ncContent, "BindsTo=town-os-package--core-nginx-1.0.service") {
		t.Fatalf("network controller missing BindsTo, got:\n%s", ncContent)
	}
	if !strings.Contains(ncContent, "After=town-os-package--core-nginx-1.0.service") {
		t.Fatalf("network controller missing After, got:\n%s", ncContent)
	}

	// Verify the network controller was enabled.
	ncEnabled := false
	for _, call := range calls {
		if call.Method == "SetStatus" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			action, ok := call.Args[1].(systemd.StatusAction)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0-network.service" && action == systemd.Enable {
				ncEnabled = true
				break
			}
		}
	}
	if !ncEnabled {
		t.Fatal("expected network controller unit to be enabled")
	}

	// Verify the service unit has Wants= for the network controller.
	var serviceContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0.service" {
				sc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				serviceContent = sc
				break
			}
		}
	}
	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-package--core-nginx-1.0.service")
	}
	if !strings.Contains(serviceContent, "Wants=town-os-package--core-nginx-1.0-network.service") {
		t.Fatalf("service unit missing Wants for network controller, got:\n%s", serviceContent)
	}
}

func TestReconcileWithNetworkMode(t *testing.T) {
	c, rr, inst, sd, mock := initReconcileTestWithNetworkMode(t, "host")

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"port":      "6379",
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// Clear mock systemd calls (simulate restart).
	sd.Calls = nil

	err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		NetworkMode:    "host",
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Find the reconciled service unit content.
	calls := sd.GetCalls()
	var serviceContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-redis-7.0.service" {
				sc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				serviceContent = sc
				break
			}
		}
	}

	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-package--core-redis-7.0.service after reconcile")
	}

	// Verify --net host and command args after reconcile.
	if !strings.Contains(serviceContent, "--net host") {
		t.Fatalf("reconciled unit missing '--net host', got:\n%s", serviceContent)
	}
	if strings.Contains(serviceContent, "-p 6379:6379") {
		t.Fatal("reconciled unit should not have -p in host mode")
	}
	if !strings.Contains(serviceContent, "redis-server") {
		t.Fatalf("reconciled unit missing command arg 'redis-server', got:\n%s", serviceContent)
	}

	// Redis 6379→6379 (same port, internal only): no network controller should be installed after reconcile.
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if strings.Contains(name, "network") {
				t.Fatalf("unexpected network controller unit installed after reconcile for internal-only port: %s", name)
			}
		}
	}
}

func TestReconcileWithNetworkModeNginxNetworkController(t *testing.T) {
	c, rr, inst, sd, mock := initReconcileTestWithNetworkMode(t, "host")

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Clear mock systemd calls (simulate restart).
	sd.Calls = nil

	err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		NetworkMode:    "host",
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()

	// Verify network controller unit was installed during reconcile.
	var ncContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0-network.service" {
				nc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				ncContent = nc
				break
			}
		}
	}
	if ncContent == "" {
		t.Fatal("expected InstallUnit call for network controller unit after reconcile")
	}
	if !strings.Contains(ncContent, "BindsTo=town-os-package--core-nginx-1.0.service") {
		t.Fatalf("network controller missing BindsTo after reconcile, got:\n%s", ncContent)
	}

	// Verify network controller was enabled.
	ncEnabled := false
	for _, call := range calls {
		if call.Method == "SetStatus" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			action, ok := call.Args[1].(systemd.StatusAction)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0-network.service" && action == systemd.Enable {
				ncEnabled = true
				break
			}
		}
	}
	if !ncEnabled {
		t.Fatal("expected network controller unit to be enabled after reconcile")
	}
}

// --- Internal port forwarding integration tests ---

func initSystemControllerInstallSystemdTestWithNetworkState(t *testing.T, networkMode string) (*systemcontroller.SystemdClient, *systemd.MockManager, string) {
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

	netStateDir := filepath.Join(dir, "network-state")
	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:                  mock,
		RepositoryRoot:           rr,
		Installer:                inst,
		Systemd:                  sd,
		NetworkMode:              networkMode,
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         netStateDir,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, sd, netStateDir
}

func TestSystemControllerInstallNginxHostModeNetworkState(t *testing.T) {
	c, _, netStateDir := initSystemControllerInstallSystemdTestWithNetworkState(t, "host")

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Verify the network state file was written.
	statePath := filepath.Join(netStateDir, "core-nginx-1.0.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read network state file: %v", err)
	}

	var state networkcontroller.PackageNetworkState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal network state: %v", err)
	}

	if state.NetworkMode != "host" {
		t.Fatalf("expected network_mode host, got %s", state.NetworkMode)
	}

	// Nginx has external 8080→80: should have Forward=true, UPnP=true.
	if len(state.Ports) != 1 {
		t.Fatalf("expected 1 port in state, got %d", len(state.Ports))
	}
	if state.Ports[0].ExternalPort != 8080 {
		t.Fatalf("expected external_port 8080, got %d", state.Ports[0].ExternalPort)
	}
	if state.Ports[0].InternalPort != 80 {
		t.Fatalf("expected internal_port 80, got %d", state.Ports[0].InternalPort)
	}
	if !state.Ports[0].UPnP {
		t.Fatal("expected UPnP=true for external port")
	}
	if !state.Ports[0].Forward {
		t.Fatal("expected Forward=true for external port with host!=container in host mode")
	}
}

func TestSystemControllerInstallRedisHostModeNoNCForSamePort(t *testing.T) {
	c, sd, netStateDir := initSystemControllerInstallSystemdTestWithNetworkState(t, "host")

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"port":      "6379",
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// Redis 6379→6379 (same port, internal only): no network controller should be installed.
	calls := sd.GetCalls()
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if strings.Contains(name, "network") {
				t.Fatalf("unexpected network controller unit installed for same-port internal mapping: %s", name)
			}
		}
	}

	// No network state file should be written (no external ports, internal same-port).
	statePath := filepath.Join(netStateDir, "core-redis-7.0.json")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("expected no network state file for same-port internal mapping, but file exists")
	}
}

func TestSystemControllerInstallNginxBridgeModeNoInternalPortForwarding(t *testing.T) {
	c, sd, _ := initSystemControllerInstallSystemdTestWithNetworkState(t, "")

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"port":      "6379",
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// Bridge mode: no network controller for internal-only ports.
	calls := sd.GetCalls()
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if strings.Contains(name, "network") {
				t.Fatalf("unexpected network controller unit installed in bridge mode: %s", name)
			}
		}
	}
}

func TestReconcileNginxHostModeNetworkState(t *testing.T) {
	c, rr, inst, sd, mock := initReconcileTestWithNetworkMode(t, "host")

	netStateDir := t.TempDir()

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Clear mock systemd calls (simulate restart).
	sd.Calls = nil

	err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:                inst,
		RepositoryRoot:           rr,
		Storage:                  mock,
		Systemd:                  sd,
		NetworkMode:              "host",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         netStateDir,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify network state file was written during reconcile.
	statePath := filepath.Join(netStateDir, "core-nginx-1.0.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read network state file after reconcile: %v", err)
	}

	var state networkcontroller.PackageNetworkState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal network state: %v", err)
	}

	if state.NetworkMode != "host" {
		t.Fatalf("expected network_mode host, got %s", state.NetworkMode)
	}
	if len(state.Ports) != 1 {
		t.Fatalf("expected 1 port after reconcile, got %d", len(state.Ports))
	}
	if state.Ports[0].ExternalPort != 8080 || state.Ports[0].InternalPort != 80 {
		t.Fatalf("expected 8080->80, got %d->%d", state.Ports[0].ExternalPort, state.Ports[0].InternalPort)
	}

	// Verify NC unit was installed during reconcile.
	calls := sd.GetCalls()
	var ncInstalled bool
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0-network.service" {
				ncInstalled = true
				break
			}
		}
	}
	if !ncInstalled {
		t.Fatal("expected network controller unit to be installed during reconcile")
	}
}

// --- Bootstrap account creation integration tests ---

// initBootstrapTest creates a server with auth enabled but no accounts.
// Returns the client and account manager for further manipulation.
func initBootstrapTest(t *testing.T) (*systemcontroller.SystemdClient, *account.SQLiteManager) {
	t.Helper()

	dir := t.TempDir()
	db, err := account.OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	mock := storage.InitBtrFSMock()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:    mock,
		AccountMgr: mgr,
		SessionMgr: sessMgr,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, mgr
}

func TestBootstrapCreateAccountNoUsers(t *testing.T) {
	c, _ := initBootstrapTest(t)

	// No accounts exist — should succeed without any token.
	acct, err := c.CreateAccount(context.TODO(), "first", "password1", "f@test.com", "555-0001", "First Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount on empty DB: %v", err)
	}
	if acct.Username != "first" {
		t.Fatalf("expected username %q, got %q", "first", acct.Username)
	}
	if !acct.Admin {
		t.Fatal("expected admin=true for bootstrap account")
	}
}

func TestBootstrapCreateAccountWithStaleToken(t *testing.T) {
	c, _ := initBootstrapTest(t)

	// Set a stale/invalid token — bootstrap mode should ignore it.
	c.Token = "stale-garbage-token-from-previous-session"

	acct, err := c.CreateAccount(context.TODO(), "first", "password1", "f@test.com", "555-0001", "First Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount with stale token on empty DB: %v", err)
	}
	if acct.Username != "first" {
		t.Fatalf("expected username %q, got %q", "first", acct.Username)
	}
}

func TestBootstrapCreateAccountAllAdminsDisabled(t *testing.T) {
	c, mgr := initBootstrapTest(t)

	// Create an admin and authenticate.
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "adminpass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	c.Token = resp.Token

	// Disable the only admin.
	if err := mgr.Disable("admin"); err != nil {
		t.Fatalf("Disable admin: %v", err)
	}

	// Clear token — bootstrap mode should re-engage.
	c.Token = ""
	acct, err := c.CreateAccount(context.TODO(), "newadmin", "password1", "new@test.com", "555-0002", "New Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount after all admins disabled: %v", err)
	}
	if acct.Username != "newadmin" {
		t.Fatalf("expected username %q, got %q", "newadmin", acct.Username)
	}
}

func TestBootstrapCreateAccountAllAdminsDisabledWithStaleToken(t *testing.T) {
	c, mgr := initBootstrapTest(t)

	// Create an admin and authenticate.
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "adminpass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	c.Token = resp.Token

	// Disable the only admin.
	if err := mgr.Disable("admin"); err != nil {
		t.Fatalf("Disable admin: %v", err)
	}

	// Keep the (now-stale) token — bootstrap mode should ignore it.
	acct, err := c.CreateAccount(context.TODO(), "newadmin", "password1", "new@test.com", "555-0002", "New Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount with stale token after all admins disabled: %v", err)
	}
	if acct.Username != "newadmin" {
		t.Fatalf("expected username %q, got %q", "newadmin", acct.Username)
	}
}

func TestBootstrapCreateAccountOnlyNonAdminUsers(t *testing.T) {
	c, _ := initBootstrapTest(t)

	// Bootstrap first admin.
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "adminpass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	c.Token = resp.Token

	// Create a non-admin user.
	if _, err := c.CreateAccount(context.TODO(), "regular", "password1", "r@test.com", "555-0001", "Regular User", false); err != nil {
		t.Fatalf("CreateAccount regular: %v", err)
	}

	// Disable the only admin.
	if err := c.DisableAccount(context.TODO(), "admin"); err != nil {
		t.Fatalf("DisableAccount admin: %v", err)
	}

	// No enabled admins remain (only a non-admin) — bootstrap should allow create.
	c.Token = ""
	acct, err := c.CreateAccount(context.TODO(), "newadmin", "password1", "new@test.com", "555-0002", "New Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount with only non-admin users: %v", err)
	}
	if acct.Username != "newadmin" {
		t.Fatalf("expected username %q, got %q", "newadmin", acct.Username)
	}
}

func TestBootstrapRejectsAfterAdminExists(t *testing.T) {
	c, _ := initBootstrapTest(t)

	// Bootstrap first admin.
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}

	// Authenticate to create an active session.
	resp, err := c.Authenticate(context.TODO(), "admin", "adminpass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	_ = resp

	// Unauthenticated create should now be rejected (active session exists).
	c.Token = ""
	_, err = c.CreateAccount(context.TODO(), "intruder", "password1", "i@test.com", "555-9999", "Intruder", false)
	if err == nil {
		t.Fatal("expected error for unauthenticated create when enabled admin exists with active session")
	}
}

func TestBootstrapNoActiveAdminSessions(t *testing.T) {
	c, _ := initBootstrapTest(t)

	// Bootstrap first admin.
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}

	// Admin exists but was never authenticated — no active sessions.
	// Bootstrap should re-engage.
	c.Token = ""
	acct, err := c.CreateAccount(context.TODO(), "newadmin", "password1", "new@test.com", "555-0002", "New Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount with no active sessions: %v", err)
	}
	if acct.Username != "newadmin" {
		t.Fatalf("expected username %q, got %q", "newadmin", acct.Username)
	}
}

func TestBootstrapPingNeedsSetup(t *testing.T) {
	c, _ := initBootstrapTest(t)

	// No accounts — needs_setup should be true.
	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if !ping.NeedsSetup {
		t.Fatal("expected needs_setup=true with no accounts")
	}

	// Create admin (no auth needed on empty DB).
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}

	// Admin exists but no session — needs_setup should still be true.
	ping, err = c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if !ping.NeedsSetup {
		t.Fatal("expected needs_setup=true with admin but no active sessions")
	}

	// Authenticate to create a session.
	resp, err := c.Authenticate(context.TODO(), "admin", "adminpass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	c.Token = resp.Token

	// Now needs_setup should be false.
	ping, err = c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if ping.NeedsSetup {
		t.Fatal("expected needs_setup=false with active admin session")
	}
}

// --- Multi-repo integration tests ---

func TestSystemControllerMultiRepoListInstalled(t *testing.T) {
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	// Install nginx from core and mosquitto from extras.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "alpha", "port": "80"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}
	if err := c.InstallPackage(context.TODO(), "mosquitto", "2.0", packages.Responses{"port": "1883"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage mosquitto@2.0: %v", err)
	}

	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(pkgs.Entries) != 2 {
		t.Fatalf("expected 2 installed, got %d", len(pkgs.Entries))
	}

	found := map[string]bool{}
	for _, p := range pkgs.Entries {
		found[p] = true
	}
	if !found["core/nginx@1.0"] {
		t.Fatalf("expected core/nginx@1.0 in installed list, got %v", pkgs.Entries)
	}
	if !found["extras/mosquitto@2.0"] {
		t.Fatalf("expected extras/mosquitto@2.0 in installed list, got %v", pkgs.Entries)
	}
}

func TestSystemControllerListPackagesByRepoIntegration(t *testing.T) {
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	groups, err := c.ListPackagesByRepo(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListPackagesByRepo: %v", err)
	}

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	// Highest-precedence repo (extras, last added) comes first.
	if groups[0].Repo != "extras" {
		t.Fatalf("expected first group to be extras, got %s", groups[0].Repo)
	}
	if groups[1].Repo != "core" {
		t.Fatalf("expected second group to be core, got %s", groups[1].Repo)
	}

	// Extras has mosquitto and postgres.
	if len(groups[0].Packages) != 2 {
		t.Fatalf("expected 2 packages in extras, got %d", len(groups[0].Packages))
	}
	// Core has demo-nginx, nginx, redis.
	if len(groups[1].Packages) != 3 {
		t.Fatalf("expected 3 packages in core, got %d", len(groups[1].Packages))
	}
}

func TestSystemControllerMultiRepoUninstallIsolation(t *testing.T) {
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	// Install from both repos.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "alpha", "port": "80"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}
	if err := c.InstallPackage(context.TODO(), "mosquitto", "2.0", packages.Responses{"port": "1883"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage mosquitto@2.0: %v", err)
	}

	// Uninstall only core/nginx.
	if err := c.UninstallPackage(context.TODO(), "core", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage core/nginx: %v", err)
	}

	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after uninstall: %v", err)
	}
	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed after uninstall, got %d", len(pkgs.Entries))
	}
	if pkgs.Entries[0] != "extras/mosquitto@2.0" {
		t.Fatalf("expected extras/mosquitto@2.0, got %s", pkgs.Entries[0])
	}
}

// TestSystemControllerUpgradeRemovesOldRecord verifies that upgrading from
// one version to another removes the old install record, leaving only the
// new version installed.
func TestSystemControllerUpgradeRemovesOldRecord(t *testing.T) {
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install nginx 1.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "alpha", "port": "80"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after install 1.0: %v", err)
	}
	if len(pkgs.Entries) != 1 || pkgs.Entries[0] != "core/nginx@1.0" {
		t.Fatalf("expected [core/nginx@1.0], got %v", pkgs.Entries)
	}

	// Upgrade to nginx 2.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", packages.Responses{"hostname": "alpha", "port": "80"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@2.0 (upgrade): %v", err)
	}

	// Verify only 2.0 remains installed.
	pkgs, err = c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after upgrade: %v", err)
	}
	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed after upgrade, got %d: %v", len(pkgs.Entries), pkgs.Entries)
	}
	if pkgs.Entries[0] != "core/nginx@2.0" {
		t.Fatalf("expected core/nginx@2.0, got %s", pkgs.Entries[0])
	}

	// Verify responses are accessible for the new version.
	resp, err := c.GetResponses(context.TODO(), "core", "nginx", "2.0")
	if err != nil {
		t.Fatalf("GetResponses nginx@2.0: %v", err)
	}
	if resp["hostname"] != "alpha" {
		t.Fatalf("expected hostname %q, got %q", "alpha", resp["hostname"])
	}
}

func initSystemControllerTestWithBtrfsBase(t *testing.T) *systemcontroller.SystemdClient {
	t.Helper()
	btr := storage.InitBtrFS("/data/btrfs")
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       btr,
		BtrfsBasePath: "/data/btrfs",
	})
	t.Cleanup(func() { ts.Server.Close() })
	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}
	return c
}

// makeTarGz builds a tar.gz archive in memory from a map of filename -> content.
func makeTarGz(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar write header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("tar write body %s: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return &buf
}

// extractTarGz reads a tar.gz stream and returns a map of filename -> content.
func extractTarGz(t *testing.T, r io.Reader) map[string]string {
	t.Helper()
	gr, err := gzip.NewReader(r)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer func() {
		if err := gr.Close(); err != nil {
			t.Errorf("gzip close: %v", err)
		}
	}()

	result := make(map[string]string)
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("tar read %s: %v", hdr.Name, err)
		}
		result[hdr.Name] = string(data)
	}
	return result
}

func TestArchiveUploadAndDownload(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()
	subvol := "archive-test-upload"

	if err := c.CreateFilesystem(ctx, storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(ctx, subvol); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", subvol, err)
		}
	})

	// Build and upload a tar.gz containing hello.txt.
	archive := makeTarGz(t, map[string]string{"hello.txt": "hello world"})
	resp, err := c.UploadArchive(ctx, subvol, archive, "test.tar.gz", "", "")
	if err != nil {
		t.Fatalf("UploadArchive: %v", err)
	}
	if resp.Message == "" {
		t.Fatal("expected non-empty upload response message")
	}

	// Verify the file was unpacked on disk.
	got, err := os.ReadFile(filepath.Join("/data/btrfs", subvol, "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile hello.txt: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("expected %q, got %q", "hello world", string(got))
	}

	// Download the archive and verify contents.
	rc, err := c.DownloadArchive(ctx, subvol, nil, "", "", "")
	if err != nil {
		t.Fatalf("DownloadArchive: %v", err)
	}
	defer func() {
		if err := rc.Close(); err != nil {
			t.Errorf("close download body: %v", err)
		}
	}()

	files := extractTarGz(t, rc)
	content, ok := files["hello.txt"]
	if !ok {
		// tar may prefix with "./"
		content, ok = files["./hello.txt"]
	}
	if !ok {
		t.Fatalf("hello.txt not found in downloaded archive, got keys: %v", mapKeys(files))
	}
	if content != "hello world" {
		t.Fatalf("downloaded hello.txt: expected %q, got %q", "hello world", content)
	}
}

func TestArchiveDownloadWithPaths(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()
	subvol := "archive-test-paths"

	if err := c.CreateFilesystem(ctx, storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(ctx, subvol); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", subvol, err)
		}
	})

	// Upload archive with two files.
	archive := makeTarGz(t, map[string]string{
		"a.txt": "content-a",
		"b.txt": "content-b",
	})
	if _, err := c.UploadArchive(ctx, subvol, archive, "test.tar.gz", "", ""); err != nil {
		t.Fatalf("UploadArchive: %v", err)
	}

	// Download with only a.txt requested.
	rc, err := c.DownloadArchive(ctx, subvol, []string{"a.txt"}, "", "", "")
	if err != nil {
		t.Fatalf("DownloadArchive: %v", err)
	}
	defer func() {
		if err := rc.Close(); err != nil {
			t.Errorf("close download body: %v", err)
		}
	}()

	files := extractTarGz(t, rc)
	if _, ok := files["a.txt"]; !ok {
		t.Fatalf("expected a.txt in archive, got keys: %v", mapKeys(files))
	}
	if _, ok := files["b.txt"]; ok {
		t.Fatal("b.txt should not be in the filtered archive")
	}
}

func TestArchiveUploadUnsupportedFormat(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()
	subvol := "archive-test-unsupported"

	if err := c.CreateFilesystem(ctx, storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(ctx, subvol); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", subvol, err)
		}
	})

	_, err := c.UploadArchive(ctx, subvol, strings.NewReader("not a real archive"), "test.zip", "", "")
	if err == nil {
		t.Fatal("expected error for unsupported format, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported error, got: %v", err)
	}
}

func TestArchiveUploadWithSubpath(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()
	subvol := "archive-test-subpath"

	if err := c.CreateFilesystem(ctx, storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(ctx, subvol); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", subvol, err)
		}
	})

	archive := makeTarGz(t, map[string]string{"nested.txt": "nested content"})
	if _, err := c.UploadArchive(ctx, subvol, archive, "test.tar.gz", "deep/sub", ""); err != nil {
		t.Fatalf("UploadArchive with subpath: %v", err)
	}

	got, err := os.ReadFile(filepath.Join("/data/btrfs", subvol, "deep/sub/nested.txt"))
	if err != nil {
		t.Fatalf("ReadFile nested.txt: %v", err)
	}
	if string(got) != "nested content" {
		t.Fatalf("expected %q, got %q", "nested content", string(got))
	}
}

func initSystemControllerTestWithStorageAndBtrfsBase(t *testing.T) (*systemcontroller.SystemdClient, *storage.BtrFS) {
	t.Helper()

	btr := storage.InitBtrFS("/data/btrfs")
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       btr,
		BtrfsBasePath: "/data/btrfs",
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, btr
}

func TestArchiveUploadDownloadInstalledVolume(t *testing.T) {
	c, btr := initSystemControllerTestWithStorageAndBtrfsBase(t)
	ctx := context.TODO()
	subvol := "installed/testrepo/testpkg/1.0/data"

	if err := btr.CreateFilesystem(storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		_ = btr.RemoveFilesystem(subvol)
		_ = btr.RemoveFilesystem("installed/testrepo/testpkg/1.0")
		_ = btr.RemoveFilesystem("installed/testrepo/testpkg")
		_ = btr.RemoveFilesystem("installed/testrepo")
	})

	archive := makeTarGz(t, map[string]string{"pkg-data.txt": "package data"})
	if _, err := c.UploadArchive(ctx, subvol, archive, "test.tar.gz", "", ""); err != nil {
		t.Fatalf("UploadArchive on installed volume: %v", err)
	}

	got, err := os.ReadFile(filepath.Join("/data/btrfs", subvol, "pkg-data.txt"))
	if err != nil {
		t.Fatalf("ReadFile pkg-data.txt: %v", err)
	}
	if string(got) != "package data" {
		t.Fatalf("expected %q, got %q", "package data", string(got))
	}

	rc, err := c.DownloadArchive(ctx, subvol, nil, "", "", "")
	if err != nil {
		t.Fatalf("DownloadArchive on installed volume: %v", err)
	}
	defer func() {
		if err := rc.Close(); err != nil {
			t.Errorf("close download body: %v", err)
		}
	}()

	files := extractTarGz(t, rc)
	content, ok := files["pkg-data.txt"]
	if !ok {
		content, ok = files["./pkg-data.txt"]
	}
	if !ok {
		t.Fatalf("pkg-data.txt not found in downloaded archive, got keys: %v", mapKeys(files))
	}
	if content != "package data" {
		t.Fatalf("downloaded pkg-data.txt: expected %q, got %q", "package data", content)
	}
}

func TestModifyInstalledVolumeQuota(t *testing.T) {
	c, btr := initSystemControllerTestWithStorageAndBtrfsBase(t)
	ctx := context.TODO()
	subvol := "installed/testrepo/quotapkg/1.0/data"

	if err := btr.CreateFilesystem(storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		_ = btr.RemoveFilesystem(subvol)
		_ = btr.RemoveFilesystem("installed/testrepo/quotapkg/1.0")
		_ = btr.RemoveFilesystem("installed/testrepo/quotapkg")
		_ = btr.RemoveFilesystem("installed/testrepo")
	})

	if err := c.ModifyFilesystem(ctx, subvol, storage.Filesystem{
		Name:  subvol,
		Quota: 1073741824,
	}); err != nil {
		t.Fatalf("ModifyFilesystem quota on installed volume: %v", err)
	}

	result, err := c.ListFilesystems(ctx, "", "installed", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	found := false
	for _, f := range result.Entries {
		if f.Name == "testrepo/quotapkg/1.0/data" {
			found = true
			if f.Quota != 1073741824 {
				t.Fatalf("expected quota 1073741824, got %d", f.Quota)
			}
			break
		}
	}
	if !found {
		t.Fatal("installed volume not found in listing after quota modification")
	}
}

func TestArchiveDownloadInstalledSubvolume(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()

	// Installed volumes are now allowed for archive operations.
	// This will fail with a tar error (directory doesn't exist) rather
	// than a reserved filesystem error.
	_, err := c.DownloadArchive(ctx, "installed/repo/pkg/1.0/data", nil, "", "", "")
	if err == nil {
		// If the directory happens to exist, the download would succeed.
		return
	}
	if strings.Contains(err.Error(), "reserved") {
		t.Fatalf("installed volumes should not be rejected as reserved: %v", err)
	}
}

func TestArchiveDownloadWithBzip2Format(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()
	subvol := "archive-test-bz2-dl"

	if err := c.CreateFilesystem(ctx, storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(ctx, subvol); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", subvol, err)
		}
	})

	// Upload a file first.
	archive := makeTarGz(t, map[string]string{"hello.txt": "hello bz2"})
	if _, err := c.UploadArchive(ctx, subvol, archive, "test.tar.gz", "", ""); err != nil {
		t.Fatalf("UploadArchive: %v", err)
	}

	// Download as bzip2.
	rc, err := c.DownloadArchive(ctx, subvol, nil, "", "tar.bz2", "")
	if err != nil {
		t.Fatalf("DownloadArchive bzip2: %v", err)
	}
	defer func() { _ = rc.Close() }()

	// Verify the bzip2 magic bytes.
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(data) < 3 {
		t.Fatal("downloaded archive is too small")
	}
	if data[0] != 0x42 || data[1] != 0x5a || data[2] != 0x68 {
		t.Fatalf("expected bzip2 magic bytes, got %x %x %x", data[0], data[1], data[2])
	}
}

func TestArchiveDownloadWithXZFormat(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()
	subvol := "archive-test-xz-dl"

	if err := c.CreateFilesystem(ctx, storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(ctx, subvol); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", subvol, err)
		}
	})

	// Upload a file first.
	archive := makeTarGz(t, map[string]string{"hello.txt": "hello xz"})
	if _, err := c.UploadArchive(ctx, subvol, archive, "test.tar.gz", "", ""); err != nil {
		t.Fatalf("UploadArchive: %v", err)
	}

	// Download as xz.
	rc, err := c.DownloadArchive(ctx, subvol, nil, "", "tar.xz", "")
	if err != nil {
		t.Fatalf("DownloadArchive xz: %v", err)
	}
	defer func() { _ = rc.Close() }()

	// Verify the xz magic bytes.
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(data) < 6 {
		t.Fatal("downloaded archive is too small")
	}
	if data[0] != 0xfd || data[1] != 0x37 || data[2] != 0x7a || data[3] != 0x58 || data[4] != 0x5a || data[5] != 0x00 {
		t.Fatalf("expected xz magic bytes, got %x %x %x %x %x %x", data[0], data[1], data[2], data[3], data[4], data[5])
	}
}

func TestArchiveDownloadInvalidFormat(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()

	_, err := c.DownloadArchive(ctx, "anything", nil, "", "tar.zst", "")
	if err == nil {
		t.Fatal("expected error for unsupported download format")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported error, got: %v", err)
	}
}

func TestArchiveDownloadWithFilename(t *testing.T) {
	c := initSystemControllerTestWithBtrfsBase(t)
	ctx := context.TODO()
	subvol := "archive-test-filename"

	if err := c.CreateFilesystem(ctx, storage.Filesystem{Name: subvol}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(ctx, subvol); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", subvol, err)
		}
	})

	archive := makeTarGz(t, map[string]string{"data.txt": "test data"})
	if _, err := c.UploadArchive(ctx, subvol, archive, "test.tar.gz", "", ""); err != nil {
		t.Fatalf("UploadArchive: %v", err)
	}

	rc, err := c.DownloadArchive(ctx, subvol, nil, "", "", "my-backup")
	if err != nil {
		t.Fatalf("DownloadArchive with filename: %v", err)
	}
	defer func() { _ = rc.Close() }()

	files := extractTarGz(t, rc)
	if _, ok := files["data.txt"]; !ok {
		if _, ok = files["./data.txt"]; !ok {
			t.Fatalf("data.txt not found in downloaded archive, got keys: %v", mapKeys(files))
		}
	}
}

func mapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// --- Real systemd integration tests ---

func TestRealSystemdListUnits(t *testing.T) {
	c := initRealSystemdTest(t)

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{Search: "repo-test"})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) == 0 {
		t.Fatal("expected at least 1 unit from real systemd")
	}

	found := false
	for _, u := range units.Entries {
		if u.Name == "town-os-package--repo-test-1.0.service" {
			found = true
			if u.LoadState != "loaded" {
				t.Fatalf("expected LoadState 'loaded', got %q", u.LoadState)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected town-os-package--repo-test-1.0.service in unit list")
	}
}

func TestRealSystemdStartStop(t *testing.T) {
	c := initRealSystemdTest(t)

	// Ensure stopped first.
	_ = c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Stop)

	// Start.
	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{Search: "repo-test"})
	if err != nil {
		t.Fatalf("ListUnits after start: %v", err)
	}

	found := false
	for _, u := range units.Entries {
		if u.Name == "town-os-package--repo-test-1.0.service" {
			found = true
			if u.ActiveState != "active" {
				t.Fatalf("expected active after start, got %q", u.ActiveState)
			}
			break
		}
	}
	if !found {
		t.Fatal("town-os-package--repo-test-1.0.service not found after start")
	}

	// Stop.
	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Stop); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	units, err = c.ListUnits(context.TODO(), systemcontroller.ListParams{Search: "repo-test"})
	if err != nil {
		t.Fatalf("ListUnits after stop: %v", err)
	}

	for _, u := range units.Entries {
		if u.Name == "town-os-package--repo-test-1.0.service" {
			if u.ActiveState != "inactive" {
				t.Fatalf("expected inactive after stop, got %q", u.ActiveState)
			}
			return
		}
	}
	t.Fatal("town-os-package--repo-test-1.0.service not found after stop")
}

func TestRealSystemdRestart(t *testing.T) {
	c := initRealSystemdTest(t)

	// Ensure started.
	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Restart); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{Search: "repo-test"})
	if err != nil {
		t.Fatalf("ListUnits after restart: %v", err)
	}

	for _, u := range units.Entries {
		if u.Name == "town-os-package--repo-test-1.0.service" {
			if u.ActiveState != "active" {
				t.Fatalf("expected active after restart, got %q", u.ActiveState)
			}
			return
		}
	}
	t.Fatal("town-os-package--repo-test-1.0.service not found after restart")
}

func TestRealSystemdEnableDisableRejected(t *testing.T) {
	c := initRealSystemdTest(t)

	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Disable); err == nil {
		t.Fatal("expected error for Disable, got nil")
	}

	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Enable); err == nil {
		t.Fatal("expected error for Enable, got nil")
	}
}

func TestRealSystemdLogReplay(t *testing.T) {
	c := initRealSystemdTest(t)

	// Ensure the service has been started at least once.
	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(time.Second)

	ch, err := c.LogReplay(context.TODO(), "town-os-package--repo-test-1.0.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	var entries []systemd.JournalEntry
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

loop:
	for {
		select {
		case entry, ok := <-ch:
			if !ok {
				break loop
			}
			entries = append(entries, entry)
			if entry.Message == "town-os-test-message" {
				break loop
			}
		case <-timer.C:
			break loop
		}
	}

	if len(entries) == 0 {
		t.Fatal("expected at least 1 journal entry")
	}

	found := false
	for _, e := range entries {
		if e.Message == "town-os-test-message" {
			found = true
			if e.SystemdUnit != "town-os-package--repo-test-1.0.service" {
				t.Fatalf("expected SystemdUnit 'town-os-package--repo-test-1.0.service', got %q", e.SystemdUnit)
			}
			break
		}
	}
	if !found {
		var messages []string
		for _, e := range entries {
			messages = append(messages, e.Message)
		}
		t.Fatalf("expected 'town-os-test-message' in journal entries, got messages: %v", messages)
	}
}

func TestInstallPackageWithGitSeed(t *testing.T) {
	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}
	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()

	// Create a local bare git repo to use as the seed source.
	seedRepo := filepath.Join(t.TempDir(), "seed.git")
	for _, args := range [][]string{
		{"init", "--bare", "-b", "main", seedRepo},
	} {
		cmd := exec.CommandContext(context.TODO(), "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	// Clone, add a file, push to seed.
	workDir := filepath.Join(t.TempDir(), "work")
	for _, args := range [][]string{
		{"clone", seedRepo, workDir},
	} {
		cmd := exec.CommandContext(context.TODO(), "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(workDir, "hello.txt"), []byte("hello from seed"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	for _, args := range [][]string{
		{"-C", workDir, "add", "hello.txt"},
		{"-C", workDir, "-c", "user.name=test", "-c", "user.email=test@test", "-c", "commit.gpgsign=false", "commit", "-m", "seed"},
		{"-C", workDir, "push", "origin", "HEAD:main"},
	} {
		cmd := exec.CommandContext(context.TODO(), "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	// Ensure bare repo HEAD points to main (default may be master on some systems).
	{
		cmd := exec.CommandContext(context.TODO(), "git", "-C", seedRepo, "symbolic-ref", "HEAD", "refs/heads/main")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git symbolic-ref HEAD (seed): %v: %s", err, out)
		}
	}

	// BtrfsBasePath points to a real temp dir so git clone can write files.
	btrfsBase := t.TempDir()

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		BtrfsBasePath:  btrfsBase,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	// Create a local file-based package repository as a bare git repo so go-git
	// can clone it. We build the content in a working copy, push to a bare repo,
	// then point AddRepository at the bare repo URL.
	seedURL := fmt.Sprintf("file://%s", seedRepo) //nolint:perfsprint // project convention
	localBareRepo := filepath.Join(t.TempDir(), "local.git")
	localWork := filepath.Join(t.TempDir(), "local-work")
	for _, args := range [][]string{
		{"init", "--bare", localBareRepo},
		{"clone", localBareRepo, localWork},
	} {
		cmd := exec.CommandContext(context.TODO(), "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	pkgDir := filepath.Join(localWork, packages.PackagesDir, "myapp")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	pkgYAML := fmt.Sprintf("image: nginx:1.0\nvolumes:\n  config:\n    mountpoint: /config\n    git: %s\n", seedURL)
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	for _, args := range [][]string{
		{"-C", localWork, "add", "."},
		{"-C", localWork, "-c", "user.name=test", "-c", "user.email=test@test", "commit", "-m", "init"},
		{"-C", localWork, "push", "origin", "HEAD:main"},
	} {
		cmd := exec.CommandContext(context.TODO(), "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	// Ensure bare repo HEAD points to main (default may be master on some systems).
	cmd := exec.CommandContext(context.TODO(), "git", "-C", localBareRepo, "symbolic-ref", "HEAD", "refs/heads/main")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git symbolic-ref HEAD: %v: %s", err, out)
	}

	if err := c.AddRepository(context.TODO(), "local", "file://"+localBareRepo, "", ""); err != nil {
		t.Fatalf("AddRepository: %v", err)
	}

	// Pre-create the volume directory (mock storage doesn't create real dirs).
	volDir := filepath.Join(btrfsBase, "installed", "local", "myapp", "1.0", "config")
	if err := os.MkdirAll(volDir, 0750); err != nil {
		t.Fatalf("MkdirAll volume dir: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "myapp", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify git cloned files exist in the volume.
	helloPath := filepath.Join(volDir, "hello.txt")
	content, err := os.ReadFile(helloPath)
	if err != nil {
		t.Fatalf("expected hello.txt in git seed volume: %v", err)
	}
	if string(content) != "hello from seed" {
		t.Fatalf("expected 'hello from seed', got %q", string(content))
	}
}

func TestGitSourceInstallAndRebuild(t *testing.T) {
	// Create a local bare git repo to serve as the git source.
	bareDir := filepath.Join(t.TempDir(), "bare.git")
	if out, err := exec.CommandContext(context.TODO(), "git", "init", "--bare", "--initial-branch=main", bareDir).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	// Clone the bare repo, add a file, and push.
	workDir := filepath.Join(t.TempDir(), "work")
	if out, err := exec.CommandContext(context.TODO(), "git", "clone", bareDir, workDir).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}

	testContent := "hello from git source"
	if err := os.WriteFile(filepath.Join(workDir, "index.html"), []byte(testContent), 0644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}

	for _, args := range [][]string{
		{"git", "-C", workDir, "config", "user.email", "test@test.com"},
		{"git", "-C", workDir, "config", "user.name", "Test"},
		{"git", "-C", workDir, "config", "commit.gpgsign", "false"},
		{"git", "-C", workDir, "add", "."},
		{"git", "-C", workDir, "commit", "-m", "initial"},
		{"git", "-C", workDir, "push", "origin", "main"},
	} {
		if out, err := exec.CommandContext(context.TODO(), args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	// Set up the system controller with BtrfsBasePath and a package referencing the local git repo.
	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0644); err != nil {
		t.Fatalf("WriteFile repositories: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}

	pkgYAML := fmt.Sprintf(`image: nginx:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  site:
    mountpoint: /var/www/html
    git: file://%s
questions: {}
git_sources:
  - url: %s
    branch: main
    volume: site
`, bareDir, bareDir)

	repoName := "test-repo"
	pkgDir := filepath.Join(dir, repoName, packages.PackagesDir, "mysite")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0644); err != nil {
		t.Fatalf("WriteFile package yaml: %v", err)
	}

	repoURL, err := url.Parse("https://example.com/test-repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	if err := rr.Add(packages.Repository{Name: repoName, URL: *repoURL}); err != nil {
		t.Fatalf("Add repo: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	btrfsBase := t.TempDir()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		BtrfsBasePath:  btrfsBase,
		Git:            &git.GoGitClient{},
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	// Pre-create the volume directory (mock storage doesn't create real dirs).
	volPath := fmt.Sprintf("installed/%s/mysite/1.0/site", repoName)
	volDir := filepath.Join(btrfsBase, volPath)
	if err := os.MkdirAll(volDir, 0750); err != nil {
		t.Fatalf("MkdirAll volume dir: %v", err)
	}

	// Install the package.
	if err := c.InstallPackage(context.TODO(), "mysite", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify the cloned file exists in the volume.
	clonedFile := filepath.Join(btrfsBase, volPath, "index.html")
	content, err := os.ReadFile(clonedFile)
	if err != nil {
		t.Fatalf("ReadFile after clone: %v", err)
	}
	if string(content) != testContent {
		t.Fatalf("expected %q, got %q", testContent, string(content))
	}

	// Push a new commit to the bare repo.
	updatedContent := "updated content"
	if err := os.WriteFile(filepath.Join(workDir, "index.html"), []byte(updatedContent), 0644); err != nil {
		t.Fatalf("WriteFile updated: %v", err)
	}
	for _, args := range [][]string{
		{"git", "-C", workDir, "add", "."},
		{"git", "-C", workDir, "commit", "-m", "update"},
		{"git", "-C", workDir, "push", "origin", "main"},
	} {
		if out, err := exec.CommandContext(context.TODO(), args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	// Call rebuild-git.
	if err := c.RebuildGitSources(context.TODO(), repoName, "mysite", "1.0"); err != nil {
		t.Fatalf("RebuildGitSources: %v", err)
	}

	// Verify the updated file content.
	content, err = os.ReadFile(clonedFile)
	if err != nil {
		t.Fatalf("ReadFile after rebuild: %v", err)
	}
	if string(content) != updatedContent {
		t.Fatalf("expected %q after rebuild, got %q", updatedContent, string(content))
	}
}

func TestRealSystemdLogReplayFields(t *testing.T) {
	c := initRealSystemdTest(t)

	// Ensure the service has been started at least once.
	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(time.Second)

	ch, err := c.LogReplay(context.TODO(), "town-os-package--repo-test-1.0.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case entry, ok := <-ch:
		if !ok {
			t.Fatal("channel closed without entries")
		}
		if entry.RealtimeTimestamp.IsZero() {
			t.Fatal("expected non-zero RealtimeTimestamp")
		}
		if entry.Hostname == "" {
			t.Fatal("expected non-empty Hostname")
		}
	case <-timer.C:
		t.Fatal("timed out waiting for journal entry")
	}
}

func TestSystemControllerListPackagesInstalledOnlyFilter(t *testing.T) {
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

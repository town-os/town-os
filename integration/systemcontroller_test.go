package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

func scRepoCredentials() (string, string) {
	return os.Getenv(packages.EnvRepoUsername), os.Getenv(packages.EnvRepoPassword)
}

func addRepoWithCreds(c *systemcontroller.SystemdClient, rawURL string) error {
	user, pass := scRepoCredentials()
	return c.AddRepository(context.TODO(), "", rawURL, user, pass)
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
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0644); err != nil {
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

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("error adding repository: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing repositories: %v", err)
	}

	if len(repos.Entries) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(repos.Entries))
	}

	if repos.Entries[0].Name != "test-packages-core" {
		t.Fatalf("expected name %q, got %q", "test-packages-core", repos.Entries[0].Name)
	}

	if repos.Entries[0].URL != coreURL.String() {
		t.Fatalf("expected URL %q, got %q", coreURL.String(), repos.Entries[0].URL)
	}
}

func TestSystemControllerRemoveRepository(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("error adding repository: %v", err)
	}

	if err := c.RemoveRepository(context.TODO(), "test-packages-core"); err != nil {
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

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("error adding core: %v", err)
	}

	if err := addRepoWithCreds(c, extrasURL.String()); err != nil {
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

	if !names["test-packages-core"] {
		t.Fatal("expected test-packages-core in list")
	}
	if !names["test-packages-extras"] {
		t.Fatal("expected test-packages-extras in list")
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

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, extrasURL.String()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	if err := c.RemoveRepository(context.TODO(), "test-packages-core"); err != nil {
		t.Fatalf("RemoveRepository core: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories after remove: %v", err)
	}

	if len(repos.Entries) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(repos.Entries))
	}

	if repos.Entries[0].Name != "test-packages-extras" {
		t.Fatalf("expected test-packages-extras to remain, got %q", repos.Entries[0].Name)
	}

	if repos.Entries[0].URL != extrasURL.String() {
		t.Fatalf("expected URL %q, got %q", extrasURL.String(), repos.Entries[0].URL)
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

		err := c.AddRepository(context.TODO(), "", coreURL.String(), "user", "")
		if err == nil {
			t.Fatal("expected error for username without password")
		}
	})

	t.Run("password without username", func(t *testing.T) {
		c := initSystemControllerRepoTest(t)

		err := c.AddRepository(context.TODO(), "", coreURL.String(), "", "pass")
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

	if err := c.AddRepository(context.TODO(), "", coreURL.String(), user, pass); err != nil {
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

	if err := c.AddRepository(context.TODO(), "", coreURL.String(), "", ""); err != nil {
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

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
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
	if pkgs.Entries[0] != "demo-nginx@1.0" {
		t.Fatalf("expected demo-nginx@1.0, got %s", pkgs.Entries[0])
	}
	if pkgs.Entries[1] != "nginx@2.0" {
		t.Fatalf("expected nginx@2.0, got %s", pkgs.Entries[1])
	}
	if pkgs.Entries[2] != "redis@7.0" {
		t.Fatalf("expected redis@7.0, got %s", pkgs.Entries[2])
	}

	// Verify round-trip through ParsePackageIdentity.
	for _, p := range pkgs.Entries {
		pi, err := packages.ParsePackageIdentity(p)
		if err != nil {
			t.Fatalf("invalid package identity %q: %v", p, err)
		}
		if pi.String() != p {
			t.Fatalf("round-trip mismatch: %q != %q", pi.String(), p)
		}
	}
}

func TestSystemControllerListPackagesMultipleRepos(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, extrasURL.String()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	pkgs, err := c.ListPackages(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}

	if len(pkgs.Entries) != 5 {
		t.Fatalf("expected 5 packages, got %d", len(pkgs.Entries))
	}

	// Verify all expected packages present in name@version format.
	pkgSet := map[string]bool{}
	for _, p := range pkgs.Entries {
		pkgSet[p] = true
	}

	for _, want := range []string{"demo-nginx@1.0", "nginx@2.0", "redis@7.0", "mosquitto@2.0", "postgres@16.0"} {
		if !pkgSet[want] {
			t.Fatalf("expected %s in package list", want)
		}
	}

	// Verify sorted order.
	for i := 1; i < len(pkgs.Entries); i++ {
		if pkgs.Entries[i-1] >= pkgs.Entries[i] {
			t.Fatalf("packages not sorted: %q >= %q at index %d", pkgs.Entries[i-1], pkgs.Entries[i], i)
		}
	}
}

func TestSystemControllerListPackagesAfterRemoveRepo(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, extrasURL.String()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	// Remove extras.
	if err := c.RemoveRepository(context.TODO(), "test-packages-extras"); err != nil {
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

	if pkgs.Entries[0] != "demo-nginx@1.0" {
		t.Fatalf("expected demo-nginx@1.0, got %s", pkgs.Entries[0])
	}
	if pkgs.Entries[1] != "nginx@2.0" {
		t.Fatalf("expected nginx@2.0, got %s", pkgs.Entries[1])
	}
	if pkgs.Entries[2] != "redis@7.0" {
		t.Fatalf("expected redis@7.0, got %s", pkgs.Entries[2])
	}
}

// --- GetPackageQuestions integration tests ---

func TestSystemControllerGetPackageQuestions(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
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

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, extrasURL.String()); err != nil {
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

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
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
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0644); err != nil {
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
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0644); err != nil {
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

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, ""); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(pkgs.Entries))
	}
	if pkgs.Entries[0] != "nginx@1.0" {
		t.Fatalf("expected nginx@1.0, got %s", pkgs.Entries[0])
	}
}

func TestSystemControllerInstallAndGetResponses(t *testing.T) {
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	responses := packages.Responses{"hostname": "example", "port": "8080"}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	got, err := c.GetResponses(context.TODO(), "nginx", "1.0")
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

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
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
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", responses, false, ""); err != nil {
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
	got, err := c.GetResponses(context.TODO(), "nginx", "2.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}
	if got["hostname"] != "webserver" {
		t.Fatalf("expected hostname %q, got %q", "webserver", got["hostname"])
	}

	// Uninstall.
	if err := c.UninstallPackage(context.TODO(), "nginx", "2.0", false); err != nil {
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

	// Verify responses gone.
	_, err = c.GetResponses(context.TODO(), "nginx", "2.0")
	if err == nil {
		t.Fatal("expected error getting responses after uninstall")
	}
}

func TestSystemControllerInstallMultiplePackages(t *testing.T) {
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, extrasURL.String()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "alpha", "port": "80"}, false, ""); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}
	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{"password": "secret", "maxmemory": "100mb"}, false, ""); err != nil {
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
	nginxResp, err := c.GetResponses(context.TODO(), "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses nginx@1.0: %v", err)
	}
	if nginxResp["hostname"] != "alpha" {
		t.Fatalf("expected nginx hostname %q, got %q", "alpha", nginxResp["hostname"])
	}

	redisResp, err := c.GetResponses(context.TODO(), "redis", "7.0")
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
	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("add core failed: %v", err)
	}
	if err := addRepoWithCreds(c, extrasURL.String()); err != nil {
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
	if err := c.RemoveRepository(context.TODO(), "test-packages-core"); err != nil {
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
	if repos.Entries[0].Name != "test-packages-extras" {
		t.Fatalf("expected test-packages-extras to remain, got %q", repos.Entries[0].Name)
	}

	// Remove the last one
	if err := c.RemoveRepository(context.TODO(), "test-packages-extras"); err != nil {
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

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, ""); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// nginx has 1 external port (8080->80):
	//   4 InstallUnit (service, socket, upnp-svc, upnp-timer) +
	//   2 Enable (socket, upnp-timer) + 1 Start (service) = 7
	calls := sd.GetCalls()
	if len(calls) != 7 {
		t.Fatalf("expected 7 systemd calls, got %d", len(calls))
	}

	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	if calls[0].Args[0].(string) != "town-os-nginx.service" {
		t.Fatalf("call 0: expected unit %q, got %v", "town-os-nginx.service", calls[0].Args[0])
	}

	// Last call should be Start for the service.
	lastCall := calls[len(calls)-1]
	if lastCall.Method != "SetStatus" {
		t.Fatalf("last call: expected SetStatus, got %q", lastCall.Method)
	}
	if lastCall.Args[1].(systemd.StatusAction) != systemd.Start {
		t.Fatalf("last call: expected Start, got %v", lastCall.Args[1])
	}
}

func TestSystemControllerUninstallRemovesSystemdUnit(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, ""); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	if err := c.UninstallPackage(context.TODO(), "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage nginx@1.0: %v", err)
	}

	// Install (7) + Uninstall: ListPackageUnitFiles + 4 units * (Stop+Disable+Uninstall) = 13 → total 20
	// (4 units in bridge mode: service, socket, upnp-service, upnp-timer — no forwarder)
	calls := sd.GetCalls()
	if len(calls) != 20 {
		t.Fatalf("expected 20 systemd calls, got %d", len(calls))
	}

	// Install phase: first call is InstallUnit for service.
	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}

	// Install phase: last install call (index 6) is Start.
	if calls[6].Args[1].(systemd.StatusAction) != systemd.Start {
		t.Fatalf("call 6: expected Start, got %v", calls[6].Args[1])
	}

	// Uninstall phase starts at index 7: ListPackageUnitFiles, then Stop, Disable, UninstallUnit per unit.
	if calls[7].Method != "ListPackageUnitFiles" {
		t.Fatalf("call 7: expected ListPackageUnitFiles, got %q", calls[7].Method)
	}
	if calls[8].Method != "SetStatus" {
		t.Fatalf("call 8: expected SetStatus, got %q", calls[8].Method)
	}
	if calls[8].Args[1].(systemd.StatusAction) != systemd.Stop {
		t.Fatalf("call 8: expected Stop, got %v", calls[8].Args[1])
	}
}

func TestSystemControllerInstallUninstallFullLifecycle(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install nginx@1.0
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, ""); err != nil {
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
	if pkgs.Entries[0] != "nginx@1.0" {
		t.Fatalf("expected nginx@1.0, got %s", pkgs.Entries[0])
	}

	// Verify 7 systemd calls from install (nginx has 1 ext port, bridge mode).
	calls := sd.GetCalls()
	if len(calls) != 7 {
		t.Fatalf("expected 7 systemd calls after install, got %d", len(calls))
	}

	// Uninstall
	if err := c.UninstallPackage(context.TODO(), "nginx", "1.0", false); err != nil {
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

	// Install (7) + Uninstall: ListPackageUnitFiles + 4 units * 3 ops = 13 → total 20
	// (4 units in bridge mode: service, socket, upnp-service, upnp-timer — no forwarder)
	calls = sd.GetCalls()
	if len(calls) != 20 {
		t.Fatalf("expected 20 systemd calls total, got %d", len(calls))
	}
}

func TestSystemControllerInstallMultiplePackagesSystemdUnits(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, ""); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{"password": "secret", "maxmemory": "100mb"}, false, ""); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// nginx (1 ext port): 4 InstallUnit + 2 Enable + 1 Start = 7
	// redis (1 int port): 2 InstallUnit + 1 Enable + 1 Start = 4
	// Total = 11
	calls := sd.GetCalls()
	if len(calls) != 11 {
		t.Fatalf("expected 11 systemd calls, got %d", len(calls))
	}

	// First call is InstallUnit for nginx service.
	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	if calls[0].Args[0].(string) != "town-os-nginx.service" {
		t.Fatalf("call 0: expected unit %q, got %q", "town-os-nginx.service", calls[0].Args[0])
	}

	// Redis service starts at index 7.
	if calls[7].Method != "InstallUnit" {
		t.Fatalf("call 7: expected InstallUnit, got %q", calls[7].Method)
	}
	if calls[7].Args[0].(string) != "town-os-redis.service" {
		t.Fatalf("call 7: expected unit %q, got %q", "town-os-redis.service", calls[7].Args[0])
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
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0644); err != nil {
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

	unitName := systemd.UnitName("nginx")
	unitPath := fmt.Sprintf("/etc/systemd/system/%s", unitName)

	// Cleanup: unconditionally stop/disable/remove all units to prevent leaks.
	t.Cleanup(func() {
		cleanup := systemd.NewManager()
		ctx := context.Background()
		allUnits := systemd.PackageUnitNames("nginx", packages.PortMap{8080: 80}, packages.PortMap{})
		for _, name := range allUnits {
			_ = cleanup.SetStatus(ctx, name, systemd.Stop)
			_ = cleanup.SetStatus(ctx, name, systemd.Disable)
			_ = cleanup.UninstallUnit(ctx, name)
		}
	})

	// Add core repo.
	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install nginx@1.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, ""); err != nil {
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
	if pkgs.Entries[0] != "nginx@1.0" {
		t.Fatalf("expected nginx@1.0, got %s", pkgs.Entries[0])
	}

	// Verify the unit is active via ListUnits.
	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{Search: "town-os-nginx"})
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
	if err := c.UninstallPackage(context.TODO(), "nginx", "1.0", false); err != nil {
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
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0644); err != nil {
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
		UPnPBinPath:    "/town-os-upnp",
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
func cleanupContainerUnits(pkgName string, external, internal packages.PortMap) {
	cleanup := systemd.NewManager()
	ctx := context.Background()
	allUnits := systemd.PackageUnitNames(pkgName, external, internal)
	for _, name := range allUnits {
		_ = cleanup.SetStatus(ctx, name, systemd.Stop)
		_ = cleanup.SetStatus(ctx, name, systemd.Disable)
		_ = cleanup.UninstallUnit(ctx, name)
	}
	containerName := fmt.Sprintf("town-os-%s", pkgName)
	_ = exec.Command("podman", "stop", "-t", "10", containerName).Run()
	_ = exec.Command("podman", "rm", "-f", containerName).Run()
}

// waitForContainer polls podman inspect until the container reaches "running"
// state or the timeout expires. pkgName is the package name (e.g. "nginx");
// the podman container name is "town-os-<pkgName>".
func waitForContainer(t *testing.T, pkgName string, timeout time.Duration) {
	t.Helper()
	containerName := fmt.Sprintf("town-os-%s", pkgName)
	unitName := fmt.Sprintf("town-os-%s.service", pkgName)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := exec.Command("podman", "inspect", "--format", "{{.State.Status}}", containerName).Output()
		if err == nil && strings.TrimSpace(string(out)) == "running" {
			return
		}
		time.Sleep(2 * time.Second)
	}
	// Log the systemd journal for debugging before failing.
	journal, _ := exec.Command("journalctl", "-u", unitName, "--no-pager", "-n", "50").Output()
	t.Fatalf("container %q did not reach running state within %v\njournal:\n%s", containerName, timeout, string(journal))
}

func TestSystemControllerRealContainerLifecycle(t *testing.T) {
	c := initSystemControllerRealContainerTest(t)

	t.Cleanup(func() {
		cleanupContainerUnits("redis", packages.PortMap{}, packages.PortMap{6379: 6379})
	})

	// Add core repo.
	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install redis@7.0.
	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, ""); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// Wait for the container to start (includes image pull).
	waitForContainer(t, "redis", 180*time.Second)

	// Verify the systemd unit is active.
	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{Search: "town-os-redis"})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	unitName := systemd.UnitName("redis")
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

	// Verify port 6379 is accessible via TCP from the host.
	conn, err := net.DialTimeout("tcp", "127.0.0.1:6379", 10*time.Second)
	if err != nil {
		logs, _ := exec.Command("podman", "logs", "--tail", "20", "town-os-redis").CombinedOutput()
		t.Fatalf("TCP connect to redis on port 6379 failed: %v\ncontainer logs:\n%s", err, string(logs))
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close redis connection: %v", err)
	}

	// Verify podman container is listed.
	out, err := exec.Command("podman", "ps", "--filter", "name=town-os-redis", "--format", "{{.Names}}").Output()
	if err != nil {
		t.Fatalf("podman ps: %v", err)
	}
	if !strings.Contains(string(out), "town-os-redis") {
		t.Fatalf("expected town-os-redis in podman ps output, got: %s", string(out))
	}

	// Uninstall redis@7.0.
	if err := c.UninstallPackage(context.TODO(), "redis", "7.0", false); err != nil {
		t.Fatalf("UninstallPackage redis@7.0: %v", err)
	}

	// Wait for the container to stop.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		inspectOut, inspectErr := exec.Command("podman", "inspect", "--format", "{{.State.Status}}", "town-os-redis").Output()
		if inspectErr != nil || strings.TrimSpace(string(inspectOut)) != "running" {
			break
		}
		time.Sleep(time.Second)
	}

	// Verify the unit file is gone.
	unitPath := fmt.Sprintf("/etc/systemd/system/%s", unitName)
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("expected unit file %q removed after uninstall, got err: %v", unitPath, err)
	}

	// Verify the container is no longer running.
	out, err = exec.Command("podman", "ps", "--filter", "name=town-os-redis", "--format", "{{.Names}}").Output()
	if err == nil && strings.Contains(string(out), "town-os-redis") {
		t.Fatal("expected town-os-redis not in podman ps after uninstall")
	}

	// Verify port 6379 is no longer accessible.
	postConn, postErr := net.DialTimeout("tcp", "127.0.0.1:6379", 2*time.Second)
	if postErr == nil {
		_ = postConn.Close()
		t.Fatal("expected port 6379 to be unreachable after uninstall")
	}
}

func TestSystemControllerRealContainerReinstall(t *testing.T) {
	c := initSystemControllerRealContainerTest(t)

	t.Cleanup(func() {
		cleanupContainerUnits("redis", packages.PortMap{}, packages.PortMap{6379: 6379})
	})

	// Add core repo.
	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install redis@7.0.
	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, ""); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	waitForContainer(t, "redis", 180*time.Second)

	// Reinstall with the same version (same-version reinstall path).
	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"password":  "newpass",
		"maxmemory": "200mb",
	}, false, ""); err != nil {
		t.Fatalf("Reinstall redis@7.0: %v", err)
	}

	// Wait for the new container to come up.
	waitForContainer(t, "redis", 180*time.Second)

	// Verify installed list still shows redis@7.0.
	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	var foundRedis bool
	for _, entry := range pkgs.Entries {
		if entry == "redis@7.0" {
			foundRedis = true
		}
	}
	if !foundRedis {
		t.Fatalf("expected redis@7.0 in installed list, got: %v", pkgs.Entries)
	}

	// Verify port 6379 is accessible via TCP after reinstall.
	conn, err := net.DialTimeout("tcp", "127.0.0.1:6379", 10*time.Second)
	if err != nil {
		logs, _ := exec.Command("podman", "logs", "--tail", "20", "town-os-redis").CombinedOutput()
		t.Fatalf("TCP connect to redis after reinstall failed: %v\ncontainer logs:\n%s", err, string(logs))
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close redis connection: %v", err)
	}

	// Uninstall.
	if err := c.UninstallPackage(context.TODO(), "redis", "7.0", false); err != nil {
		t.Fatalf("UninstallPackage redis@7.0: %v", err)
	}
}

// --- Systemd integration tests ---

func initSystemControllerSystemdTest(t *testing.T, sd *systemd.MockManager) *systemcontroller.SystemdClient {
	t.Helper()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal empty repository list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0644); err != nil {
		t.Fatalf("WriteFile repositories file: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("failed to load repository root: %v", err)
	}

	btr := storage.InitBtrFS("/data/btrfs")
	inst := packages.NewInstallManager(dir)
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: btr, RepositoryRoot: rr, Installer: inst, Systemd: sd})
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
		{Name: "nginx.service", Description: "The NGINX HTTP Server", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "redis.service", Description: "Redis", LoadState: "loaded", ActiveState: "inactive", SubState: "dead"},
		{Name: "postgres.service", Description: "PostgreSQL", LoadState: "loaded", ActiveState: "active", SubState: "running"},
	}
	c := initSystemControllerSystemdTest(t, sd)

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 3 {
		t.Fatalf("expected 3 units, got %d", len(units.Entries))
	}

	if units.Entries[0].Name != "nginx.service" {
		t.Fatalf("expected first unit nginx.service, got %q", units.Entries[0].Name)
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

	if units.Entries[1].Name != "redis.service" {
		t.Fatalf("expected second unit redis.service, got %q", units.Entries[1].Name)
	}
	if units.Entries[1].ActiveState != "inactive" {
		t.Fatalf("expected inactive state for redis, got %q", units.Entries[1].ActiveState)
	}

	if units.Entries[2].Name != "postgres.service" {
		t.Fatalf("expected third unit postgres.service, got %q", units.Entries[2].Name)
	}
}

func TestSystemControllerSystemdListUnitsPreservesAllFields(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "test.service", Description: "Test Unit", LoadState: "loaded", ActiveState: "activating", SubState: "start-pre"},
	}
	c := initSystemControllerSystemdTest(t, sd)

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units.Entries))
	}

	u := units.Entries[0]
	if u.Name != "test.service" {
		t.Fatalf("Name: expected %q, got %q", "test.service", u.Name)
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

func TestSystemControllerSystemdFullLifecycle(t *testing.T) {
	sd := systemd.InitMockManager()
	c := initSystemControllerSystemdTest(t, sd)

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
		{Name: "nginx.service", LoadState: "loaded", ActiveState: "inactive", SubState: "dead"},
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
	if err := c.SetUnitStatus(context.TODO(), "nginx.service", systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Populate log entries and replay.
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	sd.Entries = []systemd.JournalEntry{
		{Message: "Starting nginx...", RealtimeTimestamp: ts, SystemdUnit: "nginx.service", Priority: "6"},
		{Message: "Started nginx.", RealtimeTimestamp: ts.Add(time.Second), SystemdUnit: "nginx.service", Priority: "6"},
	}

	ch, err := c.LogReplay(context.TODO(), "nginx.service")
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
	if err := c.SetUnitStatus(context.TODO(), "nginx.service", systemd.Stop); err != nil {
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
		{Name: "nginx.service"},
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
	if calls[0].Args[0].(string) != "nginx.service" {
		t.Fatalf("expected unit %q, got %v", "nginx.service", calls[0].Args[0])
	}
	if calls[0].Args[1].(systemd.StatusAction) != systemd.Restart {
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
		if calls[i].Args[0].(string) != w.unit {
			t.Fatalf("call %d: expected unit %q, got %v", i, w.unit, calls[i].Args[0])
		}
		if calls[i].Args[1].(systemd.StatusAction) != w.action {
			t.Fatalf("call %d: expected action %q, got %v", i, w.action, calls[i].Args[1])
		}
	}
}

func TestSystemControllerPingUnitCountsFiltersTownOS(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-nginx.service", ActiveState: "active"},
		{Name: "town-os-redis.service", ActiveState: "active"},
		{Name: "town-os-postgres.service", ActiveState: "failed"},
		{Name: "sshd.service", ActiveState: "active"},
		{Name: "systemd-journald.service", ActiveState: "active"},
	}
	c := initSystemControllerSystemdTest(t, sd)

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

	// Simulate package "sc-purge" with two child volumes via direct storage.
	children := []string{"installed/sc-purge/data", "installed/sc-purge/logs"}
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
		_ = btr.RemoveFilesystem("installed/sc-purge")
	})

	// Verify children exist via direct storage.
	list, err := btr.ListFilesystems("installed/sc-purge/")
	if err != nil {
		t.Fatalf("ListFilesystems before purge: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 filesystems before purge, got %d", len(list))
	}

	// Purge all volumes for "sc-purge".
	if err := c.PurgeVolumes(context.TODO(), "sc-purge"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	// Verify all children are gone.
	list, err = btr.ListFilesystems("installed/sc-purge/")
	if err != nil {
		t.Fatalf("ListFilesystems after purge: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 filesystems after purge, got %d", len(list))
	}

	// Verify the parent intermediate is also gone.
	list, err = btr.ListFilesystems("installed/sc-purge")
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
	if err := btr.CreateFilesystem(storage.Filesystem{Name: "installed/sc-pfx/data"}); err != nil {
		t.Fatalf("CreateFilesystem packages/sc-pfx/data: %v", err)
	}
	if err := btr.CreateFilesystem(storage.Filesystem{Name: "installed/sc-pfx2/data"}); err != nil {
		t.Fatalf("CreateFilesystem packages/sc-pfx2/data: %v", err)
	}
	t.Cleanup(func() {
		_ = btr.RemoveFilesystem("installed/sc-pfx/data")
		_ = btr.RemoveFilesystem("installed/sc-pfx")
		_ = btr.RemoveFilesystem("installed/sc-pfx2/data")
		_ = btr.RemoveFilesystem("installed/sc-pfx2")
	})

	// Purge "sc-pfx" only.
	if err := c.PurgeVolumes(context.TODO(), "sc-pfx"); err != nil {
		t.Fatalf("PurgeVolumes sc-pfx: %v", err)
	}

	// sc-pfx should be gone.
	list, err := btr.ListFilesystems("installed/sc-pfx/")
	if err != nil {
		t.Fatalf("ListFilesystems installed/sc-pfx/: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 filesystems for installed/sc-pfx/, got %d", len(list))
	}

	// sc-pfx2 should survive.
	list, err = btr.ListFilesystems("installed/sc-pfx2/")
	if err != nil {
		t.Fatalf("ListFilesystems installed/sc-pfx2/: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 filesystem for installed/sc-pfx2/, got %d", len(list))
	}
}

func TestSystemControllerPurgeVolumesDeepNesting(t *testing.T) {
	c, btr := initSystemControllerTestWithStorage(t)

	names := []string{
		"installed/sc-dpurge/a",
		"installed/sc-dpurge/a/b",
		"installed/sc-dpurge/a/b/c",
		"installed/sc-dpurge/a/b/c/d",
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
		_ = btr.RemoveFilesystem("installed/sc-dpurge")
	})

	if err := c.PurgeVolumes(context.TODO(), "sc-dpurge"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	list, err := btr.ListFilesystems("installed/sc-dpurge")
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
	if err := c.PurgeVolumes(context.TODO(), "sc-nonexistent"); err != nil {
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
		{Name: "installed/sc-pq/data", Quota: 1048576},
		{Name: "installed/sc-pq/logs", Quota: 2097152},
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
		_ = btr.RemoveFilesystem("installed/sc-pq")
	})

	// Verify quotas exist before purge via direct storage.
	list, err := btr.ListFilesystems("installed/sc-pq/")
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

	if err := c.PurgeVolumes(context.TODO(), "sc-pq"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	list, err = btr.ListFilesystems("installed/sc-pq")
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
		"installed/sc-pmulti/alpha",
		"installed/sc-pmulti/bravo",
		"installed/sc-pmulti/charlie",
		"installed/sc-pmulti/delta",
		"installed/sc-pmulti/echo",
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
		_ = btr.RemoveFilesystem("installed/sc-pmulti")
	})

	if err := c.PurgeVolumes(context.TODO(), "sc-pmulti"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	list, err := btr.ListFilesystems("installed/sc-pmulti")
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
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0644); err != nil {
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
	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, ""); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Verify the install created systemd units.
	// nginx 1.0 has 1 external port (8080->80) and 1 volume:
	//   InstallUnit(service) + InstallUnit(socket) + InstallUnit(upnp-svc) +
	//   InstallUnit(upnp-timer) + Enable(socket) + Enable(upnp-timer) + Start(service) = 7
	installCalls := sd.GetCalls()
	if len(installCalls) != 7 {
		t.Fatalf("expected 7 systemd calls from install, got %d", len(installCalls))
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

	// Verify reconciliation re-created all units (same 7 calls).
	calls := sd.GetCalls()
	if len(calls) != 7 {
		t.Fatalf("expected 7 systemd calls from reconcile, got %d: %v", len(calls), calls)
	}

	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	unitName := calls[0].Args[0].(string)
	if unitName != "town-os-nginx.service" {
		t.Fatalf("expected unit town-os-nginx.service, got %s", unitName)
	}

	// Last call should be Start for the service.
	lastCall := calls[len(calls)-1]
	if lastCall.Method != "SetStatus" || lastCall.Args[1].(systemd.StatusAction) != systemd.Start {
		t.Fatalf("last call: expected SetStatus Start, got %s %v", lastCall.Method, lastCall.Args)
	}

	// Verify installed state is intact.
	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(pkgs.Entries) != 1 || pkgs.Entries[0] != "nginx@1.0" {
		t.Fatalf("expected [nginx@1.0], got %v", pkgs.Entries)
	}
}

func TestReconcileMultiplePackagesAfterInstall(t *testing.T) {
	c, rr, inst, sd, mock := initReconcileTest(t)

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, ""); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{"password": "secret", "maxmemory": "100mb"}, false, ""); err != nil {
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
	// nginx (1 ext port): 4 InstallUnit + 2 Enable + 1 Start = 7
	// redis (1 int port): 2 InstallUnit + 1 Enable + 1 Start = 4
	// Total = 11
	if len(calls) != 11 {
		t.Fatalf("expected 11 systemd calls, got %d", len(calls))
	}
}

func TestReconcilePreservesResponses(t *testing.T) {
	c, rr, inst, sd, mock := initReconcileTest(t)

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	responses := packages.Responses{"hostname": "myhost", "port": "9090"}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, ""); err != nil {
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
	resp, err := c.GetResponses(context.TODO(), "nginx", "1.0")
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
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0644); err != nil {
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

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, ""); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// Find the InstallUnit call for the service unit and inspect its content.
	calls := sd.GetCalls()
	var serviceContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name := call.Args[0].(string)
			if name == "town-os-redis.service" {
				serviceContent = call.Args[1].(string)
				break
			}
		}
	}

	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-redis.service")
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

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, ""); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	calls := sd.GetCalls()

	// Find the service unit content.
	var serviceContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name := call.Args[0].(string)
			if name == "town-os-redis.service" {
				serviceContent = call.Args[1].(string)
				break
			}
		}
	}

	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-redis.service")
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

	// Redis 6379→6379 (same port): no forwarder should be installed.
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name := call.Args[0].(string)
			if strings.Contains(name, "fwd") {
				t.Fatalf("unexpected forwarder unit installed for same-port mapping: %s", name)
			}
		}
	}
}

func TestSystemControllerInstallNginxBridgeMode(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTestWithNetworkMode(t, "")

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, ""); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Find the service unit content.
	calls := sd.GetCalls()
	var serviceContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name := call.Args[0].(string)
			if name == "town-os-nginx.service" {
				serviceContent = call.Args[1].(string)
				break
			}
		}
	}

	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-nginx.service")
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

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, ""); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	calls := sd.GetCalls()

	// Find the service unit content.
	var serviceContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name := call.Args[0].(string)
			if name == "town-os-nginx.service" {
				serviceContent = call.Args[1].(string)
				break
			}
		}
	}

	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-nginx.service")
	}

	// Verify --net host is present and -p is not.
	if !strings.Contains(serviceContent, "--net host") {
		t.Fatalf("service unit missing '--net host' in host mode, got:\n%s", serviceContent)
	}
	if strings.Contains(serviceContent, "-p 8080:80") {
		t.Fatal("service unit should not have -p mappings in host mode")
	}

	// Verify forwarder unit was installed (8080→80 mismatch).
	var fwdContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name := call.Args[0].(string)
			if name == "town-os-nginx-fwd-8080-tcp.service" {
				fwdContent = call.Args[1].(string)
				break
			}
		}
	}
	if fwdContent == "" {
		t.Fatal("expected InstallUnit call for forwarder unit town-os-nginx-fwd-8080-tcp.service")
	}
	if !strings.Contains(fwdContent, "TCP-LISTEN:8080,fork,reuseaddr TCP:127.0.0.1:80") {
		t.Fatalf("forwarder unit missing socat command, got:\n%s", fwdContent)
	}
}

func TestSystemControllerInstallNginxHostModeForwarder(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTestWithNetworkMode(t, "host")

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, ""); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	calls := sd.GetCalls()

	// Full verification of forwarder unit content.
	var fwdContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name := call.Args[0].(string)
			if name == "town-os-nginx-fwd-8080-tcp.service" {
				fwdContent = call.Args[1].(string)
				break
			}
		}
	}
	if fwdContent == "" {
		t.Fatal("expected InstallUnit call for town-os-nginx-fwd-8080-tcp.service")
	}
	if !strings.Contains(fwdContent, "Description=Town OS Port Forwarder: nginx 8080->80/tcp") {
		t.Fatalf("forwarder missing description, got:\n%s", fwdContent)
	}
	if !strings.Contains(fwdContent, "BindsTo=town-os-nginx.service") {
		t.Fatalf("forwarder missing BindsTo, got:\n%s", fwdContent)
	}
	if !strings.Contains(fwdContent, "After=town-os-nginx.service") {
		t.Fatalf("forwarder missing After, got:\n%s", fwdContent)
	}
	if !strings.Contains(fwdContent, "ExecStart=/usr/bin/socat TCP-LISTEN:8080,fork,reuseaddr TCP:127.0.0.1:80") {
		t.Fatalf("forwarder missing socat ExecStart, got:\n%s", fwdContent)
	}

	// Verify the forwarder was enabled.
	fwdEnabled := false
	for _, call := range calls {
		if call.Method == "SetStatus" {
			name := call.Args[0].(string)
			action := call.Args[1].(systemd.StatusAction)
			if name == "town-os-nginx-fwd-8080-tcp.service" && action == systemd.Enable {
				fwdEnabled = true
				break
			}
		}
	}
	if !fwdEnabled {
		t.Fatal("expected forwarder unit to be enabled")
	}

	// Verify the service unit has Wants= for the forwarder.
	var serviceContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name := call.Args[0].(string)
			if name == "town-os-nginx.service" {
				serviceContent = call.Args[1].(string)
				break
			}
		}
	}
	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-nginx.service")
	}
	if !strings.Contains(serviceContent, "Wants=town-os-nginx-fwd-8080-tcp.service") {
		t.Fatalf("service unit missing Wants for forwarder, got:\n%s", serviceContent)
	}

	// Verify UPnP uses ext:ext in host mode.
	var upnpContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name := call.Args[0].(string)
			if name == "town-os-nginx-upnp.service" {
				upnpContent = call.Args[1].(string)
				break
			}
		}
	}
	if upnpContent == "" {
		t.Fatal("expected InstallUnit call for town-os-nginx-upnp.service")
	}
	if !strings.Contains(upnpContent, "--port 8080:8080") {
		t.Fatalf("UPnP should use --port 8080:8080 in host mode, got:\n%s", upnpContent)
	}
	if strings.Contains(upnpContent, "--port 8080:80 ") {
		t.Fatal("UPnP should NOT use --port 8080:80 in host mode")
	}
}

func TestReconcileWithNetworkMode(t *testing.T) {
	c, rr, inst, sd, mock := initReconcileTestWithNetworkMode(t, "host")

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, ""); err != nil {
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
			name := call.Args[0].(string)
			if name == "town-os-redis.service" {
				serviceContent = call.Args[1].(string)
				break
			}
		}
	}

	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-redis.service after reconcile")
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

	// Redis 6379→6379: no forwarder should be installed after reconcile.
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name := call.Args[0].(string)
			if strings.Contains(name, "fwd") {
				t.Fatalf("unexpected forwarder unit installed after reconcile for same-port mapping: %s", name)
			}
		}
	}
}

func TestReconcileWithNetworkModeNginxForwarder(t *testing.T) {
	c, rr, inst, sd, mock := initReconcileTestWithNetworkMode(t, "host")

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, ""); err != nil {
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

	// Verify forwarder unit was installed during reconcile.
	var fwdContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name := call.Args[0].(string)
			if name == "town-os-nginx-fwd-8080-tcp.service" {
				fwdContent = call.Args[1].(string)
				break
			}
		}
	}
	if fwdContent == "" {
		t.Fatal("expected InstallUnit call for forwarder unit after reconcile")
	}
	if !strings.Contains(fwdContent, "TCP-LISTEN:8080,fork,reuseaddr TCP:127.0.0.1:80") {
		t.Fatalf("forwarder unit missing socat command after reconcile, got:\n%s", fwdContent)
	}

	// Verify forwarder was enabled.
	fwdEnabled := false
	for _, call := range calls {
		if call.Method == "SetStatus" {
			name := call.Args[0].(string)
			action := call.Args[1].(systemd.StatusAction)
			if name == "town-os-nginx-fwd-8080-tcp.service" && action == systemd.Enable {
				fwdEnabled = true
				break
			}
		}
	}
	if !fwdEnabled {
		t.Fatal("expected forwarder unit to be enabled after reconcile")
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

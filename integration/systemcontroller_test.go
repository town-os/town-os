package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	btr := storage.InitBtrFS("/data/btrfs")
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: btr})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c
}

func TestSystemControllerCreateAndList(t *testing.T) {
	c := initSystemControllerTest(t)

	baseList, err := c.ListFilesystems(context.TODO(), "")
	if err != nil {
		t.Fatalf("error listing before create: %v", err)
	}
	baseCount := len(baseList)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-create-list"}); err != nil {
		t.Fatalf("error creating filesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-create-list"); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", "sc-create-list", err)
		}
	})

	list, err := c.ListFilesystems(context.TODO(), "")
	if err != nil {
		t.Fatalf("error listing after create: %v", err)
	}

	if len(list) != baseCount+1 {
		t.Fatalf("expected %d filesystems after create, got %d", baseCount+1, len(list))
	}

	list, err = c.ListFilesystems(context.TODO(), "sc-create-list")
	if err != nil {
		t.Fatalf("error listing with exact prefix: %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("expected 1 filesystem under test path, got %d", len(list))
	}
}

func TestSystemControllerRemove(t *testing.T) {
	c := initSystemControllerTest(t)

	baseList, err := c.ListFilesystems(context.TODO(), "")
	if err != nil {
		t.Fatalf("error listing before create: %v", err)
	}
	baseCount := len(baseList)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-remove"}); err != nil {
		t.Fatalf("error creating filesystem: %v", err)
	}

	if err := c.RemoveFilesystem(context.TODO(), "sc-remove"); err != nil {
		t.Fatalf("error removing filesystem: %v", err)
	}

	list, err := c.ListFilesystems(context.TODO(), "")
	if err != nil {
		t.Fatalf("error listing after remove: %v", err)
	}

	if len(list) != baseCount {
		t.Fatalf("expected %d filesystems after remove, got %d", baseCount, len(list))
	}
}

func TestSystemControllerMultipleFilesystems(t *testing.T) {
	c := initSystemControllerTest(t)

	baseList, err := c.ListFilesystems(context.TODO(), "")
	if err != nil {
		t.Fatalf("error listing before create: %v", err)
	}
	baseCount := len(baseList)

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

	list, err := c.ListFilesystems(context.TODO(), "")
	if err != nil {
		t.Fatalf("error listing after creates: %v", err)
	}

	if len(list) != baseCount+len(names) {
		t.Fatalf("expected %d filesystems, got %d", baseCount+len(names), len(list))
	}

	// Remove one and verify count
	if err := c.RemoveFilesystem(context.TODO(), "sc-multi-b"); err != nil {
		t.Fatalf("error removing sc-multi-b: %v", err)
	}

	list, err = c.ListFilesystems(context.TODO(), "")
	if err != nil {
		t.Fatalf("error listing after partial remove: %v", err)
	}

	if len(list) != baseCount+len(names)-1 {
		t.Fatalf("expected %d filesystems after partial remove, got %d", baseCount+len(names)-1, len(list))
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

	list, err := c.ListFilesystems(context.TODO(), "sc-pfx-a")
	if err != nil {
		t.Fatalf("error listing with prefix: %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("expected 1 filesystem for prefix sc-pfx-a, got %d", len(list))
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

	baseList, err := c.ListFilesystems(context.TODO(), "")
	if err != nil {
		t.Fatalf("ListFilesystems before create: %v", err)
	}
	baseCount := len(baseList)

	// Create
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-lifecycle"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Verify exists
	list, err := c.ListFilesystems(context.TODO(), "sc-lifecycle")
	if err != nil {
		t.Fatalf("ListFilesystems to verify creation: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(list))
	}

	// Remove
	if err := c.RemoveFilesystem(context.TODO(), "sc-lifecycle"); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	// Verify gone
	list, err = c.ListFilesystems(context.TODO(), "")
	if err != nil {
		t.Fatalf("ListFilesystems to verify removal: %v", err)
	}
	if len(list) != baseCount {
		t.Fatalf("expected %d filesystems after remove, got %d", baseCount, len(list))
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

	err := c.AddRepository(context.TODO(), "", "https://gitea.com/town-os/does-not-exist.git", "", "")
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

	if len(pkgs.Entries) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs.Entries))
	}

	// Results are sorted, latest version only.
	if pkgs.Entries[0] != "nginx@2.0" {
		t.Fatalf("expected nginx@2.0, got %s", pkgs.Entries[0])
	}
	if pkgs.Entries[1] != "redis@7.0" {
		t.Fatalf("expected redis@7.0, got %s", pkgs.Entries[1])
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

	if len(pkgs.Entries) != 4 {
		t.Fatalf("expected 4 packages, got %d", len(pkgs.Entries))
	}

	// Verify all expected packages present in name@version format.
	pkgSet := map[string]bool{}
	for _, p := range pkgs.Entries {
		pkgSet[p] = true
	}

	for _, want := range []string{"nginx@2.0", "redis@7.0", "mosquitto@2.0", "postgres@16.0"} {
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
	if len(pkgs.Entries) != 2 {
		t.Fatalf("expected 2 packages after removing extras, got %d", len(pkgs.Entries))
	}

	if pkgs.Entries[0] != "nginx@2.0" {
		t.Fatalf("expected nginx@2.0, got %s", pkgs.Entries[0])
	}
	if pkgs.Entries[1] != "redis@7.0" {
		t.Fatalf("expected redis@7.0, got %s", pkgs.Entries[1])
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

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}); err != nil {
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
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses); err != nil {
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
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", responses); err != nil {
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

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "alpha", "port": "80"}); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}
	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{"password": "secret"}); err != nil {
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

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 3 {
		t.Fatalf("expected 3 systemd calls, got %d", len(calls))
	}

	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	if calls[0].Args[0].(string) != "town-os-nginx.service" {
		t.Fatalf("call 0: expected unit %q, got %v", "town-os-nginx.service", calls[0].Args[0])
	}

	if calls[1].Method != "SetStatus" {
		t.Fatalf("call 1: expected SetStatus, got %q", calls[1].Method)
	}
	if calls[1].Args[1].(systemd.StatusAction) != systemd.Enable {
		t.Fatalf("call 1: expected Enable, got %v", calls[1].Args[1])
	}

	if calls[2].Method != "SetStatus" {
		t.Fatalf("call 2: expected SetStatus, got %q", calls[2].Method)
	}
	if calls[2].Args[1].(systemd.StatusAction) != systemd.Start {
		t.Fatalf("call 2: expected Start, got %v", calls[2].Args[1])
	}
}

func TestSystemControllerUninstallRemovesSystemdUnit(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	if err := c.UninstallPackage(context.TODO(), "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage nginx@1.0: %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 6 {
		t.Fatalf("expected 6 systemd calls, got %d", len(calls))
	}

	// Install phase: InstallUnit, Enable, Start
	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	if calls[1].Args[1].(systemd.StatusAction) != systemd.Enable {
		t.Fatalf("call 1: expected Enable, got %v", calls[1].Args[1])
	}
	if calls[2].Args[1].(systemd.StatusAction) != systemd.Start {
		t.Fatalf("call 2: expected Start, got %v", calls[2].Args[1])
	}

	// Uninstall phase: Stop, Disable, UninstallUnit
	if calls[3].Method != "SetStatus" {
		t.Fatalf("call 3: expected SetStatus, got %q", calls[3].Method)
	}
	if calls[3].Args[1].(systemd.StatusAction) != systemd.Stop {
		t.Fatalf("call 3: expected Stop, got %v", calls[3].Args[1])
	}

	if calls[4].Method != "SetStatus" {
		t.Fatalf("call 4: expected SetStatus, got %q", calls[4].Method)
	}
	if calls[4].Args[1].(systemd.StatusAction) != systemd.Disable {
		t.Fatalf("call 4: expected Disable, got %v", calls[4].Args[1])
	}

	if calls[5].Method != "UninstallUnit" {
		t.Fatalf("call 5: expected UninstallUnit, got %q", calls[5].Method)
	}
	if calls[5].Args[0].(string) != "town-os-nginx.service" {
		t.Fatalf("call 5: expected unit %q, got %v", "town-os-nginx.service", calls[5].Args[0])
	}
}

func TestSystemControllerInstallUninstallFullLifecycle(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install nginx@1.0
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}); err != nil {
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

	// Verify 3 systemd calls from install
	calls := sd.GetCalls()
	if len(calls) != 3 {
		t.Fatalf("expected 3 systemd calls after install, got %d", len(calls))
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

	// Verify all 6 systemd calls with correct unit name
	calls = sd.GetCalls()
	if len(calls) != 6 {
		t.Fatalf("expected 6 systemd calls total, got %d", len(calls))
	}

	for _, call := range calls {
		unit := call.Args[0].(string)
		if unit != "town-os-nginx.service" {
			t.Fatalf("expected unit %q in call %q, got %q", "town-os-nginx.service", call.Method, unit)
		}
	}
}

func TestSystemControllerInstallMultiplePackagesSystemdUnits(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{"password": "secret"}); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 6 {
		t.Fatalf("expected 6 systemd calls (3 per package), got %d", len(calls))
	}

	// First 3 calls for nginx
	for i := 0; i < 3; i++ {
		unit := calls[i].Args[0].(string)
		if unit != "town-os-nginx.service" {
			t.Fatalf("call %d: expected unit %q, got %q", i, "town-os-nginx.service", unit)
		}
	}

	// Next 3 calls for redis
	for i := 3; i < 6; i++ {
		unit := calls[i].Args[0].(string)
		if unit != "town-os-redis.service" {
			t.Fatalf("call %d: expected unit %q, got %q", i, "town-os-redis.service", unit)
		}
	}

	// Verify call sequence for each package: InstallUnit, Enable, Start
	for _, offset := range []int{0, 3} {
		if calls[offset].Method != "InstallUnit" {
			t.Fatalf("call %d: expected InstallUnit, got %q", offset, calls[offset].Method)
		}
		if calls[offset+1].Method != "SetStatus" || calls[offset+1].Args[1].(systemd.StatusAction) != systemd.Enable {
			t.Fatalf("call %d: expected SetStatus/Enable, got %q/%v", offset+1, calls[offset+1].Method, calls[offset+1].Args[1])
		}
		if calls[offset+2].Method != "SetStatus" || calls[offset+2].Args[1].(systemd.StatusAction) != systemd.Start {
			t.Fatalf("call %d: expected SetStatus/Start, got %q/%v", offset+2, calls[offset+2].Method, calls[offset+2].Args[1])
		}
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

	// Cleanup: unconditionally stop/disable/remove the unit to prevent leaks.
	t.Cleanup(func() {
		cleanup := systemd.NewManager()
		ctx := context.Background()
		_ = cleanup.SetStatus(ctx, unitName, systemd.Stop)
		_ = cleanup.SetStatus(ctx, unitName, systemd.Disable)
		_ = cleanup.UninstallUnit(ctx, unitName)
	})

	// Add core repo.
	if err := addRepoWithCreds(c, coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install nginx@1.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}); err != nil {
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
			if u.ActiveState != "active" {
				t.Fatalf("expected unit %q ActiveState %q, got %q", unitName, "active", u.ActiveState)
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
	for _, action := range []systemd.StatusAction{systemd.Start, systemd.Stop, systemd.Restart, systemd.Enable, systemd.Disable} {
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

	// Enable and start.
	if err := c.SetUnitStatus(context.TODO(), "nginx.service", systemd.Enable); err != nil {
		t.Fatalf("Enable: %v", err)
	}
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

	// Stop and disable.
	if err := c.SetUnitStatus(context.TODO(), "nginx.service", systemd.Stop); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := c.SetUnitStatus(context.TODO(), "nginx.service", systemd.Disable); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	// Verify full call log.
	calls := sd.GetCalls()
	if len(calls) != 7 {
		t.Fatalf("expected 7 calls, got %d", len(calls))
	}

	expected := []string{"ListUnits", "ListUnits", "SetStatus", "SetStatus", "LogReplay", "SetStatus", "SetStatus"}
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

	list, err := c.ListFilesystems(context.TODO(), "sc-quota")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(list))
	}
	if list[0].Quota != 1048576 {
		t.Fatalf("expected quota %d, got %d", 1048576, list[0].Quota)
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

	list, err := c.ListFilesystems(context.TODO(), "sc-modq")
	if err != nil {
		t.Fatalf("ListFilesystems after set: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(list))
	}
	if list[0].Quota != 2097152 {
		t.Fatalf("expected quota %d, got %d", 2097152, list[0].Quota)
	}

	// Clear quota.
	if err := c.ModifyFilesystem(context.TODO(), "sc-modq", storage.Filesystem{Name: "sc-modq", Quota: 0}); err != nil {
		t.Fatalf("ModifyFilesystem clear quota: %v", err)
	}

	list, err = c.ListFilesystems(context.TODO(), "sc-modq")
	if err != nil {
		t.Fatalf("ListFilesystems after clear: %v", err)
	}
	if list[0].Quota != 0 {
		t.Fatalf("expected quota 0 after clear, got %d", list[0].Quota)
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
	list, err := c.ListFilesystems(context.TODO(), "sc-nest/")
	if err != nil {
		t.Fatalf("ListFilesystems with parent prefix: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 filesystems under sc-nest/, got %d", len(list))
	}

	// Exact match should return only the leaf.
	list, err = c.ListFilesystems(context.TODO(), "sc-nest/parent/child/deep")
	if err != nil {
		t.Fatalf("ListFilesystems exact: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 filesystem for exact match, got %d", len(list))
	}
	if list[0].Name != "sc-nest/parent/child/deep" {
		t.Fatalf("expected %q, got %q", "sc-nest/parent/child/deep", list[0].Name)
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
	list, err := c.ListFilesystems(context.TODO(), "sc-leafq/parent/child")
	if err != nil {
		t.Fatalf("ListFilesystems child: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	if list[0].Quota != 4194304 {
		t.Fatalf("expected child quota %d, got %d", 4194304, list[0].Quota)
	}

	// Parent should have no quota.
	list, err = c.ListFilesystems(context.TODO(), "sc-leafq/parent")
	if err != nil {
		t.Fatalf("ListFilesystems parent: %v", err)
	}
	for _, fs := range list {
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

	list, err := c.ListFilesystems(context.TODO(), "sc-qname")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(list))
	}
	if list[0].Name != "sc-qname" {
		t.Fatalf("expected name %q, got %q", "sc-qname", list[0].Name)
	}
	if list[0].Quota != 8192 {
		t.Fatalf("expected quota %d, got %d", 8192, list[0].Quota)
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
	list, err := c.ListFilesystems(context.TODO(), "sc-mnq/data")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("expected 4 filesystems, got %d", len(list))
	}

	// Verify individual quotas.
	quotaMap := map[string]uint64{}
	for _, f := range list {
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
	c := initSystemControllerTest(t)

	// Simulate package "sc-purge" with two child volumes.
	children := []string{"sc-purge/data", "sc-purge/logs"}
	for _, name := range children {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	// Safety net: if purge fails, clean up manually.
	t.Cleanup(func() {
		for i := len(children) - 1; i >= 0; i-- {
			_ = c.RemoveFilesystem(context.TODO(), children[i])
		}
		_ = c.RemoveFilesystem(context.TODO(), "sc-purge")
	})

	// Verify children exist.
	list, err := c.ListFilesystems(context.TODO(), "sc-purge/")
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
	list, err = c.ListFilesystems(context.TODO(), "sc-purge/")
	if err != nil {
		t.Fatalf("ListFilesystems after purge: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 filesystems after purge, got %d", len(list))
	}

	// Verify the parent intermediate is also gone.
	list, err = c.ListFilesystems(context.TODO(), "sc-purge")
	if err != nil {
		t.Fatalf("ListFilesystems parent after purge: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected parent to be purged, got %d filesystems", len(list))
	}
}

func TestSystemControllerPurgeVolumesSimilarPrefix(t *testing.T) {
	c := initSystemControllerTest(t)

	// Create two packages with similar prefixes.
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-pfx/data"}); err != nil {
		t.Fatalf("CreateFilesystem sc-pfx/data: %v", err)
	}
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-pfx2/data"}); err != nil {
		t.Fatalf("CreateFilesystem sc-pfx2/data: %v", err)
	}
	t.Cleanup(func() {
		_ = c.RemoveFilesystem(context.TODO(), "sc-pfx/data")
		_ = c.RemoveFilesystem(context.TODO(), "sc-pfx")
		_ = c.RemoveFilesystem(context.TODO(), "sc-pfx2/data")
		_ = c.RemoveFilesystem(context.TODO(), "sc-pfx2")
	})

	// Purge "sc-pfx" only.
	if err := c.PurgeVolumes(context.TODO(), "sc-pfx"); err != nil {
		t.Fatalf("PurgeVolumes sc-pfx: %v", err)
	}

	// sc-pfx should be gone.
	list, err := c.ListFilesystems(context.TODO(), "sc-pfx/")
	if err != nil {
		t.Fatalf("ListFilesystems sc-pfx/: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 filesystems for sc-pfx/, got %d", len(list))
	}

	// sc-pfx2 should survive.
	list, err = c.ListFilesystems(context.TODO(), "sc-pfx2/")
	if err != nil {
		t.Fatalf("ListFilesystems sc-pfx2/: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 filesystem for sc-pfx2/, got %d", len(list))
	}
}

func TestSystemControllerPurgeVolumesDeepNesting(t *testing.T) {
	c := initSystemControllerTest(t)

	names := []string{
		"sc-dpurge/a",
		"sc-dpurge/a/b",
		"sc-dpurge/a/b/c",
		"sc-dpurge/a/b/c/d",
	}
	for _, name := range names {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}
	t.Cleanup(func() {
		for i := len(names) - 1; i >= 0; i-- {
			_ = c.RemoveFilesystem(context.TODO(), names[i])
		}
		_ = c.RemoveFilesystem(context.TODO(), "sc-dpurge")
	})

	if err := c.PurgeVolumes(context.TODO(), "sc-dpurge"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	list, err := c.ListFilesystems(context.TODO(), "sc-dpurge")
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
	list, err := c.ListFilesystems(context.TODO(), "sc-surv")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 filesystem to survive, got %d", len(list))
	}
}

func TestSystemControllerPurgeVolumesWithQuotas(t *testing.T) {
	c := initSystemControllerTest(t)

	children := []storage.Filesystem{
		{Name: "sc-pq/data", Quota: 1048576},
		{Name: "sc-pq/logs", Quota: 2097152},
	}
	for _, f := range children {
		if err := c.CreateFilesystem(context.TODO(), f); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", f.Name, err)
		}
	}
	t.Cleanup(func() {
		for i := len(children) - 1; i >= 0; i-- {
			_ = c.RemoveFilesystem(context.TODO(), children[i].Name)
		}
		_ = c.RemoveFilesystem(context.TODO(), "sc-pq")
	})

	// Verify quotas exist before purge.
	list, err := c.ListFilesystems(context.TODO(), "sc-pq/")
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

	list, err = c.ListFilesystems(context.TODO(), "sc-pq")
	if err != nil {
		t.Fatalf("ListFilesystems after purge: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 after purge, got %d", len(list))
	}
}

func TestSystemControllerPurgeVolumesMultipleChildren(t *testing.T) {
	c := initSystemControllerTest(t)

	names := []string{
		"sc-pmulti/alpha",
		"sc-pmulti/bravo",
		"sc-pmulti/charlie",
		"sc-pmulti/delta",
		"sc-pmulti/echo",
	}
	for _, name := range names {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}
	t.Cleanup(func() {
		for i := len(names) - 1; i >= 0; i-- {
			_ = c.RemoveFilesystem(context.TODO(), names[i])
		}
		_ = c.RemoveFilesystem(context.TODO(), "sc-pmulti")
	})

	if err := c.PurgeVolumes(context.TODO(), "sc-pmulti"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	list, err := c.ListFilesystems(context.TODO(), "sc-pmulti")
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

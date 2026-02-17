package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

func initSystemControllerTest(t *testing.T) (*systemcontroller.SystemClient, string) {
	t.Helper()

	path, err := filepath.Abs("../local-mount")
	if err != nil {
		t.Fatalf("could not find absolute path: %v", err)
	}

	btr := storage.InitBtrFS()
	ts := systemcontroller.InitTestServer(btr, nil, nil)
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, path
}

func TestSystemControllerCreateAndList(t *testing.T) {
	c, path := initSystemControllerTest(t)
	testPath := filepath.Join(path, "sc-create-list")

	baseList, err := c.ListFilesystems(path)
	if err != nil {
		t.Fatalf("error listing before create: %v", err)
	}
	baseCount := len(baseList)

	if err := c.CreateFilesystem(storage.Filesystem{Name: testPath}); err != nil {
		t.Fatalf("error creating filesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(testPath); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", testPath, err)
		}
	})

	list, err := c.ListFilesystems(path)
	if err != nil {
		t.Fatalf("error listing after create: %v", err)
	}

	if len(list) != baseCount+1 {
		t.Fatalf("expected %d filesystems after create, got %d", baseCount+1, len(list))
	}

	list, err = c.ListFilesystems(testPath)
	if err != nil {
		t.Fatalf("error listing with exact prefix: %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("expected 1 filesystem under test path, got %d", len(list))
	}
}

func TestSystemControllerRemove(t *testing.T) {
	c, path := initSystemControllerTest(t)
	testPath := filepath.Join(path, "sc-remove")

	baseList, err := c.ListFilesystems(path)
	if err != nil {
		t.Fatalf("error listing before create: %v", err)
	}
	baseCount := len(baseList)

	if err := c.CreateFilesystem(storage.Filesystem{Name: testPath}); err != nil {
		t.Fatalf("error creating filesystem: %v", err)
	}

	if err := c.RemoveFilesystem(testPath); err != nil {
		t.Fatalf("error removing filesystem: %v", err)
	}

	list, err := c.ListFilesystems(path)
	if err != nil {
		t.Fatalf("error listing after remove: %v", err)
	}

	if len(list) != baseCount {
		t.Fatalf("expected %d filesystems after remove, got %d", baseCount, len(list))
	}
}

func TestSystemControllerMultipleFilesystems(t *testing.T) {
	c, path := initSystemControllerTest(t)

	baseList, err := c.ListFilesystems(path)
	if err != nil {
		t.Fatalf("error listing before create: %v", err)
	}
	baseCount := len(baseList)

	names := []string{"sc-multi-a", "sc-multi-b", "sc-multi-c"}
	for _, name := range names {
		p := filepath.Join(path, name)
		if err := c.CreateFilesystem(storage.Filesystem{Name: p}); err != nil {
			t.Fatalf("error creating %q: %v", name, err)
		}
		// sc-multi-b is explicitly removed in the test body below.
		if name != "sc-multi-b" {
			t.Cleanup(func() {
				if err := c.RemoveFilesystem(p); err != nil {
					t.Errorf("cleanup RemoveFilesystem(%q): %v", p, err)
				}
			})
		}
	}

	list, err := c.ListFilesystems(path)
	if err != nil {
		t.Fatalf("error listing after creates: %v", err)
	}

	if len(list) != baseCount+len(names) {
		t.Fatalf("expected %d filesystems, got %d", baseCount+len(names), len(list))
	}

	// Remove one and verify count
	if err := c.RemoveFilesystem(filepath.Join(path, "sc-multi-b")); err != nil {
		t.Fatalf("error removing sc-multi-b: %v", err)
	}

	list, err = c.ListFilesystems(path)
	if err != nil {
		t.Fatalf("error listing after partial remove: %v", err)
	}

	if len(list) != baseCount+len(names)-1 {
		t.Fatalf("expected %d filesystems after partial remove, got %d", baseCount+len(names)-1, len(list))
	}
}

func TestSystemControllerListPrefix(t *testing.T) {
	c, path := initSystemControllerTest(t)

	prefixA := filepath.Join(path, "sc-pfx-a")
	prefixB := filepath.Join(path, "sc-pfx-b")

	if err := c.CreateFilesystem(storage.Filesystem{Name: prefixA}); err != nil {
		t.Fatalf("error creating sc-pfx-a: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(prefixA); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", prefixA, err)
		}
	})

	if err := c.CreateFilesystem(storage.Filesystem{Name: prefixB}); err != nil {
		t.Fatalf("error creating sc-pfx-b: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(prefixB); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", prefixB, err)
		}
	})

	list, err := c.ListFilesystems(prefixA)
	if err != nil {
		t.Fatalf("error listing with prefix: %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("expected 1 filesystem for prefix sc-pfx-a, got %d", len(list))
	}
}

func TestSystemControllerModifyUnimplemented(t *testing.T) {
	c, path := initSystemControllerTest(t)
	testPath := filepath.Join(path, "sc-modify")

	if err := c.CreateFilesystem(storage.Filesystem{Name: testPath}); err != nil {
		t.Fatalf("error creating filesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(testPath); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", testPath, err)
		}
	})

	err := c.ModifyFilesystem(testPath, storage.Filesystem{Name: testPath, Quota: 1024})
	if err == nil {
		t.Fatal("expected error from unimplemented ModifyFilesystem")
	}
}

func TestSystemControllerFullLifecycle(t *testing.T) {
	c, path := initSystemControllerTest(t)
	testPath := filepath.Join(path, "sc-lifecycle")

	baseList, err := c.ListFilesystems(path)
	if err != nil {
		t.Fatalf("ListFilesystems before create: %v", err)
	}
	baseCount := len(baseList)

	// Create
	if err := c.CreateFilesystem(storage.Filesystem{Name: testPath}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Verify exists
	list, err := c.ListFilesystems(testPath)
	if err != nil {
		t.Fatalf("ListFilesystems to verify creation: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(list))
	}

	// Remove
	if err := c.RemoveFilesystem(testPath); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	// Verify gone
	list, err = c.ListFilesystems(path)
	if err != nil {
		t.Fatalf("ListFilesystems to verify removal: %v", err)
	}
	if len(list) != baseCount {
		t.Fatalf("expected %d filesystems after remove, got %d", baseCount, len(list))
	}
}

// --- Repository integration tests ---

func initSystemControllerRepoTest(t *testing.T) *systemcontroller.SystemClient {
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
	ts := systemcontroller.InitTestServer(mock, rr, nil)
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c
}

func TestSystemControllerAddAndListRepository(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := c.AddRepository(coreURL.String()); err != nil {
		t.Fatalf("error adding repository: %v", err)
	}

	repos, err := c.ListRepositories()
	if err != nil {
		t.Fatalf("error listing repositories: %v", err)
	}

	if len(repos) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(repos))
	}

	if repos[0].Name != "test-packages-core" {
		t.Fatalf("expected name %q, got %q", "test-packages-core", repos[0].Name)
	}

	if repos[0].URL != coreURL.String() {
		t.Fatalf("expected URL %q, got %q", coreURL.String(), repos[0].URL)
	}
}

func TestSystemControllerRemoveRepository(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := c.AddRepository(coreURL.String()); err != nil {
		t.Fatalf("error adding repository: %v", err)
	}

	if err := c.RemoveRepository("test-packages-core"); err != nil {
		t.Fatalf("error removing repository: %v", err)
	}

	repos, err := c.ListRepositories()
	if err != nil {
		t.Fatalf("error listing after remove: %v", err)
	}

	if len(repos) != 0 {
		t.Fatalf("expected 0 repositories after remove, got %d", len(repos))
	}
}

func TestSystemControllerAddMultipleRepositories(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := c.AddRepository(coreURL.String()); err != nil {
		t.Fatalf("error adding core: %v", err)
	}

	if err := c.AddRepository(extrasURL.String()); err != nil {
		t.Fatalf("error adding extras: %v", err)
	}

	repos, err := c.ListRepositories()
	if err != nil {
		t.Fatalf("error listing repositories: %v", err)
	}

	if len(repos) != 2 {
		t.Fatalf("expected 2 repositories, got %d", len(repos))
	}

	names := map[string]bool{}
	for _, r := range repos {
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

	repos, err := c.ListRepositories()
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}

	if len(repos) != 0 {
		t.Fatalf("expected 0 repositories, got %d", len(repos))
	}
}

func TestSystemControllerListRepositoriesAfterRemove(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := c.AddRepository(coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := c.AddRepository(extrasURL.String()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	if err := c.RemoveRepository("test-packages-core"); err != nil {
		t.Fatalf("RemoveRepository core: %v", err)
	}

	repos, err := c.ListRepositories()
	if err != nil {
		t.Fatalf("ListRepositories after remove: %v", err)
	}

	if len(repos) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(repos))
	}

	if repos[0].Name != "test-packages-extras" {
		t.Fatalf("expected test-packages-extras to remain, got %q", repos[0].Name)
	}

	if repos[0].URL != extrasURL.String() {
		t.Fatalf("expected URL %q, got %q", extrasURL.String(), repos[0].URL)
	}
}

func TestSystemControllerRemoveNonexistentRepository(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	err := c.RemoveRepository("nonexistent")
	if err == nil {
		t.Fatal("expected error removing nonexistent repository")
	}
}

// --- ListPackages integration tests ---

func TestSystemControllerListPackagesEmpty(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	pkgs, err := c.ListPackages()
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}

	if len(pkgs) != 0 {
		t.Fatalf("expected 0 packages, got %d", len(pkgs))
	}
}

func TestSystemControllerListPackagesSingleRepo(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := c.AddRepository(coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	pkgs, err := c.ListPackages()
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs))
	}

	// Results are sorted, latest version only.
	if pkgs[0] != "nginx@2.0" {
		t.Fatalf("expected nginx@2.0, got %s", pkgs[0])
	}
	if pkgs[1] != "redis@7.0" {
		t.Fatalf("expected redis@7.0, got %s", pkgs[1])
	}

	// Verify round-trip through ParsePackageIdentity.
	for _, p := range pkgs {
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

	if err := c.AddRepository(coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := c.AddRepository(extrasURL.String()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	pkgs, err := c.ListPackages()
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}

	if len(pkgs) != 4 {
		t.Fatalf("expected 4 packages, got %d", len(pkgs))
	}

	// Verify all expected packages present in name@version format.
	pkgSet := map[string]bool{}
	for _, p := range pkgs {
		pkgSet[p] = true
	}

	for _, want := range []string{"nginx@2.0", "redis@7.0", "mosquitto@2.0", "postgres@16.0"} {
		if !pkgSet[want] {
			t.Fatalf("expected %s in package list", want)
		}
	}

	// Verify sorted order.
	for i := 1; i < len(pkgs); i++ {
		if pkgs[i-1] >= pkgs[i] {
			t.Fatalf("packages not sorted: %q >= %q at index %d", pkgs[i-1], pkgs[i], i)
		}
	}
}

func TestSystemControllerListPackagesAfterRemoveRepo(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := c.AddRepository(coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := c.AddRepository(extrasURL.String()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	// Remove extras.
	if err := c.RemoveRepository("test-packages-extras"); err != nil {
		t.Fatalf("RemoveRepository extras: %v", err)
	}

	pkgs, err := c.ListPackages()
	if err != nil {
		t.Fatalf("ListPackages after remove: %v", err)
	}

	// Only core packages should remain.
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages after removing extras, got %d", len(pkgs))
	}

	if pkgs[0] != "nginx@2.0" {
		t.Fatalf("expected nginx@2.0, got %s", pkgs[0])
	}
	if pkgs[1] != "redis@7.0" {
		t.Fatalf("expected redis@7.0, got %s", pkgs[1])
	}
}

// --- GetPackageQuestions integration tests ---

func TestSystemControllerGetPackageQuestions(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := c.AddRepository(coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	questions, err := c.GetPackageQuestions("nginx")
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

	if err := c.AddRepository(coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := c.AddRepository(extrasURL.String()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	// postgres from extras
	pgQ, err := c.GetPackageQuestions("postgres")
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

	if err := c.AddRepository(coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	_, err := c.GetPackageQuestions("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
}

// --- Install integration tests ---

func initSystemControllerInstallTest(t *testing.T) (*systemcontroller.SystemClient, *packages.RepositoryRoot) {
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
	ts := systemcontroller.InitTestServer(mock, rr, inst)
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, rr
}

func TestSystemControllerInstallAndListInstalled(t *testing.T) {
	c, _ := initSystemControllerInstallTest(t)

	if err := c.AddRepository(coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage("nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	pkgs, err := c.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(pkgs) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(pkgs))
	}
	if pkgs[0] != "nginx@1.0" {
		t.Fatalf("expected nginx@1.0, got %s", pkgs[0])
	}
}

func TestSystemControllerInstallAndGetResponses(t *testing.T) {
	c, _ := initSystemControllerInstallTest(t)

	if err := c.AddRepository(coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	responses := packages.Responses{"hostname": "example", "port": "8080"}
	if err := c.InstallPackage("nginx", "1.0", responses); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	got, err := c.GetResponses("nginx", "1.0")
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

	if err := c.AddRepository(coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Start empty.
	pkgs, err := c.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled (initial): %v", err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("expected 0 installed initially, got %d", len(pkgs))
	}

	// Install with responses.
	responses := packages.Responses{"hostname": "webserver", "port": "9090"}
	if err := c.InstallPackage("nginx", "2.0", responses); err != nil {
		t.Fatalf("InstallPackage nginx@2.0: %v", err)
	}

	// Verify installed.
	pkgs, err = c.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled after install: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(pkgs))
	}

	// Verify responses.
	got, err := c.GetResponses("nginx", "2.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}
	if got["hostname"] != "webserver" {
		t.Fatalf("expected hostname %q, got %q", "webserver", got["hostname"])
	}

	// Uninstall.
	if err := c.UninstallPackage("nginx", "2.0"); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	// Verify uninstalled.
	pkgs, err = c.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled after uninstall: %v", err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("expected 0 installed after uninstall, got %d", len(pkgs))
	}

	// Verify responses gone.
	_, err = c.GetResponses("nginx", "2.0")
	if err == nil {
		t.Fatal("expected error getting responses after uninstall")
	}
}

func TestSystemControllerInstallMultiplePackages(t *testing.T) {
	c, _ := initSystemControllerInstallTest(t)

	if err := c.AddRepository(coreURL.String()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := c.AddRepository(extrasURL.String()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	if err := c.InstallPackage("nginx", "1.0", packages.Responses{"hostname": "alpha", "port": "80"}); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}
	if err := c.InstallPackage("redis", "7.0", packages.Responses{"password": "secret"}); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	pkgs, err := c.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 installed, got %d", len(pkgs))
	}

	// Each package has its own responses.
	nginxResp, err := c.GetResponses("nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses nginx@1.0: %v", err)
	}
	if nginxResp["hostname"] != "alpha" {
		t.Fatalf("expected nginx hostname %q, got %q", "alpha", nginxResp["hostname"])
	}

	redisResp, err := c.GetResponses("redis", "7.0")
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
	repos, err := c.ListRepositories()
	if err != nil {
		t.Fatalf("ListRepositories at start: %v", err)
	}
	if len(repos) != 0 {
		t.Fatalf("expected empty list, got %d", len(repos))
	}

	// Add two repos
	if err := c.AddRepository(coreURL.String()); err != nil {
		t.Fatalf("add core failed: %v", err)
	}
	if err := c.AddRepository(extrasURL.String()); err != nil {
		t.Fatalf("add extras failed: %v", err)
	}

	// Verify both present
	repos, err = c.ListRepositories()
	if err != nil {
		t.Fatalf("ListRepositories after adding repos: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repositories, got %d", len(repos))
	}

	// Remove one
	if err := c.RemoveRepository("test-packages-core"); err != nil {
		t.Fatalf("remove core failed: %v", err)
	}

	// Verify only extras remains
	repos, err = c.ListRepositories()
	if err != nil {
		t.Fatalf("ListRepositories after removing core: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repository after remove, got %d", len(repos))
	}
	if repos[0].Name != "test-packages-extras" {
		t.Fatalf("expected test-packages-extras to remain, got %q", repos[0].Name)
	}

	// Remove the last one
	if err := c.RemoveRepository("test-packages-extras"); err != nil {
		t.Fatalf("remove extras failed: %v", err)
	}

	// Verify empty
	repos, err = c.ListRepositories()
	if err != nil {
		t.Fatalf("ListRepositories at end: %v", err)
	}
	if len(repos) != 0 {
		t.Fatalf("expected 0 repositories at end, got %d", len(repos))
	}
}

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
	ts := systemcontroller.InitTestServer(btr, nil)
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
	t.Cleanup(func() { c.RemoveFilesystem(testPath) })

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
	t.Cleanup(func() { c.RemoveFilesystem(testPath) })

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
		t.Cleanup(func() { c.RemoveFilesystem(p) })
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
	t.Cleanup(func() { c.RemoveFilesystem(prefixA) })

	if err := c.CreateFilesystem(storage.Filesystem{Name: prefixB}); err != nil {
		t.Fatalf("error creating sc-pfx-b: %v", err)
	}
	t.Cleanup(func() { c.RemoveFilesystem(prefixB) })

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
	t.Cleanup(func() { c.RemoveFilesystem(testPath) })

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
		t.Fatal(err)
	}
	baseCount := len(baseList)

	// Create
	if err := c.CreateFilesystem(storage.Filesystem{Name: testPath}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	t.Cleanup(func() { c.RemoveFilesystem(testPath) })

	// Verify exists
	list, err := c.ListFilesystems(testPath)
	if err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
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
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0644); err != nil {
		t.Fatal(err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("failed to load repository root: %v", err)
	}

	mock := storage.InitBtrFSMock()
	ts := systemcontroller.InitTestServer(mock, rr)
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

func TestSystemControllerRemoveNonexistentRepository(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	err := c.RemoveRepository("nonexistent")
	if err == nil {
		t.Fatal("expected error removing nonexistent repository")
	}
}

func TestSystemControllerRepositoryFullLifecycle(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	// Start empty
	repos, err := c.ListRepositories()
	if err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
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
		t.Fatal(err)
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
		t.Fatal(err)
	}
	if len(repos) != 0 {
		t.Fatalf("expected 0 repositories at end, got %d", len(repos))
	}
}

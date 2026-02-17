package systemcontroller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
)

func initTestClient(t *testing.T) (*SystemClient, *storage.MockBtrFSController) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	controller := mock.Controller.(*storage.MockBtrFSController)
	ts := InitTestServer(mock, nil)
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, controller
}

// --- CreateFilesystem tests ---

func TestCreateFilesystem(t *testing.T) {
	c, controller := initTestClient(t)

	if err := c.CreateFilesystem(storage.Filesystem{Name: "test-vol"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fs := controller.GetFilesystems()
	if len(fs) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(fs))
	}

	if fs[0].Name != "test-vol" {
		t.Fatalf("expected filesystem name %q, got %q", "test-vol", fs[0].Name)
	}
}

func TestCreateFilesystemMultiple(t *testing.T) {
	c, controller := initTestClient(t)

	names := []string{"vol-a", "vol-b", "vol-c"}
	for _, name := range names {
		if err := c.CreateFilesystem(storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("unexpected error creating %q: %v", name, err)
		}
	}

	fs := controller.GetFilesystems()
	if len(fs) != 3 {
		t.Fatalf("expected 3 filesystems, got %d", len(fs))
	}
}

func TestCreateFilesystemBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock, nil)
	t.Cleanup(func() { ts.Server.Close() })

	resp, err := ts.Server.Client().Post(ts.Server.URL+"/storage/create", "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

// --- ModifyFilesystem tests ---

func TestModifyFilesystem(t *testing.T) {
	c, _ := initTestClient(t)

	if err := c.CreateFilesystem(storage.Filesystem{Name: "test-vol"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ModifyFilesystem is unimplemented in BtrFS backend, so expect an error
	err := c.ModifyFilesystem("test-vol", storage.Filesystem{Name: "test-vol", Quota: 1024})
	if err == nil {
		t.Fatal("expected error from unimplemented ModifyFilesystem")
	}
}

func TestModifyFilesystemBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock, nil)
	t.Cleanup(func() { ts.Server.Close() })

	resp, err := ts.Server.Client().Post(ts.Server.URL+"/storage/modify", "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

// --- RemoveFilesystem tests ---

func TestRemoveFilesystem(t *testing.T) {
	c, controller := initTestClient(t)

	if err := c.CreateFilesystem(storage.Filesystem{Name: "test-vol"}); err != nil {
		t.Fatal(err)
	}

	if err := c.RemoveFilesystem("test-vol"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fs := controller.GetFilesystems()
	if len(fs) != 0 {
		t.Fatalf("expected 0 filesystems after removal, got %d", len(fs))
	}
}

func TestRemoveFilesystemPreservesOthers(t *testing.T) {
	c, controller := initTestClient(t)

	if err := c.CreateFilesystem(storage.Filesystem{Name: "keep"}); err != nil {
		t.Fatal(err)
	}
	if err := c.CreateFilesystem(storage.Filesystem{Name: "remove"}); err != nil {
		t.Fatal(err)
	}

	if err := c.RemoveFilesystem("remove"); err != nil {
		t.Fatal(err)
	}

	fs := controller.GetFilesystems()
	if len(fs) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(fs))
	}

	if fs[0].Name != "keep" {
		t.Fatalf("expected remaining filesystem %q, got %q", "keep", fs[0].Name)
	}
}

func TestRemoveFilesystemBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock, nil)
	t.Cleanup(func() { ts.Server.Close() })

	resp, err := ts.Server.Client().Post(ts.Server.URL+"/storage/remove", "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

// --- ListFilesystems tests ---

func TestListFilesystemsEmpty(t *testing.T) {
	c, _ := initTestClient(t)

	fs, err := c.ListFilesystems("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fs) != 0 {
		t.Fatalf("expected 0 filesystems, got %d", len(fs))
	}
}

func TestListFilesystemsAll(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{"vol-a", "vol-b", "vol-c"} {
		if err := c.CreateFilesystem(storage.Filesystem{Name: name}); err != nil {
			t.Fatal(err)
		}
	}

	fs, err := c.ListFilesystems("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fs) != 3 {
		t.Fatalf("expected 3 filesystems, got %d", len(fs))
	}
}

func TestListFilesystemsWithPrefix(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{"app-web", "app-db", "data-cache"} {
		if err := c.CreateFilesystem(storage.Filesystem{Name: name}); err != nil {
			t.Fatal(err)
		}
	}

	fs, err := c.ListFilesystems("app-")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fs) != 2 {
		t.Fatalf("expected 2 filesystems with prefix 'app-', got %d", len(fs))
	}

	fs, err = c.ListFilesystems("data-")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fs) != 1 {
		t.Fatalf("expected 1 filesystem with prefix 'data-', got %d", len(fs))
	}
}

func TestListFilesystemsPrefixNoMatch(t *testing.T) {
	c, _ := initTestClient(t)

	if err := c.CreateFilesystem(storage.Filesystem{Name: "vol-a"}); err != nil {
		t.Fatal(err)
	}

	fs, err := c.ListFilesystems("nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fs) != 0 {
		t.Fatalf("expected 0 filesystems, got %d", len(fs))
	}
}

func TestListFilesystemsBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock, nil)
	t.Cleanup(func() { ts.Server.Close() })

	resp, err := ts.Server.Client().Post(ts.Server.URL+"/storage", "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

// --- Full lifecycle tests ---

func TestCreateListRemoveLifecycle(t *testing.T) {
	c, _ := initTestClient(t)

	// Start empty
	fs, err := c.ListFilesystems("")
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("expected empty list, got %d", len(fs))
	}

	// Create
	if err := c.CreateFilesystem(storage.Filesystem{Name: "lifecycle-vol"}); err != nil {
		t.Fatal(err)
	}

	// Verify present
	fs, err = c.ListFilesystems("")
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(fs))
	}
	if fs[0].Name != "lifecycle-vol" {
		t.Fatalf("expected name %q, got %q", "lifecycle-vol", fs[0].Name)
	}

	// Remove
	if err := c.RemoveFilesystem("lifecycle-vol"); err != nil {
		t.Fatal(err)
	}

	// Verify gone
	fs, err = c.ListFilesystems("")
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("expected 0 after removal, got %d", len(fs))
	}
}

func TestBulkCreateAndRemove(t *testing.T) {
	c, _ := initTestClient(t)

	count := 10
	for i := 0; i < count; i++ {
		if err := c.CreateFilesystem(storage.Filesystem{Name: fmt.Sprintf("vol-%d", i)}); err != nil {
			t.Fatal(err)
		}
	}

	fs, err := c.ListFilesystems("")
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != count {
		t.Fatalf("expected %d filesystems, got %d", count, len(fs))
	}

	// Remove evens
	for i := 0; i < count; i += 2 {
		if err := c.RemoveFilesystem(fmt.Sprintf("vol-%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	fs, err = c.ListFilesystems("")
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != count/2 {
		t.Fatalf("expected %d filesystems after removal, got %d", count/2, len(fs))
	}
}

// --- Client interface conformance ---

func TestSystemClientImplementsClientInterface(t *testing.T) {
	var _ Client = (*SystemClient)(nil)
}

func TestMockClientImplementsClientInterface(t *testing.T) {
	var _ Client = (*MockClient)(nil)
}

// --- MockClient tests ---

func TestMockClientCreateAndList(t *testing.T) {
	m := InitMockClient()

	if err := m.CreateFilesystem(storage.Filesystem{Name: "test"}); err != nil {
		t.Fatal(err)
	}

	fs, err := m.ListFilesystems("")
	if err != nil {
		t.Fatal(err)
	}

	if len(fs) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(fs))
	}

	if fs[0].Name != "test" {
		t.Fatalf("expected name %q, got %q", "test", fs[0].Name)
	}
}

func TestMockClientRemove(t *testing.T) {
	m := InitMockClient()

	if err := m.CreateFilesystem(storage.Filesystem{Name: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveFilesystem("test"); err != nil {
		t.Fatal(err)
	}

	fs, err := m.ListFilesystems("")
	if err != nil {
		t.Fatal(err)
	}

	if len(fs) != 0 {
		t.Fatalf("expected 0 filesystems, got %d", len(fs))
	}
}

func TestMockClientModify(t *testing.T) {
	m := InitMockClient()

	if err := m.CreateFilesystem(storage.Filesystem{Name: "test"}); err != nil {
		t.Fatal(err)
	}

	if err := m.ModifyFilesystem("test", storage.Filesystem{Name: "test", Quota: 2048}); err != nil {
		t.Fatal(err)
	}

	if m.Filesystems["test"].Quota != 2048 {
		t.Fatalf("expected quota 2048, got %d", m.Filesystems["test"].Quota)
	}
}

func TestMockClientModifyNotFound(t *testing.T) {
	m := InitMockClient()

	err := m.ModifyFilesystem("nope", storage.Filesystem{Name: "nope"})
	if err == nil {
		t.Fatal("expected error modifying nonexistent filesystem")
	}
}

func TestMockClientListWithPrefix(t *testing.T) {
	m := InitMockClient()

	for _, name := range []string{"app-web", "app-db", "data-cache"} {
		if err := m.CreateFilesystem(storage.Filesystem{Name: name}); err != nil {
			t.Fatal(err)
		}
	}

	fs, err := m.ListFilesystems("app-")
	if err != nil {
		t.Fatal(err)
	}

	if len(fs) != 2 {
		t.Fatalf("expected 2 filesystems with prefix, got %d", len(fs))
	}
}

func TestMockClientErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := fmt.Errorf("injected error")

	m.CreateErr = injected
	if err := m.CreateFilesystem(storage.Filesystem{Name: "test"}); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}

	m.CreateErr = nil
	m.ListErr = injected
	if _, err := m.ListFilesystems(""); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}

	m.ListErr = nil
	m.RemoveErr = injected
	if err := m.RemoveFilesystem("test"); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}

	m.RemoveErr = nil
	m.ModifyErr = injected
	if err := m.ModifyFilesystem("test", storage.Filesystem{}); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientCallLog(t *testing.T) {
	m := InitMockClient()

	m.CreateFilesystem(storage.Filesystem{Name: "a"})
	m.CreateFilesystem(storage.Filesystem{Name: "b"})
	m.ListFilesystems("")
	m.RemoveFilesystem("a")

	calls := m.GetCalls()
	if len(calls) != 4 {
		t.Fatalf("expected 4 calls, got %d", len(calls))
	}

	expected := []string{"CreateFilesystem", "CreateFilesystem", "ListFilesystems", "RemoveFilesystem"}
	for i, want := range expected {
		if calls[i].Method != want {
			t.Fatalf("call %d: expected method %q, got %q", i, want, calls[i].Method)
		}
	}
}

// --- HTTP-level edge cases ---

func TestWrongHTTPMethod(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock, nil)
	t.Cleanup(func() { ts.Server.Close() })

	resp, err := http.Get(ts.Server.URL + "/storage/create")
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 for GET on POST-only route")
	}
}

func TestNonexistentRoute(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock, nil)
	t.Cleanup(func() { ts.Server.Close() })

	resp, err := ts.Server.Client().Post(ts.Server.URL+"/nonexistent", "application/json", bytes.NewBufferString("{}"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 for nonexistent route")
	}
}

func TestEmptyBody(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock, nil)
	t.Cleanup(func() { ts.Server.Close() })

	resp, err := ts.Server.Client().Post(ts.Server.URL+"/storage/create", "application/json", bytes.NewBufferString(""))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 for empty body")
	}
}

// --- MockClient repository tests ---

func TestMockClientAddAndListRepositories(t *testing.T) {
	m := InitMockClient()

	if err := m.AddRepository("https://example.com/repo.git"); err != nil {
		t.Fatal(err)
	}

	repos, err := m.ListRepositories()
	if err != nil {
		t.Fatal(err)
	}

	if len(repos) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(repos))
	}

	if repos[0].URL != "https://example.com/repo.git" {
		t.Fatalf("expected URL %q, got %q", "https://example.com/repo.git", repos[0].URL)
	}
}

func TestMockClientAddDuplicateRepository(t *testing.T) {
	m := InitMockClient()

	if err := m.AddRepository("https://example.com/repo.git"); err != nil {
		t.Fatal(err)
	}

	err := m.AddRepository("https://example.com/repo.git")
	if err == nil {
		t.Fatal("expected error adding duplicate repository")
	}
}

func TestMockClientRemoveRepository(t *testing.T) {
	m := InitMockClient()

	if err := m.AddRepository("https://example.com/repo.git"); err != nil {
		t.Fatal(err)
	}

	if err := m.RemoveRepository("https://example.com/repo.git"); err != nil {
		t.Fatal(err)
	}

	repos, err := m.ListRepositories()
	if err != nil {
		t.Fatal(err)
	}

	if len(repos) != 0 {
		t.Fatalf("expected 0 repositories, got %d", len(repos))
	}
}

func TestMockClientRemoveRepositoryNotFound(t *testing.T) {
	m := InitMockClient()

	err := m.RemoveRepository("nonexistent")
	if err == nil {
		t.Fatal("expected error removing nonexistent repository")
	}
}

func TestMockClientRepositoryErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := fmt.Errorf("injected error")

	m.AddRepoErr = injected
	if err := m.AddRepository("https://example.com/repo.git"); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}

	m.AddRepoErr = nil
	m.RemRepoErr = injected
	if err := m.RemoveRepository("test"); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}

	m.RemRepoErr = nil
	m.ListRepoErr = injected
	if _, err := m.ListRepositories(); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientRepositoryCallLog(t *testing.T) {
	m := InitMockClient()

	m.AddRepository("https://example.com/a.git")
	m.AddRepository("https://example.com/b.git")
	m.ListRepositories()
	m.RemoveRepository("https://example.com/a.git")

	calls := m.GetCalls()
	if len(calls) != 4 {
		t.Fatalf("expected 4 calls, got %d", len(calls))
	}

	expected := []string{"AddRepository", "AddRepository", "ListRepositories", "RemoveRepository"}
	for i, want := range expected {
		if calls[i].Method != want {
			t.Fatalf("call %d: expected method %q, got %q", i, want, calls[i].Method)
		}
	}
}

// --- Repository HTTP endpoint tests ---

func emptyRepoRoot(t *testing.T) *packages.RepositoryRoot {
	t.Helper()
	return &packages.RepositoryRoot{BaseDir: t.TempDir()}
}

func TestHTTPAddRepositoryBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock, emptyRepoRoot(t))
	t.Cleanup(func() { ts.Server.Close() })

	resp, err := ts.Server.Client().Post(ts.Server.URL+"/repository/add", "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

func TestHTTPRemoveRepositoryBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock, emptyRepoRoot(t))
	t.Cleanup(func() { ts.Server.Close() })

	resp, err := ts.Server.Client().Post(ts.Server.URL+"/repository/remove", "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

func TestHTTPRemoveRepositoryNotFound(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(mock, rr)
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatal(err)
	}

	err = c.RemoveRepository("nonexistent")
	if err == nil {
		t.Fatal("expected error removing nonexistent repository")
	}
}

func TestHTTPListRepositoriesEmpty(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(mock, rr)
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatal(err)
	}

	repos, err := c.ListRepositories()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repos) != 0 {
		t.Fatalf("expected empty list, got %d", len(repos))
	}
}

func TestHTTPListRepositoriesPrePopulated(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u1, _ := url.Parse("https://example.com/repo-a.git")
	u2, _ := url.Parse("https://example.com/repo-b.git")
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u1},
		{Name: "repo-b", URL: *u2},
	}

	ts := InitTestServer(mock, rr)
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatal(err)
	}

	repos, err := c.ListRepositories()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repos) != 2 {
		t.Fatalf("expected 2 repositories, got %d", len(repos))
	}

	if repos[0].Name != "repo-a" || repos[1].Name != "repo-b" {
		t.Fatalf("unexpected repo names: %v", repos)
	}
}

func TestHTTPRepositoryWrongMethod(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock, emptyRepoRoot(t))
	t.Cleanup(func() { ts.Server.Close() })

	resp, err := http.Get(ts.Server.URL + "/repository/add")
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 for GET on POST-only route")
	}
}

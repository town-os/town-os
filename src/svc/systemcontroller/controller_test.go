package systemcontroller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
)

func testRoute(t *testing.T, base, path string) string {
	t.Helper()
	u, err := url.JoinPath(base, path)
	if err != nil {
		t.Fatalf("url.JoinPath(%q, %q): %v", base, path, err)
	}
	return u
}

func initTestClient(t *testing.T) (*SystemdClient, *storage.MockBtrFSController) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	controller := mock.Controller.(*storage.MockBtrFSController)
	ts := InitTestServer(mock, nil, nil, nil)
	t.Cleanup(ts.Close)

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
	ts := InitTestServer(mock, nil, nil, nil)
	t.Cleanup(ts.Close)

	resp, err := ts.Server.Client().Post(testRoute(t, ts.Server.URL, "storage/create"), "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

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
	ts := InitTestServer(mock, nil, nil, nil)
	t.Cleanup(ts.Close)

	resp, err := ts.Server.Client().Post(testRoute(t, ts.Server.URL, "storage/modify"), "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

// --- RemoveFilesystem tests ---

func TestRemoveFilesystem(t *testing.T) {
	c, controller := initTestClient(t)

	if err := c.CreateFilesystem(storage.Filesystem{Name: "test-vol"}); err != nil {
		t.Fatalf("CreateFilesystem %q: %v", "test-vol", err)
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
		t.Fatalf("CreateFilesystem %q: %v", "keep", err)
	}
	if err := c.CreateFilesystem(storage.Filesystem{Name: "remove"}); err != nil {
		t.Fatalf("CreateFilesystem %q: %v", "remove", err)
	}

	if err := c.RemoveFilesystem("remove"); err != nil {
		t.Fatalf("RemoveFilesystem %q: %v", "remove", err)
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
	ts := InitTestServer(mock, nil, nil, nil)
	t.Cleanup(ts.Close)

	resp, err := ts.Server.Client().Post(testRoute(t, ts.Server.URL, "storage/remove"), "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

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
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	fs, err := c.ListFilesystems("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fs) != 3 {
		t.Fatalf("expected 3 filesystems, got %d", len(fs))
	}

	found := map[string]bool{}
	for _, f := range fs {
		found[f.Name] = true
	}
	for _, want := range []string{"vol-a", "vol-b", "vol-c"} {
		if !found[want] {
			t.Fatalf("expected filesystem %q to be present", want)
		}
	}
}

func TestListFilesystemsWithPrefix(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{"app-web", "app-db", "data-cache"} {
		if err := c.CreateFilesystem(storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	fs, err := c.ListFilesystems("app-")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fs) != 2 {
		t.Fatalf("expected 2 filesystems with prefix 'app-', got %d", len(fs))
	}

	appNames := map[string]bool{}
	for _, f := range fs {
		appNames[f.Name] = true
	}
	for _, want := range []string{"app-web", "app-db"} {
		if !appNames[want] {
			t.Fatalf("expected filesystem %q in app- results", want)
		}
	}

	fs, err = c.ListFilesystems("data-")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fs) != 1 {
		t.Fatalf("expected 1 filesystem with prefix 'data-', got %d", len(fs))
	}
	if fs[0].Name != "data-cache" {
		t.Fatalf("expected data-cache, got %s", fs[0].Name)
	}
}

func TestListFilesystemsPrefixNoMatch(t *testing.T) {
	c, _ := initTestClient(t)

	if err := c.CreateFilesystem(storage.Filesystem{Name: "vol-a"}); err != nil {
		t.Fatalf("CreateFilesystem %q: %v", "vol-a", err)
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
	ts := InitTestServer(mock, nil, nil, nil)
	t.Cleanup(ts.Close)

	resp, err := ts.Server.Client().Post(testRoute(t, ts.Server.URL, "storage"), "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

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
		t.Fatalf("ListFilesystems (initial): %v", err)
	}
	if len(fs) != 0 {
		t.Fatalf("expected empty list, got %d", len(fs))
	}

	// Create
	if err := c.CreateFilesystem(storage.Filesystem{Name: "lifecycle-vol"}); err != nil {
		t.Fatalf("CreateFilesystem %q: %v", "lifecycle-vol", err)
	}

	// Verify present
	fs, err = c.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems (after create): %v", err)
	}
	if len(fs) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(fs))
	}
	if fs[0].Name != "lifecycle-vol" {
		t.Fatalf("expected name %q, got %q", "lifecycle-vol", fs[0].Name)
	}

	// Remove
	if err := c.RemoveFilesystem("lifecycle-vol"); err != nil {
		t.Fatalf("RemoveFilesystem %q: %v", "lifecycle-vol", err)
	}

	// Verify gone
	fs, err = c.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems (after remove): %v", err)
	}
	if len(fs) != 0 {
		t.Fatalf("expected 0 after removal, got %d", len(fs))
	}
}

func TestBulkCreateAndRemove(t *testing.T) {
	c, _ := initTestClient(t)

	count := 10
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("vol-%d", i)
		if err := c.CreateFilesystem(storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	fs, err := c.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems (after bulk create): %v", err)
	}
	if len(fs) != count {
		t.Fatalf("expected %d filesystems, got %d", count, len(fs))
	}

	// Remove evens
	for i := 0; i < count; i += 2 {
		name := fmt.Sprintf("vol-%d", i)
		if err := c.RemoveFilesystem(name); err != nil {
			t.Fatalf("RemoveFilesystem %q: %v", name, err)
		}
	}

	fs, err = c.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems (after bulk remove): %v", err)
	}
	if len(fs) != count/2 {
		t.Fatalf("expected %d filesystems after removal, got %d", count/2, len(fs))
	}
}

// --- Client interface conformance ---

func TestSystemdClientImplementsClientInterface(t *testing.T) {
	var _ Client = (*SystemdClient)(nil)
}

func TestMockClientImplementsClientInterface(t *testing.T) {
	var _ Client = (*MockClient)(nil)
}

// --- MockClient tests ---

func TestMockClientCreateAndList(t *testing.T) {
	m := InitMockClient()

	if err := m.CreateFilesystem(storage.Filesystem{Name: "test"}); err != nil {
		t.Fatalf("MockClient.CreateFilesystem %q: %v", "test", err)
	}

	fs, err := m.ListFilesystems("")
	if err != nil {
		t.Fatalf("MockClient.ListFilesystems: %v", err)
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
		t.Fatalf("MockClient.CreateFilesystem %q: %v", "test", err)
	}
	if err := m.RemoveFilesystem("test"); err != nil {
		t.Fatalf("MockClient.RemoveFilesystem %q: %v", "test", err)
	}

	fs, err := m.ListFilesystems("")
	if err != nil {
		t.Fatalf("MockClient.ListFilesystems: %v", err)
	}

	if len(fs) != 0 {
		t.Fatalf("expected 0 filesystems, got %d", len(fs))
	}
}

func TestMockClientModify(t *testing.T) {
	m := InitMockClient()

	if err := m.CreateFilesystem(storage.Filesystem{Name: "test"}); err != nil {
		t.Fatalf("MockClient.CreateFilesystem %q: %v", "test", err)
	}

	if err := m.ModifyFilesystem("test", storage.Filesystem{Name: "test", Quota: 2048}); err != nil {
		t.Fatalf("MockClient.ModifyFilesystem %q: %v", "test", err)
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
			t.Fatalf("MockClient.CreateFilesystem %q: %v", name, err)
		}
	}

	fs, err := m.ListFilesystems("app-")
	if err != nil {
		t.Fatalf("MockClient.ListFilesystems %q: %v", "app-", err)
	}

	if len(fs) != 2 {
		t.Fatalf("expected 2 filesystems with prefix, got %d", len(fs))
	}

	names := map[string]bool{}
	for _, f := range fs {
		names[f.Name] = true
	}
	for _, want := range []string{"app-web", "app-db"} {
		if !names[want] {
			t.Fatalf("expected filesystem %q in results", want)
		}
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

	if err := m.CreateFilesystem(storage.Filesystem{Name: "a"}); err != nil {
		t.Fatalf("MockClient.CreateFilesystem %q: %v", "a", err)
	}
	if err := m.CreateFilesystem(storage.Filesystem{Name: "b"}); err != nil {
		t.Fatalf("MockClient.CreateFilesystem %q: %v", "b", err)
	}
	if _, err := m.ListFilesystems(""); err != nil {
		t.Fatalf("MockClient.ListFilesystems: %v", err)
	}
	if err := m.RemoveFilesystem("a"); err != nil {
		t.Fatalf("MockClient.RemoveFilesystem %q: %v", "a", err)
	}

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
	ts := InitTestServer(mock, nil, nil, nil)
	t.Cleanup(ts.Close)

	resp, err := http.Get(testRoute(t, ts.Server.URL, "storage/create"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 for GET on POST-only route")
	}
}

func TestNonexistentRoute(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock, nil, nil, nil)
	t.Cleanup(ts.Close)

	resp, err := ts.Server.Client().Post(testRoute(t, ts.Server.URL, "nonexistent"), "application/json", bytes.NewBufferString("{}"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 for nonexistent route")
	}
}

func TestEmptyBody(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock, nil, nil, nil)
	t.Cleanup(ts.Close)

	resp, err := ts.Server.Client().Post(testRoute(t, ts.Server.URL, "storage/create"), "application/json", bytes.NewBufferString(""))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 for empty body")
	}
}

// --- MockClient repository tests ---

func TestMockClientAddAndListRepositories(t *testing.T) {
	m := InitMockClient()

	if err := m.AddRepository("https://example.com/repo.git"); err != nil {
		t.Fatalf("MockClient.AddRepository: %v", err)
	}

	repos, err := m.ListRepositories()
	if err != nil {
		t.Fatalf("MockClient.ListRepositories: %v", err)
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
		t.Fatalf("MockClient.AddRepository: %v", err)
	}

	err := m.AddRepository("https://example.com/repo.git")
	if err == nil {
		t.Fatal("expected error adding duplicate repository")
	}
}

func TestMockClientRemoveRepository(t *testing.T) {
	m := InitMockClient()

	if err := m.AddRepository("https://example.com/repo.git"); err != nil {
		t.Fatalf("MockClient.AddRepository: %v", err)
	}

	if err := m.RemoveRepository("https://example.com/repo.git"); err != nil {
		t.Fatalf("MockClient.RemoveRepository: %v", err)
	}

	repos, err := m.ListRepositories()
	if err != nil {
		t.Fatalf("MockClient.ListRepositories: %v", err)
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

	if err := m.AddRepository("https://example.com/a.git"); err != nil {
		t.Fatalf("MockClient.AddRepository %q: %v", "https://example.com/a.git", err)
	}
	if err := m.AddRepository("https://example.com/b.git"); err != nil {
		t.Fatalf("MockClient.AddRepository %q: %v", "https://example.com/b.git", err)
	}
	if _, err := m.ListRepositories(); err != nil {
		t.Fatalf("MockClient.ListRepositories: %v", err)
	}
	if err := m.RemoveRepository("https://example.com/a.git"); err != nil {
		t.Fatalf("MockClient.RemoveRepository %q: %v", "https://example.com/a.git", err)
	}

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
	ts := InitTestServer(mock, emptyRepoRoot(t), nil, nil)
	t.Cleanup(ts.Close)

	resp, err := ts.Server.Client().Post(testRoute(t, ts.Server.URL, "repository/add"), "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

func TestHTTPRemoveRepositoryBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock, emptyRepoRoot(t), nil, nil)
	t.Cleanup(ts.Close)

	resp, err := ts.Server.Client().Post(testRoute(t, ts.Server.URL, "repository/remove"), "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

func TestHTTPRemoveRepositoryNotFound(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(mock, rr, nil, nil)
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	err = c.RemoveRepository("nonexistent")
	if err == nil {
		t.Fatal("expected error removing nonexistent repository")
	}
}

func TestHTTPListRepositoriesEmpty(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(mock, rr, nil, nil)
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
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
	u1, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse %q: %v", "https://example.com/repo-a.git", err)
	}
	u2, err := url.Parse("https://example.com/repo-b.git")
	if err != nil {
		t.Fatalf("url.Parse %q: %v", "https://example.com/repo-b.git", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u1},
		{Name: "repo-b", URL: *u2},
	}

	ts := InitTestServer(mock, rr, nil, nil)
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
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
	ts := InitTestServer(mock, emptyRepoRoot(t), nil, nil)
	t.Cleanup(ts.Close)

	resp, err := http.Get(testRoute(t, ts.Server.URL, "repository/add"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 for GET on POST-only route")
	}
}

// --- ListPackages HTTP endpoint tests ---

func writeTestPackage(t *testing.T, baseDir, repoName, pkgName, version, content string) {
	t.Helper()
	dir := filepath.Join(baseDir, repoName, packages.PackagesDir, pkgName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("os.MkdirAll %q: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s.yaml", version)), []byte(content), 0644); err != nil {
		t.Fatalf("os.WriteFile %s/%s.yaml: %v", dir, version, err)
	}
}

func TestHTTPListPackagesEmpty(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	// create empty packages dir
	pkgDir := filepath.Join(rr.BaseDir, "repo-a", packages.PackagesDir)
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("os.MkdirAll %q: %v", pkgDir, err)
	}

	ts := InitTestServer(mock, rr, nil, nil)
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	pkgs, err := c.ListPackages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs) != 0 {
		t.Fatalf("expected 0 packages, got %d", len(pkgs))
	}
}

func TestHTTPListPackagesPopulated(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")
	writeTestPackage(t, rr.BaseDir, "repo-a", "redis", "7.0", "image: redis:7.0\n")

	ts := InitTestServer(mock, rr, nil, nil)
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	pkgs, err := c.ListPackages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs))
	}

	// results are sorted by name
	if pkgs[0] != "nginx@2.0" {
		t.Fatalf("expected nginx@2.0, got %s", pkgs[0])
	}
	if pkgs[1] != "redis@7.0" {
		t.Fatalf("expected redis@7.0, got %s", pkgs[1])
	}
}

func TestHTTPListPackagesMultipleRepos(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u1, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse %q: %v", "https://example.com/repo-a.git", err)
	}
	u2, err := url.Parse("https://example.com/repo-b.git")
	if err != nil {
		t.Fatalf("url.Parse %q: %v", "https://example.com/repo-b.git", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u1},
		{Name: "repo-b", URL: *u2},
	}

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo-b", "redis", "7.0", "image: redis:7.0\n")
	writeTestPackage(t, rr.BaseDir, "repo-b", "nginx", "3.0", "image: nginx:3.0\n")

	ts := InitTestServer(mock, rr, nil, nil)
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	pkgs, err := c.ListPackages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs))
	}

	// nginx should be 3.0 (higher version from repo-b wins)
	if pkgs[0] != "nginx@3.0" {
		t.Fatalf("expected nginx@3.0, got %s", pkgs[0])
	}
	if pkgs[1] != "redis@7.0" {
		t.Fatalf("expected redis@7.0, got %s", pkgs[1])
	}
}

func TestHTTPListPackagesWrongMethod(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(mock, rr, nil, nil)
	t.Cleanup(ts.Close)

	resp, err := ts.Server.Client().Post(testRoute(t, ts.Server.URL, "packages"), "application/json", nil)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 for POST on GET-only route")
	}
}

// --- GetPackageQuestions HTTP endpoint tests ---

func TestHTTPGetPackageQuestionsPopulated(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", `image: nginx:1.0
questions:
  hostname:
    query: "What hostname?"
    type: hostname
  port:
    query: "What port?"
    type: port
`)

	ts := InitTestServer(mock, rr, nil, nil)
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	questions, err := c.GetPackageQuestions("nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(questions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(questions))
	}
	if questions["hostname"].Query != "What hostname?" {
		t.Fatalf("expected hostname query %q, got %q", "What hostname?", questions["hostname"].Query)
	}
	if questions["hostname"].Type != packages.Hostname {
		t.Fatalf("expected hostname type %q, got %q", packages.Hostname, questions["hostname"].Type)
	}
	if questions["port"].Query != "What port?" {
		t.Fatalf("expected port query %q, got %q", "What port?", questions["port"].Query)
	}
	if questions["port"].Type != packages.Port {
		t.Fatalf("expected port type %q, got %q", packages.Port, questions["port"].Type)
	}
}

func TestHTTPGetPackageQuestionsNotFound(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	// create empty packages dir
	pkgDir := filepath.Join(rr.BaseDir, "repo-a", packages.PackagesDir)
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("os.MkdirAll: %v", err)
	}

	ts := InitTestServer(mock, rr, nil, nil)
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	_, err = c.GetPackageQuestions("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
}

func TestHTTPGetPackageQuestionsBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(mock, rr, nil, nil)
	t.Cleanup(ts.Close)

	resp, err := ts.Server.Client().Post(testRoute(t, ts.Server.URL, "packages/questions"), "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

func TestHTTPGetPackageQuestionsWrongMethod(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(mock, rr, nil, nil)
	t.Cleanup(ts.Close)

	resp, err := http.Get(testRoute(t, ts.Server.URL, "packages/questions"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 for GET on POST-only route")
	}
}

func TestHTTPGetPackageQuestionsLatestVersion(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", `image: nginx:1.0
questions:
  hostname:
    query: "Old question"
`)
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", `image: nginx:2.0
questions:
  hostname:
    query: "New question"
    type: hostname
  port:
    query: "What port?"
    type: port
`)

	ts := InitTestServer(mock, rr, nil, nil)
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	questions, err := c.GetPackageQuestions("nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(questions) != 2 {
		t.Fatalf("expected 2 questions from latest version, got %d", len(questions))
	}
	if questions["hostname"].Query != "New question" {
		t.Fatalf("expected latest version question, got %q", questions["hostname"].Query)
	}
}

// --- MockClient ListPackages tests ---

func TestMockClientListPackages(t *testing.T) {
	m := InitMockClient()
	m.Packages = []string{"nginx@2.0", "redis@7.0"}

	pkgs, err := m.ListPackages()
	if err != nil {
		t.Fatalf("MockClient.ListPackages: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs))
	}

	if pkgs[0] != "nginx@2.0" {
		t.Fatalf("expected nginx@2.0, got %s", pkgs[0])
	}
}

func TestMockClientListPackagesEmpty(t *testing.T) {
	m := InitMockClient()

	pkgs, err := m.ListPackages()
	if err != nil {
		t.Fatalf("MockClient.ListPackages: %v", err)
	}

	if len(pkgs) != 0 {
		t.Fatalf("expected 0 packages, got %d", len(pkgs))
	}
}

func TestMockClientListPackagesErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := fmt.Errorf("injected error")

	m.ListPkgErr = injected
	if _, err := m.ListPackages(); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientListPackagesCallLog(t *testing.T) {
	m := InitMockClient()
	m.Packages = []string{"nginx@1.0"}

	if _, err := m.ListPackages(); err != nil {
		t.Fatalf("MockClient.ListPackages (first call): %v", err)
	}
	if _, err := m.ListPackages(); err != nil {
		t.Fatalf("MockClient.ListPackages (second call): %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}

	for _, c := range calls {
		if c.Method != "ListPackages" {
			t.Fatalf("expected method ListPackages, got %q", c.Method)
		}
	}
}

// --- MockClient GetPackageQuestions tests ---

func TestMockClientGetPackageQuestions(t *testing.T) {
	m := InitMockClient()
	m.Questions = map[string]map[string]packages.Question{
		"nginx": {
			"hostname": {Query: "What hostname?", Type: packages.Hostname},
			"port":     {Query: "What port?", Type: packages.Port},
		},
	}

	questions, err := m.GetPackageQuestions("nginx")
	if err != nil {
		t.Fatalf("MockClient.GetPackageQuestions: %v", err)
	}

	if len(questions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(questions))
	}

	if questions["hostname"].Query != "What hostname?" {
		t.Fatalf("expected %q, got %q", "What hostname?", questions["hostname"].Query)
	}
	if questions["port"].Type != packages.Port {
		t.Fatalf("expected port type, got %q", questions["port"].Type)
	}
}

func TestMockClientGetPackageQuestionsNotFound(t *testing.T) {
	m := InitMockClient()

	_, err := m.GetPackageQuestions("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
}

func TestMockClientGetPackageQuestionsErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := fmt.Errorf("injected error")

	m.Questions = map[string]map[string]packages.Question{
		"nginx": {
			"hostname": {Query: "What hostname?"},
		},
	}

	m.QuestionsErr = injected
	if _, err := m.GetPackageQuestions("nginx"); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientGetPackageQuestionsCallLog(t *testing.T) {
	m := InitMockClient()
	m.Questions = map[string]map[string]packages.Question{
		"nginx": {
			"hostname": {Query: "What hostname?"},
		},
	}

	if _, err := m.GetPackageQuestions("nginx"); err != nil {
		t.Fatalf("MockClient.GetPackageQuestions: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	if calls[0].Method != "GetPackageQuestions" {
		t.Fatalf("expected method GetPackageQuestions, got %q", calls[0].Method)
	}

	args := calls[0].Args
	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}
	if args[0].(string) != "nginx" {
		t.Fatalf("expected arg %q, got %v", "nginx", args[0])
	}
}

func TestMockClientGetPackageQuestionsReturnsCopy(t *testing.T) {
	m := InitMockClient()
	m.Questions = map[string]map[string]packages.Question{
		"nginx": {
			"hostname": {Query: "What hostname?"},
		},
	}

	questions, err := m.GetPackageQuestions("nginx")
	if err != nil {
		t.Fatalf("MockClient.GetPackageQuestions: %v", err)
	}

	questions["hostname"] = packages.Question{Query: "mutated"}

	if m.Questions["nginx"]["hostname"].Query != "What hostname?" {
		t.Fatal("GetPackageQuestions should return a copy, not a reference")
	}
}

// --- Install HTTP endpoint tests ---

func initInstallTestClient(t *testing.T) (*SystemdClient, *packages.MockInstallManager) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(mock, rr, inst, nil)
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, inst
}

func TestHTTPInstallPackage(t *testing.T) {
	c, inst := initInstallTestClient(t)

	responses := packages.Responses{"hostname": "example"}
	if err := c.InstallPackage("nginx", "1.0", responses); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	calls := inst.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "Install" {
		t.Fatalf("expected Install call, got %q", calls[0].Method)
	}
	if calls[0].Args[0].(string) != "repo-a" {
		t.Fatalf("expected repoName %q, got %v", "repo-a", calls[0].Args[0])
	}
	if calls[0].Args[1].(string) != "nginx" {
		t.Fatalf("expected pkgName %q, got %v", "nginx", calls[0].Args[1])
	}
	if calls[0].Args[2].(string) != "1.0" {
		t.Fatalf("expected version %q, got %v", "1.0", calls[0].Args[2])
	}
}

func TestHTTPInstallPackageNotFound(t *testing.T) {
	c, _ := initInstallTestClient(t)

	err := c.InstallPackage("nonexistent", "1.0", packages.Responses{})
	if err == nil {
		t.Fatal("expected error installing nonexistent package")
	}
}

func TestHTTPInstallPackageBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock, emptyRepoRoot(t), nil, nil)
	t.Cleanup(ts.Close)

	resp, err := ts.Server.Client().Post(testRoute(t, ts.Server.URL, "packages/install"), "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

func TestHTTPUninstallPackage(t *testing.T) {
	c, inst := initInstallTestClient(t)

	// Install first so uninstall can succeed.
	if err := c.InstallPackage("nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := c.UninstallPackage("nginx", "1.0"); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	calls := inst.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[1].Method != "Uninstall" {
		t.Fatalf("expected Uninstall call, got %q", calls[1].Method)
	}
}

func TestHTTPUninstallPackageNotInstalled(t *testing.T) {
	c, _ := initInstallTestClient(t)

	err := c.UninstallPackage("nginx", "1.0")
	if err == nil {
		t.Fatal("expected error uninstalling package that is not installed")
	}
}

func TestHTTPUninstallPackageBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock, emptyRepoRoot(t), nil, nil)
	t.Cleanup(ts.Close)

	resp, err := ts.Server.Client().Post(testRoute(t, ts.Server.URL, "packages/uninstall"), "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

func TestHTTPListInstalled(t *testing.T) {
	c, _ := initInstallTestClient(t)

	if err := c.InstallPackage("nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}
	if err := c.InstallPackage("nginx", "2.0", packages.Responses{}); err != nil {
		t.Fatalf("InstallPackage nginx@2.0: %v", err)
	}

	pkgs, err := c.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 installed, got %d", len(pkgs))
	}
}

func TestHTTPListInstalledEmpty(t *testing.T) {
	c, _ := initInstallTestClient(t)

	pkgs, err := c.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(pkgs) != 0 {
		t.Fatalf("expected 0 installed, got %d", len(pkgs))
	}
}

func TestHTTPGetResponses(t *testing.T) {
	c, _ := initInstallTestClient(t)

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

func TestHTTPGetResponsesNotInstalled(t *testing.T) {
	c, _ := initInstallTestClient(t)

	_, err := c.GetResponses("nginx", "1.0")
	if err == nil {
		t.Fatal("expected error getting responses for uninstalled package")
	}
}

func TestHTTPGetResponsesBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock, emptyRepoRoot(t), nil, nil)
	t.Cleanup(ts.Close)

	resp, err := ts.Server.Client().Post(testRoute(t, ts.Server.URL, "packages/responses"), "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == 200 {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

// --- MockClient InstallPackage tests ---

func TestMockClientInstallPackage(t *testing.T) {
	m := InitMockClient()

	responses := packages.Responses{"hostname": "example"}
	if err := m.InstallPackage("nginx", "1.0", responses); err != nil {
		t.Fatalf("MockClient.InstallPackage: %v", err)
	}

	if len(m.Installed) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(m.Installed))
	}
	if m.Installed[0] != "nginx@1.0" {
		t.Fatalf("expected nginx@1.0, got %s", m.Installed[0])
	}
	if m.StoredResponses["nginx@1.0"]["hostname"] != "example" {
		t.Fatalf("expected stored response hostname=example.com, got %v", m.StoredResponses["nginx@1.0"])
	}
}

func TestMockClientInstallPackageErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := fmt.Errorf("injected error")

	m.InstallPkgErr = injected
	if err := m.InstallPackage("nginx", "1.0", packages.Responses{}); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientInstallPackageCallLog(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage("nginx", "1.0", packages.Responses{"k": "v"}); err != nil {
		t.Fatalf("MockClient.InstallPackage: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "InstallPackage" {
		t.Fatalf("expected method InstallPackage, got %q", calls[0].Method)
	}
	if len(calls[0].Args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(calls[0].Args))
	}
	if calls[0].Args[0].(string) != "nginx" {
		t.Fatalf("expected arg 0 %q, got %v", "nginx", calls[0].Args[0])
	}
	if calls[0].Args[1].(string) != "1.0" {
		t.Fatalf("expected arg 1 %q, got %v", "1.0", calls[0].Args[1])
	}
}

// --- MockClient UninstallPackage tests ---

func TestMockClientUninstallPackage(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage("nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := m.UninstallPackage("nginx", "1.0"); err != nil {
		t.Fatalf("MockClient.UninstallPackage: %v", err)
	}

	if len(m.Installed) != 0 {
		t.Fatalf("expected 0 installed after uninstall, got %d", len(m.Installed))
	}
}

func TestMockClientUninstallPackageNotInstalled(t *testing.T) {
	m := InitMockClient()

	err := m.UninstallPackage("nginx", "1.0")
	if err == nil {
		t.Fatal("expected error uninstalling non-installed package")
	}
}

func TestMockClientUninstallPackageErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := fmt.Errorf("injected error")

	m.UninstallPkgErr = injected
	if err := m.UninstallPackage("nginx", "1.0"); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientUninstallPackageCallLog(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage("nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := m.UninstallPackage("nginx", "1.0"); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[1].Method != "UninstallPackage" {
		t.Fatalf("expected method UninstallPackage, got %q", calls[1].Method)
	}
	if len(calls[1].Args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(calls[1].Args))
	}
}

// --- MockClient ListInstalled tests ---

func TestMockClientListInstalled(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage("nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}
	if err := m.InstallPackage("redis", "7.0", packages.Responses{}); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	pkgs, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("MockClient.ListInstalled: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 installed, got %d", len(pkgs))
	}
}

func TestMockClientListInstalledEmpty(t *testing.T) {
	m := InitMockClient()

	pkgs, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("MockClient.ListInstalled: %v", err)
	}

	if len(pkgs) != 0 {
		t.Fatalf("expected 0 installed, got %d", len(pkgs))
	}
}

func TestMockClientListInstalledErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := fmt.Errorf("injected error")

	m.ListInstalledErr = injected
	if _, err := m.ListInstalled(); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// --- MockClient GetResponses tests ---

func TestMockClientGetResponses(t *testing.T) {
	m := InitMockClient()

	responses := packages.Responses{"hostname": "example", "port": "8080"}
	if err := m.InstallPackage("nginx", "1.0", responses); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	got, err := m.GetResponses("nginx", "1.0")
	if err != nil {
		t.Fatalf("MockClient.GetResponses: %v", err)
	}

	if got["hostname"] != "example" {
		t.Fatalf("expected hostname %q, got %q", "example", got["hostname"])
	}
	if got["port"] != "8080" {
		t.Fatalf("expected port %q, got %q", "8080", got["port"])
	}
}

func TestMockClientGetResponsesNotInstalled(t *testing.T) {
	m := InitMockClient()

	_, err := m.GetResponses("nginx", "1.0")
	if err == nil {
		t.Fatal("expected error for non-installed package")
	}
}

func TestMockClientGetResponsesErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := fmt.Errorf("injected error")

	m.GetResponsesErr = injected
	if _, err := m.GetResponses("nginx", "1.0"); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientGetResponsesReturnsCopy(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage("nginx", "1.0", packages.Responses{"hostname": "example"}); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	got, err := m.GetResponses("nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}

	got["hostname"] = "mutated"

	if m.StoredResponses["nginx@1.0"]["hostname"] != "example" {
		t.Fatal("GetResponses should return a copy, not a reference")
	}
}

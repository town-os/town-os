package systemcontroller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
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
	ts := InitTestServer(ServerConfig{Storage: mock})
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

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "test-vol"}); err != nil {
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
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
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
	ts := InitTestServer(ServerConfig{Storage: mock})
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

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "test-vol"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ModifyFilesystem is unimplemented in BtrFS backend, so expect an error
	err := c.ModifyFilesystem(context.TODO(), "test-vol", storage.Filesystem{Name: "test-vol", Quota: 1024})
	if err == nil {
		t.Fatal("expected error from unimplemented ModifyFilesystem")
	}
}

func TestModifyFilesystemBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
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

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "test-vol"}); err != nil {
		t.Fatalf("CreateFilesystem %q: %v", "test-vol", err)
	}

	if err := c.RemoveFilesystem(context.TODO(), "test-vol"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fs := controller.GetFilesystems()
	if len(fs) != 0 {
		t.Fatalf("expected 0 filesystems after removal, got %d", len(fs))
	}
}

func TestRemoveFilesystemPreservesOthers(t *testing.T) {
	c, controller := initTestClient(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "keep"}); err != nil {
		t.Fatalf("CreateFilesystem %q: %v", "keep", err)
	}
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "remove"}); err != nil {
		t.Fatalf("CreateFilesystem %q: %v", "remove", err)
	}

	if err := c.RemoveFilesystem(context.TODO(), "remove"); err != nil {
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
	ts := InitTestServer(ServerConfig{Storage: mock})
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

	fs, err := c.ListFilesystems(context.TODO(), "")
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
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	fs, err := c.ListFilesystems(context.TODO(), "")
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
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	fs, err := c.ListFilesystems(context.TODO(), "app-")
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

	fs, err = c.ListFilesystems(context.TODO(), "data-")
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

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "vol-a"}); err != nil {
		t.Fatalf("CreateFilesystem %q: %v", "vol-a", err)
	}

	fs, err := c.ListFilesystems(context.TODO(), "nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fs) != 0 {
		t.Fatalf("expected 0 filesystems, got %d", len(fs))
	}
}

func TestListFilesystemsBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
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
	fs, err := c.ListFilesystems(context.TODO(), "")
	if err != nil {
		t.Fatalf("ListFilesystems (initial): %v", err)
	}
	if len(fs) != 0 {
		t.Fatalf("expected empty list, got %d", len(fs))
	}

	// Create
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "lifecycle-vol"}); err != nil {
		t.Fatalf("CreateFilesystem %q: %v", "lifecycle-vol", err)
	}

	// Verify present
	fs, err = c.ListFilesystems(context.TODO(), "")
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
	if err := c.RemoveFilesystem(context.TODO(), "lifecycle-vol"); err != nil {
		t.Fatalf("RemoveFilesystem %q: %v", "lifecycle-vol", err)
	}

	// Verify gone
	fs, err = c.ListFilesystems(context.TODO(), "")
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
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	fs, err := c.ListFilesystems(context.TODO(), "")
	if err != nil {
		t.Fatalf("ListFilesystems (after bulk create): %v", err)
	}
	if len(fs) != count {
		t.Fatalf("expected %d filesystems, got %d", count, len(fs))
	}

	// Remove evens
	for i := 0; i < count; i += 2 {
		name := fmt.Sprintf("vol-%d", i)
		if err := c.RemoveFilesystem(context.TODO(), name); err != nil {
			t.Fatalf("RemoveFilesystem %q: %v", name, err)
		}
	}

	fs, err = c.ListFilesystems(context.TODO(), "")
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

	if err := m.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "test"}); err != nil {
		t.Fatalf("MockClient.CreateFilesystem %q: %v", "test", err)
	}

	fs, err := m.ListFilesystems(context.TODO(), "")
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

	if err := m.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "test"}); err != nil {
		t.Fatalf("MockClient.CreateFilesystem %q: %v", "test", err)
	}
	if err := m.RemoveFilesystem(context.TODO(), "test"); err != nil {
		t.Fatalf("MockClient.RemoveFilesystem %q: %v", "test", err)
	}

	fs, err := m.ListFilesystems(context.TODO(), "")
	if err != nil {
		t.Fatalf("MockClient.ListFilesystems: %v", err)
	}

	if len(fs) != 0 {
		t.Fatalf("expected 0 filesystems, got %d", len(fs))
	}
}

func TestMockClientModify(t *testing.T) {
	m := InitMockClient()

	if err := m.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "test"}); err != nil {
		t.Fatalf("MockClient.CreateFilesystem %q: %v", "test", err)
	}

	if err := m.ModifyFilesystem(context.TODO(), "test", storage.Filesystem{Name: "test", Quota: 2048}); err != nil {
		t.Fatalf("MockClient.ModifyFilesystem %q: %v", "test", err)
	}

	if m.Filesystems["test"].Quota != 2048 {
		t.Fatalf("expected quota 2048, got %d", m.Filesystems["test"].Quota)
	}
}

func TestMockClientModifyNotFound(t *testing.T) {
	m := InitMockClient()

	err := m.ModifyFilesystem(context.TODO(), "nope", storage.Filesystem{Name: "nope"})
	if err == nil {
		t.Fatal("expected error modifying nonexistent filesystem")
	}
}

func TestMockClientListWithPrefix(t *testing.T) {
	m := InitMockClient()

	for _, name := range []string{"app-web", "app-db", "data-cache"} {
		if err := m.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("MockClient.CreateFilesystem %q: %v", name, err)
		}
	}

	fs, err := m.ListFilesystems(context.TODO(), "app-")
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
	if err := m.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "test"}); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}

	m.CreateErr = nil
	m.ListErr = injected
	if _, err := m.ListFilesystems(context.TODO(), ""); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}

	m.ListErr = nil
	m.RemoveErr = injected
	if err := m.RemoveFilesystem(context.TODO(), "test"); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}

	m.RemoveErr = nil
	m.ModifyErr = injected
	if err := m.ModifyFilesystem(context.TODO(), "test", storage.Filesystem{}); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientCallLog(t *testing.T) {
	m := InitMockClient()

	if err := m.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "a"}); err != nil {
		t.Fatalf("MockClient.CreateFilesystem %q: %v", "a", err)
	}
	if err := m.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "b"}); err != nil {
		t.Fatalf("MockClient.CreateFilesystem %q: %v", "b", err)
	}
	if _, err := m.ListFilesystems(context.TODO(), ""); err != nil {
		t.Fatalf("MockClient.ListFilesystems: %v", err)
	}
	if err := m.RemoveFilesystem(context.TODO(), "a"); err != nil {
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
	ts := InitTestServer(ServerConfig{Storage: mock})
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
	ts := InitTestServer(ServerConfig{Storage: mock})
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
	ts := InitTestServer(ServerConfig{Storage: mock})
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

	if err := m.AddRepository(context.TODO(), "", "https://example.com/repo.git", "", ""); err != nil {
		t.Fatalf("MockClient.AddRepository: %v", err)
	}

	repos, err := m.ListRepositories(context.TODO())
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

	if err := m.AddRepository(context.TODO(), "", "https://example.com/repo.git", "", ""); err != nil {
		t.Fatalf("MockClient.AddRepository: %v", err)
	}

	err := m.AddRepository(context.TODO(), "", "https://example.com/repo.git", "", "")
	if err == nil {
		t.Fatal("expected error adding duplicate repository")
	}
}

func TestMockClientRemoveRepository(t *testing.T) {
	m := InitMockClient()

	if err := m.AddRepository(context.TODO(), "", "https://example.com/repo.git", "", ""); err != nil {
		t.Fatalf("MockClient.AddRepository: %v", err)
	}

	if err := m.RemoveRepository(context.TODO(), "https://example.com/repo.git"); err != nil {
		t.Fatalf("MockClient.RemoveRepository: %v", err)
	}

	repos, err := m.ListRepositories(context.TODO())
	if err != nil {
		t.Fatalf("MockClient.ListRepositories: %v", err)
	}

	if len(repos) != 0 {
		t.Fatalf("expected 0 repositories, got %d", len(repos))
	}
}

func TestMockClientRemoveRepositoryNotFound(t *testing.T) {
	m := InitMockClient()

	err := m.RemoveRepository(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error removing nonexistent repository")
	}
}

func TestMockClientRepositoryErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := fmt.Errorf("injected error")

	m.AddRepoErr = injected
	if err := m.AddRepository(context.TODO(), "", "https://example.com/repo.git", "", ""); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}

	m.AddRepoErr = nil
	m.RemRepoErr = injected
	if err := m.RemoveRepository(context.TODO(), "test"); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}

	m.RemRepoErr = nil
	m.ListRepoErr = injected
	if _, err := m.ListRepositories(context.TODO()); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientRepositoryCallLog(t *testing.T) {
	m := InitMockClient()

	if err := m.AddRepository(context.TODO(), "", "https://example.com/a.git", "", ""); err != nil {
		t.Fatalf("MockClient.AddRepository %q: %v", "https://example.com/a.git", err)
	}
	if err := m.AddRepository(context.TODO(), "", "https://example.com/b.git", "", ""); err != nil {
		t.Fatalf("MockClient.AddRepository %q: %v", "https://example.com/b.git", err)
	}
	if _, err := m.ListRepositories(context.TODO()); err != nil {
		t.Fatalf("MockClient.ListRepositories: %v", err)
	}
	if err := m.RemoveRepository(context.TODO(), "https://example.com/a.git"); err != nil {
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
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: emptyRepoRoot(t)})
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
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: emptyRepoRoot(t)})
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
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	err = c.RemoveRepository(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error removing nonexistent repository")
	}
}

func TestHTTPListRepositoriesEmpty(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO())
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

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO())
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
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: emptyRepoRoot(t)})
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

func TestHTTPAddRepositoryBadClone(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	err = c.AddRepository(context.TODO(), "", "https://gitea.com/town-os/does-not-exist.git", "", "")
	if err == nil {
		t.Fatal("expected error for inaccessible repository")
	}

	repos, err := c.ListRepositories(context.TODO())
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}

	if len(repos) != 0 {
		t.Fatalf("expected 0 repositories after failed add, got %d", len(repos))
	}
}

func TestHTTPAddRepositoryPartialCredentials(t *testing.T) {
	t.Run("username without password", func(t *testing.T) {
		mock := storage.InitBtrFSMock()
		rr := emptyRepoRoot(t)
		ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
		t.Cleanup(ts.Close)

		c, err := ts.Client()
		if err != nil {
			t.Fatalf("ts.Client: %v", err)
		}

		err = c.AddRepository(context.TODO(), "", "https://example.com/repo.git", "user", "")
		if err == nil {
			t.Fatal("expected error for username without password")
		}
	})

	t.Run("password without username", func(t *testing.T) {
		mock := storage.InitBtrFSMock()
		rr := emptyRepoRoot(t)
		ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
		t.Cleanup(ts.Close)

		c, err := ts.Client()
		if err != nil {
			t.Fatalf("ts.Client: %v", err)
		}

		err = c.AddRepository(context.TODO(), "", "https://example.com/repo.git", "", "pass")
		if err == nil {
			t.Fatal("expected error for password without username")
		}
	})
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

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	pkgs, err := c.ListPackages(context.TODO())
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

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	pkgs, err := c.ListPackages(context.TODO())
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

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	pkgs, err := c.ListPackages(context.TODO())
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
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
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

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	questions, err := c.GetPackageQuestions(context.TODO(), "nginx")
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

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	_, err = c.GetPackageQuestions(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
}

func TestHTTPGetPackageQuestionsBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
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
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
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

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	questions, err := c.GetPackageQuestions(context.TODO(), "nginx")
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

	pkgs, err := m.ListPackages(context.TODO())
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

	pkgs, err := m.ListPackages(context.TODO())
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
	if _, err := m.ListPackages(context.TODO()); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientListPackagesCallLog(t *testing.T) {
	m := InitMockClient()
	m.Packages = []string{"nginx@1.0"}

	if _, err := m.ListPackages(context.TODO()); err != nil {
		t.Fatalf("MockClient.ListPackages (first call): %v", err)
	}
	if _, err := m.ListPackages(context.TODO()); err != nil {
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

	questions, err := m.GetPackageQuestions(context.TODO(), "nginx")
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

	_, err := m.GetPackageQuestions(context.TODO(), "nonexistent")
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
	if _, err := m.GetPackageQuestions(context.TODO(), "nginx"); err != injected {
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

	if _, err := m.GetPackageQuestions(context.TODO(), "nginx"); err != nil {
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

	questions, err := m.GetPackageQuestions(context.TODO(), "nginx")
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
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
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
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses); err != nil {
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

	err := c.InstallPackage(context.TODO(), "nonexistent", "1.0", packages.Responses{})
	if err == nil {
		t.Fatal("expected error installing nonexistent package")
	}
}

func TestHTTPInstallPackageBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: emptyRepoRoot(t)})
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
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := c.UninstallPackage(context.TODO(), "nginx", "1.0"); err != nil {
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

	err := c.UninstallPackage(context.TODO(), "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error uninstalling package that is not installed")
	}
}

func TestHTTPUninstallPackageBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: emptyRepoRoot(t)})
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

// --- Install/Uninstall with systemd integration ---

func initInstallWithSystemdTestClient(t *testing.T) (*SystemdClient, *packages.MockInstallManager, *systemd.MockManager) {
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

	inst := packages.InitMockInstallManager()
	sd := systemd.InitMockManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst, Systemd: sd})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, inst, sd
}

func TestHTTPInstallPackageCreatesSystemdUnit(t *testing.T) {
	c, _, sd := initInstallWithSystemdTestClient(t)

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "testhost"}); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 3 {
		t.Fatalf("expected 3 systemd calls, got %d: %v", len(calls), calls)
	}

	// 1. InstallUnit
	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	if calls[0].Args[0].(string) != "town-os-nginx.service" {
		t.Fatalf("call 0: expected unit name %q, got %v", "town-os-nginx.service", calls[0].Args[0])
	}

	// 2. SetStatus(enable)
	if calls[1].Method != "SetStatus" {
		t.Fatalf("call 1: expected SetStatus, got %q", calls[1].Method)
	}
	if calls[1].Args[0].(string) != "town-os-nginx.service" {
		t.Fatalf("call 1: expected unit name %q, got %v", "town-os-nginx.service", calls[1].Args[0])
	}
	if calls[1].Args[1].(systemd.StatusAction) != systemd.Enable {
		t.Fatalf("call 1: expected action %q, got %v", systemd.Enable, calls[1].Args[1])
	}

	// 3. SetStatus(start)
	if calls[2].Method != "SetStatus" {
		t.Fatalf("call 2: expected SetStatus, got %q", calls[2].Method)
	}
	if calls[2].Args[1].(systemd.StatusAction) != systemd.Start {
		t.Fatalf("call 2: expected action %q, got %v", systemd.Start, calls[2].Args[1])
	}
}

func TestHTTPUninstallPackageRemovesSystemdUnit(t *testing.T) {
	c, _, sd := initInstallWithSystemdTestClient(t)

	// Install first so uninstall can succeed.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := c.UninstallPackage(context.TODO(), "nginx", "1.0"); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	calls := sd.GetCalls()
	// Install produces 3 calls (InstallUnit, enable, start)
	// Uninstall produces 3 calls (stop, disable, UninstallUnit)
	if len(calls) != 6 {
		t.Fatalf("expected 6 systemd calls, got %d: %v", len(calls), calls)
	}

	// Uninstall calls: indices 3, 4, 5
	// 3. SetStatus(stop)
	if calls[3].Method != "SetStatus" {
		t.Fatalf("call 3: expected SetStatus, got %q", calls[3].Method)
	}
	if calls[3].Args[1].(systemd.StatusAction) != systemd.Stop {
		t.Fatalf("call 3: expected action %q, got %v", systemd.Stop, calls[3].Args[1])
	}

	// 4. SetStatus(disable)
	if calls[4].Method != "SetStatus" {
		t.Fatalf("call 4: expected SetStatus, got %q", calls[4].Method)
	}
	if calls[4].Args[1].(systemd.StatusAction) != systemd.Disable {
		t.Fatalf("call 4: expected action %q, got %v", systemd.Disable, calls[4].Args[1])
	}

	// 5. UninstallUnit
	if calls[5].Method != "UninstallUnit" {
		t.Fatalf("call 5: expected UninstallUnit, got %q", calls[5].Method)
	}
	if calls[5].Args[0].(string) != "town-os-nginx.service" {
		t.Fatalf("call 5: expected unit name %q, got %v", "town-os-nginx.service", calls[5].Args[0])
	}
}

func TestHTTPListInstalled(t *testing.T) {
	c, _ := initInstallTestClient(t)

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", packages.Responses{}); err != nil {
		t.Fatalf("InstallPackage nginx@2.0: %v", err)
	}

	pkgs, err := c.ListInstalled(context.TODO())
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 installed, got %d", len(pkgs))
	}
}

func TestHTTPListInstalledEmpty(t *testing.T) {
	c, _ := initInstallTestClient(t)

	pkgs, err := c.ListInstalled(context.TODO())
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

func TestHTTPGetResponsesNotInstalled(t *testing.T) {
	c, _ := initInstallTestClient(t)

	_, err := c.GetResponses(context.TODO(), "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error getting responses for uninstalled package")
	}
}

func TestHTTPGetResponsesBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: emptyRepoRoot(t)})
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
	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", responses); err != nil {
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
	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientInstallPackageCallLog(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"k": "v"}); err != nil {
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

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := m.UninstallPackage(context.TODO(), "nginx", "1.0"); err != nil {
		t.Fatalf("MockClient.UninstallPackage: %v", err)
	}

	if len(m.Installed) != 0 {
		t.Fatalf("expected 0 installed after uninstall, got %d", len(m.Installed))
	}
}

func TestMockClientUninstallPackageNotInstalled(t *testing.T) {
	m := InitMockClient()

	err := m.UninstallPackage(context.TODO(), "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error uninstalling non-installed package")
	}
}

func TestMockClientUninstallPackageErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := fmt.Errorf("injected error")

	m.UninstallPkgErr = injected
	if err := m.UninstallPackage(context.TODO(), "nginx", "1.0"); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientUninstallPackageCallLog(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := m.UninstallPackage(context.TODO(), "nginx", "1.0"); err != nil {
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

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}
	if err := m.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{}); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	pkgs, err := m.ListInstalled(context.TODO())
	if err != nil {
		t.Fatalf("MockClient.ListInstalled: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 installed, got %d", len(pkgs))
	}
}

func TestMockClientListInstalledEmpty(t *testing.T) {
	m := InitMockClient()

	pkgs, err := m.ListInstalled(context.TODO())
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
	if _, err := m.ListInstalled(context.TODO()); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// --- MockClient GetResponses tests ---

func TestMockClientGetResponses(t *testing.T) {
	m := InitMockClient()

	responses := packages.Responses{"hostname": "example", "port": "8080"}
	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", responses); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	got, err := m.GetResponses(context.TODO(), "nginx", "1.0")
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

	_, err := m.GetResponses(context.TODO(), "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error for non-installed package")
	}
}

func TestMockClientGetResponsesErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := fmt.Errorf("injected error")

	m.GetResponsesErr = injected
	if _, err := m.GetResponses(context.TODO(), "nginx", "1.0"); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientGetResponsesReturnsCopy(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example"}); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	got, err := m.GetResponses(context.TODO(), "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}

	got["hostname"] = "mutated"

	if m.StoredResponses["nginx@1.0"]["hostname"] != "example" {
		t.Fatal("GetResponses should return a copy, not a reference")
	}
}

// --- Account HTTP endpoint tests ---

func initAccountTestClient(t *testing.T) (*SystemdClient, account.AuditManager) {
	t.Helper()
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
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

	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, AccountMgr: mgr, SessionMgr: sessMgr, AuditMgr: auditMgr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Bootstrap: create admin account (no auth required on empty DB) and authenticate
	if _, err := c.CreateAccount(context.TODO(), "testadmin", "adminpass", "admin@test.com", "555-0000", "Test Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "testadmin", "adminpass")
	if err != nil {
		t.Fatalf("bootstrap Authenticate: %v", err)
	}
	c.Token = resp.Token

	return c, auditMgr
}

func TestHTTPCreateAccount(t *testing.T) {
	c, _ := initAccountTestClient(t)

	acct, err := c.CreateAccount(context.TODO(), "alice", "password123", "alice@test.com", "555-1234", "Alice Smith", false)
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	if acct.Username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", acct.Username)
	}
	if acct.Email != "alice@test.com" {
		t.Fatalf("expected email %q, got %q", "alice@test.com", acct.Email)
	}
}

func TestHTTPCreateAccountDuplicate(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("first CreateAccount: %v", err)
	}

	_, err := c.CreateAccount(context.TODO(), "alice", "pass2", "c@d.com", "666", "Alice2", false)
	if err == nil {
		t.Fatal("expected error creating duplicate account")
	}
}

func TestHTTPGetAccount(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	acct, err := c.GetAccount(context.TODO(), "alice")
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}

	if acct.Username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", acct.Username)
	}
}

func TestHTTPGetAccountNotFound(t *testing.T) {
	c, _ := initAccountTestClient(t)

	_, err := c.GetAccount(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent account")
	}
}

func TestHTTPUpdateAccount(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	newEmail := "new@example.com"
	acct, err := c.UpdateAccount(context.TODO(), "alice", account.UpdateFields{Email: &newEmail})
	if err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}

	if acct.Email != "new@example.com" {
		t.Fatalf("expected email %q, got %q", "new@example.com", acct.Email)
	}
}

func TestHTTPDisableAccount(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create admin to perform the disable
	if _, err := c.CreateAccount(context.TODO(), "admin", "pass", "admin@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount admin: %v", err)
	}
	adminResp, err := c.Authenticate(context.TODO(), "admin", "pass")
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}

	if _, err := c.CreateAccount(context.TODO(), "alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}

	// disable alice using admin token
	req, err := http.NewRequest("POST", c.route("account/disable"), bytes.NewBufferString(`{"username":"alice"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", adminResp.Token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// verify alice is disabled (can still be fetched)
	acct, err := c.GetAccount(context.TODO(), "alice")
	if err != nil {
		t.Fatalf("GetAccount after disable: %v", err)
	}
	if !acct.Disabled {
		t.Fatal("expected account to be disabled")
	}

	// verify disabled account cannot authenticate
	_, err = c.Authenticate(context.TODO(), "alice", "pass")
	if err == nil {
		t.Fatal("expected error authenticating disabled account")
	}
}

func TestHTTPListAccounts(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}
	if _, err := c.CreateAccount(context.TODO(), "bob", "pass", "b@b.com", "666", "Bob", false); err != nil {
		t.Fatalf("CreateAccount bob: %v", err)
	}

	accounts, err := c.ListAccounts(context.TODO())
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}

	// 3 = testadmin (bootstrap) + alice + bob
	if len(accounts) != 3 {
		t.Fatalf("expected 3 accounts, got %d", len(accounts))
	}
}

func TestHTTPAuthenticate(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password123", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "alice", "password123")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if resp.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if resp.Account.Username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", resp.Account.Username)
	}
}

func TestHTTPAuthenticateWrongPassword(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password123", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	_, err := c.Authenticate(context.TODO(), "alice", "wrongpass")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestHTTPSessionLifecycle(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "alice", "pass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// list sessions using the token
	sessions, err := c.ListSessions(context.TODO(), resp.Token)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	// get username
	username, err := c.SessionUsername(context.TODO(), resp.Token)
	if err != nil {
		t.Fatalf("SessionUsername: %v", err)
	}
	if username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", username)
	}

	// revoke session
	if err := c.RevokeSession(context.TODO(), sessions[0].ID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	// token should no longer work for listing sessions
	_, err = c.ListSessions(context.TODO(), resp.Token)
	if err == nil {
		t.Fatal("expected error after session revoke")
	}
}

func TestHTTPSessionUsernameUnauthenticated(t *testing.T) {
	c, _ := initAccountTestClient(t)

	resp, err := c.HTTP.Get(fmt.Sprintf("%s/account/me", c.BaseURL))
	if err != nil {
		t.Fatalf("GET /account/me: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("body close: %v", err)
		}
	}()

	if resp.StatusCode != 401 {
		t.Fatalf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestHTTPSessionUsernameAuthenticated(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	authResp, err := c.Authenticate(context.TODO(), "alice", "pass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	username, err := c.SessionUsername(context.TODO(), authResp.Token)
	if err != nil {
		t.Fatalf("SessionUsername: %v", err)
	}
	if username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", username)
	}
}

// --- Admin middleware tests ---

func TestAdminMiddlewareBlocksNonAdmin(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create non-admin user
	if _, err := c.CreateAccount(context.TODO(), "user", "pass", "u@b.com", "555", "User", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "user", "pass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// try to install a package (admin-only)
	req, err := http.NewRequest("POST", c.route("packages/install"), bytes.NewBufferString(`{"name":"test","version":"1.0"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", resp.Token))
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if httpResp.StatusCode != 403 {
		t.Fatalf("expected 403 for non-admin, got %d", httpResp.StatusCode)
	}
}

func TestAdminMiddlewareAllowsAdmin(t *testing.T) {
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
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
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{{Name: "repo-a", URL: *u}}

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		AccountMgr:     mgr,
		SessionMgr:     sessMgr,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// create admin user
	if _, err := c.CreateAccount(context.TODO(), "admin", "pass", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "admin", "pass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// admin should be able to install
	req, err := http.NewRequest("POST", c.route("packages/install"), bytes.NewBufferString(`{"name":"nginx","version":"1.0","responses":{}}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", resp.Token))
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if httpResp.StatusCode != 200 {
		t.Fatalf("expected 200 for admin, got %d", httpResp.StatusCode)
	}
}

func TestAdminMiddlewareNoToken(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// try without any token
	req, err := http.NewRequest("POST", c.route("packages/install"), bytes.NewBufferString(`{"name":"test","version":"1.0"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if httpResp.StatusCode != 401 {
		t.Fatalf("expected 401 for missing token, got %d", httpResp.StatusCode)
	}
}

func TestHTTPPingIncludesAccountCount(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	// 2 = testadmin (bootstrap) + alice
	if ping.Accounts != 2 {
		t.Fatalf("expected 2 accounts in ping, got %d", ping.Accounts)
	}
}

// --- Audit log tests ---

func TestHTTPAuditLogLifecycle(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create admin and authenticate
	if _, err := c.CreateAccount(context.TODO(), "admin", "pass", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "admin", "pass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// perform an action (create another account) using admin token
	req, err := http.NewRequest("POST", c.route("account/create"), bytes.NewBufferString(`{"username":"alice","password":"pass","email":"a@b.com","phone":"555","real_name":"Alice","admin":false}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", resp.Token))
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	if err := httpResp.Body.Close(); err != nil {
		t.Errorf("resp.Body.Close: %v", err)
	}
	if httpResp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", httpResp.StatusCode)
	}

	// query audit log
	page, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}

	if len(page.Entries) == 0 {
		t.Fatal("expected at least one audit entry")
	}

	// find the create account entry
	found := false
	for _, e := range page.Entries {
		if e.Action == "create account" && e.Path == "/account/create" {
			found = true
			if !e.Success {
				t.Fatal("expected success to be true")
			}
			if e.Account != "admin" {
				t.Fatalf("expected account %q, got %q", "admin", e.Account)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected to find 'create account' audit entry")
	}
}

func TestHTTPAuditLogRequiresAdmin(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create non-admin user
	if _, err := c.CreateAccount(context.TODO(), "user", "pass", "u@b.com", "555", "User", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "user", "pass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// try to access audit log
	_, err = c.ListAuditLog(context.TODO(), account.AuditListOptions{}, resp.Token)
	if err == nil {
		t.Fatal("expected error for non-admin accessing audit log")
	}
}

func TestHTTPAuditLogPagination(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create admin
	if _, err := c.CreateAccount(context.TODO(), "admin", "pass", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "pass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// perform multiple actions via authenticated requests
	for i := 0; i < 5; i++ {
		username := fmt.Sprintf("user%d", i)
		body := fmt.Sprintf(`{"username":"%s","password":"pass","email":"%s@b.com","phone":"555","real_name":"User %d","admin":false}`, username, username, i)
		req, err := http.NewRequest("POST", c.route("account/create"), bytes.NewBufferString(body))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", resp.Token))
		req.Header.Set("Content-Type", "application/json")

		httpResp, err := c.HTTP.Do(req)
		if err != nil {
			t.Fatalf("HTTP Do: %v", err)
		}
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}

	// get first page of 2
	page, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{Limit: 2}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog page 1: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected HasMore to be true")
	}

	// get second page using cursor
	cursor := page.Entries[len(page.Entries)-1].ID
	page2, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{BeforeID: cursor, Limit: 2}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog page 2: %v", err)
	}
	if len(page2.Entries) < 1 {
		t.Fatal("expected at least 1 entry in page 2")
	}

	// entries should not overlap
	for _, e1 := range page.Entries {
		for _, e2 := range page2.Entries {
			if e1.ID == e2.ID {
				t.Fatalf("found duplicate ID %d across pages", e1.ID)
			}
		}
	}
}

func TestHTTPAuditLogExcludesSessionRoutes(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	// create user and authenticate
	if _, err := c.CreateAccount(context.TODO(), "alice", "pass", "a@b.com", "555", "Alice", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "alice", "pass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// call session routes
	if _, err := c.ListSessions(context.TODO(), resp.Token); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if _, err := c.SessionUsername(context.TODO(), resp.Token); err != nil {
		t.Fatalf("SessionUsername: %v", err)
	}

	// call ping
	if _, err := c.Ping(context.TODO()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	// check audit log - session routes and ping should not be logged
	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, e := range page.Entries {
		switch e.Path {
		case "/account/sessions", "/account/me", "/status/ping":
			t.Fatalf("expected path %q to be excluded from audit log", e.Path)
		}
	}
}

func TestHTTPAuditLogIncludesAuthRoutes(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	// create user and authenticate
	if _, err := c.CreateAccount(context.TODO(), "alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	if _, err := c.Authenticate(context.TODO(), "alice", "pass"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// check audit log for authenticate entry
	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	foundAuth := false
	for _, e := range page.Entries {
		if e.Path == "/account/authenticate" {
			foundAuth = true
			break
		}
	}
	if !foundAuth {
		t.Fatal("expected authenticate route to be in audit log")
	}
}

// --- requireAuth middleware tests ---

func TestHTTPRequireAuthBlocksUnauthenticated(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// try accessing a protected route without a token
	req, err := http.NewRequest("POST", c.route("storage/create"), bytes.NewBufferString(`{"name":"test"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if httpResp.StatusCode != 401 {
		t.Fatalf("expected 401 for unauthenticated request, got %d", httpResp.StatusCode)
	}
}

func TestHTTPRequireAuthAllowsAuthenticated(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create non-admin user and authenticate
	if _, err := c.CreateAccount(context.TODO(), "user", "pass", "u@b.com", "555", "User", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "user", "pass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// access a non-admin protected route (list accounts)
	req, err := http.NewRequest("GET", c.route("account"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", resp.Token))

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if httpResp.StatusCode != 200 {
		t.Fatalf("expected 200 for authenticated non-admin, got %d", httpResp.StatusCode)
	}
}

// --- CreateAccount auth tests ---

func TestHTTPCreateAccountBootstrap(t *testing.T) {
	// Fresh DB with no accounts — createAccount should succeed without auth
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
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
	ts := InitTestServer(ServerConfig{Storage: mock, AccountMgr: mgr, SessionMgr: sessMgr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// No token set — bootstrap should allow account creation
	acct, err := c.CreateAccount(context.TODO(), "first", "pass", "f@b.com", "555", "First", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	if acct.Username != "first" {
		t.Fatalf("expected username %q, got %q", "first", acct.Username)
	}
}

func TestHTTPCreateAccountRequiresAuthWhenAccountsExist(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// Clear the token — unauthenticated request
	c.Token = ""

	req, err := http.NewRequest("POST", c.route("account/create"), bytes.NewBufferString(`{"username":"mallory","password":"pass","email":"m@b.com","phone":"555","real_name":"Mallory","admin":false}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if httpResp.StatusCode != 401 {
		t.Fatalf("expected 401 for unauthenticated create, got %d", httpResp.StatusCode)
	}
}

func TestHTTPCreateAccountNonAdminForbidden(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create non-admin user
	if _, err := c.CreateAccount(context.TODO(), "user", "pass", "u@b.com", "555", "User", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "user", "pass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// try to create account with non-admin token
	req, err := http.NewRequest("POST", c.route("account/create"), bytes.NewBufferString(`{"username":"mallory","password":"pass","email":"m@b.com","phone":"555","real_name":"Mallory","admin":false}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", resp.Token))
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if httpResp.StatusCode != 403 {
		t.Fatalf("expected 403 for non-admin create, got %d", httpResp.StatusCode)
	}
}

func TestHTTPCreateAccountBootstrapAllDisabled(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// disable the bootstrap admin
	req, err := http.NewRequest("POST", c.route("account/disable"), bytes.NewBufferString(`{"username":"testadmin"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	if err := httpResp.Body.Close(); err != nil {
		t.Errorf("resp.Body.Close: %v", err)
	}
	if httpResp.StatusCode != 200 {
		t.Fatalf("expected 200 for disable, got %d", httpResp.StatusCode)
	}

	// all accounts disabled — bootstrap should allow unauthenticated create
	c.Token = ""
	acct, err := c.CreateAccount(context.TODO(), "newadmin", "pass", "n@b.com", "555", "New Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount after all disabled: %v", err)
	}
	if acct.Username != "newadmin" {
		t.Fatalf("expected username %q, got %q", "newadmin", acct.Username)
	}
}

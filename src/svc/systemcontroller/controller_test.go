package systemcontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// injectSubvol creates a subvolume and all intermediate parents in the mock
// controller, mimicking what storage.CreateFilesystem does automatically.
// Existing subvolumes are not duplicated.
func injectSubvol(t *testing.T, ctrl *storage.MockBtrFSController, name string, quota uint64) {
	t.Helper()
	parts := strings.Split(name, "/")
	for i := 1; i <= len(parts); i++ {
		intermediate := strings.Join(parts[:i], "/")
		exists := false
		for _, fs := range ctrl.GetFilesystems() {
			if fs.Name == intermediate {
				exists = true
				break
			}
		}
		if !exists {
			if err := ctrl.SubvolCreate(intermediate); err != nil {
				t.Fatalf("SubvolCreate %q: %v", intermediate, err)
			}
		}
	}
	if quota > 0 {
		if err := ctrl.QGroupLimit(name, quota); err != nil {
			t.Fatalf("QGroupLimit %q: %v", name, err)
		}
	}
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

func TestModifyFilesystemQuota(t *testing.T) {
	c, controller := initTestClient(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "test-vol"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	if err := c.ModifyFilesystem(context.TODO(), "test-vol", storage.Filesystem{Name: "test-vol", Quota: 1024}); err != nil {
		t.Fatalf("ModifyFilesystem quota: %v", err)
	}

	if controller.Quotas["test-vol"] != 1024 {
		t.Fatalf("expected quota 1024, got %d", controller.Quotas["test-vol"])
	}
}

func TestModifyFilesystemRename(t *testing.T) {
	c, _ := initTestClient(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "old-vol"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	if err := c.ModifyFilesystem(context.TODO(), "old-vol", storage.Filesystem{Name: "new-vol", Quota: 0}); err != nil {
		t.Fatalf("ModifyFilesystem rename: %v", err)
	}

	fs, err := c.ListFilesystems(context.TODO(), "", "")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	found := false
	for _, f := range fs {
		if f.Name == "new-vol" {
			found = true
		}
		if f.Name == "old-vol" {
			t.Fatal("old name should not exist after rename")
		}
	}
	if !found {
		t.Fatal("renamed filesystem not found")
	}
}

func TestModifyFilesystemRenameAndQuota(t *testing.T) {
	c, controller := initTestClient(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "vol"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	if err := c.ModifyFilesystem(context.TODO(), "vol", storage.Filesystem{Name: "renamed", Quota: 2048}); err != nil {
		t.Fatalf("ModifyFilesystem: %v", err)
	}

	if controller.Quotas["renamed"] != 2048 {
		t.Fatalf("expected quota 2048 on renamed, got %d", controller.Quotas["renamed"])
	}
}

func TestModifyFilesystemInvalidName(t *testing.T) {
	c, _ := initTestClient(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "vol"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	err := c.ModifyFilesystem(context.TODO(), "vol", storage.Filesystem{Name: "/bad"})
	if err == nil {
		t.Fatal("expected error for leading slash")
	}

	err = c.ModifyFilesystem(context.TODO(), "vol", storage.Filesystem{Name: ".."})
	if err == nil {
		t.Fatal("expected error for path traversal")
	}

	err = c.ModifyFilesystem(context.TODO(), "vol", storage.Filesystem{Name: "has space"})
	if err == nil {
		t.Fatal("expected error for space in name")
	}
}

func TestModifyFilesystemRejectsRoot(t *testing.T) {
	c, _ := initTestClient(t)

	err := c.ModifyFilesystem(context.TODO(), "", storage.Filesystem{Name: "test"})
	if err == nil {
		t.Fatal("expected error when modifying root filesystem")
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

	fs, err := c.ListFilesystems(context.TODO(), "", "")
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

	fs, err := c.ListFilesystems(context.TODO(), "", "")
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

	fs, err := c.ListFilesystems(context.TODO(), "app-", "")
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

	fs, err = c.ListFilesystems(context.TODO(), "data-", "")
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

	fs, err := c.ListFilesystems(context.TODO(), "nope", "")
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

// --- Root filesystem protection tests ---

func TestListFilesystemsExcludesRoot(t *testing.T) {
	c, controller := initTestClient(t)

	// Inject a root filesystem entry (empty name) directly into the mock
	controller.Lock.Lock()
	controller.Filesystems = append(controller.Filesystems, storage.SubvolInfo{Name: "", ID: 999})
	controller.Lock.Unlock()

	// Create a normal filesystem via API
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "user-vol"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	fs, err := c.ListFilesystems(context.TODO(), "", "")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	if len(fs) != 1 {
		t.Fatalf("expected 1 filesystem (root filtered out), got %d", len(fs))
	}

	if fs[0].Name != "user-vol" {
		t.Fatalf("expected %q, got %q", "user-vol", fs[0].Name)
	}
}

func TestRemoveFilesystemRejectsRoot(t *testing.T) {
	c, _ := initTestClient(t)

	err := c.RemoveFilesystem(context.TODO(), "")
	if err == nil {
		t.Fatal("expected error when removing root filesystem, got nil")
	}
}

// --- Reserved filesystem protection tests ---

func TestCreateFilesystemRejectsReserved(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{
		"installed",
		"installed/nginx",
		"installed/nginx/1.0/data",
		"uninstalled",
		"uninstalled/nginx",
		"uninstalled/nginx/1.0/data",
	} {
		t.Run(name, func(t *testing.T) {
			err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name})
			if err == nil {
				t.Fatalf("expected error when creating reserved filesystem %q", name)
			}
		})
	}
}

func TestRemoveFilesystemRejectsReserved(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{
		"installed",
		"installed/nginx",
		"installed/nginx/1.0/data",
		"uninstalled",
		"uninstalled/nginx",
		"uninstalled/nginx/1.0/data",
	} {
		t.Run(name, func(t *testing.T) {
			err := c.RemoveFilesystem(context.TODO(), name)
			if err == nil {
				t.Fatalf("expected error when removing reserved filesystem %q", name)
			}
		})
	}
}

func TestModifyFilesystemRejectsReserved(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{
		"installed",
		"installed/nginx",
		"uninstalled",
		"uninstalled/nginx",
	} {
		t.Run(name, func(t *testing.T) {
			err := c.ModifyFilesystem(context.TODO(), name, storage.Filesystem{Name: "renamed"})
			if err == nil {
				t.Fatalf("expected error when modifying reserved filesystem %q", name)
			}
		})
	}
}

func TestListFilesystemsExcludesReserved(t *testing.T) {
	c, controller := initTestClient(t)

	// Inject reserved filesystem entries directly into the mock.
	// Root subvolumes "installed" and "uninstalled" should be hidden.
	// Child subvolumes get their prefix stripped and state set.
	controller.Lock.Lock()
	controller.Filesystems = append(controller.Filesystems,
		storage.SubvolInfo{Name: "installed", ID: 100},
		storage.SubvolInfo{Name: "installed/nginx", ID: 101},
		storage.SubvolInfo{Name: "installed/nginx/1.0/data", ID: 102},
		storage.SubvolInfo{Name: "uninstalled", ID: 200},
		storage.SubvolInfo{Name: "uninstalled/nginx", ID: 201},
	)
	controller.Lock.Unlock()

	// Create a normal filesystem via API
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "user-vol"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	fs, err := c.ListFilesystems(context.TODO(), "", "")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	// Should see:
	//   user-vol (state="user")
	//   nginx (stripped from installed/nginx, state="installed")
	//   nginx/1.0/data (stripped from installed/nginx/1.0/data, state="installed")
	//   nginx (stripped from uninstalled/nginx, state="uninstalled")
	// Should NOT see: installed, uninstalled (root subvolumes).
	if len(fs) != 4 {
		t.Fatalf("expected 4 filesystems, got %d: %v", len(fs), fs)
	}

	type nameState struct {
		Name  string
		State string
	}
	got := make([]nameState, len(fs))
	for i, f := range fs {
		got[i] = nameState{Name: f.Name, State: f.State}
	}

	expected := []nameState{
		{Name: "user-vol", State: "user"},
		{Name: "nginx", State: "installed"},
		{Name: "nginx/1.0/data", State: "installed"},
		{Name: "nginx", State: "uninstalled"},
	}

	for _, want := range expected {
		found := false
		for _, g := range got {
			if g.Name == want.Name && g.State == want.State {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected {Name:%q, State:%q} to be present, got: %v", want.Name, want.State, got)
		}
	}

	// Verify root subvolumes are excluded.
	for _, f := range fs {
		if f.Name == "installed" || f.Name == "uninstalled" {
			t.Fatalf("expected root subvolume %q to be hidden, but it was visible", f.Name)
		}
	}
}

func TestCreateFilesystemAllowsNonReserved(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{
		"my-installed",
		"installedbackup",
		"data",
		"user-vol",
	} {
		t.Run(name, func(t *testing.T) {
			err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name})
			if err != nil {
				t.Fatalf("unexpected error creating %q: %v", name, err)
			}
		})
	}
}

// --- Full lifecycle tests ---

func TestCreateListRemoveLifecycle(t *testing.T) {
	c, _ := initTestClient(t)

	// Start empty
	fs, err := c.ListFilesystems(context.TODO(), "", "")
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
	fs, err = c.ListFilesystems(context.TODO(), "", "")
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
	fs, err = c.ListFilesystems(context.TODO(), "", "")
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

	fs, err := c.ListFilesystems(context.TODO(), "", "")
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

	fs, err = c.ListFilesystems(context.TODO(), "", "")
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

	fs, err := m.ListFilesystems(context.TODO(), "", "")
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

	fs, err := m.ListFilesystems(context.TODO(), "", "")
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

	fs, err := m.ListFilesystems(context.TODO(), "app-", "")
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
	if _, err := m.ListFilesystems(context.TODO(), "", ""); err != injected {
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
	if _, err := m.ListFilesystems(context.TODO(), "", ""); err != nil {
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

	repos, err := m.ListRepositories(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("MockClient.ListRepositories: %v", err)
	}

	if len(repos.Entries) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(repos.Entries))
	}

	if repos.Entries[0].URL != "https://example.com/repo.git" {
		t.Fatalf("expected URL %q, got %q", "https://example.com/repo.git", repos.Entries[0].URL)
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

	repos, err := m.ListRepositories(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("MockClient.ListRepositories: %v", err)
	}

	if len(repos.Entries) != 0 {
		t.Fatalf("expected 0 repositories, got %d", len(repos.Entries))
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
	if _, err := m.ListRepositories(context.TODO(), ListParams{}); err != injected {
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
	if _, err := m.ListRepositories(context.TODO(), ListParams{}); err != nil {
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

	repos, err := c.ListRepositories(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repos.Entries) != 0 {
		t.Fatalf("expected empty list, got %d", len(repos.Entries))
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

	repos, err := c.ListRepositories(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repos.Entries) != 2 {
		t.Fatalf("expected 2 repositories, got %d", len(repos.Entries))
	}

	if repos.Entries[0].Name != "repo-a" || repos.Entries[1].Name != "repo-b" {
		t.Fatalf("unexpected repo names: %v", repos.Entries)
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

func TestHTTPRefreshRepositories(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if _, err := c.RefreshRepositories(context.TODO()); err != nil {
		t.Fatalf("unexpected error: %v", err)
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

	repos, err := c.ListRepositories(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}

	if len(repos.Entries) != 0 {
		t.Fatalf("expected 0 repositories after failed add, got %d", len(repos.Entries))
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

		err = c.AddRepository(context.TODO(), "", "https://example.com/repo.git", "", "password1")
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

	pkgs, err := c.ListPackages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 packages, got %d", len(pkgs.Entries))
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

	pkgs, err := c.ListPackages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs.Entries) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs.Entries))
	}

	// results are sorted by name
	if pkgs.Entries[0] != "nginx@2.0" {
		t.Fatalf("expected nginx@2.0, got %s", pkgs.Entries[0])
	}
	if pkgs.Entries[1] != "redis@7.0" {
		t.Fatalf("expected redis@7.0, got %s", pkgs.Entries[1])
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

	pkgs, err := c.ListPackages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs.Entries) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs.Entries))
	}

	// nginx should be 3.0 (higher version from repo-b wins)
	if pkgs.Entries[0] != "nginx@3.0" {
		t.Fatalf("expected nginx@3.0, got %s", pkgs.Entries[0])
	}
	if pkgs.Entries[1] != "redis@7.0" {
		t.Fatalf("expected redis@7.0, got %s", pkgs.Entries[1])
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

// --- HTTP GetPackageQuestionsByIdentity tests ---

func TestHTTPGetPackageQuestionsByIdentity(t *testing.T) {
	c, _ := initInstallTestClient(t)

	questions, err := c.GetPackageQuestionsByIdentity(context.TODO(), "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetPackageQuestionsByIdentity: %v", err)
	}

	if len(questions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(questions))
	}
	if questions["hostname"].Query != "What hostname should nginx serve?" {
		t.Fatalf("expected hostname query, got %q", questions["hostname"].Query)
	}
	if questions["port"].Query != "What external port should nginx listen on?" {
		t.Fatalf("expected port query, got %q", questions["port"].Query)
	}
}

func TestHTTPGetPackageQuestionsByIdentityNoQuestions(t *testing.T) {
	c, _ := initInstallTestClient(t)

	questions, err := c.GetPackageQuestionsByIdentity(context.TODO(), "nginx", "2.0")
	if err != nil {
		t.Fatalf("GetPackageQuestionsByIdentity: %v", err)
	}

	if len(questions) != 0 {
		t.Fatalf("expected 0 questions for nginx@2.0, got %d", len(questions))
	}
}

func TestHTTPGetPackageQuestionsByIdentityNotFound(t *testing.T) {
	c, _ := initInstallTestClient(t)

	_, err := c.GetPackageQuestionsByIdentity(context.TODO(), "nonexistent", "1.0")
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
}

// --- MockClient ListPackages tests ---

func TestMockClientListPackages(t *testing.T) {
	m := InitMockClient()
	m.Packages = []string{"nginx@2.0", "redis@7.0"}

	pkgs, err := m.ListPackages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("MockClient.ListPackages: %v", err)
	}

	if len(pkgs.Entries) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs.Entries))
	}

	if pkgs.Entries[0] != "nginx@2.0" {
		t.Fatalf("expected nginx@2.0, got %s", pkgs.Entries[0])
	}
}

func TestMockClientListPackagesEmpty(t *testing.T) {
	m := InitMockClient()

	pkgs, err := m.ListPackages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("MockClient.ListPackages: %v", err)
	}

	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 packages, got %d", len(pkgs.Entries))
	}
}

func TestMockClientListPackagesErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := fmt.Errorf("injected error")

	m.ListPkgErr = injected
	if _, err := m.ListPackages(context.TODO(), ListParams{}); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientListPackagesCallLog(t *testing.T) {
	m := InitMockClient()
	m.Packages = []string{"nginx@1.0"}

	if _, err := m.ListPackages(context.TODO(), ListParams{}); err != nil {
		t.Fatalf("MockClient.ListPackages (first call): %v", err)
	}
	if _, err := m.ListPackages(context.TODO(), ListParams{}); err != nil {
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

// --- MockClient GetPackageQuestionsByIdentity tests ---

func TestMockClientGetPackageQuestionsByIdentity(t *testing.T) {
	m := InitMockClient()
	m.Questions = map[string]map[string]packages.Question{
		"nginx@1.0": {
			"hostname": {Query: "What hostname?", Type: packages.Hostname},
		},
	}

	questions, err := m.GetPackageQuestionsByIdentity(context.TODO(), "nginx", "1.0")
	if err != nil {
		t.Fatalf("MockClient.GetPackageQuestionsByIdentity: %v", err)
	}

	if len(questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(questions))
	}
	if questions["hostname"].Query != "What hostname?" {
		t.Fatalf("expected %q, got %q", "What hostname?", questions["hostname"].Query)
	}
}

func TestMockClientGetPackageQuestionsByIdentityNotFound(t *testing.T) {
	m := InitMockClient()

	_, err := m.GetPackageQuestionsByIdentity(context.TODO(), "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
}

func TestMockClientGetPackageQuestionsByIdentityErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := fmt.Errorf("injected error")

	m.Questions = map[string]map[string]packages.Question{
		"nginx@1.0": {
			"hostname": {Query: "What hostname?"},
		},
	}

	m.QuestionsIdentityErr = injected
	if _, err := m.GetPackageQuestionsByIdentity(context.TODO(), "nginx", "1.0"); err != injected {
		t.Fatalf("expected injected error, got %v", err)
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
	nginx10 := `image: nginx:1.0
environment:
  NGINX_HOST: "@hostname@"
network:
  external:
    "@port@": "80"
  internal: {}
volumes: {}
questions:
  hostname:
    query: "What hostname should nginx serve?"
    type: hostname
  port:
    query: "What external port should nginx listen on?"
    type: port
notes:
  URL: "http://@hostname@:@port@"
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)
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

	responses := packages.Responses{"hostname": "example", "port": "8080"}
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", responses, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	calls := inst.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Method != "ListInstalled" {
		t.Fatalf("expected ListInstalled call, got %q", calls[0].Method)
	}
	if calls[1].Method != "Install" {
		t.Fatalf("expected Install call, got %q", calls[1].Method)
	}
	if calls[1].Args[0].(string) != "repo-a" {
		t.Fatalf("expected repoName %q, got %v", "repo-a", calls[1].Args[0])
	}
	if calls[1].Args[1].(string) != "nginx" {
		t.Fatalf("expected pkgName %q, got %v", "nginx", calls[1].Args[1])
	}
	if calls[1].Args[2].(string) != "1.0" {
		t.Fatalf("expected version %q, got %v", "1.0", calls[1].Args[2])
	}
}

func TestHTTPInstallPackageNotFound(t *testing.T) {
	c, _ := initInstallTestClient(t)

	err := c.InstallPackage(context.TODO(),"nonexistent", "1.0", packages.Responses{}, false, "")
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
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := c.UninstallPackage(context.TODO(), "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	calls := inst.GetCalls()
	// ListInstalled + Install + SetDisabled + Uninstall + ListInstalled = 5 calls
	if len(calls) != 5 {
		t.Fatalf("expected 5 calls, got %d: %v", len(calls), calls)
	}
	if calls[2].Method != "SetDisabled" {
		t.Fatalf("expected SetDisabled call, got %q", calls[2].Method)
	}
	if calls[3].Method != "Uninstall" {
		t.Fatalf("expected Uninstall call, got %q", calls[3].Method)
	}
	if calls[4].Method != "ListInstalled" {
		t.Fatalf("expected ListInstalled call, got %q", calls[4].Method)
	}
}

func TestHTTPUninstallPackageNotInstalled(t *testing.T) {
	c, _ := initInstallTestClient(t)

	err := c.UninstallPackage(context.TODO(), "nginx", "1.0", false)
	if err == nil {
		t.Fatal("expected error uninstalling package that is not installed")
	}
}

func TestHTTPUninstallPackageWithPurge(t *testing.T) {
	c, _ := initInstallTestClient(t)

	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify volumes were NOT created (nginx 1.0 has no volumes in the test fixture).
	// Install with purge=true should still succeed even with no volumes.
	if err := c.UninstallPackage(context.TODO(), "nginx", "1.0", true); err != nil {
		t.Fatalf("UninstallPackage with purge: %v", err)
	}
}

func initInstallWithVolumesTestClient(t *testing.T) (*SystemdClient, *storage.MockBtrFSController) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	controller := mock.Controller.(*storage.MockBtrFSController)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}
	nginx10 := `image: nginx:1.0
environment:
  NGINX_HOST: "@hostname@"
network:
  external: {}
  internal: {}
volumes:
  html:
    mountpoint: /var/www/html
  logs:
    mountpoint: /var/log/nginx
    quota: 2048
questions:
  hostname:
    query: "What hostname should nginx serve?"
    type: hostname
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, controller
}

func TestHTTPUninstallPackagePurgesVolumes(t *testing.T) {
	c, controller := initInstallWithVolumesTestClient(t)

	// Install a package that defines volumes.
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "example"}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify volumes were created.
	before := controller.GetFilesystems()
	volNames := map[string]bool{}
	for _, fs := range before {
		volNames[fs.Name] = true
	}
	if !volNames["installed/nginx/1.0/html"] {
		t.Fatal("expected installed/nginx/1.0/html volume to exist after install")
	}
	if !volNames["installed/nginx/1.0/logs"] {
		t.Fatal("expected installed/nginx/1.0/logs volume to exist after install")
	}

	// Uninstall with purge.
	if err := c.UninstallPackage(context.TODO(), "nginx", "1.0", true); err != nil {
		t.Fatalf("UninstallPackage with purge: %v", err)
	}

	// Verify all nginx volumes are gone.
	after := controller.GetFilesystems()
	for _, fs := range after {
		if fs.Name == "installed/nginx" || strings.HasPrefix(fs.Name, "installed/nginx/") {
			t.Fatalf("expected all nginx volumes purged, found %q", fs.Name)
		}
	}
}

func TestHTTPUninstallPackageWithoutPurgePreservesVolumes(t *testing.T) {
	c, controller := initInstallWithVolumesTestClient(t)

	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "example"}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Uninstall WITHOUT purge.
	if err := c.UninstallPackage(context.TODO(), "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	// Verify volumes are renamed to uninstalled/.
	after := controller.GetFilesystems()
	volNames := map[string]bool{}
	for _, fs := range after {
		volNames[fs.Name] = true
	}
	if !volNames["uninstalled/nginx/1.0/html"] {
		t.Fatal("expected uninstalled/nginx/1.0/html volume preserved after uninstall without purge")
	}
	if !volNames["uninstalled/nginx/1.0/logs"] {
		t.Fatal("expected uninstalled/nginx/1.0/logs volume preserved after uninstall without purge")
	}
}

func TestHTTPPurgeVolumes(t *testing.T) {
	c, controller := initTestClient(t)

	// Inject package volume entries directly into the mock controller.
	injectSubvol(t, controller, "installed/nginx/1.0/html", 1024)
	injectSubvol(t, controller, "installed/nginx/1.0/logs", 2048)
	injectSubvol(t, controller, "installed/other/1.0/data", 512)

	if err := c.PurgeVolumes(context.TODO(), "nginx"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	remaining := controller.GetFilesystems()
	for _, fs := range remaining {
		if fs.Name == "installed/nginx" || strings.HasPrefix(fs.Name, "installed/nginx/") {
			t.Fatalf("expected nginx volumes to be purged, found %s", fs.Name)
		}
	}

	found := false
	for _, fs := range remaining {
		if fs.Name == "installed/other/1.0/data" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected installed/other/1.0/data volume to be preserved")
	}
}

func TestHTTPPurgeVolumesVerifiesControllerState(t *testing.T) {
	c, controller := initTestClient(t)

	// Inject package volume hierarchy directly into the mock controller.
	for _, name := range []string{"installed/nginx/1.0/html", "installed/nginx/1.0/logs", "installed/nginx/1.0/cache/tmp"} {
		injectSubvol(t, controller, name, 0)
	}

	// Also create a volume for a different package.
	injectSubvol(t, controller, "installed/redis/7.0/data", 0)

	// Verify volumes exist before purge.
	before := controller.GetFilesystems()
	expectedBefore := map[string]bool{
		"installed":                     true,
		"installed/nginx":                true,
		"installed/nginx/1.0":            true,
		"installed/nginx/1.0/html":       true,
		"installed/nginx/1.0/logs":       true,
		"installed/nginx/1.0/cache":      true,
		"installed/nginx/1.0/cache/tmp":  true,
		"installed/redis":                true,
		"installed/redis/7.0":            true,
		"installed/redis/7.0/data":       true,
	}
	for _, fs := range before {
		if !expectedBefore[fs.Name] {
			t.Fatalf("unexpected filesystem before purge: %q", fs.Name)
		}
		delete(expectedBefore, fs.Name)
	}
	if len(expectedBefore) > 0 {
		t.Fatalf("missing filesystems before purge: %v", expectedBefore)
	}

	// Purge nginx.
	if err := c.PurgeVolumes(context.TODO(), "nginx"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	// Verify at the controller level that nginx volumes are gone.
	after := controller.GetFilesystems()
	for _, fs := range after {
		if fs.Name == "installed/nginx" || strings.HasPrefix(fs.Name, "installed/nginx/") {
			t.Fatalf("expected all nginx volumes purged, found %q in controller", fs.Name)
		}
	}

	// Verify redis volumes are untouched.
	redisFound := map[string]bool{}
	for _, fs := range after {
		redisFound[fs.Name] = true
	}
	if !redisFound["installed/redis"] {
		t.Fatal("expected installed/redis parent volume to survive purge")
	}
	if !redisFound["installed/redis/7.0/data"] {
		t.Fatal("expected installed/redis/7.0/data volume to survive purge")
	}
}

func TestHTTPPurgeVolumesSimilarPrefix(t *testing.T) {
	c, controller := initTestClient(t)

	// Inject volumes with similar prefixes: nginx and nginx2 are separate packages.
	for _, name := range []string{"installed/nginx/1.0/html", "installed/nginx2/1.0/data"} {
		injectSubvol(t, controller, name, 0)
	}

	// Purge only nginx.
	if err := c.PurgeVolumes(context.TODO(), "nginx"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	// Verify nginx is gone, nginx2 is intact.
	after := controller.GetFilesystems()
	found := map[string]bool{}
	for _, fs := range after {
		found[fs.Name] = true
	}

	if found["installed/nginx"] || found["installed/nginx/1.0/html"] {
		t.Fatal("expected nginx volumes to be purged")
	}
	if !found["installed/nginx2"] {
		t.Fatal("expected installed/nginx2 parent to survive")
	}
	if !found["installed/nginx2/1.0/data"] {
		t.Fatal("expected installed/nginx2/1.0/data to survive")
	}
}

func TestHTTPPurgeVolumesDeepNesting(t *testing.T) {
	c, controller := initTestClient(t)

	// Inject deeply nested volumes: installed/pkg/1.0/a/b/c/d.
	injectSubvol(t, controller, "installed/pkg/1.0/a/b/c/d", 0)

	// Purge the package.
	if err := c.PurgeVolumes(context.TODO(), "pkg"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	// Everything under installed/pkg should be gone; only the installed root remains.
	after := controller.GetFilesystems()
	for _, fs := range after {
		if fs.Name == "installed/pkg" || strings.HasPrefix(fs.Name, "installed/pkg/") {
			t.Fatalf("expected all pkg volumes purged, found %q", fs.Name)
		}
	}
}

func TestHTTPPurgeVolumesEmpty(t *testing.T) {
	c, controller := initTestClient(t)

	// Inject a volume for a different package.
	injectSubvol(t, controller, "installed/redis/7.0/data", 0)

	// Purge a package that has no volumes.
	if err := c.PurgeVolumes(context.TODO(), "nginx"); err != nil {
		t.Fatalf("PurgeVolumes should succeed for nonexistent package: %v", err)
	}

	// Redis volumes must be untouched.
	after := controller.GetFilesystems()
	found := map[string]bool{}
	for _, fs := range after {
		found[fs.Name] = true
	}
	if !found["installed/redis"] || !found["installed/redis/7.0/data"] {
		t.Fatalf("expected redis volumes to be intact, got: %v", after)
	}
}

func TestHTTPPurgeVolumesWithQuotas(t *testing.T) {
	c, controller := initTestClient(t)

	// Inject volumes with quotas.
	injectSubvol(t, controller, "installed/nginx/1.0/html", 1024)
	injectSubvol(t, controller, "installed/nginx/1.0/logs", 2048)

	// Purge.
	if err := c.PurgeVolumes(context.TODO(), "nginx"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	// Verify all nginx volumes gone from controller.
	after := controller.GetFilesystems()
	for _, fs := range after {
		if fs.Name == "installed/nginx" || strings.HasPrefix(fs.Name, "installed/nginx/") {
			t.Fatalf("expected all nginx volumes purged, found %q", fs.Name)
		}
	}

	// Verify quotas are cleaned up too.
	for k := range controller.Quotas {
		if strings.HasPrefix(k, "installed/nginx/") {
			t.Fatalf("expected nginx quotas cleaned up, found quota for %q", k)
		}
	}
}

func TestHTTPPurgeVolumesMultipleChildren(t *testing.T) {
	c, controller := initTestClient(t)

	// Inject many children under a single parent.
	children := []string{
		"installed/app/1.0/data", "installed/app/1.0/logs", "installed/app/1.0/cache",
		"installed/app/1.0/tmp", "installed/app/1.0/config",
	}
	for _, name := range children {
		injectSubvol(t, controller, name, 0)
	}

	if err := c.PurgeVolumes(context.TODO(), "app"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	after := controller.GetFilesystems()
	for _, fs := range after {
		if fs.Name == "installed/app" || strings.HasPrefix(fs.Name, "installed/app/") {
			t.Fatalf("expected all app volumes purged, found %q", fs.Name)
		}
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
	nginx10 := `image: nginx:1.0
environment:
  NGINX_HOST: "@hostname@"
network:
  external: {}
  internal: {}
volumes: {}
questions:
  hostname:
    query: "What hostname should nginx serve?"
    type: hostname
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)

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

	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "testhost"}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls, got %d: %v", len(calls), calls)
	}

	// 1. InstallUnit
	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	if calls[0].Args[0].(string) != "town-os-nginx.service" {
		t.Fatalf("call 0: expected unit name %q, got %v", "town-os-nginx.service", calls[0].Args[0])
	}

	// 2. SetStatus(start)
	if calls[1].Method != "SetStatus" {
		t.Fatalf("call 1: expected SetStatus, got %q", calls[1].Method)
	}
	if calls[1].Args[1].(systemd.StatusAction) != systemd.Start {
		t.Fatalf("call 1: expected action %q, got %v", systemd.Start, calls[1].Args[1])
	}
}

func TestHTTPUninstallPackageRemovesSystemdUnit(t *testing.T) {
	c, _, sd := initInstallWithSystemdTestClient(t)

	// Install first so uninstall can succeed.
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "testhost"}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := c.UninstallPackage(context.TODO(), "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	calls := sd.GetCalls()
	// Install produces 2 calls (InstallUnit, start)
	// Uninstall produces 2 calls (stop, UninstallUnit)
	if len(calls) != 4 {
		t.Fatalf("expected 4 systemd calls, got %d: %v", len(calls), calls)
	}

	// Uninstall calls: indices 2, 3
	// 2. SetStatus(stop)
	if calls[2].Method != "SetStatus" {
		t.Fatalf("call 2: expected SetStatus, got %q", calls[2].Method)
	}
	if calls[2].Args[1].(systemd.StatusAction) != systemd.Stop {
		t.Fatalf("call 2: expected action %q, got %v", systemd.Stop, calls[2].Args[1])
	}

	// 3. UninstallUnit
	if calls[3].Method != "UninstallUnit" {
		t.Fatalf("call 3: expected UninstallUnit, got %q", calls[3].Method)
	}
	if calls[3].Args[0].(string) != "town-os-nginx.service" {
		t.Fatalf("call 3: expected unit name %q, got %v", "town-os-nginx.service", calls[3].Args[0])
	}
}

// --- Install creates storage volumes ---

func TestHTTPInstallPackageCreatesVolumes(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	pkgYAML := `image: nginx:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
    quota: 1gb
  logs:
    mountpoint: /var/log/app
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "myapp", "1.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(),"myapp", "1.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify volumes were created under installed/<name>/<version>/.
	fs := mockCtrl.GetFilesystems()

	foundData := false
	foundLogs := false
	for _, f := range fs {
		if f.Name == "installed/myapp/1.0/data" {
			foundData = true
		}
		if f.Name == "installed/myapp/1.0/logs" {
			foundLogs = true
		}
	}

	if !foundData {
		t.Fatalf("expected filesystem installed/myapp/1.0/data to be created, got: %v", fs)
	}
	if !foundLogs {
		t.Fatalf("expected filesystem installed/myapp/1.0/logs to be created, got: %v", fs)
	}
}

func TestHTTPInstallPackageCreatesVolumesWithQuota(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	pkgYAML := `image: nginx:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
    quota: 2gb
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "myapp", "1.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(),"myapp", "1.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify quota was set on the volume.
	quota := mockCtrl.Quotas["installed/myapp/1.0/data"]
	if quota != 2147483648 {
		t.Fatalf("expected quota 2147483648, got %d", quota)
	}
}

func TestHTTPInstallPackageNoVolumes(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
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
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// No volumes to create — mock storage should be empty.
	fs := mockCtrl.GetFilesystems()
	if len(fs) != 0 {
		t.Fatalf("expected no filesystems, got %v", fs)
	}
}

func TestHTTPInstallPackageVolumesWithTemplatedQuota(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	pkgYAML := `image: postgres:16
environment: {}
network:
  external: {}
  internal: {}
volumes:
  pgdata:
    mountpoint: /var/lib/postgresql/data
    quota: "@size@"
questions:
  size:
    query: "How much storage for the database?"
    type: bytes
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "postgres", "16.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(),"postgres", "16.0", packages.Responses{"size": "10gb"}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify the volume was created with the templated quota.
	fs := mockCtrl.GetFilesystems()
	found := false
	for _, f := range fs {
		if f.Name == "installed/postgres/16.0/pgdata" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected filesystem installed/postgres/16.0/pgdata to be created, got: %v", fs)
	}

	// 10GB = 10 * 1024^3 = 10737418240
	quota := mockCtrl.Quotas["installed/postgres/16.0/pgdata"]
	if quota != 10737418240 {
		t.Fatalf("expected quota 10737418240, got %d", quota)
	}
}

func TestHTTPListInstalled(t *testing.T) {
	c, _ := initInstallTestClient(t)

	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, ""); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}
	if err := c.InstallPackage(context.TODO(),"nginx", "2.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage nginx@2.0: %v", err)
	}

	pkgs, err := c.ListInstalled(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(pkgs.Entries) != 2 {
		t.Fatalf("expected 2 installed, got %d", len(pkgs.Entries))
	}
}

func TestHTTPListInstalledEmpty(t *testing.T) {
	c, _ := initInstallTestClient(t)

	pkgs, err := c.ListInstalled(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 installed, got %d", len(pkgs.Entries))
	}
}

func TestHTTPGetResponses(t *testing.T) {
	c, _ := initInstallTestClient(t)

	responses := packages.Responses{"hostname": "example", "port": "8080"}
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", responses, false, ""); err != nil {
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

func TestHTTPGetInstalledInfo(t *testing.T) {
	c, _ := initInstallTestClient(t)

	// Install nginx with responses
	responses := packages.Responses{"hostname": "testhost", "port": "8081"}
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", responses, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	info, err := c.GetInstalledInfo(context.TODO(), "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetInstalledInfo: %v", err)
	}

	// Verify questions
	if info.Questions["hostname"].Query != "What hostname should nginx serve?" {
		t.Fatalf("expected hostname query, got %q", info.Questions["hostname"].Query)
	}
	if info.Questions["port"].Query != "What external port should nginx listen on?" {
		t.Fatalf("expected port query, got %q", info.Questions["port"].Query)
	}

	// Verify responses
	if info.Responses["hostname"] != "testhost" {
		t.Fatalf("expected hostname=testhost, got %q", info.Responses["hostname"])
	}
	if info.Responses["port"] != "8081" {
		t.Fatalf("expected port=8081, got %q", info.Responses["port"])
	}

	// Verify compiled notes
	if info.Notes["URL"] != "http://testhost:8081" {
		t.Fatalf("expected URL=http://testhost:8081, got %q", info.Notes["URL"])
	}
}

func TestHTTPGetInstalledInfoNotInstalled(t *testing.T) {
	c, _ := initInstallTestClient(t)

	_, err := c.GetInstalledInfo(context.TODO(), "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error getting info for uninstalled package")
	}
}

// --- MockClient InstallPackage tests ---

func TestMockClientInstallPackage(t *testing.T) {
	m := InitMockClient()

	responses := packages.Responses{"hostname": "example"}
	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, ""); err != nil {
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
	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, ""); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientInstallPackageCallLog(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"k": "v"}, false, ""); err != nil {
		t.Fatalf("MockClient.InstallPackage: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "InstallPackage" {
		t.Fatalf("expected method InstallPackage, got %q", calls[0].Method)
	}
	if len(calls[0].Args) != 5 {
		t.Fatalf("expected 5 args, got %d", len(calls[0].Args))
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

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := m.UninstallPackage(context.TODO(), "nginx", "1.0", false); err != nil {
		t.Fatalf("MockClient.UninstallPackage: %v", err)
	}

	if len(m.Installed) != 0 {
		t.Fatalf("expected 0 installed after uninstall, got %d", len(m.Installed))
	}
}

func TestMockClientUninstallPackageNotInstalled(t *testing.T) {
	m := InitMockClient()

	err := m.UninstallPackage(context.TODO(), "nginx", "1.0", false)
	if err == nil {
		t.Fatal("expected error uninstalling non-installed package")
	}
}

func TestMockClientUninstallPackageErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := fmt.Errorf("injected error")

	m.UninstallPkgErr = injected
	if err := m.UninstallPackage(context.TODO(), "nginx", "1.0", false); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientUninstallPackageCallLog(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := m.UninstallPackage(context.TODO(), "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[1].Method != "UninstallPackage" {
		t.Fatalf("expected method UninstallPackage, got %q", calls[1].Method)
	}
	if len(calls[1].Args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(calls[1].Args))
	}
}

// --- MockClient UninstallPackage purge tests ---

func TestMockClientUninstallPackagePurgesVolumes(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	m.Filesystems["installed/nginx"] = storage.Filesystem{Name: "installed/nginx"}
	m.Filesystems["installed/nginx/1.0/html"] = storage.Filesystem{Name: "installed/nginx/1.0/html", Quota: 1024}
	m.Filesystems["installed/nginx/1.0/logs"] = storage.Filesystem{Name: "installed/nginx/1.0/logs", Quota: 2048}
	m.Filesystems["installed/other/1.0/data"] = storage.Filesystem{Name: "installed/other/1.0/data"}

	if err := m.UninstallPackage(context.TODO(), "nginx", "1.0", true); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	if len(m.Installed) != 0 {
		t.Fatalf("expected 0 installed after uninstall, got %d", len(m.Installed))
	}

	if _, ok := m.Filesystems["installed/nginx"]; ok {
		t.Fatal("expected installed/nginx parent volume to be purged")
	}
	if _, ok := m.Filesystems["installed/nginx/1.0/html"]; ok {
		t.Fatal("expected installed/nginx/1.0/html volume to be purged")
	}
	if _, ok := m.Filesystems["installed/nginx/1.0/logs"]; ok {
		t.Fatal("expected installed/nginx/1.0/logs volume to be purged")
	}
	if _, ok := m.Filesystems["installed/other/1.0/data"]; !ok {
		t.Fatal("expected installed/other/1.0/data volume to be preserved")
	}
}

func TestMockClientUninstallPackageNoPurge(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	m.Filesystems["installed/nginx/1.0/html"] = storage.Filesystem{Name: "installed/nginx/1.0/html", Quota: 1024}

	if err := m.UninstallPackage(context.TODO(), "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	if _, ok := m.Filesystems["installed/nginx/1.0/html"]; !ok {
		t.Fatal("expected installed/nginx/1.0/html volume to be preserved when purge is false")
	}
}

// --- MockClient PurgeVolumes tests ---

func TestMockClientPurgeVolumes(t *testing.T) {
	m := InitMockClient()

	m.Filesystems["installed/nginx"] = storage.Filesystem{Name: "installed/nginx"}
	m.Filesystems["installed/nginx/1.0/html"] = storage.Filesystem{Name: "installed/nginx/1.0/html"}
	m.Filesystems["installed/nginx/1.0/logs"] = storage.Filesystem{Name: "installed/nginx/1.0/logs"}
	m.Filesystems["installed/other/1.0/data"] = storage.Filesystem{Name: "installed/other/1.0/data"}

	if err := m.PurgeVolumes(context.TODO(), "nginx"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	if _, ok := m.Filesystems["installed/nginx"]; ok {
		t.Fatal("expected installed/nginx parent volume to be purged")
	}
	if _, ok := m.Filesystems["installed/nginx/1.0/html"]; ok {
		t.Fatal("expected installed/nginx/1.0/html volume to be purged")
	}
	if _, ok := m.Filesystems["installed/nginx/1.0/logs"]; ok {
		t.Fatal("expected installed/nginx/1.0/logs volume to be purged")
	}
	if _, ok := m.Filesystems["installed/other/1.0/data"]; !ok {
		t.Fatal("expected installed/other/1.0/data volume to be preserved")
	}

	calls := m.GetCalls()
	if len(calls) != 1 || calls[0].Method != "PurgeVolumes" {
		t.Fatalf("expected 1 PurgeVolumes call, got %v", calls)
	}
}

// --- MockClient ListInstalled tests ---

func TestMockClientListInstalled(t *testing.T) {
	m := InitMockClient()

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}
	if err := m.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	pkgs, err := m.ListInstalled(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("MockClient.ListInstalled: %v", err)
	}

	if len(pkgs.Entries) != 2 {
		t.Fatalf("expected 2 installed, got %d", len(pkgs.Entries))
	}
}

func TestMockClientListInstalledEmpty(t *testing.T) {
	m := InitMockClient()

	pkgs, err := m.ListInstalled(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("MockClient.ListInstalled: %v", err)
	}

	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 installed, got %d", len(pkgs.Entries))
	}
}

func TestMockClientListInstalledErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := fmt.Errorf("injected error")

	m.ListInstalledErr = injected
	if _, err := m.ListInstalled(context.TODO(), ListParams{}); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// --- MockClient GetResponses tests ---

func TestMockClientGetResponses(t *testing.T) {
	m := InitMockClient()

	responses := packages.Responses{"hostname": "example", "port": "8080"}
	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, ""); err != nil {
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

	if err := m.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example"}, false, ""); err != nil {
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

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("first CreateAccount: %v", err)
	}

	_, err := c.CreateAccount(context.TODO(), "alice", "password2", "c@d.com", "666", "Alice2", false)
	if err == nil {
		t.Fatal("expected error creating duplicate account")
	}
}

func TestHTTPGetAccount(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
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

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
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
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "admin@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount admin: %v", err)
	}
	adminResp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
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
	_, err = c.Authenticate(context.TODO(), "alice", "password1")
	if err == nil {
		t.Fatal("expected error authenticating disabled account")
	}
}

func TestHTTPEnableAccount(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create admin
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "admin@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount admin: %v", err)
	}
	adminResp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}

	// create and disable alice
	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}

	req, err := http.NewRequest("POST", c.route("account/disable"), bytes.NewBufferString(`{"username":"alice"}`))
	if err != nil {
		t.Fatalf("NewRequest disable: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", adminResp.Token))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do disable: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("resp.Body.Close: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("disable: expected 200, got %d", resp.StatusCode)
	}

	// verify disabled
	acct, err := c.GetAccount(context.TODO(), "alice")
	if err != nil {
		t.Fatalf("GetAccount after disable: %v", err)
	}
	if !acct.Disabled {
		t.Fatal("expected account to be disabled")
	}

	// enable alice
	req, err = http.NewRequest("POST", c.route("account/enable"), bytes.NewBufferString(`{"username":"alice"}`))
	if err != nil {
		t.Fatalf("NewRequest enable: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", adminResp.Token))
	req.Header.Set("Content-Type", "application/json")
	resp, err = c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do enable: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("resp.Body.Close: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("enable: expected 200, got %d", resp.StatusCode)
	}

	// verify enabled
	acct, err = c.GetAccount(context.TODO(), "alice")
	if err != nil {
		t.Fatalf("GetAccount after enable: %v", err)
	}
	if acct.Disabled {
		t.Fatal("expected account to be enabled")
	}

	// verify can authenticate again
	if _, err := c.Authenticate(context.TODO(), "alice", "password1"); err != nil {
		t.Fatalf("Authenticate after enable: %v", err)
	}
}

func TestHTTPAdminChangeRejected(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create admin and a regular user
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "admin@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount admin: %v", err)
	}
	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}

	adminResp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}
	c.Token = adminResp.Token

	// admin tries to promote alice -- should get 403
	adminTrue := true
	_, err = c.UpdateAccount(context.TODO(), "alice", account.UpdateFields{Admin: &adminTrue})
	if err == nil {
		t.Fatal("expected error when changing admin status")
	}

	// verify alice is still not admin
	alice, err := c.GetAccount(context.TODO(), "alice")
	if err != nil {
		t.Fatalf("GetAccount alice: %v", err)
	}
	if alice.Admin {
		t.Fatal("alice should not be admin")
	}
}

func TestHTTPAdminDemoteRejected(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create two admins
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "admin@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount admin: %v", err)
	}
	if _, err := c.CreateAccount(context.TODO(), "admin2", "password1", "a2@b.com", "555", "Admin2", true); err != nil {
		t.Fatalf("CreateAccount admin2: %v", err)
	}

	adminResp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}
	c.Token = adminResp.Token

	// admin tries to demote admin2 -- should get 403
	adminFalse := false
	_, err = c.UpdateAccount(context.TODO(), "admin2", account.UpdateFields{Admin: &adminFalse})
	if err == nil {
		t.Fatal("expected error when changing admin status")
	}

	// verify admin2 is still admin
	admin2, err := c.GetAccount(context.TODO(), "admin2")
	if err != nil {
		t.Fatalf("GetAccount admin2: %v", err)
	}
	if !admin2.Admin {
		t.Fatal("admin2 should still be admin")
	}
}

func TestHTTPListAccounts(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}
	if _, err := c.CreateAccount(context.TODO(), "bob", "password1", "b@b.com", "666", "Bob", false); err != nil {
		t.Fatalf("CreateAccount bob: %v", err)
	}

	accounts, err := c.ListAccounts(context.TODO(), "", "")
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

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "alice", "password1")
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

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	authResp, err := c.Authenticate(context.TODO(), "alice", "password1")
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
	if _, err := c.CreateAccount(context.TODO(), "user", "password1", "u@b.com", "555", "User", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "user", "password1")
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
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
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

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
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

	// only testadmin is an enabled admin
	if ping.Admins != 1 {
		t.Fatalf("expected 1 admin in ping, got %d", ping.Admins)
	}
}

func TestHTTPPingUnitCountsFiltersTownOS(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-nginx.service", ActiveState: "active"},
		{Name: "town-os-redis.service", ActiveState: "active"},
		{Name: "town-os-postgres.service", ActiveState: "failed"},
		{Name: "sshd.service", ActiveState: "active"},
		{Name: "systemd-journald.service", ActiveState: "active"},
	}

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

// --- Audit log tests ---

func TestHTTPAuditLogLifecycle(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create admin and authenticate
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// perform an action (create another account) using admin token
	req, err := http.NewRequest("POST", c.route("account/create"), bytes.NewBufferString(`{"username":"alice","password":"password1","email":"a@b.com","phone":"555","real_name":"Alice","admin":false}`))
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
	if _, err := c.CreateAccount(context.TODO(), "user", "password1", "u@b.com", "555", "User", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "user", "password1")
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
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// perform multiple actions via authenticated requests
	for i := 0; i < 5; i++ {
		username := fmt.Sprintf("user%d", i)
		body := fmt.Sprintf(`{"username":"%s","password":"password1","email":"%s@b.com","phone":"555","real_name":"User %d","admin":false}`, username, username, i)
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
	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "alice", "password1")
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
	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	if _, err := c.Authenticate(context.TODO(), "alice", "password1"); err != nil {
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
	if _, err := c.CreateAccount(context.TODO(), "user", "password1", "u@b.com", "555", "User", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "user", "password1")
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
	acct, err := c.CreateAccount(context.TODO(), "first", "password1", "f@b.com", "555", "First", true)
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

	req, err := http.NewRequest("POST", c.route("account/create"), bytes.NewBufferString(`{"username":"mallory","password":"password1","email":"m@b.com","phone":"555","real_name":"Mallory","admin":false}`))
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
	if _, err := c.CreateAccount(context.TODO(), "user", "password1", "u@b.com", "555", "User", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "user", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// try to create account with non-admin token
	req, err := http.NewRequest("POST", c.route("account/create"), bytes.NewBufferString(`{"username":"mallory","password":"password1","email":"m@b.com","phone":"555","real_name":"Mallory","admin":false}`))
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
	acct, err := c.CreateAccount(context.TODO(), "newadmin", "password1", "n@b.com", "555", "New Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount after all disabled: %v", err)
	}
	if acct.Username != "newadmin" {
		t.Fatalf("expected username %q, got %q", "newadmin", acct.Username)
	}
}

func TestHTTPCreateAccountBootstrapNoAdmins(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// Create a non-admin user
	if _, err := c.CreateAccount(context.TODO(), "regularuser", "password1", "r@b.com", "555", "Regular", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	// Disable the only admin
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

	// Ping should report 0 admins
	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if ping.Admins != 0 {
		t.Fatalf("expected 0 admins, got %d", ping.Admins)
	}
	if ping.Accounts != 2 {
		t.Fatalf("expected 2 accounts, got %d", ping.Accounts)
	}

	// No enabled admin — bootstrap should allow unauthenticated create
	c.Token = ""
	acct, err := c.CreateAccount(context.TODO(), "newadmin", "password1", "n@b.com", "555", "New Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount with no admins: %v", err)
	}
	if acct.Username != "newadmin" {
		t.Fatalf("expected username %q, got %q", "newadmin", acct.Username)
	}
}

// --- Sort integration tests ---

func TestHTTPListAccountsSortByUsername(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// Create accounts in non-alphabetical order
	for _, name := range []string{"charlie", "alice", "bob"} {
		if _, err := c.CreateAccount(context.TODO(), name, "password1", fmt.Sprintf("%s@test.com", name), "555", name, false); err != nil {
			t.Fatalf("CreateAccount %q: %v", name, err)
		}
	}

	// Sort ascending by username
	accounts, err := c.ListAccounts(context.TODO(), "username", "asc")
	if err != nil {
		t.Fatalf("ListAccounts sort asc: %v", err)
	}

	// testadmin (bootstrap) + alice + bob + charlie = 4
	if len(accounts) != 4 {
		t.Fatalf("expected 4 accounts, got %d", len(accounts))
	}

	// First should be alice (alphabetically after testadmin? No, 'a' < 't')
	if accounts[0].Username != "alice" {
		t.Fatalf("expected first account %q, got %q", "alice", accounts[0].Username)
	}
	if accounts[1].Username != "bob" {
		t.Fatalf("expected second account %q, got %q", "bob", accounts[1].Username)
	}

	// Sort descending by username
	accountsDesc, err := c.ListAccounts(context.TODO(), "username", "desc")
	if err != nil {
		t.Fatalf("ListAccounts sort desc: %v", err)
	}

	if accountsDesc[0].Username != "testadmin" {
		t.Fatalf("expected first desc account %q, got %q", "testadmin", accountsDesc[0].Username)
	}
}

func TestHTTPListAccountsSortByAdmin(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "user1", "password1", "u1@test.com", "555", "User1", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if _, err := c.CreateAccount(context.TODO(), "admin2", "password1", "a2@test.com", "555", "Admin2", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	// Sort ascending by admin (false < true)
	accounts, err := c.ListAccounts(context.TODO(), "admin", "asc")
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}

	// Non-admins should come first
	if accounts[0].Admin {
		t.Fatal("expected first account to be non-admin when sorted asc by admin")
	}
}

func TestHTTPListAccountsNoSort(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@test.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	// No sort params should still work
	accounts, err := c.ListAccounts(context.TODO(), "", "")
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}

	if len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accounts))
	}
}

func TestHTTPListFilesystemsSorted(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{"zeta", "alpha", "middle"} {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	// ListFilesystems sort params go in the POST body; use raw HTTP to pass them
	pr, pw := io.Pipe()
	go pipeEncode(pw, FilesystemName{Name: "", SortBy: "name", SortOrder: "asc"})

	resp, err := c.postJSON(context.TODO(), "storage", pr)
	if err != nil {
		t.Fatalf("ListFilesystems sorted: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var fs []storage.Filesystem
	if err := json.NewDecoder(resp.Body).Decode(&fs); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(fs) != 3 {
		t.Fatalf("expected 3 filesystems, got %d", len(fs))
	}

	if fs[0].Name != "alpha" {
		t.Fatalf("expected first filesystem %q, got %q", "alpha", fs[0].Name)
	}
	if fs[1].Name != "middle" {
		t.Fatalf("expected second filesystem %q, got %q", "middle", fs[1].Name)
	}
	if fs[2].Name != "zeta" {
		t.Fatalf("expected third filesystem %q, got %q", "zeta", fs[2].Name)
	}
}

func TestHTTPListFilesystemsSortedDesc(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{"zeta", "alpha", "middle"} {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	pr, pw := io.Pipe()
	go pipeEncode(pw, FilesystemName{Name: "", SortBy: "name", SortOrder: "desc"})

	resp, err := c.postJSON(context.TODO(), "storage", pr)
	if err != nil {
		t.Fatalf("ListFilesystems sorted desc: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var fs []storage.Filesystem
	if err := json.NewDecoder(resp.Body).Decode(&fs); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(fs) != 3 {
		t.Fatalf("expected 3 filesystems, got %d", len(fs))
	}

	if fs[0].Name != "zeta" {
		t.Fatalf("expected first filesystem %q, got %q", "zeta", fs[0].Name)
	}
	if fs[2].Name != "alpha" {
		t.Fatalf("expected third filesystem %q, got %q", "alpha", fs[2].Name)
	}
}

func TestHTTPAuditLogSortByAccount(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	// create admin and authenticate
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// Directly insert audit entries with different accounts to test sort
	for _, user := range []string{"charlie", "alice", "bob"} {
		if err := auditMgr.LogEntry(account.AuditEntry{
			Account:   user,
			Action:    "test",
			Path:      "/test",
			Success:   true,
		}); err != nil {
			t.Fatalf("LogEntry: %v", err)
		}
	}

	// Sort ascending by account
	page, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{
		SortBy:    "account",
		SortOrder: "asc",
	}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog sorted: %v", err)
	}

	// Find the test entries (skip audit entries from the create/auth operations)
	var testEntries []account.AuditEntry
	for _, e := range page.Entries {
		if e.Action == "test" {
			testEntries = append(testEntries, e)
		}
	}

	if len(testEntries) < 3 {
		t.Fatalf("expected at least 3 test entries, got %d", len(testEntries))
	}

	if testEntries[0].Account != "alice" {
		t.Fatalf("expected first test entry account %q, got %q", "alice", testEntries[0].Account)
	}
	if testEntries[1].Account != "bob" {
		t.Fatalf("expected second test entry account %q, got %q", "bob", testEntries[1].Account)
	}
	if testEntries[2].Account != "charlie" {
		t.Fatalf("expected third test entry account %q, got %q", "charlie", testEntries[2].Account)
	}
}

func TestHTTPAuditLogSortByIDASc(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create admin and authenticate
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// Perform a few actions to create audit entries
	for i := 0; i < 3; i++ {
		username := fmt.Sprintf("sortuser%d", i)
		body := fmt.Sprintf(`{"username":"%s","password":"password1","email":"%s@b.com","phone":"555","real_name":"User %d","admin":false}`, username, username, i)
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

	// Sort ascending by ID
	page, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{
		SortBy:    "id",
		SortOrder: "asc",
	}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog sorted asc: %v", err)
	}

	if len(page.Entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(page.Entries))
	}

	// Ascending: IDs should increase
	for i := 1; i < len(page.Entries); i++ {
		if page.Entries[i].ID < page.Entries[i-1].ID {
			t.Fatalf("entry %d ID (%d) < entry %d ID (%d) in asc sort", i, page.Entries[i].ID, i-1, page.Entries[i-1].ID)
		}
	}

	// Sort descending by ID (default behavior)
	pageDesc, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{
		SortBy:    "id",
		SortOrder: "desc",
	}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog sorted desc: %v", err)
	}

	// Descending: IDs should decrease
	for i := 1; i < len(pageDesc.Entries); i++ {
		if pageDesc.Entries[i].ID > pageDesc.Entries[i-1].ID {
			t.Fatalf("entry %d ID (%d) > entry %d ID (%d) in desc sort", i, pageDesc.Entries[i].ID, i-1, pageDesc.Entries[i-1].ID)
		}
	}
}

// --- Systemd handler tests ---

func initSystemdTestClient(t *testing.T) (*SystemdClient, *systemd.MockManager) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	ts := InitTestServer(ServerConfig{Storage: mock, Systemd: sd})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, sd
}

func TestHTTPListUnits(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	sd.Units = []systemd.UnitStatus{
		{Name: "foo.service", Description: "Foo", LoadState: "loaded", ActiveState: "active", SubState: "running", UnitFileState: "enabled"},
		{Name: "bar.service", Description: "Bar", LoadState: "loaded", ActiveState: "inactive", SubState: "dead", UnitFileState: "disabled"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units.Entries))
	}

	if units.Entries[0].Name != "foo.service" {
		t.Fatalf("expected first unit %q, got %q", "foo.service", units.Entries[0].Name)
	}
	if units.Entries[0].UnitFileState != "enabled" {
		t.Fatalf("expected first unit UnitFileState %q, got %q", "enabled", units.Entries[0].UnitFileState)
	}
	if units.Entries[1].Name != "bar.service" {
		t.Fatalf("expected second unit %q, got %q", "bar.service", units.Entries[1].Name)
	}
	if units.Entries[1].UnitFileState != "disabled" {
		t.Fatalf("expected second unit UnitFileState %q, got %q", "disabled", units.Entries[1].UnitFileState)
	}
}

func TestHTTPListUnitsEmpty(t *testing.T) {
	c, _ := initSystemdTestClient(t)

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 0 {
		t.Fatalf("expected 0 units, got %d", len(units.Entries))
	}
}

func TestHTTPSetUnitStatusStart(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	if err := c.SetUnitStatus(context.TODO(), "test.service", systemd.Start); err != nil {
		t.Fatalf("SetUnitStatus(start): %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "SetStatus" {
		t.Fatalf("expected method SetStatus, got %q", calls[0].Method)
	}
	if calls[0].Args[0].(string) != "test.service" {
		t.Fatalf("expected unit %q, got %v", "test.service", calls[0].Args[0])
	}
	if calls[0].Args[1].(systemd.StatusAction) != systemd.Start {
		t.Fatalf("expected action %q, got %v", systemd.Start, calls[0].Args[1])
	}
}

func TestHTTPSetUnitStatusStop(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	if err := c.SetUnitStatus(context.TODO(), "test.service", systemd.Stop); err != nil {
		t.Fatalf("SetUnitStatus(stop): %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Args[1].(systemd.StatusAction) != systemd.Stop {
		t.Fatalf("expected action %q, got %v", systemd.Stop, calls[0].Args[1])
	}
}

func TestHTTPSetUnitStatusRestart(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	if err := c.SetUnitStatus(context.TODO(), "test.service", systemd.Restart); err != nil {
		t.Fatalf("SetUnitStatus(restart): %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Args[1].(systemd.StatusAction) != systemd.Restart {
		t.Fatalf("expected action %q, got %v", systemd.Restart, calls[0].Args[1])
	}
}

func TestHTTPSetUnitStatusEnableRejected(t *testing.T) {
	c, _ := initSystemdTestClient(t)

	err := c.SetUnitStatus(context.TODO(), "test.service", systemd.Enable)
	if err == nil {
		t.Fatal("expected error for enable action")
	}
}

func TestHTTPSetUnitStatusDisableRejected(t *testing.T) {
	c, _ := initSystemdTestClient(t)

	err := c.SetUnitStatus(context.TODO(), "test.service", systemd.Disable)
	if err == nil {
		t.Fatal("expected error for disable action")
	}
}

func TestHTTPSetUnitStatusStopSystemcontrollerRejected(t *testing.T) {
	c, _ := initSystemdTestClient(t)

	err := c.SetUnitStatus(context.TODO(), "town-os-systemcontroller.service", systemd.Stop)
	if err == nil {
		t.Fatal("expected error stopping systemcontroller")
	}
}

func TestHTTPSetUnitStatusInvalidAction(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	sd.StatusErr = fmt.Errorf("injected error")

	err := c.SetUnitStatus(context.TODO(), "test.service", systemd.Start)
	if err == nil {
		t.Fatal("expected error from SetUnitStatus with injected error")
	}
}

func TestHTTPDisablePackage(t *testing.T) {
	c, inst, sd := initInstallWithSystemdTestClient(t)

	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "testhost"}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := c.DisablePackage(context.TODO(), "nginx"); err != nil {
		t.Fatalf("DisablePackage: %v", err)
	}

	instCalls := inst.GetCalls()
	found := false
	for _, call := range instCalls {
		if call.Method == "SetDisabled" && call.Args[0].(string) == "nginx" && call.Args[1].(bool) == true {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected SetDisabled(nginx, true) call")
	}

	sdCalls := sd.GetCalls()
	lastCall := sdCalls[len(sdCalls)-1]
	if lastCall.Method != "SetStatus" || lastCall.Args[1].(systemd.StatusAction) != systemd.Stop {
		t.Fatalf("expected last systemd call to be Stop, got %s %v", lastCall.Method, lastCall.Args)
	}
}

func TestHTTPEnablePackage(t *testing.T) {
	c, inst, sd := initInstallWithSystemdTestClient(t)

	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "testhost"}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := c.DisablePackage(context.TODO(), "nginx"); err != nil {
		t.Fatalf("DisablePackage: %v", err)
	}

	if err := c.EnablePackage(context.TODO(), "nginx"); err != nil {
		t.Fatalf("EnablePackage: %v", err)
	}

	instCalls := inst.GetCalls()
	found := false
	for _, call := range instCalls {
		if call.Method == "SetDisabled" && call.Args[0].(string) == "nginx" && call.Args[1].(bool) == false {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected SetDisabled(nginx, false) call")
	}

	sdCalls := sd.GetCalls()
	lastCall := sdCalls[len(sdCalls)-1]
	if lastCall.Method != "SetStatus" || lastCall.Args[1].(systemd.StatusAction) != systemd.Start {
		t.Fatalf("expected last systemd call to be Start, got %s %v", lastCall.Method, lastCall.Args)
	}
}

func TestHTTPLogReplay(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Message: "entry one", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Message: "entry two", RealtimeTimestamp: now.Add(-time.Second)},
		{Message: "entry three", RealtimeTimestamp: now},
	}

	ch, err := c.LogReplay(context.TODO(), "test.service")
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

	if entries[0].Message != "entry one" {
		t.Fatalf("expected first message %q, got %q", "entry one", entries[0].Message)
	}
	if entries[2].Message != "entry three" {
		t.Fatalf("expected third message %q, got %q", "entry three", entries[2].Message)
	}
}

func TestHTTPLogReplayEmpty(t *testing.T) {
	c, _ := initSystemdTestClient(t)

	ch, err := c.LogReplay(context.TODO(), "test.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	var count int
	for range ch {
		count++
	}

	if count != 0 {
		t.Fatalf("expected 0 entries, got %d", count)
	}
}

func TestHTTPLogReplayError(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	sd.LogErr = fmt.Errorf("injected log error")

	_, err := c.LogReplay(context.TODO(), "test.service")
	if err == nil {
		t.Fatal("expected error from LogReplay with injected error")
	}
}

func TestHTTPLogReplayEmptyUnit(t *testing.T) {
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	ts := InitTestServer(ServerConfig{Storage: mock, Systemd: sd})
	t.Cleanup(ts.Close)

	resp, err := ts.Server.Client().Get(fmt.Sprintf("%s/systemd/logs", ts.Server.URL))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 for system-wide log replay (empty unit), got %d", resp.StatusCode)
	}
}

func TestHTTPLogTail(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "first", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c2", Message: "second", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "third", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "fourth", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c5", Message: "fifth", RealtimeTimestamp: now},
	}

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 3})
	if err != nil {
		t.Fatalf("LogTail: %v", err)
	}

	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "third" {
		t.Fatalf("expected first entry %q, got %q", "third", result.Entries[0].Message)
	}
	if result.Entries[2].Message != "fifth" {
		t.Fatalf("expected last entry %q, got %q", "fifth", result.Entries[2].Message)
	}

	if result.Cursor != "c3" {
		t.Fatalf("expected cursor %q, got %q", "c3", result.Cursor)
	}
	if result.EndCursor != "c5" {
		t.Fatalf("expected end_cursor %q, got %q", "c5", result.EndCursor)
	}
}

func TestHTTPLogTailWithCursor(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "first", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c2", Message: "second", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "third", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "fourth", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c5", Message: "fifth", RealtimeTimestamp: now},
	}

	// Get entries before cursor c3 (should get c1, c2)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, BeforeCursor: "c3"})
	if err != nil {
		t.Fatalf("LogTail with cursor: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries before c3, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "first" {
		t.Fatalf("expected first entry %q, got %q", "first", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "second" {
		t.Fatalf("expected second entry %q, got %q", "second", result.Entries[1].Message)
	}
}

func TestHTTPLogTailEmpty(t *testing.T) {
	c, _ := initSystemdTestClient(t)

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100})
	if err != nil {
		t.Fatalf("LogTail: %v", err)
	}

	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(result.Entries))
	}
}

func TestHTTPLogTailError(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	sd.LogErr = fmt.Errorf("injected log error")

	_, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100})
	if err == nil {
		t.Fatal("expected error from LogTail with injected error")
	}
}

func TestHTTPLogTailEmptyUnit(t *testing.T) {
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	ts := InitTestServer(ServerConfig{Storage: mock, Systemd: sd})
	t.Cleanup(ts.Close)

	resp, err := ts.Server.Client().Get(fmt.Sprintf("%s/systemd/logs/tail", ts.Server.URL))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 for system-wide log tail (empty unit), got %d", resp.StatusCode)
	}
}

func TestHTTPLogTailGrep(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "starting nginx", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c2", Message: "connection from 10.0.0.1", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "error: upstream timeout", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "connection from 10.0.0.2", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c5", Message: "stopping nginx", RealtimeTimestamp: now},
	}

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Grep: "connection"})
	if err != nil {
		t.Fatalf("LogTail with grep: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries matching 'connection', got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "connection from 10.0.0.1" {
		t.Fatalf("expected first match %q, got %q", "connection from 10.0.0.1", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "connection from 10.0.0.2" {
		t.Fatalf("expected second match %q, got %q", "connection from 10.0.0.2", result.Entries[1].Message)
	}
}

func TestHTTPLogTailGrepCaseInsensitive(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "ERROR: something failed", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c2", Message: "info: all good", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c3", Message: "error: another failure", RealtimeTimestamp: now},
	}

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Grep: "error"})
	if err != nil {
		t.Fatalf("LogTail with grep: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries matching 'error' (case-insensitive), got %d", len(result.Entries))
	}
}

func TestHTTPLogTailGrepNoMatch(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "hello world", RealtimeTimestamp: now},
	}

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Grep: "nonexistent"})
	if err != nil {
		t.Fatalf("LogTail with grep: %v", err)
	}

	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries for non-matching grep, got %d", len(result.Entries))
	}
}

func TestHTTPLogTailAfterCursor(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "first", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c2", Message: "second", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "third", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "fourth", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c5", Message: "fifth", RealtimeTimestamp: now},
	}

	// Get entries after cursor c2 (should get c3, c4, c5)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, AfterCursor: "c2"})
	if err != nil {
		t.Fatalf("LogTail after cursor: %v", err)
	}

	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries after c2, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "third" {
		t.Fatalf("expected first entry %q, got %q", "third", result.Entries[0].Message)
	}
	if result.Entries[2].Message != "fifth" {
		t.Fatalf("expected last entry %q, got %q", "fifth", result.Entries[2].Message)
	}

	if result.Cursor != "c3" {
		t.Fatalf("expected cursor %q, got %q", "c3", result.Cursor)
	}
	if result.EndCursor != "c5" {
		t.Fatalf("expected end_cursor %q, got %q", "c5", result.EndCursor)
	}
}

func TestHTTPLogTailAfterCursorEmpty(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "first", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c2", Message: "second", RealtimeTimestamp: now},
	}

	// Get entries after last cursor (should be empty)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, AfterCursor: "c2"})
	if err != nil {
		t.Fatalf("LogTail after cursor: %v", err)
	}

	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries after last cursor, got %d", len(result.Entries))
	}
}

func TestHTTPLogTailAfterCursorWithLimit(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "first", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c2", Message: "second", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "third", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "fourth", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c5", Message: "fifth", RealtimeTimestamp: now},
	}

	// Get at most 2 entries after cursor c1
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 2, AfterCursor: "c1"})
	if err != nil {
		t.Fatalf("LogTail after cursor with limit: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "second" {
		t.Fatalf("expected first entry %q, got %q", "second", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "third" {
		t.Fatalf("expected second entry %q, got %q", "third", result.Entries[1].Message)
	}

	if result.EndCursor != "c3" {
		t.Fatalf("expected end_cursor %q, got %q", "c3", result.EndCursor)
	}
}

func TestHTTPLogTailAfterCursorWithGrep(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "start", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c2", Message: "error: disk full", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "info: ok", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "error: timeout", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c5", Message: "done", RealtimeTimestamp: now},
	}

	// Get entries after c1 matching "error"
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, AfterCursor: "c1", Grep: "error"})
	if err != nil {
		t.Fatalf("LogTail after cursor with grep: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries matching grep, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "error: disk full" {
		t.Fatalf("expected first entry %q, got %q", "error: disk full", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "error: timeout" {
		t.Fatalf("expected second entry %q, got %q", "error: timeout", result.Entries[1].Message)
	}
}

func TestHTTPLogTailSince(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "old entry", RealtimeTimestamp: now.Add(-10 * time.Second)},
		{Cursor: "c2", Message: "also old", RealtimeTimestamp: now.Add(-8 * time.Second)},
		{Cursor: "c3", Message: "recent one", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c4", Message: "newer one", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c5", Message: "newest", RealtimeTimestamp: now},
	}

	// Get entries since 5 seconds ago (should get c3, c4, c5)
	since := now.Add(-5 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Since: since})
	if err != nil {
		t.Fatalf("LogTail since: %v", err)
	}

	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries since cutoff, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "recent one" {
		t.Fatalf("expected first entry %q, got %q", "recent one", result.Entries[0].Message)
	}
	if result.Entries[2].Message != "newest" {
		t.Fatalf("expected last entry %q, got %q", "newest", result.Entries[2].Message)
	}

	if result.EndCursor != "c5" {
		t.Fatalf("expected end_cursor %q, got %q", "c5", result.EndCursor)
	}
}

func TestHTTPLogTailSinceWithGrep(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "error: old", RealtimeTimestamp: now.Add(-10 * time.Second)},
		{Cursor: "c2", Message: "info: recent", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "error: recent", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "info: newest", RealtimeTimestamp: now.Add(-time.Second)},
	}

	// Get entries since 5 seconds ago matching "error" (should get only c3)
	since := now.Add(-5 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Since: since, Grep: "error"})
	if err != nil {
		t.Fatalf("LogTail since with grep: %v", err)
	}

	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "error: recent" {
		t.Fatalf("expected entry %q, got %q", "error: recent", result.Entries[0].Message)
	}
}

func TestHTTPLogTailSinceEmpty(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "old", RealtimeTimestamp: now.Add(-10 * time.Second)},
	}

	// All entries are before 'since', should return empty
	since := now.Add(-5 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Since: since})
	if err != nil {
		t.Fatalf("LogTail since: %v", err)
	}

	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(result.Entries))
	}
}

func TestHTTPLogTailSinceWithLimit(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "old", RealtimeTimestamp: now.Add(-10 * time.Second)},
		{Cursor: "c2", Message: "a", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "b", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "c", RealtimeTimestamp: now.Add(-time.Second)},
	}

	// Get at most 2 entries since 5 seconds ago
	since := now.Add(-5 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 2, Since: since})
	if err != nil {
		t.Fatalf("LogTail since with limit: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "a" {
		t.Fatalf("expected first entry %q, got %q", "a", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "b" {
		t.Fatalf("expected second entry %q, got %q", "b", result.Entries[1].Message)
	}
}

func TestHTTPLogTailSinceUntil(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "before window", RealtimeTimestamp: now.Add(-10 * time.Second)},
		{Cursor: "c2", Message: "in window", RealtimeTimestamp: now.Add(-5 * time.Second)},
		{Cursor: "c3", Message: "also in window", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c4", Message: "after window", RealtimeTimestamp: now.Add(-1 * time.Second)},
		{Cursor: "c5", Message: "latest", RealtimeTimestamp: now},
	}

	// Window from -7s to -2s: should get c2 and c3
	since := now.Add(-7 * time.Second)
	until := now.Add(-2 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Since: since, Until: until})
	if err != nil {
		t.Fatalf("LogTail since+until: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries in window, got %d", len(result.Entries))
	}
	if result.Entries[0].Message != "in window" {
		t.Fatalf("expected first entry %q, got %q", "in window", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "also in window" {
		t.Fatalf("expected second entry %q, got %q", "also in window", result.Entries[1].Message)
	}
}

func TestHTTPLogTailSinceUntilEmpty(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "old entry", RealtimeTimestamp: now.Add(-10 * time.Second)},
		{Cursor: "c2", Message: "newer entry", RealtimeTimestamp: now.Add(-1 * time.Second)},
	}

	// Window that contains no entries: -8s to -5s
	since := now.Add(-8 * time.Second)
	until := now.Add(-5 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Since: since, Until: until})
	if err != nil {
		t.Fatalf("LogTail since+until empty: %v", err)
	}

	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries in window, got %d", len(result.Entries))
	}
}

func TestHTTPLogTailSinceUntilWithGrep(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "error: old", RealtimeTimestamp: now.Add(-10 * time.Second)},
		{Cursor: "c2", Message: "info: in window", RealtimeTimestamp: now.Add(-5 * time.Second)},
		{Cursor: "c3", Message: "error: in window", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c4", Message: "error: after window", RealtimeTimestamp: now.Add(-1 * time.Second)},
	}

	// Window from -7s to -2s matching "error": should get only c3
	since := now.Add(-7 * time.Second)
	until := now.Add(-2 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Since: since, Until: until, Grep: "error"})
	if err != nil {
		t.Fatalf("LogTail since+until+grep: %v", err)
	}

	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result.Entries))
	}
	if result.Entries[0].Message != "error: in window" {
		t.Fatalf("expected entry %q, got %q", "error: in window", result.Entries[0].Message)
	}
}

func TestHTTPLogTailUntilBeforeAllEntries(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "entry 1", RealtimeTimestamp: now.Add(-5 * time.Second)},
		{Cursor: "c2", Message: "entry 2", RealtimeTimestamp: now.Add(-3 * time.Second)},
	}

	// Since before all entries, until also before all entries
	since := now.Add(-20 * time.Second)
	until := now.Add(-10 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Since: since, Until: until})
	if err != nil {
		t.Fatalf("LogTail until before all: %v", err)
	}

	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(result.Entries))
	}
}

func TestHTTPLogTailSinceUntilWithLimit(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "old", RealtimeTimestamp: now.Add(-10 * time.Second)},
		{Cursor: "c2", Message: "a", RealtimeTimestamp: now.Add(-5 * time.Second)},
		{Cursor: "c3", Message: "b", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c4", Message: "c", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c5", Message: "after", RealtimeTimestamp: now.Add(-1 * time.Second)},
	}

	// Window from -7s to -2s with limit 2: should get c2 and c3
	since := now.Add(-7 * time.Second)
	until := now.Add(-2 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 2, Since: since, Until: until})
	if err != nil {
		t.Fatalf("LogTail since+until+limit: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result.Entries))
	}
	if result.Entries[0].Message != "a" {
		t.Fatalf("expected first %q, got %q", "a", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "b" {
		t.Fatalf("expected second %q, got %q", "b", result.Entries[1].Message)
	}
}

// --- Pagination tests ---

func TestHTTPListUnitsPagination(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	sd.Units = []systemd.UnitStatus{
		{Name: "a.service", Description: "A", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "b.service", Description: "B", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "c.service", Description: "C", LoadState: "loaded", ActiveState: "active", SubState: "running"},
	}

	// First page: limit=2, offset=0
	page, err := c.ListUnits(context.TODO(), ListParams{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListUnits page 0: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}
	if page.TotalPages != 2 {
		t.Fatalf("expected 2 total pages, got %d", page.TotalPages)
	}

	// Second page: limit=2, offset=2
	page, err = c.ListUnits(context.TODO(), ListParams{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListUnits page 1: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false")
	}
}

func TestHTTPListPackagesPagination(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo-a", "alpha", "1.0", "image: alpha:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo-a", "bravo", "1.0", "image: bravo:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo-a", "charlie", "1.0", "image: charlie:1.0\n")

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// First page
	page, err := c.ListPackages(context.TODO(), ListParams{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListPackages page 0: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}

	// Second page
	page, err = c.ListPackages(context.TODO(), ListParams{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListPackages page 1: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false")
	}
}

func TestHTTPListInstalledPagination(t *testing.T) {
	c, _ := initInstallTestClient(t)

	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, ""); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}
	if err := c.InstallPackage(context.TODO(),"nginx", "2.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage nginx@2.0: %v", err)
	}

	// Limit 1
	page, err := c.ListInstalled(context.TODO(), ListParams{Limit: 1, Offset: 0})
	if err != nil {
		t.Fatalf("ListInstalled page 0: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}
	if page.TotalPages != 2 {
		t.Fatalf("expected 2 total pages, got %d", page.TotalPages)
	}

	// Second page
	page, err = c.ListInstalled(context.TODO(), ListParams{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("ListInstalled page 1: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false")
	}
}

func TestHTTPListRepositoriesPagination(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u1, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	u2, err := url.Parse("https://example.com/repo-b.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	u3, err := url.Parse("https://example.com/repo-c.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u1},
		{Name: "repo-b", URL: *u2},
		{Name: "repo-c", URL: *u3},
	}

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// First page
	page, err := c.ListRepositories(context.TODO(), ListParams{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListRepositories page 0: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}
	if page.TotalPages != 2 {
		t.Fatalf("expected 2 total pages, got %d", page.TotalPages)
	}

	// Second page
	page, err = c.ListRepositories(context.TODO(), ListParams{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListRepositories page 1: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false")
	}
}

func TestPaginateHelper(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e"}

	// Default limit (20) returns all
	p := paginate(items, 0, 0)
	if len(p.Entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(p.Entries))
	}
	if p.HasMore {
		t.Fatal("expected has_more=false")
	}
	if p.TotalPages != 1 {
		t.Fatalf("expected 1 total page, got %d", p.TotalPages)
	}

	// Limit=2, offset=0
	p = paginate(items, 2, 0)
	if len(p.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(p.Entries))
	}
	if !p.HasMore {
		t.Fatal("expected has_more=true")
	}
	if p.TotalPages != 3 {
		t.Fatalf("expected 3 total pages, got %d", p.TotalPages)
	}
	if p.Entries[0] != "a" || p.Entries[1] != "b" {
		t.Fatalf("unexpected entries: %v", p.Entries)
	}

	// Limit=2, offset=4
	p = paginate(items, 2, 4)
	if len(p.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(p.Entries))
	}
	if p.HasMore {
		t.Fatal("expected has_more=false")
	}

	// Offset beyond end
	p = paginate(items, 2, 100)
	if len(p.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(p.Entries))
	}
	if p.HasMore {
		t.Fatal("expected has_more=false")
	}

	// Negative offset clamped to 0
	p = paginate(items, 2, -5)
	if len(p.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(p.Entries))
	}

	// Empty slice
	p = paginate([]string{}, 10, 0)
	if len(p.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(p.Entries))
	}
	if p.HasMore {
		t.Fatal("expected has_more=false for empty")
	}
	if p.TotalPages != 1 {
		t.Fatalf("expected 1 total page for empty, got %d", p.TotalPages)
	}
}

func TestListParamsQueryString(t *testing.T) {
	// Empty
	p := ListParams{}
	if qs := p.QueryString(); qs != "" {
		t.Fatalf("expected empty query string, got %q", qs)
	}

	// Sort only
	p = ListParams{SortBy: "name", SortOrder: "desc"}
	qs := p.QueryString()
	if qs == "" {
		t.Fatal("expected non-empty query string")
	}

	// Full params
	p = ListParams{SortBy: "name", SortOrder: "asc", Limit: 10, Offset: 20}
	qs = p.QueryString()
	if qs == "" {
		t.Fatal("expected non-empty query string")
	}

	// With search
	p = ListParams{Search: "nginx"}
	qs = p.QueryString()
	if qs == "" {
		t.Fatal("expected non-empty query string for search")
	}
	if !strings.Contains(qs, "search=nginx") {
		t.Fatalf("expected search=nginx in query string, got %q", qs)
	}
}

func TestFilterSearchStrings(t *testing.T) {
	items := []string{"nginx@1.0", "redis@7.0", "postgres@16.0", "nginx@2.0"}

	// Match
	result := filterSearch(items, "nginx")
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}

	// Case insensitive
	result = filterSearch(items, "REDIS")
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}

	// No match
	result = filterSearch(items, "mysql")
	if len(result) != 0 {
		t.Fatalf("expected 0 results, got %d", len(result))
	}

	// Empty search returns all
	result = filterSearch(items, "")
	if len(result) != 4 {
		t.Fatalf("expected 4 results, got %d", len(result))
	}
}

func TestFilterSearchStructs(t *testing.T) {
	units := []systemd.UnitStatus{
		{Name: "nginx.service", Description: "NGINX web server", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "redis.service", Description: "Redis cache", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "postgres.service", Description: "PostgreSQL database", LoadState: "loaded", ActiveState: "failed", SubState: "dead"},
	}

	// Match by name
	result := filterSearch(units, "nginx")
	if len(result) != 1 {
		t.Fatalf("expected 1 result for 'nginx', got %d", len(result))
	}
	if result[0].Name != "nginx.service" {
		t.Fatalf("expected nginx.service, got %s", result[0].Name)
	}

	// Match by description
	result = filterSearch(units, "database")
	if len(result) != 1 {
		t.Fatalf("expected 1 result for 'database', got %d", len(result))
	}
	if result[0].Name != "postgres.service" {
		t.Fatalf("expected postgres.service, got %s", result[0].Name)
	}

	// Match by state
	result = filterSearch(units, "failed")
	if len(result) != 1 {
		t.Fatalf("expected 1 result for 'failed', got %d", len(result))
	}

	// Case insensitive
	result = filterSearch(units, "REDIS")
	if len(result) != 1 {
		t.Fatalf("expected 1 result for 'REDIS', got %d", len(result))
	}

	// Partial match across multiple results
	result = filterSearch(units, "service")
	if len(result) != 3 {
		t.Fatalf("expected 3 results for 'service', got %d", len(result))
	}

	// No match
	result = filterSearch(units, "mysql")
	if len(result) != 0 {
		t.Fatalf("expected 0 results for 'mysql', got %d", len(result))
	}
}

func TestHTTPListUnitsSearch(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	sd.Units = []systemd.UnitStatus{
		{Name: "nginx.service", Description: "NGINX web server", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "redis.service", Description: "Redis cache", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "postgres.service", Description: "PostgreSQL database", LoadState: "loaded", ActiveState: "active", SubState: "running"},
	}

	// Search for "nginx"
	page, err := c.ListUnits(context.TODO(), ListParams{Search: "nginx"})
	if err != nil {
		t.Fatalf("ListUnits search: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if page.Entries[0].Name != "nginx.service" {
		t.Fatalf("expected nginx.service, got %s", page.Entries[0].Name)
	}

	// Search with pagination
	page, err = c.ListUnits(context.TODO(), ListParams{Search: "service", Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListUnits search+page: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}

	// No match
	page, err = c.ListUnits(context.TODO(), ListParams{Search: "mysql"})
	if err != nil {
		t.Fatalf("ListUnits search no match: %v", err)
	}
	if len(page.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(page.Entries))
	}
}

func TestHTTPListPackagesSearch(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo", "nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "redis", "7.0", "image: redis:7.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "postgres", "16.0", "image: postgres:16.0\n")

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Search for "nginx"
	page, err := c.ListPackages(context.TODO(), ListParams{Search: "nginx"})
	if err != nil {
		t.Fatalf("ListPackages search: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}

	// No match
	page, err = c.ListPackages(context.TODO(), ListParams{Search: "mysql"})
	if err != nil {
		t.Fatalf("ListPackages search no match: %v", err)
	}
	if len(page.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(page.Entries))
	}
}

func TestHTTPListRepositoriesSearch(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u1, _ := url.Parse("https://example.com/core.git")
	u2, _ := url.Parse("https://example.com/extras.git")
	rr.Items = []packages.Repository{
		{Name: "core", URL: *u1},
		{Name: "extras", URL: *u2},
	}

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Search by name
	page, err := c.ListRepositories(context.TODO(), ListParams{Search: "core"})
	if err != nil {
		t.Fatalf("ListRepositories search: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if page.Entries[0].Name != "core" {
		t.Fatalf("expected core, got %s", page.Entries[0].Name)
	}

	// Search by URL
	page, err = c.ListRepositories(context.TODO(), ListParams{Search: "extras"})
	if err != nil {
		t.Fatalf("ListRepositories search by URL: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
}

func TestSanitizeAuditDetail(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "removes password from top level",
			body: `{"username":"admin","password":"secret"}`,
			want: `{"username":"admin"}`,
		},
		{
			name: "removes password from nested fields",
			body: `{"username":"admin","fields":{"password":"new","real_name":"Bob"}}`,
			want: `{"fields":{"real_name":"Bob"},"username":"admin"}`,
		},
		{
			name: "preserves body without password",
			body: `{"name":"nginx","version":"1.0"}`,
			want: `{"name":"nginx","version":"1.0"}`,
		},
		{
			name: "returns empty for invalid JSON",
			body: `not json`,
			want: "",
		},
		{
			name: "returns empty for empty body",
			body: ``,
			want: "",
		},
		{
			name: "preserves nested objects",
			body: `{"name":"nginx","responses":{"port":"8080"}}`,
			want: `{"name":"nginx","responses":{"port":"8080"}}`,
		},
		{
			name: "removes password from deeply nested objects",
			body: `{"data":{"inner":{"password":"deep","name":"ok"}}}`,
			want: `{"data":{"inner":{"name":"ok"}}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeAuditDetail([]byte(tc.body))
			if got != tc.want {
				t.Fatalf("sanitizeAuditDetail(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

func TestHTTPAuditDetailCaptured(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	// create admin
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// Disable a user - this should capture detail
	_, _ = c.CreateAccount(context.TODO(), "user1", "password1", "u@b.com", "555", "User", false)

	// The disable call has a simple body: {"username":"user1"}
	body := `{"username":"user1"}`
	req, err := http.NewRequest("POST", c.route("account/disable"), bytes.NewBufferString(body))
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

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	found := false
	for _, e := range page.Entries {
		if e.Action == "disable account" && e.Detail != "" {
			found = true
			if !strings.Contains(e.Detail, "user1") {
				t.Fatalf("expected detail to contain 'user1', got %q", e.Detail)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected to find audit entry with detail for disable account")
	}
}

func TestHTTPAuditDetailRedactsPassword(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	// create admin
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, e := range page.Entries {
		if e.Action == "create account" {
			if strings.Contains(e.Detail, "password1") {
				t.Fatalf("expected detail to NOT contain password, got %q", e.Detail)
			}
			if !strings.Contains(e.Detail, "admin") {
				t.Fatalf("expected detail to contain username 'admin', got %q", e.Detail)
			}
			return
		}
	}
	t.Fatal("expected to find create account audit entry")
}

func TestHTTPAuditDetailValidJSON(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "admin", "secret12", "admin@test.com", "555-1234", "Admin User", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, e := range page.Entries {
		if e.Action == "create account" && e.Detail != "" {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(e.Detail), &parsed); err != nil {
				t.Fatalf("detail is not valid JSON: %q, err: %v", e.Detail, err)
			}

			if parsed["username"] != "admin" {
				t.Fatalf("expected username 'admin', got %v", parsed["username"])
			}
			if parsed["email"] != "admin@test.com" {
				t.Fatalf("expected email 'admin@test.com', got %v", parsed["email"])
			}
			if parsed["real_name"] != "Admin User" {
				t.Fatalf("expected real_name 'Admin User', got %v", parsed["real_name"])
			}
			if _, exists := parsed["password"]; exists {
				t.Fatal("detail must not contain password field")
			}
			return
		}
	}
	t.Fatal("expected to find create account audit entry with detail")
}

func TestHTTPAuditDetailAuthenticateRedactsPassword(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "admin", "mypass12", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	if _, err := c.Authenticate(context.TODO(), "admin", "mypass12"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, e := range page.Entries {
		if e.Action == "authenticate" && e.Detail != "" {
			if strings.Contains(e.Detail, "mypass12") {
				t.Fatalf("authenticate detail must not contain password, got %q", e.Detail)
			}
			if !strings.Contains(e.Detail, "admin") {
				t.Fatalf("authenticate detail should contain username, got %q", e.Detail)
			}
			var parsed map[string]any
			if err := json.Unmarshal([]byte(e.Detail), &parsed); err != nil {
				t.Fatalf("detail is not valid JSON: %q", e.Detail)
			}
			if _, exists := parsed["password"]; exists {
				t.Fatal("detail must not contain password field")
			}
			return
		}
	}
	t.Fatal("expected to find authenticate audit entry with detail")
}

func TestHTTPAuditDetailNeverContainsPassword(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "admin", "supersecret", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "admin", "supersecret")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	c.Token = resp.Token

	// Update account with password change
	newpw := "newpassword"
	_, _ = c.UpdateAccount(context.TODO(), "admin", account.UpdateFields{Password: &newpw})

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, e := range page.Entries {
		if e.Detail == "" {
			continue
		}
		if strings.Contains(e.Detail, "supersecret") {
			t.Fatalf("entry %q detail contains password 'supersecret': %q", e.Action, e.Detail)
		}
		if strings.Contains(e.Detail, "newpassword") {
			t.Fatalf("entry %q detail contains password 'newpassword': %q", e.Action, e.Detail)
		}
		if strings.Contains(e.Detail, `"password"`) {
			t.Fatalf("entry %q detail contains password field: %q", e.Action, e.Detail)
		}
	}
}

// --- Install validation errors ---

func TestHTTPInstallValidationErrors(t *testing.T) {
	c, _ := initInstallTestClient(t)

	// nginx@1.0 has questions: hostname (hostname type) and port (port type).
	// Send empty responses to trigger missing errors for both.
	err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{}, false, "")
	if err == nil {
		t.Fatal("expected error from validation")
	}

	var pe *ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ProblemError, got %T: %v", err, err)
	}

	if pe.Problem.Status != 422 {
		t.Fatalf("expected status 422, got %d", pe.Problem.Status)
	}

	if len(pe.ValidationErrors) != 2 {
		t.Fatalf("expected 2 validation errors, got %d: %v", len(pe.ValidationErrors), pe.ValidationErrors)
	}

	errMap := map[string]string{}
	for _, ve := range pe.ValidationErrors {
		errMap[ve.Name] = ve.Error
	}
	if errMap["hostname"] != packages.ErrMissingResponse.Error() {
		t.Fatalf("expected missing response for hostname, got %q", errMap["hostname"])
	}
	if errMap["port"] != packages.ErrMissingResponse.Error() {
		t.Fatalf("expected missing response for port, got %q", errMap["port"])
	}
}

func TestHTTPInstallValidationErrorsEmptyResponse(t *testing.T) {
	c, _ := initInstallTestClient(t)

	err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{
		"hostname": "",
		"port":     "",
	}, false, "")
	if err == nil {
		t.Fatal("expected error from validation")
	}

	var pe *ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ProblemError, got %T: %v", err, err)
	}

	if pe.Problem.Status != 422 {
		t.Fatalf("expected status 422, got %d", pe.Problem.Status)
	}

	if len(pe.ValidationErrors) != 2 {
		t.Fatalf("expected 2 validation errors, got %d: %v", len(pe.ValidationErrors), pe.ValidationErrors)
	}

	for _, ve := range pe.ValidationErrors {
		if ve.Error != packages.ErrEmptyResponse.Error() {
			t.Fatalf("expected empty response error for %q, got %q", ve.Name, ve.Error)
		}
	}
}

func TestHTTPInstallValidationErrorsUnknownQuestion(t *testing.T) {
	c, _ := initInstallTestClient(t)

	err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
		"bogus":    "value",
	}, false, "")
	if err == nil {
		t.Fatal("expected error from validation")
	}

	var pe *ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ProblemError, got %T: %v", err, err)
	}

	if len(pe.ValidationErrors) != 1 {
		t.Fatalf("expected 1 validation error, got %d: %v", len(pe.ValidationErrors), pe.ValidationErrors)
	}

	if pe.ValidationErrors[0].Name != "bogus" {
		t.Fatalf("expected error for 'bogus', got %q", pe.ValidationErrors[0].Name)
	}
	if pe.ValidationErrors[0].Error != packages.ErrInvalidResponse.Error() {
		t.Fatalf("expected %q, got %q", packages.ErrInvalidResponse.Error(), pe.ValidationErrors[0].Error)
	}
}

// --- Reinstall ---

func TestHTTPReinstallPackage(t *testing.T) {
	c, inst := initInstallTestClient(t)

	// Install first time.
	responses := packages.Responses{"hostname": "example", "port": "8080"}
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", responses, false, ""); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Reinstall with different responses.
	responses2 := packages.Responses{"hostname": "newhost", "port": "9090"}
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", responses2, false, ""); err != nil {
		t.Fatalf("reinstall: %v", err)
	}

	calls := inst.GetCalls()
	// First install: ListInstalled + Install = 2
	// Reinstall: ListInstalled + Uninstall + Install = 3
	// Total = 5
	if len(calls) != 5 {
		methods := make([]string, len(calls))
		for i, c := range calls {
			methods[i] = c.Method
		}
		t.Fatalf("expected 5 calls, got %d: %v", len(calls), methods)
	}

	// Reinstall should: ListInstalled, Uninstall, Install
	if calls[2].Method != "ListInstalled" {
		t.Fatalf("call 2: expected ListInstalled, got %q", calls[2].Method)
	}
	if calls[3].Method != "Uninstall" {
		t.Fatalf("call 3: expected Uninstall, got %q", calls[3].Method)
	}
	if calls[4].Method != "Install" {
		t.Fatalf("call 4: expected Install, got %q", calls[4].Method)
	}

	// Verify new responses were used.
	newResp := calls[4].Args[3].(packages.Responses)
	if newResp["hostname"] != "newhost" {
		t.Fatalf("expected hostname %q, got %q", "newhost", newResp["hostname"])
	}
}

func TestHTTPReinstallPackageWithSystemd(t *testing.T) {
	c, _, sd := initInstallWithSystemdTestClient(t)

	// Install first.
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "testhost"}, false, ""); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Reinstall.
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "newhost"}, false, ""); err != nil {
		t.Fatalf("reinstall: %v", err)
	}

	calls := sd.GetCalls()
	// First install: InstallUnit, Start = 2
	// Reinstall: Stop, UninstallUnit, InstallUnit, Start = 4
	// Total = 6
	if len(calls) != 6 {
		methods := make([]string, len(calls))
		for i, c := range calls {
			methods[i] = c.Method
		}
		t.Fatalf("expected 6 systemd calls, got %d: %v", len(calls), methods)
	}

	// Reinstall teardown: Stop, UninstallUnit
	if calls[2].Args[1].(systemd.StatusAction) != systemd.Stop {
		t.Fatalf("call 2: expected Stop, got %v", calls[2].Args[1])
	}
	if calls[3].Method != "UninstallUnit" {
		t.Fatalf("call 3: expected UninstallUnit, got %q", calls[3].Method)
	}

	// Reinstall setup: InstallUnit, Start
	if calls[4].Method != "InstallUnit" {
		t.Fatalf("call 4: expected InstallUnit, got %q", calls[4].Method)
	}
	if calls[5].Args[1].(systemd.StatusAction) != systemd.Start {
		t.Fatalf("call 5: expected Start, got %v", calls[5].Args[1])
	}
}

// --- Settings tests ---

func initSettingsTestClient(t *testing.T) (*SystemdClient, string) {
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

	settingsMgr, err := account.InitSettingsManager(db)
	if err != nil {
		t.Fatalf("InitSettingsManager: %v", err)
	}

	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, AccountMgr: mgr, SessionMgr: sessMgr, AuditMgr: auditMgr, SettingsMgr: settingsMgr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Bootstrap: create admin account and authenticate
	if _, err := c.CreateAccount(context.TODO(), "testadmin", "adminpass", "admin@test.com", "555-0000", "Test Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "testadmin", "adminpass")
	if err != nil {
		t.Fatalf("bootstrap Authenticate: %v", err)
	}
	c.Token = resp.Token

	return c, resp.Token
}

func TestHTTPSettingsDefaultsOnInit(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	// Defaults should be present without any explicit Set calls.
	val, err := c.GetSetting(context.TODO(), "default_quota")
	if err != nil {
		t.Fatalf("GetSetting default_quota: %v", err)
	}
	if val != account.DefaultSettings["default_quota"] {
		t.Fatalf("expected default %q, got %q", account.DefaultSettings["default_quota"], val)
	}

	// List should include all defaults.
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

func TestHTTPSettingsSetAndGet(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	// Override the seeded default.
	if err := c.SetSetting(context.TODO(), "default_quota", "0"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "default_quota")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "0" {
		t.Fatalf("expected %q, got %q", "0", val)
	}
}

func TestHTTPSettingsGetNotFound(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	_, err := c.GetSetting(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent setting")
	}
}

func TestHTTPSettingsList(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	// Add a custom setting alongside the seeded defaults.
	if err := c.SetSetting(context.TODO(), "custom_key", "hello"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	settings, err := c.GetSettings(context.TODO())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}

	// Should have all defaults plus the custom key.
	wantLen := len(account.DefaultSettings) + 1
	if len(settings) != wantLen {
		t.Fatalf("expected %d settings, got %d: %v", wantLen, len(settings), settings)
	}
	if settings["default_quota"] != account.DefaultSettings["default_quota"] {
		t.Fatalf("expected default_quota %q, got %q", account.DefaultSettings["default_quota"], settings["default_quota"])
	}
	if settings["custom_key"] != "hello" {
		t.Fatalf("expected custom_key %q, got %q", "hello", settings["custom_key"])
	}
}

func TestHTTPSettingsOverwrite(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	if err := c.SetSetting(context.TODO(), "default_quota", "100"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := c.SetSetting(context.TODO(), "default_quota", "200"); err != nil {
		t.Fatalf("SetSetting overwrite: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "default_quota")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "200" {
		t.Fatalf("expected %q, got %q", "200", val)
	}
}

func TestHTTPSettingsSetNewKey(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	if err := c.SetSetting(context.TODO(), "motd", "welcome"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "motd")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "welcome" {
		t.Fatalf("expected %q, got %q", "welcome", val)
	}
}

func TestHTTPSettingsSetAuditLog(t *testing.T) {
	c, token := initSettingsTestClient(t)

	if err := c.SetSetting(context.TODO(), "default_quota", "0"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	page, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{}, token)
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}

	found := false
	for _, e := range page.Entries {
		if e.Action == "update setting" && e.Path == "/settings/set" {
			found = true
			if !e.Success {
				t.Fatal("expected success to be true")
			}
			if e.Account != "testadmin" {
				t.Fatalf("expected account %q, got %q", "testadmin", e.Account)
			}
			if e.Detail == "" {
				t.Fatal("expected non-empty audit detail")
			}
			break
		}
	}
	if !found {
		t.Fatal("expected to find 'update setting' audit entry")
	}
}

func TestHTTPSettingsGetNotAudited(t *testing.T) {
	c, token := initSettingsTestClient(t)

	// Read operations should not appear in audit log.
	if _, err := c.GetSetting(context.TODO(), "default_quota"); err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if _, err := c.GetSettings(context.TODO()); err != nil {
		t.Fatalf("GetSettings: %v", err)
	}

	page, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{}, token)
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}

	for _, e := range page.Entries {
		if e.Path == "/settings/get" || e.Path == "/settings" {
			t.Fatalf("read-only settings path %q should not be audited", e.Path)
		}
	}
}

func TestHTTPSettingsRequiresAdmin(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	// Create a non-admin user.
	if _, err := c.CreateAccount(context.TODO(), "user", "password1", "u@b.com", "555", "User", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "user", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
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

// --- Settings byte-value normalization tests ---

func TestHTTPSettingsQuotaHumanReadable(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	table := []struct {
		input    string
		expected string
	}{
		{"500GB", "536870912000"},
		{"500gb", "536870912000"},
		{"1TB", "1099511627776"},
		{"100MB", "104857600"},
		{"0", "0"},
		{"1073741824", "1073741824"},
	}

	for _, tc := range table {
		if err := c.SetSetting(context.TODO(), "default_quota", tc.input); err != nil {
			t.Fatalf("SetSetting(%q): %v", tc.input, err)
		}
		val, err := c.GetSetting(context.TODO(), "default_quota")
		if err != nil {
			t.Fatalf("GetSetting after setting %q: %v", tc.input, err)
		}
		if val != tc.expected {
			t.Fatalf("SetSetting(%q): expected stored value %q, got %q", tc.input, tc.expected, val)
		}
	}
}

func TestHTTPSettingsQuotaInvalidValue(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	badValues := []string{"not-a-number", "-5GB", "abc"}
	for _, v := range badValues {
		if err := c.SetSetting(context.TODO(), "default_quota", v); err == nil {
			t.Fatalf("expected error for invalid quota value %q", v)
		}
	}
}

func TestHTTPSettingsNonQuotaKeyNotNormalized(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	// Non-quota keys should store values as-is without byte parsing.
	if err := c.SetSetting(context.TODO(), "motd", "500GB"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	val, err := c.GetSetting(context.TODO(), "motd")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "500GB" {
		t.Fatalf("expected non-quota key to store raw value %q, got %q", "500GB", val)
	}
}

// --- MockClient Settings tests ---

func TestMockClientSettingsDefaults(t *testing.T) {
	m := InitMockClient()

	// Defaults should be present immediately.
	val, err := m.GetSetting(context.TODO(), "default_quota")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != account.DefaultSettings["default_quota"] {
		t.Fatalf("expected %q, got %q", account.DefaultSettings["default_quota"], val)
	}

	settings, err := m.GetSettings(context.TODO())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if len(settings) != len(account.DefaultSettings) {
		t.Fatalf("expected %d settings, got %d", len(account.DefaultSettings), len(settings))
	}
}

func TestMockClientSettingsOverride(t *testing.T) {
	m := InitMockClient()

	if err := m.SetSetting(context.TODO(), "default_quota", "50"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	val, err := m.GetSetting(context.TODO(), "default_quota")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "50" {
		t.Fatalf("expected %q, got %q", "50", val)
	}
}

func TestMockClientGetSettingNotFound(t *testing.T) {
	m := InitMockClient()

	_, err := m.GetSetting(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent setting")
	}
}

// --- Multi-version and volume management tests ---

func TestHTTPListPackageVersions(t *testing.T) {
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

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	versions, err := c.ListPackageVersions(context.TODO(), "nginx")
	if err != nil {
		t.Fatalf("ListPackageVersions: %v", err)
	}

	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d: %v", len(versions), versions)
	}

	// Should be sorted highest first.
	if versions[0] != "2.0" {
		t.Fatalf("expected first version %q, got %q", "2.0", versions[0])
	}
	if versions[1] != "1.0" {
		t.Fatalf("expected second version %q, got %q", "1.0", versions[1])
	}
}

func TestHTTPInstallPackageNewVolumePaths(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	pkgYAML := `image: nginx:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
  logs:
    mountpoint: /var/log/app
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	fs := mockCtrl.GetFilesystems()
	found := map[string]bool{}
	for _, f := range fs {
		found[f.Name] = true
	}

	expectedData := "installed/nginx/1.0/data"
	expectedLogs := "installed/nginx/1.0/logs"
	if !found[expectedData] {
		t.Fatalf("expected filesystem %q, got: %v", expectedData, fs)
	}
	if !found[expectedLogs] {
		t.Fatalf("expected filesystem %q, got: %v", expectedLogs, fs)
	}
}

func TestHTTPUninstallPreservesVolumes(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	pkgYAML := `image: nginx:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Uninstall WITHOUT purge.
	if err := c.UninstallPackage(context.TODO(), "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	fs := mockCtrl.GetFilesystems()
	found := map[string]bool{}
	for _, f := range fs {
		found[f.Name] = true
	}

	// installed/nginx should be gone (renamed to uninstalled/nginx).
	for name := range found {
		if strings.HasPrefix(name, "installed/nginx") {
			t.Fatalf("expected installed/nginx volumes to be renamed away, found %q", name)
		}
	}

	// uninstalled/nginx/... should exist.
	if !found["uninstalled/nginx"] {
		t.Fatalf("expected uninstalled/nginx to exist, got: %v", fs)
	}
	if !found["uninstalled/nginx/1.0/data"] {
		t.Fatalf("expected uninstalled/nginx/1.0/data to exist, got: %v", fs)
	}
}

func TestHTTPUninstallWithOtherVersionsPreservesAll(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	volYAML := func(version string) string {
		return fmt.Sprintf(`image: nginx:%s
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
questions: {}
`, version)
	}

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", volYAML("1.0"))
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", volYAML("2.0"))

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install nginx 1.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	// Install nginx 2.0 (upgrade: stops 1.0 unit, keeps old install record).
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage 2.0: %v", err)
	}

	// Uninstall nginx 1.0 without purge.
	if err := c.UninstallPackage(context.TODO(), "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage 1.0: %v", err)
	}

	// Because nginx 2.0 is still installed, volumes should NOT be renamed.
	fs := mockCtrl.GetFilesystems()
	found := map[string]bool{}
	for _, f := range fs {
		found[f.Name] = true
	}

	// installed/nginx/... should still exist (not moved to uninstalled).
	if !found["installed/nginx/1.0/data"] {
		t.Fatalf("expected installed/nginx/1.0/data to still exist, got: %v", fs)
	}
	if !found["installed/nginx/2.0/data"] {
		t.Fatalf("expected installed/nginx/2.0/data to still exist, got: %v", fs)
	}

	for name := range found {
		if strings.HasPrefix(name, "uninstalled/") {
			t.Fatalf("expected no uninstalled volumes, found %q", name)
		}
	}
}

func TestHTTPInstallWithVolumeReuse(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	pkgYAML := `image: nginx:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install, then uninstall without purge (volumes move to uninstalled).
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}
	if err := c.UninstallPackage(context.TODO(), "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	// Verify volumes were moved to uninstalled.
	midFS := mockCtrl.GetFilesystems()
	midFound := map[string]bool{}
	for _, f := range midFS {
		midFound[f.Name] = true
	}
	if !midFound["uninstalled/nginx"] {
		t.Fatalf("expected uninstalled/nginx after uninstall, got: %v", midFS)
	}

	// Reinstall with reuseVolumes=true.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, true, ""); err != nil {
		t.Fatalf("InstallPackage with reuse: %v", err)
	}

	// Verify volumes were renamed back from uninstalled to installed.
	afterFS := mockCtrl.GetFilesystems()
	afterFound := map[string]bool{}
	for _, f := range afterFS {
		afterFound[f.Name] = true
	}

	if !afterFound["installed/nginx/1.0/data"] {
		t.Fatalf("expected installed/nginx/1.0/data after reuse, got: %v", afterFS)
	}

	for name := range afterFound {
		if strings.HasPrefix(name, "uninstalled/nginx") {
			t.Fatalf("expected no uninstalled/nginx after reuse, found %q", name)
		}
	}
}

func TestHTTPInstallWithImport(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	volYAML := func(version string) string {
		return fmt.Sprintf(`image: nginx:%s
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
questions: {}
`, version)
	}

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", volYAML("1.0"))
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", volYAML("2.0"))

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install nginx 1.0 first.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	// Install nginx 2.0 with importFromVersion="1.0".
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", packages.Responses{}, false, "1.0"); err != nil {
		t.Fatalf("InstallPackage 2.0 with import: %v", err)
	}

	// Verify that the 2.0 volume exists (created via snapshot from 1.0).
	fs := mockCtrl.GetFilesystems()
	found := map[string]bool{}
	for _, f := range fs {
		found[f.Name] = true
	}

	expected20 := "installed/nginx/2.0/data"
	if !found[expected20] {
		t.Fatalf("expected %q to exist after import, got: %v", expected20, fs)
	}

	// Verify snapshot was called by checking the mock controller log.
	callLog := mockCtrl.GetLog()
	snapshotFound := false
	for _, entry := range callLog {
		if entry.Operation == "SubvolSnapshot" {
			snapshotFound = true
			break
		}
	}
	if !snapshotFound {
		t.Fatal("expected SubvolSnapshot to be called for import")
	}
}

func TestHTTPPurgeUninstalledVolumes(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Inject filesystems under uninstalled/nginx/ to simulate preserved volumes.
	injectSubvol(t, mockCtrl, "uninstalled/nginx/1.0/data", 0)
	injectSubvol(t, mockCtrl, "uninstalled/nginx/1.0/logs", 0)

	// Also inject a volume for a different package to ensure it is not affected.
	injectSubvol(t, mockCtrl, "uninstalled/redis/7.0/data", 0)

	// Purge uninstalled volumes for nginx.
	if err := c.PurgeUninstalledVolumes(context.TODO(), "nginx"); err != nil {
		t.Fatalf("PurgeUninstalledVolumes: %v", err)
	}

	// Verify nginx uninstalled volumes are gone.
	fs := mockCtrl.GetFilesystems()
	for _, f := range fs {
		if strings.HasPrefix(f.Name, "uninstalled/nginx") {
			t.Fatalf("expected uninstalled/nginx volumes to be purged, found %q", f.Name)
		}
	}

	// Verify redis uninstalled volumes are intact.
	found := map[string]bool{}
	for _, f := range fs {
		found[f.Name] = true
	}
	if !found["uninstalled/redis/7.0/data"] {
		t.Fatalf("expected uninstalled/redis/7.0/data to be preserved, got: %v", fs)
	}
}

func TestHTTPUpgradeStopsOldUnit(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	nginx10 := `image: nginx:1.0
environment:
  NGINX_HOST: "@hostname@"
network:
  external: {}
  internal: {}
volumes: {}
questions:
  hostname:
    query: "What hostname should nginx serve?"
    type: hostname
`
	nginx20 := `image: nginx:2.0
environment:
  NGINX_HOST: "@hostname@"
network:
  external: {}
  internal: {}
volumes: {}
questions:
  hostname:
    query: "What hostname should nginx serve?"
    type: hostname
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", nginx20)

	inst := packages.InitMockInstallManager()
	sd := systemd.InitMockManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst, Systemd: sd})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install nginx 1.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "testhost"}, false, ""); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	// Upgrade to nginx 2.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", packages.Responses{"hostname": "testhost"}, false, ""); err != nil {
		t.Fatalf("InstallPackage 2.0: %v", err)
	}

	calls := sd.GetCalls()
	// First install: InstallUnit + Start = 2
	// Upgrade: Stop + UninstallUnit + InstallUnit + Start = 4
	// Total = 6
	if len(calls) != 6 {
		methods := make([]string, len(calls))
		for i, cl := range calls {
			methods[i] = cl.Method
		}
		t.Fatalf("expected 6 systemd calls, got %d: %v", len(calls), methods)
	}

	// First install: InstallUnit, Start
	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	if calls[1].Args[1].(systemd.StatusAction) != systemd.Start {
		t.Fatalf("call 1: expected Start, got %v", calls[1].Args[1])
	}

	// Upgrade teardown: Stop, UninstallUnit
	if calls[2].Args[1].(systemd.StatusAction) != systemd.Stop {
		t.Fatalf("call 2: expected Stop, got %v", calls[2].Args[1])
	}
	if calls[3].Method != "UninstallUnit" {
		t.Fatalf("call 3: expected UninstallUnit, got %q", calls[3].Method)
	}

	// Upgrade setup: InstallUnit, Start
	if calls[4].Method != "InstallUnit" {
		t.Fatalf("call 4: expected InstallUnit, got %q", calls[4].Method)
	}
	if calls[5].Args[1].(systemd.StatusAction) != systemd.Start {
		t.Fatalf("call 5: expected Start, got %v", calls[5].Args[1])
	}
}

func TestHTTPListPackagesIncludesInstalledOlderVersions(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	// Only nginx 2.0 is in the repo.
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")

	// But nginx 1.0 was previously installed (still registered).
	inst := packages.InitMockInstallManager()
	inst.Installed = []packages.PackageIdentity{{Name: "nginx", Version: "1.0"}}

	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	pkgs, err := c.ListPackages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}

	// The repo has nginx@2.0. The installed list has nginx@1.0.
	// ListPackages merges both: since both share the name "nginx", the repo
	// entry (nginx@2.0) is already listed, and the installed entry (nginx@1.0)
	// has the same name so it should NOT be duplicated (the merge logic dedupes
	// by package name). Therefore we expect exactly 1 entry: nginx@2.0.
	found := map[string]bool{}
	for _, entry := range pkgs.Entries {
		found[entry] = true
	}

	if !found["nginx@2.0"] {
		t.Fatalf("expected nginx@2.0 in packages list, got: %v", pkgs.Entries)
	}

	// The merge logic in listPackages checks by package name, not by
	// name@version. Since "nginx" is already known from the repo (nginx@2.0),
	// installed nginx@1.0 is not added again. This test verifies the merge
	// correctly handles the case where an older version is installed.
	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 package entry (repo provides latest), got %d: %v", len(pkgs.Entries), pkgs.Entries)
	}
}

// --- Older version selection tests ---

func TestHTTPInstallOlderVersion(t *testing.T) {
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

	// Explicitly install the older version 1.0 (not the latest 2.0).
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	calls := inst.GetCalls()
	// Should have: ListInstalled, Install
	found := false
	for _, call := range calls {
		if call.Method == "Install" {
			if call.Args[2].(string) != "1.0" {
				t.Fatalf("expected install version %q, got %v", "1.0", call.Args[2])
			}
			found = true
		}
	}
	if !found {
		t.Fatal("expected Install call, not found")
	}

	installed, err := c.ListInstalled(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	foundInstalled := false
	for _, pkg := range installed.Entries {
		if pkg == "nginx@1.0" {
			foundInstalled = true
		}
		if pkg == "nginx@2.0" {
			t.Fatal("nginx@2.0 should NOT be installed")
		}
	}
	if !foundInstalled {
		t.Fatalf("expected nginx@1.0 in installed list, got: %v", installed.Entries)
	}
}

func TestHTTPDowngradeFromNewerToOlderVersion(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	nginx := `image: nginx:latest
environment:
  NGINX_HOST: "@hostname@"
network:
  external: {}
  internal: {}
volumes: {}
questions:
  hostname:
    query: "What hostname?"
    type: hostname
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx)
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", nginx)

	inst := packages.InitMockInstallManager()
	sd := systemd.InitMockManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst, Systemd: sd})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install the newer version first.
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", packages.Responses{"hostname": "testhost"}, false, ""); err != nil {
		t.Fatalf("InstallPackage 2.0: %v", err)
	}

	// Now downgrade to the older version.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "testhost"}, false, ""); err != nil {
		t.Fatalf("InstallPackage 1.0 (downgrade): %v", err)
	}

	sdCalls := sd.GetCalls()
	// First install: InstallUnit + Start = 2
	// Downgrade: Stop + UninstallUnit + InstallUnit + Start = 4
	// Total = 6
	if len(sdCalls) != 6 {
		methods := make([]string, len(sdCalls))
		for i, cl := range sdCalls {
			methods[i] = cl.Method
		}
		t.Fatalf("expected 6 systemd calls, got %d: %v", len(sdCalls), methods)
	}

	// Downgrade teardown: Stop old unit
	if sdCalls[2].Args[1].(systemd.StatusAction) != systemd.Stop {
		t.Fatalf("call 2: expected Stop, got %v", sdCalls[2].Args[1])
	}
	// Downgrade teardown: UninstallUnit
	if sdCalls[3].Method != "UninstallUnit" {
		t.Fatalf("call 3: expected UninstallUnit, got %q", sdCalls[3].Method)
	}
	// Downgrade setup: InstallUnit with 1.0 content
	if sdCalls[4].Method != "InstallUnit" {
		t.Fatalf("call 4: expected InstallUnit, got %q", sdCalls[4].Method)
	}
	unitContent := sdCalls[4].Args[1].(string)
	if !strings.Contains(unitContent, "1.0") {
		t.Fatalf("expected unit content to reference version 1.0, got: %s", unitContent)
	}
	// Downgrade setup: Start new unit
	if sdCalls[5].Args[1].(systemd.StatusAction) != systemd.Start {
		t.Fatalf("call 5: expected Start, got %v", sdCalls[5].Args[1])
	}
}

func TestHTTPInstallOlderVersionWithQuestions(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	nginx10 := `image: nginx:1.0
environment:
  NGINX_HOST: "@hostname@"
network:
  external:
    "@port@": "80"
  internal: {}
volumes: {}
questions:
  hostname:
    query: "What hostname should nginx serve?"
    type: hostname
  port:
    query: "What external port?"
    type: port
`
	nginx20 := `image: nginx:2.0
environment: {}
network:
  external: {}
  internal: {}
volumes: {}
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", nginx20)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Fetch questions for the older version specifically.
	questions, err := c.GetPackageQuestionsByIdentity(context.TODO(), "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetPackageQuestionsByIdentity 1.0: %v", err)
	}
	if len(questions) != 2 {
		t.Fatalf("expected 2 questions for nginx@1.0, got %d", len(questions))
	}
	if _, ok := questions["hostname"]; !ok {
		t.Fatal("expected 'hostname' question for nginx@1.0")
	}
	if _, ok := questions["port"]; !ok {
		t.Fatal("expected 'port' question for nginx@1.0")
	}

	// Fetch questions for the newer version — should have none.
	questions20, err := c.GetPackageQuestionsByIdentity(context.TODO(), "nginx", "2.0")
	if err != nil {
		t.Fatalf("GetPackageQuestionsByIdentity 2.0: %v", err)
	}
	if len(questions20) != 0 {
		t.Fatalf("expected 0 questions for nginx@2.0, got %d", len(questions20))
	}

	// Install the older version with responses.
	responses := packages.Responses{"hostname": "myhost", "port": "9090"}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, ""); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	// Verify the install recorded the correct version and responses.
	calls := inst.GetCalls()
	for _, call := range calls {
		if call.Method == "Install" {
			if call.Args[2].(string) != "1.0" {
				t.Fatalf("expected version 1.0, got %v", call.Args[2])
			}
			r := call.Args[3].(packages.Responses)
			if r["hostname"] != "myhost" {
				t.Fatalf("expected hostname=myhost, got %v", r["hostname"])
			}
			if r["port"] != "9090" {
				t.Fatalf("expected port=9090, got %v", r["port"])
			}
		}
	}
}

func TestHTTPInstallOlderVersionWithVolumes(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	nginx10 := `image: nginx:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
  logs:
    mountpoint: /var/log/nginx
questions: {}
`
	nginx20 := `image: nginx:2.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", nginx20)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install the older version 1.0 which has two volumes (data + logs).
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, ""); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	fs := mockCtrl.GetFilesystems()
	volNames := map[string]bool{}
	for _, f := range fs {
		volNames[f.Name] = true
	}

	if !volNames["installed/nginx/1.0/data"] {
		t.Fatal("expected volume installed/nginx/1.0/data")
	}
	if !volNames["installed/nginx/1.0/logs"] {
		t.Fatal("expected volume installed/nginx/1.0/logs")
	}
	// Verify 2.0 volumes were NOT created.
	if volNames["installed/nginx/2.0/data"] {
		t.Fatal("installed/nginx/2.0/data should NOT exist")
	}
}

func TestHTTPInstallOlderVersionNotFound(t *testing.T) {
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

	// Try to install a version that doesn't exist.
	err = c.InstallPackage(context.TODO(), "nginx", "3.0", packages.Responses{}, false, "")
	if err == nil {
		t.Fatal("expected error installing nonexistent version 3.0")
	}
}

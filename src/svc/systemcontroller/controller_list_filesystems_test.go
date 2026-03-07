package systemcontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
)

func TestListFilesystemsEmpty(t *testing.T) {
	c, _ := initTestClient(t)

	fsResult, err := c.ListFilesystems(context.TODO(), "", "", ListParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fsResult.Entries) != 0 {
		t.Fatalf("expected 0 filesystems, got %d", len(fsResult.Entries))
	}
}

func TestListFilesystemsAll(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{"vol-a", "vol-b", "vol-c"} {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	fsResult, err := c.ListFilesystems(context.TODO(), "", "", ListParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fsResult.Entries) != 3 {
		t.Fatalf("expected 3 filesystems, got %d", len(fsResult.Entries))
	}

	found := map[string]bool{}
	for _, f := range fsResult.Entries {
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

	fsResult, err := c.ListFilesystems(context.TODO(), "app-", "", ListParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fsResult.Entries) != 2 {
		t.Fatalf("expected 2 filesystems with prefix 'app-', got %d", len(fsResult.Entries))
	}

	appNames := map[string]bool{}
	for _, f := range fsResult.Entries {
		appNames[f.Name] = true
	}
	for _, want := range []string{"app-web", "app-db"} {
		if !appNames[want] {
			t.Fatalf("expected filesystem %q in app- results", want)
		}
	}

	fsResult, err = c.ListFilesystems(context.TODO(), "data-", "", ListParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fsResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem with prefix 'data-', got %d", len(fsResult.Entries))
	}
	if fsResult.Entries[0].Name != "data-cache" {
		t.Fatalf("expected data-cache, got %s", fsResult.Entries[0].Name)
	}
}

func TestListFilesystemsPrefixNoMatch(t *testing.T) {
	c, _ := initTestClient(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "vol-a"}); err != nil {
		t.Fatalf("CreateFilesystem %q: %v", "vol-a", err)
	}

	fsResult, err := c.ListFilesystems(context.TODO(), "nope", "", ListParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fsResult.Entries) != 0 {
		t.Fatalf("expected 0 filesystems, got %d", len(fsResult.Entries))
	}
}

func TestListFilesystemsBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "storage"), bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

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

	fsResult, err := c.ListFilesystems(context.TODO(), "", "", ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	if len(fsResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem (root filtered out), got %d", len(fsResult.Entries))
	}

	if fsResult.Entries[0].Name != "user-vol" {
		t.Fatalf("expected %q, got %q", "user-vol", fsResult.Entries[0].Name)
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
		storage.SubvolInfo{Name: "installed/core/nginx", ID: 101},
		storage.SubvolInfo{Name: "installed/core/nginx/1.0/data", ID: 102},
		storage.SubvolInfo{Name: "uninstalled", ID: 200},
		storage.SubvolInfo{Name: "uninstalled/core/nginx", ID: 201},
	)
	controller.Lock.Unlock()

	// Create a normal filesystem via API
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "user-vol"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	fsResult, err := c.ListFilesystems(context.TODO(), "", "", ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	// Should see:
	//   user-vol (state="user")
	//   nginx (stripped from installed/core/nginx, state="installed")
	//   nginx/1.0/data (stripped from installed/core/nginx/1.0/data, state="installed")
	//   nginx (stripped from uninstalled/core/nginx, state="uninstalled")
	// Should NOT see: installed, uninstalled (root subvolumes).
	if len(fsResult.Entries) != 4 {
		t.Fatalf("expected 4 filesystems, got %d: %v", len(fsResult.Entries), fsResult.Entries)
	}

	type nameState struct {
		Name  string
		State string
	}
	got := make([]nameState, len(fsResult.Entries))
	for i, f := range fsResult.Entries {
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
	for _, f := range fsResult.Entries {
		if f.Name == "installed" || f.Name == "uninstalled" {
			t.Fatalf("expected root subvolume %q to be hidden, but it was visible", f.Name)
		}
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

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var fsResult PageResult[storage.Filesystem]
	if err := json.NewDecoder(resp.Body).Decode(&fsResult); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(fsResult.Entries) != 3 {
		t.Fatalf("expected 3 filesystems, got %d", len(fsResult.Entries))
	}

	if fsResult.Entries[0].Name != "alpha" {
		t.Fatalf("expected first filesystem %q, got %q", "alpha", fsResult.Entries[0].Name)
	}
	if fsResult.Entries[1].Name != "middle" {
		t.Fatalf("expected second filesystem %q, got %q", "middle", fsResult.Entries[1].Name)
	}
	if fsResult.Entries[2].Name != "zeta" {
		t.Fatalf("expected third filesystem %q, got %q", "zeta", fsResult.Entries[2].Name)
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

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var fsResult PageResult[storage.Filesystem]
	if err := json.NewDecoder(resp.Body).Decode(&fsResult); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(fsResult.Entries) != 3 {
		t.Fatalf("expected 3 filesystems, got %d", len(fsResult.Entries))
	}

	if fsResult.Entries[0].Name != "zeta" {
		t.Fatalf("expected first filesystem %q, got %q", "zeta", fsResult.Entries[0].Name)
	}
	if fsResult.Entries[2].Name != "alpha" {
		t.Fatalf("expected third filesystem %q, got %q", "alpha", fsResult.Entries[2].Name)
	}
}

func TestHTTPListFilesystemsPagination(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	// Page 1: limit=2, offset=0
	fsResult, err := c.ListFilesystems(context.TODO(), "", "", ListParams{Limit: 2, Offset: 0, SortBy: "name", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListFilesystems page1: %v", err)
	}
	if len(fsResult.Entries) != 2 {
		t.Fatalf("expected 2 entries on page1, got %d", len(fsResult.Entries))
	}
	if !fsResult.HasMore {
		t.Fatal("expected has_more=true on page1")
	}
	if fsResult.TotalCount != 5 {
		t.Fatalf("expected total_count=5, got %d", fsResult.TotalCount)
	}
	if fsResult.Entries[0].Name != "alpha" {
		t.Fatalf("expected first entry %q, got %q", "alpha", fsResult.Entries[0].Name)
	}

	// Page 3: limit=2, offset=4
	fsResult, err = c.ListFilesystems(context.TODO(), "", "", ListParams{Limit: 2, Offset: 4, SortBy: "name", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListFilesystems page3: %v", err)
	}
	if len(fsResult.Entries) != 1 {
		t.Fatalf("expected 1 entry on last page, got %d", len(fsResult.Entries))
	}
	if fsResult.HasMore {
		t.Fatal("expected has_more=false on last page")
	}
}

func TestHTTPListFilesystemsSearch(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{"app-data", "app-logs", "db-main"} {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	fsResult, err := c.ListFilesystems(context.TODO(), "", "", ListParams{Search: "app"})
	if err != nil {
		t.Fatalf("ListFilesystems search: %v", err)
	}
	if len(fsResult.Entries) != 2 {
		t.Fatalf("expected 2 results for search 'app', got %d", len(fsResult.Entries))
	}

	fsResult, err = c.ListFilesystems(context.TODO(), "", "", ListParams{Search: "db"})
	if err != nil {
		t.Fatalf("ListFilesystems search db: %v", err)
	}
	if len(fsResult.Entries) != 1 {
		t.Fatalf("expected 1 result for search 'db', got %d", len(fsResult.Entries))
	}

	fsResult, err = c.ListFilesystems(context.TODO(), "", "", ListParams{Search: "nope"})
	if err != nil {
		t.Fatalf("ListFilesystems search nope: %v", err)
	}
	if len(fsResult.Entries) != 0 {
		t.Fatalf("expected 0 results for search 'nope', got %d", len(fsResult.Entries))
	}
}

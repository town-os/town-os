package systemcontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/storage"
)

func TestHTTPCreatePage(t *testing.T) {
	mock := storage.InitBtrFSMock()
	basePath := t.TempDir()

	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	pages, err := InitPagesStore(db)
	if err != nil {
		t.Fatalf("InitPagesStore: %v", err)
	}

	ts := InitTestServer(ServerConfig{
		Storage:       mock,
		BtrfsBasePath: basePath,
		Pages:         pages,
	})
	t.Cleanup(ts.Close)

	body, err := json.Marshal(CreatePageRequest{
		Name:    "test-site",
		RepoURL: "https://github.com/example/site",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "/pages/create"), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var page Page
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Name != "test-site" {
		t.Fatalf("expected name test-site, got %q", page.Name)
	}
}

func TestHTTPCreatePageMissingFields(t *testing.T) {
	mock := storage.InitBtrFSMock()
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	pages, err := InitPagesStore(db)
	if err != nil {
		t.Fatalf("InitPagesStore: %v", err)
	}

	ts := InitTestServer(ServerConfig{Storage: mock, Pages: pages})
	t.Cleanup(ts.Close)

	body, err := json.Marshal(CreatePageRequest{Name: "test"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "/pages/create"), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHTTPListPages(t *testing.T) {
	mock := storage.InitBtrFSMock()
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	pages, err := InitPagesStore(db)
	if err != nil {
		t.Fatalf("InitPagesStore: %v", err)
	}

	_ = pages.Create(Page{Name: "site-a", RepoURL: "https://example.com/a"})
	_ = pages.Create(Page{Name: "site-b", RepoURL: "https://example.com/b"})

	ts := InitTestServer(ServerConfig{Storage: mock, Pages: pages})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, testRoute(t, ts.Server.URL, "/pages"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result PagesPage
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result.Entries))
	}
}

func TestHTTPRemovePage(t *testing.T) {
	mock := storage.InitBtrFSMock()
	basePath := t.TempDir()
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	pages, err := InitPagesStore(db)
	if err != nil {
		t.Fatalf("InitPagesStore: %v", err)
	}

	_ = pages.Create(Page{Name: "rm-me", RepoURL: "https://example.com/rm"})

	// Create the directory that would have been cloned.
	pageDir := filepath.Join(basePath, "pages", "rm-me")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	ts := InitTestServer(ServerConfig{Storage: mock, BtrfsBasePath: basePath, Pages: pages})
	t.Cleanup(ts.Close)

	body, err := json.Marshal(RemovePageRequest{Name: "rm-me"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "/pages/remove"), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	_, err = pages.Get("rm-me")
	if err == nil {
		t.Fatal("expected page to be removed from database")
	}

	// Verify the cloned directory was deleted.
	if _, err := os.Stat(pageDir); !os.IsNotExist(err) {
		t.Fatalf("expected page directory %s to be removed", pageDir)
	}
}

func TestHTTPUpdatePage(t *testing.T) {
	mock := storage.InitBtrFSMock()
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	pages, err := InitPagesStore(db)
	if err != nil {
		t.Fatalf("InitPagesStore: %v", err)
	}

	_ = pages.Create(Page{Name: "update-me", RepoURL: "https://example.com/old"})

	ts := InitTestServer(ServerConfig{Storage: mock, Pages: pages})
	t.Cleanup(ts.Close)

	body, err := json.Marshal(UpdatePageRequest{Name: "update-me", Branch: "develop"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "/pages/update"), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	page, err := pages.Get("update-me")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if page.Branch != "develop" {
		t.Fatalf("expected branch develop, got %q", page.Branch)
	}
}

func TestHTTPRebuildPagePull(t *testing.T) {
	mock := storage.InitBtrFSMock()
	basePath := t.TempDir()

	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	pages, err := InitPagesStore(db)
	if err != nil {
		t.Fatalf("InitPagesStore: %v", err)
	}

	_ = pages.Create(Page{Name: "rebuild-me", RepoURL: "https://example.com/repo", Status: "active"})

	// Create a .git directory so rebuild uses Pull.
	pageDir := filepath.Join(basePath, "pages", "rebuild-me", ".git")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	gitMock := git.InitMockClient()
	ts := InitTestServer(ServerConfig{
		Storage:       mock,
		BtrfsBasePath: basePath,
		Pages:         pages,
		Git:           gitMock,
	})
	t.Cleanup(ts.Close)

	body, err := json.Marshal(RebuildPageRequest{Name: "rebuild-me"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "/pages/rebuild"), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify Pull was called with the correct path.
	calls := gitMock.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 git call, got %d", len(calls))
	}
	if calls[0].Method != "Pull" {
		t.Fatalf("expected Pull call, got %q", calls[0].Method)
	}
	expectedDest := filepath.Join(basePath, "pages", "rebuild-me")
	if calls[0].Args[0] != expectedDest {
		t.Fatalf("expected Pull dest %q, got %q", expectedDest, calls[0].Args[0])
	}

	// Verify status updated to active.
	page, err := pages.Get("rebuild-me")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if page.Status != "active" {
		t.Fatalf("expected status active, got %q", page.Status)
	}
}

func TestHTTPRebuildPageCloneFallback(t *testing.T) {
	mock := storage.InitBtrFSMock()
	basePath := t.TempDir()

	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	pages, err := InitPagesStore(db)
	if err != nil {
		t.Fatalf("InitPagesStore: %v", err)
	}

	_ = pages.Create(Page{Name: "no-git", RepoURL: "https://example.com/repo", Status: "error"})

	// Do NOT create .git directory -- rebuild should fall back to Clone.

	gitMock := git.InitMockClient()
	ts := InitTestServer(ServerConfig{
		Storage:       mock,
		BtrfsBasePath: basePath,
		Pages:         pages,
		Git:           gitMock,
	})
	t.Cleanup(ts.Close)

	body, err := json.Marshal(RebuildPageRequest{Name: "no-git"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "/pages/rebuild"), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify Clone was called (not Pull) since .git was missing.
	calls := gitMock.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 git call, got %d", len(calls))
	}
	if calls[0].Method != "Clone" {
		t.Fatalf("expected Clone call, got %q", calls[0].Method)
	}

	// Verify status updated to active.
	page, err := pages.Get("no-git")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if page.Status != "active" {
		t.Fatalf("expected status active, got %q", page.Status)
	}
}

func TestHTTPRebuildPageNotFound(t *testing.T) {
	mock := storage.InitBtrFSMock()

	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	pages, err := InitPagesStore(db)
	if err != nil {
		t.Fatalf("InitPagesStore: %v", err)
	}

	ts := InitTestServer(ServerConfig{Storage: mock, Pages: pages})
	t.Cleanup(ts.Close)

	body, err := json.Marshal(RebuildPageRequest{Name: "does-not-exist"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "/pages/rebuild"), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

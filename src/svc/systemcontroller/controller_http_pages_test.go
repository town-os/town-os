package systemcontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/account"
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
		t.Fatal("expected page to be removed")
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

package systemcontroller

import (
	"context"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
)

func initPagesTestClient(t *testing.T) *SystemdClient {
	t.Helper()
	mock := storage.InitBtrFSMock()

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

	pagesMgr := account.InitMockPagesManager()
	gitCloner := &packages.MockGitCloner{}

	ts := InitTestServer(ServerConfig{
		Storage:       mock,
		AccountMgr:    mgr,
		SessionMgr:    sessMgr,
		AuditMgr:      auditMgr,
		PagesMgr:      pagesMgr,
		GitCloner:     gitCloner,
		BtrfsBasePath: t.TempDir(),
	})
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

	return c
}

func TestHTTPCreatePage(t *testing.T) {
	c := initPagesTestClient(t)

	page, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if page.Name != "my-site" {
		t.Errorf("expected name %q, got %q", "my-site", page.Name)
	}
	if page.RepoURL != "https://github.com/user/site.git" {
		t.Errorf("expected repo_url %q, got %q", "https://github.com/user/site.git", page.RepoURL)
	}
	if page.Branch != "main" {
		t.Errorf("expected branch %q, got %q", "main", page.Branch)
	}
	if page.Domain != "site.example.com" {
		t.Errorf("expected domain %q, got %q", "site.example.com", page.Domain)
	}
}

func TestHTTPCreatePageDefaultDomain(t *testing.T) {
	c := initPagesTestClient(t)

	page, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if page.Domain != "my-site" {
		t.Errorf("expected default domain %q, got %q", "my-site", page.Domain)
	}
}

func TestHTTPCreatePageDuplicate(t *testing.T) {
	c := initPagesTestClient(t)

	_, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	_, err = c.CreatePage(context.TODO(), "my-site", "https://github.com/user/other.git", "main", "other.example.com")
	if err == nil {
		t.Fatal("expected error for duplicate page name")
	}
}

func TestHTTPUpdatePage(t *testing.T) {
	c := initPagesTestClient(t)

	_, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	newDomain := "new.example.com"
	updated, err := c.UpdatePage(context.TODO(), "my-site", account.PageSiteUpdate{Domain: &newDomain})
	if err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}

	if updated.Domain != newDomain {
		t.Errorf("expected domain %q, got %q", newDomain, updated.Domain)
	}
}

func TestHTTPUpdatePageNotFound(t *testing.T) {
	c := initPagesTestClient(t)

	newDomain := "new.example.com"
	_, err := c.UpdatePage(context.TODO(), "nonexistent", account.PageSiteUpdate{Domain: &newDomain})
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestHTTPRemovePage(t *testing.T) {
	c := initPagesTestClient(t)

	_, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if err := c.RemovePage(context.TODO(), "my-site"); err != nil {
		t.Fatalf("RemovePage: %v", err)
	}

	// Verify it's gone via list.
	pages, err := c.ListPages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 0 {
		t.Errorf("expected 0 pages after remove, got %d", len(pages.Entries))
	}
}

func TestHTTPRemovePageNotFound(t *testing.T) {
	c := initPagesTestClient(t)

	err := c.RemovePage(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestHTTPListPages(t *testing.T) {
	c := initPagesTestClient(t)

	// Empty list.
	pages, err := c.ListPages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 0 {
		t.Fatalf("expected 0 pages, got %d", len(pages.Entries))
	}

	// Create two pages.
	_, err = c.CreatePage(context.TODO(), "alpha", "https://github.com/user/alpha.git", "main", "alpha.example.com")
	if err != nil {
		t.Fatalf("CreatePage alpha: %v", err)
	}
	_, err = c.CreatePage(context.TODO(), "beta", "https://github.com/user/beta.git", "develop", "beta.example.com")
	if err != nil {
		t.Fatalf("CreatePage beta: %v", err)
	}

	pages, err = c.ListPages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages.Entries))
	}
}

func TestHTTPListPagesSearch(t *testing.T) {
	c := initPagesTestClient(t)

	_, err := c.CreatePage(context.TODO(), "alpha-site", "https://github.com/user/alpha.git", "main", "alpha.example.com")
	if err != nil {
		t.Fatalf("CreatePage alpha-site: %v", err)
	}
	_, err = c.CreatePage(context.TODO(), "beta-site", "https://github.com/user/beta.git", "develop", "beta.example.com")
	if err != nil {
		t.Fatalf("CreatePage beta-site: %v", err)
	}

	pages, err := c.ListPages(context.TODO(), ListParams{Search: "alpha"})
	if err != nil {
		t.Fatalf("ListPages with search: %v", err)
	}
	if len(pages.Entries) != 1 {
		t.Fatalf("expected 1 page matching search, got %d", len(pages.Entries))
	}
	if pages.Entries[0].Name != "alpha-site" {
		t.Errorf("expected %q, got %q", "alpha-site", pages.Entries[0].Name)
	}
}

func TestHTTPRebuildPage(t *testing.T) {
	c := initPagesTestClient(t)

	_, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	page, err := c.RebuildPage(context.TODO(), "my-site")
	if err != nil {
		t.Fatalf("RebuildPage: %v", err)
	}

	if page.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", page.Status)
	}
}

func TestHTTPRebuildPageNotFound(t *testing.T) {
	c := initPagesTestClient(t)

	_, err := c.RebuildPage(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestHTTPPagesRequireAuth(t *testing.T) {
	c := initPagesTestClient(t)
	c.Token = "" // Clear auth token.

	_, err := c.ListPages(context.TODO(), ListParams{})
	if err == nil {
		t.Fatal("expected auth error for ListPages without token")
	}
}

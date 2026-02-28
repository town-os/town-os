package integration_test

import (
	"context"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// --- Pages integration tests ---

func initSystemControllerPagesTest(t *testing.T) *systemcontroller.SystemdClient {
	t.Helper()
	c, _ := initSystemControllerPagesTestWithAudit(t)
	return c
}

func initSystemControllerPagesTestWithAudit(t *testing.T) (*systemcontroller.SystemdClient, account.AuditManager) {
	t.Helper()

	dir := t.TempDir()
	db, err := account.OpenDB(filepath.Join(dir, "test.db"))
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

	pagesMgr, err := account.InitPagesManager(db)
	if err != nil {
		t.Fatalf("InitPagesManager: %v", err)
	}

	mock := storage.InitBtrFSMock()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       mock,
		AccountMgr:    mgr,
		SessionMgr:    sessMgr,
		AuditMgr:      auditMgr,
		PagesMgr:      pagesMgr,
		Git:           git.InitMockClient(),
		BtrfsBasePath: dir,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Bootstrap: create admin account and authenticate.
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "adminpass")
	if err != nil {
		t.Fatalf("bootstrap Authenticate: %v", err)
	}
	c.Token = resp.Token

	return c, auditMgr
}

func TestPagesCreateAndList(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	// Empty list.
	pages, err := c.ListPages(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 0 {
		t.Fatalf("expected 0 pages, got %d", len(pages.Entries))
	}

	// Create a page.
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

	// List should now have one entry.
	pages, err = c.ListPages(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages.Entries))
	}
}

func TestPagesCreateDefaultDomain(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	page, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if page.Domain != "my-site" {
		t.Errorf("expected default domain %q, got %q", "my-site", page.Domain)
	}
}

func TestPagesCreateDuplicate(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	_, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/other.git", "main", "other.example.com")
	if err == nil {
		t.Fatal("expected error for duplicate page name")
	}
}

func TestPagesUpdate(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com"); err != nil {
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

func TestPagesUpdateNotFound(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	newDomain := "new.example.com"
	_, err := c.UpdatePage(context.TODO(), "nonexistent", account.PageSiteUpdate{Domain: &newDomain})
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestPagesRemove(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if err := c.RemovePage(context.TODO(), "my-site"); err != nil {
		t.Fatalf("RemovePage: %v", err)
	}

	pages, err := c.ListPages(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 0 {
		t.Errorf("expected 0 pages after remove, got %d", len(pages.Entries))
	}
}

func TestPagesRemoveNotFound(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	err := c.RemovePage(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestPagesListSearch(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "alpha-site", "https://github.com/user/alpha.git", "main", "alpha.example.com"); err != nil {
		t.Fatalf("CreatePage alpha-site: %v", err)
	}
	if _, err := c.CreatePage(context.TODO(), "beta-site", "https://github.com/user/beta.git", "develop", "beta.example.com"); err != nil {
		t.Fatalf("CreatePage beta-site: %v", err)
	}

	pages, err := c.ListPages(context.TODO(), systemcontroller.ListParams{Search: "alpha"})
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

func TestPagesRequireAuth(t *testing.T) {
	c := initSystemControllerPagesTest(t)
	c.Token = "" // Clear auth token.

	_, err := c.ListPages(context.TODO(), systemcontroller.ListParams{})
	if err == nil {
		t.Fatal("expected auth error for ListPages without token")
	}
}

func TestPagesRebuild(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	page, err := c.RebuildPage(context.TODO(), "my-site")
	if err != nil {
		t.Fatalf("RebuildPage: %v", err)
	}

	if page.Name != "my-site" {
		t.Errorf("expected name %q, got %q", "my-site", page.Name)
	}
}

func TestPagesRebuildNotFound(t *testing.T) {
	c := initSystemControllerPagesTest(t)

	_, err := c.RebuildPage(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestPagesAuditRebuildLogged(t *testing.T) {
	c, auditMgr := initSystemControllerPagesTestWithAudit(t)

	if _, err := c.CreatePage(context.TODO(), "audit-site", "https://github.com/user/site.git", "main", "audit.example.com"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if _, err := c.RebuildPage(context.TODO(), "audit-site"); err != nil {
		t.Fatalf("RebuildPage: %v", err)
	}

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	found := false
	for _, e := range page.Entries {
		if e.Action == "rebuild page" && e.Path == "/pages/rebuild" {
			found = true
			if !e.Success {
				t.Fatal("expected success to be true")
			}
			break
		}
	}
	if !found {
		t.Fatal("expected to find 'rebuild page' audit entry")
	}
}

func TestPagesAuditCreateLogged(t *testing.T) {
	c, auditMgr := initSystemControllerPagesTestWithAudit(t)

	if _, err := c.CreatePage(context.TODO(), "audit-site", "https://github.com/user/site.git", "main", "audit.example.com"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	found := false
	for _, e := range page.Entries {
		if e.Action == "create page" && e.Path == "/pages/create" {
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
		t.Fatal("expected to find 'create page' audit entry")
	}
}

func TestPagesAuditUpdateLogged(t *testing.T) {
	c, auditMgr := initSystemControllerPagesTestWithAudit(t)

	if _, err := c.CreatePage(context.TODO(), "audit-site", "https://github.com/user/site.git", "main", "audit.example.com"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	newDomain := "updated.example.com"
	if _, err := c.UpdatePage(context.TODO(), "audit-site", account.PageSiteUpdate{Domain: &newDomain}); err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	found := false
	for _, e := range page.Entries {
		if e.Action == "update page" && e.Path == "/pages/update" {
			found = true
			if !e.Success {
				t.Fatal("expected success to be true")
			}
			break
		}
	}
	if !found {
		t.Fatal("expected to find 'update page' audit entry")
	}
}

func TestPagesAuditRemoveLogged(t *testing.T) {
	c, auditMgr := initSystemControllerPagesTestWithAudit(t)

	if _, err := c.CreatePage(context.TODO(), "audit-site", "https://github.com/user/site.git", "main", "audit.example.com"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if err := c.RemovePage(context.TODO(), "audit-site"); err != nil {
		t.Fatalf("RemovePage: %v", err)
	}

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	found := false
	for _, e := range page.Entries {
		if e.Action == "remove page" && e.Path == "/pages/remove" {
			found = true
			if !e.Success {
				t.Fatal("expected success to be true")
			}
			break
		}
	}
	if !found {
		t.Fatal("expected to find 'remove page' audit entry")
	}
}

func TestPagesAuditListExcluded(t *testing.T) {
	c, auditMgr := initSystemControllerPagesTestWithAudit(t)

	// Call list pages multiple times - should NOT be audit logged.
	if _, err := c.ListPages(context.TODO(), systemcontroller.ListParams{}); err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if _, err := c.ListPages(context.TODO(), systemcontroller.ListParams{Search: "test"}); err != nil {
		t.Fatalf("ListPages: %v", err)
	}

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, e := range page.Entries {
		if e.Path == "/pages" {
			t.Fatal("expected /pages (list) to be excluded from audit log")
		}
	}
}

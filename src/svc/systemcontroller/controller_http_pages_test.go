// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/storage"
)

type pagesTestEnv struct {
	Client    *SystemdClient
	GitClient *git.MockClient
	AuditMgr  account.AuditManager
	Storage   *storage.MockBtrFSController
	BtrfsBase string
}

func initPagesTestClient(t *testing.T) (*SystemdClient, *git.MockClient) {
	t.Helper()
	env := initPagesTestEnv(t)
	return env.Client, env.GitClient
}

func initPagesTestClientWithAudit(t *testing.T) (*SystemdClient, account.AuditManager) {
	t.Helper()
	env := initPagesTestEnv(t)
	return env.Client, env.AuditMgr
}

func initPagesTestEnv(t *testing.T) pagesTestEnv {
	t.Helper()
	mock := storage.InitBtrFSMock()
	controller, ok := mock.Controller.(*storage.MockBtrFSController)
	if !ok {
		t.Fatal("expected *storage.MockBtrFSController")
	}

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
	gitClient := git.InitMockClient()

	btrfsBase := t.TempDir()
	ts := InitTestServer(ServerConfig{
		Storage:       mock,
		AccountMgr:    mgr,
		SessionMgr:    sessMgr,
		AuditMgr:      auditMgr,
		PagesMgr:      pagesMgr,
		Git:           gitClient,
		BtrfsBasePath: btrfsBase,
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

	return pagesTestEnv{
		Client:    c,
		GitClient: gitClient,
		AuditMgr:  auditMgr,
		Storage:   controller,
		BtrfsBase: btrfsBase,
	}
}

func TestHTTPCreatePageReturnsAsyncPending(t *testing.T) {
	c, _ := initPagesTestClient(t)

	page, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// The create endpoint should return immediately with pending status
	// while the clone happens asynchronously.
	if page.Status != "pending" {
		t.Errorf("expected status %q, got %q", "pending", page.Status)
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
	if page.SourceType != account.PageSourceGit {
		t.Errorf("expected source_type %q, got %q", account.PageSourceGit, page.SourceType)
	}

	// Wait briefly for the background goroutine to complete, then verify
	// the status transitioned to active.
	time.Sleep(100 * time.Millisecond)
	pages, err := c.ListPages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages.Entries))
	}
	if pages.Entries[0].Status != "active" {
		t.Errorf("expected status %q after async clone, got %q", "active", pages.Entries[0].Status)
	}
}

func TestHTTPCreatePageDefaultDomain(t *testing.T) {
	c, _ := initPagesTestClient(t)

	page, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "", account.PageSourceGit, "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if page.Domain != "my-site" {
		t.Errorf("expected default domain %q, got %q", "my-site", page.Domain)
	}
}

func TestHTTPCreatePageDuplicate(t *testing.T) {
	c, _ := initPagesTestClient(t)

	_, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	_, err = c.CreatePage(context.TODO(), "my-site", "https://github.com/user/other.git", "main", "other.example.com", account.PageSourceGit, "", "")
	if err == nil {
		t.Fatal("expected error for duplicate page name")
	}
}

func TestHTTPCreatePageArchiveSourceType(t *testing.T) {
	c, _ := initPagesTestClient(t)

	page, err := c.CreatePage(context.TODO(), "archive-site", "", "", "archive.example.com", account.PageSourceArchive, "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if page.SourceType != account.PageSourceArchive {
		t.Errorf("expected source_type %q, got %q", account.PageSourceArchive, page.SourceType)
	}
	if page.Status != "pending" {
		t.Errorf("expected status %q for archive page, got %q", "pending", page.Status)
	}
}

func TestHTTPCreatePageDefaultSourceType(t *testing.T) {
	c, _ := initPagesTestClient(t)

	page, err := c.CreatePage(context.TODO(), "default-site", "", "", "default.example.com", "", "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if page.SourceType != account.PageSourceArchive {
		t.Errorf("expected default source_type %q, got %q", account.PageSourceArchive, page.SourceType)
	}
}

func TestHTTPCreatePageContainerImageSourceType(t *testing.T) {
	c, _ := initPagesTestClient(t)

	page, err := c.CreatePage(context.TODO(), "image-site", "", "", "image.example.com", account.PageSourceContainerImage, "nginx:latest", "/usr/share/nginx/html")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if page.SourceType != account.PageSourceContainerImage {
		t.Errorf("expected source_type %q, got %q", account.PageSourceContainerImage, page.SourceType)
	}
	if page.Image != "nginx:latest" {
		t.Errorf("expected image %q, got %q", "nginx:latest", page.Image)
	}
	if page.ImageDirectory != "/usr/share/nginx/html" {
		t.Errorf("expected image_directory %q, got %q", "/usr/share/nginx/html", page.ImageDirectory)
	}
}

func TestHTTPUpdatePage(t *testing.T) {
	c, _ := initPagesTestClient(t)

	_, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "")
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
	c, _ := initPagesTestClient(t)

	newDomain := "new.example.com"
	_, err := c.UpdatePage(context.TODO(), "nonexistent", account.PageSiteUpdate{Domain: &newDomain})
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestHTTPRemovePage(t *testing.T) {
	c, _ := initPagesTestClient(t)

	_, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "")
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
	c, _ := initPagesTestClient(t)

	err := c.RemovePage(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestHTTPListPages(t *testing.T) {
	c, _ := initPagesTestClient(t)

	// Empty list.
	pages, err := c.ListPages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 0 {
		t.Fatalf("expected 0 pages, got %d", len(pages.Entries))
	}

	// Create two pages.
	_, err = c.CreatePage(context.TODO(), "alpha", "https://github.com/user/alpha.git", "main", "alpha.example.com", account.PageSourceGit, "", "")
	if err != nil {
		t.Fatalf("CreatePage alpha: %v", err)
	}
	_, err = c.CreatePage(context.TODO(), "beta", "https://github.com/user/beta.git", "develop", "beta.example.com", account.PageSourceGit, "", "")
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
	c, _ := initPagesTestClient(t)

	_, err := c.CreatePage(context.TODO(), "alpha-site", "https://github.com/user/alpha.git", "main", "alpha.example.com", account.PageSourceGit, "", "")
	if err != nil {
		t.Fatalf("CreatePage alpha-site: %v", err)
	}
	_, err = c.CreatePage(context.TODO(), "beta-site", "https://github.com/user/beta.git", "develop", "beta.example.com", account.PageSourceGit, "", "")
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
	c, _ := initPagesTestClient(t)

	_, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// Wait for async clone to complete.
	time.Sleep(100 * time.Millisecond)

	page, err := c.RebuildPage(context.TODO(), "my-site")
	if err != nil {
		t.Fatalf("RebuildPage: %v", err)
	}

	if page.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", page.Status)
	}
}

func TestHTTPRebuildPageNotFound(t *testing.T) {
	c, _ := initPagesTestClient(t)

	_, err := c.RebuildPage(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestHTTPPagesRequireAuth(t *testing.T) {
	c, _ := initPagesTestClient(t)
	c.Token = "" // Clear auth token.

	_, err := c.ListPages(context.TODO(), ListParams{})
	if err == nil {
		t.Fatal("expected auth error for ListPages without token")
	}
}

func TestHTTPCreatePageCloneFailureSetsErrorStatus(t *testing.T) {
	c, gitClient := initPagesTestClient(t)

	gitClient.CloneErr = errors.New("clone failed: repository not found")

	page, err := c.CreatePage(context.TODO(), "bad-site", "https://github.com/user/missing.git", "main", "bad.example.com", account.PageSourceGit, "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// Initial response should still be pending.
	if page.Status != "pending" {
		t.Errorf("expected status %q, got %q", "pending", page.Status)
	}

	// Wait for the async goroutine to process the clone error.
	time.Sleep(100 * time.Millisecond)

	pages, err := c.ListPages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages.Entries))
	}
	if pages.Entries[0].Status != "error" {
		t.Errorf("expected status %q after clone failure, got %q", "error", pages.Entries[0].Status)
	}
}

func TestHTTPRebuildPageCloneFailureSetsErrorStatus(t *testing.T) {
	c, gitClient := initPagesTestClient(t)

	_, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// Wait for initial async clone to complete.
	time.Sleep(100 * time.Millisecond)

	// Now set clone to fail for the rebuild (no .git dir exists in mock, so rebuild clones).
	gitClient.CloneErr = errors.New("clone failed: network error")

	_, err = c.RebuildPage(context.TODO(), "my-site")
	if err == nil {
		t.Fatal("expected error from RebuildPage when clone fails")
	}

	// Verify the page status was set to error.
	pages, err := c.ListPages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages.Entries))
	}
	if pages.Entries[0].Status != "error" {
		t.Errorf("expected status %q after rebuild failure, got %q", "error", pages.Entries[0].Status)
	}
}

func TestHTTPRebuildArchivePageReturnsError(t *testing.T) {
	c, _ := initPagesTestClient(t)

	_, err := c.CreatePage(context.TODO(), "archive-site", "", "", "archive.example.com", account.PageSourceArchive, "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	_, err = c.RebuildPage(context.TODO(), "archive-site")
	if err == nil {
		t.Fatal("expected error when rebuilding archive page")
	}
}

func TestHTTPPagesAuditCreatePage(t *testing.T) {
	c, auditMgr := initPagesTestClientWithAudit(t)

	if _, err := c.CreatePage(context.TODO(), "audit-site", "https://github.com/user/site.git", "main", "audit.example.com", account.PageSourceGit, "", ""); err != nil {
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
			if e.Account != "testadmin" {
				t.Fatalf("expected account %q, got %q", "testadmin", e.Account)
			}
			if e.Detail == "" {
				t.Fatal("expected non-empty detail")
			}
			break
		}
	}
	if !found {
		t.Fatal("expected to find 'create page' audit entry")
	}
}

func TestHTTPPagesAuditUpdatePage(t *testing.T) {
	c, auditMgr := initPagesTestClientWithAudit(t)

	if _, err := c.CreatePage(context.TODO(), "audit-site", "https://github.com/user/site.git", "main", "audit.example.com", account.PageSourceGit, "", ""); err != nil {
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

func TestHTTPPagesAuditRemovePage(t *testing.T) {
	c, auditMgr := initPagesTestClientWithAudit(t)

	if _, err := c.CreatePage(context.TODO(), "audit-site", "https://github.com/user/site.git", "main", "audit.example.com", account.PageSourceGit, "", ""); err != nil {
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

func TestHTTPPagesAuditRebuildPage(t *testing.T) {
	c, auditMgr := initPagesTestClientWithAudit(t)

	if _, err := c.CreatePage(context.TODO(), "audit-site", "https://github.com/user/site.git", "main", "audit.example.com", account.PageSourceGit, "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// Wait for async clone to complete before rebuilding.
	time.Sleep(100 * time.Millisecond)

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

func TestHTTPPagesAuditListExcluded(t *testing.T) {
	c, auditMgr := initPagesTestClientWithAudit(t)

	// Call list pages - should NOT be audit logged.
	if _, err := c.ListPages(context.TODO(), ListParams{}); err != nil {
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

func TestHTTPListPagesIncludesSourceType(t *testing.T) {
	c, _ := initPagesTestClient(t)

	_, err := c.CreatePage(context.TODO(), "archive-page", "", "", "archive.example.com", account.PageSourceArchive, "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	_, err = c.CreatePage(context.TODO(), "git-page", "https://github.com/user/site.git", "main", "git.example.com", account.PageSourceGit, "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	pages, err := c.ListPages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages.Entries))
	}

	for _, p := range pages.Entries {
		if p.Name == "archive-page" && p.SourceType != account.PageSourceArchive {
			t.Errorf("expected source_type %q for archive-page, got %q", account.PageSourceArchive, p.SourceType)
		}
		if p.Name == "git-page" && p.SourceType != account.PageSourceGit {
			t.Errorf("expected source_type %q for git-page, got %q", account.PageSourceGit, p.SourceType)
		}
	}
}

func TestHTTPCreatePageCreatesSubvolume(t *testing.T) {
	env := initPagesTestEnv(t)

	_, err := env.Client.CreatePage(context.TODO(), "my-site", "", "", "site.example.com", account.PageSourceArchive, "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	fs := env.Storage.GetFilesystems()
	found := false
	for _, f := range fs {
		if f.Name == "pages/my-site" {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, len(fs))
		for i, f := range fs {
			names[i] = f.Name
		}
		t.Fatalf("expected pages/my-site subvolume, got %v", names)
	}
}

func TestHTTPRemovePageRemovesSubvolume(t *testing.T) {
	env := initPagesTestEnv(t)

	_, err := env.Client.CreatePage(context.TODO(), "my-site", "", "", "site.example.com", account.PageSourceArchive, "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if err := env.Client.RemovePage(context.TODO(), "my-site"); err != nil {
		t.Fatalf("RemovePage: %v", err)
	}

	fs := env.Storage.GetFilesystems()
	for _, f := range fs {
		if f.Name == "pages/my-site" {
			t.Fatal("expected pages/my-site subvolume to be removed")
		}
	}
}

func TestHTTPCreatePageSymlinkCreated(t *testing.T) {
	env := initPagesTestEnv(t)

	// Ensure the webroot directory exists.
	if err := EnsurePagesWebroot(env.BtrfsBase); err != nil {
		t.Fatalf("EnsurePagesWebroot: %v", err)
	}

	_, err := env.Client.CreatePage(context.TODO(), "my-site", "", "", "site.example.com", account.PageSourceArchive, "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	linkPath := filepath.Join(env.BtrfsBase, PagesWebrootDir, "my-site")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}

	expected := "/data/pages/my-site"
	if target != expected {
		t.Fatalf("expected symlink target %q, got %q", expected, target)
	}
}

func TestHTTPRemovePageRemovesSymlink(t *testing.T) {
	env := initPagesTestEnv(t)

	// Ensure the webroot directory exists.
	if err := EnsurePagesWebroot(env.BtrfsBase); err != nil {
		t.Fatalf("EnsurePagesWebroot: %v", err)
	}

	_, err := env.Client.CreatePage(context.TODO(), "my-site", "", "", "site.example.com", account.PageSourceArchive, "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// Verify symlink was created.
	linkPath := filepath.Join(env.BtrfsBase, PagesWebrootDir, "my-site")
	if _, err := os.Readlink(linkPath); err != nil {
		t.Fatalf("expected symlink to exist after create: %v", err)
	}

	// Remove the page.
	if err := env.Client.RemovePage(context.TODO(), "my-site"); err != nil {
		t.Fatalf("RemovePage: %v", err)
	}

	// Verify symlink was removed.
	if _, err := os.Lstat(linkPath); !os.IsNotExist(err) {
		t.Fatal("expected symlink to be removed after page removal")
	}
}

// createTestTarGz builds a small gzip-compressed tar archive containing a
// single index.html file, suitable for upload handler tests.
func createTestTarGz(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	content := []byte("<html><body>Hello</body></html>")
	if err := tw.WriteHeader(&tar.Header{
		Name: "index.html",
		Size: int64(len(content)),
		Mode: 0o644,
	}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar.Close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip.Close: %v", err)
	}
	return &buf
}

func TestHTTPCreatePageContainerImageReturnsAsyncPending(t *testing.T) {
	c, _ := initPagesTestClient(t)

	page, err := c.CreatePage(context.TODO(), "image-site", "", "", "image.example.com", account.PageSourceContainerImage, "nginx:latest", "/usr/share/nginx/html")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// The create endpoint should return immediately with pending status
	// while the image extraction happens asynchronously.
	if page.Status != "pending" {
		t.Errorf("expected status %q, got %q", "pending", page.Status)
	}

	if page.Name != "image-site" {
		t.Errorf("expected name %q, got %q", "image-site", page.Name)
	}
	if page.SourceType != account.PageSourceContainerImage {
		t.Errorf("expected source_type %q, got %q", account.PageSourceContainerImage, page.SourceType)
	}
	if page.Image != "nginx:latest" {
		t.Errorf("expected image %q, got %q", "nginx:latest", page.Image)
	}
	if page.ImageDirectory != "/usr/share/nginx/html" {
		t.Errorf("expected image_directory %q, got %q", "/usr/share/nginx/html", page.ImageDirectory)
	}

	// Wait briefly for the background goroutine to process (extraction will
	// fail in test environments without podman, setting status to error).
	time.Sleep(100 * time.Millisecond)
	pages, err := c.ListPages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages.Entries))
	}
	// Status should have transitioned from pending (to either active or error
	// depending on whether podman is available).
	if pages.Entries[0].Status == "pending" {
		t.Error("expected status to transition from pending after async extraction")
	}
}

func TestHTTPUploadPageArchive(t *testing.T) {
	if _, err := exec.LookPath("pigz"); err != nil {
		t.Skip("pigz not installed")
	}
	env := initPagesTestEnv(t)

	_, err := env.Client.CreatePage(context.TODO(), "archive-site", "", "", "archive.example.com", account.PageSourceArchive, "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// Create the target directory since mock btrfs doesn't create real dirs.
	targetDir := filepath.Join(env.BtrfsBase, PagesVolumePrefix, "archive-site")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	archive := createTestTarGz(t)
	page, err := env.Client.UploadPageArchive(context.TODO(), "archive-site", archive, "site.tar.gz")
	if err != nil {
		t.Fatalf("UploadPageArchive: %v", err)
	}

	if page.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", page.Status)
	}

	// Verify extracted file exists.
	if _, err := os.Stat(filepath.Join(targetDir, "index.html")); err != nil {
		t.Errorf("expected index.html to exist after upload: %v", err)
	}
}

func TestHTTPUploadPageArchiveNonArchivePage(t *testing.T) {
	env := initPagesTestEnv(t)

	_, err := env.Client.CreatePage(context.TODO(), "git-site", "https://github.com/user/site.git", "main", "git.example.com", account.PageSourceGit, "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	archive := createTestTarGz(t)
	_, err = env.Client.UploadPageArchive(context.TODO(), "git-site", archive, "site.tar.gz")
	if err == nil {
		t.Fatal("expected error when uploading to non-archive page")
	}
}

func TestHTTPUploadPageArchiveNotFound(t *testing.T) {
	env := initPagesTestEnv(t)

	archive := createTestTarGz(t)
	_, err := env.Client.UploadPageArchive(context.TODO(), "nonexistent", archive, "site.tar.gz")
	if err == nil {
		t.Fatal("expected error when uploading to nonexistent page")
	}
}

func TestHTTPUploadPageArchiveContainerImageSourceType(t *testing.T) {
	c, _ := initPagesTestClient(t)

	// Create a container_image page, then try to upload an archive to it.
	_, err := c.CreatePage(context.TODO(), "image-site", "", "", "image.example.com", account.PageSourceContainerImage, "nginx:latest", "/usr/share/nginx/html")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	_, err = c.UploadPageArchive(context.TODO(), "image-site", strings.NewReader("dummy"), "dummy.tar.gz")
	if err == nil {
		t.Fatal("expected error when uploading archive to container_image-type page")
	}
}

func TestHTTPRebuildContainerImagePageSetsErrorStatus(t *testing.T) {
	c, _ := initPagesTestClient(t)

	_, err := c.CreatePage(context.TODO(), "image-site", "", "", "image.example.com", account.PageSourceContainerImage, "nginx:latest", "/usr/share/nginx/html")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// Rebuild will fail because reconcileExtractFromImage uses podman which is
	// unavailable in mock tests. The handler should set status to error.
	_, err = c.RebuildPage(context.TODO(), "image-site")
	if err == nil {
		t.Fatal("expected error when rebuilding container_image page in mock env")
	}

	// Verify the page status was set to error.
	pages, err := c.ListPages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages.Entries))
	}
	if pages.Entries[0].Status != "error" {
		t.Errorf("expected status %q after rebuild failure, got %q", "error", pages.Entries[0].Status)
	}
}

func TestHTTPUpdatePageImageFields(t *testing.T) {
	c, _ := initPagesTestClient(t)

	_, err := c.CreatePage(context.TODO(), "image-site", "", "", "image.example.com", account.PageSourceContainerImage, "nginx:latest", "/usr/share/nginx/html")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	newImage := "alpine:latest"
	newDir := "/srv/html"
	updated, err := c.UpdatePage(context.TODO(), "image-site", account.PageSiteUpdate{
		Image:          &newImage,
		ImageDirectory: &newDir,
	})
	if err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}
	if updated.Image != newImage {
		t.Errorf("expected image %q, got %q", newImage, updated.Image)
	}
	if updated.ImageDirectory != newDir {
		t.Errorf("expected image_directory %q, got %q", newDir, updated.ImageDirectory)
	}
}

func TestHTTPListPagesSorting(t *testing.T) {
	c, _ := initPagesTestClient(t)

	_, err := c.CreatePage(context.TODO(), "zeta-site", "", "", "zeta.example.com", account.PageSourceArchive, "", "")
	if err != nil {
		t.Fatalf("CreatePage zeta: %v", err)
	}
	_, err = c.CreatePage(context.TODO(), "alpha-site", "", "", "alpha.example.com", account.PageSourceArchive, "", "")
	if err != nil {
		t.Fatalf("CreatePage alpha: %v", err)
	}

	// Sort by name ascending (default).
	pages, err := c.ListPages(context.TODO(), ListParams{SortBy: "name", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages.Entries))
	}
	if pages.Entries[0].Name != "alpha-site" {
		t.Errorf("expected first page %q, got %q", "alpha-site", pages.Entries[0].Name)
	}

	// Sort by name descending.
	pages, err = c.ListPages(context.TODO(), ListParams{SortBy: "name", SortOrder: "desc"})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if pages.Entries[0].Name != "zeta-site" {
		t.Errorf("expected first page %q with desc sort, got %q", "zeta-site", pages.Entries[0].Name)
	}
}

func TestHTTPListPagesPagination(t *testing.T) {
	c, _ := initPagesTestClient(t)

	for i := range 3 {
		name := fmt.Sprintf("page-%d", i)
		_, err := c.CreatePage(context.TODO(), name, "", "", name+".example.com", account.PageSourceArchive, "", "")
		if err != nil {
			t.Fatalf("CreatePage %s: %v", name, err)
		}
	}

	// Request first page with limit 2.
	pages, err := c.ListPages(context.TODO(), ListParams{Limit: 2})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(pages.Entries))
	}
	if !pages.HasMore {
		t.Error("expected HasMore to be true")
	}
	if pages.TotalCount != 3 {
		t.Errorf("expected TotalCount 3, got %d", pages.TotalCount)
	}

	// Request second page.
	pages, err = c.ListPages(context.TODO(), ListParams{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages.Entries) != 1 {
		t.Fatalf("expected 1 entry on second page, got %d", len(pages.Entries))
	}
	if pages.HasMore {
		t.Error("expected HasMore to be false on last page")
	}
}

func TestHTTPPagesAuditUploadPageArchive(t *testing.T) {
	c, auditMgr := initPagesTestClientWithAudit(t)

	if _, err := c.CreatePage(context.TODO(), "archive-site", "", "", "archive.example.com", account.PageSourceArchive, "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// Attempt upload (will fail due to invalid archive content, but audit should still be logged).
	_, _ = c.UploadPageArchive(context.TODO(), "archive-site", strings.NewReader("not-an-archive"), "test.tar.gz")

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	found := false
	for _, e := range page.Entries {
		if e.Action == "upload page archive" && e.Path == "/pages/upload" {
			found = true
			if e.Account != "testadmin" {
				t.Fatalf("expected account %q, got %q", "testadmin", e.Account)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected to find 'upload page archive' audit entry")
	}
}

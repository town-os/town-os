// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// --- Pages integration tests ---

type pagesIntegrationEnv struct {
	Client    *systemcontroller.SystemdClient
	AuditMgr  account.AuditManager
	BtrfsBase string
}

func initSystemControllerPagesTest(t *testing.T) *systemcontroller.SystemdClient {
	t.Helper()
	env := initSystemControllerPagesEnv(t)
	return env.Client
}

func initSystemControllerPagesTestWithAudit(t *testing.T) (*systemcontroller.SystemdClient, account.AuditManager) {
	t.Helper()
	env := initSystemControllerPagesEnv(t)
	return env.Client, env.AuditMgr
}

func initSystemControllerPagesEnv(t *testing.T) pagesIntegrationEnv {
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

	auditMgr, err := account.InitAuditManager(t.Context(), db)
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

	return pagesIntegrationEnv{
		Client:    c,
		AuditMgr:  auditMgr,
		BtrfsBase: dir,
	}
}

func TestPagesCreateAndList(t *testing.T) {
	t.Parallel()
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
	page, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "", "")
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
	if page.SourceType != account.PageSourceGit {
		t.Errorf("expected source_type %q, got %q", account.PageSourceGit, page.SourceType)
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
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	page, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "", account.PageSourceGit, "", "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if page.Domain != "my-site" {
		t.Errorf("expected default domain %q, got %q", "my-site", page.Domain)
	}
}

func TestPagesCreateDuplicate(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	_, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/other.git", "main", "other.example.com", account.PageSourceGit, "", "", "")
	if err == nil {
		t.Fatal("expected error for duplicate page name")
	}
}

func TestPagesCreateArchiveSourceType(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	page, err := c.CreatePage(context.TODO(), "archive-site", "", "", "archive.example.com", account.PageSourceArchive, "", "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if page.SourceType != account.PageSourceArchive {
		t.Errorf("expected source_type %q, got %q", account.PageSourceArchive, page.SourceType)
	}
}

func TestPagesCreateContainerImageSourceType(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	page, err := c.CreatePage(context.TODO(), "image-site", "", "", "image.example.com", account.PageSourceContainerImage, "nginx:latest", "/usr/share/nginx/html", "")
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

func TestPagesCreateDefaultSourceType(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	page, err := c.CreatePage(context.TODO(), "default-site", "", "", "default.example.com", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if page.SourceType != account.PageSourceArchive {
		t.Errorf("expected default source_type %q, got %q", account.PageSourceArchive, page.SourceType)
	}
}

func TestPagesUpdate(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "", ""); err != nil {
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

func TestPagesUpdateSourceType(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	newType := account.PageSourceArchive
	updated, err := c.UpdatePage(context.TODO(), "my-site", account.PageSiteUpdate{SourceType: &newType})
	if err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}
	if updated.SourceType != account.PageSourceArchive {
		t.Errorf("expected source_type %q, got %q", account.PageSourceArchive, updated.SourceType)
	}
}

func TestPagesUpdateNotFound(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	newDomain := "new.example.com"
	_, err := c.UpdatePage(context.TODO(), "nonexistent", account.PageSiteUpdate{Domain: &newDomain})
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestPagesRemove(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "", ""); err != nil {
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
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	err := c.RemovePage(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestPagesListSearch(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "alpha-site", "https://github.com/user/alpha.git", "main", "alpha.example.com", account.PageSourceGit, "", "", ""); err != nil {
		t.Fatalf("CreatePage alpha-site: %v", err)
	}
	if _, err := c.CreatePage(context.TODO(), "beta-site", "https://github.com/user/beta.git", "develop", "beta.example.com", account.PageSourceGit, "", "", ""); err != nil {
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
	t.Parallel()
	c := initSystemControllerPagesTest(t)
	c.Token = "" // Clear auth token.

	_, err := c.ListPages(context.TODO(), systemcontroller.ListParams{})
	if err == nil {
		t.Fatal("expected auth error for ListPages without token")
	}
}

func TestPagesRebuild(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "my-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "", ""); err != nil {
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
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	_, err := c.RebuildPage(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestPagesRebuildArchiveReturnsError(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "archive-site", "", "", "archive.example.com", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	_, err := c.RebuildPage(context.TODO(), "archive-site")
	if err == nil {
		t.Fatal("expected error when rebuilding archive page")
	}
}

func TestPagesListIncludesSourceType(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "archive-page", "", "", "archive.example.com", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if _, err := c.CreatePage(context.TODO(), "git-page", "https://github.com/user/site.git", "main", "git.example.com", account.PageSourceGit, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	pages, err := c.ListPages(context.TODO(), systemcontroller.ListParams{})
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

func TestPagesAuditRebuildLogged(t *testing.T) {
	t.Parallel()
	c, auditMgr := initSystemControllerPagesTestWithAudit(t)

	if _, err := c.CreatePage(context.TODO(), "audit-site", "https://github.com/user/site.git", "main", "audit.example.com", account.PageSourceGit, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if _, err := c.RebuildPage(context.TODO(), "audit-site"); err != nil {
		t.Fatalf("RebuildPage: %v", err)
	}

	page, err := auditMgr.List(t.Context(), account.AuditListOptions{})
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
	t.Parallel()
	c, auditMgr := initSystemControllerPagesTestWithAudit(t)

	if _, err := c.CreatePage(context.TODO(), "audit-site", "https://github.com/user/site.git", "main", "audit.example.com", account.PageSourceGit, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	page, err := auditMgr.List(t.Context(), account.AuditListOptions{})
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
	t.Parallel()
	c, auditMgr := initSystemControllerPagesTestWithAudit(t)

	if _, err := c.CreatePage(context.TODO(), "audit-site", "https://github.com/user/site.git", "main", "audit.example.com", account.PageSourceGit, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	newDomain := "updated.example.com"
	if _, err := c.UpdatePage(context.TODO(), "audit-site", account.PageSiteUpdate{Domain: &newDomain}); err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}

	page, err := auditMgr.List(t.Context(), account.AuditListOptions{})
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
	t.Parallel()
	c, auditMgr := initSystemControllerPagesTestWithAudit(t)

	if _, err := c.CreatePage(context.TODO(), "audit-site", "https://github.com/user/site.git", "main", "audit.example.com", account.PageSourceGit, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if err := c.RemovePage(context.TODO(), "audit-site"); err != nil {
		t.Fatalf("RemovePage: %v", err)
	}

	page, err := auditMgr.List(t.Context(), account.AuditListOptions{})
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
	t.Parallel()
	c, auditMgr := initSystemControllerPagesTestWithAudit(t)

	// Call list pages multiple times - should NOT be audit logged.
	if _, err := c.ListPages(context.TODO(), systemcontroller.ListParams{}); err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if _, err := c.ListPages(context.TODO(), systemcontroller.ListParams{Search: "test"}); err != nil {
		t.Fatalf("ListPages: %v", err)
	}

	page, err := auditMgr.List(t.Context(), account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, e := range page.Entries {
		if e.Path == "/pages" {
			t.Fatal("expected /pages (list) to be excluded from audit log")
		}
	}
}

func TestPagesCreateCreatesSymlink(t *testing.T) {
	t.Parallel()
	env := initSystemControllerPagesEnv(t)

	// Ensure the webroot directory exists.
	webrootDir := filepath.Join(env.BtrfsBase, "pages-webroot")
	if err := os.MkdirAll(webrootDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	_, err := env.Client.CreatePage(context.TODO(), "my-site", "", "", "site.example.com", account.PageSourceArchive, "", "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// The webroot symlink is named by the served FQDN (the page's domain).
	linkPath := filepath.Join(webrootDir, "site.example.com")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}

	expected := "/data/pages/site.example.com"
	if target != expected {
		t.Fatalf("expected symlink target %q, got %q", expected, target)
	}
}

func TestPagesRemoveRemovesSymlink(t *testing.T) {
	t.Parallel()
	env := initSystemControllerPagesEnv(t)

	// Ensure the webroot directory exists.
	webrootDir := filepath.Join(env.BtrfsBase, "pages-webroot")
	if err := os.MkdirAll(webrootDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	_, err := env.Client.CreatePage(context.TODO(), "my-site", "", "", "site.example.com", account.PageSourceArchive, "", "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// Verify symlink exists (named by the served FQDN / domain).
	linkPath := filepath.Join(webrootDir, "site.example.com")
	if _, err := os.Readlink(linkPath); err != nil {
		t.Fatalf("expected symlink to exist: %v", err)
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

func TestPagesUploadArchiveWrongSourceType(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	// Create a git page, then try to upload an archive to it.
	if _, err := c.CreatePage(context.TODO(), "git-site", "https://github.com/user/site.git", "main", "git.example.com", account.PageSourceGit, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	_, err := c.UploadPageArchive(context.TODO(), "git-site", strings.NewReader("dummy"), "dummy.tar.gz")
	if err == nil {
		t.Fatal("expected error when uploading archive to git-type page")
	}
}

func TestPagesUploadArchivePageNotFound(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	_, err := c.UploadPageArchive(context.TODO(), "nonexistent", strings.NewReader("dummy"), "dummy.tar.gz")
	if err == nil {
		t.Fatal("expected error when uploading to nonexistent page")
	}
}

func TestPagesUploadArchiveContainerImageSourceType(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	// Create a container_image page, then try to upload an archive to it.
	if _, err := c.CreatePage(context.TODO(), "image-site", "", "", "image.example.com", account.PageSourceContainerImage, "nginx:latest", "/usr/share/nginx/html", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	_, err := c.UploadPageArchive(context.TODO(), "image-site", strings.NewReader("dummy"), "dummy.tar.gz")
	if err == nil {
		t.Fatal("expected error when uploading archive to container_image-type page")
	}
}

func TestPagesRebuildContainerImageSetsErrorStatus(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "image-site", "", "", "image.example.com", account.PageSourceContainerImage, "nginx:latest", "/usr/share/nginx/html", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// Rebuild will fail because reconcileExtractFromImage uses podman which is
	// unavailable in test. The handler should set status to error.
	_, err := c.RebuildPage(context.TODO(), "image-site")
	if err == nil {
		t.Fatal("expected error when rebuilding container_image page in mock env")
	}

	// Verify the page status was set to error.
	pages, err := c.ListPages(context.TODO(), systemcontroller.ListParams{})
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

func TestPagesUpdateImageFields(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "image-site", "", "", "image.example.com", account.PageSourceContainerImage, "nginx:latest", "/usr/share/nginx/html", ""); err != nil {
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

func TestPagesListSorting(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	if _, err := c.CreatePage(context.TODO(), "zeta-site", "", "", "zeta.example.com", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage zeta: %v", err)
	}
	if _, err := c.CreatePage(context.TODO(), "alpha-site", "", "", "alpha.example.com", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage alpha: %v", err)
	}

	// Sort by name ascending.
	pages, err := c.ListPages(context.TODO(), systemcontroller.ListParams{SortBy: "name", SortOrder: "asc"})
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
	pages, err = c.ListPages(context.TODO(), systemcontroller.ListParams{SortBy: "name", SortOrder: "desc"})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if pages.Entries[0].Name != "zeta-site" {
		t.Errorf("expected first page %q with desc sort, got %q", "zeta-site", pages.Entries[0].Name)
	}
}

func TestPagesListPagination(t *testing.T) {
	t.Parallel()
	c := initSystemControllerPagesTest(t)

	for i := range 3 {
		name := fmt.Sprintf("page-%d", i)
		if _, err := c.CreatePage(context.TODO(), name, "", "", name+".example.com", account.PageSourceArchive, "", "", ""); err != nil {
			t.Fatalf("CreatePage %s: %v", name, err)
		}
	}

	// Request first page with limit 2.
	pages, err := c.ListPages(context.TODO(), systemcontroller.ListParams{Limit: 2})
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
	pages, err = c.ListPages(context.TODO(), systemcontroller.ListParams{Limit: 2, Offset: 2})
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

func TestPagesAuditUploadLogged(t *testing.T) {
	t.Parallel()
	c, auditMgr := initSystemControllerPagesTestWithAudit(t)

	if _, err := c.CreatePage(context.TODO(), "archive-site", "", "", "archive.example.com", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// Attempt upload (will fail due to invalid content, but audit should still be logged).
	_, _ = c.UploadPageArchive(context.TODO(), "archive-site", strings.NewReader("not-an-archive"), "test.tar.gz")

	page, err := auditMgr.List(t.Context(), account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	found := false
	for _, e := range page.Entries {
		if e.Action == "upload page archive" && e.Path == "/pages/upload" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected to find 'upload page archive' audit entry")
	}
}

// initSystemControllerPagesTLSEnv builds a pages test server wired with a real
// local CA and a recording rolodex client so the HTTPS-by-domain path (leaf
// issuance + per-domain Caddyfile + DNS registration) can be exercised
// end-to-end through the HTTP API. Returns the client, the rolodex mock, and
// the btrfs base dir for on-disk assertions.
func initSystemControllerPagesTLSEnv(t *testing.T) (*systemcontroller.SystemdClient, *rolodex.MockClient, *ingress.MockClient, string) {
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
	sessMgr, err := account.InitSessionManager(db, mgr, []byte("test-signing-key-for-sessions-32"))
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}
	auditMgr, err := account.InitAuditManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitAuditManager: %v", err)
	}
	pagesMgr, err := account.InitPagesManager(db)
	if err != nil {
		t.Fatalf("InitPagesManager: %v", err)
	}

	ca, err := townostls.EnsureCA(filepath.Join(dir, "tls"))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	rol := &rolodex.MockClient{}
	ing := &ingress.MockClient{}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       storage.InitBtrFSMock(),
		AccountMgr:    mgr,
		SessionMgr:    sessMgr,
		AuditMgr:      auditMgr,
		PagesMgr:      pagesMgr,
		Git:           git.InitMockClient(),
		BtrfsBasePath: dir,
		TLSCA:         ca,
		RolodexClient: rol,
		IngressClient: ing,
	})
	t.Cleanup(func() { ts.Server.Close() })
	ts.SetInternalIP("192.0.2.10")

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "adminpass")
	if err != nil {
		t.Fatalf("bootstrap Authenticate: %v", err)
	}
	c.Token = resp.Token

	return c, rol, ing, dir
}

// pagesMockAddedA reports whether the rolodex mock recorded an AddRecord for an
// A record with the given name and value.
func pagesMockAddedA(rol *rolodex.MockClient, name, value string) bool {
	for _, call := range rol.GetCalls() {
		if call.Method != "AddRecord" || len(call.Args) == 0 {
			continue
		}
		rec, ok := call.Args[0].(*upstream.DnsRecord)
		if !ok {
			continue
		}
		if rec.Name == name && rec.Value == value && rec.RecordType == upstream.RecordTypeA {
			return true
		}
	}
	return false
}

// pagesMockAddedAnyName reports whether the rolodex mock recorded an AddRecord
// for any record with the given name (regardless of type/value).
func pagesMockAddedAnyName(rol *rolodex.MockClient, name string) bool {
	for _, call := range rol.GetCalls() {
		if call.Method != "AddRecord" || len(call.Args) == 0 {
			continue
		}
		rec, ok := call.Args[0].(*upstream.DnsRecord)
		if !ok {
			continue
		}
		if rec.Name == name {
			return true
		}
	}
	return false
}

// pagesMockRemoved reports whether the rolodex mock recorded a RemoveRecord for
// the given name.
func pagesMockRemoved(rol *rolodex.MockClient, name string) bool {
	for _, call := range rol.GetCalls() {
		if call.Method != "RemoveRecord" || len(call.Args) == 0 {
			continue
		}
		got, ok := call.Args[0].(string)
		if !ok {
			continue
		}
		if got == name {
			return true
		}
	}
	return false
}

// TestPagesServedOverHTTPSByDomain exercises the full HTTPS-by-domain path: an
// internal page gets a local-CA leaf, a per-domain HTTPS vhost, and a rolodex A
// record; a public-FQDN page gets an ACME vhost and no rolodex record; removal
// retires both the vhost and the DNS record.
func TestPagesServedOverHTTPSByDomain(t *testing.T) {
	t.Parallel()
	c, rol, ing, dir := initSystemControllerPagesTLSEnv(t)
	ctx := context.TODO()

	// Pages are no longer served by a file-mounted ingress Caddyfile: the
	// shared :443 ingress is programmed over gRPC with one route per page FQDN,
	// reverse-proxying to the standalone pages static-file service.
	pagesBackend := systemd.SystemServiceContainerName(systemcontroller.PagesServiceKey) + ":80"

	// Internal page (bare label) → blog.home routed to the pages service,
	// terminating TLS with the page's local-CA leaf.
	if _, err := c.CreatePage(ctx, "blog", "", "", "blog", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage(blog): %v", err)
	}
	route := ing.RouteFor("blog.home")
	if route == nil {
		t.Fatalf("ingress not programmed with a route for blog.home; routes: %+v", ing.Routes)
	}
	if route.GetBackend() != pagesBackend {
		t.Fatalf("blog route backend = %q, want %q", route.GetBackend(), pagesBackend)
	}
	if route.GetAcme() {
		t.Fatalf("internal page must not use ACME: %+v", route)
	}
	if route.GetCertDir() == "" {
		t.Fatalf("internal page route must pin a local leaf: %+v", route)
	}
	if _, err := os.Stat(filepath.Join(dir, "tls", "leaves", "pages", "blog", "current", "cert.pem")); err != nil {
		t.Fatalf("expected leaf cert for blog: %v", err)
	}
	if !pagesMockAddedA(rol, "blog.home.", "192.0.2.10") {
		t.Fatalf("expected A record blog.home. -> 192.0.2.10, calls: %+v", rol.GetCalls())
	}

	// Public-FQDN page → ACME ingress route, no rolodex record (resolved by user DNS).
	if _, err := c.CreatePage(ctx, "shop", "", "", "shop.example.com", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage(shop): %v", err)
	}
	shop := ing.RouteFor("shop.example.com")
	if shop == nil || !shop.GetAcme() {
		t.Fatalf("expected an ACME ingress route for the public page; routes: %+v", ing.Routes)
	}
	if pagesMockAddedAnyName(rol, "shop.example.com.") {
		t.Fatal("public FQDN page must not get a rolodex A record")
	}

	// Removal withdraws the ingress route and retires the DNS record.
	if err := c.RemovePage(ctx, "blog"); err != nil {
		t.Fatalf("RemovePage(blog): %v", err)
	}
	if r := ing.RouteFor("blog.home"); r != nil {
		t.Fatalf("blog route should be gone after removal: %+v", r)
	}
	if !pagesMockRemoved(rol, "blog.home.") {
		t.Fatalf("expected RemoveRecord for blog.home., calls: %+v", rol.GetCalls())
	}
}

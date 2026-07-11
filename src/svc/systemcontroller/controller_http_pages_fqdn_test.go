// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
)

func storageHasFS(ctrl *storage.MockBtrFSController, name string) bool {
	for _, f := range ctrl.GetFilesystems() {
		if f.Name == name {
			return true
		}
	}
	return false
}

// TestHTTPCreatePageInternalUsesFQDNDir verifies an internal page's content
// subvolume and webroot symlink are keyed by the served FQDN (name + TLD),
// not the short name. The test env has no settings manager, so the TLD
// defaults to "home".
func TestHTTPCreatePageInternalUsesFQDNDir(t *testing.T) {
	env := initPagesTestEnv(t)
	if err := EnsurePagesWebroot(env.BtrfsBase); err != nil {
		t.Fatalf("EnsurePagesWebroot: %v", err)
	}

	// Default domain == name "blog"; internal => served FQDN "blog.home".
	if _, err := env.Client.CreatePage(context.TODO(), "blog", "", "", "", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if !storageHasFS(env.Storage, "pages/blog.home") {
		t.Fatalf("expected pages/blog.home subvolume, got %v", env.Storage.GetFilesystems())
	}

	linkPath := filepath.Join(env.BtrfsBase, PagesWebrootDir, "blog.home")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if want := "/data/pages/blog.home"; target != want {
		t.Fatalf("symlink target = %q, want %q", target, want)
	}
}

// TestHTTPCreatePageLabelCollisionDistinctDirs proves two pages whose first
// DNS label collides (blog.a.com vs blog.b.com) get distinct FQDN-named
// directories — the bug the FQDN keying fixes (the old leftmost-label scheme
// mapped both to /srv/blog).
func TestHTTPCreatePageLabelCollisionDistinctDirs(t *testing.T) {
	env := initPagesTestEnv(t)
	if err := EnsurePagesWebroot(env.BtrfsBase); err != nil {
		t.Fatalf("EnsurePagesWebroot: %v", err)
	}

	if _, err := env.Client.CreatePage(context.TODO(), "blog-a", "", "", "blog.a.com", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage a: %v", err)
	}
	if _, err := env.Client.CreatePage(context.TODO(), "blog-b", "", "", "blog.b.com", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage b: %v", err)
	}

	for _, want := range []string{"pages/blog.a.com", "pages/blog.b.com"} {
		if !storageHasFS(env.Storage, want) {
			t.Fatalf("expected %s subvolume, got %v", want, env.Storage.GetFilesystems())
		}
	}
}

// TestHTTPCreatePageGitCloneFailureResets verifies a failed git clone resets
// the page's content subvolume (so no partial/broken tree lingers) and marks
// the page status "error".
func TestHTTPCreatePageGitCloneFailureResets(t *testing.T) {
	env := initPagesTestEnv(t)
	if err := EnsurePagesWebroot(env.BtrfsBase); err != nil {
		t.Fatalf("EnsurePagesWebroot: %v", err)
	}
	env.GitClient.CloneErr = errors.New("mock clone failure")

	if _, err := env.Client.CreatePage(context.TODO(), "failclone", "https://example.invalid/repo.git", "main", "", account.PageSourceGit, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// The clone runs in a background goroutine; poll until the status settles.
	var status string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		res, err := env.Client.ListPages(context.TODO(), ListParams{})
		if err != nil {
			t.Fatalf("ListPages: %v", err)
		}
		for _, p := range res.Entries {
			if p.Name == "failclone" {
				status = p.Status
			}
		}
		if status == "error" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != "error" {
		t.Fatalf("expected status error after failed clone, got %q", status)
	}

	// Reset removes then recreates the subvolume: a SubvolDelete must have run
	// and the (empty) subvolume must still exist.
	deleted := false
	for _, call := range env.Storage.GetLog() {
		if call.Operation == "SubvolDelete" {
			deleted = true
		}
	}
	if !deleted {
		t.Error("expected the failed clone to reset (delete) the page content")
	}
	if !storageHasFS(env.Storage, "pages/failclone.home") {
		t.Error("expected pages/failclone.home to exist (recreated empty) after reset")
	}
}

// initPagesTLDTestEnv is like initPagesTestEnv but wires a mock rolodex client
// and a settings manager so /dns/tld can run and exercise the page-directory
// migration.
func initPagesTLDTestEnv(t *testing.T) (*SystemdClient, *storage.MockBtrFSController, string) {
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
	sessMgr, err := account.InitSessionManager(db, mgr, []byte("test-signing-key-for-sessions-32"))
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	btrfsBase := t.TempDir()
	ts := InitTestServer(ServerConfig{
		Storage:       mock,
		AccountMgr:    mgr,
		SessionMgr:    sessMgr,
		PagesMgr:      account.InitMockPagesManager(),
		Git:           git.InitMockClient(),
		RolodexClient: &rolodex.MockClient{},
		SettingsMgr:   &mockSettingsManager{values: map[string]string{"dns_tld": "home"}},
		BtrfsBasePath: btrfsBase,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	if _, err := c.CreateAccount(context.TODO(), "tldadmin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "tldadmin", "adminpass")
	if err != nil {
		t.Fatalf("bootstrap Authenticate: %v", err)
	}
	c.Token = resp.Token
	return c, controller, btrfsBase
}

// TestHTTPSetDNSTLDRenamesInternalPageDirs verifies that changing the DNS TLD
// renames an internal page's content subvolume and webroot symlink to the new
// FQDN so served content follows the hostname.
func TestHTTPSetDNSTLDRenamesInternalPageDirs(t *testing.T) {
	c, ctrl, btrfsBase := initPagesTLDTestEnv(t)
	if err := EnsurePagesWebroot(btrfsBase); err != nil {
		t.Fatalf("EnsurePagesWebroot: %v", err)
	}

	// Internal page: default domain "blog" => dir "blog.home" at the start.
	if _, err := c.CreatePage(context.TODO(), "blog", "", "", "", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if !storageHasFS(ctrl, "pages/blog.home") {
		t.Fatalf("expected pages/blog.home before TLD change, got %v", ctrl.GetFilesystems())
	}

	if err := c.SetDNSTLD(context.TODO(), "lan"); err != nil {
		t.Fatalf("SetDNSTLD: %v", err)
	}

	if storageHasFS(ctrl, "pages/blog.home") {
		t.Error("expected pages/blog.home to be renamed away after TLD change")
	}
	if !storageHasFS(ctrl, "pages/blog.lan") {
		t.Fatalf("expected pages/blog.lan after TLD change, got %v", ctrl.GetFilesystems())
	}

	// The webroot symlink follows too.
	if _, err := os.Readlink(filepath.Join(btrfsBase, PagesWebrootDir, "blog.lan")); err != nil {
		t.Errorf("expected blog.lan symlink after TLD change: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(btrfsBase, PagesWebrootDir, "blog.home")); !os.IsNotExist(err) {
		t.Error("expected blog.home symlink to be removed after TLD change")
	}
}

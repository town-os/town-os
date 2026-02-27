package account

import (
	"errors"
	"path/filepath"
	"testing"
)

func initPagesTestDB(t *testing.T) *SQLitePagesManager {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := InitPagesManager(db)
	if err != nil {
		t.Fatalf("InitPagesManager: %v", err)
	}
	return mgr
}

func TestPagesCreateAndGet(t *testing.T) {
	mgr := initPagesTestDB(t)

	page, err := mgr.Create("my-site", "https://github.com/user/site.git", "main", "site.example.com")
	if err != nil {
		t.Fatalf("Create: %v", err)
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
	if page.Status != "pending" {
		t.Errorf("expected status %q, got %q", "pending", page.Status)
	}

	got, err := mgr.Get("my-site")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Name != page.Name || got.RepoURL != page.RepoURL || got.Domain != page.Domain {
		t.Errorf("Get returned different page than Create")
	}
}

func TestPagesCreateDefaultBranch(t *testing.T) {
	mgr := initPagesTestDB(t)

	page, err := mgr.Create("my-site", "https://github.com/user/site.git", "", "site.example.com")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if page.Branch != "main" {
		t.Errorf("expected default branch %q, got %q", "main", page.Branch)
	}
}

func TestPagesCreateDuplicate(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("my-site", "https://github.com/user/site.git", "main", "site.example.com")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = mgr.Create("my-site", "https://github.com/user/other.git", "main", "other.example.com")
	if !errors.Is(err, ErrDuplicatePageName) {
		t.Fatalf("expected ErrDuplicatePageName, got %v", err)
	}
}

func TestPagesCreateValidation(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("", "https://github.com/user/site.git", "main", "site.example.com")
	if !errors.Is(err, ErrPageNameRequired) {
		t.Fatalf("expected ErrPageNameRequired, got %v", err)
	}

	_, err = mgr.Create("my-site", "", "main", "site.example.com")
	if !errors.Is(err, ErrPageRepoRequired) {
		t.Fatalf("expected ErrPageRepoRequired, got %v", err)
	}

	_, err = mgr.Create("my-site", "https://github.com/user/site.git", "main", "")
	if !errors.Is(err, ErrPageDomainRequired) {
		t.Fatalf("expected ErrPageDomainRequired, got %v", err)
	}
}

func TestPagesGetNotFound(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Get("nonexistent")
	if !errors.Is(err, ErrPageNotFound) {
		t.Fatalf("expected ErrPageNotFound, got %v", err)
	}
}

func TestPagesUpdate(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("my-site", "https://github.com/user/site.git", "main", "site.example.com")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	newURL := "https://github.com/user/new-site.git"
	newBranch := "develop"
	newDomain := "new.example.com"
	newStatus := "active"

	updated, err := mgr.Update("my-site", PageSiteUpdate{
		RepoURL: &newURL,
		Branch:  &newBranch,
		Domain:  &newDomain,
		Status:  &newStatus,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if updated.RepoURL != newURL {
		t.Errorf("expected repo_url %q, got %q", newURL, updated.RepoURL)
	}
	if updated.Branch != newBranch {
		t.Errorf("expected branch %q, got %q", newBranch, updated.Branch)
	}
	if updated.Domain != newDomain {
		t.Errorf("expected domain %q, got %q", newDomain, updated.Domain)
	}
	if updated.Status != newStatus {
		t.Errorf("expected status %q, got %q", newStatus, updated.Status)
	}
}

func TestPagesUpdateNotFound(t *testing.T) {
	mgr := initPagesTestDB(t)

	newURL := "https://github.com/user/new.git"
	_, err := mgr.Update("nonexistent", PageSiteUpdate{RepoURL: &newURL})
	if !errors.Is(err, ErrPageNotFound) {
		t.Fatalf("expected ErrPageNotFound, got %v", err)
	}
}

func TestPagesUpdateNoFields(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("my-site", "https://github.com/user/site.git", "main", "site.example.com")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := mgr.Update("my-site", PageSiteUpdate{})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.RepoURL != "https://github.com/user/site.git" {
		t.Errorf("expected unchanged repo_url")
	}
}

func TestPagesUpdateEmptyRepoURL(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("my-site", "https://github.com/user/site.git", "main", "site.example.com")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	empty := ""
	_, err = mgr.Update("my-site", PageSiteUpdate{RepoURL: &empty})
	if !errors.Is(err, ErrPageRepoRequired) {
		t.Fatalf("expected ErrPageRepoRequired, got %v", err)
	}
}

func TestPagesUpdateEmptyDomain(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("my-site", "https://github.com/user/site.git", "main", "site.example.com")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	empty := ""
	_, err = mgr.Update("my-site", PageSiteUpdate{Domain: &empty})
	if !errors.Is(err, ErrPageDomainRequired) {
		t.Fatalf("expected ErrPageDomainRequired, got %v", err)
	}
}

func TestPagesRemove(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("my-site", "https://github.com/user/site.git", "main", "site.example.com")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := mgr.Remove("my-site"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	_, err = mgr.Get("my-site")
	if !errors.Is(err, ErrPageNotFound) {
		t.Fatalf("expected ErrPageNotFound after remove, got %v", err)
	}
}

func TestPagesRemoveNotFound(t *testing.T) {
	mgr := initPagesTestDB(t)

	err := mgr.Remove("nonexistent")
	if !errors.Is(err, ErrPageNotFound) {
		t.Fatalf("expected ErrPageNotFound, got %v", err)
	}
}

func TestPagesList(t *testing.T) {
	mgr := initPagesTestDB(t)

	pages, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(pages) != 0 {
		t.Fatalf("expected 0 pages, got %d", len(pages))
	}

	_, err = mgr.Create("alpha", "https://github.com/user/alpha.git", "main", "alpha.example.com")
	if err != nil {
		t.Fatalf("Create alpha: %v", err)
	}
	_, err = mgr.Create("beta", "https://github.com/user/beta.git", "develop", "beta.example.com")
	if err != nil {
		t.Fatalf("Create beta: %v", err)
	}

	pages, err = mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages))
	}
	if pages[0].Name != "alpha" {
		t.Errorf("expected first page %q, got %q", "alpha", pages[0].Name)
	}
	if pages[1].Name != "beta" {
		t.Errorf("expected second page %q, got %q", "beta", pages[1].Name)
	}
}

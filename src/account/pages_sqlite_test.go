// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

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

	page, err := mgr.Create("my-site", "https://github.com/user/site.git", "main", "site.example.com", PageSourceGit, "", "", "")
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
	if page.SourceType != PageSourceGit {
		t.Errorf("expected source_type %q, got %q", PageSourceGit, page.SourceType)
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
	if got.SourceType != PageSourceGit {
		t.Errorf("expected source_type %q, got %q", PageSourceGit, got.SourceType)
	}
}

func TestPagesCreateDefaultBranch(t *testing.T) {
	mgr := initPagesTestDB(t)

	page, err := mgr.Create("my-site", "https://github.com/user/site.git", "", "site.example.com", PageSourceGit, "", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if page.Branch != "main" {
		t.Errorf("expected default branch %q, got %q", "main", page.Branch)
	}
}

func TestPagesCreateDuplicate(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("my-site", "https://github.com/user/site.git", "main", "site.example.com", PageSourceGit, "", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = mgr.Create("my-site", "https://github.com/user/other.git", "main", "other.example.com", PageSourceGit, "", "", "")
	if !errors.Is(err, ErrDuplicatePageName) {
		t.Fatalf("expected ErrDuplicatePageName, got %v", err)
	}
}

func TestPagesCreateValidation(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("", "https://github.com/user/site.git", "main", "site.example.com", PageSourceGit, "", "", "")
	if !errors.Is(err, ErrPageNameRequired) {
		t.Fatalf("expected ErrPageNameRequired, got %v", err)
	}

	_, err = mgr.Create("my-site", "", "main", "site.example.com", PageSourceGit, "", "", "")
	if !errors.Is(err, ErrPageRepoRequired) {
		t.Fatalf("expected ErrPageRepoRequired, got %v", err)
	}

	_, err = mgr.Create("my-site", "https://github.com/user/site.git", "main", "", PageSourceGit, "", "", "")
	if !errors.Is(err, ErrPageDomainRequired) {
		t.Fatalf("expected ErrPageDomainRequired, got %v", err)
	}
}

func TestPagesCreateArchiveSourceType(t *testing.T) {
	mgr := initPagesTestDB(t)

	page, err := mgr.Create("archive-site", "", "", "archive.example.com", PageSourceArchive, "", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if page.SourceType != PageSourceArchive {
		t.Errorf("expected source_type %q, got %q", PageSourceArchive, page.SourceType)
	}
	if page.RepoURL != "" {
		t.Errorf("expected empty repo_url for archive, got %q", page.RepoURL)
	}
}

func TestPagesCreateContainerImageSourceType(t *testing.T) {
	mgr := initPagesTestDB(t)

	page, err := mgr.Create("image-site", "", "", "image.example.com", PageSourceContainerImage, "nginx:latest", "/usr/share/nginx/html", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if page.SourceType != PageSourceContainerImage {
		t.Errorf("expected source_type %q, got %q", PageSourceContainerImage, page.SourceType)
	}
	if page.Image != "nginx:latest" {
		t.Errorf("expected image %q, got %q", "nginx:latest", page.Image)
	}
	if page.ImageDirectory != "/usr/share/nginx/html" {
		t.Errorf("expected image_directory %q, got %q", "/usr/share/nginx/html", page.ImageDirectory)
	}
}

func TestPagesCreateContainerImageRequiresImage(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("image-site", "", "", "image.example.com", PageSourceContainerImage, "", "/usr/share/nginx/html", "")
	if !errors.Is(err, ErrPageImageRequired) {
		t.Fatalf("expected ErrPageImageRequired, got %v", err)
	}
}

func TestPagesCreateContainerImageRequiresDirectory(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("image-site", "", "", "image.example.com", PageSourceContainerImage, "nginx:latest", "", "")
	if !errors.Is(err, ErrPageImageDirectoryRequired) {
		t.Fatalf("expected ErrPageImageDirectoryRequired, got %v", err)
	}
}

func TestPagesCreateInvalidSourceType(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("bad-site", "", "", "bad.example.com", "invalid", "", "", "")
	if !errors.Is(err, ErrPageInvalidSourceType) {
		t.Fatalf("expected ErrPageInvalidSourceType, got %v", err)
	}
}

func TestPagesCreateDefaultSourceType(t *testing.T) {
	mgr := initPagesTestDB(t)

	page, err := mgr.Create("default-site", "", "", "default.example.com", "", "", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if page.SourceType != PageSourceArchive {
		t.Errorf("expected default source_type %q, got %q", PageSourceArchive, page.SourceType)
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

	_, err := mgr.Create("my-site", "https://github.com/user/site.git", "main", "site.example.com", PageSourceGit, "", "", "")
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

func TestPagesUpdateSourceType(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("my-site", "https://github.com/user/site.git", "main", "site.example.com", PageSourceGit, "", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	newType := PageSourceArchive
	updated, err := mgr.Update("my-site", PageSiteUpdate{SourceType: &newType})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.SourceType != PageSourceArchive {
		t.Errorf("expected source_type %q, got %q", PageSourceArchive, updated.SourceType)
	}
}

func TestPagesUpdateInvalidSourceType(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("my-site", "", "", "site.example.com", PageSourceArchive, "", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	invalid := "invalid"
	_, err = mgr.Update("my-site", PageSiteUpdate{SourceType: &invalid})
	if !errors.Is(err, ErrPageInvalidSourceType) {
		t.Fatalf("expected ErrPageInvalidSourceType, got %v", err)
	}
}

func TestPagesUpdateImageFields(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("my-site", "", "", "site.example.com", PageSourceContainerImage, "nginx:latest", "/html", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	newImage := "alpine:latest"
	newDir := "/srv"
	updated, err := mgr.Update("my-site", PageSiteUpdate{Image: &newImage, ImageDirectory: &newDir})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Image != newImage {
		t.Errorf("expected image %q, got %q", newImage, updated.Image)
	}
	if updated.ImageDirectory != newDir {
		t.Errorf("expected image_directory %q, got %q", newDir, updated.ImageDirectory)
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

	_, err := mgr.Create("my-site", "https://github.com/user/site.git", "main", "site.example.com", PageSourceGit, "", "", "")
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

func TestPagesUpdateEmptyDomain(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("my-site", "https://github.com/user/site.git", "main", "site.example.com", PageSourceGit, "", "", "")
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

	_, err := mgr.Create("my-site", "https://github.com/user/site.git", "main", "site.example.com", PageSourceGit, "", "", "")
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

	_, err = mgr.Create("alpha", "https://github.com/user/alpha.git", "main", "alpha.example.com", PageSourceGit, "", "", "")
	if err != nil {
		t.Fatalf("Create alpha: %v", err)
	}
	_, err = mgr.Create("beta", "https://github.com/user/beta.git", "develop", "beta.example.com", PageSourceGit, "", "", "")
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

func TestPagesListIncludesSourceType(t *testing.T) {
	mgr := initPagesTestDB(t)

	_, err := mgr.Create("archive-page", "", "", "archive.example.com", PageSourceArchive, "", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	pages, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages))
	}
	if pages[0].SourceType != PageSourceArchive {
		t.Errorf("expected source_type %q, got %q", PageSourceArchive, pages[0].SourceType)
	}
}

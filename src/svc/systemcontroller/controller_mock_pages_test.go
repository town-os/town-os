// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"testing"

	"gitea.com/town-os/town-os/src/account"
)

func TestMockClientCreatePage(t *testing.T) {
	m := InitMockClient()

	page, err := m.CreatePage(context.TODO(), "test-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if page.Name != "test-site" {
		t.Errorf("expected name %q, got %q", "test-site", page.Name)
	}
	if page.RepoURL != "https://github.com/user/site.git" {
		t.Errorf("expected repo_url %q, got %q", "https://github.com/user/site.git", page.RepoURL)
	}
	if page.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", page.Status)
	}
	if page.SourceType != account.PageSourceGit {
		t.Errorf("expected source_type %q, got %q", account.PageSourceGit, page.SourceType)
	}
}

func TestMockClientCreatePageArchive(t *testing.T) {
	m := InitMockClient()

	page, err := m.CreatePage(context.TODO(), "archive-site", "", "", "archive.example.com", account.PageSourceArchive, "", "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if page.SourceType != account.PageSourceArchive {
		t.Errorf("expected source_type %q, got %q", account.PageSourceArchive, page.SourceType)
	}
}

func TestMockClientCreatePageContainerImage(t *testing.T) {
	m := InitMockClient()

	page, err := m.CreatePage(context.TODO(), "image-site", "", "", "image.example.com", account.PageSourceContainerImage, "nginx:latest", "/html", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if page.SourceType != account.PageSourceContainerImage {
		t.Errorf("expected source_type %q, got %q", account.PageSourceContainerImage, page.SourceType)
	}
	if page.Image != "nginx:latest" {
		t.Errorf("expected image %q, got %q", "nginx:latest", page.Image)
	}
	if page.ImageDirectory != "/html" {
		t.Errorf("expected image_directory %q, got %q", "/html", page.ImageDirectory)
	}
}

func TestMockClientCreatePageDefaultSourceType(t *testing.T) {
	m := InitMockClient()

	page, err := m.CreatePage(context.TODO(), "default-site", "", "", "default.example.com", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if page.SourceType != account.PageSourceArchive {
		t.Errorf("expected default source_type %q, got %q", account.PageSourceArchive, page.SourceType)
	}
}

func TestMockClientCreatePageDuplicate(t *testing.T) {
	m := InitMockClient()

	_, err := m.CreatePage(context.TODO(), "test-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	_, err = m.CreatePage(context.TODO(), "test-site", "https://github.com/user/other.git", "main", "other.example.com", account.PageSourceGit, "", "", "")
	if !errors.Is(err, account.ErrDuplicatePageName) {
		t.Fatalf("expected ErrDuplicatePageName, got %v", err)
	}
}

func TestMockClientUpdatePage(t *testing.T) {
	m := InitMockClient()

	_, err := m.CreatePage(context.TODO(), "test-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	newDomain := "new.example.com"
	updated, err := m.UpdatePage(context.TODO(), "test-site", account.PageSiteUpdate{Domain: &newDomain})
	if err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}

	if updated.Domain != newDomain {
		t.Errorf("expected domain %q, got %q", newDomain, updated.Domain)
	}
}

func TestMockClientUpdatePageSourceType(t *testing.T) {
	m := InitMockClient()

	_, err := m.CreatePage(context.TODO(), "test-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	newType := account.PageSourceArchive
	updated, err := m.UpdatePage(context.TODO(), "test-site", account.PageSiteUpdate{SourceType: &newType})
	if err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}
	if updated.SourceType != account.PageSourceArchive {
		t.Errorf("expected source_type %q, got %q", account.PageSourceArchive, updated.SourceType)
	}
}

func TestMockClientUpdatePageNotFound(t *testing.T) {
	m := InitMockClient()

	newDomain := "new.example.com"
	_, err := m.UpdatePage(context.TODO(), "nonexistent", account.PageSiteUpdate{Domain: &newDomain})
	if !errors.Is(err, account.ErrPageNotFound) {
		t.Fatalf("expected ErrPageNotFound, got %v", err)
	}
}

func TestMockClientRemovePage(t *testing.T) {
	m := InitMockClient()

	_, err := m.CreatePage(context.TODO(), "test-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if err := m.RemovePage(context.TODO(), "test-site"); err != nil {
		t.Fatalf("RemovePage: %v", err)
	}

	if len(m.Pages) != 0 {
		t.Errorf("expected 0 pages after remove, got %d", len(m.Pages))
	}
}

func TestMockClientRemovePageNotFound(t *testing.T) {
	m := InitMockClient()

	err := m.RemovePage(context.TODO(), "nonexistent")
	if !errors.Is(err, account.ErrPageNotFound) {
		t.Fatalf("expected ErrPageNotFound, got %v", err)
	}
}

func TestMockClientListPages(t *testing.T) {
	m := InitMockClient()

	result, err := m.ListPages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 pages, got %d", len(result.Entries))
	}

	_, err = m.CreatePage(context.TODO(), "alpha", "https://github.com/user/alpha.git", "main", "alpha.example.com", account.PageSourceGit, "", "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	result, err = m.ListPages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 page, got %d", len(result.Entries))
	}
}

func TestMockClientRebuildPage(t *testing.T) {
	m := InitMockClient()

	_, err := m.CreatePage(context.TODO(), "test-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	page, err := m.RebuildPage(context.TODO(), "test-site")
	if err != nil {
		t.Fatalf("RebuildPage: %v", err)
	}

	if page.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", page.Status)
	}
}

func TestMockClientRebuildPageNotFound(t *testing.T) {
	m := InitMockClient()

	_, err := m.RebuildPage(context.TODO(), "nonexistent")
	if !errors.Is(err, account.ErrPageNotFound) {
		t.Fatalf("expected ErrPageNotFound, got %v", err)
	}
}

func TestMockClientUploadPageArchive(t *testing.T) {
	m := InitMockClient()

	_, err := m.CreatePage(context.TODO(), "archive-site", "", "", "archive.example.com", account.PageSourceArchive, "", "", "")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	page, err := m.UploadPageArchive(context.TODO(), "archive-site", nil, "site.tar.gz")
	if err != nil {
		t.Fatalf("UploadPageArchive: %v", err)
	}
	if page.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", page.Status)
	}
}

func TestMockClientUploadPageArchiveNotFound(t *testing.T) {
	m := InitMockClient()

	_, err := m.UploadPageArchive(context.TODO(), "nonexistent", nil, "site.tar.gz")
	if !errors.Is(err, account.ErrPageNotFound) {
		t.Fatalf("expected ErrPageNotFound, got %v", err)
	}
}

func TestMockClientCreatePageError(t *testing.T) {
	m := InitMockClient()
	m.CreatePageErr = account.ErrPageRepoRequired

	_, err := m.CreatePage(context.TODO(), "test-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "", "")
	if !errors.Is(err, account.ErrPageRepoRequired) {
		t.Fatalf("expected ErrPageRepoRequired, got %v", err)
	}
}

func TestMockClientPageCallsTracked(t *testing.T) {
	m := InitMockClient()

	if _, err := m.CreatePage(context.TODO(), "test-site", "https://github.com/user/site.git", "main", "site.example.com", account.PageSourceGit, "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ListPages(context.TODO(), ListParams{}); err != nil {
		t.Fatal(err)
	}

	calls := m.GetCalls()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 calls, got %d", len(calls))
	}
	if calls[0].Method != "CreatePage" {
		t.Errorf("expected first call %q, got %q", "CreatePage", calls[0].Method)
	}
	if calls[1].Method != "ListPages" {
		t.Errorf("expected second call %q, got %q", "ListPages", calls[1].Method)
	}
}

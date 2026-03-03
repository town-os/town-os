package systemcontroller

import (
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/account"
)

func initTestPagesStore(t *testing.T) *PagesStore {
	t.Helper()
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { if err := db.Close(); err != nil { t.Errorf("db.Close: %v", err) } })

	store, err := InitPagesStore(db)
	if err != nil {
		t.Fatalf("InitPagesStore: %v", err)
	}
	return store
}

func TestPagesStoreCreateAndGet(t *testing.T) {
	store := initTestPagesStore(t)

	if err := store.Create(Page{
		Name:    "my-site",
		RepoURL: "https://github.com/example/site",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	page, err := store.Get("my-site")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if page.Name != "my-site" {
		t.Fatalf("expected name my-site, got %q", page.Name)
	}
	if page.Branch != "main" {
		t.Fatalf("expected default branch main, got %q", page.Branch)
	}
	if page.Domain != "my-site" {
		t.Fatalf("expected default domain my-site, got %q", page.Domain)
	}
	if page.Status != PageStatusPending {
		t.Fatalf("expected status pending, got %q", page.Status)
	}
	if page.SourceType != "archive" {
		t.Fatalf("expected default source_type archive, got %q", page.SourceType)
	}
}

func TestPagesStoreCreateWithSourceType(t *testing.T) {
	store := initTestPagesStore(t)

	if err := store.Create(Page{
		Name:       "git-site",
		RepoURL:    "https://github.com/example/site",
		SourceType: "git",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	page, err := store.Get("git-site")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if page.SourceType != "git" {
		t.Fatalf("expected source_type git, got %q", page.SourceType)
	}
}

func TestPagesStoreCreateContainerImage(t *testing.T) {
	store := initTestPagesStore(t)

	if err := store.Create(Page{
		Name:           "image-site",
		SourceType:     "container_image",
		Image:          "nginx:latest",
		ImageDirectory: "/usr/share/nginx/html",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	page, err := store.Get("image-site")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if page.SourceType != "container_image" {
		t.Fatalf("expected source_type container_image, got %q", page.SourceType)
	}
	if page.Image != "nginx:latest" {
		t.Fatalf("expected image nginx:latest, got %q", page.Image)
	}
	if page.ImageDirectory != "/usr/share/nginx/html" {
		t.Fatalf("expected image_directory /usr/share/nginx/html, got %q", page.ImageDirectory)
	}
}

func TestPagesStoreUpdate(t *testing.T) {
	store := initTestPagesStore(t)

	if err := store.Create(Page{
		Name:    "test-page",
		RepoURL: "https://github.com/example/test",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := store.Update("test-page", map[string]string{
		"branch": "develop",
		"status": PageStatusActive,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	page, err := store.Get("test-page")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if page.Branch != "develop" {
		t.Fatalf("expected branch develop, got %q", page.Branch)
	}
	if page.Status != PageStatusActive {
		t.Fatalf("expected status active, got %q", page.Status)
	}
}

func TestPagesStoreUpdateSourceType(t *testing.T) {
	store := initTestPagesStore(t)

	if err := store.Create(Page{
		Name:       "test-page",
		SourceType: "archive",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := store.Update("test-page", map[string]string{
		"source_type": "git",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	page, err := store.Get("test-page")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if page.SourceType != "git" {
		t.Fatalf("expected source_type git, got %q", page.SourceType)
	}
}

func TestPagesStoreUpdateImageFields(t *testing.T) {
	store := initTestPagesStore(t)

	if err := store.Create(Page{
		Name:       "test-page",
		SourceType: "container_image",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := store.Update("test-page", map[string]string{
		"image":           "alpine:latest",
		"image_directory": "/srv",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	page, err := store.Get("test-page")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if page.Image != "alpine:latest" {
		t.Fatalf("expected image alpine:latest, got %q", page.Image)
	}
	if page.ImageDirectory != "/srv" {
		t.Fatalf("expected image_directory /srv, got %q", page.ImageDirectory)
	}
}

func TestPagesStoreUpdateNotFound(t *testing.T) {
	store := initTestPagesStore(t)

	err := store.Update("nonexistent", map[string]string{"branch": "develop"})
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestPagesStoreRemove(t *testing.T) {
	store := initTestPagesStore(t)

	if err := store.Create(Page{
		Name:    "delete-me",
		RepoURL: "https://github.com/example/delete",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := store.Remove("delete-me"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	_, err := store.Get("delete-me")
	if err == nil {
		t.Fatal("expected error after remove")
	}
}

func TestPagesStoreRemoveNotFound(t *testing.T) {
	store := initTestPagesStore(t)

	err := store.Remove("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestPagesStoreList(t *testing.T) {
	store := initTestPagesStore(t)

	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := store.Create(Page{
			Name:    name,
			RepoURL: "https://github.com/example/" + name,
		}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	result, err := store.List(PagesListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result.Entries))
	}
	if result.TotalCount != 3 {
		t.Fatalf("expected total count 3, got %d", result.TotalCount)
	}
}

func TestPagesStoreListIncludesSourceType(t *testing.T) {
	store := initTestPagesStore(t)

	if err := store.Create(Page{
		Name:       "git-site",
		SourceType: "git",
		RepoURL:    "https://github.com/example/site",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.Create(Page{
		Name:       "archive-site",
		SourceType: "archive",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	result, err := store.List(PagesListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result.Entries))
	}
	for _, p := range result.Entries {
		if p.Name == "git-site" && p.SourceType != "git" {
			t.Errorf("expected source_type git, got %q", p.SourceType)
		}
		if p.Name == "archive-site" && p.SourceType != "archive" {
			t.Errorf("expected source_type archive, got %q", p.SourceType)
		}
	}
}

func TestPagesStoreListWithSearch(t *testing.T) {
	store := initTestPagesStore(t)

	for _, name := range []string{"my-blog", "docs-site", "app-landing"} {
		if err := store.Create(Page{
			Name:    name,
			RepoURL: "https://github.com/example/" + name,
		}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	result, err := store.List(PagesListOptions{Search: "blog"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result.Entries))
	}
	if result.Entries[0].Name != "my-blog" {
		t.Fatalf("expected my-blog, got %q", result.Entries[0].Name)
	}
}

func TestPagesStoreListPagination(t *testing.T) {
	store := initTestPagesStore(t)

	for i := range 5 {
		name := string(rune('a' + i))
		if err := store.Create(Page{
			Name:    name,
			RepoURL: "https://github.com/example/" + name,
		}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	result, err := store.List(PagesListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result.Entries))
	}
	if !result.HasMore {
		t.Fatal("expected has_more=true")
	}
	if result.TotalPages != 3 {
		t.Fatalf("expected 3 total pages, got %d", result.TotalPages)
	}
}

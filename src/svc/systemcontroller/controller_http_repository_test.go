package systemcontroller

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
)

func TestHTTPAddRepositoryBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: emptyRepoRoot(t)})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "repository/add"), bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

func TestHTTPRemoveRepositoryBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: emptyRepoRoot(t)})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "repository/remove"), bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

func TestHTTPRemoveRepositoryNotFound(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	err = c.RemoveRepository(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error removing nonexistent repository")
	}
}

func TestHTTPListRepositoriesEmpty(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repos.Entries) != 0 {
		t.Fatalf("expected empty list, got %d", len(repos.Entries))
	}
}

func TestHTTPListRepositoriesPrePopulated(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u1, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse %q: %v", "https://example.com/repo-a.git", err)
	}
	u2, err := url.Parse("https://example.com/repo-b.git")
	if err != nil {
		t.Fatalf("url.Parse %q: %v", "https://example.com/repo-b.git", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u1},
		{Name: "repo-b", URL: *u2},
	}

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repos.Entries) != 2 {
		t.Fatalf("expected 2 repositories, got %d", len(repos.Entries))
	}

	if repos.Entries[0].Name != "repo-a" || repos.Entries[1].Name != "repo-b" {
		t.Fatalf("unexpected repo names: %v", repos.Entries)
	}
}

func TestHTTPRepositoryWrongMethod(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: emptyRepoRoot(t)})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, testRoute(t, ts.Server.URL, "repository/add"), nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 for GET on POST-only route")
	}
}

func TestHTTPRefreshRepositories(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if _, err := c.RefreshRepositories(context.TODO()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPAddRepositoryBadClone(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	err = c.AddRepository(context.TODO(), "", "https://github.com/town-os/does-not-exist.git", "", "")
	if err == nil {
		t.Fatal("expected error for inaccessible repository")
	}

	repos, err := c.ListRepositories(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}

	if len(repos.Entries) != 0 {
		t.Fatalf("expected 0 repositories after failed add, got %d", len(repos.Entries))
	}
}

func TestHTTPAddRepositoryPartialCredentials(t *testing.T) {
	t.Run("username without password", func(t *testing.T) {
		mock := storage.InitBtrFSMock()
		rr := emptyRepoRoot(t)
		ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
		t.Cleanup(ts.Close)

		c, err := ts.Client()
		if err != nil {
			t.Fatalf("ts.Client: %v", err)
		}

		err = c.AddRepository(context.TODO(), "", "https://example.com/repo.git", "user", "")
		if err == nil {
			t.Fatal("expected error for username without password")
		}
	})

	t.Run("password without username", func(t *testing.T) {
		mock := storage.InitBtrFSMock()
		rr := emptyRepoRoot(t)
		ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
		t.Cleanup(ts.Close)

		c, err := ts.Client()
		if err != nil {
			t.Fatalf("ts.Client: %v", err)
		}

		err = c.AddRepository(context.TODO(), "", "https://example.com/repo.git", "", "password1")
		if err == nil {
			t.Fatal("expected error for password without username")
		}
	})
}

func TestHTTPListRepositoriesPagination(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u1, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	u2, err := url.Parse("https://example.com/repo-b.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	u3, err := url.Parse("https://example.com/repo-c.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u1},
		{Name: "repo-b", URL: *u2},
		{Name: "repo-c", URL: *u3},
	}

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// First page
	page, err := c.ListRepositories(context.TODO(), ListParams{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListRepositories page 0: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}
	if page.TotalPages != 2 {
		t.Fatalf("expected 2 total pages, got %d", page.TotalPages)
	}

	// Second page
	page, err = c.ListRepositories(context.TODO(), ListParams{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListRepositories page 1: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false")
	}
}

func TestHTTPListRepositoriesSearch(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u1, _ := url.Parse("https://example.com/core.git")
	u2, _ := url.Parse("https://example.com/extras.git")
	rr.Items = []packages.Repository{
		{Name: "core", URL: *u1},
		{Name: "extras", URL: *u2},
	}

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Search by name
	page, err := c.ListRepositories(context.TODO(), ListParams{Search: "core"})
	if err != nil {
		t.Fatalf("ListRepositories search: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if page.Entries[0].Name != "core" {
		t.Fatalf("expected core, got %s", page.Entries[0].Name)
	}

	// Search by URL
	page, err = c.ListRepositories(context.TODO(), ListParams{Search: "extras"})
	if err != nil {
		t.Fatalf("ListRepositories search by URL: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
}

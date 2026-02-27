package systemcontroller

import (
	"context"
	"net/url"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
)

func TestHTTPListPackagesInstalledOnlyFilter(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo", "nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "redis", "7.0", "image: redis:7.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "postgres", "16.0", "image: postgres:16.0\n")

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install only nginx and redis
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx: %v", err)
	}
	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis: %v", err)
	}

	// Without filter: should return all 3 packages
	page, err := c.ListPackages(context.TODO(), ListParams{SortBy: "name", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListPackages all: %v", err)
	}
	if len(page.Entries) != 3 {
		t.Fatalf("expected 3 entries without filter, got %d", len(page.Entries))
	}

	// With installed_only=true: should return only 2 installed packages
	page, err = c.ListPackages(context.TODO(), ListParams{InstalledOnly: true, SortBy: "name", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListPackages installed_only: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 installed entries, got %d", len(page.Entries))
	}
	if page.Entries[0].Name != "nginx" {
		t.Fatalf("expected first installed package 'nginx', got %q", page.Entries[0].Name)
	}
	if page.Entries[1].Name != "redis" {
		t.Fatalf("expected second installed package 'redis', got %q", page.Entries[1].Name)
	}
	for _, e := range page.Entries {
		if !e.Installed {
			t.Fatalf("expected all entries to have installed=true, got false for %q", e.Name)
		}
	}
}

func TestHTTPListPackagesInstalledOnlyWithPagination(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}

	// Create 5 packages, install 3
	for _, name := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
		writeTestPackage(t, rr.BaseDir, "repo", name, "1.0", "image: "+name+":1.0\n")
	}

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	for _, name := range []string{"alpha", "gamma", "epsilon"} {
		if err := c.InstallPackage(context.TODO(), name, "1.0", packages.Responses{}, false, "", false); err != nil {
			t.Fatalf("InstallPackage %s: %v", name, err)
		}
	}

	// installed_only with limit=2: page 1
	page, err := c.ListPackages(context.TODO(), ListParams{InstalledOnly: true, SortBy: "name", SortOrder: "asc", Limit: 2})
	if err != nil {
		t.Fatalf("ListPackages installed_only page1: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries on page 1, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true on page 1")
	}
	if page.TotalCount != 3 {
		t.Fatalf("expected total_count=3, got %d", page.TotalCount)
	}
	if page.TotalPages != 2 {
		t.Fatalf("expected total_pages=2, got %d", page.TotalPages)
	}
	if page.Entries[0].Name != "alpha" {
		t.Fatalf("expected first entry 'alpha', got %q", page.Entries[0].Name)
	}
	if page.Entries[1].Name != "epsilon" {
		t.Fatalf("expected second entry 'epsilon', got %q", page.Entries[1].Name)
	}

	// Page 2
	page, err = c.ListPackages(context.TODO(), ListParams{InstalledOnly: true, SortBy: "name", SortOrder: "asc", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListPackages installed_only page2: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry on page 2, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false on page 2")
	}
	if page.Entries[0].Name != "gamma" {
		t.Fatalf("expected 'gamma', got %q", page.Entries[0].Name)
	}
}

func TestHTTPListPackagesInstalledOnlyWithSearch(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo", "web-nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "web-apache", "1.0", "image: apache:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "db-postgres", "1.0", "image: postgres:1.0\n")

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install web-nginx and db-postgres (not web-apache)
	if err := c.InstallPackage(context.TODO(), "web-nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage web-nginx: %v", err)
	}
	if err := c.InstallPackage(context.TODO(), "db-postgres", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage db-postgres: %v", err)
	}

	// Search for "web" with installed_only: should only return web-nginx
	page, err := c.ListPackages(context.TODO(), ListParams{InstalledOnly: true, Search: "web"})
	if err != nil {
		t.Fatalf("ListPackages installed_only+search: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 result, got %d", len(page.Entries))
	}
	if page.Entries[0].Name != "web-nginx" {
		t.Fatalf("expected 'web-nginx', got %q", page.Entries[0].Name)
	}

	// Search for "web" without installed_only: should return both web packages
	page, err = c.ListPackages(context.TODO(), ListParams{Search: "web", SortBy: "name", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListPackages search only: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 results, got %d", len(page.Entries))
	}
}

func TestHTTPListPackagesInstalledOnlyEmpty(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo", "nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "redis", "7.0", "image: redis:7.0\n")

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// No packages installed, installed_only=true should return empty
	page, err := c.ListPackages(context.TODO(), ListParams{InstalledOnly: true})
	if err != nil {
		t.Fatalf("ListPackages installed_only empty: %v", err)
	}
	if len(page.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false")
	}
	if page.TotalCount != 0 {
		t.Fatalf("expected total_count=0, got %d", page.TotalCount)
	}
}

func TestHTTPListPackagesInstalledOnlyDescSort(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo", "alpha", "1.0", "image: alpha:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "beta", "1.0", "image: beta:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "gamma", "1.0", "image: gamma:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "zeta", "1.0", "image: zeta:1.0\n")

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	for _, name := range []string{"alpha", "gamma", "zeta"} {
		if err := c.InstallPackage(context.TODO(), name, "1.0", packages.Responses{}, false, "", false); err != nil {
			t.Fatalf("InstallPackage %s: %v", name, err)
		}
	}

	// installed_only + sort desc
	page, err := c.ListPackages(context.TODO(), ListParams{InstalledOnly: true, SortBy: "name", SortOrder: "desc"})
	if err != nil {
		t.Fatalf("ListPackages installed_only desc: %v", err)
	}
	if len(page.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(page.Entries))
	}
	if page.Entries[0].Name != "zeta" {
		t.Fatalf("expected first entry 'zeta', got %q", page.Entries[0].Name)
	}
	if page.Entries[2].Name != "alpha" {
		t.Fatalf("expected last entry 'alpha', got %q", page.Entries[2].Name)
	}
}

func TestHTTPListPackagesInstalledOnlyCaseInsensitiveSearch(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo", "MyApp", "1.0", "image: myapp:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "other", "1.0", "image: other:1.0\n")

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "MyApp", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	for _, term := range []string{"myapp", "MYAPP", "MyApp"} {
		page, err := c.ListPackages(context.TODO(), ListParams{InstalledOnly: true, Search: term})
		if err != nil {
			t.Fatalf("ListPackages installed_only search %q: %v", term, err)
		}
		if len(page.Entries) != 1 {
			t.Fatalf("expected 1 result for search %q, got %d", term, len(page.Entries))
		}
	}
}

func TestListParamsQueryStringInstalledOnly(t *testing.T) {
	p := ListParams{InstalledOnly: true, SortBy: "name"}
	qs := p.QueryString()

	if qs == "" {
		t.Fatal("expected non-empty query string")
	}

	values, err := url.ParseQuery(qs[1:])
	if err != nil {
		t.Fatalf("failed to parse query string: %v", err)
	}
	if values.Get("installed_only") != "true" {
		t.Fatalf("expected installed_only=true, got %q", values.Get("installed_only"))
	}
}

func TestListParamsQueryStringOmitsInstalledOnlyWhenFalse(t *testing.T) {
	p := ListParams{SortBy: "name"}
	qs := p.QueryString()

	values, err := url.ParseQuery(qs[1:])
	if err != nil {
		t.Fatalf("failed to parse query string: %v", err)
	}
	if values.Get("installed_only") != "" {
		t.Fatalf("expected installed_only to be omitted, got %q", values.Get("installed_only"))
	}
}

func TestHTTPListPackagesInstalledOnlySearchSortPagination(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}

	// Create 5 "web-" packages and 2 "db-" packages
	for _, name := range []string{"web-alpha", "web-beta", "web-gamma", "web-delta", "db-main", "db-backup"} {
		writeTestPackage(t, rr.BaseDir, "repo", name, "1.0", "image: "+name+":1.0\n")
	}

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install 3 web packages and 1 db package
	for _, name := range []string{"web-alpha", "web-gamma", "web-delta", "db-main"} {
		if err := c.InstallPackage(context.TODO(), name, "1.0", packages.Responses{}, false, "", false); err != nil {
			t.Fatalf("InstallPackage %s: %v", name, err)
		}
	}

	// installed_only + search "web" + sort asc + limit 2
	page, err := c.ListPackages(context.TODO(), ListParams{
		InstalledOnly: true,
		Search:        "web",
		SortBy:        "name",
		SortOrder:     "asc",
		Limit:         2,
		Offset:        0,
	})
	if err != nil {
		t.Fatalf("ListPackages combined: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}
	if page.TotalCount != 3 {
		t.Fatalf("expected total_count=3 (3 installed web packages), got %d", page.TotalCount)
	}
	if page.Entries[0].Name != "web-alpha" {
		t.Fatalf("expected first entry 'web-alpha', got %q", page.Entries[0].Name)
	}
	if page.Entries[1].Name != "web-delta" {
		t.Fatalf("expected second entry 'web-delta', got %q", page.Entries[1].Name)
	}

	// Page 2
	page, err = c.ListPackages(context.TODO(), ListParams{
		InstalledOnly: true,
		Search:        "web",
		SortBy:        "name",
		SortOrder:     "asc",
		Limit:         2,
		Offset:        2,
	})
	if err != nil {
		t.Fatalf("ListPackages combined page2: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry on page 2, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false on page 2")
	}
	if page.Entries[0].Name != "web-gamma" {
		t.Fatalf("expected 'web-gamma', got %q", page.Entries[0].Name)
	}
}

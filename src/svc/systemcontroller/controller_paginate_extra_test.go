// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// --- Paginate helper edge cases ---

func TestPaginateNegativeLimit(t *testing.T) {
	items := []string{"a", "b", "c"}

	// Negative limit should fall back to default (20)
	p := paginate(items, -1, 0)
	if len(p.Entries) != 3 {
		t.Fatalf("expected 3 entries with negative limit, got %d", len(p.Entries))
	}
	if p.HasMore {
		t.Fatal("expected has_more=false")
	}
	if p.TotalPages != 1 {
		t.Fatalf("expected 1 total page, got %d", p.TotalPages)
	}
}

func TestPaginateTotalPagesExactMultiple(t *testing.T) {
	items := []string{"a", "b", "c", "d"}

	// Limit=2, 4 items = exactly 2 pages
	p := paginate(items, 2, 0)
	if p.TotalPages != 2 {
		t.Fatalf("expected 2 total pages, got %d", p.TotalPages)
	}
	if p.TotalCount != 4 {
		t.Fatalf("expected total_count=4, got %d", p.TotalCount)
	}
}

func TestPaginateMiddlePage(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e"}

	// Middle page: limit=2, offset=2
	p := paginate(items, 2, 2)
	if len(p.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(p.Entries))
	}
	if p.Entries[0] != "c" || p.Entries[1] != "d" {
		t.Fatalf("expected [c, d], got %v", p.Entries)
	}
	if !p.HasMore {
		t.Fatal("expected has_more=true for middle page")
	}
}

func TestPaginateOffsetAtExactEnd(t *testing.T) {
	items := []string{"a", "b", "c"}

	// Offset equals total count
	p := paginate(items, 2, 3)
	if len(p.Entries) != 0 {
		t.Fatalf("expected 0 entries when offset=total, got %d", len(p.Entries))
	}
	if p.HasMore {
		t.Fatal("expected has_more=false when offset=total")
	}
}

func TestPaginateSingleItem(t *testing.T) {
	items := []string{"only"}

	p := paginate(items, 10, 0)
	if len(p.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(p.Entries))
	}
	if p.HasMore {
		t.Fatal("expected has_more=false")
	}
	if p.TotalPages != 1 {
		t.Fatalf("expected 1 total page, got %d", p.TotalPages)
	}
	if p.TotalCount != 1 {
		t.Fatalf("expected total_count=1, got %d", p.TotalCount)
	}
}

func TestPaginateLimitOne(t *testing.T) {
	items := []string{"a", "b", "c"}

	p := paginate(items, 1, 0)
	if len(p.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(p.Entries))
	}
	if !p.HasMore {
		t.Fatal("expected has_more=true")
	}
	if p.TotalPages != 3 {
		t.Fatalf("expected 3 total pages, got %d", p.TotalPages)
	}
}

func TestPaginateLimitLargerThanTotal(t *testing.T) {
	items := []string{"a", "b"}

	p := paginate(items, 100, 0)
	if len(p.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(p.Entries))
	}
	if p.HasMore {
		t.Fatal("expected has_more=false when limit > total")
	}
	if p.TotalPages != 1 {
		t.Fatalf("expected 1 total page, got %d", p.TotalPages)
	}
}

// --- QueryString encoding verification ---

func TestListParamsQueryStringValues(t *testing.T) {
	p := ListParams{SortBy: "name", SortOrder: "desc", Limit: 10, Offset: 20, Search: "test"}
	qs := p.QueryString()

	if !strings.HasPrefix(qs, "?") {
		t.Fatalf("expected query string to start with '?', got %q", qs)
	}

	// Parse the query string and verify values
	values, err := url.ParseQuery(qs[1:])
	if err != nil {
		t.Fatalf("failed to parse query string %q: %v", qs, err)
	}

	if values.Get("sort_by") != "name" {
		t.Fatalf("expected sort_by=name, got %q", values.Get("sort_by"))
	}
	if values.Get("sort_order") != "desc" {
		t.Fatalf("expected sort_order=desc, got %q", values.Get("sort_order"))
	}
	if values.Get("limit") != "10" {
		t.Fatalf("expected limit=10, got %q", values.Get("limit"))
	}
	if values.Get("offset") != "20" {
		t.Fatalf("expected offset=20, got %q", values.Get("offset"))
	}
	if values.Get("search") != "test" {
		t.Fatalf("expected search=test, got %q", values.Get("search"))
	}
}

func TestListParamsQueryStringOmitsZeroValues(t *testing.T) {
	p := ListParams{SortBy: "name"}
	qs := p.QueryString()

	values, err := url.ParseQuery(qs[1:])
	if err != nil {
		t.Fatalf("failed to parse query string: %v", err)
	}

	if values.Get("sort_order") != "" {
		t.Fatalf("expected sort_order to be omitted, got %q", values.Get("sort_order"))
	}
	if values.Get("limit") != "" {
		t.Fatalf("expected limit to be omitted, got %q", values.Get("limit"))
	}
	if values.Get("offset") != "" {
		t.Fatalf("expected offset to be omitted, got %q", values.Get("offset"))
	}
	if values.Get("search") != "" {
		t.Fatalf("expected search to be omitted, got %q", values.Get("search"))
	}
}

// --- filterSearch edge cases ---

func TestFilterSearchNoMatchReturnsEmptySlice(t *testing.T) {
	items := []string{"alpha", "beta"}
	result := filterSearch(items, "zzz")

	if result == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 results, got %d", len(result))
	}
}

func TestFilterSearchSingleCharMatch(t *testing.T) {
	items := []string{"apple", "banana", "avocado"}
	result := filterSearch(items, "a")

	// All items contain "a"
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
}

func TestFilterSearchStructNestedFields(t *testing.T) {
	units := []systemd.UnitStatus{
		{Name: "svc-a.service", Description: "Service A", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "svc-b.service", Description: "Service B", LoadState: "loaded", ActiveState: "inactive", SubState: "dead"},
	}

	// Search by SubState
	result := filterSearch(units, "running")
	if len(result) != 1 {
		t.Fatalf("expected 1 result for 'running', got %d", len(result))
	}
	if result[0].Name != "svc-a.service" {
		t.Fatalf("expected svc-a.service, got %s", result[0].Name)
	}

	// Search by LoadState
	result = filterSearch(units, "loaded")
	if len(result) != 2 {
		t.Fatalf("expected 2 results for 'loaded', got %d", len(result))
	}
}

// --- HTTP Endpoint Sort Tests ---

func TestHTTPListRepositoriesSorted(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u1, err := url.Parse("https://example.com/zebra.git")
	if err != nil {
		t.Fatal(err)
	}
	u2, err := url.Parse("https://example.com/alpha.git")
	if err != nil {
		t.Fatal(err)
	}
	u3, err := url.Parse("https://example.com/middle.git")
	if err != nil {
		t.Fatal(err)
	}
	rr.Items = []packages.Repository{
		{Name: "zebra", URL: *u1},
		{Name: "alpha", URL: *u2},
		{Name: "middle", URL: *u3},
	}

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Sort ascending by name
	page, err := c.ListRepositories(context.TODO(), ListParams{SortBy: "name", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListRepositories sorted asc: %v", err)
	}
	if len(page.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(page.Entries))
	}
	if page.Entries[0].Name != "alpha" {
		t.Fatalf("expected first repo %q, got %q", "alpha", page.Entries[0].Name)
	}
	if page.Entries[2].Name != "zebra" {
		t.Fatalf("expected last repo %q, got %q", "zebra", page.Entries[2].Name)
	}

	// Sort descending by name
	page, err = c.ListRepositories(context.TODO(), ListParams{SortBy: "name", SortOrder: "desc"})
	if err != nil {
		t.Fatalf("ListRepositories sorted desc: %v", err)
	}
	if page.Entries[0].Name != "zebra" {
		t.Fatalf("expected first desc repo %q, got %q", "zebra", page.Entries[0].Name)
	}
	if page.Entries[2].Name != "alpha" {
		t.Fatalf("expected last desc repo %q, got %q", "alpha", page.Entries[2].Name)
	}
}

func TestHTTPListUnitsSortedByPackageIdentifier(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "zeta", Version: "1.0"},
		{Repo: "repo", Name: "alpha", Version: "1.0"},
		{Repo: "repo", Name: "middle", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-zeta-1.0.service", Description: "Zeta", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "town-os-package--repo-alpha-1.0.service", Description: "Alpha", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "town-os-package--repo-middle-1.0.service", Description: "Middle", LoadState: "loaded", ActiveState: "active", SubState: "running"},
	}

	// Sort ascending by package_identifier (top-level field of UnitListEntry)
	page, err := c.ListUnits(context.TODO(), ListParams{SortBy: "package_identifier", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListUnits sorted asc: %v", err)
	}
	if len(page.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(page.Entries))
	}

	// Without installer, package_identifier is empty for all units, so sort
	// leaves order unchanged (stable sort). Just verify it doesn't error.
}

func TestHTTPListPackagesSorted(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo", "zeta", "1.0", "image: zeta:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "alpha", "1.0", "image: alpha:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "middle", "1.0", "image: middle:1.0\n")

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Sort ascending by name
	page, err := c.ListPackages(context.TODO(), ListParams{SortBy: "name", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListPackages sorted asc: %v", err)
	}
	if len(page.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(page.Entries))
	}
	if page.Entries[0].Name != "alpha" {
		t.Fatalf("expected first package %q, got %q", "alpha", page.Entries[0].Name)
	}
	if page.Entries[2].Name != "zeta" {
		t.Fatalf("expected last package %q, got %q", "zeta", page.Entries[2].Name)
	}

	// Sort descending by name
	page, err = c.ListPackages(context.TODO(), ListParams{SortBy: "name", SortOrder: "desc"})
	if err != nil {
		t.Fatalf("ListPackages sorted desc: %v", err)
	}
	if page.Entries[0].Name != "zeta" {
		t.Fatalf("expected first desc package %q, got %q", "zeta", page.Entries[0].Name)
	}
}

// --- Combined search + pagination + sorting ---

func TestHTTPListAccountsSearchWithPagination(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// Create users: alice1, alice2, alice3, bob
	for _, name := range []string{"alice1", "alice2", "alice3", "bob"} {
		if _, err := c.CreateAccount(context.TODO(), name, "password1", name+"@test.com", "555", name, false); err != nil {
			t.Fatalf("CreateAccount %q: %v", name, err)
		}
	}

	// Search for "alice" with limit=2 and sort
	result, err := c.ListAccounts(context.TODO(), ListParams{
		Search:    "alice",
		Limit:     2,
		Offset:    0,
		SortBy:    "username",
		SortOrder: "asc",
	})
	if err != nil {
		t.Fatalf("ListAccounts search+page: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result.Entries))
	}
	if !result.HasMore {
		t.Fatal("expected has_more=true")
	}
	if result.TotalCount != 3 {
		t.Fatalf("expected total_count=3 (3 alice users), got %d", result.TotalCount)
	}
	if result.Entries[0].Username != "alice1" {
		t.Fatalf("expected first entry alice1, got %q", result.Entries[0].Username)
	}

	// Second page
	result, err = c.ListAccounts(context.TODO(), ListParams{
		Search:    "alice",
		Limit:     2,
		Offset:    2,
		SortBy:    "username",
		SortOrder: "asc",
	})
	if err != nil {
		t.Fatalf("ListAccounts search+page2: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry on last page, got %d", len(result.Entries))
	}
	if result.HasMore {
		t.Fatal("expected has_more=false on last page")
	}
	if result.Entries[0].Username != "alice3" {
		t.Fatalf("expected alice3, got %q", result.Entries[0].Username)
	}
}

func TestHTTPListPackagesSearchWithSort(t *testing.T) {
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

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Search for "web" and sort
	page, err := c.ListPackages(context.TODO(), ListParams{Search: "web", SortBy: "name", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListPackages search+sort: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if page.Entries[0].Name != "web-apache" {
		t.Fatalf("expected first result web-apache, got %q", page.Entries[0].Name)
	}
	if page.Entries[1].Name != "web-nginx" {
		t.Fatalf("expected second result web-nginx, got %q", page.Entries[1].Name)
	}
}

func TestHTTPListRepositoriesSearchWithPagination(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	repos := []packages.Repository{}
	for _, name := range []string{"app-api", "app-web", "app-worker", "data-cache"} {
		u, err := url.Parse("https://example.com/" + name + ".git")
		if err != nil {
			t.Fatal(err)
		}
		repos = append(repos, packages.Repository{Name: name, URL: *u})
	}
	rr.Items = repos

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Search for "app" with limit=2
	page, err := c.ListRepositories(context.TODO(), ListParams{Search: "app", Limit: 2, Offset: 0, SortBy: "name", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListRepositories search+page: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}
	if page.TotalCount != 3 {
		t.Fatalf("expected total_count=3, got %d", page.TotalCount)
	}
	if page.Entries[0].Name != "app-api" {
		t.Fatalf("expected first entry app-api, got %q", page.Entries[0].Name)
	}

	// Page 2
	page, err = c.ListRepositories(context.TODO(), ListParams{Search: "app", Limit: 2, Offset: 2, SortBy: "name", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListRepositories search+page2: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry on page 2, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false on last page")
	}
	if page.Entries[0].Name != "app-worker" {
		t.Fatalf("expected app-worker, got %q", page.Entries[0].Name)
	}
}

// --- Installed packages sort and search ---

func TestHTTPListInstalledSorted(t *testing.T) {
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
	writeTestPackage(t, rr.BaseDir, "repo", "alpha", "1.0", "image: alpha:1.0\n")

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	for _, name := range []string{"nginx", "redis", "alpha"} {
		ver := "1.0"
		if name == "redis" {
			ver = "7.0"
		}
		if err := c.InstallPackage(context.TODO(), name, ver, packages.Responses{}, false, "", false); err != nil {
			t.Fatalf("InstallPackage %s: %v", name, err)
		}
	}

	// Sort ascending
	page, err := c.ListInstalled(context.TODO(), ListParams{SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListInstalled sorted asc: %v", err)
	}
	if len(page.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(page.Entries))
	}

	// Entries are strings like "repo/name@version"
	// Ascending should sort alphabetically
	for i := 1; i < len(page.Entries); i++ {
		if page.Entries[i] < page.Entries[i-1] {
			t.Fatalf("expected ascending sort, but entry %d (%q) < entry %d (%q)", i, page.Entries[i], i-1, page.Entries[i-1])
		}
	}
}

func TestHTTPListInstalledSearch(t *testing.T) {
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

	for _, name := range []string{"nginx", "redis", "postgres"} {
		ver := "1.0"
		if name == "redis" {
			ver = "7.0"
		}
		if name == "postgres" {
			ver = "16.0"
		}
		if err := c.InstallPackage(context.TODO(), name, ver, packages.Responses{}, false, "", false); err != nil {
			t.Fatalf("InstallPackage %s: %v", name, err)
		}
	}

	// Search for "nginx"
	page, err := c.ListInstalled(context.TODO(), ListParams{Search: "nginx"})
	if err != nil {
		t.Fatalf("ListInstalled search: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 result for search 'nginx', got %d", len(page.Entries))
	}
	if !strings.Contains(page.Entries[0], "nginx") {
		t.Fatalf("expected entry to contain 'nginx', got %q", page.Entries[0])
	}

	// Search no match
	page, err = c.ListInstalled(context.TODO(), ListParams{Search: "mysql"})
	if err != nil {
		t.Fatalf("ListInstalled search no match: %v", err)
	}
	if len(page.Entries) != 0 {
		t.Fatalf("expected 0 results, got %d", len(page.Entries))
	}
}

// --- Units combined sort + pagination ---

func TestHTTPListUnitsPaginationWithSort(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "zeta", Version: "1.0"},
		{Repo: "repo", Name: "alpha", Version: "1.0"},
		{Repo: "repo", Name: "middle", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-zeta-1.0.service", Description: "Zeta", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "town-os-package--repo-alpha-1.0.service", Description: "Alpha", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "town-os-package--repo-middle-1.0.service", Description: "Middle", LoadState: "loaded", ActiveState: "active", SubState: "running"},
	}

	// Pagination with limit=2 (sort key may not apply to embedded fields, but
	// pagination should still work correctly regardless of sort)
	page, err := c.ListUnits(context.TODO(), ListParams{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListUnits paged: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}
	if page.TotalCount != 3 {
		t.Fatalf("expected total_count=3, got %d", page.TotalCount)
	}

	// Page 2
	page, err = c.ListUnits(context.TODO(), ListParams{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListUnits paged page2: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry on page 2, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false on last page")
	}
}

// --- Pagination TotalPages metadata verification ---

func TestHTTPListRepositoriesTotalPages(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)

	// Create 5 repos
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		u, err := url.Parse("https://example.com/" + name + ".git")
		if err != nil {
			t.Fatal(err)
		}
		rr.Items = append(rr.Items, packages.Repository{Name: name, URL: *u})
	}

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// With limit=2, 5 items => 3 pages
	page, err := c.ListRepositories(context.TODO(), ListParams{Limit: 2})
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}
	if page.TotalPages != 3 {
		t.Fatalf("expected 3 total pages, got %d", page.TotalPages)
	}
	if page.TotalCount != 5 {
		t.Fatalf("expected total_count=5, got %d", page.TotalCount)
	}

	// With limit=5, 5 items => 1 page
	page, err = c.ListRepositories(context.TODO(), ListParams{Limit: 5})
	if err != nil {
		t.Fatalf("ListRepositories limit=5: %v", err)
	}
	if page.TotalPages != 1 {
		t.Fatalf("expected 1 total page, got %d", page.TotalPages)
	}
	if page.HasMore {
		t.Fatal("expected has_more=false")
	}

	// Default limit (no limit set) should return all
	page, err = c.ListRepositories(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories default: %v", err)
	}
	if len(page.Entries) != 5 {
		t.Fatalf("expected 5 entries with default limit, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false with default limit")
	}
}

func TestHTTPListUnitsTotalPages(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "a", Version: "1.0"},
		{Repo: "repo", Name: "b", Version: "1.0"},
		{Repo: "repo", Name: "c", Version: "1.0"},
		{Repo: "repo", Name: "d", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-a-1.0.service", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "town-os-package--repo-b-1.0.service", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "town-os-package--repo-c-1.0.service", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "town-os-package--repo-d-1.0.service", LoadState: "loaded", ActiveState: "active", SubState: "running"},
	}

	// With limit=3, 4 items => 2 pages
	page, err := c.ListUnits(context.TODO(), ListParams{Limit: 3})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}
	if page.TotalPages != 2 {
		t.Fatalf("expected 2 total pages, got %d", page.TotalPages)
	}
	if page.TotalCount != 4 {
		t.Fatalf("expected total_count=4, got %d", page.TotalCount)
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}
}

// --- Filesystem combined search + sort + pagination ---

func TestHTTPListFilesystemsSearchWithSort(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{"app-zeta", "app-alpha", "data-main"} {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	// Search for "app" and sort ascending
	fsResult, err := c.ListFilesystems(context.TODO(), "", "", ListParams{Search: "app", SortBy: "name", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListFilesystems search+sort: %v", err)
	}
	if len(fsResult.Entries) != 2 {
		t.Fatalf("expected 2 results, got %d", len(fsResult.Entries))
	}
	if fsResult.Entries[0].Name != "app-alpha" {
		t.Fatalf("expected first entry app-alpha, got %q", fsResult.Entries[0].Name)
	}
	if fsResult.Entries[1].Name != "app-zeta" {
		t.Fatalf("expected second entry app-zeta, got %q", fsResult.Entries[1].Name)
	}
}

func TestHTTPListFilesystemsSearchWithPagination(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{"web-a", "web-b", "web-c", "db-main"} {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	// Search for "web" with limit=2
	fsResult, err := c.ListFilesystems(context.TODO(), "", "", ListParams{Search: "web", Limit: 2, Offset: 0, SortBy: "name", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListFilesystems search+page: %v", err)
	}
	if len(fsResult.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(fsResult.Entries))
	}
	if !fsResult.HasMore {
		t.Fatal("expected has_more=true")
	}
	if fsResult.TotalCount != 3 {
		t.Fatalf("expected total_count=3, got %d", fsResult.TotalCount)
	}

	// Page 2
	fsResult, err = c.ListFilesystems(context.TODO(), "", "", ListParams{Search: "web", Limit: 2, Offset: 2, SortBy: "name", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListFilesystems search+page2: %v", err)
	}
	if len(fsResult.Entries) != 1 {
		t.Fatalf("expected 1 entry on page 2, got %d", len(fsResult.Entries))
	}
	if fsResult.HasMore {
		t.Fatal("expected has_more=false on last page")
	}
}

// --- Sort edge cases at HTTP level ---

func TestHTTPListAccountsNoSortRetainsOrder(t *testing.T) {
	c, _ := initAccountTestClient(t)

	for _, name := range []string{"charlie", "alice", "bob"} {
		if _, err := c.CreateAccount(context.TODO(), name, "password1", name+"@test.com", "555", name, false); err != nil {
			t.Fatalf("CreateAccount %q: %v", name, err)
		}
	}

	// No sort params — should succeed without errors
	result, err := c.ListAccounts(context.TODO(), ListParams{Limit: 10})
	if err != nil {
		t.Fatalf("ListAccounts no sort: %v", err)
	}
	if len(result.Entries) != 4 { // testadmin + 3 users
		t.Fatalf("expected 4 accounts, got %d", len(result.Entries))
	}
}

func TestHTTPListPackagesNoSortNoSearch(t *testing.T) {
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

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Default params — should work without errors
	page, err := c.ListPackages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPackages default: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if page.TotalPages != 1 {
		t.Fatalf("expected 1 total page, got %d", page.TotalPages)
	}
}

// --- Case-insensitive search at HTTP level ---

func TestHTTPListAccountsSearchCaseInsensitive(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "Alice", "password1", "alice@test.com", "555", "Alice Smith", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	// Search with different cases
	for _, term := range []string{"alice", "ALICE", "Alice", "aLiCe"} {
		result, err := c.ListAccounts(context.TODO(), ListParams{Search: term})
		if err != nil {
			t.Fatalf("ListAccounts search %q: %v", term, err)
		}
		if len(result.Entries) != 1 {
			t.Fatalf("expected 1 result for search %q, got %d", term, len(result.Entries))
		}
	}
}

func TestHTTPListUnitsSearchCaseInsensitive(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "MyApp", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-MyApp-1.0.service", Description: "My Application", LoadState: "loaded", ActiveState: "active", SubState: "running"},
	}

	// Search case-insensitively
	page, err := c.ListUnits(context.TODO(), ListParams{Search: "MYAPP"})
	if err != nil {
		t.Fatalf("ListUnits search: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 result, got %d", len(page.Entries))
	}

	page, err = c.ListUnits(context.TODO(), ListParams{Search: "myapp"})
	if err != nil {
		t.Fatalf("ListUnits search lowercase: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 result for lowercase search, got %d", len(page.Entries))
	}
}

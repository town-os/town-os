package systemcontroller

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// --- tagName unit tests ---

func TestTagNameBasic(t *testing.T) {
	if got := tagName("name"); got != "name" {
		t.Fatalf("expected %q, got %q", "name", got)
	}
}

func TestTagNameWithOmitempty(t *testing.T) {
	if got := tagName("name,omitempty"); got != "name" {
		t.Fatalf("expected %q, got %q", "name", got)
	}
}

func TestTagNameDash(t *testing.T) {
	if got := tagName("-"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestTagNameEmpty(t *testing.T) {
	if got := tagName(""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestTagNameMultipleOptions(t *testing.T) {
	if got := tagName("field,omitempty,string"); got != "field" {
		t.Fatalf("expected %q, got %q", "field", got)
	}
}

// --- fieldIndex unit tests ---

func TestFieldIndexByJsonTag(t *testing.T) {
	idx, ok := fieldIndex[sortTestStruct]("name")
	if !ok {
		t.Fatal("expected to find field by json tag 'name'")
	}
	if idx != 0 {
		t.Fatalf("expected index 0, got %d", idx)
	}
}

func TestFieldIndexByFieldName(t *testing.T) {
	// sortTestNoTag has no json tags, so it falls back to field name
	idx, ok := fieldIndex[sortTestNoTag]("Name")
	if !ok {
		t.Fatal("expected to find field by name 'Name'")
	}
	if idx != 0 {
		t.Fatalf("expected index 0, got %d", idx)
	}
}

func TestFieldIndexNotFound(t *testing.T) {
	_, ok := fieldIndex[sortTestStruct]("nonexistent")
	if ok {
		t.Fatal("expected field not found")
	}
}

// --- compareValues unit tests ---

func TestCompareValuesString(t *testing.T) {
	a := reflect.ValueOf("alice")
	b := reflect.ValueOf("bob")

	if cmp := compareValues(a, b); cmp >= 0 {
		t.Fatalf("expected alice < bob, got %d", cmp)
	}
	if cmp := compareValues(b, a); cmp <= 0 {
		t.Fatalf("expected bob > alice, got %d", cmp)
	}
	if cmp := compareValues(a, a); cmp != 0 {
		t.Fatalf("expected equal, got %d", cmp)
	}
}

func TestCompareValuesInt(t *testing.T) {
	a := reflect.ValueOf(int64(10))
	b := reflect.ValueOf(int64(20))

	if cmp := compareValues(a, b); cmp >= 0 {
		t.Fatalf("expected 10 < 20, got %d", cmp)
	}
	if cmp := compareValues(b, a); cmp <= 0 {
		t.Fatalf("expected 20 > 10, got %d", cmp)
	}
	if cmp := compareValues(a, a); cmp != 0 {
		t.Fatalf("expected equal, got %d", cmp)
	}
}

func TestCompareValuesUint(t *testing.T) {
	a := reflect.ValueOf(uint64(5))
	b := reflect.ValueOf(uint64(15))

	if cmp := compareValues(a, b); cmp >= 0 {
		t.Fatalf("expected 5 < 15, got %d", cmp)
	}
	if cmp := compareValues(b, a); cmp <= 0 {
		t.Fatalf("expected 15 > 5, got %d", cmp)
	}
}

func TestCompareValuesBool(t *testing.T) {
	f := reflect.ValueOf(false)
	tr := reflect.ValueOf(true)

	if cmp := compareValues(f, tr); cmp >= 0 {
		t.Fatalf("expected false < true, got %d", cmp)
	}
	if cmp := compareValues(tr, f); cmp <= 0 {
		t.Fatalf("expected true > false, got %d", cmp)
	}
	if cmp := compareValues(tr, tr); cmp != 0 {
		t.Fatalf("expected equal, got %d", cmp)
	}
}

func TestCompareValuesTime(t *testing.T) {
	t1 := reflect.ValueOf(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	t2 := reflect.ValueOf(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))

	if cmp := compareValues(t1, t2); cmp >= 0 {
		t.Fatalf("expected t1 < t2, got %d", cmp)
	}
	if cmp := compareValues(t2, t1); cmp <= 0 {
		t.Fatalf("expected t2 > t1, got %d", cmp)
	}
	if cmp := compareValues(t1, t1); cmp != 0 {
		t.Fatalf("expected equal, got %d", cmp)
	}
}

func TestCompareValuesDefaultFallback(t *testing.T) {
	// Float values go through the default branch (Sprint comparison)
	a := reflect.ValueOf(1.5)
	b := reflect.ValueOf(2.5)

	cmp := compareValues(a, b)
	if cmp >= 0 {
		t.Fatalf("expected 1.5 < 2.5 via Sprint, got %d", cmp)
	}
}

// --- matchesSearch edge cases ---

func TestMatchesSearchNonStringNonStructValue(t *testing.T) {
	// matchesSearch with a plain int — exercises the non-struct, non-string fallback
	if !matchesSearch(42, "42") {
		t.Fatal("expected 42 to match '42'")
	}
	if matchesSearch(42, "99") {
		t.Fatal("expected 42 not to match '99'")
	}
}

func TestMatchesSearchNestedStruct(t *testing.T) {
	type Inner struct {
		City string
	}
	type Outer struct {
		Name  string
		Addr  Inner
		Count int
	}

	item := Outer{Name: "Alice", Addr: Inner{City: "Portland"}, Count: 5}
	if !matchesSearch(item, "portland") {
		t.Fatal("expected nested struct field to match")
	}
	if !matchesSearch(item, "alice") {
		t.Fatal("expected top-level field to match")
	}
	if matchesSearch(item, "seattle") {
		t.Fatal("expected no match for 'seattle'")
	}
}

// --- filterSearch edge cases ---

func TestFilterSearchEmptySlice(t *testing.T) {
	result := filterSearch([]string{}, "test")
	if result == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 results, got %d", len(result))
	}
}

func TestFilterSearchNilSlice(t *testing.T) {
	var items []string
	result := filterSearch(items, "test")
	if result == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 results, got %d", len(result))
	}
}

// --- sortSlice direction edge cases ---

func TestSortSliceEmptySortOrderDefaultsToAsc(t *testing.T) {
	data := []sortTestStruct{
		{Name: "charlie"},
		{Name: "alice"},
		{Name: "bob"},
	}

	sortSlice(data, "name", "")

	// Empty sortOrder is not "asc" or "desc" and is empty, which means
	// it falls into the pinned path (empty pinned value). With empty pinned,
	// no items match, so they sort alphabetically by comparison.
	expected := []string{"alice", "bob", "charlie"}
	for i, want := range expected {
		if data[i].Name != want {
			t.Fatalf("index %d: expected %q, got %q", i, want, data[i].Name)
		}
	}
}

func TestSortSliceCaseInsensitiveDirection(t *testing.T) {
	data := []sortTestStruct{
		{Name: "charlie"},
		{Name: "alice"},
		{Name: "bob"},
	}

	sortSlice(data, "name", "ASC")
	expected := []string{"alice", "bob", "charlie"}
	for i, want := range expected {
		if data[i].Name != want {
			t.Fatalf("ASC index %d: expected %q, got %q", i, want, data[i].Name)
		}
	}

	data = []sortTestStruct{
		{Name: "alice"},
		{Name: "charlie"},
		{Name: "bob"},
	}
	sortSlice(data, "name", "DESC")
	expectedDesc := []string{"charlie", "bob", "alice"}
	for i, want := range expectedDesc {
		if data[i].Name != want {
			t.Fatalf("DESC index %d: expected %q, got %q", i, want, data[i].Name)
		}
	}
}

// --- Paginate nil/empty edge cases ---

func TestPaginateNilSlice(t *testing.T) {
	var items []string
	p := paginate(items, 10, 0)
	if p.Entries == nil {
		t.Fatal("expected non-nil entries for nil input")
	}
	if len(p.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(p.Entries))
	}
	if p.TotalCount != 0 {
		t.Fatalf("expected total_count=0, got %d", p.TotalCount)
	}
	if p.TotalPages != 1 {
		t.Fatalf("expected total_pages=1, got %d", p.TotalPages)
	}
}

func TestPaginateLargeLimit(t *testing.T) {
	items := []string{"a", "b"}
	p := paginate(items, 1000000, 0)
	if len(p.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(p.Entries))
	}
	if p.HasMore {
		t.Fatal("expected has_more=false")
	}
	if p.TotalPages != 1 {
		t.Fatalf("expected 1 total page, got %d", p.TotalPages)
	}
}

// --- HTTP endpoint: ListInstalled descending sort ---

func TestHTTPListInstalledSortedDesc(t *testing.T) {
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
	writeTestPackage(t, rr.BaseDir, "repo", "nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "zeta", "1.0", "image: zeta:1.0\n")

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	for _, name := range []string{"alpha", "nginx", "zeta"} {
		if err := c.InstallPackage(context.TODO(), name, "1.0", packages.Responses{}, false, "", false); err != nil {
			t.Fatalf("InstallPackage %s: %v", name, err)
		}
	}

	// Sort descending
	page, err := c.ListInstalled(context.TODO(), ListParams{SortOrder: "desc"})
	if err != nil {
		t.Fatalf("ListInstalled desc: %v", err)
	}
	if len(page.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(page.Entries))
	}

	// Verify descending order
	for i := 1; i < len(page.Entries); i++ {
		if page.Entries[i] > page.Entries[i-1] {
			t.Fatalf("expected descending sort, but entry %d (%q) > entry %d (%q)", i, page.Entries[i], i-1, page.Entries[i-1])
		}
	}
}

// --- HTTP endpoint: ListInstalled combined search + sort + pagination ---

func TestHTTPListInstalledSearchSortPagination(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}
	for _, name := range []string{"web-a", "web-b", "web-c", "db-main"} {
		writeTestPackage(t, rr.BaseDir, "repo", name, "1.0", fmt.Sprintf("image: %s:1.0\n", name))
	}

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	for _, name := range []string{"web-a", "web-b", "web-c", "db-main"} {
		if err := c.InstallPackage(context.TODO(), name, "1.0", packages.Responses{}, false, "", false); err != nil {
			t.Fatalf("InstallPackage %s: %v", name, err)
		}
	}

	// Search "web" + sort asc + limit=2
	page, err := c.ListInstalled(context.TODO(), ListParams{Search: "web", SortOrder: "asc", Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListInstalled search+sort+page: %v", err)
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
	page, err = c.ListInstalled(context.TODO(), ListParams{Search: "web", SortOrder: "asc", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListInstalled search+sort+page2: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry on page 2, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false on last page")
	}
}

// --- HTTP endpoint: audit log search ---

func TestHTTPAuditLogSearch(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	// create admin and authenticate
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// Insert audit entries with distinct actions
	for _, action := range []string{"install package", "remove package", "create account"} {
		if err := auditMgr.LogEntry(account.AuditEntry{
			Account: "admin",
			Action:  action,
			Path:    "/test/" + strings.ReplaceAll(action, " ", "-"),
			Success: true,
		}); err != nil {
			t.Fatalf("LogEntry: %v", err)
		}
	}

	// Search for "install"
	page, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{Search: "install"}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog search: %v", err)
	}

	found := false
	for _, e := range page.Entries {
		if e.Action == "install package" {
			found = true
		}
		if e.Action == "remove package" {
			t.Fatal("search 'install' should not return 'remove package' entry")
		}
	}
	if !found {
		t.Fatal("expected to find 'install package' in search results")
	}
}

// --- HTTP endpoint: audit log account filter ---

func TestHTTPAuditLogFilterByAccount(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// Insert entries from different accounts
	for _, user := range []string{"alice", "bob", "alice"} {
		if err := auditMgr.LogEntry(account.AuditEntry{
			Account: user,
			Action:  "test action",
			Path:    "/test",
			Success: true,
		}); err != nil {
			t.Fatalf("LogEntry: %v", err)
		}
	}

	// Filter by account "alice"
	page, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{Account: "alice"}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog filter by account: %v", err)
	}

	for _, e := range page.Entries {
		if e.Account != "alice" {
			t.Fatalf("expected only alice entries, got account %q", e.Account)
		}
	}

	if len(page.Entries) < 2 {
		t.Fatalf("expected at least 2 alice entries, got %d", len(page.Entries))
	}
}

// --- HTTP endpoint: audit log TotalPages and TotalCount ---

func TestHTTPAuditLogTotalPagesAndCount(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// Insert 5 entries
	for i := range 5 {
		if err := auditMgr.LogEntry(account.AuditEntry{
			Account: "admin",
			Action:  fmt.Sprintf("action-%d", i),
			Path:    "/test",
			Success: true,
		}); err != nil {
			t.Fatalf("LogEntry: %v", err)
		}
	}

	// Get with limit=2
	page, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{Limit: 2}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}

	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}
	// TotalCount should include at least the 5 test entries plus entries from create/auth
	if page.TotalCount < 5 {
		t.Fatalf("expected total_count >= 5, got %d", page.TotalCount)
	}
	if page.TotalPages < 3 {
		t.Fatalf("expected total_pages >= 3, got %d", page.TotalPages)
	}
}

// --- HTTP endpoint: audit log offset-based pagination ---

func TestHTTPAuditLogOffsetPagination(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// Insert entries with unique actions
	for i := range 4 {
		if err := auditMgr.LogEntry(account.AuditEntry{
			Account: "admin",
			Action:  fmt.Sprintf("offset-test-%d", i),
			Path:    "/test",
			Success: true,
		}); err != nil {
			t.Fatalf("LogEntry: %v", err)
		}
	}

	// Use offset-based pagination (not cursor-based)
	page1, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{
		Limit:     2,
		Offset:    0,
		SortBy:    "id",
		SortOrder: "asc",
	}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog page1: %v", err)
	}
	if len(page1.Entries) != 2 {
		t.Fatalf("expected 2 entries on page 1, got %d", len(page1.Entries))
	}
	if !page1.HasMore {
		t.Fatal("expected has_more=true on page 1")
	}

	page2, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{
		Limit:     2,
		Offset:    2,
		SortBy:    "id",
		SortOrder: "asc",
	}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog page2: %v", err)
	}
	if len(page2.Entries) < 1 {
		t.Fatal("expected at least 1 entry on page 2")
	}

	// Ensure no overlap
	for _, e1 := range page1.Entries {
		for _, e2 := range page2.Entries {
			if e1.ID == e2.ID {
				t.Fatalf("found duplicate ID %d across pages", e1.ID)
			}
		}
	}
}

// --- HTTP endpoint: ListUnits search + sort combined ---

func TestHTTPListUnitsSearchWithSort(t *testing.T) {
	c, sd := initSystemdTestClient(t)

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-web-nginx-1.0.service", Description: "NGINX web", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "town-os-package--repo-web-apache-1.0.service", Description: "Apache web", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "town-os-package--repo-db-postgres-1.0.service", Description: "PostgreSQL", LoadState: "loaded", ActiveState: "active", SubState: "running"},
	}

	// Search "web" — should match 2 units (by description)
	page, err := c.ListUnits(context.TODO(), ListParams{Search: "web"})
	if err != nil {
		t.Fatalf("ListUnits search: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 results for 'web', got %d", len(page.Entries))
	}

	// Search "web" with pagination limit=1
	page, err = c.ListUnits(context.TODO(), ListParams{Search: "web", Limit: 1})
	if err != nil {
		t.Fatalf("ListUnits search+limit: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}
	if page.TotalCount != 2 {
		t.Fatalf("expected total_count=2, got %d", page.TotalCount)
	}
}

// --- HTTP endpoint: ListFilesystems pagination with sort (no search) ---

func TestHTTPListFilesystemsPaginationWithSort(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{"zeta", "alpha", "middle"} {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	// Sort ascending + limit=2
	fsResult, err := c.ListFilesystems(context.TODO(), "", "", ListParams{SortBy: "name", SortOrder: "asc", Limit: 2})
	if err != nil {
		t.Fatalf("ListFilesystems sort+page: %v", err)
	}
	if len(fsResult.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(fsResult.Entries))
	}
	if !fsResult.HasMore {
		t.Fatal("expected has_more=true")
	}
	if fsResult.Entries[0].Name != "alpha" {
		t.Fatalf("expected first entry alpha, got %q", fsResult.Entries[0].Name)
	}
	if fsResult.Entries[1].Name != "middle" {
		t.Fatalf("expected second entry middle, got %q", fsResult.Entries[1].Name)
	}

	// Page 2
	fsResult, err = c.ListFilesystems(context.TODO(), "", "", ListParams{SortBy: "name", SortOrder: "asc", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListFilesystems sort+page2: %v", err)
	}
	if len(fsResult.Entries) != 1 {
		t.Fatalf("expected 1 entry on page 2, got %d", len(fsResult.Entries))
	}
	if fsResult.HasMore {
		t.Fatal("expected has_more=false on last page")
	}
	if fsResult.Entries[0].Name != "zeta" {
		t.Fatalf("expected zeta, got %q", fsResult.Entries[0].Name)
	}
}

// --- HTTP endpoint: ListFilesystems TotalPages and TotalCount ---

func TestHTTPListFilesystemsTotalPagesAndCount(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{"a", "b", "c", "d", "e"} {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	// With limit=2, 5 items => 3 pages
	fsResult, err := c.ListFilesystems(context.TODO(), "", "", ListParams{Limit: 2})
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}
	if fsResult.TotalPages != 3 {
		t.Fatalf("expected 3 total pages, got %d", fsResult.TotalPages)
	}
	if fsResult.TotalCount != 5 {
		t.Fatalf("expected total_count=5, got %d", fsResult.TotalCount)
	}
}

// --- HTTP endpoint: ListRepositories search case-insensitive ---

func TestHTTPListRepositoriesSearchCaseInsensitive(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, _ := url.Parse("https://example.com/MyRepo.git")
	rr.Items = []packages.Repository{
		{Name: "MyRepo", URL: *u},
	}

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	for _, term := range []string{"myrepo", "MYREPO", "MyRepo", "mYrEpO"} {
		page, err := c.ListRepositories(context.TODO(), ListParams{Search: term})
		if err != nil {
			t.Fatalf("ListRepositories search %q: %v", term, err)
		}
		if len(page.Entries) != 1 {
			t.Fatalf("expected 1 result for search %q, got %d", term, len(page.Entries))
		}
	}
}

// --- HTTP endpoint: ListPackages case-insensitive search ---

func TestHTTPListPackagesSearchCaseInsensitive(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, _ := url.Parse("https://example.com/repo.git")
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}
	writeTestPackage(t, rr.BaseDir, "repo", "MyApp", "1.0", "image: myapp:1.0\n")

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	for _, term := range []string{"myapp", "MYAPP", "MyApp"} {
		page, err := c.ListPackages(context.TODO(), ListParams{Search: term})
		if err != nil {
			t.Fatalf("ListPackages search %q: %v", term, err)
		}
		if len(page.Entries) != 1 {
			t.Fatalf("expected 1 result for search %q, got %d", term, len(page.Entries))
		}
	}
}

// --- HTTP endpoint: ListInstalled case-insensitive search ---

func TestHTTPListInstalledSearchCaseInsensitive(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, _ := url.Parse("https://example.com/repo.git")
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}
	writeTestPackage(t, rr.BaseDir, "repo", "nginx", "1.0", "image: nginx:1.0\n")

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	for _, term := range []string{"nginx", "NGINX", "Nginx"} {
		page, err := c.ListInstalled(context.TODO(), ListParams{Search: term})
		if err != nil {
			t.Fatalf("ListInstalled search %q: %v", term, err)
		}
		if len(page.Entries) != 1 {
			t.Fatalf("expected 1 result for search %q, got %d", term, len(page.Entries))
		}
	}
}

// --- HTTP endpoint: ListFilesystems search case-insensitive ---

func TestHTTPListFilesystemsSearchCaseInsensitive(t *testing.T) {
	c, _ := initTestClient(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "MyData"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	for _, term := range []string{"mydata", "MYDATA", "MyData"} {
		fsResult, err := c.ListFilesystems(context.TODO(), "", "", ListParams{Search: term})
		if err != nil {
			t.Fatalf("ListFilesystems search %q: %v", term, err)
		}
		if len(fsResult.Entries) != 1 {
			t.Fatalf("expected 1 result for search %q, got %d", term, len(fsResult.Entries))
		}
	}
}

// --- Empty results across all paginated endpoints ---

func TestHTTPListRepositoriesEmptySearch(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, _ := url.Parse("https://example.com/repo.git")
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	page, err := c.ListRepositories(context.TODO(), ListParams{Search: "nonexistent"})
	if err != nil {
		t.Fatalf("ListRepositories search: %v", err)
	}
	if len(page.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false for empty results")
	}
	if page.TotalCount != 0 {
		t.Fatalf("expected total_count=0, got %d", page.TotalCount)
	}
}

func TestHTTPListFilesystemsEmptySearch(t *testing.T) {
	c, _ := initTestClient(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "myfs"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	fsResult, err := c.ListFilesystems(context.TODO(), "", "", ListParams{Search: "nonexistent"})
	if err != nil {
		t.Fatalf("ListFilesystems search: %v", err)
	}
	if len(fsResult.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(fsResult.Entries))
	}
	if fsResult.HasMore {
		t.Fatal("expected has_more=false for empty results")
	}
}

func TestHTTPListAccountsEmptySearch(t *testing.T) {
	c, _ := initAccountTestClient(t)

	result, err := c.ListAccounts(context.TODO(), ListParams{Search: "nonexistent"})
	if err != nil {
		t.Fatalf("ListAccounts search: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(result.Entries))
	}
	if result.HasMore {
		t.Fatal("expected has_more=false for empty results")
	}
	if result.TotalCount != 0 {
		t.Fatalf("expected total_count=0, got %d", result.TotalCount)
	}
}

// --- Default limit (0) returns all items across endpoints ---

func TestHTTPListPackagesDefaultLimit(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, _ := url.Parse("https://example.com/repo.git")
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}

	for _, name := range []string{"a", "b", "c", "d", "e"} {
		writeTestPackage(t, rr.BaseDir, "repo", name, "1.0", fmt.Sprintf("image: %s:1.0\n", name))
	}

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Default (no limit) should return all 5
	page, err := c.ListPackages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}
	if len(page.Entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false with default limit")
	}
}

func TestHTTPListAccountsDefaultLimit(t *testing.T) {
	c, _ := initAccountTestClient(t)

	for i := range 5 {
		name := fmt.Sprintf("user%d", i)
		if _, err := c.CreateAccount(context.TODO(), name, "password1", name+"@test.com", "555", name, false); err != nil {
			t.Fatalf("CreateAccount %q: %v", name, err)
		}
	}

	// Default (no limit) should return all 6 (testadmin + 5)
	result, err := c.ListAccounts(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(result.Entries) != 6 {
		t.Fatalf("expected 6 entries, got %d", len(result.Entries))
	}
	if result.HasMore {
		t.Fatal("expected has_more=false with default limit")
	}
}

// --- ListPackages combined sort + pagination ---

func TestHTTPListPackagesSortWithPagination(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, _ := url.Parse("https://example.com/repo.git")
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo", "zeta", "1.0", "image: zeta:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "alpha", "1.0", "image: alpha:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "middle", "1.0", "image: middle:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "beta", "1.0", "image: beta:1.0\n")

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Sort asc + limit=2
	page, err := c.ListPackages(context.TODO(), ListParams{SortBy: "name", SortOrder: "asc", Limit: 2})
	if err != nil {
		t.Fatalf("ListPackages sort+page: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if page.Entries[0].Name != "alpha" {
		t.Fatalf("expected first entry alpha, got %q", page.Entries[0].Name)
	}
	if page.Entries[1].Name != "beta" {
		t.Fatalf("expected second entry beta, got %q", page.Entries[1].Name)
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}
	if page.TotalPages != 2 {
		t.Fatalf("expected 2 total pages, got %d", page.TotalPages)
	}

	// Page 2
	page, err = c.ListPackages(context.TODO(), ListParams{SortBy: "name", SortOrder: "asc", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListPackages sort+page2: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries on page 2, got %d", len(page.Entries))
	}
	if page.Entries[0].Name != "middle" {
		t.Fatalf("expected middle, got %q", page.Entries[0].Name)
	}
	if page.Entries[1].Name != "zeta" {
		t.Fatalf("expected zeta, got %q", page.Entries[1].Name)
	}
	if page.HasMore {
		t.Fatal("expected has_more=false on last page")
	}
}

// --- Sort by different fields on accounts ---

func TestHTTPListAccountsSortByEmail(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "user-z", "password1", "z@test.com", "555", "Zeta", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if _, err := c.CreateAccount(context.TODO(), "user-a", "password1", "a@test.com", "555", "Alpha", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	result, err := c.ListAccounts(context.TODO(), ListParams{SortBy: "email", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListAccounts sort by email: %v", err)
	}

	// Find the two test users (skip testadmin)
	var emails []string
	for _, acct := range result.Entries {
		emails = append(emails, acct.Email)
	}

	// Verify ascending order
	for i := 1; i < len(emails); i++ {
		if emails[i] < emails[i-1] {
			t.Fatalf("expected ascending email order, but %q < %q", emails[i], emails[i-1])
		}
	}
}

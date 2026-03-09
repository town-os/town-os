// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/systemd"
)

func TestPaginateHelper(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e"}

	// Default limit (20) returns all
	p := paginate(items, 0, 0)
	if len(p.Entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(p.Entries))
	}
	if p.HasMore {
		t.Fatal("expected has_more=false")
	}
	if p.TotalPages != 1 {
		t.Fatalf("expected 1 total page, got %d", p.TotalPages)
	}
	if p.TotalCount != 5 {
		t.Fatalf("expected total_count=5, got %d", p.TotalCount)
	}

	// Limit=2, offset=0
	p = paginate(items, 2, 0)
	if len(p.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(p.Entries))
	}
	if !p.HasMore {
		t.Fatal("expected has_more=true")
	}
	if p.TotalPages != 3 {
		t.Fatalf("expected 3 total pages, got %d", p.TotalPages)
	}
	if p.TotalCount != 5 {
		t.Fatalf("expected total_count=5 with pagination, got %d", p.TotalCount)
	}
	if p.Entries[0] != "a" || p.Entries[1] != "b" {
		t.Fatalf("unexpected entries: %v", p.Entries)
	}

	// Limit=2, offset=4
	p = paginate(items, 2, 4)
	if len(p.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(p.Entries))
	}
	if p.HasMore {
		t.Fatal("expected has_more=false")
	}

	// Offset beyond end
	p = paginate(items, 2, 100)
	if len(p.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(p.Entries))
	}
	if p.HasMore {
		t.Fatal("expected has_more=false")
	}

	// Negative offset clamped to 0
	p = paginate(items, 2, -5)
	if len(p.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(p.Entries))
	}

	// Empty slice
	p = paginate([]string{}, 10, 0)
	if len(p.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(p.Entries))
	}
	if p.HasMore {
		t.Fatal("expected has_more=false for empty")
	}
	if p.TotalPages != 1 {
		t.Fatalf("expected 1 total page for empty, got %d", p.TotalPages)
	}
}

func TestListParamsQueryString(t *testing.T) {
	// Empty
	p := ListParams{}
	if qs := p.QueryString(); qs != "" {
		t.Fatalf("expected empty query string, got %q", qs)
	}

	// Sort only
	p = ListParams{SortBy: "name", SortOrder: "desc"}
	qs := p.QueryString()
	if qs == "" {
		t.Fatal("expected non-empty query string")
	}

	// Full params
	p = ListParams{SortBy: "name", SortOrder: "asc", Limit: 10, Offset: 20}
	qs = p.QueryString()
	if qs == "" {
		t.Fatal("expected non-empty query string")
	}

	// With search
	p = ListParams{Search: "nginx"}
	qs = p.QueryString()
	if qs == "" {
		t.Fatal("expected non-empty query string for search")
	}
	if !strings.Contains(qs, "search=nginx") {
		t.Fatalf("expected search=nginx in query string, got %q", qs)
	}
}

func TestFilterSearchStrings(t *testing.T) {
	items := []string{"mock-repo/nginx@1.0", "mock-repo/redis@7.0", "mock-repo/postgres@16.0", "mock-repo/nginx@2.0"}

	// Match
	result := filterSearch(items, "nginx")
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}

	// Case insensitive
	result = filterSearch(items, "REDIS")
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}

	// No match
	result = filterSearch(items, "mysql")
	if len(result) != 0 {
		t.Fatalf("expected 0 results, got %d", len(result))
	}

	// Empty search returns all
	result = filterSearch(items, "")
	if len(result) != 4 {
		t.Fatalf("expected 4 results, got %d", len(result))
	}
}

func TestFilterSearchStructs(t *testing.T) {
	units := []systemd.UnitStatus{
		{Name: "nginx.service", Description: "NGINX web server", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "redis.service", Description: "Redis cache", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "postgres.service", Description: "PostgreSQL database", LoadState: "loaded", ActiveState: "failed", SubState: "dead"},
	}

	// Match by name
	result := filterSearch(units, "nginx")
	if len(result) != 1 {
		t.Fatalf("expected 1 result for 'nginx', got %d", len(result))
	}
	if result[0].Name != "nginx.service" {
		t.Fatalf("expected nginx.service, got %s", result[0].Name)
	}

	// Match by description
	result = filterSearch(units, "database")
	if len(result) != 1 {
		t.Fatalf("expected 1 result for 'database', got %d", len(result))
	}
	if result[0].Name != "postgres.service" {
		t.Fatalf("expected postgres.service, got %s", result[0].Name)
	}

	// Match by state
	result = filterSearch(units, "failed")
	if len(result) != 1 {
		t.Fatalf("expected 1 result for 'failed', got %d", len(result))
	}

	// Case insensitive
	result = filterSearch(units, "REDIS")
	if len(result) != 1 {
		t.Fatalf("expected 1 result for 'REDIS', got %d", len(result))
	}

	// Partial match across multiple results
	result = filterSearch(units, "service")
	if len(result) != 3 {
		t.Fatalf("expected 3 results for 'service', got %d", len(result))
	}

	// No match
	result = filterSearch(units, "mysql")
	if len(result) != 0 {
		t.Fatalf("expected 0 results for 'mysql', got %d", len(result))
	}
}

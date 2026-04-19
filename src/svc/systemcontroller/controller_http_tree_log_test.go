// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// Every tree-log test shares the same gitea/postgres dep fixture the flat
// unit-tree tests use: one parent, one dep, the canonical shape. The
// handler walks `LoadDependencies` server-side, so the fixture needs a
// matching install record and dep record — the journal-entry fixture
// itself can stay unit-agnostic since the mock manager does not filter.

func treeLogFixture(t *testing.T) (*SystemdClient, *systemd.MockManager) {
	t.Helper()
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "core", Name: "gitea", Version: "1.0"},
		{Repo: "core", Name: "gitea--dep--postgres", Version: "15.0"},
	}
	inst.Dependencies["core/gitea"] = map[string]packages.DependencyRecord{
		"db": {EffectiveName: "gitea--dep--postgres", Package: "postgres", Repo: "core", Version: "15.0"},
	}
	return c, sd
}

// --- LogTailTree: unit expansion ---

func TestHTTPLogTailTreeExpandsDependenciesToUnitMatches(t *testing.T) {
	c, sd := treeLogFixture(t)

	if _, err := c.LogTailTree(context.TODO(), "core", "gitea", "1.0", systemd.LogTailParams{Lines: 10}); err != nil {
		t.Fatalf("LogTailTree: %v", err)
	}

	p := lastLogTailParams(t, sd)
	want := []string{
		"town-os-package--core-gitea--dep--postgres-15.0.service",
		"town-os-package--core-gitea-1.0.service",
	}
	if !reflect.DeepEqual(p.Units, want) {
		t.Fatalf("Units = %v, want %v (leaves-first order)", p.Units, want)
	}
	if p.Unit != "" {
		t.Errorf("Unit should be empty on tree tail, got %q", p.Unit)
	}
}

func TestHTTPLogTailTreeStandalonePackageSingleUnit(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)
	inst.Installed = []packages.PackageIdentity{{Repo: "repo", Name: "nginx", Version: "1.0"}}

	if _, err := c.LogTailTree(context.TODO(), "repo", "nginx", "1.0", systemd.LogTailParams{Lines: 10}); err != nil {
		t.Fatalf("LogTailTree: %v", err)
	}

	p := lastLogTailParams(t, sd)
	if !reflect.DeepEqual(p.Units, []string{"town-os-package--repo-nginx-1.0.service"}) {
		t.Fatalf("Units = %v, want single parent unit", p.Units)
	}
}

func TestHTTPLogTailTreeNestedThreeLevels(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "core", Name: "app", Version: "1.0"},
		{Repo: "core", Name: "app--dep--db", Version: "15.0"},
		{Repo: "core", Name: "app--dep--db--dep--backup", Version: "2.0"},
	}
	inst.Dependencies["core/app"] = map[string]packages.DependencyRecord{
		"db": {EffectiveName: "app--dep--db", Package: "db", Repo: "core", Version: "15.0"},
	}
	inst.Dependencies["core/app--dep--db"] = map[string]packages.DependencyRecord{
		"backup": {EffectiveName: "app--dep--db--dep--backup", Package: "backup", Repo: "core", Version: "2.0"},
	}

	if _, err := c.LogTailTree(context.TODO(), "core", "app", "1.0", systemd.LogTailParams{Lines: 10}); err != nil {
		t.Fatalf("LogTailTree: %v", err)
	}

	p := lastLogTailParams(t, sd)
	want := []string{
		"town-os-package--core-app--dep--db--dep--backup-2.0.service",
		"town-os-package--core-app--dep--db-15.0.service",
		"town-os-package--core-app-1.0.service",
	}
	if !reflect.DeepEqual(p.Units, want) {
		t.Fatalf("Units = %v, want %v (leaves-first)", p.Units, want)
	}
}

func TestHTTPLogTailTreeMissingPackagePassesRootUnitOnly(t *testing.T) {
	// An uninstalled root has no dependency records, so collectTreeUnits
	// returns a single-element list with the root unit name. The handler
	// must still route through LogTail with that scoped Units slice —
	// never a zero-length list (which would leak system-wide logs
	// through sdjournal).
	c, sd := treeLogFixture(t)

	if _, err := c.LogTailTree(context.TODO(), "core", "nonexistent", "1.0", systemd.LogTailParams{Lines: 10}); err != nil {
		t.Fatalf("LogTailTree: %v", err)
	}
	p := lastLogTailParams(t, sd)
	if !reflect.DeepEqual(p.Units, []string{"town-os-package--core-nonexistent-1.0.service"}) {
		t.Fatalf("Units = %v, want single root-only unit", p.Units)
	}
}

// --- LogTailTree: filter passthrough ---

func TestHTTPLogTailTreePassesFilters(t *testing.T) {
	c, sd := treeLogFixture(t)

	since := time.Unix(1_700_000_000, 0)
	until := time.Unix(1_700_003_600, 0)
	if _, err := c.LogTailTree(context.TODO(), "core", "gitea", "1.0", systemd.LogTailParams{
		Lines:        42,
		Grep:         "error",
		Priority:     3,
		Since:        since,
		Until:        until,
		BeforeCursor: "cur-before",
		AfterCursor:  "cur-after",
	}); err != nil {
		t.Fatalf("LogTailTree: %v", err)
	}

	p := lastLogTailParams(t, sd)
	if p.Lines != 42 {
		t.Errorf("Lines = %d, want 42", p.Lines)
	}
	if p.Grep != "error" {
		t.Errorf("Grep = %q, want error", p.Grep)
	}
	if p.Priority != 3 {
		t.Errorf("Priority = %d, want 3", p.Priority)
	}
	if !p.Since.Equal(since) {
		t.Errorf("Since = %v, want %v", p.Since, since)
	}
	if !p.Until.Equal(until) {
		t.Errorf("Until = %v, want %v", p.Until, until)
	}
	if p.BeforeCursor != "cur-before" {
		t.Errorf("BeforeCursor = %q", p.BeforeCursor)
	}
	if p.AfterCursor != "cur-after" {
		t.Errorf("AfterCursor = %q", p.AfterCursor)
	}
}

// --- LogTailTree: validation ---

func TestHTTPLogTailTreeRequiresAllIdentityFields(t *testing.T) {
	c, _ := treeLogFixture(t)

	cases := []struct {
		repo, name, version string
	}{
		{"", "gitea", "1.0"},
		{"core", "", "1.0"},
		{"core", "gitea", ""},
	}
	for _, m := range cases {
		if _, err := c.LogTailTree(context.TODO(), m.repo, m.name, m.version, systemd.LogTailParams{Lines: 10}); err == nil {
			t.Errorf("expected error for missing identity field: %+v", m)
		}
	}
}

// --- LogTailTree: returns entries ---

func TestHTTPLogTailTreeReturnsJournalEntries(t *testing.T) {
	c, sd := treeLogFixture(t)
	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "gitea started", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c2", Message: "postgres ready", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c3", Message: "gitea connected to postgres", RealtimeTimestamp: now},
	}

	result, err := c.LogTailTree(context.TODO(), "core", "gitea", "1.0", systemd.LogTailParams{Lines: 100})
	if err != nil {
		t.Fatalf("LogTailTree: %v", err)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result.Entries))
	}
}

// --- LogReplayTree: SSE stream & unit expansion ---

func TestHTTPLogReplayTreeStreamsMergedEntries(t *testing.T) {
	c, sd := treeLogFixture(t)
	sd.Entries = []systemd.JournalEntry{
		{Message: "gitea log line"},
		{Message: "postgres log line"},
	}

	ch, err := c.LogReplayTree(context.TODO(), "core", "gitea", "1.0")
	if err != nil {
		t.Fatalf("LogReplayTree: %v", err)
	}

	var out []systemd.JournalEntry
	for e := range ch {
		out = append(out, e)
		if len(out) >= 2 {
			break
		}
	}
	if len(out) < 2 {
		t.Fatalf("expected at least 2 entries in tree replay, got %d", len(out))
	}

	// Confirm the server expanded to the full unit list.
	var recorded []string
	for _, call := range sd.GetCalls() {
		if call.Method != "LogReplay" {
			continue
		}
		if v, ok := call.Args[0].([]string); ok {
			recorded = v
		}
	}
	want := []string{
		"town-os-package--core-gitea--dep--postgres-15.0.service",
		"town-os-package--core-gitea-1.0.service",
	}
	if !reflect.DeepEqual(recorded, want) {
		t.Fatalf("LogReplay recorded Units = %v, want %v", recorded, want)
	}
}

func TestHTTPLogReplayTreeRejectsMissingIdentity(t *testing.T) {
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, Systemd: sd, Installer: inst})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet,
		ts.Server.URL+"/systemd/logs/tree?repo=core&name=&version=1.0", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", resp.StatusCode)
	}
}

// TestHTTPLogReplayTreeHeartbeatWhenEmpty proves the SSE stream stays
// open (via heartbeat lines) when the underlying replay has nothing to
// emit yet. The single-unit handler has the same pattern; the tree
// handler should match it so idle group-log views don't thrash the
// client's fetch().
func TestHTTPLogReplayTreeHeartbeatWhenEmpty(t *testing.T) {
	c, _ := treeLogFixture(t)

	// Raw HTTP to read SSE frames rather than JournalEntry JSON.
	q := url.Values{}
	q.Set("repo", "core")
	q.Set("name", "gitea")
	q.Set("version", "1.0")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/systemd/logs/tree?"+q.Encode(), nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream prefix", ct)
	}

	// Read until we see a heartbeat colon line or the context expires.
	sawHeartbeat := make(chan struct{})
	go func() {
		defer close(sawHeartbeat)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if line == ":" {
				return
			}
		}
	}()

	select {
	case <-sawHeartbeat:
	case <-time.After(2500 * time.Millisecond):
		t.Fatal("did not receive SSE heartbeat within 2.5s")
	}
}

// --- helpers ---

// lastLogTailParams pulls the most recent LogTail call off the mock and
// asserts on its LogTailParams arg. Every tree-tail test asserts on the
// same shape so factoring the boilerplate keeps the individual cases
// focused on the field they're pinning.
func lastLogTailParams(t *testing.T, sd *systemd.MockManager) systemd.LogTailParams {
	t.Helper()
	calls := sd.GetCalls()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].Method != "LogTail" {
			continue
		}
		p, ok := calls[i].Args[0].(systemd.LogTailParams)
		if !ok {
			t.Fatalf("LogTail arg 0 = %T, want LogTailParams", calls[i].Args[0])
		}
		return p
	}
	t.Fatal("no LogTail call recorded")
	return systemd.LogTailParams{}
}

// readSSEData is a convenience for raw SSE tests — not used by the
// tree-log cases (they go through the typed client), but kept here so
// future regressions have a starting point without re-implementing the
// stream parser.
var _ = readSSEData

func readSSEData(r io.Reader) []string {
	scanner := bufio.NewScanner(r)
	var out []string
	for scanner.Scan() {
		line := scanner.Text()
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			out = append(out, data)
		}
	}
	return out
}

// Guard against accidental coupling: if `json.Marshal` of the
// JournalEntry ever starts using a stable field name different from
// "message", the SSE client will silently drop frames. This trivial
// round-trip pins the shape.
func TestJournalEntryJSONShapeForSSE(t *testing.T) {
	b, err := json.Marshal(systemd.JournalEntry{Message: "hi"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(b), `"Message":"hi"`) {
		t.Fatalf("JournalEntry JSON changed: %s", b)
	}
}

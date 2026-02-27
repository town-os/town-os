package systemcontroller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

func TestHTTPLogReplay(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Message: "entry one", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Message: "entry two", RealtimeTimestamp: now.Add(-time.Second)},
		{Message: "entry three", RealtimeTimestamp: now},
	}

	ch, err := c.LogReplay(context.TODO(), "test.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	var entries []systemd.JournalEntry
	for e := range ch {
		entries = append(entries, e)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	if entries[0].Message != "entry one" {
		t.Fatalf("expected first message %q, got %q", "entry one", entries[0].Message)
	}
	if entries[2].Message != "entry three" {
		t.Fatalf("expected third message %q, got %q", "entry three", entries[2].Message)
	}
}

func TestHTTPLogReplayEmpty(t *testing.T) {
	c, _, _ := initSystemdTestClient(t)

	ch, err := c.LogReplay(context.TODO(), "test.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	var count int
	for range ch {
		count++
	}

	if count != 0 {
		t.Fatalf("expected 0 entries, got %d", count)
	}
}

func TestHTTPLogReplayError(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	sd.LogErr = errors.New("injected log error")

	_, err := c.LogReplay(context.TODO(), "test.service")
	if err == nil {
		t.Fatal("expected error from LogReplay with injected error")
	}
}

func TestHTTPLogReplayEmptyUnit(t *testing.T) {
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	ts := InitTestServer(ServerConfig{Storage: mock, Systemd: sd})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, fmt.Sprintf("%s/systemd/logs", ts.Server.URL), nil) //nolint:perfsprint // project convention
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for system-wide log replay (empty unit), got %d", resp.StatusCode)
	}
}

func TestHTTPLogTail(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "first", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c2", Message: "second", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "third", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "fourth", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c5", Message: "fifth", RealtimeTimestamp: now},
	}

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 3})
	if err != nil {
		t.Fatalf("LogTail: %v", err)
	}

	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "third" {
		t.Fatalf("expected first entry %q, got %q", "third", result.Entries[0].Message)
	}
	if result.Entries[2].Message != "fifth" {
		t.Fatalf("expected last entry %q, got %q", "fifth", result.Entries[2].Message)
	}

	if result.Cursor != "c3" {
		t.Fatalf("expected cursor %q, got %q", "c3", result.Cursor)
	}
	if result.EndCursor != "c5" {
		t.Fatalf("expected end_cursor %q, got %q", "c5", result.EndCursor)
	}
}

func TestHTTPLogTailWithCursor(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "first", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c2", Message: "second", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "third", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "fourth", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c5", Message: "fifth", RealtimeTimestamp: now},
	}

	// Get entries before cursor c3 (should get c1, c2)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, BeforeCursor: "c3"})
	if err != nil {
		t.Fatalf("LogTail with cursor: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries before c3, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "first" {
		t.Fatalf("expected first entry %q, got %q", "first", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "second" {
		t.Fatalf("expected second entry %q, got %q", "second", result.Entries[1].Message)
	}
}

func TestHTTPLogTailEmpty(t *testing.T) {
	c, _, _ := initSystemdTestClient(t)

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100})
	if err != nil {
		t.Fatalf("LogTail: %v", err)
	}

	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(result.Entries))
	}
}

func TestHTTPLogTailError(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	sd.LogErr = errors.New("injected log error")

	_, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100})
	if err == nil {
		t.Fatal("expected error from LogTail with injected error")
	}
}

func TestHTTPLogTailEmptyUnit(t *testing.T) {
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	ts := InitTestServer(ServerConfig{Storage: mock, Systemd: sd})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, fmt.Sprintf("%s/systemd/logs/tail", ts.Server.URL), nil) //nolint:perfsprint // project convention
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for system-wide log tail (empty unit), got %d", resp.StatusCode)
	}
}

func TestHTTPLogTailGrep(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "starting nginx", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c2", Message: "connection from 10.0.0.1", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "error: upstream timeout", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "connection from 10.0.0.2", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c5", Message: "stopping nginx", RealtimeTimestamp: now},
	}

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Grep: "connection"})
	if err != nil {
		t.Fatalf("LogTail with grep: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries matching 'connection', got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "connection from 10.0.0.1" {
		t.Fatalf("expected first match %q, got %q", "connection from 10.0.0.1", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "connection from 10.0.0.2" {
		t.Fatalf("expected second match %q, got %q", "connection from 10.0.0.2", result.Entries[1].Message)
	}
}

func TestHTTPLogTailGrepCaseInsensitive(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "ERROR: something failed", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c2", Message: "info: all good", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c3", Message: "error: another failure", RealtimeTimestamp: now},
	}

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Grep: "error"})
	if err != nil {
		t.Fatalf("LogTail with grep: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries matching 'error' (case-insensitive), got %d", len(result.Entries))
	}
}

func TestHTTPLogTailGrepNoMatch(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "hello world", RealtimeTimestamp: now},
	}

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Grep: "nonexistent"})
	if err != nil {
		t.Fatalf("LogTail with grep: %v", err)
	}

	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries for non-matching grep, got %d", len(result.Entries))
	}
}

func TestHTTPLogTailAfterCursor(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "first", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c2", Message: "second", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "third", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "fourth", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c5", Message: "fifth", RealtimeTimestamp: now},
	}

	// Get entries after cursor c2 (should get c3, c4, c5)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, AfterCursor: "c2"})
	if err != nil {
		t.Fatalf("LogTail after cursor: %v", err)
	}

	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries after c2, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "third" {
		t.Fatalf("expected first entry %q, got %q", "third", result.Entries[0].Message)
	}
	if result.Entries[2].Message != "fifth" {
		t.Fatalf("expected last entry %q, got %q", "fifth", result.Entries[2].Message)
	}

	if result.Cursor != "c3" {
		t.Fatalf("expected cursor %q, got %q", "c3", result.Cursor)
	}
	if result.EndCursor != "c5" {
		t.Fatalf("expected end_cursor %q, got %q", "c5", result.EndCursor)
	}
}

func TestHTTPLogTailAfterCursorEmpty(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "first", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c2", Message: "second", RealtimeTimestamp: now},
	}

	// Get entries after last cursor (should be empty)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, AfterCursor: "c2"})
	if err != nil {
		t.Fatalf("LogTail after cursor: %v", err)
	}

	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries after last cursor, got %d", len(result.Entries))
	}
}

func TestHTTPLogTailAfterCursorWithLimit(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "first", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c2", Message: "second", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "third", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "fourth", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c5", Message: "fifth", RealtimeTimestamp: now},
	}

	// Get at most 2 entries after cursor c1
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 2, AfterCursor: "c1"})
	if err != nil {
		t.Fatalf("LogTail after cursor with limit: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "second" {
		t.Fatalf("expected first entry %q, got %q", "second", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "third" {
		t.Fatalf("expected second entry %q, got %q", "third", result.Entries[1].Message)
	}

	if result.EndCursor != "c3" {
		t.Fatalf("expected end_cursor %q, got %q", "c3", result.EndCursor)
	}
}

func TestHTTPLogTailAfterCursorWithGrep(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "start", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c2", Message: "error: disk full", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "info: ok", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "error: timeout", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c5", Message: "done", RealtimeTimestamp: now},
	}

	// Get entries after c1 matching "error"
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, AfterCursor: "c1", Grep: "error"})
	if err != nil {
		t.Fatalf("LogTail after cursor with grep: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries matching grep, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "error: disk full" {
		t.Fatalf("expected first entry %q, got %q", "error: disk full", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "error: timeout" {
		t.Fatalf("expected second entry %q, got %q", "error: timeout", result.Entries[1].Message)
	}
}

func TestHTTPLogTailSince(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "old entry", RealtimeTimestamp: now.Add(-10 * time.Second)},
		{Cursor: "c2", Message: "also old", RealtimeTimestamp: now.Add(-8 * time.Second)},
		{Cursor: "c3", Message: "recent one", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c4", Message: "newer one", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c5", Message: "newest", RealtimeTimestamp: now},
	}

	// Get entries since 5 seconds ago (should get c3, c4, c5)
	since := now.Add(-5 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Since: since})
	if err != nil {
		t.Fatalf("LogTail since: %v", err)
	}

	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries since cutoff, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "recent one" {
		t.Fatalf("expected first entry %q, got %q", "recent one", result.Entries[0].Message)
	}
	if result.Entries[2].Message != "newest" {
		t.Fatalf("expected last entry %q, got %q", "newest", result.Entries[2].Message)
	}

	if result.EndCursor != "c5" {
		t.Fatalf("expected end_cursor %q, got %q", "c5", result.EndCursor)
	}
}

func TestHTTPLogTailSinceWithGrep(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "error: old", RealtimeTimestamp: now.Add(-10 * time.Second)},
		{Cursor: "c2", Message: "info: recent", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "error: recent", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "info: newest", RealtimeTimestamp: now.Add(-time.Second)},
	}

	// Get entries since 5 seconds ago matching "error" (should get only c3)
	since := now.Add(-5 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Since: since, Grep: "error"})
	if err != nil {
		t.Fatalf("LogTail since with grep: %v", err)
	}

	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "error: recent" {
		t.Fatalf("expected entry %q, got %q", "error: recent", result.Entries[0].Message)
	}
}

func TestHTTPLogTailSinceEmpty(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "old", RealtimeTimestamp: now.Add(-10 * time.Second)},
	}

	// All entries are before 'since', should return empty
	since := now.Add(-5 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Since: since})
	if err != nil {
		t.Fatalf("LogTail since: %v", err)
	}

	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(result.Entries))
	}
}

func TestHTTPLogTailSinceWithLimit(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "old", RealtimeTimestamp: now.Add(-10 * time.Second)},
		{Cursor: "c2", Message: "a", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "b", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "c", RealtimeTimestamp: now.Add(-time.Second)},
	}

	// Get at most 2 entries since 5 seconds ago
	since := now.Add(-5 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 2, Since: since})
	if err != nil {
		t.Fatalf("LogTail since with limit: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "a" {
		t.Fatalf("expected first entry %q, got %q", "a", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "b" {
		t.Fatalf("expected second entry %q, got %q", "b", result.Entries[1].Message)
	}
}

func TestHTTPLogTailSinceUntil(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "before window", RealtimeTimestamp: now.Add(-10 * time.Second)},
		{Cursor: "c2", Message: "in window", RealtimeTimestamp: now.Add(-5 * time.Second)},
		{Cursor: "c3", Message: "also in window", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c4", Message: "after window", RealtimeTimestamp: now.Add(-1 * time.Second)},
		{Cursor: "c5", Message: "latest", RealtimeTimestamp: now},
	}

	// Window from -7s to -2s: should get c2 and c3
	since := now.Add(-7 * time.Second)
	until := now.Add(-2 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Since: since, Until: until})
	if err != nil {
		t.Fatalf("LogTail since+until: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries in window, got %d", len(result.Entries))
	}
	if result.Entries[0].Message != "in window" {
		t.Fatalf("expected first entry %q, got %q", "in window", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "also in window" {
		t.Fatalf("expected second entry %q, got %q", "also in window", result.Entries[1].Message)
	}
}

func TestHTTPLogTailSinceUntilEmpty(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "old entry", RealtimeTimestamp: now.Add(-10 * time.Second)},
		{Cursor: "c2", Message: "newer entry", RealtimeTimestamp: now.Add(-1 * time.Second)},
	}

	// Window that contains no entries: -8s to -5s
	since := now.Add(-8 * time.Second)
	until := now.Add(-5 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Since: since, Until: until})
	if err != nil {
		t.Fatalf("LogTail since+until empty: %v", err)
	}

	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries in window, got %d", len(result.Entries))
	}
}

func TestHTTPLogTailSinceUntilWithGrep(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "error: old", RealtimeTimestamp: now.Add(-10 * time.Second)},
		{Cursor: "c2", Message: "info: in window", RealtimeTimestamp: now.Add(-5 * time.Second)},
		{Cursor: "c3", Message: "error: in window", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c4", Message: "error: after window", RealtimeTimestamp: now.Add(-1 * time.Second)},
	}

	// Window from -7s to -2s matching "error": should get only c3
	since := now.Add(-7 * time.Second)
	until := now.Add(-2 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Since: since, Until: until, Grep: "error"})
	if err != nil {
		t.Fatalf("LogTail since+until+grep: %v", err)
	}

	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result.Entries))
	}
	if result.Entries[0].Message != "error: in window" {
		t.Fatalf("expected entry %q, got %q", "error: in window", result.Entries[0].Message)
	}
}

func TestHTTPLogTailUntilBeforeAllEntries(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "entry 1", RealtimeTimestamp: now.Add(-5 * time.Second)},
		{Cursor: "c2", Message: "entry 2", RealtimeTimestamp: now.Add(-3 * time.Second)},
	}

	// Since before all entries, until also before all entries
	since := now.Add(-20 * time.Second)
	until := now.Add(-10 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Since: since, Until: until})
	if err != nil {
		t.Fatalf("LogTail until before all: %v", err)
	}

	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(result.Entries))
	}
}

func TestHTTPLogTailSinceUntilWithLimit(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "old", RealtimeTimestamp: now.Add(-10 * time.Second)},
		{Cursor: "c2", Message: "a", RealtimeTimestamp: now.Add(-5 * time.Second)},
		{Cursor: "c3", Message: "b", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c4", Message: "c", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c5", Message: "after", RealtimeTimestamp: now.Add(-1 * time.Second)},
	}

	// Window from -7s to -2s with limit 2: should get c2 and c3
	since := now.Add(-7 * time.Second)
	until := now.Add(-2 * time.Second)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 2, Since: since, Until: until})
	if err != nil {
		t.Fatalf("LogTail since+until+limit: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result.Entries))
	}
	if result.Entries[0].Message != "a" {
		t.Fatalf("expected first %q, got %q", "a", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "b" {
		t.Fatalf("expected second %q, got %q", "b", result.Entries[1].Message)
	}
}

func TestHTTPLogTailPriority(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "debug msg", Priority: "7", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c2", Message: "info msg", Priority: "6", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "error msg", Priority: "3", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "critical msg", Priority: "2", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c5", Message: "warning msg", Priority: "4", RealtimeTimestamp: now},
	}

	// Priority 3 = error and above (emergency=0, alert=1, crit=2, err=3)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Priority: 3})
	if err != nil {
		t.Fatalf("LogTail with priority: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries with priority <= 3, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "error msg" {
		t.Fatalf("expected first entry %q, got %q", "error msg", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "critical msg" {
		t.Fatalf("expected second entry %q, got %q", "critical msg", result.Entries[1].Message)
	}
}

func TestHTTPLogTailPriorityNoFilter(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "debug msg", Priority: "7", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c2", Message: "error msg", Priority: "3", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c3", Message: "info msg", Priority: "6", RealtimeTimestamp: now},
	}

	// Priority 0 means no filter
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Priority: 0})
	if err != nil {
		t.Fatalf("LogTail with no priority filter: %v", err)
	}

	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries with no priority filter, got %d", len(result.Entries))
	}
}

func TestHTTPLogTailPriorityWithGrep(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "error: disk full", Priority: "3", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c2", Message: "info: disk ok", Priority: "6", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "error: disk timeout", Priority: "3", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "debug: disk check", Priority: "7", RealtimeTimestamp: now.Add(-time.Second)},
	}

	// Priority 3 + grep "disk full"
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Priority: 3, Grep: "disk full"})
	if err != nil {
		t.Fatalf("LogTail with priority+grep: %v", err)
	}

	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry matching priority+grep, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "error: disk full" {
		t.Fatalf("expected entry %q, got %q", "error: disk full", result.Entries[0].Message)
	}
}

func TestHTTPLogTailPriorityEmptyPriority(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "no priority", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c2", Message: "has priority", Priority: "3", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c3", Message: "high priority", Priority: "7", RealtimeTimestamp: now},
	}

	// Entries with empty priority should pass through (no priority field to filter on)
	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Priority: 3})
	if err != nil {
		t.Fatalf("LogTail with priority: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries (empty priority + priority 3), got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "no priority" {
		t.Fatalf("expected first entry %q, got %q", "no priority", result.Entries[0].Message)
	}
	if result.Entries[1].Message != "has priority" {
		t.Fatalf("expected second entry %q, got %q", "has priority", result.Entries[1].Message)
	}
}

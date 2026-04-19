// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemd

import (
	"context"
	"reflect"
	"testing"
)

// dedupeNonEmpty backs every multi-unit code path: it's what turns a
// caller's raw string list into the deterministic sdjournal match order.
// These tests pin the contract so callers can rely on the shape without
// sorting or deduping upstream.

func TestDedupeNonEmptyPreservesFirstSeenOrderAndDropsDupes(t *testing.T) {
	got := dedupeNonEmpty([]string{"b", "a", "b", "c", "a"})
	want := []string{"b", "a", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dedupeNonEmpty = %v, want %v", got, want)
	}
}

func TestDedupeNonEmptyFiltersEmptyStrings(t *testing.T) {
	got := dedupeNonEmpty([]string{"", "a", "", "b", ""})
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dedupeNonEmpty = %v, want %v", got, want)
	}
}

func TestDedupeNonEmptyReturnsEmptyForEmptyInput(t *testing.T) {
	// Nil and empty inputs return a length-zero result — callers range
	// over it so nil vs. [] is interchangeable. Pinning only the length
	// keeps the implementation free to preserve either shape.
	if got := dedupeNonEmpty(nil); len(got) != 0 {
		t.Fatalf("dedupeNonEmpty(nil) = %v, want empty", got)
	}
	if got := dedupeNonEmpty([]string{}); len(got) != 0 {
		t.Fatalf("dedupeNonEmpty([]) = %v, want empty", got)
	}
	if got := dedupeNonEmpty([]string{"", ""}); len(got) != 0 {
		t.Fatalf("dedupeNonEmpty(empties) = %v, want empty", got)
	}
}

// logTailUnits encodes the Units-vs-Unit precedence rule used by both the
// real manager and the HTTP controller. Changing its behavior silently
// would route multi-unit tree queries to the wrong journal match set.

func TestLogTailUnitsPrefersUnitsOverUnit(t *testing.T) {
	got := logTailUnits(LogTailParams{Unit: "ignored.service", Units: []string{"a.service", "b.service"}})
	want := []string{"a.service", "b.service"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("logTailUnits = %v, want %v", got, want)
	}
}

func TestLogTailUnitsFallsBackToUnitWhenUnitsEmpty(t *testing.T) {
	got := logTailUnits(LogTailParams{Unit: "only.service"})
	if !reflect.DeepEqual(got, []string{"only.service"}) {
		t.Fatalf("logTailUnits = %v, want [only.service]", got)
	}
}

func TestLogTailUnitsReturnsNilForSystemWideQuery(t *testing.T) {
	if got := logTailUnits(LogTailParams{}); got != nil {
		t.Fatalf("logTailUnits(empty) = %v, want nil", got)
	}
}

func TestLogTailUnitsDedupesExplicitUnitsList(t *testing.T) {
	got := logTailUnits(LogTailParams{Units: []string{"a", "a", "b"}})
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("logTailUnits dedupes = %v, want %v", got, want)
	}
}

// --- MockManager multi-unit LogReplay tests ---
//
// The mock does not actually filter entries by unit (the real journal
// does), so these assertions cover the recorded-call contract: callers
// rely on `calls[0].Args[0]` to verify which units a client reached out
// about. A single-unit call records a string; multi-unit records the
// slice; zero-unit (system-wide) records an empty string.

func TestMockManagerLogReplayRecordsSingleUnitAsString(t *testing.T) {
	m := InitMockManager()
	if _, err := m.LogReplay(context.Background(), "only.service"); err != nil {
		t.Fatalf("LogReplay: %v", err)
	}
	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	name, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatalf("expected string arg, got %T", calls[0].Args[0])
	}
	if name != "only.service" {
		t.Fatalf("recorded unit = %q, want only.service", name)
	}
}

func TestMockManagerLogReplayRecordsMultiUnitAsSlice(t *testing.T) {
	m := InitMockManager()
	if _, err := m.LogReplay(context.Background(), "a.service", "b.service", "c.service"); err != nil {
		t.Fatalf("LogReplay: %v", err)
	}
	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	names, ok := calls[0].Args[0].([]string)
	if !ok {
		t.Fatalf("expected []string arg, got %T", calls[0].Args[0])
	}
	want := []string{"a.service", "b.service", "c.service"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("recorded units = %v, want %v", names, want)
	}
}

func TestMockManagerLogReplayZeroUnitsRecordsEmptyString(t *testing.T) {
	m := InitMockManager()
	if _, err := m.LogReplay(context.Background()); err != nil {
		t.Fatalf("LogReplay: %v", err)
	}
	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	name, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatalf("expected string arg for zero-unit call, got %T", calls[0].Args[0])
	}
	if name != "" {
		t.Fatalf("expected empty string, got %q", name)
	}
}

func TestMockManagerLogTailAcceptsUnitsField(t *testing.T) {
	m := InitMockManager()
	m.Entries = []JournalEntry{
		{Cursor: "c1", Message: "entry one"},
		{Cursor: "c2", Message: "entry two"},
	}

	result, err := m.LogTail(context.Background(), LogTailParams{
		Units: []string{"a.service", "b.service"},
		Lines: 100,
	})
	if err != nil {
		t.Fatalf("LogTail with Units: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result.Entries))
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	p, ok := calls[0].Args[0].(LogTailParams)
	if !ok {
		t.Fatalf("expected LogTailParams arg, got %T", calls[0].Args[0])
	}
	if !reflect.DeepEqual(p.Units, []string{"a.service", "b.service"}) {
		t.Fatalf("recorded Units = %v, want [a.service b.service]", p.Units)
	}
}

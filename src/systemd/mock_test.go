package systemd

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// --- Interface conformance ---

func TestSystemdManagerImplementsManager(t *testing.T) {
	var _ Manager = (*SystemdManager)(nil)
}

func TestSystemdMockManagerImplementsManager(t *testing.T) {
	var _ Manager = (*MockManager)(nil)
}

// --- ListUnits tests ---

func TestSystemdMockManagerListUnits(t *testing.T) {
	m := InitMockManager()
	m.Units = []UnitStatus{
		{Name: "nginx.service", Description: "nginx", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "redis.service", Description: "redis", LoadState: "loaded", ActiveState: "inactive", SubState: "dead"},
	}

	units, err := m.ListUnits(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units))
	}

	if units[0].Name != "nginx.service" {
		t.Fatalf("expected name %q, got %q", "nginx.service", units[0].Name)
	}

	if units[1].ActiveState != "inactive" {
		t.Fatalf("expected active state %q, got %q", "inactive", units[1].ActiveState)
	}
}

func TestSystemdMockManagerListUnitsEmpty(t *testing.T) {
	m := InitMockManager()

	units, err := m.ListUnits(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 0 {
		t.Fatalf("expected 0 units, got %d", len(units))
	}
}

func TestSystemdMockManagerListUnitsReturnsCopy(t *testing.T) {
	m := InitMockManager()
	m.Units = []UnitStatus{
		{Name: "nginx.service"},
	}

	units, err := m.ListUnits(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	units[0].Name = "mutated"

	if m.Units[0].Name != "nginx.service" {
		t.Fatal("ListUnits should return a copy, not a reference")
	}
}

func TestSystemdMockManagerListUnitsErrorInjection(t *testing.T) {
	m := InitMockManager()
	injected := fmt.Errorf("injected error")

	m.ListErr = injected
	_, err := m.ListUnits(context.Background())
	if err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// --- SetStatus tests ---

func TestSystemdMockManagerSetStatusAllActions(t *testing.T) {
	for _, action := range []StatusAction{Start, Stop, Restart, Enable, Disable} {
		t.Run(string(action), func(t *testing.T) {
			m := InitMockManager()

			err := m.SetStatus(context.Background(), "nginx.service", action)
			if err != nil {
				t.Fatalf("unexpected error for action %q: %v", action, err)
			}
		})
	}
}

func TestSystemdMockManagerSetStatusInvalidAction(t *testing.T) {
	m := InitMockManager()

	err := m.SetStatus(context.Background(), "nginx.service", StatusAction("bogus"))
	if err == nil {
		t.Fatal("expected error for invalid action")
	}
	if !errors.Is(err, ErrInvalidAction) {
		t.Fatalf("expected ErrInvalidAction, got %v", err)
	}
}

func TestSystemdMockManagerSetStatusErrorInjection(t *testing.T) {
	m := InitMockManager()
	injected := fmt.Errorf("injected error")

	m.StatusErr = injected
	err := m.SetStatus(context.Background(), "nginx.service", Start)
	if err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestSystemdMockManagerSetStatusErrorInjectionTakesPrecedence(t *testing.T) {
	m := InitMockManager()
	injected := fmt.Errorf("injected error")

	m.StatusErr = injected
	err := m.SetStatus(context.Background(), "nginx.service", StatusAction("bogus"))
	if err != injected {
		t.Fatalf("expected injected error to take precedence over invalid action, got %v", err)
	}
}

func TestSystemdMockManagerSetStatusCallArgs(t *testing.T) {
	m := InitMockManager()

	if err := m.SetStatus(context.Background(), "nginx.service", Restart); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	if calls[0].Method != "SetStatus" {
		t.Fatalf("expected method %q, got %q", "SetStatus", calls[0].Method)
	}

	if len(calls[0].Args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(calls[0].Args))
	}

	unit, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatalf("expected string arg, got %T", calls[0].Args[0])
	}
	if unit != "nginx.service" {
		t.Fatalf("expected unit %q, got %q", "nginx.service", unit)
	}

	action, ok := calls[0].Args[1].(StatusAction)
	if !ok {
		t.Fatalf("expected StatusAction arg, got %T", calls[0].Args[1])
	}
	if action != Restart {
		t.Fatalf("expected action %q, got %q", Restart, action)
	}
}

// --- LogReplay tests ---

func TestSystemdMockManagerLogReplay(t *testing.T) {
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	m := InitMockManager()
	m.Entries = []JournalEntry{
		{Message: "started", RealtimeTimestamp: ts, SystemdUnit: "nginx.service"},
		{Message: "listening on :80", RealtimeTimestamp: ts.Add(time.Second), SystemdUnit: "nginx.service"},
	}

	ch, err := m.LogReplay(context.Background(), "nginx.service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var entries []JournalEntry
	for e := range ch {
		entries = append(entries, e)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].Message != "started" {
		t.Fatalf("expected message %q, got %q", "started", entries[0].Message)
	}

	if entries[1].Message != "listening on :80" {
		t.Fatalf("expected message %q, got %q", "listening on :80", entries[1].Message)
	}
}

func TestSystemdMockManagerLogReplayEmpty(t *testing.T) {
	m := InitMockManager()

	ch, err := m.LogReplay(context.Background(), "nginx.service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var entries []JournalEntry
	for e := range ch {
		entries = append(entries, e)
	}

	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestSystemdMockManagerLogReplayContextCancellation(t *testing.T) {
	m := InitMockManager()
	m.Entries = make([]JournalEntry, 1000)
	for i := range m.Entries {
		m.Entries[i] = JournalEntry{Message: fmt.Sprintf("entry %d", i)}
	}

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := m.LogReplay(ctx, "nginx.service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read a few entries then cancel.
	<-ch
	<-ch
	cancel()

	// Drain remaining entries; channel should close.
	count := 2
	for range ch {
		count++
	}

	if count >= 1000 {
		t.Fatal("expected context cancellation to stop replay early")
	}
}

func TestSystemdMockManagerLogReplayErrorInjection(t *testing.T) {
	m := InitMockManager()
	injected := fmt.Errorf("injected error")

	m.LogErr = injected
	_, err := m.LogReplay(context.Background(), "nginx.service")
	if err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestSystemdMockManagerLogReplayReturnsCopy(t *testing.T) {
	m := InitMockManager()
	m.Entries = []JournalEntry{
		{Message: "original"},
	}

	ch, err := m.LogReplay(context.Background(), "nginx.service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry := <-ch
	entry.Message = "mutated"

	if m.Entries[0].Message != "original" {
		t.Fatal("LogReplay should return copies, not references")
	}
}

// --- Call log tests ---

func TestSystemdMockManagerCallLog(t *testing.T) {
	m := InitMockManager()
	m.Units = []UnitStatus{{Name: "nginx.service"}}
	m.Entries = []JournalEntry{{Message: "hello"}}

	if _, err := m.ListUnits(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := m.SetStatus(context.Background(), "nginx.service", Start); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ch, err := m.LogReplay(context.Background(), "nginx.service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for range ch {
	}

	calls := m.GetCalls()
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}

	expected := []string{"ListUnits", "SetStatus", "LogReplay"}
	for i, want := range expected {
		if calls[i].Method != want {
			t.Fatalf("call %d: expected method %q, got %q", i, want, calls[i].Method)
		}
	}
}

func TestSystemdMockManagerCallLogReturnsCopy(t *testing.T) {
	m := InitMockManager()

	if _, err := m.ListUnits(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := m.GetCalls()
	calls[0].Method = "mutated"

	if m.Calls[0].Method != "ListUnits" {
		t.Fatal("GetCalls should return a copy, not a reference")
	}
}

func TestSystemdMockManagerListUnitsCallLogNilArgs(t *testing.T) {
	m := InitMockManager()

	if _, err := m.ListUnits(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := m.GetCalls()
	if calls[0].Args != nil {
		t.Fatalf("expected nil args for ListUnits, got %v", calls[0].Args)
	}
}

func TestSystemdMockManagerLogReplayCallLogArgs(t *testing.T) {
	m := InitMockManager()

	ch, err := m.LogReplay(context.Background(), "redis.service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for range ch {
	}

	calls := m.GetCalls()
	if len(calls[0].Args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(calls[0].Args))
	}

	unit, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatalf("expected string arg, got %T", calls[0].Args[0])
	}
	if unit != "redis.service" {
		t.Fatalf("expected unit %q, got %q", "redis.service", unit)
	}
}

// --- Lifecycle ---

func TestSystemdMockManagerLifecycle(t *testing.T) {
	m := InitMockManager()

	// Start with no units.
	units, err := m.ListUnits(context.Background())
	if err != nil {
		t.Fatalf("ListUnits (initial): %v", err)
	}
	if len(units) != 0 {
		t.Fatalf("expected 0 units initially, got %d", len(units))
	}

	// Populate units.
	m.Units = []UnitStatus{
		{Name: "nginx.service", LoadState: "loaded", ActiveState: "inactive", SubState: "dead"},
	}

	units, err = m.ListUnits(context.Background())
	if err != nil {
		t.Fatalf("ListUnits (after populate): %v", err)
	}
	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units))
	}
	if units[0].ActiveState != "inactive" {
		t.Fatalf("expected inactive, got %q", units[0].ActiveState)
	}

	// Enable and start.
	if err := m.SetStatus(context.Background(), "nginx.service", Enable); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := m.SetStatus(context.Background(), "nginx.service", Start); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Populate log entries and replay.
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	m.Entries = []JournalEntry{
		{Message: "Starting nginx...", RealtimeTimestamp: ts, SystemdUnit: "nginx.service", Priority: "6"},
		{Message: "Started nginx.", RealtimeTimestamp: ts.Add(time.Second), SystemdUnit: "nginx.service", Priority: "6"},
	}

	ch, err := m.LogReplay(context.Background(), "nginx.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	var entries []JournalEntry
	for e := range ch {
		entries = append(entries, e)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}
	if entries[0].Priority != "6" {
		t.Fatalf("expected priority %q, got %q", "6", entries[0].Priority)
	}

	// Stop and disable.
	if err := m.SetStatus(context.Background(), "nginx.service", Stop); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := m.SetStatus(context.Background(), "nginx.service", Disable); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	// Verify full call log.
	calls := m.GetCalls()
	if len(calls) != 7 {
		t.Fatalf("expected 7 calls, got %d", len(calls))
	}

	expected := []string{"ListUnits", "ListUnits", "SetStatus", "SetStatus", "LogReplay", "SetStatus", "SetStatus"}
	for i, want := range expected {
		if calls[i].Method != want {
			t.Fatalf("call %d: expected method %q, got %q", i, want, calls[i].Method)
		}
	}
}

// --- ListPackageUnitFiles tests ---

func TestMockManagerListPackageUnitFiles(t *testing.T) {
	m := InitMockManager()
	ctx := context.Background()

	// Install several units for repo-a/nginx/1.0.
	if err := m.InstallUnit(ctx, "town-os-package--repo-a-nginx-1.0.service", "content"); err != nil {
		t.Fatalf("InstallUnit: %v", err)
	}
	if err := m.InstallUnit(ctx, "town-os-package--repo-a-nginx-1.0-8080-tcp.socket", "content"); err != nil {
		t.Fatalf("InstallUnit: %v", err)
	}
	if err := m.InstallUnit(ctx, "town-os-package--repo-a-nginx-1.0-upnp.service", "content"); err != nil {
		t.Fatalf("InstallUnit: %v", err)
	}
	if err := m.InstallUnit(ctx, "town-os-package--repo-a-nginx-1.0-upnp.timer", "content"); err != nil {
		t.Fatalf("InstallUnit: %v", err)
	}

	// Install a unit for a different package.
	if err := m.InstallUnit(ctx, "town-os-package--repo-a-redis-7.0.service", "content"); err != nil {
		t.Fatalf("InstallUnit: %v", err)
	}

	names, err := m.ListPackageUnitFiles(ctx, "repo-a", "nginx", "1.0")
	if err != nil {
		t.Fatalf("ListPackageUnitFiles: %v", err)
	}

	if len(names) != 4 {
		t.Fatalf("expected 4 unit files, got %d: %v", len(names), names)
	}

	// Should be sorted.
	expectedNames := []string{
		"town-os-package--repo-a-nginx-1.0-8080-tcp.socket",
		"town-os-package--repo-a-nginx-1.0-upnp.service",
		"town-os-package--repo-a-nginx-1.0-upnp.timer",
		"town-os-package--repo-a-nginx-1.0.service",
	}
	for i, want := range expectedNames {
		if names[i] != want {
			t.Fatalf("names[%d]: expected %q, got %q", i, want, names[i])
		}
	}
}

func TestMockManagerListPackageUnitFilesAfterUninstall(t *testing.T) {
	m := InitMockManager()
	ctx := context.Background()

	if err := m.InstallUnit(ctx, "town-os-package--repo-a-nginx-1.0.service", "content"); err != nil {
		t.Fatalf("InstallUnit: %v", err)
	}
	if err := m.InstallUnit(ctx, "town-os-package--repo-a-nginx-1.0-8080-tcp.socket", "content"); err != nil {
		t.Fatalf("InstallUnit: %v", err)
	}

	// Uninstall all.
	if err := m.UninstallUnit(ctx, "town-os-package--repo-a-nginx-1.0.service"); err != nil {
		t.Fatalf("UninstallUnit: %v", err)
	}
	if err := m.UninstallUnit(ctx, "town-os-package--repo-a-nginx-1.0-8080-tcp.socket"); err != nil {
		t.Fatalf("UninstallUnit: %v", err)
	}

	names, err := m.ListPackageUnitFiles(ctx, "repo-a", "nginx", "1.0")
	if err != nil {
		t.Fatalf("ListPackageUnitFiles: %v", err)
	}

	if len(names) != 0 {
		t.Fatalf("expected 0 unit files after uninstall, got %d: %v", len(names), names)
	}
}

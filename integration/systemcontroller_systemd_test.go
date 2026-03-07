package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// --- Systemd integration tests ---

func TestSystemControllerSystemdListUnitsEmpty(t *testing.T) {
	sd := systemd.InitMockManager()
	c := initSystemControllerSystemdTest(t, sd)

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 0 {
		t.Fatalf("expected 0 units, got %d", len(units.Entries))
	}
}

func TestSystemControllerSystemdListUnitsPopulated(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", Description: "The NGINX HTTP Server", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "town-os-package--repo-redis-2.0.service", Description: "Redis", LoadState: "loaded", ActiveState: "inactive", SubState: "dead"},
		{Name: "town-os-package--repo-postgres-16.0.service", Description: "PostgreSQL", LoadState: "loaded", ActiveState: "active", SubState: "running"},
	}
	c := initSystemControllerSystemdTest(t, sd,
		packages.PackageIdentity{Repo: "repo", Name: "nginx", Version: "1.0"},
		packages.PackageIdentity{Repo: "repo", Name: "redis", Version: "2.0"},
		packages.PackageIdentity{Repo: "repo", Name: "postgres", Version: "16.0"},
	)

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 3 {
		t.Fatalf("expected 3 units, got %d", len(units.Entries))
	}

	if units.Entries[0].Name != "town-os-package--repo-nginx-1.0.service" {
		t.Fatalf("expected first unit town-os-package--repo-nginx-1.0.service, got %q", units.Entries[0].Name)
	}
	if units.Entries[0].Description != "The NGINX HTTP Server" {
		t.Fatalf("expected description %q, got %q", "The NGINX HTTP Server", units.Entries[0].Description)
	}
	if units.Entries[0].ActiveState != "active" {
		t.Fatalf("expected active state %q, got %q", "active", units.Entries[0].ActiveState)
	}
	if units.Entries[0].SubState != "running" {
		t.Fatalf("expected sub state %q, got %q", "running", units.Entries[0].SubState)
	}

	if units.Entries[1].Name != "town-os-package--repo-redis-2.0.service" {
		t.Fatalf("expected second unit town-os-package--repo-redis-2.0.service, got %q", units.Entries[1].Name)
	}
	if units.Entries[1].ActiveState != "inactive" {
		t.Fatalf("expected inactive state for redis, got %q", units.Entries[1].ActiveState)
	}

	if units.Entries[2].Name != "town-os-package--repo-postgres-16.0.service" {
		t.Fatalf("expected third unit town-os-package--repo-postgres-16.0.service, got %q", units.Entries[2].Name)
	}
}

func TestSystemControllerSystemdListUnitsPreservesAllFields(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-test-1.0.service", Description: "Test Unit", LoadState: "loaded", ActiveState: "activating", SubState: "start-pre"},
	}
	c := initSystemControllerSystemdTest(t, sd,
		packages.PackageIdentity{Repo: "repo", Name: "test", Version: "1.0"},
	)

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units.Entries))
	}

	u := units.Entries[0]
	if u.Name != "town-os-package--repo-test-1.0.service" {
		t.Fatalf("Name: expected %q, got %q", "town-os-package--repo-test-1.0.service", u.Name)
	}
	if u.Description != "Test Unit" {
		t.Fatalf("Description: expected %q, got %q", "Test Unit", u.Description)
	}
	if u.LoadState != "loaded" {
		t.Fatalf("LoadState: expected %q, got %q", "loaded", u.LoadState)
	}
	if u.ActiveState != "activating" {
		t.Fatalf("ActiveState: expected %q, got %q", "activating", u.ActiveState)
	}
	if u.SubState != "start-pre" {
		t.Fatalf("SubState: expected %q, got %q", "start-pre", u.SubState)
	}
}

func TestSystemControllerSystemdSetUnitStatusAllActions(t *testing.T) {
	for _, action := range []systemd.StatusAction{systemd.Start, systemd.Stop, systemd.Restart} {
		t.Run(string(action), func(t *testing.T) {
			sd := systemd.InitMockManager()
			c := initSystemControllerSystemdTest(t, sd)

			if err := c.SetUnitStatus(context.TODO(), "nginx.service", action); err != nil {
				t.Fatalf("SetUnitStatus(%q, %q): %v", "nginx.service", action, err)
			}

			calls := sd.GetCalls()
			if len(calls) != 1 {
				t.Fatalf("expected 1 call, got %d", len(calls))
			}
			if calls[0].Method != "SetStatus" {
				t.Fatalf("expected SetStatus call, got %q", calls[0].Method)
			}

			unit, ok := calls[0].Args[0].(string)
			if !ok {
				t.Fatalf("expected string arg, got %T", calls[0].Args[0])
			}
			if unit != "nginx.service" {
				t.Fatalf("expected unit %q, got %q", "nginx.service", unit)
			}

			gotAction, ok := calls[0].Args[1].(systemd.StatusAction)
			if !ok {
				t.Fatalf("expected StatusAction arg, got %T", calls[0].Args[1])
			}
			if gotAction != action {
				t.Fatalf("expected action %q, got %q", action, gotAction)
			}
		})
	}

	// Enable and disable must be rejected.
	for _, action := range []systemd.StatusAction{systemd.Enable, systemd.Disable} {
		t.Run(string(action), func(t *testing.T) {
			sd := systemd.InitMockManager()
			c := initSystemControllerSystemdTest(t, sd)

			err := c.SetUnitStatus(context.TODO(), "nginx.service", action)
			if err == nil {
				t.Fatalf("expected error for %q action", action)
			}
		})
	}
}

func TestSystemControllerSystemdSetUnitStatusInvalidAction(t *testing.T) {
	sd := systemd.InitMockManager()
	c := initSystemControllerSystemdTest(t, sd)

	err := c.SetUnitStatus(context.TODO(), "nginx.service", systemd.StatusAction("bogus"))
	if err == nil {
		t.Fatal("expected error for invalid action")
	}
}

func TestSystemControllerSystemdLogReplay(t *testing.T) {
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	sd := systemd.InitMockManager()
	sd.Entries = []systemd.JournalEntry{
		{Message: "Starting nginx...", RealtimeTimestamp: ts, SystemdUnit: "nginx.service", Priority: "6"},
		{Message: "Started nginx.", RealtimeTimestamp: ts.Add(time.Second), SystemdUnit: "nginx.service", Priority: "6"},
		{Message: "Listening on :80", RealtimeTimestamp: ts.Add(2 * time.Second), SystemdUnit: "nginx.service", Priority: "6"},
	}
	c := initSystemControllerSystemdTest(t, sd)

	ch, err := c.LogReplay(context.TODO(), "nginx.service")
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

	if entries[0].Message != "Starting nginx..." {
		t.Fatalf("expected first message %q, got %q", "Starting nginx...", entries[0].Message)
	}
	if entries[1].Message != "Started nginx." {
		t.Fatalf("expected second message %q, got %q", "Started nginx.", entries[1].Message)
	}
	if entries[2].Message != "Listening on :80" {
		t.Fatalf("expected third message %q, got %q", "Listening on :80", entries[2].Message)
	}
}

func TestSystemControllerSystemdLogReplayPreservesFields(t *testing.T) {
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	sd := systemd.InitMockManager()
	sd.Entries = []systemd.JournalEntry{
		{
			Message:           "test message",
			RealtimeTimestamp: ts,
			Priority:          "4",
			SystemdUnit:       "test.service",
			Hostname:          "testhost",
			PID:               "1234",
			UID:               "1000",
			Comm:              "nginx",
			SyslogIdentifier:  "nginx",
			Transport:         "journal",
		},
	}
	c := initSystemControllerSystemdTest(t, sd)

	ch, err := c.LogReplay(context.TODO(), "test.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	entry := <-ch
	// Drain the channel.
	for range ch {
	}

	if entry.Message != "test message" {
		t.Fatalf("Message: expected %q, got %q", "test message", entry.Message)
	}
	if !entry.RealtimeTimestamp.Equal(ts) {
		t.Fatalf("RealtimeTimestamp: expected %v, got %v", ts, entry.RealtimeTimestamp)
	}
	if entry.Priority != "4" {
		t.Fatalf("Priority: expected %q, got %q", "4", entry.Priority)
	}
	if entry.SystemdUnit != "test.service" {
		t.Fatalf("SystemdUnit: expected %q, got %q", "test.service", entry.SystemdUnit)
	}
	if entry.Hostname != "testhost" {
		t.Fatalf("Hostname: expected %q, got %q", "testhost", entry.Hostname)
	}
	if entry.PID != "1234" {
		t.Fatalf("PID: expected %q, got %q", "1234", entry.PID)
	}
	if entry.UID != "1000" {
		t.Fatalf("UID: expected %q, got %q", "1000", entry.UID)
	}
	if entry.Comm != "nginx" {
		t.Fatalf("Comm: expected %q, got %q", "nginx", entry.Comm)
	}
	if entry.SyslogIdentifier != "nginx" {
		t.Fatalf("SyslogIdentifier: expected %q, got %q", "nginx", entry.SyslogIdentifier)
	}
	if entry.Transport != "journal" {
		t.Fatalf("Transport: expected %q, got %q", "journal", entry.Transport)
	}
}

func TestSystemControllerSystemdLogReplayEmpty(t *testing.T) {
	sd := systemd.InitMockManager()
	c := initSystemControllerSystemdTest(t, sd)

	ch, err := c.LogReplay(context.TODO(), "nginx.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	var entries []systemd.JournalEntry
	for e := range ch {
		entries = append(entries, e)
	}

	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestSystemControllerSystemdLogReplayManyEntries(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Entries = make([]systemd.JournalEntry, 100)
	for i := range sd.Entries {
		sd.Entries[i] = systemd.JournalEntry{
			Message:     time.Now().Format(time.RFC3339Nano),
			SystemdUnit: "bulk.service",
			Priority:    "6",
		}
	}
	c := initSystemControllerSystemdTest(t, sd)

	ch, err := c.LogReplay(context.TODO(), "bulk.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	count := 0
	for range ch {
		count++
	}

	if count != 100 {
		t.Fatalf("expected 100 entries, got %d", count)
	}
}

func TestSystemControllerSystemdLogReplayCallLog(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Entries = []systemd.JournalEntry{
		{Message: "hello"},
	}
	c := initSystemControllerSystemdTest(t, sd)

	ch, err := c.LogReplay(context.TODO(), "nginx.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}
	for range ch {
	}

	calls := sd.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "LogReplay" {
		t.Fatalf("expected LogReplay call, got %q", calls[0].Method)
	}
	if len(calls[0].Args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(calls[0].Args))
	}
	unit, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatalf("expected string arg, got %T", calls[0].Args[0])
	}
	if unit != "nginx.service" {
		t.Fatalf("expected unit %q, got %q", "nginx.service", unit)
	}
}

func TestSystemControllerSystemdLogTailPriority(t *testing.T) {
	sd := systemd.InitMockManager()
	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "debug msg", Priority: "7", RealtimeTimestamp: now.Add(-4 * time.Second)},
		{Cursor: "c2", Message: "info msg", Priority: "6", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c3", Message: "error msg", Priority: "3", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c4", Message: "critical msg", Priority: "2", RealtimeTimestamp: now.Add(-time.Second)},
	}
	c := initSystemControllerSystemdTest(t, sd)

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

func TestSystemControllerSystemdLogTailPriorityNoFilter(t *testing.T) {
	sd := systemd.InitMockManager()
	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "debug msg", Priority: "7", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c2", Message: "error msg", Priority: "3", RealtimeTimestamp: now.Add(-time.Second)},
	}
	c := initSystemControllerSystemdTest(t, sd)

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100})
	if err != nil {
		t.Fatalf("LogTail without priority: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries without priority filter, got %d", len(result.Entries))
	}
}

func TestSystemControllerSystemdLogTailPriorityWithGrep(t *testing.T) {
	sd := systemd.InitMockManager()
	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "error: disk full", Priority: "3", RealtimeTimestamp: now.Add(-3 * time.Second)},
		{Cursor: "c2", Message: "info: disk ok", Priority: "6", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c3", Message: "error: memory low", Priority: "3", RealtimeTimestamp: now.Add(-time.Second)},
	}
	c := initSystemControllerSystemdTest(t, sd)

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Priority: 3, Grep: "disk"})
	if err != nil {
		t.Fatalf("LogTail with priority+grep: %v", err)
	}

	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result.Entries))
	}

	if result.Entries[0].Message != "error: disk full" {
		t.Fatalf("expected entry %q, got %q", "error: disk full", result.Entries[0].Message)
	}
}

func TestSystemControllerSystemdLogTailPriorityEmptyPriority(t *testing.T) {
	sd := systemd.InitMockManager()
	now := time.Now()
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "no priority", RealtimeTimestamp: now.Add(-2 * time.Second)},
		{Cursor: "c2", Message: "has priority", Priority: "3", RealtimeTimestamp: now.Add(-time.Second)},
		{Cursor: "c3", Message: "high priority", Priority: "7", RealtimeTimestamp: now},
	}
	c := initSystemControllerSystemdTest(t, sd)

	result, err := c.LogTail(context.TODO(), systemd.LogTailParams{Unit: "test.service", Lines: 100, Priority: 3})
	if err != nil {
		t.Fatalf("LogTail with priority: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries (empty + pri 3), got %d", len(result.Entries))
	}
}

func TestSystemControllerSystemdFullLifecycle(t *testing.T) {
	sd := systemd.InitMockManager()
	c := initSystemControllerSystemdTest(t, sd,
		packages.PackageIdentity{Repo: "repo", Name: "nginx", Version: "1.0"},
	)

	// Start with no units.
	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits (initial): %v", err)
	}
	if len(units.Entries) != 0 {
		t.Fatalf("expected 0 units initially, got %d", len(units.Entries))
	}

	// Populate units.
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", LoadState: "loaded", ActiveState: "inactive", SubState: "dead"},
	}

	units, err = c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits (after populate): %v", err)
	}
	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units.Entries))
	}
	if units.Entries[0].ActiveState != "inactive" {
		t.Fatalf("expected inactive, got %q", units.Entries[0].ActiveState)
	}

	// Start.
	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-nginx-1.0.service", systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Populate log entries and replay.
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	sd.Entries = []systemd.JournalEntry{
		{Message: "Starting nginx...", RealtimeTimestamp: ts, SystemdUnit: "town-os-package--repo-nginx-1.0.service", Priority: "6"},
		{Message: "Started nginx.", RealtimeTimestamp: ts.Add(time.Second), SystemdUnit: "town-os-package--repo-nginx-1.0.service", Priority: "6"},
	}

	ch, err := c.LogReplay(context.TODO(), "town-os-package--repo-nginx-1.0.service")
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	var entries []systemd.JournalEntry
	for e := range ch {
		entries = append(entries, e)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}
	if entries[0].Message != "Starting nginx..." {
		t.Fatalf("expected first message %q, got %q", "Starting nginx...", entries[0].Message)
	}

	// Stop.
	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-nginx-1.0.service", systemd.Stop); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Verify full call log.
	calls := sd.GetCalls()
	if len(calls) != 5 {
		t.Fatalf("expected 5 calls, got %d", len(calls))
	}

	expected := []string{"ListUnits", "ListUnits", "SetStatus", "LogReplay", "SetStatus"}
	for i, want := range expected {
		if calls[i].Method != want {
			t.Fatalf("call %d: expected method %q, got %q", i, want, calls[i].Method)
		}
	}
}

func TestSystemControllerSystemdListUnitsCallLog(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service"},
	}
	c := initSystemControllerSystemdTest(t, sd)

	if _, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{}); err != nil {
		t.Fatalf("ListUnits: %v", err)
	}
	if _, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{}); err != nil {
		t.Fatalf("ListUnits (second): %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	for _, call := range calls {
		if call.Method != "ListUnits" {
			t.Fatalf("expected ListUnits call, got %q", call.Method)
		}
	}
}

func TestSystemControllerSystemdSetUnitStatusCallLog(t *testing.T) {
	sd := systemd.InitMockManager()
	c := initSystemControllerSystemdTest(t, sd)

	if err := c.SetUnitStatus(context.TODO(), "nginx.service", systemd.Restart); err != nil {
		t.Fatalf("SetUnitStatus: %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "SetStatus" {
		t.Fatalf("expected SetStatus, got %q", calls[0].Method)
	}
	if len(calls[0].Args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(calls[0].Args))
	}
	unitStr, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if unitStr != "nginx.service" {
		t.Fatalf("expected unit %q, got %v", "nginx.service", calls[0].Args[0])
	}
	action, ok := calls[0].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if action != systemd.Restart {
		t.Fatalf("expected action %q, got %v", systemd.Restart, calls[0].Args[1])
	}
}

func TestSystemControllerSystemdSetUnitStatusMultipleUnits(t *testing.T) {
	sd := systemd.InitMockManager()
	c := initSystemControllerSystemdTest(t, sd)

	if err := c.SetUnitStatus(context.TODO(), "nginx.service", systemd.Start); err != nil {
		t.Fatalf("Start nginx: %v", err)
	}
	if err := c.SetUnitStatus(context.TODO(), "redis.service", systemd.Stop); err != nil {
		t.Fatalf("Stop redis: %v", err)
	}
	if err := c.SetUnitStatus(context.TODO(), "postgres.service", systemd.Restart); err != nil {
		t.Fatalf("Restart postgres: %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}

	type expectedCall struct {
		unit   string
		action systemd.StatusAction
	}
	want := []expectedCall{
		{"nginx.service", systemd.Start},
		{"redis.service", systemd.Stop},
		{"postgres.service", systemd.Restart},
	}
	for i, w := range want {
		unitName, ok := calls[i].Args[0].(string)
		if !ok {
			t.Fatal("type assertion failed")
		}
		if unitName != w.unit {
			t.Fatalf("call %d: expected unit %q, got %v", i, w.unit, calls[i].Args[0])
		}
		actionVal, ok := calls[i].Args[1].(systemd.StatusAction)
		if !ok {
			t.Fatal("type assertion failed")
		}
		if actionVal != w.action {
			t.Fatalf("call %d: expected action %q, got %v", i, w.action, calls[i].Args[1])
		}
	}
}

func TestSystemControllerPingUnitCountsFiltersTownOS(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--core-nginx-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--core-redis-7.0.service", ActiveState: "active"},
		{Name: "town-os-package--extras-postgres-16.0.service", ActiveState: "failed"},
		{Name: "sshd.service", ActiveState: "active"},
		{Name: "systemd-journald.service", ActiveState: "active"},
	}
	c := initSystemControllerSystemdTest(t, sd,
		packages.PackageIdentity{Repo: "core", Name: "nginx", Version: "1.0"},
		packages.PackageIdentity{Repo: "core", Name: "redis", Version: "7.0"},
		packages.PackageIdentity{Repo: "extras", Name: "postgres", Version: "16.0"},
	)

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if ping.Units == nil {
		t.Fatal("expected units in ping response")
	}

	if ping.Units.Total != 3 {
		t.Fatalf("expected 3 total town-os units, got %d", ping.Units.Total)
	}

	if ping.Units.Active != 2 {
		t.Fatalf("expected 2 active town-os units, got %d", ping.Units.Active)
	}

	if ping.Units.Failed != 1 {
		t.Fatalf("expected 1 failed town-os unit, got %d", ping.Units.Failed)
	}
}

func TestSystemControllerPingUnitCountsExcludesUninstalled(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--core-nginx-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--core-redis-7.0.service", ActiveState: "active"},
		{Name: "town-os-package--extras-postgres-16.0.service", ActiveState: "failed"},
	}
	// Only nginx is installed; redis and postgres units are leftover.
	c := initSystemControllerSystemdTest(t, sd,
		packages.PackageIdentity{Repo: "core", Name: "nginx", Version: "1.0"},
	)

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if ping.Units == nil {
		t.Fatal("expected units in ping response")
	}

	if ping.Units.Total != 1 {
		t.Fatalf("expected 1 total unit (only installed), got %d", ping.Units.Total)
	}

	if ping.Units.Active != 1 {
		t.Fatalf("expected 1 active unit, got %d", ping.Units.Active)
	}

	if ping.Units.Failed != 0 {
		t.Fatalf("expected 0 failed units, got %d", ping.Units.Failed)
	}
}

func TestSystemControllerListUnitsFiltersNonPackageUnits(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-nginx-1.0-network.service", ActiveState: "active"},
		{Name: "town-os-package--repo-nginx-1.0-8080-tcp.socket", ActiveState: "active"},
		{Name: "sshd.service", ActiveState: "active"},
		{Name: "systemd-journald.service", ActiveState: "active"},
		{Name: "town-os-systemcontroller.service", ActiveState: "active"},
	}
	c := initSystemControllerSystemdTest(t, sd,
		packages.PackageIdentity{Repo: "repo", Name: "nginx", Version: "1.0"},
	)

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit (only main package service), got %d", len(units.Entries))
	}
	if units.Entries[0].Name != "town-os-package--repo-nginx-1.0.service" {
		t.Fatalf("expected town-os-package--repo-nginx-1.0.service, got %s", units.Entries[0].Name)
	}
}

func TestSystemControllerListUnitsDescriptionEnrichment(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-redis-7.0.service", ActiveState: "active"},
	}

	btr := storage.InitBtrFS("/town-os")
	inst := packages.InitMockInstallManager()
	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "nginx", Version: "1.0"},
		{Repo: "repo", Name: "redis", Version: "7.0"},
	}

	dir := t.TempDir()
	rr := &packages.RepositoryRoot{
		BaseDir: dir,
		Items:   []packages.Repository{{Name: "repo"}},
		Git:     &git.GoGitClient{Home: dir},
	}

	// Write package files with descriptions.
	pkgDir := filepath.Join(dir, "repo", packages.PackagesDir, "nginx")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte("image: nginx:1.0\ndescription: A fast web server\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	redisPkgDir := filepath.Join(dir, "repo", packages.PackagesDir, "redis")
	if err := os.MkdirAll(redisPkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(redisPkgDir, "7.0.yaml"), []byte("image: redis:7.0\ndescription: In-memory data store\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        btr,
		Installer:      inst,
		Systemd:        sd,
		RepositoryRoot: rr,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units.Entries))
	}

	descMap := map[string]string{}
	for _, e := range units.Entries {
		descMap[e.PackageIdentifier] = e.PackageDescription
	}

	if descMap["repo/nginx@1.0"] != "A fast web server" {
		t.Fatalf("expected nginx description %q, got %q", "A fast web server", descMap["repo/nginx@1.0"])
	}
	if descMap["repo/redis@7.0"] != "In-memory data store" {
		t.Fatalf("expected redis description %q, got %q", "In-memory data store", descMap["repo/redis@7.0"])
	}
}

func TestSystemControllerListUnitsNoDescriptionWithoutRepoRoot(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
	}
	c := initSystemControllerSystemdTest(t, sd,
		packages.PackageIdentity{Repo: "repo", Name: "nginx", Version: "1.0"},
	)

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units.Entries))
	}
	if units.Entries[0].PackageDescription != "" {
		t.Fatalf("expected empty description without repo root, got %q", units.Entries[0].PackageDescription)
	}
}

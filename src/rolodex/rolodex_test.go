// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package rolodex

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/systemd"
)

func rolodexTestDir(t *testing.T, pattern string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", pattern) //nolint:usetesting // dash-case dir names
	if err != nil {
		t.Fatalf("MkdirTemp(%s): %v", pattern, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestSystemServices(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-svc-*")
	mgr := NewManager(Config{
		Systemd:        systemd.InitMockManager(),
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:test",
		UnixSocketPath: filepath.Join(dir, "rolodex.sock"),
	})

	svcs := mgr.SystemServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}

	svc := svcs[0]
	if svc.Key != "rolodex" {
		t.Fatalf("expected key %q, got %q", "rolodex", svc.Key)
	}
	if svc.DisplayName != "Rolodex DNS" {
		t.Fatalf("expected display name %q, got %q", "Rolodex DNS", svc.DisplayName)
	}
	if svc.Image != "quay.io/town/rolodex:test" {
		t.Fatalf("expected image %q, got %q", "quay.io/town/rolodex:test", svc.Image)
	}
	if svc.Port != DefaultDNSPort {
		t.Fatalf("expected port %q, got %q", DefaultDNSPort, svc.Port)
	}
	if svc.UnitName != systemd.SystemServiceUnitName("rolodex") {
		t.Fatalf("expected unit name %q, got %q", systemd.SystemServiceUnitName("rolodex"), svc.UnitName)
	}
}

func TestSystemServicesCustomPort(t *testing.T) {
	mgr := NewManager(Config{
		Systemd:        systemd.InitMockManager(),
		DataDir:        t.TempDir(),
		Image:          "quay.io/town/rolodex:test",
		UnixSocketPath: filepath.Join(t.TempDir(), "rolodex.sock"),
		DNSPort:        "15353",
	})

	svcs := mgr.SystemServices()
	if svcs[0].Port != "15353" {
		t.Fatalf("expected port %q, got %q", "15353", svcs[0].Port)
	}
}

func TestSystemServicesCustomKey(t *testing.T) {
	mgr := NewManager(Config{
		Systemd:        systemd.InitMockManager(),
		DataDir:        t.TempDir(),
		Image:          "quay.io/town/rolodex:test",
		UnixSocketPath: filepath.Join(t.TempDir(), "rolodex.sock"),
		Key:            "test-rolodex",
	})

	svcs := mgr.SystemServices()
	if svcs[0].Key != "test-rolodex" {
		t.Fatalf("expected key %q, got %q", "test-rolodex", svcs[0].Key)
	}
	if svcs[0].UnitName != systemd.SystemServiceUnitName("test-rolodex") {
		t.Fatalf("expected unit name %q, got %q", systemd.SystemServiceUnitName("test-rolodex"), svcs[0].UnitName)
	}
}

func TestSocketPath(t *testing.T) {
	mgr := NewManager(Config{
		UnixSocketPath: "/data/rolodex/rolodex.sock",
	})

	if got := mgr.SocketPath(); got != "/data/rolodex/rolodex.sock" {
		t.Fatalf("expected socket path %q, got %q", "/data/rolodex/rolodex.sock", got)
	}
}

func TestKeyDefault(t *testing.T) {
	mgr := NewManager(Config{})
	if got := mgr.Key(); got != "rolodex" {
		t.Fatalf("expected default key %q, got %q", "rolodex", got)
	}
}

func TestKeyCustom(t *testing.T) {
	mgr := NewManager(Config{Key: "custom"})
	if got := mgr.Key(); got != "custom" {
		t.Fatalf("expected key %q, got %q", "custom", got)
	}
}

func TestStatusReportsRunningWhenUnitActive(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("rolodex"), ActiveState: "active", SubState: "running"},
	}

	mgr := NewManager(Config{
		Systemd: sd,
		DataDir: t.TempDir(),
		Image:   "quay.io/town/rolodex:test",
	})

	status := mgr.Status(context.Background())
	if !status.Running {
		t.Fatal("expected Running=true when unit is active")
	}
}

func TestStatusReportsNotRunningWhenUnitInactive(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("rolodex"), ActiveState: "inactive"},
	}

	mgr := NewManager(Config{
		Systemd: sd,
		DataDir: t.TempDir(),
		Image:   "quay.io/town/rolodex:test",
	})

	status := mgr.Status(context.Background())
	if status.Running {
		t.Fatal("expected Running=false when unit is inactive")
	}
}

func TestStatusUsesGetUnitStatesTargetedCall(t *testing.T) {
	// Regression: Status() must query only its own unit name via
	// GetUnitStates, not enumerate every unit via ListUnits. On overlayfs
	// hosts a broad ListUnits call triggers ESTALE floods for unrelated
	// unit files.
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("rolodex"), ActiveState: "active"},
	}

	mgr := NewManager(Config{
		Systemd: sd,
		DataDir: t.TempDir(),
		Image:   "quay.io/town/rolodex:test",
	})

	_ = mgr.Status(context.Background())

	calls := sd.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 systemd call, got %d: %+v", len(calls), calls)
	}
	if calls[0].Method != "GetUnitStates" {
		t.Fatalf("expected GetUnitStates, got %q", calls[0].Method)
	}
	names, ok := calls[0].Args[0].([]string)
	if !ok {
		t.Fatalf("expected []string arg, got %T", calls[0].Args[0])
	}
	want := systemd.SystemServiceUnitName("rolodex")
	if len(names) != 1 || names[0] != want {
		t.Fatalf("expected single query for %q, got %v", want, names)
	}
}

func TestStatusCustomKey(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("custom-rolodex"), ActiveState: "active"},
	}

	mgr := NewManager(Config{
		Systemd: sd,
		DataDir: t.TempDir(),
		Image:   "quay.io/town/rolodex:test",
		Key:     "custom-rolodex",
	})

	status := mgr.Status(context.Background())
	if !status.Running {
		t.Fatal("expected Running=true when custom-key unit is active")
	}
	if status.Name != systemd.SystemServiceContainerName("custom-rolodex") {
		t.Fatalf("unexpected container name: %q", status.Name)
	}
}

func TestMockClientAddRecord(t *testing.T) {
	mc := &MockClient{}
	rec := &upstream.DnsRecord{
		Name:       "test.example.com.",
		RecordType: upstream.RecordTypeA,
		Value:      "1.2.3.4",
	}

	if err := mc.AddRecord(context.Background(), rec); err != nil {
		t.Fatalf("AddRecord: %v", err)
	}

	calls := mc.GetCalls()
	if len(calls) != 1 || calls[0].Method != "AddRecord" {
		t.Fatalf("expected 1 AddRecord call, got %v", calls)
	}

	records, err := mc.ListRecords(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
}

func TestMockClientRemoveRecord(t *testing.T) {
	mc := &MockClient{RemoveCount: 1}

	count, err := mc.RemoveRecord(context.Background(), "test.example.com.", nil)
	if err != nil {
		t.Fatalf("RemoveRecord: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected remove count 1, got %d", count)
	}

	calls := mc.GetCalls()
	if len(calls) != 1 || calls[0].Method != "RemoveRecord" {
		t.Fatalf("expected 1 RemoveRecord call, got %v", calls)
	}
}

func TestMockClientListRecords(t *testing.T) {
	mc := &MockClient{}

	records, err := mc.ListRecords(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records, got %d", len(records))
	}

	calls := mc.GetCalls()
	if len(calls) != 1 || calls[0].Method != "ListRecords" {
		t.Fatalf("expected 1 ListRecords call, got %v", calls)
	}
}

// findFreePort returns a free TCP port on the loopback interface by binding
// to port 0 and reading back the assigned port.
func findFreePort(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("findFreePort: %v", err)
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return port
}

func TestWaitForDNSReadyImmediate(t *testing.T) {
	port := findFreePort(t)

	// Start a TCP listener before calling WaitForDNSReady.
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", net.JoinHostPort(DNSLoopback, port))
	if err != nil {
		// 127.0.0.2 may not be configured on all CI hosts; skip gracefully.
		t.Skip("127.0.0.2 not available, skipping")
	}
	t.Cleanup(func() { _ = ln.Close() })

	mgr := NewManager(Config{
		Systemd: systemd.InitMockManager(),
		DataDir: t.TempDir(),
		Image:   "quay.io/town/rolodex:test",
		DNSPort: port,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	if err := mgr.WaitForDNSReady(ctx); err != nil {
		t.Fatalf("WaitForDNSReady: %v", err)
	}

	// Should complete almost immediately (well under 1 second).
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("WaitForDNSReady took %v, expected < 2s", elapsed)
	}
}

func TestWaitForDNSReadyDelayedStart(t *testing.T) {
	port := findFreePort(t)

	// Verify 127.0.0.2 is reachable.
	var lc net.ListenConfig
	probe, err := lc.Listen(context.Background(), "tcp", net.JoinHostPort(DNSLoopback, port))
	if err != nil {
		t.Skip("127.0.0.2 not available, skipping")
	}
	if err := probe.Close(); err != nil {
		t.Fatalf("close probe: %v", err)
	}

	mgr := NewManager(Config{
		Systemd: systemd.InitMockManager(),
		DataDir: t.TempDir(),
		Image:   "quay.io/town/rolodex:test",
		DNSPort: port,
	})

	// Start the listener after a short delay to simulate container startup.
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		time.Sleep(500 * time.Millisecond)
		ln, listenErr := lc.Listen(context.Background(), "tcp", net.JoinHostPort(DNSLoopback, port))
		if listenErr != nil {
			return
		}
		<-done
		_ = ln.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	if err := mgr.WaitForDNSReady(ctx); err != nil {
		t.Fatalf("WaitForDNSReady: %v", err)
	}

	elapsed := time.Since(start)
	if elapsed < 400*time.Millisecond {
		t.Fatalf("WaitForDNSReady returned too quickly (%v), listener should not be up yet", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("WaitForDNSReady took %v, expected < 5s", elapsed)
	}
}

func TestWaitForDNSReadyContextCancelled(t *testing.T) {
	port := findFreePort(t)

	mgr := NewManager(Config{
		Systemd: systemd.InitMockManager(),
		DataDir: t.TempDir(),
		Image:   "quay.io/town/rolodex:test",
		DNSPort: port,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := mgr.WaitForDNSReady(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from WaitForDNSReady when no listener and context expires")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded in error, got: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("WaitForDNSReady took %v after context timeout, expected prompt return", elapsed)
	}
}

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package rolodex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/systemd"
)

// selectiveErrMock wraps MockManager but returns a custom error for a
// specific unit+action pair on SetStatus. All other calls pass through.
type selectiveErrMock struct {
	*systemd.MockManager

	failUnit   string
	failAction systemd.StatusAction
	failErr    error
}

func (s *selectiveErrMock) SetStatus(ctx context.Context, unit string, action systemd.StatusAction) error {
	// Record the call via the wrapped mock (StatusErr is nil so this always succeeds).
	_ = s.MockManager.SetStatus(ctx, unit, action)
	if unit == s.failUnit && action == s.failAction {
		return s.failErr
	}
	return nil
}

func rolodexTestDir(t *testing.T, pattern string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", pattern) //nolint:usetesting // dash-case dir names
	if err != nil {
		t.Fatalf("MkdirTemp(%s): %v", pattern, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestWriteConfig(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-write-config-*")
	mgr := NewManager(Config{
		Systemd:        systemd.InitMockManager(),
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
	})

	if err := mgr.writeConfig(); err != nil {
		t.Fatalf("writeConfig: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "rolodex.yml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	content := string(data)
	for _, want := range []string{
		"database_path: /data/rolodex.db",
		"udp_bind:",
		"tcp_bind:",
		"unix_socket: /data/rolodex.sock",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("config missing %q:\n%s", want, content)
		}
	}
}

func TestUnitConfigsLocal(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-unit-local-*")
	mgr := NewManager(Config{
		Systemd:        systemd.InitMockManager(),
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		Local:          true,
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
	})

	configs := mgr.unitConfigs()
	if len(configs) != 1 {
		t.Fatalf("expected 1 unit config, got %d", len(configs))
	}

	cfg := configs[0]
	if cfg.Key != "rolodex" {
		t.Fatalf("expected key %q, got %q", "rolodex", cfg.Key)
	}

	// Local mode should bind DNSLoopback:53 to container port 53.
	wantTCP := DNSLoopback + ":53:53/tcp"
	wantUDP := DNSLoopback + ":53:53/udp"
	hasLocalMapping := false
	for _, arg := range cfg.Args {
		if arg == wantTCP || arg == wantUDP {
			hasLocalMapping = true
			break
		}
	}
	if !hasLocalMapping {
		t.Fatalf("expected %s port mapping in local mode, got args: %v", wantTCP, cfg.Args)
	}

	// Should NOT have production port mappings (53:53).
	for _, arg := range cfg.Args {
		if arg == "53:53/tcp" || arg == "53:53/udp" {
			t.Fatalf("unexpected production port mapping in local mode: %s", arg)
		}
	}
}

func TestUnitConfigsRemote(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-unit-remote-*")
	mgr := NewManager(Config{
		Systemd:        systemd.InitMockManager(),
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		Local:          false,
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
	})

	configs := mgr.unitConfigs()
	if len(configs) != 1 {
		t.Fatalf("expected 1 unit config, got %d", len(configs))
	}

	cfg := configs[0]

	// Production mode should bind 0.0.0.0:53 to container port 53.
	hasPortMapping := false
	for _, arg := range cfg.Args {
		if arg == "53:53/tcp" || arg == "53:53/udp" {
			hasPortMapping = true
			break
		}
	}
	if !hasPortMapping {
		t.Fatalf("expected 53:53 port mappings in production mode, got args: %v", cfg.Args)
	}

	// Should NOT have --net host.
	for i, arg := range cfg.Args {
		if arg == "--net" && i+1 < len(cfg.Args) && cfg.Args[i+1] == "host" {
			t.Fatal("unexpected --net host in remote mode")
		}
	}
}

func TestUnitConfigsVolumeMount(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-unit-vol-*")
	mgr := NewManager(Config{
		Systemd:        systemd.InitMockManager(),
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
	})

	configs := mgr.unitConfigs()
	cfg := configs[0]

	expectedMount := dir + ":/data"
	hasVolumeMount := false
	for i, arg := range cfg.Args {
		if arg == "-v" && i+1 < len(cfg.Args) && cfg.Args[i+1] == expectedMount {
			hasVolumeMount = true
			break
		}
	}
	if !hasVolumeMount {
		t.Fatalf("expected volume mount %q, got args: %v", expectedMount, cfg.Args)
	}

	if len(cfg.VolumeDirs) != 1 || cfg.VolumeDirs[0] != dir {
		t.Fatalf("expected VolumeDirs [%s], got %v", dir, cfg.VolumeDirs)
	}
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

func TestStartProductionDisablesResolved(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-prod-start-*")
	mock := systemd.InitMockManager()

	// Create a symlink as resolv.conf to simulate systemd-resolved.
	stubFile := filepath.Join(dir, "stub-resolv.conf")
	if err := os.WriteFile(stubFile, []byte("nameserver 127.0.0.53\n"), 0644); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	resolvPath := filepath.Join(dir, "resolv.conf")
	if err := os.Symlink(stubFile, resolvPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	mgr := NewManager(Config{
		Systemd:        mock,
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		Local:          false,
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
		ResolvConfPath: resolvPath,
	})

	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Verify resolv.conf is a regular file with 127.0.0.1.
	fi, err := os.Lstat(resolvPath)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("expected regular file, got symlink")
	}
	data, err := os.ReadFile(resolvPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(data); got != "nameserver 127.0.0.1\n" {
		t.Fatalf("expected resolv.conf %q, got %q", "nameserver 127.0.0.1\n", got)
	}

	// Verify systemd-resolved was stopped and disabled.
	calls := mock.GetCalls()
	hasStop := false
	hasDisable := false
	for _, c := range calls {
		if c.Method != "SetStatus" || len(c.Args) < 2 {
			continue
		}
		unit, _ := c.Args[0].(string)
		action, _ := c.Args[1].(systemd.StatusAction)
		if unit == resolvedUnit && action == systemd.Stop {
			hasStop = true
		}
		if unit == resolvedUnit && action == systemd.Disable {
			hasDisable = true
		}
	}
	if !hasStop {
		t.Error("expected SetStatus(systemd-resolved.service, stop)")
	}
	if !hasDisable {
		t.Error("expected SetStatus(systemd-resolved.service, disable)")
	}

	// Verify rolodex unit was installed and started.
	unitName := systemd.SystemServiceUnitName("rolodex")
	if !mock.InstalledUnits[unitName] {
		t.Errorf("expected unit %s to be installed", unitName)
	}
	hasStart := false
	for _, c := range calls {
		if c.Method == "SetStatus" && len(c.Args) >= 2 {
			u, _ := c.Args[0].(string)
			a, _ := c.Args[1].(systemd.StatusAction)
			if u == unitName && a == systemd.Start {
				hasStart = true
			}
		}
	}
	if !hasStart {
		t.Errorf("expected SetStatus(%s, start)", unitName)
	}
}

func TestStartProductionRollbackOnFailure(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-prod-rollback-*")
	inner := systemd.InitMockManager()

	unitName := systemd.SystemServiceUnitName("rolodex")
	mock := &selectiveErrMock{
		MockManager: inner,
		failUnit:    unitName,
		failAction:  systemd.Start,
		failErr:     errors.New("container start failed"),
	}

	// Create a symlink as resolv.conf.
	stubFile := filepath.Join(dir, "stub-resolv.conf")
	if err := os.WriteFile(stubFile, []byte("nameserver 127.0.0.53\n"), 0644); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	resolvPath := filepath.Join(dir, "resolv.conf")
	if err := os.Symlink(stubFile, resolvPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	mgr := NewManager(Config{
		Systemd:        mock,
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		Local:          false,
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
		ResolvConfPath: resolvPath,
	})

	err := mgr.Start(context.Background())
	if err == nil {
		t.Fatal("expected error from Start")
	}

	// Verify rollback: resolved was re-enabled and re-started.
	calls := inner.GetCalls()
	hasEnable := false
	hasStart := false
	for _, c := range calls {
		if c.Method != "SetStatus" || len(c.Args) < 2 {
			continue
		}
		unit, _ := c.Args[0].(string)
		action, _ := c.Args[1].(systemd.StatusAction)
		if unit == resolvedUnit && action == systemd.Enable {
			hasEnable = true
		}
		if unit == resolvedUnit && action == systemd.Start {
			hasStart = true
		}
	}
	if !hasEnable {
		t.Error("expected rollback SetStatus(systemd-resolved.service, enable)")
	}
	if !hasStart {
		t.Error("expected rollback SetStatus(systemd-resolved.service, start)")
	}

	// Verify resolv.conf was restored as a symlink.
	fi, err := os.Lstat(resolvPath)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected symlink after rollback, got regular file")
	}
	target, err := os.Readlink(resolvPath)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if target != stubFile {
		t.Fatalf("expected symlink target %q, got %q", stubFile, target)
	}
}

func TestStopProductionRestoresResolved(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-prod-stop-*")
	mock := systemd.InitMockManager()

	// Create a symlink as resolv.conf.
	stubFile := filepath.Join(dir, "stub-resolv.conf")
	if err := os.WriteFile(stubFile, []byte("nameserver 127.0.0.53\n"), 0644); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	resolvPath := filepath.Join(dir, "resolv.conf")
	if err := os.Symlink(stubFile, resolvPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	mgr := NewManager(Config{
		Systemd:        mock,
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		Local:          false,
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
		ResolvConfPath: resolvPath,
	})

	// Start first so the manager has state to restore.
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Clear call log so we only see Stop-phase calls.
	mock.Calls = nil

	if err := mgr.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Verify ordering: resolved enable+start must come BEFORE rolodex stop.
	calls := mock.GetCalls()
	unitName := systemd.SystemServiceUnitName("rolodex")
	resolvedEnableIdx := -1
	resolvedStartIdx := -1
	rolodexStopIdx := -1
	for i, c := range calls {
		if c.Method != "SetStatus" || len(c.Args) < 2 {
			continue
		}
		unit, _ := c.Args[0].(string)
		action, _ := c.Args[1].(systemd.StatusAction)
		if unit == resolvedUnit && action == systemd.Enable && resolvedEnableIdx == -1 {
			resolvedEnableIdx = i
		}
		if unit == resolvedUnit && action == systemd.Start && resolvedStartIdx == -1 {
			resolvedStartIdx = i
		}
		if unit == unitName && action == systemd.Stop && rolodexStopIdx == -1 {
			rolodexStopIdx = i
		}
	}

	if resolvedEnableIdx == -1 {
		t.Fatal("missing SetStatus(systemd-resolved.service, enable)")
	}
	if resolvedStartIdx == -1 {
		t.Fatal("missing SetStatus(systemd-resolved.service, start)")
	}
	if rolodexStopIdx == -1 {
		t.Fatalf("missing SetStatus(%s, stop)", unitName)
	}
	if resolvedEnableIdx > rolodexStopIdx {
		t.Fatalf("resolved enable (idx %d) must come before rolodex stop (idx %d)", resolvedEnableIdx, rolodexStopIdx)
	}
	if resolvedStartIdx > rolodexStopIdx {
		t.Fatalf("resolved start (idx %d) must come before rolodex stop (idx %d)", resolvedStartIdx, rolodexStopIdx)
	}

	// Verify resolv.conf was restored as a symlink.
	fi, err := os.Lstat(resolvPath)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected symlink after Stop, got regular file")
	}
	target, err := os.Readlink(resolvPath)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if target != stubFile {
		t.Fatalf("expected symlink target %q, got %q", stubFile, target)
	}
}

func TestStartLocalSkipsResolved(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-local-skip-*")
	mock := systemd.InitMockManager()

	// Write a regular resolv.conf.
	resolvPath := filepath.Join(dir, "resolv.conf")
	origContent := "nameserver 8.8.8.8\n"
	if err := os.WriteFile(resolvPath, []byte(origContent), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	mgr := NewManager(Config{
		Systemd:        mock,
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		Local:          true,
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
		ResolvConfPath: resolvPath,
	})

	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Verify NO calls to systemd-resolved.service.
	for _, c := range mock.GetCalls() {
		if c.Method == "SetStatus" && len(c.Args) >= 1 {
			unit, _ := c.Args[0].(string)
			if unit == resolvedUnit {
				t.Fatalf("unexpected call to %s in local mode: %v", resolvedUnit, c)
			}
		}
	}

	// Verify resolv.conf is unchanged.
	data, err := os.ReadFile(resolvPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(data); got != origContent {
		t.Fatalf("resolv.conf changed in local mode: got %q, want %q", got, origContent)
	}
}

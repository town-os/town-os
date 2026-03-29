// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package rolodex

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	// Production mode without PublicAddr should bind DNSLoopback:53 to container port 53.
	wantTCP := DNSLoopback + ":53:53/tcp"
	wantUDP := DNSLoopback + ":53:53/udp"
	hasPortMapping := false
	for _, arg := range cfg.Args {
		if arg == wantTCP || arg == wantUDP {
			hasPortMapping = true
			break
		}
	}
	if !hasPortMapping {
		t.Fatalf("expected %s port mapping in production mode, got args: %v", wantTCP, cfg.Args)
	}

	// Should NOT have --net host.
	for i, arg := range cfg.Args {
		if arg == "--net" && i+1 < len(cfg.Args) && cfg.Args[i+1] == "host" {
			t.Fatal("unexpected --net host in remote mode")
		}
	}
}

func TestUnitConfigsPublicAddr(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-unit-public-*")
	mgr := NewManager(Config{
		Systemd:        systemd.InitMockManager(),
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		Local:          false,
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
		PublicAddr:     "192.168.5.9",
	})

	configs := mgr.unitConfigs()
	cfg := configs[0]

	// Should have both DNSLoopback and PublicAddr bindings.
	wantLoopTCP := DNSLoopback + ":53:53/tcp"
	wantLoopUDP := DNSLoopback + ":53:53/udp"
	wantPubTCP := "192.168.5.9:53:53/tcp"
	wantPubUDP := "192.168.5.9:53:53/udp"

	found := map[string]bool{}
	for _, arg := range cfg.Args {
		found[arg] = true
	}

	for _, want := range []string{wantLoopTCP, wantLoopUDP, wantPubTCP, wantPubUDP} {
		if !found[want] {
			t.Fatalf("expected mapping %s, got args: %v", want, cfg.Args)
		}
	}
}

func TestSetPublicAddr(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-set-pub-*")
	mgr := NewManager(Config{
		Systemd:        systemd.InitMockManager(),
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
	})

	// First set should return true (changed).
	if !mgr.SetPublicAddr("10.0.0.1") {
		t.Fatal("expected SetPublicAddr to return true on first set")
	}

	// Same value should return false.
	if mgr.SetPublicAddr("10.0.0.1") {
		t.Fatal("expected SetPublicAddr to return false for same value")
	}

	// Different value should return true.
	if !mgr.SetPublicAddr("10.0.0.2") {
		t.Fatal("expected SetPublicAddr to return true for different value")
	}
}

func TestUnitConfigsCustomPort(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-unit-port-*")
	mgr := NewManager(Config{
		Systemd:        systemd.InitMockManager(),
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		Local:          true,
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
		DNSPort:        "15353",
	})

	configs := mgr.unitConfigs()
	if len(configs) != 1 {
		t.Fatalf("expected 1 unit config, got %d", len(configs))
	}

	cfg := configs[0]

	// Custom port should map host 15353 to container 53.
	wantTCP := DNSLoopback + ":15353:53/tcp"
	wantUDP := DNSLoopback + ":15353:53/udp"
	hasTCP := false
	hasUDP := false
	for _, arg := range cfg.Args {
		if arg == wantTCP {
			hasTCP = true
		}
		if arg == wantUDP {
			hasUDP = true
		}
	}
	if !hasTCP {
		t.Fatalf("expected TCP mapping %s, got args: %v", wantTCP, cfg.Args)
	}
	if !hasUDP {
		t.Fatalf("expected UDP mapping %s, got args: %v", wantUDP, cfg.Args)
	}

	// Status and SystemServices should report the custom port.
	status := mgr.Status(context.Background())
	if status.Port != "15353" {
		t.Fatalf("expected status port %q, got %q", "15353", status.Port)
	}
	svcs := mgr.SystemServices()
	if svcs[0].Port != "15353" {
		t.Fatalf("expected service port %q, got %q", "15353", svcs[0].Port)
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

func TestStartProductionRewritesResolv(t *testing.T) {
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

	restarted, err := mgr.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !restarted {
		t.Fatal("expected restart on first Start (unit not yet installed)")
	}

	// Verify resolv.conf is a regular file pointing at DNSLoopback.
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
	want := "nameserver " + DNSLoopback + "\n"
	if got := string(data); got != want {
		t.Fatalf("expected resolv.conf %q, got %q", want, got)
	}

	// Verify rolodex unit was installed and restarted.
	unitName := systemd.SystemServiceUnitName("rolodex")
	if _, ok := mock.InstalledUnits[unitName]; !ok {
		t.Errorf("expected unit %s to be installed", unitName)
	}
	hasRestart := false
	for _, c := range mock.GetCalls() {
		if c.Method == "SetStatus" && len(c.Args) >= 2 {
			u, _ := c.Args[0].(string)
			a, _ := c.Args[1].(systemd.StatusAction)
			if u == unitName && a == systemd.Restart {
				hasRestart = true
			}
		}
	}
	if !hasRestart {
		t.Errorf("expected SetStatus(%s, restart)", unitName)
	}
}

func TestStartProductionRollbackOnFailure(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-prod-rollback-*")
	inner := systemd.InitMockManager()

	unitName := systemd.SystemServiceUnitName("rolodex")
	mock := &selectiveErrMock{
		MockManager: inner,
		failUnit:    unitName,
		failAction:  systemd.Restart,
		failErr:     errors.New("container restart failed"),
	}

	mgr := NewManager(Config{
		Systemd:        mock,
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		Local:          false,
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
	})

	_, err := mgr.Start(context.Background())
	if err == nil {
		t.Fatal("expected error from Start")
	}
}

func TestStartSkipsRestartWhenUnitUnchanged(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-skip-restart-*")
	mock := systemd.InitMockManager()

	mgr := NewManager(Config{
		Systemd:        mock,
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		Local:          true,
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
	})

	// First Start installs and restarts the unit (ReadUnit returns error).
	restarted, err := mgr.Start(context.Background())
	if err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if !restarted {
		t.Fatal("expected restart on first Start")
	}

	// Clear call log.
	mock.Calls = nil

	// Second Start should find the unit unchanged and skip restart.
	restarted, err = mgr.Start(context.Background())
	if err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if restarted {
		t.Fatal("expected no restart when unit is unchanged")
	}

	// Verify no Restart/Start/Stop calls were made.
	for _, c := range mock.GetCalls() {
		if c.Method == "SetStatus" {
			t.Fatalf("unexpected SetStatus call: %v", c)
		}
	}
}

func TestStartRestartsWhenUnitChanged(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-changed-restart-*")
	mock := systemd.InitMockManager()

	mgr := NewManager(Config{
		Systemd:        mock,
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		Local:          true,
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
	})

	// First Start installs the unit.
	if _, err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}

	// Change the public address so the unit content changes.
	mgr.SetPublicAddr("10.0.0.1")

	mock.Calls = nil

	// Second Start should detect the change and restart.
	restarted, err := mgr.Start(context.Background())
	if err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if !restarted {
		t.Fatal("expected restart when public address changed")
	}

	unitName := systemd.SystemServiceUnitName("rolodex")
	hasRestart := false
	for _, c := range mock.GetCalls() {
		if c.Method == "SetStatus" && len(c.Args) >= 2 {
			u, _ := c.Args[0].(string)
			a, _ := c.Args[1].(systemd.StatusAction)
			if u == unitName && a == systemd.Restart {
				hasRestart = true
			}
		}
	}
	if !hasRestart {
		t.Fatalf("expected Restart call for %s", unitName)
	}
}

func TestStopProductionRestoresResolv(t *testing.T) {
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
	if _, err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Clear call log so we only see Stop-phase calls.
	mock.Calls = nil

	if err := mgr.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Stop should NOT call SetStatus(Stop) — the Rolodex unit is a boot
	// service and is not stopped by the systemcontroller.
	for _, c := range mock.GetCalls() {
		if c.Method == "SetStatus" && len(c.Args) >= 2 {
			action, _ := c.Args[1].(systemd.StatusAction)
			if action == systemd.Stop {
				t.Fatal("Stop() should not stop the rolodex unit")
			}
		}
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

func TestConfigureResolvedMDNS(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-mdns-*")
	mock := systemd.InitMockManager()

	resolvedDir := filepath.Join(dir, "resolved.conf.d")
	mgr := NewManager(Config{
		Systemd:         mock,
		DataDir:         filepath.Join(dir, "data"),
		Image:           "quay.io/town/rolodex:latest",
		Local:           true,
		UnixSocketPath:  filepath.Join(dir, DefaultGRPCSocket),
		ResolvedConfDir: resolvedDir,
	})

	if _, err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Verify the drop-in was written.
	dropIn, err := os.ReadFile(filepath.Join(resolvedDir, "townos.conf"))
	if err != nil {
		t.Fatalf("read drop-in: %v", err)
	}
	content := string(dropIn)
	if !strings.Contains(content, "MulticastDNS=yes") {
		t.Errorf("drop-in missing MulticastDNS=yes:\n%s", content)
	}
	if !strings.Contains(content, "DNSStubListener=yes") {
		t.Errorf("drop-in missing DNSStubListener=yes:\n%s", content)
	}
}

func TestConfigureResolvedMDNSSkippedWhenEmpty(t *testing.T) {
	dir := rolodexTestDir(t, "rolodex-mdns-skip-*")
	mock := systemd.InitMockManager()

	mgr := NewManager(Config{
		Systemd:        mock,
		DataDir:        filepath.Join(dir, "data"),
		Image:          "quay.io/town/rolodex:latest",
		Local:          true,
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
	})

	if _, err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// No ResolvedConfDir set — no drop-in directory should be created.
	resolvedDir := filepath.Join(dir, "resolved.conf.d")
	if _, err := os.Stat(resolvedDir); err == nil {
		t.Fatal("resolved.conf.d should not exist when ResolvedConfDir is empty")
	}
}

func TestInterfaceForIP(t *testing.T) {
	// Empty and invalid inputs should return "".
	if got := interfaceForIP(""); got != "" {
		t.Errorf("interfaceForIP(\"\") = %q, want \"\"", got)
	}
	if got := interfaceForIP("not-an-ip"); got != "" {
		t.Errorf("interfaceForIP(\"not-an-ip\") = %q, want \"\"", got)
	}

	// Loopback (127.0.0.1) should always resolve to an interface.
	if got := interfaceForIP("127.0.0.1"); got == "" {
		t.Error("interfaceForIP(\"127.0.0.1\") returned \"\", want loopback interface")
	}

	// Non-existent IP should return "".
	if got := interfaceForIP("198.51.100.99"); got != "" {
		t.Errorf("interfaceForIP(\"198.51.100.99\") = %q, want \"\"", got)
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

	if _, err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
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

func TestUnitConfigsResolvConfDirectives(t *testing.T) {
	dataDir := t.TempDir()

	t.Run("production mode includes ExecStartPre and ExecStopPost", func(t *testing.T) {
		mgr := NewManager(Config{
			Systemd:        systemd.InitMockManager(),
			DataDir:        dataDir,
			Image:          "quay.io/town/rolodex:testing",
			ResolvConfPath: "/etc/resolv.conf",
			PublicAddr:     "192.168.1.1",
		})

		cfgs := mgr.unitConfigs()
		if len(cfgs) != 1 {
			t.Fatalf("expected 1 unit config, got %d", len(cfgs))
		}
		cfg := cfgs[0]

		if len(cfg.ExecStartPre) != 1 {
			t.Fatalf("expected 1 ExecStartPre, got %d", len(cfg.ExecStartPre))
		}
		if !strings.Contains(cfg.ExecStartPre[0], DNSLoopback) {
			t.Fatalf("ExecStartPre should reference %s, got %q", DNSLoopback, cfg.ExecStartPre[0])
		}
		if !strings.Contains(cfg.ExecStartPre[0], "/etc/resolv.conf") {
			t.Fatalf("ExecStartPre should reference resolv.conf, got %q", cfg.ExecStartPre[0])
		}

		if len(cfg.ExecStopPost) != 2 {
			t.Fatalf("expected 2 ExecStopPost commands, got %d", len(cfg.ExecStopPost))
		}
		if !strings.Contains(cfg.ExecStopPost[0], "stub-resolv.conf") {
			t.Fatalf("ExecStopPost[0] should restore resolved stub, got %q", cfg.ExecStopPost[0])
		}
		if !strings.Contains(cfg.ExecStopPost[1], "systemd-resolved") {
			t.Fatalf("ExecStopPost[1] should restart resolved, got %q", cfg.ExecStopPost[1])
		}
	})

	t.Run("local mode omits resolv directives", func(t *testing.T) {
		mgr := NewManager(Config{
			Systemd:        systemd.InitMockManager(),
			DataDir:        dataDir,
			Image:          "quay.io/town/rolodex:testing",
			Local:          true,
			ResolvConfPath: "/etc/resolv.conf",
		})

		cfgs := mgr.unitConfigs()
		cfg := cfgs[0]

		if len(cfg.ExecStartPre) != 0 {
			t.Fatalf("local mode should not have ExecStartPre, got %v", cfg.ExecStartPre)
		}
		if len(cfg.ExecStopPost) != 0 {
			t.Fatalf("local mode should not have ExecStopPost, got %v", cfg.ExecStopPost)
		}
	})

	t.Run("no resolv path omits directives", func(t *testing.T) {
		mgr := NewManager(Config{
			Systemd: systemd.InitMockManager(),
			DataDir: dataDir,
			Image:   "quay.io/town/rolodex:testing",
		})

		cfgs := mgr.unitConfigs()
		cfg := cfgs[0]

		if len(cfg.ExecStartPre) != 0 {
			t.Fatalf("should not have ExecStartPre without ResolvConfPath, got %v", cfg.ExecStartPre)
		}
		if len(cfg.ExecStopPost) != 0 {
			t.Fatalf("should not have ExecStopPost without ResolvConfPath, got %v", cfg.ExecStopPost)
		}
	})
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
	t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck // best-effort cleanup

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
		_ = ln.Close() //nolint:errcheck // goroutine cleanup
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

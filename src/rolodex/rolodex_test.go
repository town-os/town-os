package rolodex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	// Local mode should forward DNSLoopback:53 to container port 53.
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

	// Should NOT have remote port mappings.
	for _, arg := range cfg.Args {
		if strings.HasPrefix(arg, "5300:") {
			t.Fatalf("unexpected remote port mapping in local mode: %s", arg)
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

	// Remote mode should have -p port mappings.
	hasPortMapping := false
	for _, arg := range cfg.Args {
		if arg == "5300:53/tcp" || arg == "5300:53/udp" {
			hasPortMapping = true
			break
		}
	}
	if !hasPortMapping {
		t.Fatalf("expected port mappings in remote mode, got args: %v", cfg.Args)
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
	mgr := NewManager(Config{
		Systemd:        systemd.InitMockManager(),
		DataDir:        rolodexTestDir(t, "rolodex-svc-*"),
		Image:          "quay.io/town/rolodex:test",
		UnixSocketPath: "/tmp/rolodex.sock",
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

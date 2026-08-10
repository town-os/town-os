package rolodex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/systemd"
)

// TestRolodexConfigWritesMetricsSection pins the one fact Prometheus ingestion
// depends on: rolodex's metrics endpoint is opt-in upstream and absent by
// default, so if this section stops being written the listener simply never
// starts and the scrape job goes permanently down.
func TestRolodexConfigWritesMetricsSection(t *testing.T) {
	t.Parallel()
	cfg := rolodexConfig(rolodexConfigParams{Port: DefaultDNSPort, Forwarders: DefaultForwarders, Mode: DefaultResolutionMode, MetricsPort: DefaultMetricsPort})
	want := "metrics:\n  bind: \"" + DNSLoopback + ":" + DefaultMetricsPort + "\"\n"
	if !strings.Contains(cfg, want) {
		t.Fatalf("expected metrics section %q, got:\n%s", want, cfg)
	}
}

// TestRolodexConfigMetricsPortOverride covers the harness path: the metrics
// listener lands in the host namespace like every other rolodex bind, so a
// concurrent test run must be able to relocate it — IRON RULE.
func TestRolodexConfigMetricsPortOverride(t *testing.T) {
	t.Parallel()
	cfg := rolodexConfig(rolodexConfigParams{Port: DefaultDNSPort, Forwarders: DefaultForwarders, Mode: DefaultResolutionMode, MetricsPort: "39153"})
	if !strings.Contains(cfg, "\n  bind: \""+DNSLoopback+":39153\"\n") {
		t.Fatalf("expected overridden metrics port, got:\n%s", cfg)
	}
	if strings.Contains(cfg, ":"+DefaultMetricsPort) {
		t.Fatalf("config still references the default metrics port %s:\n%s", DefaultMetricsPort, cfg)
	}
}

// TestRolodexConfigEmptyMetricsPortDefaults asserts the empty string means "use
// the default", the same convention every other port field here follows.
func TestRolodexConfigEmptyMetricsPortDefaults(t *testing.T) {
	t.Parallel()
	cfg := rolodexConfig(rolodexConfigParams{Port: DefaultDNSPort, Forwarders: DefaultForwarders, Mode: DefaultResolutionMode, MetricsPort: ""})
	if !strings.Contains(cfg, "\n  bind: \""+DNSLoopback+":"+DefaultMetricsPort+"\"\n") {
		t.Fatalf("expected default metrics port when unset, got:\n%s", cfg)
	}
}

// TestManagerMetricsAddrMatchesRenderedBind is the anti-drift test. MetricsAddr
// is what the Prometheus scrape target is built from and rolodex.yml is what the
// listener is built from; if the two ever disagree the target is aimed at
// nothing and rolodex reads as down while it is in fact perfectly healthy.
func TestManagerMetricsAddrMatchesRenderedBind(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mgr := NewManager(Config{
		Systemd:        systemd.InitMockManager(),
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
		MetricsPort:    "41234",
	})

	if got, want := mgr.MetricsPort(), "41234"; got != want {
		t.Errorf("MetricsPort() = %q, want %q", got, want)
	}
	addr := mgr.MetricsAddr()
	if want := DNSLoopback + ":41234"; addr != want {
		t.Errorf("MetricsAddr() = %q, want %q", addr, want)
	}

	if _, err := mgr.WriteConfig(); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "rolodex.yml"))
	if err != nil {
		t.Fatalf("read rolodex.yml: %v", err)
	}
	if !strings.Contains(string(raw), "bind: \""+addr+"\"") {
		t.Fatalf("rolodex.yml does not bind MetricsAddr() %q:\n%s", addr, raw)
	}
}

// TestManagerMetricsPortDefaults covers a manager that names no port, which is
// what a production boot with no environment override produces.
func TestManagerMetricsPortDefaults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mgr := NewManager(Config{
		Systemd:        systemd.InitMockManager(),
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
	})
	if got, want := mgr.MetricsPort(), DefaultMetricsPort; got != want {
		t.Errorf("MetricsPort() = %q, want %q", got, want)
	}
	if got, want := mgr.MetricsAddr(), DNSLoopback+":"+DefaultMetricsPort; got != want {
		t.Errorf("MetricsAddr() = %q, want %q", got, want)
	}
}

// TestRewriteConfigCarriesMetricsSection guards the runtime path. RewriteConfig
// is what a resolution-mode change goes through, and it renders the whole file:
// a metrics section written at boot but dropped on rewrite would silently
// disable the endpoint the first time an operator changed an unrelated setting.
func TestRewriteConfigCarriesMetricsSection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mgr := NewManager(Config{
		Systemd:        systemd.InitMockManager(),
		DataDir:        dir,
		Image:          "quay.io/town/rolodex:latest",
		UnixSocketPath: filepath.Join(dir, DefaultGRPCSocket),
	})
	if _, err := mgr.RewriteConfig(); err != nil {
		t.Fatalf("RewriteConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "rolodex.yml"))
	if err != nil {
		t.Fatalf("read rolodex.yml: %v", err)
	}
	if !strings.Contains(string(raw), "metrics:\n  bind: \""+mgr.MetricsAddr()+"\"") {
		t.Fatalf("RewriteConfig dropped the metrics section:\n%s", raw)
	}
}

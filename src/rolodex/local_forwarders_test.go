package rolodex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func localForwarderManager(t *testing.T, cfg Config) (*Manager, string) {
	t.Helper()

	dataDir := t.TempDir()
	cfg.DataDir = dataDir
	return NewManager(cfg), dataDir
}

func readRolodexConfig(t *testing.T, dataDir string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dataDir, "rolodex.yml"))
	if err != nil {
		t.Fatalf("read rolodex.yml: %v", err)
	}
	return string(data)
}

func TestForwardersDefaultToThePublicResolvers(t *testing.T) {
	t.Parallel()

	m, _ := localForwarderManager(t, Config{})
	if strings.Join(m.Forwarders(), ",") != strings.Join(DefaultForwarders, ",") {
		t.Fatalf("Forwarders() = %v, want %v", m.Forwarders(), DefaultForwarders)
	}
	if m.LocalForwarders() {
		t.Fatal("LocalForwarders() = true, want false by default")
	}
}

func TestLocalForwardersReplaceTheForwarderList(t *testing.T) {
	t.Parallel()

	resolv := writeResolvConf(t, "resolv.conf", "nameserver 192.168.4.1\n")
	m, dataDir := localForwarderManager(t, Config{
		LocalForwarders: true,
		ResolvConfPaths: []string{resolv},
	})

	if got := strings.Join(m.Forwarders(), ","); got != "192.168.4.1:53" {
		t.Fatalf("Forwarders() = %v, want [192.168.4.1:53]", m.Forwarders())
	}

	if _, err := m.RewriteConfig(); err != nil {
		t.Fatalf("RewriteConfig: %v", err)
	}
	config := readRolodexConfig(t, dataDir)
	if !strings.Contains(config, `- "192.168.4.1:53"`) {
		t.Fatalf("rolodex.yml does not forward to the local resolver:\n%s", config)
	}
	for _, public := range DefaultForwarders {
		if strings.Contains(config, public) {
			t.Fatalf("rolodex.yml still names the public forwarder %q:\n%s", public, config)
		}
	}
}

// Discovery reads files that may hold nothing usable — a box with no lease yet,
// or one whose only nameserver line is a loopback stub. An empty result must
// keep the forwarders already configured: deleting them would leave the auto
// chain's local tier pointing at nothing, which is strictly worse than the
// public defaults this switch was turned on to replace.
func TestLocalForwardersFallBackWhenDiscoveryFindsNothing(t *testing.T) {
	t.Parallel()

	stub := writeResolvConf(t, "stub-resolv.conf", "nameserver 127.0.0.53\n")
	m, _ := localForwarderManager(t, Config{
		LocalForwarders: true,
		ResolvConfPaths: []string{stub},
		Forwarders:      []string{"9.9.9.9:53"},
	})

	if got := strings.Join(m.Forwarders(), ","); got != "9.9.9.9:53" {
		t.Fatalf("Forwarders() = %v, want the configured [9.9.9.9:53]", m.Forwarders())
	}
}

func TestLocalForwardersFallBackToTheDefaultsWithNothingConfigured(t *testing.T) {
	t.Parallel()

	stub := writeResolvConf(t, "stub-resolv.conf", "nameserver 127.0.0.53\n")
	m, _ := localForwarderManager(t, Config{
		LocalForwarders: true,
		ResolvConfPaths: []string{stub},
	})

	if strings.Join(m.Forwarders(), ",") != strings.Join(DefaultForwarders, ",") {
		t.Fatalf("Forwarders() = %v, want %v", m.Forwarders(), DefaultForwarders)
	}
}

// The forwarder list is orthogonal to the resolution mode: it changes WHICH
// addresses the local tier holds, not whether that tier is consulted. Turning
// it on must not move the mode, which decides that.
func TestLocalForwardersLeaveTheResolutionModeAlone(t *testing.T) {
	t.Parallel()

	resolv := writeResolvConf(t, "resolv.conf", "nameserver 192.168.4.1\n")
	m, dataDir := localForwarderManager(t, Config{
		ResolutionMode:  ResolutionModeAuto,
		LocalForwarders: true,
		ResolvConfPaths: []string{resolv},
	})

	if _, err := m.RewriteConfig(); err != nil {
		t.Fatalf("RewriteConfig: %v", err)
	}
	if !strings.Contains(readRolodexConfig(t, dataDir), "  mode: "+ResolutionModeAuto) {
		t.Fatalf("rolodex.yml does not keep mode %q:\n%s", ResolutionModeAuto, readRolodexConfig(t, dataDir))
	}
	if m.ResolutionMode() != ResolutionModeAuto {
		t.Fatalf("ResolutionMode() = %q, want %q", m.ResolutionMode(), ResolutionModeAuto)
	}
}

func TestSetLocalForwardersRewritesTheConfigBothWays(t *testing.T) {
	t.Parallel()

	resolv := writeResolvConf(t, "resolv.conf", "nameserver 192.168.4.1\n")
	m, dataDir := localForwarderManager(t, Config{ResolvConfPaths: []string{resolv}})

	if _, err := m.RewriteConfig(); err != nil {
		t.Fatalf("RewriteConfig: %v", err)
	}

	m.SetLocalForwarders(true)
	written, err := m.RewriteConfig()
	if err != nil {
		t.Fatalf("RewriteConfig on: %v", err)
	}
	if !written {
		t.Fatal("RewriteConfig reported no change when switching to local forwarders")
	}
	if !strings.Contains(readRolodexConfig(t, dataDir), `- "192.168.4.1:53"`) {
		t.Fatalf("rolodex.yml did not pick up the local resolver:\n%s", readRolodexConfig(t, dataDir))
	}

	// And back: an operator who turns this off must get the public resolvers
	// again, not be stuck on a network's resolver they have since left.
	m.SetLocalForwarders(false)
	written, err = m.RewriteConfig()
	if err != nil {
		t.Fatalf("RewriteConfig off: %v", err)
	}
	if !written {
		t.Fatal("RewriteConfig reported no change when switching back")
	}
	if !strings.Contains(readRolodexConfig(t, dataDir), DefaultForwarders[0]) {
		t.Fatalf("rolodex.yml did not return to the public forwarders:\n%s", readRolodexConfig(t, dataDir))
	}
}

// An unchanged render must report no change, because the caller restarts
// rolodex on the strength of that boolean — a rewrite that always claimed a
// change would bounce DNS for the whole box on every settings write.
func TestRewriteConfigReportsNoChangeWhenTheRenderIsIdentical(t *testing.T) {
	t.Parallel()

	resolv := writeResolvConf(t, "resolv.conf", "nameserver 192.168.4.1\n")
	m, _ := localForwarderManager(t, Config{
		LocalForwarders: true,
		ResolvConfPaths: []string{resolv},
	})

	if _, err := m.RewriteConfig(); err != nil {
		t.Fatalf("RewriteConfig: %v", err)
	}
	written, err := m.RewriteConfig()
	if err != nil {
		t.Fatalf("RewriteConfig again: %v", err)
	}
	if written {
		t.Fatal("RewriteConfig reported a change for an identical render")
	}
}

package rolodex

import (
	"os"
	"path/filepath"
	"testing"
)

// A rolodex.yml as the install image writes it on a real box
// (scripts/rolodex-config.sh in ../install): both loopbacks plus the host's
// routable address, and the DHCP gateway as the local forwarder. None of it is
// derivable from this package — the bind list comes from enumerating the host's
// own addresses, which only the installer can do.
const scriptWrittenConfig = `database_path: /data/rolodex.db
dns:
  bind:
    - udp: "127.0.0.2:53"
    - tcp: "127.0.0.2:53"
    - udp: "[::1]:53"
    - tcp: "[::1]:53"
    - udp: "192.168.122.50:53"
    - tcp: "192.168.122.50:53"
grpc:
  tcp_bind: ""
  unix_socket: /data/rolodex.sock
  shared_secret: ""
forwarders:
  - "192.168.122.1:53"
resolution:
  mode: auto
rbl:
  enabled: false
  providers: []
metrics:
  bind: "127.0.0.2:9153"
`

// TestManagerNeverWritesTheConfigFile is the invariant this package is now
// built on: rolodex.yml belongs to the install image, and nothing here writes
// it. Settings reach rolodex over gRPC instead (see ProgramRolodex).
//
// It is a test rather than a convention because of what the convention cost
// when it was one. This manager can only render a single hardcoded 127.0.0.2
// bind — it has no way to learn the host's routable address or its second
// loopback — so any code path that re-rendered the file replaced a real box's
// six binds with one, and everything on the network resolving through
// 192.168.122.50 stopped. The trigger was a blocklist change: an operator
// toggling a DNSBL in the UI silently took DNS off the LAN, because saving a
// blocklist rewrote the whole file.
//
// Writing the file at all is therefore the bug, not writing it wrongly. The
// setters below are every mutation Town OS performs on this manager; none of
// them may touch the disk.
func TestManagerNeverWritesTheConfigFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "rolodex.yml")
	if err := os.WriteFile(configPath, []byte(scriptWrittenConfig), 0o644); err != nil {
		t.Fatalf("plant the install image's config: %v", err)
	}

	m := NewManager(Config{DataDir: dir})
	mutateEverySetting(m)

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after mutations: %v", err)
	}
	if string(got) != scriptWrittenConfig {
		t.Errorf("the install image's rolodex.yml was modified.\n--- want ---\n%s\n--- got ---\n%s",
			scriptWrittenConfig, got)
	}
}

// The counterpart: with no file to preserve, none may be created either.
//
// Asserted separately because the two fail for different reasons. A writer that
// merges into an existing file passes the test above and still fails here, and
// a fresh box is exactly where a "just render the defaults" path looks harmless
// — right up until it renders one 127.0.0.2 bind onto a box the installer was
// about to configure properly.
func TestManagerCreatesNoConfigFileOnAFreshBox(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(Config{DataDir: dir})
	mutateEverySetting(m)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read data dir: %v", err)
	}
	for _, e := range entries {
		t.Errorf("this package created %q in the rolodex data dir; only the install image writes there", e.Name())
	}
}

// mutateEverySetting exercises every setter Town OS calls on the manager. It is
// shared by both tests so a newly added setter has to be added in one place to
// be covered by both — and so the list itself reads as the answer to "what can
// change here".
func mutateEverySetting(m *Manager) {
	m.SetResolutionMode(ResolutionModeForward)
	m.SetLocalForwarders(true)
	m.SetBlocklist(Blocklist{
		Enabled:   true,
		Providers: []BlocklistProvider{{Zone: "dbl.spamhaus.org", Enabled: true}},
	})
}

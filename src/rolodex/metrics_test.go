// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package rolodex

import (
	"net"
	"testing"
)

// TestManagerMetricsAddrDefaults pins the address Prometheus is pointed at.
//
// This is the one setting with two independent writers that can never ask each
// other what they chose: the install image writes `metrics.bind` into
// rolodex.yml (the listener is opened once at startup from that section, so
// there is no runtime call that can move it), and Town OS writes the scrape
// target from MetricsAddr. If this default ever changes on one side only, the
// scrape target points at a closed port and every DNS panel draws an empty
// chart rather than an error — which is exactly how it shipped once already.
func TestManagerMetricsAddrDefaults(t *testing.T) {
	t.Parallel()

	m := NewManager(Config{})
	want := net.JoinHostPort(DNSLoopback, DefaultMetricsPort)
	if got := m.MetricsAddr(); got != want {
		t.Errorf("MetricsAddr() = %q, want %q", got, want)
	}
	if want != "127.0.0.2:9153" {
		t.Errorf("the default metrics address moved to %q; scripts/rolodex-config.sh in ../install hardcodes 127.0.0.2:9153 and must move with it", want)
	}
}

// TestManagerMetricsPortOverride covers the harness case: the rolodex container
// runs --net host, so a test box and a `make dev` box on the same machine would
// otherwise fight over 9153 and one would crash-loop under Restart=always.
func TestManagerMetricsPortOverride(t *testing.T) {
	t.Parallel()

	m := NewManager(Config{MetricsPort: "39153"})
	if got, want := m.MetricsPort(), "39153"; got != want {
		t.Errorf("MetricsPort() = %q, want %q", got, want)
	}
	if got, want := m.MetricsAddr(), net.JoinHostPort(DNSLoopback, "39153"); got != want {
		t.Errorf("MetricsAddr() = %q, want %q", got, want)
	}
}

// TestManagerMetricsPortDefaults asserts an unset port falls back rather than
// rendering an address with an empty port, which would be a scrape target that
// can never connect.
func TestManagerMetricsPortDefaults(t *testing.T) {
	t.Parallel()

	m := NewManager(Config{})
	if got, want := m.MetricsPort(), DefaultMetricsPort; got != want {
		t.Errorf("MetricsPort() = %q, want %q", got, want)
	}
	if _, port, err := net.SplitHostPort(m.MetricsAddr()); err != nil || port == "" {
		t.Errorf("MetricsAddr() = %q, which does not split into host and port (%v)", m.MetricsAddr(), err)
	}
}

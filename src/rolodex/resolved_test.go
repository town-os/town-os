package rolodex

import (
	"context"
	"testing"
)

func TestConfigureResolvedRoutingSkipsEmptyTLD(t *testing.T) {
	t.Parallel()
	// Should not panic or do anything when TLD is empty.
	ConfigureResolvedRouting(context.Background(), "", "127.0.0.2")
}

func TestConfigureResolvedRoutingSkipsEmptyAddr(t *testing.T) {
	t.Parallel()
	// Should not panic or do anything when loopback addr is empty.
	ConfigureResolvedRouting(context.Background(), "home", "")
}

func TestConfigureResolvedRoutingNonFatal(t *testing.T) {
	t.Parallel()
	// Even if /etc/systemd/resolved.conf.d is not writable or systemctl
	// is unavailable, the function must not panic — it logs and returns.
	// In the test environment, writing to /etc/systemd/ will likely fail
	// (non-root), which exercises the non-fatal error path.
	ConfigureResolvedRouting(context.Background(), "home", "127.0.0.2")
}

func TestConfigureResolvedRoutingRespectsContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	// Must not panic with a cancelled context.
	ConfigureResolvedRouting(ctx, "home", "127.0.0.2")
}

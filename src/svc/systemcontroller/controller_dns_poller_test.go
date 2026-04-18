// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestDNSPollerIntervalDefault locks the hourly default in place — any
// change to "push once an hour" behaviour needs to land deliberately, not
// via a constant rename.
func TestDNSPollerIntervalDefault(t *testing.T) {
	s := &serverBase{}
	if got := s.pollerDNSIntervalValue(); got != time.Hour {
		t.Fatalf("default DNS poller interval = %v, want 1h", got)
	}
}

// TestDNSPollerIntervalOverride asserts the override field takes
// precedence over the default. Tests that drive tickDNSPoll can crank
// this down to zero or a tiny value to iterate fast.
func TestDNSPollerIntervalOverride(t *testing.T) {
	s := &serverBase{pollerDNSInterval: 250 * time.Millisecond}
	if got := s.pollerDNSIntervalValue(); got != 250*time.Millisecond {
		t.Fatalf("overridden DNS poller interval = %v, want 250ms", got)
	}
}

// TestTickDNSPollCallsInjectedReconciler confirms tickDNSPoll routes
// through pollerDNSReconciler (the test seam) rather than the real
// rolodex client when the seam is set. This is how every other test
// exercises the poller without standing up a rolodex instance.
func TestTickDNSPollCallsInjectedReconciler(t *testing.T) {
	var calls atomic.Int32
	s := &serverBase{
		pollerDNSReconciler: func(_ context.Context) error {
			calls.Add(1)
			return nil
		},
	}
	s.tickDNSPoll(context.Background())
	s.tickDNSPoll(context.Background())
	if got := calls.Load(); got != 2 {
		t.Fatalf("pollerDNSReconciler called %d times, want 2", got)
	}
}

// TestTickDNSPollSwallowsError asserts a reconciler error does not
// propagate out of the tick — the poller is a best-effort drift repair
// and a transient rolodex hiccup must not crash the goroutine (next
// tick gets another shot).
func TestTickDNSPollSwallowsError(t *testing.T) {
	s := &serverBase{
		pollerDNSReconciler: func(_ context.Context) error {
			return errors.New("rolodex down")
		},
	}
	// Would panic if tickDNSPoll didn't catch the error.
	s.tickDNSPoll(context.Background())
}

// TestTickDNSPollNoRolodexClientIsNoOp guards the real-rolodex branch:
// when GetRolodexClient returns nil (rolodex not yet up, or test with
// no seam injected), the tick must not panic or block.
func TestTickDNSPollNoRolodexClientIsNoOp(t *testing.T) {
	s := &serverBase{}
	// No pollerDNSReconciler seam; GetRolodexClient returns nil because
	// we didn't set s.RolodexSocketPath. Should be a clean no-op.
	s.tickDNSPoll(context.Background())
}

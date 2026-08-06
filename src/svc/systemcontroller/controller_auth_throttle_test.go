// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

// POST /account/authenticate is public and each attempt costs one argon2id
// hash at 64 MiB. Unthrottled, that is both an online password oracle and an
// unauthenticated way to schedule unbounded memory allocation on a box designed
// to run from RAM.

func TestLoginLimiterAllowsUpToMaxThenRefuses(t *testing.T) {
	l := newLoginLimiter()
	l.max = 3

	for i := range l.max {
		if !l.allow("10.0.0.1") {
			t.Fatalf("attempt %d refused, want allowed", i+1)
		}
	}
	if l.allow("10.0.0.1") {
		t.Fatal("attempt past the limit was allowed")
	}
}

// A refused attempt still counts. Otherwise being blocked drains the window and
// the limit degrades into a duty cycle the attacker sets.
func TestLoginLimiterRefusedAttemptsStillCount(t *testing.T) {
	base := time.Now()
	l := newLoginLimiter()
	l.max = 2
	l.now = func() time.Time { return base }

	for range 5 {
		l.allow("10.0.0.1")
	}

	// One second later, still inside the window: the earlier attempts — all of
	// them, refused included — must still be holding the source out.
	l.now = func() time.Time { return base.Add(time.Second) }
	if l.allow("10.0.0.1") {
		t.Fatal("source was let back in while its window was still full")
	}
}

func TestLoginLimiterWindowExpires(t *testing.T) {
	base := time.Now()
	l := newLoginLimiter()
	l.max = 2
	l.window = time.Minute
	l.now = func() time.Time { return base }

	for range 3 {
		l.allow("10.0.0.1")
	}

	l.now = func() time.Time { return base.Add(2 * time.Minute) }
	if !l.allow("10.0.0.1") {
		t.Fatal("source still refused after its window elapsed")
	}
}

// One abusive client must not lock out the household.
func TestLoginLimiterIsPerSource(t *testing.T) {
	l := newLoginLimiter()
	l.max = 2

	for range 5 {
		l.allow("10.0.0.1")
	}
	if !l.allow("10.0.0.2") {
		t.Fatal("a different source was refused because of another's attempts")
	}
}

func TestLoginLimiterResetOnSuccess(t *testing.T) {
	l := newLoginLimiter()
	l.max = 2

	l.allow("10.0.0.1")
	l.allow("10.0.0.1")
	l.reset("10.0.0.1")

	if !l.allow("10.0.0.1") {
		t.Fatal("source still refused after a successful authentication reset it")
	}
}

func TestLoginLimiterSweepsIdleSources(t *testing.T) {
	base := time.Now()
	l := newLoginLimiter()
	l.window = time.Minute
	l.now = func() time.Time { return base }

	for i := range loginSweepEvery {
		l.allow(string(rune('a'+i%26)) + string(rune('a'+i/26)))
	}
	// Everything recorded so far is now outside the window; the next sweep
	// should drop it rather than growing the map forever.
	l.now = func() time.Time { return base.Add(2 * time.Minute) }
	for range loginSweepEvery {
		l.allow("survivor")
	}

	l.mu.Lock()
	size := len(l.attempts)
	l.mu.Unlock()
	if size > 1 {
		t.Fatalf("limiter holds %d sources after a sweep, want only the active one", size)
	}
}

func TestLoginGateBoundsConcurrency(t *testing.T) {
	g := newLoginGate()
	g.wait = 50 * time.Millisecond

	for i := range loginMaxConcurrent {
		if !g.acquire() {
			t.Fatalf("slot %d not acquired, want the gate to admit %d", i+1, loginMaxConcurrent)
		}
	}
	if g.acquire() {
		t.Fatal("gate admitted more than loginMaxConcurrent hashes")
	}

	g.release()
	if !g.acquire() {
		t.Fatal("gate did not admit a waiter after a slot was released")
	}
}

func TestLoginGateReleasesUnderConcurrentUse(t *testing.T) {
	g := newLoginGate()
	g.wait = 2 * time.Second

	var wg sync.WaitGroup
	for range loginMaxConcurrent * 3 {
		wg.Go(func() {
			if g.acquire() {
				g.release()
			}
		})
	}
	wg.Wait()

	// Every slot must be free again: a leaked one would wedge every future
	// login on the box until a restart.
	for i := range loginMaxConcurrent {
		if !g.acquire() {
			t.Fatalf("slot %d unavailable after all holders released", i+1)
		}
	}
}

// End to end through the router. A nonexistent username costs no hash
// (Authenticate returns before verifying), so this drives the limiter without
// paying 21 × 64 MiB.
func TestAuthenticateRateLimited(t *testing.T) {
	c, _ := initAccountTestClient(t)

	body := `{"username":"nobody","password":"whatever1"}`
	for i := range loginMaxAttempts {
		code, respBody := postStatus(t, c, "account/authenticate", body)
		if code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d (%s), want 401", i+1, code, respBody)
		}
	}

	code, respBody := postStatus(t, c, "account/authenticate", body)
	if code != http.StatusTooManyRequests {
		t.Fatalf("attempt past the limit = %d (%s), want 429", code, respBody)
	}

	// The refusal must not leak which of the two limits was hit, or whether
	// the username exists.
	if _, err := c.Authenticate(context.TODO(), "testadmin", "adminpass"); err == nil {
		t.Fatal("a valid login succeeded while the source was rate-limited")
	}
}

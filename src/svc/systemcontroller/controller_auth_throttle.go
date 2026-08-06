package systemcontroller

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Login throttling.
//
// POST /account/authenticate is public and every attempt costs one argon2id
// hash at 64 MiB (account.argonMemory). That is the right cost for a password
// hash and the wrong thing to let an unauthenticated caller schedule without
// limit: a few hundred concurrent attempts is tens of gigabytes of allocation
// on a box whose whole design point is running from RAM, and the failure is not
// a slow login — it is the OOM killer taking the controller.
//
// Two independent limits, because they answer different questions:
//
//   - loginLimiter caps ATTEMPTS per source over a window. It is what makes
//     online password guessing infeasible, and it is keyed per source address
//     so one abusive client cannot lock out the household.
//   - loginGate caps CONCURRENT hashes across all sources. It is what bounds
//     peak memory regardless of how many distinct sources are attempting, which
//     the per-source limiter alone cannot do.
//
// Both are deliberately in-memory and per-process: they protect this process's
// memory and CPU, a restart is not an attack vector worth defending here, and
// persisting them would make a failed login a database write.

const (
	// loginWindow and loginMaxAttempts allow a household's worth of typos and
	// a genuine "which password was it" session, while capping a guesser at
	// well under one attempt per second sustained.
	loginWindow      = 5 * time.Minute
	loginMaxAttempts = 20

	// loginMaxConcurrent bounds peak argon2 memory at roughly
	// loginMaxConcurrent × 64 MiB. Four keeps that near a quarter gigabyte
	// while still letting a family log in simultaneously.
	loginMaxConcurrent = 4

	// loginGateWait is how long a request waits for a hashing slot before
	// being turned away. Long enough to absorb a burst, short enough that a
	// flood cannot pile up unbounded goroutines holding request state.
	loginGateWait = 5 * time.Second

	// loginSweepEvery bounds the limiter's map growth: every N recorded
	// attempts it drops sources whose window has fully elapsed. Sweeping on a
	// counter rather than a timer keeps this free of a goroutine that would
	// outlive the server in tests.
	loginSweepEvery = 256
)

// loginLimiter is a fixed-window attempt counter keyed by source address.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	sweepIn  int

	// now is overridable so tests can advance the window without sleeping.
	now func() time.Time
	// window and max are overridable so tests need not make 20 requests.
	window time.Duration
	max    int
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		attempts: map[string][]time.Time{},
		now:      time.Now,
		window:   loginWindow,
		max:      loginMaxAttempts,
	}
}

// allow records an attempt from key and reports whether it may proceed.
//
// The attempt is recorded even when it is refused, so a caller that keeps
// hammering keeps its window full rather than draining it by being blocked —
// otherwise the limit degrades into a duty cycle.
func (l *loginLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	cutoff := now.Add(-l.window)

	kept := l.attempts[key][:0]
	for _, at := range l.attempts[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	kept = append(kept, now)
	l.attempts[key] = kept

	l.sweepIn++
	if l.sweepIn >= loginSweepEvery {
		l.sweepIn = 0
		l.sweepLocked(cutoff)
	}

	return len(kept) <= l.max
}

// reset clears a source's window. Called on a successful authentication: the
// caller has proved it is not guessing, and a household sharing one NAT address
// should not accumulate toward a lockout through ordinary use.
func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// sweepLocked drops sources with no attempt still inside the window.
func (l *loginLimiter) sweepLocked(cutoff time.Time) {
	for key, times := range l.attempts {
		if len(times) == 0 || !times[len(times)-1].After(cutoff) {
			delete(l.attempts, key)
		}
	}
}

// loginGate is a counting semaphore bounding concurrent password hashes.
type loginGate struct {
	slots chan struct{}
	wait  time.Duration
}

func newLoginGate() *loginGate {
	return &loginGate{
		slots: make(chan struct{}, loginMaxConcurrent),
		wait:  loginGateWait,
	}
}

// acquire takes a hashing slot, returning false if none frees up in time.
func (g *loginGate) acquire() bool {
	timer := time.NewTimer(g.wait)
	defer timer.Stop()
	select {
	case g.slots <- struct{}{}:
		return true
	case <-timer.C:
		return false
	}
}

func (g *loginGate) release() { <-g.slots }

// loginThrottle returns the per-handler-set throttle, built on first use so a
// handler set constructed without one (every test server) still works.
func (s *SystemControllerHandlers) loginThrottle() (*loginLimiter, *loginGate) {
	s.loginOnce.Do(func() {
		s.loginLimiterStore = newLoginLimiter()
		s.loginGateStore = newLoginGate()
	})
	return s.loginLimiterStore, s.loginGateStore
}

// loginSourceKey identifies the client for rate-limiting purposes.
//
// RemoteAddr, not X-Forwarded-For: the controller is reached directly rather
// than through a proxy, and trusting a header here would let an attacker mint a
// fresh identity per request and erase the limit entirely.
func loginSourceKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

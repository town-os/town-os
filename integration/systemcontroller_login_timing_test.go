// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// Authentication failures are deliberately indistinguishable in their error
// text -- wrong password, unknown user, and disabled account all surface as
// "invalid credentials" -- specifically to prevent username enumeration.
//
// The timing is not. SQLiteManager.Authenticate returns before hashing on both
// of the non-password paths:
//
//	acct, err := m.Get(username)
//	if err != nil {
//	    return nil, ErrInvalidCredentials     // no argon2 runs
//	}
//	if acct.Disabled {
//	    return nil, ErrAccountDisabled        // no argon2 runs
//	}
//	if !verifyPassword(acct.PasswordHash, password) {   // argon2id, 64 MiB
//
// So a real, enabled username costs one argon2id at m=64MiB, and anything else
// costs a SQLite point lookup. That is roughly a thousandfold difference and it
// is measurable over the network in a single request -- which hands back
// exactly the answer the uniform error strings withhold, including which
// accounts exist and which of them have been disabled.
//
// The login limiter (20 attempts / 5 min / source) slows an enumeration sweep;
// it does not close the channel, and it does not apply to the first attempt
// against any given name.
//
// The fix is to hash against a fixed dummy argon2id hash on both early-return
// paths so every failure pays the same cost.
//
// These tests assert the SECURE behaviour and fail against the current code.

const (
	// timingSamples is per measured group. The statistic is the MEDIAN, so a
	// scheduler hiccup or a noisy neighbour in a parallel `make test-full` moves
	// one sample and not the result.
	timingSamples = 9

	// timingMinRatio is how close a non-password failure must come to a
	// password failure. A correct implementation lands at ~1.0; the current one
	// lands around 0.001. A quarter is a deliberately loose bar -- it is far
	// above anything an implementation that actually hashes could fail, and far
	// below anything the current short-circuit could reach.
	timingMinRatio = 0.25
)

// medianDuration returns the median of the samples, which is the right summary
// for a latency distribution with a long right tail.
func medianDuration(samples []time.Duration) time.Duration {
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	return sorted[len(sorted)/2]
}

// timeCalls runs fn timingSamples times and returns the median wall time. fn is
// expected to fail; a success means the fixture is wrong, not that the timing
// is interesting.
func timeCalls(t *testing.T, label string, fn func() error) time.Duration {
	t.Helper()
	samples := make([]time.Duration, 0, timingSamples)
	for range timingSamples {
		start := time.Now()
		err := fn()
		samples = append(samples, time.Since(start))
		if err == nil {
			t.Fatalf("%s: expected an authentication failure, got success", label)
		}
	}
	median := medianDuration(samples)
	t.Logf("%s: median %v over %d samples", label, median, timingSamples)
	return median
}

// assertIndistinguishable fails when `other` is measurably cheaper than the
// wrong-password baseline.
func assertIndistinguishable(t *testing.T, baseline, other time.Duration, what string) {
	t.Helper()
	if baseline <= 0 {
		t.Fatal("baseline measured as zero; the fixture is not exercising argon2")
	}
	ratio := float64(other) / float64(baseline)
	if ratio < timingMinRatio {
		t.Errorf("%s is distinguishable by timing: %v vs %v for a wrong password (%.4f of baseline, want >= %.2f). "+
			"An unauthenticated caller can enumerate accounts one request at a time despite the uniform error text.",
			what, other, baseline, ratio, timingMinRatio)
	}
}

func initLoginTimingManager(t *testing.T) *account.SQLiteManager {
	t.Helper()

	db, err := account.OpenDB(filepath.Join(t.TempDir(), "timing.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("db.Close: %v", cerr)
		}
	})

	mgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	if _, err := mgr.Create("known", "knownpass1", "k@test.com", "555-0000", "Known", false); err != nil {
		t.Fatalf("Create known: %v", err)
	}
	if _, err := mgr.Create("shutoff", "shutoffpass1", "s@test.com", "555-0001", "Shut Off", false); err != nil {
		t.Fatalf("Create shutoff: %v", err)
	}
	if err := mgr.Disable("shutoff"); err != nil {
		t.Fatalf("Disable shutoff: %v", err)
	}

	return mgr
}

// TestAuthenticateDoesNotLeakAccountExistenceByTiming drives the real argon2id
// path against a real SQLite store, with no HTTP layer in the way, so the
// measurement is of the credential check itself rather than of request
// overhead.
func TestAuthenticateDoesNotLeakAccountExistenceByTiming(t *testing.T) {
	t.Parallel()
	mgr := initLoginTimingManager(t)

	baseline := timeCalls(t, "existing account, wrong password", func() error {
		_, err := mgr.Authenticate("known", "wrongpassword1")
		return err
	})

	unknown := timeCalls(t, "nonexistent account", func() error {
		_, err := mgr.Authenticate("no-such-account", "wrongpassword1")
		return err
	})

	assertIndistinguishable(t, baseline, unknown, "a nonexistent username")
}

// A disabled account short-circuits one line further down, before
// verifyPassword, so it is distinguishable from an enabled one the same way --
// which tells an attacker not just that the account exists but that it has been
// turned off, and that is the account worth attacking on some other surface.
func TestAuthenticateDoesNotLeakDisabledAccountsByTiming(t *testing.T) {
	t.Parallel()
	mgr := initLoginTimingManager(t)

	baseline := timeCalls(t, "existing account, wrong password", func() error {
		_, err := mgr.Authenticate("known", "wrongpassword1")
		return err
	})

	disabled := timeCalls(t, "disabled account", func() error {
		_, err := mgr.Authenticate("shutoff", "wrongpassword1")
		return err
	})

	assertIndistinguishable(t, baseline, disabled, "a disabled account")
}

// Same oracle, over the wire on the public route, to show it is not an artifact
// of calling the manager directly.
//
// The sample count is halved because POST /account/authenticate is rate limited
// to 20 attempts per 5 minutes per source and every request here comes from
// loopback. 2x5 = 10 failures stays inside that window with room to spare; a
// successful login would reset it, but deliberately is not used, since the
// resets would themselves perturb the measurement.
func TestAuthenticateEndpointDoesNotLeakAccountExistenceByTiming(t *testing.T) {
	t.Parallel()

	db, err := account.OpenDB(filepath.Join(t.TempDir(), "timing-http.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("db.Close: %v", cerr)
		}
	})

	mgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	sessMgr, err := account.InitSessionManager(db, mgr, []byte("test-signing-key-for-sessions-32"))
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:    storage.InitBtrFSMock(),
		AccountMgr: mgr,
		SessionMgr: sessMgr,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if _, err := c.CreateAccount(context.TODO(), "known", "knownpass1", "k@test.com", "555-0000", "Known", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}

	errRateLimited := errors.New("rate limited")

	// attempt posts one login and reports failure. A 429 is surfaced distinctly
	// so an exhausted window is reported as a broken fixture rather than being
	// silently folded into the timing.
	attempt := func(username string) error {
		body := `{"username":` + jsonQuote(t, username) + `,"password":"definitelywrong1"}`
		req, rerr := http.NewRequestWithContext(context.TODO(), http.MethodPost,
			c.BaseURL+"/account/authenticate", strings.NewReader(body))
		if rerr != nil {
			t.Fatalf("build authenticate request: %v", rerr)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, rerr := c.HTTP.Do(req)
		if rerr != nil {
			t.Fatalf("POST account/authenticate: %v", rerr)
		}
		defer func() {
			if cerr := resp.Body.Close(); cerr != nil {
				t.Errorf("close body: %v", cerr)
			}
		}()
		if _, cerr := io.Copy(io.Discard, resp.Body); cerr != nil {
			t.Errorf("drain body: %v", cerr)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			return errRateLimited
		}
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		return errors.New(resp.Status)
	}

	const httpSamples = 5
	measure := func(label, username string) time.Duration {
		samples := make([]time.Duration, 0, httpSamples)
		for range httpSamples {
			start := time.Now()
			err := attempt(username)
			samples = append(samples, time.Since(start))
			if errors.Is(err, errRateLimited) {
				t.Fatalf("%s: login window exhausted mid-measurement; lower httpSamples", label)
			}
			if err == nil {
				t.Fatalf("%s: expected an authentication failure, got success", label)
			}
		}
		median := medianDuration(samples)
		t.Logf("%s: median %v over %d samples", label, median, httpSamples)
		return median
	}

	baseline := measure("POST /account/authenticate, existing account", "known")
	unknown := measure("POST /account/authenticate, nonexistent account", "no-such-account")

	assertIndistinguishable(t, baseline, unknown, "a nonexistent username over HTTP")
}

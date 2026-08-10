// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- parseLogLevel ---

func TestParseLogLevelDebug(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debug")
	if got := parseLogLevel(); got != slog.LevelDebug {
		t.Fatalf("expected LevelDebug, got %v", got)
	}
}

func TestParseLogLevelInfo(t *testing.T) {
	t.Setenv("LOG_LEVEL", "info")
	if got := parseLogLevel(); got != slog.LevelInfo {
		t.Fatalf("expected LevelInfo, got %v", got)
	}
}

func TestParseLogLevelWarn(t *testing.T) {
	t.Setenv("LOG_LEVEL", "warn")
	if got := parseLogLevel(); got != slog.LevelWarn {
		t.Fatalf("expected LevelWarn, got %v", got)
	}
}

func TestParseLogLevelWarning(t *testing.T) {
	t.Setenv("LOG_LEVEL", "warning")
	if got := parseLogLevel(); got != slog.LevelWarn {
		t.Fatalf("expected LevelWarn for 'warning', got %v", got)
	}
}

func TestParseLogLevelError(t *testing.T) {
	t.Setenv("LOG_LEVEL", "error")
	if got := parseLogLevel(); got != slog.LevelError {
		t.Fatalf("expected LevelError, got %v", got)
	}
}

func TestParseLogLevelDefault(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")
	if got := parseLogLevel(); got != slog.LevelError {
		t.Fatalf("expected LevelError for empty, got %v", got)
	}
}

func TestParseLogLevelCaseInsensitive(t *testing.T) {
	t.Setenv("LOG_LEVEL", "DEBUG")
	if got := parseLogLevel(); got != slog.LevelDebug {
		t.Fatalf("expected LevelDebug for uppercase, got %v", got)
	}
}

func TestParseLogLevelUnknown(t *testing.T) {
	t.Setenv("LOG_LEVEL", "trace")
	if got := parseLogLevel(); got != slog.LevelError {
		t.Fatalf("expected LevelError for unknown, got %v", got)
	}
}

// --- fetchExternalIP ---

func TestFetchExternalIPSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"ip": "203.0.113.42"}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	s := &serverBase{externalIPURL: srv.URL}
	if ok := s.fetchAndStoreExternalIP(context.Background()); !ok {
		t.Fatal("expected fetchAndStoreExternalIP to succeed")
	}
	if ip := s.GetExternalIP(); ip != "203.0.113.42" {
		t.Fatalf("expected 203.0.113.42, got %q", ip)
	}
}

func TestFetchExternalIPBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := &serverBase{externalIPURL: srv.URL}
	if ok := s.fetchAndStoreExternalIP(context.Background()); ok {
		t.Fatal("expected fetchAndStoreExternalIP to fail on 500")
	}
	if ip := s.GetExternalIP(); ip != "" {
		t.Fatalf("expected empty IP after failed fetch, got %q", ip)
	}
}

func TestFetchExternalIPEmptyIPField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(`{"ip":""}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()

	s := &serverBase{externalIPURL: srv.URL}
	if ok := s.fetchAndStoreExternalIP(context.Background()); ok {
		t.Fatal("expected fetchAndStoreExternalIP to return false when ip is empty")
	}
	if ip := s.GetExternalIP(); ip != "" {
		t.Fatalf("expected empty IP when server returned empty, got %q", ip)
	}
}

func TestFetchExternalIPMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("not json")); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()

	s := &serverBase{externalIPURL: srv.URL}
	if ok := s.fetchAndStoreExternalIP(context.Background()); ok {
		t.Fatal("expected fetchAndStoreExternalIP to fail on malformed JSON")
	}
}

func TestFetchExternalIPWarnLogFiresOnceDuringFailureStreak(t *testing.T) {
	// Only the FIRST failure while external_ip is uncached should surface
	// at Warn. Subsequent failures in the same streak drop to Debug so the
	// hourly-poll-while-DNS-is-broken scenario doesn't fill the journal.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	handler := &levelCountingHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(prev)

	s := &serverBase{externalIPURL: srv.URL}
	if ok := s.fetchAndStoreExternalIP(context.Background()); ok {
		t.Fatal("expected failure on first attempt")
	}
	if got := handler.warnCount(); got != 1 {
		t.Fatalf("expected 1 Warn log after first failure, got %d", got)
	}
	// Second failure in a row: still no successful fetch, but we already
	// logged Warn once — subsequent failures should stay at Debug.
	if ok := s.fetchAndStoreExternalIP(context.Background()); ok {
		t.Fatal("expected failure on second attempt")
	}
	if got := handler.warnCount(); got != 1 {
		t.Fatalf("expected Warn count to stay at 1 after second failure, got %d", got)
	}
}

func TestFetchExternalIPTransientFailureWhileCachedStaysQuiet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	handler := &levelCountingHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(prev)

	s := &serverBase{externalIPURL: srv.URL}
	// Seed a cached value so subsequent failures are "transient" and quiet.
	s.externalIP.Store("192.0.2.99")
	if ok := s.fetchAndStoreExternalIP(context.Background()); ok {
		t.Fatal("expected failure")
	}
	if got := handler.warnCount(); got != 0 {
		t.Fatalf("expected no Warn when cached IP is present, got %d", got)
	}
}

func TestFetchExternalIPWithStartupBackoffSucceedsOnSecondAttempt(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if _, err := w.Write([]byte(`{"ip":"203.0.113.7"}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()

	s := &serverBase{
		externalIPURL: srv.URL,
		// Shrink the waits so the test runs in milliseconds, not minutes.
		externalIPStartupBackoffs: []time.Duration{0, time.Millisecond},
	}
	s.fetchExternalIPWithStartupBackoff(context.Background())
	if ip := s.GetExternalIP(); ip != "203.0.113.7" {
		t.Fatalf("expected backoff to pick up second attempt, got %q", ip)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("expected exactly 2 attempts, got %d", got)
	}
}

func TestFetchExternalIPWithStartupBackoffExitsOnContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	s := &serverBase{
		externalIPURL: srv.URL,
		// One immediate attempt, then a long wait that should never fire
		// because we cancel ctx.
		externalIPStartupBackoffs: []time.Duration{0, time.Hour},
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.fetchExternalIPWithStartupBackoff(ctx)
	}()
	// Give the first attempt time to fail, then cancel before the 1h wait.
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("fetchExternalIPWithStartupBackoff did not exit after ctx cancel")
	}
}

// levelCountingHandler is a minimal slog.Handler that counts records at
// each level. Used by fetchExternalIP log-level tests.
type levelCountingHandler struct {
	mu     sync.Mutex
	counts map[slog.Level]int
}

func (h *levelCountingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *levelCountingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.counts == nil {
		h.counts = map[slog.Level]int{}
	}
	h.counts[r.Level]++
	return nil
}
func (h *levelCountingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *levelCountingHandler) WithGroup(_ string) slog.Handler      { return h }
func (h *levelCountingHandler) warnCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.counts[slog.LevelWarn]
}

// --- startNetworkPoller ---

func TestStartNetworkPollerContextCancellation(t *testing.T) {
	s := &serverBase{}
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately to test that the goroutine exits cleanly.
	cancel()
	s.startNetworkPoller(ctx)

	// Allow goroutine to exit.
	time.Sleep(100 * time.Millisecond)
}

// --- NewHandler ---

func TestNewHandlerStartsNetworkPoller(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// NewHandler should not panic and should start the network poller.
	// The actual ipinfo.io fetch will either succeed or timeout gracefully.
	// AuthDisabled is explicit because this config has no session manager and
	// NewHandler refuses to serve an unauthenticated box by accident.
	handler, err := NewHandler(ctx, ServerConfig{AuthDisabled: true})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	if handler == nil {
		t.Fatal("expected non-nil handler from NewHandler")
	}

	// Cancel to clean up the poller goroutine.
	cancel()
	time.Sleep(100 * time.Millisecond)
}

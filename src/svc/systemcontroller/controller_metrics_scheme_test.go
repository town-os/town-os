// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// probeContext bounds a probe test so a hung dial fails as this test rather
// than as the package's timeout.
func probeContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// hostPort reduces a test server's base URL to the "host:port" a listen
// address is.
func hostPort(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u.Host
}

// closedListenerAddr returns an address nothing is listening on: an ephemeral
// port is bound to learn its number, then released. Never a fixed port — a
// concurrent test-full run holds its own. IRON RULE.
func closedListenerAddr(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close probe listener: %v", err)
	}
	return addr
}

// A cleartext listener must be reported as cleartext even when the boot
// believed it had configured TLS. This is the deployed failure: prometheus.yml
// carried `scheme: https` against a plain :5309, so every scrape of the
// controller failed with "server gave HTTP response to HTTPS client" and every
// townos_* metric was exported and then dropped.
func TestListenerSpeaksTLSReportsCleartextListener(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	if ListenerSpeaksTLS(probeContext(t), hostPort(t, srv.URL), true) {
		t.Error("a plain HTTP listener was reported as speaking TLS")
	}
}

// The reverse must hold too, or the operator who turned TLS on loses the
// scrape instead of gaining a secure one.
func TestListenerSpeaksTLSReportsTLSListener(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// configured=false: the answer must come from the handshake, not from the
	// caller's belief. TOWN_OS_TLS_CERT/KEY terminate TLS without going through
	// the variable the old derivation read, and this is what covers that.
	if !ListenerSpeaksTLS(probeContext(t), hostPort(t, srv.URL), false) {
		t.Error("a TLS listener was reported as cleartext")
	}
}

// Nothing accepted the connection, so there is no socket to ask. Falling back
// to the boot's own view beats emitting no scheme: the job is emitted either
// way, and a guessed scheme can at least be right.
func TestListenerSpeaksTLSFallsBackWhenUnreachable(t *testing.T) {
	t.Parallel()

	addr := closedListenerAddr(t)
	ctx := probeContext(t)

	if !ListenerSpeaksTLS(ctx, addr, true) {
		t.Error("unreachable listener did not fall back to the configured true")
	}
	if ListenerSpeaksTLS(ctx, addr, false) {
		t.Error("unreachable listener did not fall back to the configured false")
	}
}

// An underivable -listen has no target to probe, and MetricsScrapeTarget
// already omits the job for it. Returning the fallback keeps the two decisions
// from disagreeing about what an unparseable address means.
func TestListenerSpeaksTLSFallsBackWithoutATarget(t *testing.T) {
	t.Parallel()

	ctx := probeContext(t)
	for _, addr := range []string{"", "not-an-address"} {
		if !ListenerSpeaksTLS(ctx, addr, true) {
			t.Errorf("ListenerSpeaksTLS(%q, true) = false, want the fallback", addr)
		}
		if ListenerSpeaksTLS(ctx, addr, false) {
			t.Errorf("ListenerSpeaksTLS(%q, false) = true, want the fallback", addr)
		}
	}
}

// A cancelled context must not hang the boot on its own listener.
func TestListenerSpeaksTLSHonoursContextCancellation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// The fallback, since a cancelled probe learned nothing about the socket.
	if !ListenerSpeaksTLS(ctx, hostPort(t, srv.URL), true) {
		t.Error("a cancelled probe did not fall back to the configured value")
	}
}

// The probe repairs the scrape silently, which would hide the one case that is
// worse than a missing metric: an operator who asked for TLS and is serving the
// login password in the clear.
func TestSchemeDisagreementReportsOnlyRealDisagreements(t *testing.T) {
	t.Parallel()

	if msg := SchemeDisagreement(true, true); msg != "" {
		t.Errorf("agreeing TLS reported a disagreement: %q", msg)
	}
	if msg := SchemeDisagreement(false, false); msg != "" {
		t.Errorf("agreeing cleartext reported a disagreement: %q", msg)
	}
	if msg := SchemeDisagreement(false, true); msg == "" {
		t.Error("configured TLS against a cleartext listener reported nothing")
	}
	if msg := SchemeDisagreement(true, false); msg == "" {
		t.Error("an unconfigured TLS listener reported nothing")
	}
}

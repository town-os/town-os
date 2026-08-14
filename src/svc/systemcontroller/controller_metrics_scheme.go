// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	cryptotls "crypto/tls"
	"errors"
	"log/slog"
	"net"
	"time"
)

// listenerProbeTimeout bounds the whole probe — dial plus handshake. It is
// short because the target is this process's own listener on the loopback: a
// connection that has not been answered in this long is not going to be, and
// the boot has better things to do than wait for it.
const listenerProbeTimeout = 3 * time.Second

// ListenerSpeaksTLS reports whether the controller's own listener answers a TLS
// handshake, by opening one against it.
//
// The scrape scheme is asked of the socket rather than derived a second time
// from the environment because deriving it twice is what broke. prometheus.yml
// gets `scheme: https` from a boolean captured at boot; the listener gets TLS
// from ControllerTLSRequested() at a different point in the same boot. The two
// are supposed to agree, and on a deployed box they did not: Prometheus opened
// TLS to a cleartext :5309 and failed every scrape with "server gave HTTP
// response to HTTPS client", for the life of the config. Nothing detected it —
// `insecure_skip_verify` does not help, because the problem is that there is no
// handshake at all — and the only trace was a job sitting `down` inside
// Prometheus's own target list, which nothing on the box surfaces. Every
// townos_* metric was exported and dropped on the floor while the stack
// reported itself healthy.
//
// A handshake cannot disagree with the socket. It is also the only test that
// covers the operator-supplied-certificate path (TOWN_OS_TLS_CERT /
// TOWN_OS_TLS_KEY) and anything else that terminates TLS without going through
// the variable this used to read.
//
// configured is the fallback for the one case with no answer: nothing accepted
// the connection, so there is no socket to ask, and the boot's own view of what
// it set up is the best guess available. A wrong guess there costs the
// controller scrape job; refusing to emit one costs it too, and silently.
func ListenerSpeaksTLS(ctx context.Context, listenAddr string, configured bool) bool {
	target := MetricsScrapeTarget(listenAddr)
	if target == "" {
		return configured
	}

	probeCtx, cancel := context.WithTimeout(ctx, listenerProbeTimeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(probeCtx, "tcp", target)
	if err != nil {
		slog.Warn("could not reach our own listener to determine its scrape scheme",
			"target", target, "assuming_tls", configured, "error", err)
		return configured
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			slog.Debug("close listener scheme probe", "target", target, "error", closeErr)
		}
	}()

	host, _, splitErr := net.SplitHostPort(target)
	if splitErr != nil {
		// Unreachable: MetricsScrapeTarget builds target with JoinHostPort.
		host = ""
	}

	// InsecureSkipVerify because the question is "does this socket speak TLS",
	// not "is its certificate trustworthy" — the leaf is issued by the box's own
	// CA, which this process would have to hand itself to verify. The connection
	// is torn down immediately and carries no request.
	tlsConn := cryptotls.Client(conn, &cryptotls.Config{
		InsecureSkipVerify: true, //nolint:gosec // G402 -- probing for a handshake, not trusting the peer; see above
		ServerName:         host,
		MinVersion:         cryptotls.VersionTLS12,
	})
	if err := tlsConn.HandshakeContext(probeCtx); err != nil {
		// A cleartext http.Server reads the ClientHello as a malformed request
		// line and answers 400, which is not a TLS record — so the handshake
		// fails fast and this is the normal, expected path on a plain listener.
		slog.Debug("listener did not complete a TLS handshake; scraping it as http",
			"target", target, "error", err)
		return false
	}
	return true
}

// SchemeDisagreement returns a message describing a listener whose observed
// scheme is not the one the boot configured, or "" when the two agree.
//
// It exists so the disagreement is reported ONCE, loudly, at the point it is
// discovered. The scrape itself is repaired either way — the probe wins — but a
// box serving cleartext when the operator asked for TLS is a security problem
// the repaired scrape would otherwise hide completely.
func SchemeDisagreement(observedTLS, configuredTLS bool) string {
	switch {
	case observedTLS == configuredTLS:
		return ""
	case configuredTLS:
		return "TLS was configured for the control plane listener but it answers cleartext; " +
			"scraping it over http and serving requests unencrypted"
	default:
		return "the control plane listener answers TLS although none was configured; scraping it over https"
	}
}

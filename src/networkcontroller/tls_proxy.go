// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package networkcontroller

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"sync"
	"time"
)

// tlsForwarder runs an in-process TLS listener on a single external port.
// Incoming TLS connections are terminated with the leaf cert + key read from
// certDir and their plaintext is proxied bidirectionally to target:internal
// over the shared podman network. No HTTP parsing happens here — the proxy
// is protocol-agnostic so WebSockets, BOSH long-polling, gRPC and raw TLS
// tunnels all pass through unchanged.
//
// One tlsForwarder instance serves exactly one external port. It owns its
// goroutine and is torn down via Close, which stops the Accept loop and
// interrupts any in-flight copies. Socat-based forwarders have a retry
// channel wired into the controller's Run loop; the TLS path does not need
// one because tls.Listen + net.Listen failures surface synchronously from
// startTLSForwarder, and the Accept loop logs and continues on transient
// per-connection errors.
type tlsForwarder struct {
	ext      uint16
	target   string
	intPort  uint16
	certDir  string
	listener net.Listener

	closeOnce sync.Once
	closed    chan struct{}
}

// tlsProxyDialTimeout bounds each upstream connect. The parent and the
// dependency containers live on the same podman network so dialing should be
// fast; a short timeout keeps failed upstreams from tying up accept slots.
const tlsProxyDialTimeout = 5 * time.Second

// startTLSForwarder binds ext on all interfaces with a TLS config whose
// GetCertificate re-reads cert.pem/key.pem on every handshake. This makes
// cert rotation observable without needing an explicit reload path —
// reconcile can overwrite the files and the next client handshake picks up
// the new pair. The target container is resolved by DNS on each upstream
// dial, not at listener start, so container restarts that change IPs still
// reach the right upstream.
func startTLSForwarder(ext uint16, target string, intPort uint16, certDir string) (*tlsForwarder, error) {
	if certDir == "" {
		return nil, errors.New("tls: cert dir is empty")
	}

	f := &tlsForwarder{
		ext:     ext,
		target:  target,
		intPort: intPort,
		certDir: certDir,
		closed:  make(chan struct{}),
	}

	cfg := &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: f.getCertificate,
	}

	ln, err := tls.Listen("tcp", fmt.Sprintf(":%d", ext), cfg)
	if err != nil {
		return nil, fmt.Errorf("tls listen %d: %w", ext, err)
	}
	f.listener = ln

	go f.acceptLoop()
	return f, nil
}

// getCertificate is called from each ClientHello. It re-reads cert.pem/key.pem
// from certDir so a reconcile that rewrites the leaf is picked up without a
// listener restart.
func (f *tlsForwarder) getCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	certPath := filepath.Join(f.certDir, "cert.pem")
	keyPath := filepath.Join(f.certDir, "key.pem")
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load leaf cert %s: %w", f.certDir, err)
	}
	return &pair, nil
}

// acceptLoop pulls TLS connections off the listener and hands each to a
// proxy goroutine. A permanent listener error (listener closed) exits the
// loop; transient per-connection errors are logged and skipped.
func (f *tlsForwarder) acceptLoop() {
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			select {
			case <-f.closed:
				return
			default:
			}
			// The only way Accept keeps failing forever is a wedged
			// listener; log once and exit so reconcile can retry on
			// the next state-file write.
			slog.Error(fmt.Sprintf("tls accept %d: %v", f.ext, err))
			return
		}
		go f.proxy(conn)
	}
}

// proxy handles one terminated TLS connection: dial the upstream container
// in plaintext, then shuttle bytes in both directions until one side
// closes. net.Pipe-style full-duplex copies are used so long-lived streams
// (WebSockets, XMPP BOSH) do not stall waiting for the other direction.
func (f *tlsForwarder) proxy(client net.Conn) {
	defer func() {
		if err := client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			slog.Debug(fmt.Sprintf("tls close client %d: %v", f.ext, err))
		}
	}()

	dialCtx, cancel := context.WithTimeout(context.Background(), tlsProxyDialTimeout)
	defer cancel()

	d := net.Dialer{}
	upstream, err := d.DialContext(dialCtx, "tcp", fmt.Sprintf("%s:%d", f.target, f.intPort))
	if err != nil {
		slog.Debug(fmt.Sprintf("tls dial %s:%d: %v", f.target, f.intPort, err))
		return
	}
	defer func() {
		if err := upstream.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			slog.Debug(fmt.Sprintf("tls close upstream %d: %v", f.ext, err))
		}
	}()

	// Drive both directions concurrently. Closing either side on EOF
	// causes the paired io.Copy to unblock.
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, client) //nolint:errcheck // tunnel errors are expected at stream end
		// Best-effort half-close so the upstream sees EOF promptly.
		if cw, ok := upstream.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite() //nolint:errcheck
		}
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream) //nolint:errcheck // see above
		if cw, ok := client.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite() //nolint:errcheck
		}
		done <- struct{}{}
	}()
	<-done
	<-done
}

// Close stops the listener and interrupts any in-flight Accept. In-flight
// proxy goroutines finish naturally when their underlying connection is
// closed (either by the peer or by process exit).
func (f *tlsForwarder) Close() error {
	var err error
	f.closeOnce.Do(func() {
		close(f.closed)
		if f.listener != nil {
			if cerr := f.listener.Close(); cerr != nil && !errors.Is(cerr, net.ErrClosed) {
				err = cerr
			}
		}
	})
	return err
}

// Addr returns the local address the listener is bound to. Used by tests
// that pick an ephemeral port so they can dial it directly.
func (f *tlsForwarder) Addr() net.Addr {
	if f.listener == nil {
		return nil
	}
	return f.listener.Addr()
}

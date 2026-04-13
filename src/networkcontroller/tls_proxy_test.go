// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package networkcontroller

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/upnp"
)

// --- Stub TLS starter for reconcile-path tests ---

type stubTLS struct {
	ext      uint16
	target   string
	intPort  uint16
	certPath string
	closed   atomic.Bool
}

func (s *stubTLS) Close() error {
	s.closed.Store(true)
	return nil
}

type stubTLSStarter struct {
	mu    sync.Mutex
	calls []*stubTLS
}

func (s *stubTLSStarter) start(ext uint16, target string, intPort uint16, certPath string) (TLSListener, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := &stubTLS{ext: ext, target: target, intPort: intPort, certPath: certPath}
	s.calls = append(s.calls, c)
	return c, nil
}

func (s *stubTLSStarter) Calls() []*stubTLS {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*stubTLS, len(s.calls))
	copy(out, s.calls)
	return out
}

func TestReconcileStartsTLSListenerInsteadOfSocat(t *testing.T) {
	runner := newMockRunner()
	stub := &stubTLSStarter{}
	ctrl := NewControllerWithRunnerAndTarget(&upnp.MockManager{}, runner, "town-os-package--test-nginx-1.0")
	ctrl.SetTLSStarter(stub.start)

	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{
				ExternalPort: 8443,
				InternalPort: 80,
				UPnP:         true,
				Forward:      true,
				TLS:          true,
				CertPath:     "/etc/town-os/tls/leaves/test/nginx/1.0",
			},
		},
	})

	if calls := runner.GetCalls(); len(calls) != 0 {
		t.Fatalf("expected 0 socat calls on TLS port, got %d", len(calls))
	}
	tlsCalls := stub.Calls()
	if len(tlsCalls) != 1 {
		t.Fatalf("expected 1 TLS start, got %d", len(tlsCalls))
	}
	c := tlsCalls[0]
	if c.ext != 8443 || c.intPort != 80 {
		t.Errorf("unexpected TLS ports: ext=%d int=%d", c.ext, c.intPort)
	}
	if c.target != "town-os-package--test-nginx-1.0" {
		t.Errorf("unexpected TLS target: %q", c.target)
	}
	if c.certPath != "/etc/town-os/tls/leaves/test/nginx/1.0" {
		t.Errorf("unexpected cert path: %q", c.certPath)
	}

	if len(ctrl.GetForwarders()) != 1 {
		t.Errorf("want 1 forwarder tracked, got %d", len(ctrl.GetForwarders()))
	}
}

func TestReconcileRebuildsTLSOnCertPathChange(t *testing.T) {
	runner := newMockRunner()
	stub := &stubTLSStarter{}
	ctrl := NewControllerWithRunnerAndTarget(&upnp.MockManager{}, runner, "town-os-package--test-nginx-1.0")
	ctrl.SetTLSStarter(stub.start)

	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8443, InternalPort: 80, UPnP: false, Forward: true, TLS: true, CertPath: "/etc/town-os/tls/leaves/a"},
		},
	})

	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8443, InternalPort: 80, UPnP: false, Forward: true, TLS: true, CertPath: "/etc/town-os/tls/leaves/b"},
		},
	})

	calls := stub.Calls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 TLS starts (initial + rebuild), got %d", len(calls))
	}
	if !calls[0].closed.Load() {
		t.Error("first TLS forwarder was not closed on cert path change")
	}
	if calls[1].certPath != "/etc/town-os/tls/leaves/b" {
		t.Errorf("rebuild cert path = %q, want leaves/b", calls[1].certPath)
	}
}

func TestReconcileSwapsTLSToSocat(t *testing.T) {
	runner := newMockRunner()
	stub := &stubTLSStarter{}
	ctrl := NewControllerWithRunnerAndTarget(&upnp.MockManager{}, runner, "town-os-package--test-nginx-1.0")
	ctrl.SetTLSStarter(stub.start)

	// Start with TLS.
	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8443, InternalPort: 80, Forward: true, TLS: true, CertPath: "/etc/tls/a"},
		},
	})
	// Remove TLS but keep the port (e.g. package switched supplies off).
	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8443, InternalPort: 80, Forward: true},
		},
	})

	if !stub.Calls()[0].closed.Load() {
		t.Error("TLS forwarder not closed when TLS flag cleared")
	}
	if calls := runner.GetCalls(); len(calls) != 1 || calls[0].Name != "/usr/bin/socat" {
		t.Errorf("expected socat to take over, got %+v", runner.GetCalls())
	}
}

// --- End-to-end TLS proxy with real listener ---

func TestTLSForwarderEndToEndRoundTrip(t *testing.T) {
	// Upstream: an echo server that reads a line and writes it back uppercase.
	lc := net.ListenConfig{}
	upstreamLn, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("upstream listen: %v", err)
	}
	defer func() {
		if err := upstreamLn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Logf("upstream close: %v", err)
		}
	}()
	go func() {
		for {
			conn, err := upstreamLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }() //nolint:errcheck // test cleanup
				r := bufio.NewReader(c)
				line, err := r.ReadString('\n')
				if err != nil && err != io.EOF {
					return
				}
				if _, err := io.WriteString(c, "ECHO:"+line); err != nil {
					return
				}
			}(conn)
		}
	}()

	_, upstreamPortStr, err := net.SplitHostPort(upstreamLn.Addr().String())
	if err != nil {
		t.Fatalf("split upstream addr: %v", err)
	}
	var upstreamPort uint16
	if _, err := fmt.Sscanf(upstreamPortStr, "%d", &upstreamPort); err != nil {
		t.Fatalf("parse upstream port: %v", err)
	}

	// Issue a leaf cert pointing at 127.0.0.1.
	caCert, caKey := generateTestCA(t)
	certDir := t.TempDir()
	writeTestLeaf(t, certDir, caCert, caKey, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})

	// Start the proxy bound to an ephemeral port.
	fwd, err := startTLSForwarder(0, "127.0.0.1", upstreamPort, certDir)
	if err != nil {
		t.Fatalf("startTLSForwarder: %v", err)
	}
	defer func() {
		if err := fwd.Close(); err != nil {
			t.Logf("close fwd: %v", err)
		}
	}()

	// Dial with a client that trusts our CA.
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 2 * time.Second},
		Config: &tls.Config{
			RootCAs:    pool,
			ServerName: "127.0.0.1",
			MinVersion: tls.VersionTLS12,
		},
	}
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	rawConn, err := dialer.DialContext(dialCtx, "tcp", fwd.Addr().String())
	if err != nil {
		t.Fatalf("tls dial: %v", err)
	}
	conn, ok := rawConn.(*tls.Conn)
	if !ok {
		t.Fatalf("expected *tls.Conn, got %T", rawConn)
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck // test cleanup

	if _, err := io.WriteString(conn, "hello\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	if err := conn.SetReadDeadline(deadline); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(buf[:n])
	if got != "ECHO:hello\n" {
		t.Errorf("roundtrip got %q, want %q", got, "ECHO:hello\n")
	}
}

func TestTLSForwarderReloadsCertOnHandshake(t *testing.T) {
	// Use a throwaway upstream so the proxy has something to dial.
	lc := net.ListenConfig{}
	upstreamLn, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("upstream listen: %v", err)
	}
	defer func() {
		if err := upstreamLn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Logf("upstream close: %v", err)
		}
	}()
	go func() {
		for {
			conn, err := upstreamLn.Accept()
			if err != nil {
				return
			}
			_ = conn.Close() //nolint:errcheck // test cleanup
		}
	}()
	_, portStr, err := net.SplitHostPort(upstreamLn.Addr().String())
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	var upstreamPort uint16
	if _, err := fmt.Sscanf(portStr, "%d", &upstreamPort); err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Write cert A, handshake once with CA A, then overwrite with cert B
	// signed by CA B and handshake again. The second handshake must succeed
	// against CA B — proving getCertificate re-reads the file on every
	// client hello.
	caA, keyA := generateTestCA(t)
	caB, keyB := generateTestCA(t)
	certDir := t.TempDir()
	writeTestLeaf(t, certDir, caA, keyA, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})

	fwd, err := startTLSForwarder(0, "127.0.0.1", upstreamPort, certDir)
	if err != nil {
		t.Fatalf("startTLSForwarder: %v", err)
	}
	defer func() { _ = fwd.Close() }() //nolint:errcheck // test cleanup

	poolA := x509.NewCertPool()
	poolA.AddCert(caA)
	if err := tryHandshake(fwd.Addr().String(), poolA); err != nil {
		t.Fatalf("handshake A: %v", err)
	}

	// Rewrite with CA B.
	writeTestLeaf(t, certDir, caB, keyB, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})

	poolB := x509.NewCertPool()
	poolB.AddCert(caB)
	if err := tryHandshake(fwd.Addr().String(), poolB); err != nil {
		t.Fatalf("handshake B (after cert rewrite): %v", err)
	}
}

func tryHandshake(addr string, pool *x509.CertPool) error {
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 2 * time.Second},
		Config: &tls.Config{
			RootCAs:    pool,
			ServerName: "127.0.0.1",
			MinVersion: tls.VersionTLS12,
		},
	}
	dialCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

// --- Test cert helpers (kept in this file so NC tests can stand alone) ---

func generateTestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("ca serial: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("ca create: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ca parse: %v", err)
	}
	return cert, key
}

func writeTestLeaf(t *testing.T, dir string, ca *x509.Certificate, caKey *ecdsa.PrivateKey, dns []string, ips []net.IP) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("leaf serial: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "leaf"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dns,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("leaf create: %v", err)
	}
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	caDER := ca.Raw
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("leaf key marshal: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	chain := append([]byte{}, leafPEM...)
	chain = append(chain, caPEM...)
	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), chain, 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "key.pem"), keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
}

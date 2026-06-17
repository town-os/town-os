// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package ingress

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/caddysup"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// TestIngressRoutesTLSToBackend boots the real ingress (Server + a caddy child
// on an ephemeral port) against a temp local-CA leaf and a stub HTTP backend,
// then verifies that a programmed route terminates TLS and reverse-proxies to
// the backend, and that removing the route stops serving it. The test is fully
// isolated (ephemeral port, temp socket-free Server, temp certs) per the IRON
// RULE and skips when no caddy binary is available.
func TestIngressRoutesTLSToBackend(t *testing.T) {
	caddyBin := findCaddy(t)

	// Temp local CA + leaf for test.local.
	tlsDir := t.TempDir()
	ca, err := townostls.EnsureCA(tlsDir)
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	leafDir := filepath.Join(tlsDir, "leaf")
	if err := os.MkdirAll(leafDir, 0o755); err != nil { //nolint:gosec // test dir
		t.Fatal(err)
	}
	if err := ca.IssueLeaf(leafDir, []string{"test.local"}); err != nil {
		t.Fatalf("IssueLeaf: %v", err)
	}

	// Stub HTTP backend the ingress will reverse-proxy to.
	const body = "hello from backend"
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer backend.Close()
	backendAddr := strings.TrimPrefix(backend.URL, "http://")

	port := freePort(t)
	sup := caddysup.NewSupervisor(caddyBin, filepath.Join(t.TempDir(), "Caddyfile"))
	srv := NewServer(sup, port)
	if err := srv.Bootstrap(); err != nil {
		t.Fatalf("bootstrap caddy: %v", err)
	}
	defer func() { _ = sup.Shutdown() }()

	ctx := context.Background()
	if _, err := srv.SetRoutes(ctx, &ingresspb.SetRoutesRequest{Routes: []*ingresspb.Route{
		{Hostname: "test.local", Backend: backendAddr, CertDir: leafDir},
	}}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	client := caClient(t, filepath.Join(tlsDir, "ca.crt"), port)

	// Poll until caddy is serving the route (reload + bind is near-instant but
	// not guaranteed synchronous).
	if !poll(t, func() bool {
		resp, err := client.Get("https://test.local/")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode == http.StatusOK && string(b) == body
	}) {
		t.Fatal("ingress did not serve the backend through the programmed route")
	}

	// Remove the route → caddy reloads with no sites and stops serving it.
	if _, err := srv.RemoveRoute(ctx, &ingresspb.RemoveRouteRequest{Hostname: "test.local"}); err != nil {
		t.Fatalf("RemoveRoute: %v", err)
	}
	if !poll(t, func() bool {
		resp, err := client.Get("https://test.local/")
		if err != nil {
			return true // connection refused once the port is released
		}
		defer resp.Body.Close()
		return resp.StatusCode != http.StatusOK
	}) {
		t.Fatal("route still served after RemoveRoute")
	}
}

func findCaddy(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("caddy"); err == nil {
		return p
	}
	if _, err := os.Stat(caddysup.DefaultCaddyBinary); err == nil {
		return caddysup.DefaultCaddyBinary
	}
	t.Skip("caddy binary not found; skipping ingress integration test")
	return ""
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// caClient returns an HTTP client that trusts the given CA cert and always
// dials 127.0.0.1:port while presenting SNI for the requested host.
func caClient(t *testing.T, caPath string, port int) *http.Client {
	t.Helper()
	caPEM, err := os.ReadFile(caPath) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read CA: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("append CA cert failed")
	}
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			},
			TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "test.local", MinVersion: tls.VersionTLS12},
		},
	}
}

func poll(t *testing.T, cond func() bool) bool {
	t.Helper()
	for i := 0; i < 50; i++ {
		if cond() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

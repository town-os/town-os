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
	if err := os.MkdirAll(leafDir, 0o755); err != nil {
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
	srv := NewServer(sup, port, freePort(t), "", WithCaddyAdminPort(freePort(t)))
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
		resp, err := httpGet(t, client, "https://test.local/")
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
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
		resp, err := httpGet(t, client, "https://test.local/")
		if err != nil {
			return true // connection refused once the port is released
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode != http.StatusOK
	}) {
		t.Fatal("route still served after RemoveRoute")
	}
}

// TestIngressHTTPPortRouting boots the real ingress (Server + caddy child) with
// both an HTTPS and an HTTP listener on ephemeral ports and a default backend,
// then drives the full :80 Host-routing contract against real caddy:
//
//  1. a page route (ServeHttp) is served directly over plain HTTP,
//  2. a package route (no ServeHttp) is redirected :80 -> :443 and never serves
//     backend content over HTTP,
//  3. a host with no route falls through to the default backend (the UI), and
//  4. HTTPS still terminates TLS per host and proxies to the right backend.
//
// This is the test that proves the mixed `https://host` + `http://host` + bare
// `:80` catch-all config actually loads in caddy (not just renders). Fully
// isolated (ephemeral ports, temp CA) per the IRON RULE; skips without caddy.
func TestIngressHTTPPortRouting(t *testing.T) {
	caddyBin := findCaddy(t)

	tlsDir := t.TempDir()
	ca, err := townostls.EnsureCA(tlsDir)
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	issue := func(host string) string {
		dir := filepath.Join(tlsDir, host)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := ca.IssueLeaf(dir, []string{host}); err != nil {
			t.Fatalf("IssueLeaf %s: %v", host, err)
		}
		return dir
	}
	pageLeaf := issue("page.local")
	pkgLeaf := issue("pkg.local")

	mkBackend := func(body string) string {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, body)
		}))
		t.Cleanup(s.Close)
		return strings.TrimPrefix(s.URL, "http://")
	}
	pageBackend := mkBackend("PAGE-BODY")
	pkgBackend := mkBackend("PKG-BODY")
	uiBackend := mkBackend("UI-BODY")

	httpsPort := freePort(t)
	httpPort := freePort(t)
	sup := caddysup.NewSupervisor(caddyBin, filepath.Join(t.TempDir(), "Caddyfile"))
	srv := NewServer(sup, httpsPort, httpPort, uiBackend, WithCaddyAdminPort(freePort(t)))
	if err := srv.Bootstrap(); err != nil {
		t.Fatalf("bootstrap caddy: %v", err)
	}
	defer func() { _ = sup.Shutdown() }()

	ctx := context.Background()
	if _, err := srv.SetRoutes(ctx, &ingresspb.SetRoutesRequest{Routes: []*ingresspb.Route{
		{Hostname: "page.local", Backend: pageBackend, CertDir: pageLeaf, ServeHttp: true},
		{Hostname: "pkg.local", Backend: pkgBackend, CertDir: pkgLeaf},
	}}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	https := caClientSNI(t, filepath.Join(tlsDir, "ca.crt"), httpsPort)
	plain := plainClientNoRedirect(t, httpPort)

	// (1) Page served directly over plain HTTP on :80. Poll here gates readiness:
	// all routes land in one SetRoutes/reload, so once this passes the package
	// redirect and default backend are live too.
	if !poll(t, func() bool {
		resp, err := httpGet(t, plain, "http://page.local/")
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode == http.StatusOK && string(b) == "PAGE-BODY"
	}) {
		t.Fatal("page route not served over plain HTTP on :80")
	}

	// (2) Package on :80 is a redirect to HTTPS — it must not serve content.
	resp, err := httpGet(t, plain, "http://pkg.local/")
	if err != nil {
		t.Fatalf("pkg http GET: %v", err)
	}
	pkgBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		t.Fatalf("package :80 must redirect, got status %d body %q", resp.StatusCode, pkgBody)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "https://pkg.local") {
		t.Fatalf("package :80 redirect Location = %q, want https://pkg.local...", loc)
	}
	if strings.Contains(string(pkgBody), "PKG-BODY") {
		t.Fatalf("package :80 must not serve backend content, got %q", pkgBody)
	}

	// (3) Unmatched host on :80 falls through to the default backend (the UI).
	uiResp, err := httpGet(t, plain, "http://unmatched.invalid/")
	if err != nil {
		t.Fatalf("default-backend http GET: %v", err)
	}
	uiBody, _ := io.ReadAll(uiResp.Body)
	_ = uiResp.Body.Close()
	if uiResp.StatusCode != http.StatusOK || string(uiBody) != "UI-BODY" {
		t.Fatalf("unmatched host must hit default backend, got status %d body %q", uiResp.StatusCode, uiBody)
	}

	// (4) HTTPS still terminates TLS per host and proxies to the right backend.
	for host, want := range map[string]string{"page.local": "PAGE-BODY", "pkg.local": "PKG-BODY"} {
		resp, err := httpGet(t, https, "https://"+host+"/")
		if err != nil {
			t.Fatalf("https %s GET: %v", host, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(body) != want {
			t.Fatalf("https %s: status %d body %q, want %q", host, resp.StatusCode, body, want)
		}
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
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener address type %T", l.Addr())
	}
	return addr.Port
}

// httpGet issues a context-aware GET (lint: noctx requires Do with a request).
func httpGet(t *testing.T, client *http.Client, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

// caClient returns an HTTP client that trusts the given CA cert and always
// dials 127.0.0.1:port while presenting SNI for the requested host.
func caClient(t *testing.T, caPath string, port int) *http.Client {
	t.Helper()
	caPEM, err := os.ReadFile(caPath)
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

// caClientSNI is like caClient but derives SNI from each request's URL host
// (ServerName unset), so one client can reach multiple ingress vhosts. It trusts
// the given CA and always dials 127.0.0.1:port.
func caClientSNI(t *testing.T, caPath string, port int) *http.Client {
	t.Helper()
	caPEM, err := os.ReadFile(caPath)
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
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		},
	}
}

// plainClientNoRedirect returns a plain-HTTP client that always dials
// 127.0.0.1:port and does NOT follow redirects, so a package's :80 -> :443
// redirect can be asserted rather than transparently chased.
func plainClientNoRedirect(t *testing.T, port int) *http.Client {
	t.Helper()
	return &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			},
		},
	}
}

func poll(t *testing.T, cond func() bool) bool {
	t.Helper()
	for range 50 {
		if cond() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// TestIngressProxiesToAnHTTPSBackend is the end-to-end proof that every hop can
// be HTTPS: a route whose backend terminates its own TLS, and a path backend on
// the same vhost doing the same, both served through real caddy.
//
// This is what fronting rolodex's DoH listener depends on. That listener speaks
// TLS with a self-signed certificate, and the ingress reaches it over https with
// verification skipped on the internal hop while the client still validates the
// ingress's own leaf. Until PathBackend carried its own scheme, a path backend
// was proxied as plaintext no matter what stood behind it — a 502 whose cause is
// invisible from either end.
func TestIngressProxiesToAnHTTPSBackend(t *testing.T) {
	caddyBin := findCaddy(t)

	tlsDir := t.TempDir()
	ca, err := townostls.EnsureCA(tlsDir)
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	leafDir := filepath.Join(tlsDir, "leaf")
	if err := os.MkdirAll(leafDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ca.IssueLeaf(leafDir, []string{"dns.local"}); err != nil {
		t.Fatalf("IssueLeaf: %v", err)
	}

	// Two TLS backends with certificates the ingress has no reason to trust —
	// exactly rolodex's self-signed DoH listener, which is why the proxy hop
	// must skip verification rather than fail closed.
	const rootBody = "root over tls"
	const queryBody = "dns-query over tls"
	rootBackend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(rootBody))
	}))
	defer rootBackend.Close()
	queryBackend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The path must arrive unstripped: rolodex serves /dns-query and 404s
		// everything else, so a handle_path here would break every query.
		if r.URL.Path != "/dns-query" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(queryBody))
	}))
	defer queryBackend.Close()

	port := freePort(t)
	sup := caddysup.NewSupervisor(caddyBin, filepath.Join(t.TempDir(), "Caddyfile"))
	srv := NewServer(sup, port, freePort(t), "", WithCaddyAdminPort(freePort(t)))
	if err := srv.Bootstrap(); err != nil {
		t.Fatalf("bootstrap caddy: %v", err)
	}
	defer func() { _ = sup.Shutdown() }()

	ctx := context.Background()
	if _, err := srv.SetRoutes(ctx, &ingresspb.SetRoutesRequest{Routes: []*ingresspb.Route{{
		Hostname:   "dns.local",
		Backend:    strings.TrimPrefix(rootBackend.URL, "https://"),
		CertDir:    leafDir,
		BackendTls: true,
		PathBackends: []*ingresspb.PathBackend{{
			Path:       "/dns-query",
			Backend:    strings.TrimPrefix(queryBackend.URL, "https://"),
			BackendTls: true,
		}},
	}}}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	client := caClient(t, filepath.Join(tlsDir, "ca.crt"), port)

	// The path backend: an HTTPS hop reached through a handle block.
	if !poll(t, func() bool {
		resp, err := httpGet(t, client, "https://dns.local/dns-query")
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode == http.StatusOK && string(b) == queryBody
	}) {
		t.Fatal("ingress did not proxy the path backend over https")
	}

	// The route's own backend: the catch-all handle, also HTTPS.
	resp, err := httpGet(t, client, "https://dns.local/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != rootBody {
		t.Fatalf("route backend over https = %d %q, want 200 %q", resp.StatusCode, body, rootBody)
	}
}

// TestIngressDefaultBackendOverHTTPS covers the :80 fallback vhost — the last
// hop in the renderer that could only speak plaintext, and the only one with no
// test of any kind before this.
//
// The fallback is what a host with no route of its own lands on (the Town OS UI,
// so bare-IP login keeps working). It was rendered with a bare `reverse_proxy`
// rather than through writeReverseProxy, so a backend that terminates its own
// TLS got plaintext sent at a TLS socket. That fails as a 502 with nothing in it
// to name the cause — the same failure PathBackend had, in the one place the
// route-level flag could not reach.
//
// Both arms run, because the bug is a scheme mismatch and only asserting the
// HTTPS one would pass just as well if the flag were ignored and every fallback
// were proxied over https.
func TestIngressDefaultBackendOverHTTPS(t *testing.T) {
	caddyBin := findCaddy(t)

	for _, tc := range []struct {
		name       string
		backendTLS bool
		body       string
	}{
		{name: "https-fallback", backendTLS: true, body: "fallback over tls"},
		{name: "plain-fallback", backendTLS: false, body: "fallback over plaintext"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A backend whose certificate the ingress has no reason to trust,
			// exactly like rolodex's self-signed DoH listener: the internal hop
			// skips verification, so this must still be reached.
			var backend *httptest.Server
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			})
			if tc.backendTLS {
				backend = httptest.NewTLSServer(handler)
			} else {
				backend = httptest.NewServer(handler)
			}
			defer backend.Close()

			addr := backend.URL
			for _, scheme := range []string{"https://", "http://"} {
				addr = strings.TrimPrefix(addr, scheme)
			}

			httpPort := freePort(t)
			sup := caddysup.NewSupervisor(caddyBin, filepath.Join(t.TempDir(), "Caddyfile"))
			srv := NewServer(sup, freePort(t), httpPort, addr,
				WithDefaultBackendTLS(tc.backendTLS), WithCaddyAdminPort(freePort(t)))
			if err := srv.Bootstrap(); err != nil {
				t.Fatalf("bootstrap caddy: %v", err)
			}
			defer func() { _ = sup.Shutdown() }()

			// No routes at all: every host falls through to the bare :80 block,
			// which is the vhost under test.
			client := plainClientNoRedirect(t, httpPort)
			var got string
			if !poll(t, func() bool {
				resp, err := httpGet(t, client, "http://unrouted.invalid/")
				if err != nil {
					return false
				}
				defer func() { _ = resp.Body.Close() }()
				b, readErr := io.ReadAll(resp.Body)
				if readErr != nil {
					return false
				}
				got = string(b)
				return resp.StatusCode == http.StatusOK && got == tc.body
			}) {
				t.Fatalf("default backend (backend_tls=%v) never served its body; last response %q", tc.backendTLS, got)
			}
		})
	}
}

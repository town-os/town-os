// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/ingress/ingressctl"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// TestIntegrationIngressE2ERealContainer is the full data-plane e2e for the
// shipped town-os-ingress image: it boots a REAL ingress container plus a real
// nginx backend on a dedicated podman network, programs a route over gRPC with
// a real local-CA leaf, and verifies that an HTTPS client (trusting the Town OS
// CA) reaches the backend through the ingress on :PORT — then that removing the
// route stops serving it.
//
// HARNESS-ONLY: this needs a real systemd + podman + the loaded INGRESS_IMAGE
// and nginx image, i.e. it only runs inside `make test-integration`. It skips
// cleanly off-harness (gated on INGRESS_IMAGE). Everything is uniquely named
// with an ephemeral host port + dedicated network, so it never collides with
// the booted systemcontroller's production ingress on :443 (IRON RULE).
func TestIntegrationIngressE2ERealContainer(t *testing.T) {
	ingressImage := os.Getenv("INGRESS_IMAGE")
	if ingressImage == "" {
		t.Skip("harness-only: INGRESS_IMAGE unset (run via make test-integration)")
	}
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not available")
	}

	ctx := context.Background()
	uniq := strconv.FormatInt(time.Now().UnixNano(), 36)
	netName := "town-os-ingress-e2e-" + uniq
	backendName := "town-os-ingress-e2e-backend-" + uniq
	const fqdn = "e2e.local.home"
	const backendImage = "docker.io/library/nginx:1.27-alpine"

	// Dedicated podman network so the ingress and backend can resolve each other
	// by container DNS without touching the production ingress network.
	mustPodman(t, "network", "create", netName)
	t.Cleanup(func() { _ = podman("network", "rm", "-f", netName) })

	// Real backend container on the network.
	mustPodman(t, "run", "-d", "--replace", "--name", backendName, "--net", netName, backendImage)
	t.Cleanup(func() { _ = podman("rm", "-f", backendName) })

	// Program the route to the backend's IP on the shared network, not its
	// container name. Production resolves backends by container name over the
	// ingress network's podman DNS (aardvark), but that DNS isn't available in
	// the nested-podman test harness — the ingress container's resolv.conf falls
	// back to a public resolver that can't resolve a container name. The IP is
	// always reachable on the shared network, so this exercises the ingress's
	// real TLS-terminate-and-reverse-proxy data plane without depending on the
	// harness having container DNS.
	backendIP := e2eContainerIP(t, backendName, netName)

	// Local CA + leaf for the test FQDN under the shared TLS subvolume the
	// ingress container mounts read-only at /etc/town-os/tls.
	ca, err := townostls.EnsureCA("/town-os/tls")
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	leafDir := filepath.Join("/town-os/tls/leaves/e2e", uniq)
	if err := os.MkdirAll(leafDir, 0o755); err != nil { //nolint:gosec // served read-only
		t.Fatalf("mkdir leaf: %v", err)
	}
	if err := ca.IssueLeaf(leafDir, []string{fqdn}); err != nil {
		t.Fatalf("IssueLeaf: %v", err)
	}
	containerLeafDir := filepath.Join("/etc/town-os/tls/leaves/e2e", uniq)

	// Real ingress container: unique key/unit, dedicated network, ephemeral port.
	port := e2eFreePort(t)
	dataDir := filepath.Join("/run/town-os/ingress-e2e", uniq)
	if err := os.MkdirAll(dataDir, 0o755); err != nil { //nolint:gosec // holds the gRPC socket
		t.Fatalf("mkdir data: %v", err)
	}
	mgr := ingressctl.NewManager(ingressctl.Config{
		Systemd:     systemd.NewManager(),
		DataDir:     dataDir,
		TLSHostDir:  "/town-os/tls",
		Image:       ingressImage,
		PullNever:   true,
		Key:         "ingress-e2e-" + uniq,
		NetworkName: netName,
		HostPort:    port,
	})
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("start ingress container: %v", err)
	}
	t.Cleanup(func() {
		sd := systemd.NewManager()
		unit := systemd.SystemServiceUnitName(mgr.Key())
		_ = sd.SetStatus(ctx, unit, systemd.Stop)
		_ = sd.UninstallUnit(ctx, unit)
		_ = podman("rm", "-f", systemd.SystemServiceContainerName(mgr.Key()))
	})
	if err := mgr.WaitForReady(ctx); err != nil {
		t.Fatalf("ingress gRPC socket not ready: %v", err)
	}

	// Program the route over gRPC.
	ic, err := ingress.Dial(ctx, mgr.SocketPath())
	if err != nil {
		t.Fatalf("dial ingress: %v", err)
	}
	defer func() { _ = ic.Close() }()
	if err := ic.SetRoutes(ctx, []*ingresspb.Route{{
		Hostname: fqdn,
		Backend:  net.JoinHostPort(backendIP, "80"),
		CertDir:  containerLeafDir,
	}}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	client := e2eCAClient(t, "/town-os/tls/ca.crt", fqdn, port)

	var lastErr error
	var lastStatus int
	if !e2ePoll(func() bool {
		resp, err := e2eGet(client, "https://"+fqdn+"/")
		if err != nil {
			lastErr, lastStatus = err, 0
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(io.Discard, resp.Body)
		lastErr, lastStatus = nil, resp.StatusCode
		return resp.StatusCode == http.StatusOK
	}) {
		// The config + reload path is unit-/repro-verified, so a failure here is
		// almost always a data-plane condition the bare assertion hides: caddy
		// unreachable (client error), a TLS mismatch, or — most commonly — the
		// reverse_proxy can't reach the backend. Dump caddy's own logs and probe
		// the backend from inside the ingress so the cause is unambiguous.
		t.Logf("serve poll exhausted: lastStatus=%d lastErr=%v", lastStatus, lastErr)
		e2eDumpIngressDiag(t, systemd.SystemServiceContainerName(mgr.Key()), backendIP)
		t.Fatal("ingress did not serve the backend over TLS through the programmed route")
	}

	// Withdraw the route → the ingress stops serving the host.
	if err := ic.RemoveRoute(ctx, fqdn); err != nil {
		t.Fatalf("RemoveRoute: %v", err)
	}
	if !e2ePoll(func() bool {
		resp, err := e2eGet(client, "https://"+fqdn+"/")
		if err != nil {
			return true
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode != http.StatusOK
	}) {
		t.Fatal("ingress still served the host after RemoveRoute")
	}
}

func podman(args ...string) error {
	return exec.Command("podman", args...).Run() //nolint:gosec,noctx // test-controlled args
}

func mustPodman(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("podman", args...).CombinedOutput(); err != nil { //nolint:gosec,noctx // test-controlled args
		t.Fatalf("podman %v: %v\n%s", args, err, out)
	}
}

func e2eFreePort(t *testing.T) int {
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

// e2eGet issues a context-aware GET (lint: noctx requires Do with a request).
func e2eGet(client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

// e2eCAClient returns an HTTP client that trusts the given CA and always dials
// 127.0.0.1:port while presenting SNI for host.
func e2eCAClient(t *testing.T, caPath, host string, port int) *http.Client {
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
			TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: host, MinVersion: tls.VersionTLS12},
		},
	}
}

func e2ePoll(cond func() bool) bool {
	for range 100 {
		if cond() {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// e2eContainerIP returns the container's IPv4 on the given podman network,
// polling until podman has assigned one. Programming the route to this IP (not
// the container name) keeps the e2e independent of container-DNS availability in
// the harness while still exercising the ingress's TLS-terminate-and-reverse-
// proxy data plane against a real backend.
func e2eContainerIP(t *testing.T, container, network string) string {
	t.Helper()
	tmpl := fmt.Sprintf("{{(index .NetworkSettings.Networks %q).IPAddress}}", network)
	var ip string
	if !e2ePoll(func() bool {
		out, err := exec.Command("podman", "inspect", "-f", tmpl, container).Output() //nolint:gosec,noctx // test-controlled args
		if err != nil {
			return false
		}
		ip = strings.TrimSpace(string(out))
		return ip != ""
	}) {
		t.Fatalf("backend %s got no IP on network %s", container, network)
	}
	return ip
}

// e2eDumpIngressDiag captures, on a serve failure, the ingress container's caddy
// output (its reverse_proxy errors name the exact cause: "no such host" = DNS,
// "connection refused"/"i/o timeout" = connectivity, none = TLS/SNI) plus a
// reachability probe to the backend from inside the ingress container and the
// ingress's network membership. Best-effort: every probe is logged, never fatal
// — it only adds signal to an already-failing test.
func e2eDumpIngressDiag(t *testing.T, ingressContainer, backendAddr string) {
	t.Helper()
	if out, err := exec.Command("podman", "logs", ingressContainer).CombinedOutput(); err == nil { //nolint:gosec,noctx // test-controlled args
		t.Logf("ingress container (%s) logs:\n%s", ingressContainer, out)
	} else {
		t.Logf("podman logs %s: %v\n%s", ingressContainer, err, out)
	}
	probe := exec.Command("podman", "exec", ingressContainer, "sh", "-c", //nolint:gosec,noctx // test-controlled args
		"wget -T 5 -qO- http://"+backendAddr+":80/ >/dev/null 2>&1 && echo BACKEND_OK || echo BACKEND_UNREACHABLE")
	if out, err := probe.CombinedOutput(); err == nil {
		t.Logf("ingress->backend(%s) probe:\n%s", backendAddr, out)
	} else {
		t.Logf("ingress->backend(%s) probe: %v\n%s", backendAddr, err, out)
	}
	if out, err := exec.Command("podman", "inspect", ingressContainer, "--format", "{{json .NetworkSettings.Networks}}").CombinedOutput(); err == nil { //nolint:gosec,noctx // test-controlled args
		t.Logf("%s networks: %s", ingressContainer, out)
	}
}

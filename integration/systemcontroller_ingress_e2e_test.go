// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/ingress"
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
	mgr := ingress.NewManager(ingress.Config{
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
		Backend:  backendName + ":80",
		CertDir:  containerLeafDir,
	}}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	client := e2eCAClient(t, "/town-os/tls/ca.crt", fqdn, port)

	if !e2ePoll(func() bool {
		resp, err := client.Get("https://" + fqdn + "/")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode == http.StatusOK
	}) {
		t.Fatal("ingress did not serve the backend over TLS through the programmed route")
	}

	// Withdraw the route → the ingress stops serving the host.
	if err := ic.RemoveRoute(ctx, fqdn); err != nil {
		t.Fatalf("RemoveRoute: %v", err)
	}
	if !e2ePoll(func() bool {
		resp, err := client.Get("https://" + fqdn + "/")
		if err != nil {
			return true
		}
		defer resp.Body.Close()
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
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
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
	for i := 0; i < 100; i++ {
		if cond() {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

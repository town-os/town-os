// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	cryptotls "crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// The control plane's own listener, terminated with the box's local CA.
//
// The unit tests cover the config and the leaf; this one runs an actual TLS
// listener and completes a real request over it with a client that trusts only
// the CA the box generated — which is the whole claim: a client that fetched
// GET /tls/ca.crt can reach :5309 without trusting anything else, and one that
// did not, cannot.

// serveControllerTLS starts an HTTPS listener carrying the given handler and
// returns its address. The port is ephemeral so concurrent runs never collide.
func serveControllerTLS(t *testing.T, tlsCfg *cryptotls.Config, handler http.Handler) string {
	t.Helper()

	lis, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	srv := &http.Server{
		Handler:           handler,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// The serve error is reported through a channel rather than logged from the
	// goroutine: t.Logf after the test function returns is a panic, and the
	// server outlives the test body by however long Shutdown takes.
	serveErrCh := make(chan error, 1)
	go func() {
		// Cert and key already live in TLSConfig.Certificates.
		serveErrCh <- srv.ServeTLS(lis, "", "")
	}()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if cerr := srv.Shutdown(shutdownCtx); cerr != nil {
			t.Errorf("shutdown: %v", cerr)
		}
		if serveErr := <-serveErrCh; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			t.Errorf("ServeTLS: %v", serveErr)
		}
	})

	return lis.Addr().String()
}

// caPool is the trust store a client builds from GET /tls/ca.crt.
func caPool(t *testing.T, btrfsBase string) *x509.CertPool {
	t.Helper()

	pem, err := os.ReadFile(filepath.Join(btrfsBase, systemcontroller.TLSSubvolume, "ca.crt"))
	if err != nil {
		t.Fatalf("read CA: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		t.Fatal("CA PEM did not parse")
	}
	return pool
}

func TestSystemControllerTLSListenerServesOverLocalCA(t *testing.T) {
	// t.Setenv forbids t.Parallel; the listener uses an ephemeral port so
	// running serially costs nothing but a moment.
	t.Setenv(systemcontroller.EnvTLS, "1")
	t.Setenv(systemcontroller.EnvTLSCert, "")
	t.Setenv(systemcontroller.EnvTLSKey, "")
	t.Setenv(systemcontroller.EnvTLSSANs, "")

	base := t.TempDir()
	tlsCfg, err := systemcontroller.ControllerTLSConfig(base, nil)
	if err != nil {
		t.Fatalf("ControllerTLSConfig: %v", err)
	}
	if tlsCfg == nil {
		t.Fatal("no TLS config with TOWN_OS_TLS=1")
	}

	// The boot stub, because that is what answers on this socket for the first
	// part of every boot — the window a UI watching a self-update lives in.
	bs := systemcontroller.NewBootStatus()
	t.Cleanup(bs.Done)
	handler := systemcontroller.NewRootHandler(systemcontroller.NewBootHandler(bs))
	addr := serveControllerTLS(t, tlsCfg, handler)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &cryptotls.Config{
				RootCAs:    caPool(t, base),
				MinVersion: cryptotls.VersionTLS12,
			},
		},
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://"+addr+"/status/ping", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("HTTPS GET /status/ping: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.Errorf("close body: %v", cerr)
		}
	}()

	// The boot stub answers 503 while booting — the point here is that the
	// request completed over TLS at all, with a verified chain.
	if resp.StatusCode != http.StatusServiceUnavailable && resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 503 (booting) or 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var ping struct {
		Booting bool   `json:"booting"`
		BootID  string `json:"boot_id"`
	}
	if err := json.Unmarshal(body, &ping); err != nil {
		t.Fatalf("decode ping (%s): %v", body, err)
	}
	if ping.BootID == "" {
		t.Errorf("ping over TLS returned no boot_id: %s", body)
	}
}

// A client that has not been given the CA must fail the handshake. That is the
// reason TLS is opt-in rather than the default: until the CA is installed, a
// browser cannot complete an XHR to this listener, and unlike a navigation
// there is no interstitial to click through.
func TestSystemControllerTLSListenerRejectsUntrustingClient(t *testing.T) {
	t.Setenv(systemcontroller.EnvTLS, "1")
	t.Setenv(systemcontroller.EnvTLSCert, "")
	t.Setenv(systemcontroller.EnvTLSKey, "")
	t.Setenv(systemcontroller.EnvTLSSANs, "")

	base := t.TempDir()
	tlsCfg, err := systemcontroller.ControllerTLSConfig(base, nil)
	if err != nil {
		t.Fatalf("ControllerTLSConfig: %v", err)
	}

	bs := systemcontroller.NewBootStatus()
	t.Cleanup(bs.Done)
	addr := serveControllerTLS(t, tlsCfg, systemcontroller.NewRootHandler(systemcontroller.NewBootHandler(bs)))

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://"+addr+"/status/ping", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := client.Do(req) //nolint:bodyclose // the request is expected to fail before a body exists
	if err == nil {
		if cerr := resp.Body.Close(); cerr != nil {
			t.Errorf("close body: %v", cerr)
		}
		t.Fatal("a client with the system trust store completed a handshake against the local CA")
	}
}

// Nothing changes for a box that has not asked for TLS: the listener stays
// plain HTTP, which is what every existing deployment is running.
func TestSystemControllerTLSDisabledByDefault(t *testing.T) {
	t.Setenv(systemcontroller.EnvTLS, "")
	t.Setenv(systemcontroller.EnvTLSCert, "")
	t.Setenv(systemcontroller.EnvTLSKey, "")

	cfg, err := systemcontroller.ControllerTLSConfig(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("ControllerTLSConfig: %v", err)
	}
	if cfg != nil {
		t.Fatal("TLS was configured without being asked for")
	}
}

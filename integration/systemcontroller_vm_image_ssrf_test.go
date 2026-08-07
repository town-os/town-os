// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// POST /vm-images/upload takes a URL from the request and fetches it from the
// controller with a bare client:
//
//	client := &http.Client{}
//	resp, err := client.Do(req)
//
// No address policy, no scheme restriction, and the controller sits on the host
// network with the podman socket -- so the URL is fetched from a position no
// remote caller has: loopback services, the LAN, link-local metadata endpoints,
// and every Town OS system service that binds a port.
//
// The OAuth device flow, which has exactly the same shape (a URL that is not
// the operator's, fetched by the controller), does this correctly:
// packages.CheckOAuthAddr runs in the dialer's Control hook, after resolution
// and on every redirect, and refuses loopback, private, link-local, multicast,
// unspecified and CGNAT addresses. The same guard belongs here.
//
// Admin-only, so this is defence in depth rather than a boundary crossing --
// but the point of an SSRF guard is that "the caller is trusted" and "the
// caller can aim the box at its own internals" are different statements, and
// this endpoint is also the one that follows redirects.
//
// These tests assert the SECURE behaviour and fail against the current code.

func initVMImageSSRFTest(t *testing.T) *systemcontroller.SystemdClient {
	t.Helper()

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       storage.InitBtrFSMock(),
		BtrfsBasePath: t.TempDir(),
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return c
}

// internalService stands in for anything listening where only the box can
// reach it. httptest binds 127.0.0.1, which is the address class the guard has
// to refuse first.
func internalService(t *testing.T) (url string, hits *atomic.Int64) {
	t.Helper()

	var counter atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		counter.Add(1)
		// A plausible-looking payload, so a fetch that succeeds gets far enough
		// to be unambiguous rather than failing on an empty body.
		if _, err := w.Write([]byte("QFI\xfb")); err != nil {
			t.Errorf("write internal response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	return srv.URL + "/secret.qcow2", &counter
}

func TestVMImageUploadRefusesLoopbackURL(t *testing.T) {
	t.Parallel()
	c := initVMImageSSRFTest(t)

	target, hits := internalService(t)

	_, err := c.UploadVMImage(context.TODO(), target, "probe.raw")
	if err == nil {
		t.Error("UploadVMImage accepted a loopback URL")
	}

	if n := hits.Load(); n != 0 {
		t.Errorf("the controller made %d request(s) to a loopback service on behalf of the caller; "+
			"downloadFile needs the packages.CheckOAuthAddr dialer guard the OAuth flow already uses", n)
	}
}

// A redirect is the half a parse-time check cannot cover: the submitted URL is
// public and the hop that lands on loopback is chosen by the remote server.
// This is why the OAuth client checks in the dialer rather than on the URL.
func TestVMImageUploadRefusesRedirectToLoopback(t *testing.T) {
	t.Parallel()
	c := initVMImageSSRFTest(t)

	target, hits := internalService(t)

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusFound)
	}))
	t.Cleanup(redirector.Close)

	if _, err := c.UploadVMImage(context.TODO(), redirector.URL+"/image.qcow2", "probe.raw"); err == nil {
		t.Error("UploadVMImage followed a redirect onto a loopback address without complaint")
	}

	if n := hits.Load(); n != 0 {
		t.Errorf("the controller followed a redirect into %d request(s) against a loopback service", n)
	}
}

// A non-HTTP scheme has no business here at all. file:// is the one that turns
// a fetch into an arbitrary local read.
func TestVMImageUploadRefusesNonHTTPSchemes(t *testing.T) {
	t.Parallel()
	c := initVMImageSSRFTest(t)

	for _, raw := range []string{
		"file:///etc/shadow",
		"gopher://127.0.0.1:70/",
		"ftp://127.0.0.1/image.raw",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			if _, err := c.UploadVMImage(context.TODO(), raw, "probe.raw"); err == nil {
				t.Errorf("UploadVMImage accepted %q", raw)
			}
		})
	}
}

// The link-local metadata address every cloud provider answers on. Not
// reachable from the test container, so this asserts the refusal happens
// before the dial rather than as a connection timeout -- a guard that only
// works because the network happens to be unreachable is not a guard.
func TestVMImageUploadRefusesLinkLocalURL(t *testing.T) {
	t.Parallel()
	c := initVMImageSSRFTest(t)

	// Bounded, because the point is that the refusal comes from policy rather
	// than from the address being unroutable. Without a guard the request is
	// actually dialled, and the handler's own timeout is 30 minutes.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := c.UploadVMImage(ctx, "http://169.254.169.254/latest/meta-data/", "probe.raw")
	if err == nil {
		t.Fatal("UploadVMImage accepted a link-local metadata URL")
	}
	// A refusal names the policy; a timeout or a connection error names the
	// network. Only the first is the guard doing its job.
	if !strings.Contains(strings.ToLower(err.Error()), "not allowed") &&
		!strings.Contains(strings.ToLower(err.Error()), "not a public address") {
		t.Errorf("UploadVMImage failed for %q with %v, which does not look like an address-policy refusal; "+
			"the guard must reject before dialling, not rely on the address being unroutable",
			"http://169.254.169.254/latest/meta-data/", err)
	}
}

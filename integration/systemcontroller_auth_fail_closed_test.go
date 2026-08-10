// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// The unit tests drive the middleware functions directly. This drives a real
// router over real HTTP, because the question is what an anonymous client on
// the network actually gets — and the answer used to be the route.
//
// A box with no session manager can authenticate nobody. Every authorization
// decision on the box was derived from that one nil: requireAuth, requireAdmin,
// and requireGrant all read it as "authentication is not configured, so let it
// through", so an unauthenticated caller reached /packages/install and
// /storage/remove on a controller that drives the host podman socket as root.

// authFailClosedServer builds a router with no session manager and no
// AuthDisabled — the state that used to serve everything to everybody. It
// bypasses InitTestServer deliberately: that helper now sets AuthDisabled for
// exactly this config, which is the behavior under test everywhere else.
func authFailClosedServer(t *testing.T) *httptest.Server {
	t.Helper()

	handler, err := systemcontroller.NewHandler(t.Context(), systemcontroller.ServerConfig{
		Storage: storage.InitBtrFSMock(),
	})
	if !errors.Is(err, systemcontroller.ErrAuthNotConfigured) {
		t.Fatalf("NewHandler with no session manager: err = %v, want ErrAuthNotConfigured", err)
	}
	if handler != nil {
		t.Fatal("NewHandler returned a handler alongside the error")
	}

	// Having proved construction refuses it, build the same thing the only
	// way that is still possible — through AuthDisabled — and confirm the
	// opposite: a box that ASKED for no auth is served, because the whole
	// test corpus depends on that.
	handler, err = systemcontroller.NewHandler(t.Context(), systemcontroller.ServerConfig{
		Storage:      storage.InitBtrFSMock(),
		AuthDisabled: true,
	})
	if err != nil {
		t.Fatalf("NewHandler with AuthDisabled: %v", err)
	}

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// TestAuthNotConfiguredIsRefusedAtConstruction is the primary assertion: the
// misconfiguration cannot produce a serving handler at all.
func TestAuthNotConfiguredIsRefusedAtConstruction(t *testing.T) {
	t.Parallel()

	handler, err := systemcontroller.NewHandler(t.Context(), systemcontroller.ServerConfig{
		Storage: storage.InitBtrFSMock(),
	})
	if !errors.Is(err, systemcontroller.ErrAuthNotConfigured) {
		t.Fatalf("NewHandler error = %v, want ErrAuthNotConfigured", err)
	}
	if handler != nil {
		t.Error("NewHandler returned a handler alongside the error: a caller that ignored err would serve an unauthenticated box")
	}
}

// TestAuthDisabledBoxStillServes pins the other half. Roughly 230 test call
// sites construct a server with no session manager; if AuthDisabled stopped
// serving them the corpus would fail wholesale rather than meaningfully.
func TestAuthDisabledBoxStillServes(t *testing.T) {
	t.Parallel()
	srv := authFailClosedServer(t)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, srv.URL+"/storage", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /storage: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.Errorf("close body: %v", cerr)
		}
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if resp.StatusCode == http.StatusInternalServerError {
		t.Fatalf("POST /storage on an explicitly unauthenticated box = 500 (%s); AuthDisabled must serve, not refuse", body)
	}
}

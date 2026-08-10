// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// authPingBody is the subset of /status/ping the dashboard's session poll
// reads. `username` is the whole decision: empty means "nobody is logged in
// here", and the poll reacts by clearing the token and navigating to the
// login page.
type authPingBody struct {
	Status   string `json:"status"`
	Username string `json:"username"`
	Booting  bool   `json:"booting"`
	BootID   string `json:"boot_id"`
}

// pingWithToken performs the exact request the UI's session poll makes: a
// GET /status/ping carrying the operator's bearer token.
func pingWithToken(t *testing.T, url, token string) (int, authPingBody) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/status/ping", nil)
	if err != nil {
		t.Fatalf("new ping request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	defer func() {
		if cErr := resp.Body.Close(); cErr != nil {
			t.Errorf("close ping body: %v", cErr)
		}
	}()

	var body authPingBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode ping body: %v", err)
	}
	return resp.StatusCode, body
}

// TestRefreshCoreServicesInvalidatesTheOperatorsSession drives the HTTP
// sequence a browser sees during a Refresh Core Services — through a real
// listener, a real SQLite account/session store, and the real RootHandler
// swap — and pins WHY the UI must suspend its session poll for the duration.
//
// The restarted controller generates a fresh JWT signing key and clears the
// sessions table (InitSessionManager, boot step 7). So the token the operator
// is holding dies with the outgoing process. The successor answers that token
// with a perfectly successful 200 that simply carries no `username` — which
// is byte-for-byte what an expired session looks like. A session poll running
// across the restart therefore fires `setSessionExpired(); clearToken();
// navigate('/')` the instant the successor comes up, unmounting the dialog
// that was showing the operator the update they asked for. That was the bug:
// click Refresh, get bounced to the login screen, never see a single stage.
//
// The fix (ui/src/lib/session-guard.js) suspends the poll for the whole
// restart and lets the boot-status stream — which needs no auth at all —
// report completion. This test pins the two backend facts that fix rests on:
//  1. a pre-restart token is unauthenticated against the successor, and
//  2. /status/ping and /boot-status serve the whole restart without a token,
//     so the dialog can keep reporting status with a dead session.
func TestRefreshCoreServicesInvalidatesTheOperatorsSession(t *testing.T) {
	db, err := account.OpenDB(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if cErr := db.Close(); cErr != nil {
			t.Errorf("db.Close: %v", cErr)
		}
	})

	acctMgr, err := account.InitManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	// --- Generation 1: the controller the operator is logged into. ---
	sess1, err := account.InitSessionManager(t.Context(), db, acctMgr, []byte("generation-1-ephemeral-key-32byt"))
	if err != nil {
		t.Fatalf("InitSessionManager gen1: %v", err)
	}

	bs1 := systemcontroller.NewBootStatus()
	ctx1, cancel1 := context.WithCancel(t.Context())
	t.Cleanup(cancel1)
	gen1, err := systemcontroller.NewHandler(ctx1, systemcontroller.ServerConfig{
		Storage:    storage.InitBtrFS("/town-os"),
		AccountMgr: acctMgr,
		SessionMgr: sess1,
		BootID:     bs1.BootID(),
	})
	if err != nil {
		t.Fatalf("NewHandler gen1: %v", err)
	}
	root := systemcontroller.NewRootHandler(gen1)
	srv := httptest.NewServer(root)
	t.Cleanup(srv.Close)

	if _, err := acctMgr.Create(t.Context(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	token, err := sess1.Create(t.Context(), "admin")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	code, body := pingWithToken(t, srv.URL, token)
	if code != http.StatusOK {
		t.Fatalf("gen1 ping status = %d, want 200", code)
	}
	if body.Username != "admin" {
		t.Fatalf("gen1 ping username = %q, want %q — the operator is logged in here", body.Username, "admin")
	}

	// --- The refresh restarts the controller: boot stub takes over. ---
	bs2 := systemcontroller.NewBootStatus()
	root.Swap(systemcontroller.NewBootHandler(bs2))
	bs2.Step(systemcontroller.StepBootController)

	code, body = pingWithToken(t, srv.URL, token)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("booting ping status = %d, want 503", code)
	}
	if !body.Booting {
		t.Error("booting ping: booting = false, want true")
	}
	if body.BootID == bs1.BootID() {
		t.Fatal("booting ping reports the outgoing boot_id; the UI cannot tell the successor from its predecessor")
	}
	// The successor's stub answers a token it has never seen without
	// erroring on it — the poll must NOT read this as an auth failure.
	if body.Username != "" {
		t.Errorf("booting ping username = %q, want empty (the successor has no sessions yet)", body.Username)
	}

	// The stub streams the boot to anyone, authenticated or not: this is the
	// channel the dialog uses once its session is dead.
	streamCode, frames := readBootStatus(t, srv.URL, 1)
	if streamCode != http.StatusOK {
		t.Fatalf("booting /boot-status status = %d, want 200 (no auth required)", streamCode)
	}
	if len(frames) != 1 || frames[0].Step != systemcontroller.StepBootController {
		t.Fatalf("booting /boot-status frames = %+v, want the successor's first step", frames)
	}

	// --- Generation 2 finishes booting. ---
	// A new signing key, exactly as boot step 7 does — which drops every row
	// in the sessions table.
	sess2, err := account.InitSessionManager(t.Context(), db, acctMgr, []byte("generation-2-ephemeral-key-32byt"))
	if err != nil {
		t.Fatalf("InitSessionManager gen2: %v", err)
	}

	sessions, err := sess1.List(t.Context(), "admin")
	if err != nil {
		t.Fatalf("list sessions after restart: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("restart left %d session(s) in the table; boot must clear them all", len(sessions))
	}

	ctx2, cancel2 := context.WithCancel(t.Context())
	t.Cleanup(cancel2)
	gen2, err := systemcontroller.NewHandler(ctx2, systemcontroller.ServerConfig{
		Storage:    storage.InitBtrFS("/town-os"),
		AccountMgr: acctMgr,
		SessionMgr: sess2,
		BootID:     bs2.BootID(),
	})
	if err != nil {
		t.Fatalf("NewHandler gen2: %v", err)
	}
	root.Swap(gen2)
	bs2.Done()

	// THE BUG, in one assertion: the successor answers the operator's token
	// with a clean 200 that names nobody. Indistinguishable from an expired
	// session, so an un-suspended poll logs the operator out right here.
	code, body = pingWithToken(t, srv.URL, token)
	if code != http.StatusOK {
		t.Fatalf("post-restart ping status = %d, want 200", code)
	}
	if body.Username != "" {
		t.Fatalf("post-restart ping username = %q, want empty: the restart regenerates the signing key and "+
			"clears the sessions table, so the pre-restart token cannot still authenticate", body.Username)
	}
	if body.Status != "ok" {
		t.Errorf("post-restart ping status field = %q, want %q", body.Status, "ok")
	}
	if body.BootID != bs2.BootID() {
		t.Errorf("post-restart ping boot_id = %q, want the successor's %q", body.BootID, bs2.BootID())
	}

	// And the same ping without any token at all answers identically — the
	// dialog does not need a session to see the system come back.
	anonCode, anonBody := pingWithToken(t, srv.URL, "")
	if anonCode != http.StatusOK {
		t.Fatalf("anonymous post-restart ping status = %d, want 200", anonCode)
	}
	if anonBody.BootID != bs2.BootID() {
		t.Errorf("anonymous ping boot_id = %q, want %q", anonBody.BootID, bs2.BootID())
	}

	// Re-authenticating against the successor works: the account survived,
	// only the session did not. This is what the operator's Reload lands on.
	newToken, err := sess2.Create(t.Context(), "admin")
	if err != nil {
		t.Fatalf("re-authenticate against the successor: %v", err)
	}
	code, body = pingWithToken(t, srv.URL, newToken)
	if code != http.StatusOK {
		t.Fatalf("re-authenticated ping status = %d, want 200", code)
	}
	if body.Username != "admin" {
		t.Fatalf("re-authenticated ping username = %q, want %q", body.Username, "admin")
	}
}

// TestRefreshBootStatusStreamCompletesWithoutAuth pins the channel the
// refresh dialog now depends on instead of its old ping-and-reload loop: the
// successor's /boot-status stream runs to a `done` frame with no credentials
// whatsoever. That `done` is what flips the dialog into its completion state,
// so if this route ever required auth the dialog would hang forever behind a
// session that the restart itself destroyed.
func TestRefreshBootStatusStreamCompletesWithoutAuth(t *testing.T) {
	bs := systemcontroller.NewBootStatus()
	root := systemcontroller.NewRootHandler(systemcontroller.NewBootHandler(bs))
	srv := httptest.NewServer(root)
	t.Cleanup(srv.Close)

	steps := []string{
		systemcontroller.StepBootController,
		systemcontroller.StepBootDNS,
		systemcontroller.StepBootServices,
		systemcontroller.StepRestartPackages,
		systemcontroller.StepReady,
	}
	for _, step := range steps {
		bs.Step(step)
	}
	bs.Done()

	// No Authorization header anywhere in readBootStatus — that is the point.
	code, frames := readBootStatus(t, srv.URL, len(steps)+1)
	if code != http.StatusOK {
		t.Fatalf("/boot-status status = %d, want 200 without auth", code)
	}
	if len(frames) == 0 {
		t.Fatal("/boot-status returned no frames")
	}
	if !frames[len(frames)-1].Done {
		t.Fatalf("/boot-status frames = %+v, want a terminating done frame — the dialog's only completion signal", frames)
	}
	for i, step := range steps {
		if frames[i].Step != step {
			t.Fatalf("frame %d step = %q, want %q", i, frames[i].Step, step)
		}
	}
}

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package gfeh

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// serveSocket runs an httptest server on a Unix socket inside the test's own
// temp dir, so concurrent runs cannot collide on a path — IRON RULE.
func serveSocket(t *testing.T, handler http.Handler) *UnixClient {
	t.Helper()

	// t.TempDir paths can be long enough to exceed the ~104-byte sun_path
	// limit on some systems; a short filename keeps headroom.
	socket := filepath.Join(t.TempDir(), "a.sock")
	var lc net.ListenConfig
	listener, err := lc.Listen(context.Background(), "unix", socket)
	if err != nil {
		t.Fatalf("listen on %s: %v", socket, err)
	}

	server := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second},
	}
	server.Start()
	t.Cleanup(server.Close)

	return NewClient(socket)
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func jsonHandler(t *testing.T, status int, body any) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body == nil {
			return
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("encode response: %v", err)
		}
	})
}

// TestNamesDecodesAnAbsentNetwork: the contract says absent means "the
// default", so the pointer must stay nil rather than becoming "".
func TestNamesDecodesAnAbsentNetwork(t *testing.T) {
	client := serveSocket(t, jsonHandler(t, 200, map[string]any{
		"partition": "home",
		"names": []map[string]any{
			{"hostname": "s3.gfeh", "view": "s3", "port": 9000},
		},
	}))

	got, err := client.Names(testCtx(t))
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if got.Network != nil {
		t.Errorf("Network = %q, want nil for an absent field", *got.Network)
	}
	if got.NetworkName("home") != "home" {
		t.Errorf("NetworkName = %q, want home", got.NetworkName("home"))
	}
	if len(got.Names) != 1 || got.Names[0].Hostname != "s3.gfeh" || got.Names[0].Port != 9000 {
		t.Errorf("names = %+v", got.Names)
	}
}

func TestNamesDecodesANamedNetwork(t *testing.T) {
	client := serveSocket(t, jsonHandler(t, 200, map[string]any{
		"partition": "office",
		"network":   "office",
		"names":     []map[string]any{},
	}))

	got, err := client.Names(testCtx(t))
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if got.Network == nil || *got.Network != "office" {
		t.Fatalf("Network = %v, want office", got.Network)
	}
}

// TestListGrantsSendsThePrincipal: gfehd's handler takes principal as a
// required query parameter with no default, so an absent one is a 4xx rather
// than "every grant".
func TestListGrantsSendsThePrincipal(t *testing.T) {
	var gotQuery string
	client := serveSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("principal")
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode([]Grant{}); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))

	if _, err := client.ListGrants(testCtx(t), "alice smith"); err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if gotQuery != "alice smith" {
		t.Errorf("principal query = %q, want %q", gotQuery, "alice smith")
	}
}

// TestPathsAreEscaped: a principal or token with a slash in it must not be able
// to address a different route on the admin socket.
func TestPathsAreEscaped(t *testing.T) {
	var gotPath string
	client := serveSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusOK)
	}))

	if err := client.DeletePrincipal(testCtx(t), "a/b"); err != nil {
		t.Fatalf("DeletePrincipal: %v", err)
	}
	if gotPath != "/v1/principals/a%2Fb" {
		t.Errorf("path = %q, want the slash escaped", gotPath)
	}
}

// TestStatusMapping is what lets reconcile treat "already exists" as success
// while still failing on a genuine error.
func TestStatusMapping(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{
		{http.StatusNotFound, ErrNotFound},
		{http.StatusConflict, ErrAlreadyExists},
		{http.StatusBadRequest, ErrBadRequest},
	} {
		client := serveSocket(t, jsonHandler(t, tc.status, map[string]string{"error": "nope"}))
		_, err := client.Health(testCtx(t))
		if err == nil {
			t.Fatalf("status %d: expected an error", tc.status)
		}
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d: err = %v, want errors.Is %v", tc.status, err, tc.want)
		}
		var se *StatusError
		if !errors.As(err, &se) {
			t.Fatalf("status %d: err is not a *StatusError: %v", tc.status, err)
		}
		if se.Message != "nope" {
			t.Errorf("status %d: message = %q, want the daemon's own text", tc.status, se.Message)
		}
	}
}

// TestUnreachableSocketIsUnavailable: a daemon that is not up yet is the
// ordinary case during boot, and callers distinguish it so a partition that is
// still starting does not look like a partition that is broken.
func TestUnreachableSocketIsUnavailable(t *testing.T) {
	client := NewClient(filepath.Join(t.TempDir(), "absent.sock"))

	if _, err := client.Health(testCtx(t)); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want errors.Is ErrUnavailable", err)
	}
}

// TestListsDecodeToEmptySlices: gfehd answers `[]` and the collectors range
// over the result, so a nil is fine — but a JSON `null` must not become a
// decode error.
func TestListsDecodeToEmptySlices(t *testing.T) {
	client := serveSocket(t, jsonHandler(t, 200, []Principal{}))

	got, err := client.ListPrincipals(testCtx(t))
	if err != nil {
		t.Fatalf("ListPrincipals: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("principals = %v, want empty", got)
	}
}

// TestCreateGrantReportsTheStoredRights: gfeh clamps a grant to the
// principal's ceiling, and an administrator has to be able to see that it was
// narrowed or they will believe they gave access nobody has.
func TestCreateGrantReportsTheStoredRights(t *testing.T) {
	client := serveSocket(t, jsonHandler(t, 200, Grant{
		ID: 7, Principal: "alice", Path: "/photos", Perm: []string{PermRead, PermList},
	}))

	got, err := client.CreateGrant(testCtx(t), Grant{
		Principal: "alice", Path: "/photos", Perm: []string{PermAll},
	})
	if err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}
	if got.ID != 7 {
		t.Errorf("id = %d, want 7", got.ID)
	}
	if len(got.Perm) != 2 {
		t.Errorf("perm = %v, want the clamped pair the daemon reported", got.Perm)
	}
}

func TestRevokeGrantUsesTheID(t *testing.T) {
	var gotPath, gotMethod string
	client := serveSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.WriteHeader(http.StatusOK)
	}))

	if err := client.RevokeGrant(testCtx(t), 42); err != nil {
		t.Fatalf("RevokeGrant: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/v1/grants/42" {
		t.Errorf("%s %s, want DELETE /v1/grants/42", gotMethod, gotPath)
	}
}

// TestMockClientSatisfiesClient keeps the mock honest against the interface the
// collectors take.
func TestMockClientSatisfiesClient(t *testing.T) {
	var _ Client = (*MockClient)(nil)
	var _ Client = (*UnixClient)(nil)

	m := NewMockClient("home", "", Name{Hostname: "s3.gfeh", View: ViewS3, Port: PortS3})
	names, err := m.Names(testCtx(t))
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if names.Network != nil {
		t.Error("an empty network must stay absent in the mock too")
	}
	if m.CallsFor("Names") != 1 {
		t.Errorf("CallsFor(Names) = %d, want 1", m.CallsFor("Names"))
	}
}

func TestMockClientPrincipalLifecycle(t *testing.T) {
	ctx := testCtx(t)
	m := NewMockClient("home", "")

	if _, err := m.CreatePrincipal(ctx, Principal{Name: "alice", Ceiling: []string{PermReadWrite}}); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}
	if _, err := m.CreatePrincipal(ctx, Principal{Name: "alice"}); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("duplicate principal err = %v, want ErrAlreadyExists", err)
	}
	if _, err := m.CreateGrant(ctx, Grant{Principal: "alice", Path: "/", Perm: []string{PermRead}}); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	// Deleting a principal takes its grants with it, as gfeh's schema does.
	if err := m.DeletePrincipal(ctx, "alice"); err != nil {
		t.Fatalf("DeletePrincipal: %v", err)
	}
	grants, err := m.ListGrants(ctx, "alice")
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 0 {
		t.Errorf("grants survived their principal: %v", grants)
	}
}

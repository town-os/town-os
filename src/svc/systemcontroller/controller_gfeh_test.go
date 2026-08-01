// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/storage"
	"github.com/labstack/echo/v5"
)

// gfehUITest builds a handler over a stub registry, so the proxy routes are
// exercised without podman, systemd, or a real gfehd.
func gfehUITest(t *testing.T, clients map[string]gfeh.Client) (*SystemControllerHandlers, account.Manager) {
	t.Helper()

	acctMgr := account.InitMockManager()
	nm := account.InitMockNetworkManager()
	if _, err := nm.Create(&account.Network{Name: "office", TLD: "office", Enabled: true}); err != nil {
		t.Fatalf("create network: %v", err)
	}

	sb := &serverBase{ServerConfig: ServerConfig{
		Storage:      storage.InitBtrFSMock(),
		AccountMgr:   acctMgr,
		NetworkMgr:   nm,
		GfehRegistry: stubGfehRegistry{clients: clients},
	}}
	return getHandler(t.Context(), sb), acctMgr
}

// callGfeh drives one handler and returns the recorder.
func callGfeh(t *testing.T, h func(*echo.Context) error, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader *strings.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		reader = strings.NewReader(string(encoded))
	} else {
		reader = strings.NewReader("")
	}

	req := httptest.NewRequestWithContext(t.Context(), method, target, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	e := echo.New()
	c := e.NewContext(req, rec)
	if err := h(c); err != nil {
		var he *echo.HTTPError
		if ok := asEchoError(err, &he); ok {
			rec.Code = he.Code
			return rec
		}
		rec.Code = http.StatusInternalServerError
	}
	return rec
}

func asEchoError(err error, out **echo.HTTPError) bool {
	he, ok := err.(*echo.HTTPError) //nolint:errorlint // echo returns this type directly
	if ok {
		*out = he
	}
	return ok
}

// TestGfehListReportsNamesAndLiveness. A partition that exists but does not
// answer is a distinct state — its data is there, its names are not being
// published — and the UI has to be able to say so.
func TestGfehListReportsNamesAndLiveness(t *testing.T) {
	h, _ := gfehUITest(t, map[string]gfeh.Client{
		"office": allViews("office", "office"),
	})

	rec := callGfeh(t, h.listGfeh, http.MethodGet, "/gfeh", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	var out []GfehPartitionView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d partitions, want 1", len(out))
	}
	if !out[0].Running {
		t.Error("a healthy partition reported as not running")
	}
	if out[0].TLD != "office" {
		t.Errorf("TLD = %q, want office", out[0].TLD)
	}
	if len(out[0].Names) != 5 {
		t.Errorf("got %d names, want 5: %+v", len(out[0].Names), out[0].Names)
	}
	for _, n := range out[0].Names {
		if n.View == gfeh.ViewSMB && n.HTTP {
			t.Error("smb was reported as an HTTP view; the UI would tell a user to browse to it")
		}
	}
}

// TestGfehListIsEmptyWhenObjectStorageIsOff: a configuration, not a failure.
func TestGfehListIsEmptyWhenObjectStorageIsOff(t *testing.T) {
	sb := &serverBase{ServerConfig: ServerConfig{Storage: storage.InitBtrFSMock()}}
	h := getHandler(t.Context(), sb)

	rec := callGfeh(t, h.listGfeh, http.MethodGet, "/gfeh", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("body = %s, want []", got)
	}
}

// TestGfehRoutesRejectAnUnknownNetwork: a typo reads as "no such partition",
// not as object storage being unavailable.
func TestGfehRoutesRejectAnUnknownNetwork(t *testing.T) {
	h, _ := gfehUITest(t, map[string]gfeh.Client{"office": allViews("office", "office")})

	rec := callGfeh(t, h.listGfehPrincipals, http.MethodGet, "/gfeh/principals?network=nope", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}

	rec = callGfeh(t, h.listGfehPrincipals, http.MethodGet, "/gfeh/principals", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing network: status %d, want 400", rec.Code)
	}
}

// TestGfehAddPrincipalRequiresARealAccount. Creating a principal for a username
// that does not exist would put a grant in the forest nobody can ever
// authenticate as, and nothing would ever clean it up.
func TestGfehAddPrincipalRequiresARealAccount(t *testing.T) {
	h, _ := gfehUITest(t, map[string]gfeh.Client{"office": allViews("office", "office")})

	rec := callGfeh(t, h.addGfehPrincipal, http.MethodPost, "/gfeh/principals/add",
		GfehPrincipalRequest{Network: "office", Principal: "ghost"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400 for an account that does not exist", rec.Code)
	}
}

// TestGfehAddPrincipalProjectsTheAccountsCeiling: a Town OS administrator is a
// gfeh superuser because they create the roots of the forest; an ordinary
// account gets a ceiling and no grants, because authenticating is not
// authorization.
func TestGfehAddPrincipalProjectsTheAccountsCeiling(t *testing.T) {
	client := allViews("office", "office")
	h, acctMgr := gfehUITest(t, map[string]gfeh.Client{"office": client})

	mustAccount(t, acctMgr, "alice", false, nil)
	if _, err := acctMgr.Create("root", "hunter2hunter2", "r@example.com", "5551234", "Root", true); err != nil {
		t.Fatalf("create admin: %v", err)
	}

	for _, tc := range []struct {
		user string
		want string
	}{
		{"alice", gfeh.PermReadWrite},
		{"root", gfeh.PermAll},
	} {
		rec := callGfeh(t, h.addGfehPrincipal, http.MethodPost, "/gfeh/principals/add",
			GfehPrincipalRequest{Network: "office", Principal: tc.user})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", tc.user, rec.Code, rec.Body.String())
		}
		var out GfehPrincipalView
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(out.Ceiling) != 1 || out.Ceiling[0] != tc.want {
			t.Errorf("%s ceiling = %v, want [%s]", tc.user, out.Ceiling, tc.want)
		}
		if !out.Account {
			t.Errorf("%s was not marked as a Town OS account", tc.user)
		}
	}
}

// TestGfehAddPrincipalConflictsOnADuplicate, so the UI can say "already added"
// rather than reporting a server error.
func TestGfehAddPrincipalConflictsOnADuplicate(t *testing.T) {
	h, acctMgr := gfehUITest(t, map[string]gfeh.Client{"office": allViews("office", "office")})
	mustAccount(t, acctMgr, "alice", false, nil)

	body := GfehPrincipalRequest{Network: "office", Principal: "alice"}
	if rec := callGfeh(t, h.addGfehPrincipal, http.MethodPost, "/gfeh/principals/add", body); rec.Code != http.StatusOK {
		t.Fatalf("first add: status %d", rec.Code)
	}
	if rec := callGfeh(t, h.addGfehPrincipal, http.MethodPost, "/gfeh/principals/add", body); rec.Code != http.StatusConflict {
		t.Errorf("duplicate add: status %d, want 409", rec.Code)
	}
}

// TestGfehGrantsRequireAPrincipal. gfehd's handler requires the query
// parameter, so answering differently here would make the proxy disagree with
// what it proxies.
func TestGfehGrantsRequireAPrincipal(t *testing.T) {
	h, _ := gfehUITest(t, map[string]gfeh.Client{"office": allViews("office", "office")})

	rec := callGfeh(t, h.listGfehGrants, http.MethodGet, "/gfeh/grants?network=office", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400 with no principal", rec.Code)
	}
}

// TestGfehAddGrantReportsWhatWasStored. gfeh clamps a grant to the principal's
// ceiling, and an administrator has to be able to see it was narrowed or they
// will believe they gave access nobody has.
func TestGfehAddGrantReportsWhatWasStored(t *testing.T) {
	h, _ := gfehUITest(t, map[string]gfeh.Client{"office": allViews("office", "office")})

	rec := callGfeh(t, h.addGfehGrant, http.MethodPost, "/gfeh/grants/add", GfehGrantRequest{
		Network: "office", Principal: "alice", Path: "/photos",
		Perm: []string{gfeh.PermRead}, Inheritable: true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	var out GfehGrantView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ID == 0 {
		t.Error("no grant id was returned; the UI could not revoke it")
	}
	if out.Path != "/photos" || !out.Inheritable {
		t.Errorf("grant = %+v", out)
	}
}

func TestGfehAddGrantValidatesItsFields(t *testing.T) {
	h, _ := gfehUITest(t, map[string]gfeh.Client{"office": allViews("office", "office")})

	for _, tc := range []struct {
		name string
		req  GfehGrantRequest
	}{
		{"no principal", GfehGrantRequest{Network: "office", Path: "/x"}},
		{"no path", GfehGrantRequest{Network: "office", Principal: "alice"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := callGfeh(t, h.addGfehGrant, http.MethodPost, "/gfeh/grants/add", tc.req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status %d, want 400", rec.Code)
			}
		})
	}
}

// TestGfehGrantLifecycle: add, list, revoke.
func TestGfehGrantLifecycle(t *testing.T) {
	h, _ := gfehUITest(t, map[string]gfeh.Client{"office": allViews("office", "office")})

	rec := callGfeh(t, h.addGfehGrant, http.MethodPost, "/gfeh/grants/add", GfehGrantRequest{
		Network: "office", Principal: "alice", Path: "/", Perm: []string{gfeh.PermRead},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add: %d", rec.Code)
	}
	var added GfehGrantView
	if err := json.Unmarshal(rec.Body.Bytes(), &added); err != nil {
		t.Fatalf("decode: %v", err)
	}

	rec = callGfeh(t, h.listGfehGrants, http.MethodGet, "/gfeh/grants?network=office&principal=alice", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	var listed []GfehGrantView
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("got %d grants, want 1", len(listed))
	}

	rec = callGfeh(t, h.revokeGfehGrant, http.MethodPost, "/gfeh/grants/revoke",
		GfehRevokeRequest{Network: "office", ID: added.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d", rec.Code)
	}

	rec = callGfeh(t, h.listGfehGrants, http.MethodGet, "/gfeh/grants?network=office&principal=alice", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed) != 0 {
		t.Errorf("the grant survived revocation: %+v", listed)
	}
}

// TestGfehRevokeAnAbsentGrantIs404, not a server error.
func TestGfehRevokeAnAbsentGrantIs404(t *testing.T) {
	h, _ := gfehUITest(t, map[string]gfeh.Client{"office": allViews("office", "office")})

	rec := callGfeh(t, h.revokeGfehGrant, http.MethodPost, "/gfeh/grants/revoke",
		GfehRevokeRequest{Network: "office", ID: 999})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}
}

// TestGfehExposuresListAndWithdraw.
func TestGfehExposuresListAndWithdraw(t *testing.T) {
	client := allViews("office", "office")
	client.Exposures = []gfeh.Exposure{{Token: "abc", Path: "/photos/cat.jpg", Enabled: true}}
	h, _ := gfehUITest(t, map[string]gfeh.Client{"office": client})

	rec := callGfeh(t, h.listGfehExposures, http.MethodGet, "/gfeh/exposures?network=office", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	var listed []GfehExposureView
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed) != 1 || listed[0].Token != "abc" {
		t.Fatalf("exposures = %+v", listed)
	}

	rec = callGfeh(t, h.withdrawGfehExposure, http.MethodPost, "/gfeh/exposures/withdraw",
		GfehExposureRequest{Network: "office", Token: "abc"})
	if rec.Code != http.StatusOK {
		t.Fatalf("withdraw: %d", rec.Code)
	}
	if len(client.Exposures) != 0 {
		t.Error("the link still resolves after being withdrawn")
	}
}

// TestGfehAuditKeysCoverEveryMutatingRoute. A path missing from RouteActionKeys
// shows up in the audit log with an empty action, which is worse than not
// logging it: the row exists and says nothing.
func TestGfehAuditKeysCoverEveryMutatingRoute(t *testing.T) {
	for _, path := range []string{
		"/gfeh/partitions/create",
		"/gfeh/partitions/modify",
		"/gfeh/partitions/remove",
		"/gfeh/principals/add",
		"/gfeh/principals/remove",
		"/gfeh/grants/add",
		"/gfeh/grants/revoke",
		"/gfeh/exposures/withdraw",
	} {
		if _, ok := account.RouteActionKeys[path]; !ok {
			t.Errorf("%s has no audit action key", path)
		}
		if account.RouteActions[path] == "" {
			t.Errorf("%s has no localized audit action", path)
		}
	}
}

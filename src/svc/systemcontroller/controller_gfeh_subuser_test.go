// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"gitea.com/town-os/town-os/src/gfeh"
)

// decodeJSON reads a recorder's body into out, failing the test on malformed
// JSON rather than leaving the caller asserting against a zero value.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
}

// The grant that creates object-storage users.
//
// Two different things are called "creating a user" here and conflating them
// gives the wrong answer about who may do what:
//
//   - Projecting a Town OS account into a partition. That is POST
//     /gfeh/principals/add, it is Town OS's decision, and it is gated by
//     requireObjectStorage plus the network scope.
//   - Creating a sub-user *inside* the partition, beneath an existing
//     principal. That is gfeh's decision, governed by the `create-subuser`
//     right (gfeh.PermCreateSubuser) held as a grant, and Town OS neither
//     enforces nor can override it.
//
// These tests pin the second, because it is the one with no test at all and the
// one whose absence is invisible: a projected account arrives with a ceiling
// and *no grants*, so it cannot create a sub-user until somebody grants it the
// right — and if the grant is silently narrowed away, the UI must say so.

// withoutCreateSubuser is a clamp standing in for a ceiling that does not admit
// the right. It narrows exactly one permission and leaves the rest, which is
// what makes an assertion about the result unambiguous.
func withoutCreateSubuser(perm []string) []string {
	out := make([]string, 0, len(perm))
	for _, p := range perm {
		if p != gfeh.PermCreateSubuser {
			out = append(out, p)
		}
	}
	return out
}

// The right has to be offered at all: the UI builds its permission checkboxes
// from AllPerms, so a right missing from that list is a right an administrator
// cannot grant through Town OS no matter what gfehd would accept.
func TestCreateSubuserIsAnOfferablePermission(t *testing.T) {
	if !slices.Contains(gfeh.AllPerms, gfeh.PermCreateSubuser) {
		t.Fatalf("gfeh.AllPerms omits %q; nothing in the UI could ever grant it: %v",
			gfeh.PermCreateSubuser, gfeh.AllPerms)
	}
}

// A projected account starts with no grants whatsoever -- so it cannot create a
// sub-user, and cannot read a byte either. "Authenticating is not
// authorization" is the rule; this is the test that would fail if a future
// change started handing new principals a starter grant.
func TestProjectedAccountStartsWithNoGrants(t *testing.T) {
	client := &gfeh.MockClient{}
	h, acctMgr := gfehUITest(t, map[string]gfeh.Client{"office": client})
	mustAccount(t, acctMgr, "alice", false)

	rec := callGfeh(t, h.addGfehPrincipal, http.MethodPost, "/gfeh/principals/add",
		GfehPrincipalRequest{Network: "office", Principal: "alice"})
	if rec.Code != http.StatusOK {
		t.Fatalf("add principal = %d (%s)", rec.Code, rec.Body.String())
	}

	grants, err := client.ListGrants(t.Context(), "alice")
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 0 {
		t.Errorf("a freshly projected account holds %d grant(s): %+v", len(grants), grants)
	}
}

// An ordinary account projects the read-write ceiling, which is a data-plane
// authority: it does not carry the right to mint further users. The ceiling is
// what gfehd clamps against, so this is the fact that decides the next test.
func TestOrdinaryAccountCeilingIsNotAdministrative(t *testing.T) {
	ceiling := gfeh.CeilingForAccount(false)
	if slices.Contains(ceiling, gfeh.PermAll) {
		t.Errorf("an ordinary account projects %v, which admits everything", ceiling)
	}
	if slices.Contains(ceiling, gfeh.PermCreateSubuser) {
		t.Errorf("an ordinary account projects %v, which already grants sub-user creation", ceiling)
	}

	if admin := gfeh.CeilingForAccount(true); !slices.Contains(admin, gfeh.PermAll) {
		t.Errorf("an administrator projects %v, want the superuser ceiling", admin)
	}
}

// When the daemon narrows the grant, Town OS must report what came BACK, not
// what was asked for. Echoing the request would tell an administrator they had
// handed out sub-user creation when the daemon had refused it -- access nobody
// has, believed to exist, which is the failure mode worth a test.
func TestAddGrantReportsCreateSubuserNarrowedAway(t *testing.T) {
	client := &gfeh.MockClient{GrantClamp: withoutCreateSubuser}
	h, acctMgr := gfehUITest(t, map[string]gfeh.Client{"office": client})
	mustAccount(t, acctMgr, "alice", false)

	rec := callGfeh(t, h.addGfehGrant, http.MethodPost, "/gfeh/grants/add", GfehGrantRequest{
		Network:   "office",
		Principal: "alice",
		Path:      "/",
		Perm:      []string{gfeh.PermRead, gfeh.PermCreateSubuser},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add grant = %d (%s)", rec.Code, rec.Body.String())
	}

	var view GfehGrantView
	decodeJSON(t, rec, &view)
	if slices.Contains(view.Perm, gfeh.PermCreateSubuser) {
		t.Errorf("the response claims %q was granted; the daemon had narrowed it away: %v",
			gfeh.PermCreateSubuser, view.Perm)
	}
	if !slices.Contains(view.Perm, gfeh.PermRead) {
		t.Errorf("the rest of the grant was lost too: %v", view.Perm)
	}
}

// ... and when the ceiling does admit it, the right survives and is reported.
// Without this the test above passes for a system that silently drops the
// permission everywhere, which is the opposite bug.
func TestAddGrantKeepsCreateSubuserWhenTheCeilingAdmitsIt(t *testing.T) {
	client := &gfeh.MockClient{}
	h, acctMgr := gfehUITest(t, map[string]gfeh.Client{"office": client})
	// An administrator, so the ceiling this principal projects really is the
	// superuser one and the daemon has no reason to narrow anything.
	mustAccount(t, acctMgr, "root", true)

	rec := callGfeh(t, h.addGfehGrant, http.MethodPost, "/gfeh/grants/add", GfehGrantRequest{
		Network:     "office",
		Principal:   "root",
		Path:        "/",
		Perm:        []string{gfeh.PermCreateSubuser},
		Inheritable: true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add grant = %d (%s)", rec.Code, rec.Body.String())
	}

	var view GfehGrantView
	decodeJSON(t, rec, &view)
	if !slices.Contains(view.Perm, gfeh.PermCreateSubuser) {
		t.Errorf("the sub-user right was dropped: %v", view.Perm)
	}
	if !view.Inheritable {
		t.Error("the grant came back non-inheritable; a sub-user right that stops at one path is close to useless")
	}

	// And it is readable afterwards, which is what the Grants tab renders.
	grants, err := client.ListGrants(t.Context(), "root")
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 1 || !slices.Contains(grants[0].Perm, gfeh.PermCreateSubuser) {
		t.Errorf("stored grants = %+v, want one carrying %q", grants, gfeh.PermCreateSubuser)
	}
}

// Revoking is how the right is taken back, and it has to actually remove the
// row: a revoke that reported success while leaving the grant in place would
// leave sub-user creation available to somebody an administrator believes they
// have cut off.
func TestRevokingTheCreateSubuserGrantRemovesIt(t *testing.T) {
	client := &gfeh.MockClient{}
	h, acctMgr := gfehUITest(t, map[string]gfeh.Client{"office": client})
	mustAccount(t, acctMgr, "alice", false)

	add := callGfeh(t, h.addGfehGrant, http.MethodPost, "/gfeh/grants/add", GfehGrantRequest{
		Network: "office", Principal: "alice", Path: "/", Perm: []string{gfeh.PermCreateSubuser},
	})
	if add.Code != http.StatusOK {
		t.Fatalf("add grant = %d (%s)", add.Code, add.Body.String())
	}
	var view GfehGrantView
	decodeJSON(t, add, &view)

	rec := callGfeh(t, h.revokeGfehGrant, http.MethodPost, "/gfeh/grants/revoke",
		GfehRevokeRequest{Network: "office", ID: view.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke = %d (%s)", rec.Code, rec.Body.String())
	}

	grants, err := client.ListGrants(t.Context(), "alice")
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 0 {
		t.Errorf("the grant survived revocation: %+v", grants)
	}
}

// Removing the principal takes its grants with it. Otherwise a re-projected
// account of the same name would inherit whatever the previous one held --
// including sub-user creation nobody re-granted.
func TestRemovingAPrincipalTakesItsCreateSubuserGrant(t *testing.T) {
	client := &gfeh.MockClient{}
	h, acctMgr := gfehUITest(t, map[string]gfeh.Client{"office": client})
	mustAccount(t, acctMgr, "alice", false)

	if rec := callGfeh(t, h.addGfehPrincipal, http.MethodPost, "/gfeh/principals/add",
		GfehPrincipalRequest{Network: "office", Principal: "alice"}); rec.Code != http.StatusOK {
		t.Fatalf("add principal = %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := callGfeh(t, h.addGfehGrant, http.MethodPost, "/gfeh/grants/add", GfehGrantRequest{
		Network: "office", Principal: "alice", Path: "/", Perm: []string{gfeh.PermCreateSubuser},
	}); rec.Code != http.StatusOK {
		t.Fatalf("add grant = %d (%s)", rec.Code, rec.Body.String())
	}

	if rec := callGfeh(t, h.removeGfehPrincipal, http.MethodPost, "/gfeh/principals/remove",
		GfehPrincipalRequest{Network: "office", Principal: "alice"}); rec.Code != http.StatusOK {
		t.Fatalf("remove principal = %d (%s)", rec.Code, rec.Body.String())
	}

	grants, err := client.ListGrants(t.Context(), "alice")
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 0 {
		t.Errorf("grants outlived the principal: %+v", grants)
	}
}

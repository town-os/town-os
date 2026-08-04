// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration

import (
	"context"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/account"
)

// The home network exists on a box that has never booted a controller.
//
// Over a real database rather than the mock, because the invariant is about
// what is on disk: the row is written by InitNetworkManager alongside the
// tables, so it is there for the very first request the box ever serves --
// before boot reconcile, which is what used to create it.
func TestIntegrationHomeNetworkExistsBeforeAnythingCreatesIt(t *testing.T) {
	t.Parallel()

	db, err := account.OpenDB(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	nm, err := account.InitNetworkManager(db)
	if err != nil {
		t.Fatalf("InitNetworkManager: %v", err)
	}

	home, err := nm.Get(account.DefaultNetworkName)
	if err != nil {
		t.Fatalf("the home network is missing on a fresh database: %v", err)
	}
	if !home.Enabled {
		t.Error("the home network came back disabled")
	}
	nets, err := nm.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(nets) != 1 {
		t.Fatalf("networks = %+v, want just the home network", nets)
	}
}

// The first account on the box belongs to it.
//
// Driven through the HTTP bootstrap path -- the unauthenticated POST
// /account/create that only works while no administrator exists -- because that
// is the account in question, and it is created long before boot reconcile has
// run on a fresh box. The scope it comes back with therefore has to name a
// network that already exists.
func TestIntegrationFirstAccountBelongsToTheHomeNetwork(t *testing.T) {
	t.Parallel()
	e := initObjectStorageEnv(t)

	c, err := e.ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	c.Token = e.adminToken

	admin, err := c.GetAccount(context.Background(), "admin")
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if len(admin.Networks) != 1 || admin.Networks[0] != account.DefaultNetworkName {
		t.Errorf("first account scope = %v, want [%s]", admin.Networks, account.DefaultNetworkName)
	}

	// Membership, not confinement: it holds no explicit grants, is not
	// restricted, and its authority does not follow from the scope -- an
	// administrator is on every network, including ones created later.
	if admin.Restricted() {
		t.Error("the first account came back restricted")
	}
	if !admin.MayAdministerNetwork("a-network-created-tomorrow") {
		t.Error("an administrator's authority must not follow from its scope")
	}

	// And the same holds for an ordinary account created afterwards.
	create := `{"username":"resident","password":"residentpass1","email":"r@test.com","phone":"555-0004","real_name":"Resident","admin":false}`
	if code, out := e.post(t, "account/create", e.adminToken, create); code != 200 {
		t.Fatalf("create account = %d (%s), want 200", code, out)
	}
	resident, err := c.GetAccount(context.Background(), "resident")
	if err != nil {
		t.Fatalf("GetAccount resident: %v", err)
	}
	if len(resident.Networks) != 1 || resident.Networks[0] != account.DefaultNetworkName {
		t.Errorf("ordinary account scope = %v, want [%s]", resident.Networks, account.DefaultNetworkName)
	}
	if resident.Restricted() {
		t.Error("an ordinary account came back restricted")
	}
}

// A scoped account's networks are exactly what the administrator chose. Folding
// the home network into every account would widen a portal scoped to `office`
// into the home network -- and there the scope IS the confinement.
func TestIntegrationGrantedAccountScopeIsNotWidenedByHome(t *testing.T) {
	t.Parallel()
	e := initObjectStorageEnv(t)

	create := `{"username":"portal","password":"portalpass1","email":"p@test.com","phone":"555-0005","real_name":"Portal","grants":["gfeh"],"networks":["office"]}`
	if code, out := e.post(t, "account/create", e.adminToken, create); code != 200 {
		t.Fatalf("create granted account = %d (%s), want 200", code, out)
	}

	c, err := e.ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	c.Token = e.adminToken

	portal, err := c.GetAccount(context.Background(), "portal")
	if err != nil {
		t.Fatalf("GetAccount portal: %v", err)
	}
	if len(portal.Networks) != 1 || portal.Networks[0] != "office" {
		t.Errorf("granted account scope = %v, want [office]", portal.Networks)
	}
	if portal.MayAdministerNetwork(account.DefaultNetworkName) {
		t.Error("a scoped account was widened into the home network")
	}
}

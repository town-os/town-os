// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// objectStorageEnv is a controller over a REAL SQLite database, which is the
// point: the account kind is a column, and the interesting failures (a
// migration that defaults the wrong way, a kind that does not survive the
// restart which throws away every session) only exist against real storage.
type objectStorageEnv struct {
	dbPath     string
	ts         *systemcontroller.TestServer
	adminToken string
}

func initObjectStorageEnv(t *testing.T) *objectStorageEnv {
	t.Helper()
	env := &objectStorageEnv{dbPath: filepath.Join(t.TempDir(), "objectstorage.db")}
	env.start(t, "test-signing-key-for-sessions-32")

	c, err := env.ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	ctx := context.Background()
	if _, err := c.CreateAccount(ctx, "admin", "adminpass1", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(ctx, "admin", "adminpass1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	env.adminToken = resp.Token
	return env
}

// start brings a controller up over the env's database file. Called again by
// restart with a different signing key, which is what a real restart does.
func (e *objectStorageEnv) start(t *testing.T, signingKey string) {
	t.Helper()

	db, err := account.OpenDB(e.dbPath)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	sessMgr, err := account.InitSessionManager(db, mgr, []byte(signingKey))
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}
	nm, err := account.InitNetworkManager(db)
	if err != nil {
		t.Fatalf("InitNetworkManager: %v", err)
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:    storage.InitBtrFSMock(),
		AccountMgr: mgr,
		SessionMgr: sessMgr,
		NetworkMgr: nm,
	})
	t.Cleanup(ts.Close)
	e.ts = ts
}

// restart replaces the controller with a fresh one over the same database and a
// fresh signing key, exactly as a real restart does: every session is dropped,
// nothing in the accounts table is.
func (e *objectStorageEnv) restart(t *testing.T) {
	t.Helper()
	e.ts.Close()
	e.start(t, "second-signing-key-for-sessions")

	c, err := e.ts.Client()
	if err != nil {
		t.Fatalf("ts.Client after restart: %v", err)
	}
	resp, err := c.Authenticate(context.Background(), "admin", "adminpass1")
	if err != nil {
		t.Fatalf("Authenticate after restart: %v", err)
	}
	e.adminToken = resp.Token
}

// post issues an authenticated JSON POST and returns the status and body.
// Every route this file exercises is a POST -- the reads are covered by the
// unit tests, which can distinguish admitted from denied without a database.
func (e *objectStorageEnv) post(t *testing.T, path, token, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, e.ts.Server.URL+"/"+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest POST %s: %v", path, err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP POST %s: %v", path, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(out)
}

func (e *objectStorageEnv) login(t *testing.T, username, password string) string {
	t.Helper()
	c, err := e.ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	resp, err := c.Authenticate(context.Background(), username, password)
	if err != nil {
		t.Fatalf("Authenticate %s: %v", username, err)
	}
	return resp.Token
}

// addPrincipal is the canonical mutating request, on a named network.
//
// 503 is the authorized answer in this controller (it has no gfeh registry), so
// throughout this file "not 403" is what distinguishes admitted from denied.
func addPrincipal(network string) string {
	return `{"network":"` + network + `","principal":"storagehand"}`
}

// The whole feature end to end against real storage: an ordinary account is
// denied, an administrator makes it network-only, the account is admitted, and
// the kind survives the restart that invalidates every session.
func TestIntegrationNetworkOnlyKindSurvivesRestart(t *testing.T) {
	t.Parallel()
	e := initObjectStorageEnv(t)

	create := `{"username":"storagehand","password":"handpass123","email":"s@test.com","phone":"555-0002","real_name":"Storage Hand","admin":false}`
	if code, out := e.post(t, "account/create", e.adminToken, create); code != http.StatusOK {
		t.Fatalf("create account = %d (%s), want 200", code, out)
	}

	token := e.login(t, "storagehand", "handpass123")
	if code, out := e.post(t, "gfeh/principals/add", token, addPrincipal("office")); code != http.StatusForbidden {
		t.Fatalf("before the kind was granted = %d (%s), want 403", code, out)
	}

	grant := `{"username":"storagehand","fields":{"grants":["gfeh"],"networks":["office"]}}`
	if code, out := e.post(t, "account/update", e.adminToken, grant); code != http.StatusOK {
		t.Fatalf("grant = %d (%s), want 200", code, out)
	}
	if code, out := e.post(t, "gfeh/principals/add", token, addPrincipal("office")); code == http.StatusForbidden {
		t.Fatalf("after the grant the account was still denied (%s)", out)
	}

	e.restart(t)

	// The old token is dead -- the signing key changed and InitSessionManager
	// cleared the table -- so this is a genuinely new session over the old row.
	token = e.login(t, "storagehand", "handpass123")
	if code, out := e.post(t, "gfeh/principals/add", token, addPrincipal("office")); code == http.StatusForbidden {
		t.Fatalf("the network-only kind did not survive a restart (%s)", out)
	}

	// Demoting persists just as well. Both fields go in one request, because a
	// network-only account with an empty scope is a state the store refuses.
	revoke := `{"username":"storagehand","fields":{"grants":[],"networks":[]}}`
	if code, out := e.post(t, "account/update", e.adminToken, revoke); code != http.StatusOK {
		t.Fatalf("revoke = %d (%s), want 200", code, out)
	}
	e.restart(t)
	token = e.login(t, "storagehand", "handpass123")
	if code, out := e.post(t, "gfeh/principals/add", token, addPrincipal("office")); code != http.StatusForbidden {
		t.Errorf("after demotion and restart = %d (%s), want 403", code, out)
	}
}

// The scope is the whole difference between a network-only account and an
// administrator, and it has to hold across a restart: the scope is stored as a
// JSON column and read back through a different process than wrote it.
func TestIntegrationNetworkOnlyScopeHoldsAcrossRestart(t *testing.T) {
	t.Parallel()
	e := initObjectStorageEnv(t)

	create := `{"username":"storagehand","password":"handpass123","email":"s@test.com","phone":"555-0002","real_name":"Storage Hand","grants":["gfeh"],"networks":["office"]}`
	if code, out := e.post(t, "account/create", e.adminToken, create); code != http.StatusOK {
		t.Fatalf("create account = %d (%s), want 200", code, out)
	}

	for _, phase := range []string{"before restart", "after restart"} {
		token := e.login(t, "storagehand", "handpass123")

		if code, out := e.post(t, "gfeh/principals/add", token, addPrincipal("office")); code == http.StatusForbidden {
			t.Errorf("%s: denied on its own network (%s)", phase, out)
		}
		if code, out := e.post(t, "gfeh/principals/add", token, addPrincipal("home")); code != http.StatusForbidden {
			t.Errorf("%s: reached another network = %d (%s), want 403", phase, code, out)
		}

		if phase == "before restart" {
			e.restart(t)
		}
	}
}

// Partition provisioning is the line the kind does not cross: it is the
// contract surface gfeh's own client talks to, and it stays admin-only.
func TestIntegrationNetworkOnlyStopsAtPartitions(t *testing.T) {
	t.Parallel()
	e := initObjectStorageEnv(t)

	create := `{"username":"storagehand","password":"handpass123","email":"s@test.com","phone":"555-0002","real_name":"Storage Hand","grants":["gfeh"],"networks":["office"]}`
	if code, out := e.post(t, "account/create", e.adminToken, create); code != http.StatusOK {
		t.Fatalf("create account = %d (%s), want 200", code, out)
	}
	token := e.login(t, "storagehand", "handpass123")

	if code, out := e.post(t, "gfeh/partitions/create", token, `{"name":"photos","quota":0}`); code != http.StatusForbidden {
		t.Errorf("partition create as a network-only account = %d (%s), want 403", code, out)
	}

	// The administrator can still do it, so the route is refusing the caller
	// rather than being broken.
	if code, out := e.post(t, "gfeh/partitions/create", e.adminToken, `{"name":"photos","quota":0}`); code != http.StatusOK {
		t.Errorf("partition create as admin = %d (%s), want 200", code, out)
	}
}

// A pre-existing database must not gain authority by being opened by a newer
// controller. This is the migration's fail-closed direction, checked through
// the HTTP surface rather than against the column.
func TestIntegrationMigrationDoesNotGrantExistingAccounts(t *testing.T) {
	t.Parallel()
	e := initObjectStorageEnv(t)

	create := `{"username":"olduser","password":"olduserpass","email":"o@test.com","phone":"555-0003","real_name":"Old User","admin":false}`
	if code, out := e.post(t, "account/create", e.adminToken, create); code != http.StatusOK {
		t.Fatalf("create account = %d (%s), want 200", code, out)
	}

	e.restart(t)

	token := e.login(t, "olduser", "olduserpass")
	if code, out := e.post(t, "gfeh/principals/add", token, `{"network":"office","principal":"olduser"}`); code != http.StatusForbidden {
		t.Errorf("an untouched account = %d (%s), want 403", code, out)
	}
}

// startLegacyController brings a controller up over a database written at the
// previous release's schema, with the seeded administrator logged in.
//
// Written through a real controller rather than against the column, because
// this is the one path the unit tests cannot see whole: the column is migrated
// by InitManager, the grant set is read by the session layer, and the scope is
// enforced by a handler -- three layers that only meet here.
func startLegacyController(t *testing.T) *objectStorageEnv {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyAccountsDB(t, dbPath)

	e := &objectStorageEnv{dbPath: dbPath}
	e.start(t, "test-signing-key-for-sessions-32")

	// The bootstrap admin was seeded into the legacy table, and its password
	// hash was written by this same build, so it authenticates normally.
	e.adminToken = e.login(t, "admin", "adminpass1")
	return e
}

// The legacy object_storage column comes back as the gfeh grant, confined to
// the scope the old row carried: admitted on its own network, refused on
// another, and still short of partition provisioning.
func TestIntegrationLegacyObjectStorageAccountAdministersItsScope(t *testing.T) {
	t.Parallel()
	e := startLegacyController(t)

	token := e.login(t, "filer", "filerpass123")
	if code, out := e.post(t, "gfeh/principals/add", token, addPrincipal("office")); code == http.StatusForbidden {
		t.Errorf("a migrated object-storage account cannot administer its own network (%s)", out)
	}
	if code, out := e.post(t, "gfeh/principals/add", token, addPrincipal("home")); code != http.StatusForbidden {
		t.Errorf("a migrated account escaped its scope = %d (%s), want 403", code, out)
	}

	// It is still confined everywhere else -- the migration must not have
	// turned it into an ordinary dashboard account, nor into an administrator.
	if code, out := e.post(t, "gfeh/partitions/create", token, `{"name":"photos","quota":0}`); code != http.StatusForbidden {
		t.Errorf("a migrated account provisioned a partition = %d (%s), want 403", code, out)
	}
}

// Each legacy capability column becomes its own grant and no other, and an
// upgrade is where getting that wrong is invisible: nobody re-reads the
// permissions of an account that already existed.
//
// A row that only ever held the WireGuard capability keeps exactly peer
// enrollment. Widening it to object storage would hand an account that was
// enrolling a phone the run of a partition's principal forest -- the direction
// you cannot take back, since the account keeps its password and nothing on
// screen says its authority grew.
func TestIntegrationLegacyWireGuardAccountDoesNotGainObjectStorage(t *testing.T) {
	t.Parallel()
	e := startLegacyController(t)

	token := e.login(t, "portal", "portalpass1")
	if code, out := e.post(t, "gfeh/principals/add", token, addPrincipal("office")); code != http.StatusForbidden {
		t.Errorf("a migrated WireGuard account reached object storage = %d (%s), want 403", code, out)
	}
	if code, out := e.post(t, "gfeh/partitions/create", token, `{"name":"photos","quota":0}`); code != http.StatusForbidden {
		t.Errorf("a migrated WireGuard account provisioned a partition = %d (%s), want 403", code, out)
	}

	// ... and it did keep peer enrollment, which is the other half of the same
	// mistake: the migration must not quietly demote it to a dashboard account
	// either. This controller has no networks, so 404 is the admitted answer --
	// an account that had lost the grant would be refused by requirePeerEnroll
	// before the lookup, with a 403.
	enroll := `{"network":"office","public_key":"","name":"phone"}`
	if code, out := e.post(t, "networks/peers/add", token, enroll); code == http.StatusForbidden {
		t.Errorf("a migrated WireGuard account lost peer enrollment (%s)", out)
	}
}

// seedLegacyAccountsDB writes an accounts table at the previous release's
// schema, with an administrator and one account per legacy capability column:
// `portal` held wireguard, `filer` held object_storage. Both are scoped to
// `office`, so a migration that carried the wrong column into the wrong grant
// shows up as an admitted request rather than as an equally-refused one.
//
// The password hashes are produced by creating the rows through the CURRENT
// manager against a throwaway database and copying them across, so the hash
// format is whatever this build actually verifies -- a hand-written constant
// would be testing the migration against a credential nothing can log in with.
func seedLegacyAccountsDB(t *testing.T, path string) {
	t.Helper()

	hashes := map[string]string{
		"admin":  hashFor(t, "admin", "adminpass1", true),
		"portal": hashFor(t, "portal", "portalpass1", false),
		"filer":  hashFor(t, "filer", "filerpass123", false),
	}

	db, err := account.OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB legacy: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("legacy db.Close: %v", err)
		}
	}()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE accounts (
		username       TEXT PRIMARY KEY,
		password_hash  TEXT NOT NULL,
		email          TEXT NOT NULL,
		phone          TEXT NOT NULL,
		real_name      TEXT NOT NULL,
		admin          INTEGER NOT NULL DEFAULT 0,
		disabled       INTEGER NOT NULL DEFAULT 0,
		wireguard      INTEGER NOT NULL DEFAULT 0,
		networks       TEXT NOT NULL DEFAULT '[]',
		object_storage INTEGER NOT NULL DEFAULT 0,
		smb_nt_hash    TEXT NOT NULL DEFAULT '',
		created_at     TEXT NOT NULL,
		updated_at     TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create legacy accounts table: %v", err)
	}

	insert := `INSERT INTO accounts
		(username, password_hash, email, phone, real_name, admin, disabled, wireguard, networks, object_storage, smb_nt_hash, created_at, updated_at)
		VALUES (?, ?, ?, '555-0000', ?, ?, 0, ?, ?, ?, ?, '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`

	for _, row := range []struct {
		username, email, realName    string
		admin, wireguard, objStorage int
		networks, ntHash             string
	}{
		{"admin", "admin@test.com", "Admin", 1, 0, 0, "[]", ""},
		{"portal", "portal@test.com", "Portal", 0, 1, 0, `["office"]`, "a4f49c406510bdcab6824ee7c30fd852"},
		{"filer", "filer@test.com", "Filer", 0, 0, 1, `["office"]`, "b4f49c406510bdcab6824ee7c30fd852"},
	} {
		if _, err := db.ExecContext(ctx, insert,
			row.username, hashes[row.username], row.email, row.realName,
			row.admin, row.wireguard, row.networks, row.objStorage, row.ntHash,
		); err != nil {
			t.Fatalf("insert legacy row %s: %v", row.username, err)
		}
	}
}

// hashFor creates an account in a throwaway database and returns the stored
// password hash, so the legacy fixture carries a credential this build can
// actually verify.
func hashFor(t *testing.T, username, password string, admin bool) string {
	t.Helper()

	db, err := account.OpenDB(filepath.Join(t.TempDir(), "hash-"+username+".db"))
	if err != nil {
		t.Fatalf("OpenDB for hash: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("hash db.Close: %v", err)
		}
	}()

	mgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager for hash: %v", err)
	}
	if _, err := mgr.Create(username, password, username+"@test.com", "555-0000", "Hash Source", admin); err != nil {
		t.Fatalf("Create for hash: %v", err)
	}

	var hash string
	row := db.QueryRowContext(context.Background(), "SELECT password_hash FROM accounts WHERE username = ?", username)
	if err := row.Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("no row for %s after Create", username)
		}
		t.Fatalf("scan password hash: %v", err)
	}
	return hash
}

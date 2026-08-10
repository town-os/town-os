// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/storage"
)

func TestHTTPCreateAccount(t *testing.T) {
	c, _ := initAccountTestClient(t)

	acct, err := c.CreateAccount(context.TODO(), "alice", "password123", "alice@test.com", "555-1234", "Alice Smith", false)
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	if acct.Username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", acct.Username)
	}
	if acct.Email != "alice@test.com" {
		t.Fatalf("expected email %q, got %q", "alice@test.com", acct.Email)
	}
}

func TestHTTPCreateAccountDuplicate(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("first CreateAccount: %v", err)
	}

	_, err := c.CreateAccount(context.TODO(), "alice", "password2", "c@d.com", "666", "Alice2", false)
	if err == nil {
		t.Fatal("expected error creating duplicate account")
	}
}

func TestHTTPGetAccount(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	acct, err := c.GetAccount(context.TODO(), "alice")
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}

	if acct.Username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", acct.Username)
	}
}

func TestHTTPGetAccountNotFound(t *testing.T) {
	c, _ := initAccountTestClient(t)

	_, err := c.GetAccount(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent account")
	}
}

func TestHTTPUpdateAccount(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	newEmail := "new@example.com"
	acct, err := c.UpdateAccount(context.TODO(), "alice", account.UpdateFields{Email: &newEmail})
	if err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}

	if acct.Email != "new@example.com" {
		t.Fatalf("expected email %q, got %q", "new@example.com", acct.Email)
	}
}

func TestHTTPDisableAccount(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create admin to perform the disable
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "admin@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount admin: %v", err)
	}
	adminResp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}

	// disable alice using admin token
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("account/disable"), bytes.NewBufferString(`{"username":"alice"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+adminResp.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// verify alice is disabled (can still be fetched)
	acct, err := c.GetAccount(context.TODO(), "alice")
	if err != nil {
		t.Fatalf("GetAccount after disable: %v", err)
	}
	if !acct.Disabled {
		t.Fatal("expected account to be disabled")
	}

	// verify disabled account cannot authenticate
	_, err = c.Authenticate(context.TODO(), "alice", "password1")
	if err == nil {
		t.Fatal("expected error authenticating disabled account")
	}
}

func TestHTTPEnableAccount(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create admin
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "admin@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount admin: %v", err)
	}
	adminResp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}

	// create and disable alice
	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("account/disable"), bytes.NewBufferString(`{"username":"alice"}`))
	if err != nil {
		t.Fatalf("NewRequestWithContext disable: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+adminResp.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do disable: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("resp.Body.Close: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable: expected 200, got %d", resp.StatusCode)
	}

	// verify disabled
	acct, err := c.GetAccount(context.TODO(), "alice")
	if err != nil {
		t.Fatalf("GetAccount after disable: %v", err)
	}
	if !acct.Disabled {
		t.Fatal("expected account to be disabled")
	}

	// enable alice
	req, err = http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("account/enable"), bytes.NewBufferString(`{"username":"alice"}`))
	if err != nil {
		t.Fatalf("NewRequestWithContext enable: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+adminResp.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do enable: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("resp.Body.Close: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enable: expected 200, got %d", resp.StatusCode)
	}

	// verify enabled
	acct, err = c.GetAccount(context.TODO(), "alice")
	if err != nil {
		t.Fatalf("GetAccount after enable: %v", err)
	}
	if acct.Disabled {
		t.Fatal("expected account to be enabled")
	}

	// verify can authenticate again
	if _, err := c.Authenticate(context.TODO(), "alice", "password1"); err != nil {
		t.Fatalf("Authenticate after enable: %v", err)
	}
}

func TestHTTPAdminChangeRejected(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create admin and a regular user
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "admin@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount admin: %v", err)
	}
	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}

	adminResp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}
	c.Token = adminResp.Token

	// admin tries to promote alice -- should get 403
	adminTrue := true
	_, err = c.UpdateAccount(context.TODO(), "alice", account.UpdateFields{Admin: &adminTrue})
	if err == nil {
		t.Fatal("expected error when changing admin status")
	}

	// verify alice is still not admin
	alice, err := c.GetAccount(context.TODO(), "alice")
	if err != nil {
		t.Fatalf("GetAccount alice: %v", err)
	}
	if alice.Admin {
		t.Fatal("alice should not be admin")
	}
}

func TestHTTPAdminDemoteRejected(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create two admins
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "admin@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount admin: %v", err)
	}
	if _, err := c.CreateAccount(context.TODO(), "admin2", "password1", "a2@b.com", "555", "Admin2", true); err != nil {
		t.Fatalf("CreateAccount admin2: %v", err)
	}

	adminResp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}
	c.Token = adminResp.Token

	// admin tries to demote admin2 -- should get 403
	adminFalse := false
	_, err = c.UpdateAccount(context.TODO(), "admin2", account.UpdateFields{Admin: &adminFalse})
	if err == nil {
		t.Fatal("expected error when changing admin status")
	}

	// verify admin2 is still admin
	admin2, err := c.GetAccount(context.TODO(), "admin2")
	if err != nil {
		t.Fatalf("GetAccount admin2: %v", err)
	}
	if !admin2.Admin {
		t.Fatal("admin2 should still be admin")
	}
}

func TestHTTPListAccounts(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}
	if _, err := c.CreateAccount(context.TODO(), "bob", "password1", "b@b.com", "666", "Bob", false); err != nil {
		t.Fatalf("CreateAccount bob: %v", err)
	}

	accounts, err := c.ListAccounts(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}

	// 3 = testadmin (bootstrap) + alice + bob
	if len(accounts.Entries) != 3 {
		t.Fatalf("expected 3 accounts, got %d", len(accounts.Entries))
	}
}

func TestHTTPListAccountsSortByUsername(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// Create accounts in non-alphabetical order
	for _, name := range []string{"charlie", "alice", "bob"} {
		if _, err := c.CreateAccount(context.TODO(), name, "password1", name+"@test.com", "555", name, false); err != nil {
			t.Fatalf("CreateAccount %q: %v", name, err)
		}
	}

	// Sort ascending by username
	accounts, err := c.ListAccounts(context.TODO(), ListParams{SortBy: "username", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListAccounts sort asc: %v", err)
	}

	// testadmin (bootstrap) + alice + bob + charlie = 4
	if len(accounts.Entries) != 4 {
		t.Fatalf("expected 4 accounts, got %d", len(accounts.Entries))
	}

	// First should be alice (alphabetically after testadmin? No, 'a' < 't')
	if accounts.Entries[0].Username != "alice" {
		t.Fatalf("expected first account %q, got %q", "alice", accounts.Entries[0].Username)
	}
	if accounts.Entries[1].Username != "bob" {
		t.Fatalf("expected second account %q, got %q", "bob", accounts.Entries[1].Username)
	}

	// Sort descending by username
	accountsDesc, err := c.ListAccounts(context.TODO(), ListParams{SortBy: "username", SortOrder: "desc"})
	if err != nil {
		t.Fatalf("ListAccounts sort desc: %v", err)
	}

	if accountsDesc.Entries[0].Username != "testadmin" {
		t.Fatalf("expected first desc account %q, got %q", "testadmin", accountsDesc.Entries[0].Username)
	}
}

func TestHTTPListAccountsSortByAdmin(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "user1", "password1", "u1@test.com", "555", "User1", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if _, err := c.CreateAccount(context.TODO(), "admin2", "password1", "a2@test.com", "555", "Admin2", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	// Sort ascending by admin (false < true)
	accounts, err := c.ListAccounts(context.TODO(), ListParams{SortBy: "admin", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}

	// Non-admins should come first
	if accounts.Entries[0].Admin {
		t.Fatal("expected first account to be non-admin when sorted asc by admin")
	}
}

func TestHTTPListAccountsNoSort(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@test.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	// No sort params should still work
	accounts, err := c.ListAccounts(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}

	if len(accounts.Entries) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accounts.Entries))
	}
}

func TestHTTPListAccountsPagination(t *testing.T) {
	c, _ := initAccountTestClient(t)

	names := []string{"alice", "bob", "charlie", "diana"}
	emails := []string{"alice@test.com", "bob@test.com", "charlie@test.com", "diana@test.com"}
	for i, name := range names {
		if _, err := c.CreateAccount(context.TODO(), name, "password1", emails[i], "555-0001", name, false); err != nil {
			t.Fatalf("CreateAccount %q: %v", name, err)
		}
	}

	// 5 total accounts: testadmin (bootstrapped) + alice, bob, charlie, diana

	// Page 1: limit=2, offset=0
	result, err := c.ListAccounts(context.TODO(), ListParams{Limit: 2, Offset: 0, SortBy: "username", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListAccounts page1: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries on page1, got %d", len(result.Entries))
	}
	if !result.HasMore {
		t.Fatal("expected has_more=true on page1")
	}
	if result.TotalCount != 5 {
		t.Fatalf("expected total_count=5, got %d", result.TotalCount)
	}

	// Page 2: limit=2, offset=2
	result, err = c.ListAccounts(context.TODO(), ListParams{Limit: 2, Offset: 2, SortBy: "username", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListAccounts page2: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries on page2, got %d", len(result.Entries))
	}
	if !result.HasMore {
		t.Fatal("expected has_more=true on page2")
	}

	// Page 3: limit=2, offset=4
	result, err = c.ListAccounts(context.TODO(), ListParams{Limit: 2, Offset: 4, SortBy: "username", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListAccounts page3: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry on page3, got %d", len(result.Entries))
	}
	if result.HasMore {
		t.Fatal("expected has_more=false on last page")
	}
}

func TestHTTPListAccountsSearch(t *testing.T) {
	c, _ := initAccountTestClient(t)

	names := []string{"alice", "bob", "charlie"}
	emails := []string{"alice@test.com", "bob@test.com", "charlie@test.com"}
	for i, name := range names {
		if _, err := c.CreateAccount(context.TODO(), name, "password1", emails[i], "555-0001", name, false); err != nil {
			t.Fatalf("CreateAccount %q: %v", name, err)
		}
	}

	result, err := c.ListAccounts(context.TODO(), ListParams{Search: "alic"})
	if err != nil {
		t.Fatalf("ListAccounts search: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 result for search 'alic', got %d", len(result.Entries))
	}
	if result.Entries[0].Username != "alice" {
		t.Fatalf("expected alice, got %s", result.Entries[0].Username)
	}

	result, err = c.ListAccounts(context.TODO(), ListParams{Search: "nope"})
	if err != nil {
		t.Fatalf("ListAccounts search nope: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 results for search 'nope', got %d", len(result.Entries))
	}
}

func TestHTTPCreateAccountBootstrap(t *testing.T) {
	// Fresh DB with no accounts — createAccount should succeed without auth
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
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
	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(t.Context(), db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, AccountMgr: mgr, SessionMgr: sessMgr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// No token set — bootstrap should allow account creation
	acct, err := c.CreateAccount(context.TODO(), "first", "password1", "f@b.com", "555", "First", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	if acct.Username != "first" {
		t.Fatalf("expected username %q, got %q", "first", acct.Username)
	}
}

func TestHTTPCreateAccountRequiresAuthWhenAccountsExist(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// Clear the token — unauthenticated request
	c.Token = ""

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("account/create"), bytes.NewBufferString(`{"username":"mallory","password":"password1","email":"m@b.com","phone":"555","real_name":"Mallory","admin":false}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if httpResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated create, got %d", httpResp.StatusCode)
	}
}

func TestHTTPCreateAccountNonAdminForbidden(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create non-admin user
	if _, err := c.CreateAccount(context.TODO(), "user", "password1", "u@b.com", "555", "User", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "user", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// try to create account with non-admin token
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("account/create"), bytes.NewBufferString(`{"username":"mallory","password":"password1","email":"m@b.com","phone":"555","real_name":"Mallory","admin":false}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+resp.Token)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if httpResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin create, got %d", httpResp.StatusCode)
	}
}

func TestHTTPCreateAccountBootstrapAllDisabled(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// disable the bootstrap admin
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("account/disable"), bytes.NewBufferString(`{"username":"testadmin"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	if err := httpResp.Body.Close(); err != nil {
		t.Errorf("resp.Body.Close: %v", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for disable, got %d", httpResp.StatusCode)
	}

	// all accounts disabled — bootstrap should allow unauthenticated create
	c.Token = ""
	acct, err := c.CreateAccount(context.TODO(), "newadmin", "password1", "n@b.com", "555", "New Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount after all disabled: %v", err)
	}
	if acct.Username != "newadmin" {
		t.Fatalf("expected username %q, got %q", "newadmin", acct.Username)
	}
}

func TestHTTPCreateAccountBootstrapNoAdmins(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// Create a non-admin user
	if _, err := c.CreateAccount(context.TODO(), "regularuser", "password1", "r@b.com", "555", "Regular", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	// Disable the only admin
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("account/disable"), bytes.NewBufferString(`{"username":"testadmin"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	if err := httpResp.Body.Close(); err != nil {
		t.Errorf("resp.Body.Close: %v", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for disable, got %d", httpResp.StatusCode)
	}

	// Ping should report 0 admins
	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if ping.Admins != 0 {
		t.Fatalf("expected 0 admins, got %d", ping.Admins)
	}
	if ping.Accounts != 2 {
		t.Fatalf("expected 2 accounts, got %d", ping.Accounts)
	}

	// No enabled admin — bootstrap should allow unauthenticated create
	c.Token = ""
	acct, err := c.CreateAccount(context.TODO(), "newadmin", "password1", "n@b.com", "555", "New Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount with no admins: %v", err)
	}
	if acct.Username != "newadmin" {
		t.Fatalf("expected username %q, got %q", "newadmin", acct.Username)
	}
}

func TestHTTPCreateAccountBootstrapWithStaleToken(t *testing.T) {
	// Fresh DB with no accounts — a stale/invalid bearer token should be ignored.
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
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
	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(t.Context(), db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, AccountMgr: mgr, SessionMgr: sessMgr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Set a stale token — bootstrap mode should ignore it.
	c.Token = "stale-garbage-token-from-previous-session"

	acct, err := c.CreateAccount(context.TODO(), "first", "password1", "f@b.com", "555", "First", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount with stale token: %v", err)
	}
	if acct.Username != "first" {
		t.Fatalf("expected username %q, got %q", "first", acct.Username)
	}
}

func TestHTTPCreateAccountBootstrapAllDisabledWithStaleToken(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// Disable the bootstrap admin.
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("account/disable"), bytes.NewBufferString(`{"username":"testadmin"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	if err := httpResp.Body.Close(); err != nil {
		t.Errorf("resp.Body.Close: %v", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for disable, got %d", httpResp.StatusCode)
	}

	// Keep the (now-stale) token — bootstrap mode should ignore it.
	acct, err := c.CreateAccount(context.TODO(), "newadmin", "password1", "n@b.com", "555", "New Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount with stale token after all disabled: %v", err)
	}
	if acct.Username != "newadmin" {
		t.Fatalf("expected username %q, got %q", "newadmin", acct.Username)
	}
}

func TestHTTPCreateAccountRejectsWhenAdminExistsNoSession(t *testing.T) {
	// Admin exists but was never authenticated — unauthenticated create should
	// be rejected because an enabled admin account exists.
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
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
	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(t.Context(), db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, AccountMgr: mgr, SessionMgr: sessMgr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Bootstrap first admin (no auth required on empty DB).
	if _, err := c.CreateAccount(context.TODO(), "first", "password1", "f@b.com", "555", "First", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}

	// Admin exists — unauthenticated create should be rejected.
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("account/create"), bytes.NewBufferString(`{"username":"second","password":"password1","email":"s@b.com","phone":"555","real_name":"Second","admin":true}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if httpResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated create when admin exists, got %d", httpResp.StatusCode)
	}
}

func TestHTTPCreateAccountRejectsStaleTokenWhenAdminExists(t *testing.T) {
	// Admin exists, stale token — should be rejected because admin exists.
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
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
	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(t.Context(), db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, AccountMgr: mgr, SessionMgr: sessMgr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Bootstrap first admin.
	if _, err := c.CreateAccount(context.TODO(), "first", "password1", "f@b.com", "555", "First", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}

	// Set a stale token — admin exists, should reject.
	c.Token = "stale-garbage-token-from-previous-session"

	_, err = c.CreateAccount(context.TODO(), "second", "password1", "s@b.com", "555", "Second", true)
	if err == nil {
		t.Fatal("expected error creating account with stale token when admin exists")
	}
}

func TestHTTPCreateAccountRejectsWithoutToken(t *testing.T) {
	// Admin exists + no token → 401.
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
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
	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(t.Context(), db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, AccountMgr: mgr, SessionMgr: sessMgr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Bootstrap first admin.
	if _, err := c.CreateAccount(context.TODO(), "first", "password1", "f@b.com", "555", "First", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}

	// Clear token — admin exists, should require auth.
	c.Token = ""

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("account/create"), bytes.NewBufferString(`{"username":"intruder","password":"password1","email":"i@b.com","phone":"555","real_name":"Intruder","admin":false}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if httpResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated create when admin exists, got %d", httpResp.StatusCode)
	}
}

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
)

// --- Audit log tests ---

func TestHTTPAuditLogLifecycle(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create admin and authenticate
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// perform an action (create another account) using admin token
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("account/create"), bytes.NewBufferString(`{"username":"alice","password":"password1","email":"a@b.com","phone":"555","real_name":"Alice","admin":false}`))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+resp.Token)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	if err := httpResp.Body.Close(); err != nil {
		t.Errorf("resp.Body.Close: %v", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", httpResp.StatusCode)
	}

	// query audit log
	page, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}

	if len(page.Entries) == 0 {
		t.Fatal("expected at least one audit entry")
	}

	// find the create account entry
	found := false
	for _, e := range page.Entries {
		if e.Action == "create account" && e.Path == "/account/create" {
			found = true
			if !e.Success {
				t.Fatal("expected success to be true")
			}
			if e.Account != "admin" {
				t.Fatalf("expected account %q, got %q", "admin", e.Account)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected to find 'create account' audit entry")
	}
}

func TestHTTPAuditLogLocalhostAllowed(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create non-admin user
	if _, err := c.CreateAccount(context.TODO(), "user", "password1", "u@b.com", "555", "User", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "user", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// Localhost requests are allowed regardless of admin status
	// (localhostOrAuth middleware).
	_, err = c.ListAuditLog(context.TODO(), account.AuditListOptions{}, resp.Token)
	if err != nil {
		t.Fatalf("expected localhost audit log access to succeed: %v", err)
	}
}

func TestHTTPAuditLogPagination(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create admin
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// perform multiple actions via authenticated requests
	for i := range 5 {
		username := fmt.Sprintf("user%d", i)
		body := fmt.Sprintf(`{"username":"%s","password":"password1","email":"%s@b.com","phone":"555","real_name":"User %d","admin":false}`, username, username, i)
		req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("account/create"), bytes.NewBufferString(body))
		if err != nil {
			t.Fatalf("NewRequestWithContext: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+resp.Token)
		req.Header.Set("Content-Type", "application/json")

		httpResp, err := c.HTTP.Do(req)
		if err != nil {
			t.Fatalf("HTTP Do: %v", err)
		}
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}

	// get first page of 2
	page, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{Limit: 2}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog page 1: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected HasMore to be true")
	}

	// get second page using cursor
	cursor := page.Entries[len(page.Entries)-1].ID
	page2, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{BeforeID: cursor, Limit: 2}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog page 2: %v", err)
	}
	if len(page2.Entries) < 1 {
		t.Fatal("expected at least 1 entry in page 2")
	}

	// entries should not overlap
	for _, e1 := range page.Entries {
		for _, e2 := range page2.Entries {
			if e1.ID == e2.ID {
				t.Fatalf("found duplicate ID %d across pages", e1.ID)
			}
		}
	}
}

func TestHTTPAuditLogExcludesSessionRoutes(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	// create user and authenticate
	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "alice", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// call session routes
	if _, err := c.ListSessions(context.TODO(), resp.Token); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if _, err := c.SessionUsername(context.TODO(), resp.Token); err != nil {
		t.Fatalf("SessionUsername: %v", err)
	}

	// call ping
	if _, err := c.Ping(context.TODO()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	// hit newly excluded read-only package/page routes.
	// The handlers may error (no package manager in test), but the audit
	// middleware exclusion fires before handler execution matters for logging.
	excludedGETs := []string{"/packages/featured", "/pages"}
	for _, path := range excludedGETs {
		req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, c.route(path), nil)
		if err != nil {
			t.Fatalf("NewRequest GET %s: %v", path, err)
		}
		req.Header.Set("Authorization", "Bearer "+resp.Token)
		httpResp, err := c.HTTP.Do(req)
		if err == nil {
			_ = httpResp.Body.Close()
		}
	}

	excludedPOSTs := []string{"/packages/installed/info", "/packages/last-responses", "/packages/install-preview", "/packages/manifest"}
	for _, path := range excludedPOSTs {
		req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route(path), bytes.NewBufferString("{}"))
		if err != nil {
			t.Fatalf("NewRequest POST %s: %v", path, err)
		}
		req.Header.Set("Authorization", "Bearer "+resp.Token)
		req.Header.Set("Content-Type", "application/json")
		httpResp, err := c.HTTP.Do(req)
		if err == nil {
			_ = httpResp.Body.Close()
		}
	}

	// check audit log - session routes, ping, and read-only package/page routes should not be logged
	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, e := range page.Entries {
		switch e.Path {
		case "/account/sessions", "/account/me", "/status/ping",
			"/packages/featured", "/packages/installed/info",
			"/packages/last-responses", "/packages/install-preview",
			"/packages/manifest", "/pages":
			t.Fatalf("expected path %q to be excluded from audit log", e.Path)
		}
	}
}

func TestHTTPAuditLogExcludesReadOnlyPackageRoutes(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	// create admin and authenticate
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// call read-only package endpoints (they will error because no packages are
	// installed, but the audit middleware checks exclusion before logging)
	// POST read-only endpoints
	postPaths := []string{
		"/packages/installed/info",
		"/packages/last-responses",
		"/packages/install-preview",
		"/packages/manifest",
		"/storage/package-volumes",
	}

	for _, path := range postPaths {
		body := `{"repo":"test","name":"pkg","version":"1.0"}`
		req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route(path[1:]), bytes.NewBufferString(body))
		if err != nil {
			t.Fatalf("NewRequest for %s: %v", path, err)
		}
		req.Header.Set("Authorization", "Bearer "+resp.Token)
		req.Header.Set("Content-Type", "application/json")

		// Handlers may fail (e.g. nil installer) since the test environment
		// does not set up package infrastructure. We only care that no audit
		// entry is created for these paths.
		httpResp, err := c.HTTP.Do(req)
		if err != nil {
			continue
		}
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}

	// GET read-only endpoints
	getPaths := []string{
		"/packages/featured",
		"/pages",
	}

	for _, path := range getPaths {
		req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, c.route(path[1:]), nil)
		if err != nil {
			t.Fatalf("NewRequest for %s: %v", path, err)
		}
		req.Header.Set("Authorization", "Bearer "+resp.Token)

		httpResp, err := c.HTTP.Do(req)
		if err != nil {
			continue
		}
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}

	readOnlyPaths := make([]string, 0, len(postPaths)+len(getPaths))
	readOnlyPaths = append(readOnlyPaths, postPaths...)
	readOnlyPaths = append(readOnlyPaths, getPaths...)

	// verify none of the read-only package paths appear in the audit log
	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, e := range page.Entries {
		for _, path := range readOnlyPaths {
			if e.Path == path {
				t.Fatalf("expected read-only path %q to be excluded from audit log", path)
			}
		}
	}
}

func TestHTTPAuditLogExcludesDNSReadOnlyRoutes(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	// create admin and authenticate
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// Hit DNS read-only endpoints. Handlers will likely error (no DNS service
	// in unit tests), but the audit middleware exclusion fires regardless.
	dnsReadOnly := []string{"/dns/status", "/dns/records", "/dns/tld", "/dns/rbl/local", "/dns/dnsbl/allowlist"}
	for _, path := range dnsReadOnly {
		req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, c.route(path[1:]), nil)
		if err != nil {
			t.Fatalf("NewRequest GET %s: %v", path, err)
		}
		req.Header.Set("Authorization", "Bearer "+resp.Token)
		httpResp, err := c.HTTP.Do(req)
		if err == nil {
			_ = httpResp.Body.Close()
		}
	}

	// Verify none of the DNS read-only paths appear in the audit log
	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, e := range page.Entries {
		for _, path := range dnsReadOnly {
			if e.Path == path {
				t.Fatalf("expected DNS read-only path %q to be excluded from audit log", path)
			}
		}
	}
}

func TestHTTPAuditLogIncludesAuthRoutes(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	// create user and authenticate
	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	if _, err := c.Authenticate(context.TODO(), "alice", "password1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// check audit log for authenticate entry
	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	foundAuth := false
	for _, e := range page.Entries {
		if e.Path == "/account/authenticate" {
			foundAuth = true
			break
		}
	}
	if !foundAuth {
		t.Fatal("expected authenticate route to be in audit log")
	}
}

func TestHTTPAuditLogSortByAccount(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	// create admin and authenticate
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// Directly insert audit entries with different accounts to test sort
	for _, user := range []string{"charlie", "alice", "bob"} {
		if err := auditMgr.LogEntry(account.AuditEntry{
			Account:   user,
			Action:    "test",
			Path:      "/test",
			Success:   true,
		}); err != nil {
			t.Fatalf("LogEntry: %v", err)
		}
	}

	// Sort ascending by account
	page, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{
		SortBy:    "account",
		SortOrder: "asc",
	}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog sorted: %v", err)
	}

	// Find the test entries (skip audit entries from the create/auth operations)
	var testEntries []account.AuditEntry
	for _, e := range page.Entries {
		if e.Action == "test" {
			testEntries = append(testEntries, e)
		}
	}

	if len(testEntries) < 3 {
		t.Fatalf("expected at least 3 test entries, got %d", len(testEntries))
	}

	if testEntries[0].Account != "alice" {
		t.Fatalf("expected first test entry account %q, got %q", "alice", testEntries[0].Account)
	}
	if testEntries[1].Account != "bob" {
		t.Fatalf("expected second test entry account %q, got %q", "bob", testEntries[1].Account)
	}
	if testEntries[2].Account != "charlie" {
		t.Fatalf("expected third test entry account %q, got %q", "charlie", testEntries[2].Account)
	}
}

func TestHTTPAuditLogSortByIDASc(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create admin and authenticate
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// Perform a few actions to create audit entries
	for i := range 3 {
		username := fmt.Sprintf("sortuser%d", i)
		body := fmt.Sprintf(`{"username":"%s","password":"password1","email":"%s@b.com","phone":"555","real_name":"User %d","admin":false}`, username, username, i)
		req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("account/create"), bytes.NewBufferString(body))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+resp.Token)
		req.Header.Set("Content-Type", "application/json")

		httpResp, err := c.HTTP.Do(req)
		if err != nil {
			t.Fatalf("HTTP Do: %v", err)
		}
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}

	// Sort ascending by ID
	page, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{
		SortBy:    "id",
		SortOrder: "asc",
	}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog sorted asc: %v", err)
	}

	if len(page.Entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(page.Entries))
	}

	// Ascending: IDs should increase
	for i := 1; i < len(page.Entries); i++ {
		if page.Entries[i].ID < page.Entries[i-1].ID {
			t.Fatalf("entry %d ID (%d) < entry %d ID (%d) in asc sort", i, page.Entries[i].ID, i-1, page.Entries[i-1].ID)
		}
	}

	// Sort descending by ID (default behavior)
	pageDesc, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{
		SortBy:    "id",
		SortOrder: "desc",
	}, resp.Token)
	if err != nil {
		t.Fatalf("ListAuditLog sorted desc: %v", err)
	}

	// Descending: IDs should decrease
	for i := 1; i < len(pageDesc.Entries); i++ {
		if pageDesc.Entries[i].ID > pageDesc.Entries[i-1].ID {
			t.Fatalf("entry %d ID (%d) > entry %d ID (%d) in desc sort", i, pageDesc.Entries[i].ID, i-1, pageDesc.Entries[i-1].ID)
		}
	}
}

func TestSanitizeAuditDetail(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		// The sanitizer masks rather than deletes: an auditor should be able to
		// tell a field that was present and withheld from one that was never
		// sent at all. See auditRedactedKeys / auditOpaqueKeys.
		{
			name: "masks password at top level",
			body: `{"username":"admin","password":"secret"}`,
			want: `{"password":"[REDACTED]","username":"admin"}`,
		},
		{
			name: "masks password inside fields, keeping the rest",
			body: `{"username":"admin","fields":{"password":"new","real_name":"Bob"}}`,
			want: `{"fields":{"password":"[REDACTED]","real_name":"Bob"},"username":"admin"}`,
		},
		{
			name: "preserves body without password",
			body: `{"name":"nginx","version":"1.0"}`,
			want: `{"name":"nginx","version":"1.0"}`,
		},
		{
			name: "returns empty for invalid JSON",
			body: `not json`,
			want: "",
		},
		{
			name: "returns empty for empty body",
			body: ``,
			want: "",
		},
		{
			// A package's answers are keyed by names only its author knows, so
			// there is no vocabulary to match on and the whole subtree goes.
			// This is where `type: secret` answers live.
			name: "redacts the whole install responses subtree",
			body: `{"name":"nginx","responses":{"port":"8080"}}`,
			want: `{"name":"nginx","responses":"[REDACTED]"}`,
		},
		{
			name: "masks password in deeply nested objects",
			body: `{"data":{"inner":{"password":"deep","name":"ok"}}}`,
			want: `{"data":{"inner":{"name":"ok","password":"[REDACTED]"}}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeAuditDetail([]byte(tc.body))
			if got != tc.want {
				t.Fatalf("sanitizeAuditDetail(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

func TestHTTPAuditDetailCaptured(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	// create admin
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// Disable a user - this should capture detail
	if _, err := c.CreateAccount(context.TODO(), "user1", "password1", "u@b.com", "555", "User", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	// The disable call has a simple body: {"username":"user1"}
	body := `{"username":"user1"}`
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("account/disable"), bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+resp.Token)
	req.Header.Set("Content-Type", "application/json")
	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	if err := httpResp.Body.Close(); err != nil {
		t.Errorf("resp.Body.Close: %v", err)
	}

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	found := false
	for _, e := range page.Entries {
		if e.Action == "disable account" && e.Detail != "" {
			found = true
			if !strings.Contains(e.Detail, "user1") {
				t.Fatalf("expected detail to contain 'user1', got %q", e.Detail)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected to find audit entry with detail for disable account")
	}
}

func TestHTTPAuditDetailRedactsPassword(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	// create admin
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, e := range page.Entries {
		if e.Action == "create account" {
			if strings.Contains(e.Detail, "password1") {
				t.Fatalf("expected detail to NOT contain password, got %q", e.Detail)
			}
			if !strings.Contains(e.Detail, "admin") {
				t.Fatalf("expected detail to contain username 'admin', got %q", e.Detail)
			}
			return
		}
	}
	t.Fatal("expected to find create account audit entry")
}

func TestHTTPAuditDetailValidJSON(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "admin", "secret12", "admin@test.com", "555-1234", "Admin User", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, e := range page.Entries {
		if e.Action == "create account" && e.Detail != "" {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(e.Detail), &parsed); err != nil {
				t.Fatalf("detail is not valid JSON: %q, err: %v", e.Detail, err)
			}

			if parsed["username"] != "admin" {
				t.Fatalf("expected username 'admin', got %v", parsed["username"])
			}
			if parsed["email"] != "admin@test.com" {
				t.Fatalf("expected email 'admin@test.com', got %v", parsed["email"])
			}
			if parsed["real_name"] != "Admin User" {
				t.Fatalf("expected real_name 'Admin User', got %v", parsed["real_name"])
			}
			// The key survives; its value must not. The sanitizer masks
			// credentials rather than deleting the key, so the log still
			// records that a password was submitted — itself the auditable
			// fact — without recording what it was.
			if pw, exists := parsed["password"]; !exists {
				t.Error("detail should keep the password key as a redaction marker")
			} else if pw != auditRedacted {
				t.Errorf("password = %v, want %q", pw, auditRedacted)
			}
			return
		}
	}
	t.Fatal("expected to find create account audit entry with detail")
}

func TestHTTPAuditDetailAuthenticateRedactsPassword(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "admin", "mypass12", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	if _, err := c.Authenticate(context.TODO(), "admin", "mypass12"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, e := range page.Entries {
		if e.Action == "authenticate" && e.Detail != "" {
			if strings.Contains(e.Detail, "mypass12") {
				t.Fatalf("authenticate detail must not contain password, got %q", e.Detail)
			}
			if !strings.Contains(e.Detail, "admin") {
				t.Fatalf("authenticate detail should contain username, got %q", e.Detail)
			}
			var parsed map[string]any
			if err := json.Unmarshal([]byte(e.Detail), &parsed); err != nil {
				t.Fatalf("detail is not valid JSON: %q", e.Detail)
			}
			// The key survives; its value must not. The sanitizer masks
			// credentials rather than deleting the key, so the log still
			// records that a password was submitted — itself the auditable
			// fact — without recording what it was.
			if pw, exists := parsed["password"]; !exists {
				t.Error("detail should keep the password key as a redaction marker")
			} else if pw != auditRedacted {
				t.Errorf("password = %v, want %q", pw, auditRedacted)
			}
			return
		}
	}
	t.Fatal("expected to find authenticate audit entry with detail")
}

func TestHTTPAuditLogIncludesDNSWriteRoutes(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	// create admin and authenticate
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// DNS write endpoints with their expected audit action strings
	dnsWriteRoutes := []struct {
		path   string
		action string
		body   string
	}{
		{"/dns/records/add", "add dns record", `{"name":"test.example.com","type":"A","value":"1.2.3.4"}`},
		{"/dns/records/remove", "remove dns record", `{"name":"test.example.com","type":"A","value":"1.2.3.4"}`},
		{"/dns/tld", "set dns tld", `{"tld":"example.com"}`},
		{"/dns/setup", "setup dns", `{}`},
	}

	for _, rt := range dnsWriteRoutes {
		req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route(rt.path[1:]), bytes.NewBufferString(rt.body))
		if err != nil {
			t.Fatalf("NewRequest POST %s: %v", rt.path, err)
		}
		req.Header.Set("Authorization", "Bearer "+resp.Token)
		req.Header.Set("Content-Type", "application/json")
		httpResp, err := c.HTTP.Do(req)
		if err == nil {
			_ = httpResp.Body.Close()
		}
	}

	// Verify each DNS write route appears in the audit log with the correct action
	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, rt := range dnsWriteRoutes {
		found := false
		for _, e := range page.Entries {
			if e.Path == rt.path && e.Action == rt.action {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected audit entry with path=%q action=%q", rt.path, rt.action)
		}
	}
}

// The allowlist read is excluded from the audit log but its two writes are not:
// exempting a name from every blocklist is exactly the kind of change an
// operator should be able to account for afterwards. Like the blocklist routes
// they mirror, these carry no named action in account.RouteActions — the path is
// what identifies them.
func TestHTTPAuditLogIncludesDnsblAllowlistWrites(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	writes := []struct {
		path string
		body string
	}{
		{"/dns/dnsbl/allowlist/add", `{"name":"cdn.example.com","reason":"false positive"}`},
		{"/dns/dnsbl/allowlist/remove", `{"name":"cdn.example.com"}`},
	}

	// The handlers error without a rolodex client; the audit middleware runs
	// either way, which is the point.
	for _, w := range writes {
		req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route(w.path[1:]), bytes.NewBufferString(w.body))
		if err != nil {
			t.Fatalf("NewRequest POST %s: %v", w.path, err)
		}
		req.Header.Set("Authorization", "Bearer "+resp.Token)
		req.Header.Set("Content-Type", "application/json")
		httpResp, err := c.HTTP.Do(req)
		if err == nil {
			_ = httpResp.Body.Close()
		}
	}

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, w := range writes {
		found := false
		for _, e := range page.Entries {
			if e.Path == w.path {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected an audit entry for %q", w.path)
		}
	}
}

func TestHTTPAuditDetailNeverContainsPassword(t *testing.T) {
	c, auditMgr := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "admin", "supersecret", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "admin", "supersecret")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	c.Token = resp.Token

	// Update account with password change
	newpw := "newpassword"
	if _, err := c.UpdateAccount(context.TODO(), "admin", account.UpdateFields{Password: &newpw}); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}

	page, err := auditMgr.List(account.AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, e := range page.Entries {
		if e.Detail == "" {
			continue
		}
		if strings.Contains(e.Detail, "supersecret") {
			t.Fatalf("entry %q detail contains password 'supersecret': %q", e.Action, e.Detail)
		}
		if strings.Contains(e.Detail, "newpassword") {
			t.Fatalf("entry %q detail contains password 'newpassword': %q", e.Action, e.Detail)
		}
		// The key itself may appear — the sanitizer masks values rather than
		// deleting keys — but only ever carrying the redaction marker. The
		// detail is compact JSON, so the marker is adjacent to the key.
		if strings.Contains(e.Detail, `"password"`) &&
			!strings.Contains(e.Detail, `"password":"`+auditRedacted+`"`) {
			t.Errorf("entry %q detail has an unredacted password field: %q", e.Action, e.Detail)
		}
	}
}

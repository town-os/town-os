package account

import (
	"path/filepath"
	"testing"
)

// --- Interface conformance ---

func TestSQLiteManagerImplementsManager(t *testing.T) {
	var _ Manager = (*SQLiteManager)(nil)
}

func TestMockManagerImplementsManager(t *testing.T) {
	var _ Manager = (*MockManager)(nil)
}

// --- Helpers ---

func initTestDB(t *testing.T) *SQLiteManager {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	return mgr
}

// --- Create tests ---

func TestCreateAccount(t *testing.T) {
	mgr := initTestDB(t)

	acct, err := mgr.Create("alice", "password123", "alice@example.com", "555-1234", "Alice Smith", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if acct.Username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", acct.Username)
	}
	if acct.Email != "alice@example.com" {
		t.Fatalf("expected email %q, got %q", "alice@example.com", acct.Email)
	}
	if acct.Phone != "555-1234" {
		t.Fatalf("expected phone %q, got %q", "555-1234", acct.Phone)
	}
	if acct.RealName != "Alice Smith" {
		t.Fatalf("expected real_name %q, got %q", "Alice Smith", acct.RealName)
	}
	if acct.Admin {
		t.Fatal("expected admin to be false")
	}
	if acct.CreatedAt.IsZero() {
		t.Fatal("expected non-zero created_at")
	}
}

func TestCreateAccountAdmin(t *testing.T) {
	mgr := initTestDB(t)

	acct, err := mgr.Create("admin", "password123", "admin@example.com", "555-0000", "Admin User", true)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if !acct.Admin {
		t.Fatal("expected admin to be true")
	}
}

func TestCreateAccountDuplicate(t *testing.T) {
	mgr := initTestDB(t)

	if _, err := mgr.Create("alice", "pass1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err := mgr.Create("alice", "pass2", "c@d.com", "666", "Alice2", false)
	if err != ErrDuplicateUsername {
		t.Fatalf("expected ErrDuplicateUsername, got %v", err)
	}
}

func TestCreateAccountMissingContactInfo(t *testing.T) {
	mgr := initTestDB(t)

	tests := []struct {
		name     string
		email    string
		phone    string
		realName string
	}{
		{"empty email", "", "555", "Alice"},
		{"empty phone", "a@b.com", "", "Alice"},
		{"empty realName", "a@b.com", "555", ""},
		{"whitespace email", "  ", "555", "Alice"},
	}

	for _, tt := range tests {
		_, err := mgr.Create("user-"+tt.name, "pass", tt.email, tt.phone, tt.realName, false)
		if err != ErrMissingContactInfo {
			t.Fatalf("%s: expected ErrMissingContactInfo, got %v", tt.name, err)
		}
	}
}

func TestCreateAccountInvalidEmail(t *testing.T) {
	mgr := initTestDB(t)

	tests := []struct {
		name  string
		email string
	}{
		{"no at sign", "invalid"},
		{"no domain", "user@"},
		{"no local part", "@domain.com"},
		{"no tld", "user@domain"},
		{"spaces in email", "user @domain.com"},
	}

	for _, tt := range tests {
		_, err := mgr.Create("user-"+tt.name, "pass", tt.email, "555-1234", "Test", false)
		if err != ErrInvalidEmail {
			t.Fatalf("%s: expected ErrInvalidEmail, got %v", tt.name, err)
		}
	}
}

func TestCreateAccountInvalidPhone(t *testing.T) {
	mgr := initTestDB(t)

	tests := []struct {
		name  string
		phone string
	}{
		{"letters", "abc"},
		{"special chars", "555#1234"},
		{"at sign", "555@1234"},
	}

	for _, tt := range tests {
		_, err := mgr.Create("user-"+tt.name, "pass", "a@b.com", tt.phone, "Test", false)
		if err != ErrInvalidPhone {
			t.Fatalf("%s: expected ErrInvalidPhone, got %v", tt.name, err)
		}
	}
}

func TestCreateAccountValidPhoneFormats(t *testing.T) {
	mgr := initTestDB(t)

	tests := []struct {
		name  string
		phone string
	}{
		{"digits only", "5551234"},
		{"with dashes", "555-123-4567"},
		{"with parens", "(555) 123-4567"},
		{"international", "+1-555-123-4567"},
		{"with dots", "555.123.4567"},
	}

	for _, tt := range tests {
		_, err := mgr.Create("user-"+tt.name, "pass", "a@b.com", tt.phone, "Test", false)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
	}
}

func TestUpdateAccountInvalidEmail(t *testing.T) {
	mgr := initTestDB(t)

	if _, err := mgr.Create("alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	badEmail := "not-an-email"
	_, err := mgr.Update("alice", UpdateFields{Email: &badEmail})
	if err != ErrInvalidEmail {
		t.Fatalf("expected ErrInvalidEmail, got %v", err)
	}
}

func TestUpdateAccountInvalidPhone(t *testing.T) {
	mgr := initTestDB(t)

	if _, err := mgr.Create("alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	badPhone := "abc"
	_, err := mgr.Update("alice", UpdateFields{Phone: &badPhone})
	if err != ErrInvalidPhone {
		t.Fatalf("expected ErrInvalidPhone, got %v", err)
	}
}

// --- Get tests ---

func TestGetAccount(t *testing.T) {
	mgr := initTestDB(t)

	if _, err := mgr.Create("alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	acct, err := mgr.Get("alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if acct.Username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", acct.Username)
	}
	if acct.Email != "a@b.com" {
		t.Fatalf("expected email %q, got %q", "a@b.com", acct.Email)
	}
}

func TestGetAccountNotFound(t *testing.T) {
	mgr := initTestDB(t)

	_, err := mgr.Get("nonexistent")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// --- Update tests ---

func TestUpdateAccount(t *testing.T) {
	mgr := initTestDB(t)

	if _, err := mgr.Create("alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	newEmail := "new@example.com"
	admin := true
	acct, err := mgr.Update("alice", UpdateFields{
		Email: &newEmail,
		Admin: &admin,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if acct.Email != "new@example.com" {
		t.Fatalf("expected email %q, got %q", "new@example.com", acct.Email)
	}
	if !acct.Admin {
		t.Fatal("expected admin to be true")
	}
	if acct.Phone != "555" {
		t.Fatalf("expected phone unchanged %q, got %q", "555", acct.Phone)
	}
}

func TestUpdateAccountPassword(t *testing.T) {
	mgr := initTestDB(t)

	if _, err := mgr.Create("alice", "oldpass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	newPass := "newpass"
	if _, err := mgr.Update("alice", UpdateFields{Password: &newPass}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// old password should fail
	if _, err := mgr.Authenticate("alice", "oldpass"); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials with old password, got %v", err)
	}

	// new password should work
	if _, err := mgr.Authenticate("alice", "newpass"); err != nil {
		t.Fatalf("Authenticate with new password: %v", err)
	}
}

func TestUpdateAccountNotFound(t *testing.T) {
	mgr := initTestDB(t)

	newEmail := "new@example.com"
	_, err := mgr.Update("nonexistent", UpdateFields{Email: &newEmail})
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateAccountNoFields(t *testing.T) {
	mgr := initTestDB(t)

	if _, err := mgr.Create("alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	acct, err := mgr.Update("alice", UpdateFields{})
	if err != nil {
		t.Fatalf("Update with no fields: %v", err)
	}

	if acct.Username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", acct.Username)
	}
}

// --- Disable tests ---

func TestDisableAccount(t *testing.T) {
	mgr := initTestDB(t)

	if _, err := mgr.Create("alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := mgr.Disable("alice"); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	acct, err := mgr.Get("alice")
	if err != nil {
		t.Fatalf("Get after disable: %v", err)
	}
	if !acct.Disabled {
		t.Fatal("expected account to be disabled")
	}
}

func TestDisableAccountNotFound(t *testing.T) {
	mgr := initTestDB(t)

	err := mgr.Disable("nonexistent")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDisabledAccountCannotAuthenticate(t *testing.T) {
	mgr := initTestDB(t)

	if _, err := mgr.Create("alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := mgr.Disable("alice"); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	_, err := mgr.Authenticate("alice", "pass")
	if err != ErrAccountDisabled {
		t.Fatalf("expected ErrAccountDisabled, got %v", err)
	}
}

// --- List tests ---

func TestListAccounts(t *testing.T) {
	mgr := initTestDB(t)

	if _, err := mgr.Create("alice", "pass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("Create alice: %v", err)
	}
	if _, err := mgr.Create("bob", "pass", "b@b.com", "666", "Bob", true); err != nil {
		t.Fatalf("Create bob: %v", err)
	}

	accounts, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accounts))
	}

	// sorted by username
	if accounts[0].Username != "alice" {
		t.Fatalf("expected first account alice, got %q", accounts[0].Username)
	}
	if accounts[1].Username != "bob" {
		t.Fatalf("expected second account bob, got %q", accounts[1].Username)
	}
}

func TestListAccountsEmpty(t *testing.T) {
	mgr := initTestDB(t)

	accounts, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(accounts) != 0 {
		t.Fatalf("expected 0 accounts, got %d", len(accounts))
	}
}

// --- Authenticate tests ---

func TestAuthenticateSuccess(t *testing.T) {
	mgr := initTestDB(t)

	if _, err := mgr.Create("alice", "correctpass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	acct, err := mgr.Authenticate("alice", "correctpass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if acct.Username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", acct.Username)
	}
	if acct.PasswordHash != "" {
		t.Fatal("expected password hash to be cleared on authenticate response")
	}
}

func TestAuthenticateWrongPassword(t *testing.T) {
	mgr := initTestDB(t)

	if _, err := mgr.Create("alice", "correctpass", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := mgr.Authenticate("alice", "wrongpass")
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestAuthenticateNonexistentUser(t *testing.T) {
	mgr := initTestDB(t)

	_, err := mgr.Authenticate("nonexistent", "anypass")
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials for nonexistent user, got %v", err)
	}
}

// --- Password hashing tests ---

func TestPasswordHashVerify(t *testing.T) {
	hash, err := hashPassword("testpassword")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}

	if !verifyPassword(hash, "testpassword") {
		t.Fatal("verifyPassword should return true for correct password")
	}

	if verifyPassword(hash, "wrongpassword") {
		t.Fatal("verifyPassword should return false for incorrect password")
	}
}

func TestPasswordHashUnique(t *testing.T) {
	hash1, err := hashPassword("same")
	if err != nil {
		t.Fatalf("hashPassword 1: %v", err)
	}

	hash2, err := hashPassword("same")
	if err != nil {
		t.Fatalf("hashPassword 2: %v", err)
	}

	if hash1 == hash2 {
		t.Fatal("two hashes of the same password should differ (different salts)")
	}
}

// --- Full lifecycle ---

func TestAccountLifecycle(t *testing.T) {
	mgr := initTestDB(t)

	// Create
	acct, err := mgr.Create("alice", "pass123", "alice@test.com", "555-1234", "Alice Smith", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if acct.Username != "alice" {
		t.Fatalf("expected username alice, got %q", acct.Username)
	}

	// Authenticate
	if _, err := mgr.Authenticate("alice", "pass123"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// Update
	newEmail := "newalice@test.com"
	acct, err = mgr.Update("alice", UpdateFields{Email: &newEmail})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if acct.Email != "newalice@test.com" {
		t.Fatalf("expected updated email, got %q", acct.Email)
	}

	// List
	accounts, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}

	// Disable
	if err := mgr.Disable("alice"); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	// Verify disabled
	acct, err = mgr.Get("alice")
	if err != nil {
		t.Fatalf("Get after disable: %v", err)
	}
	if !acct.Disabled {
		t.Fatal("expected account to be disabled")
	}

	// Verify cannot authenticate
	_, err = mgr.Authenticate("alice", "pass123")
	if err != ErrAccountDisabled {
		t.Fatalf("expected ErrAccountDisabled, got %v", err)
	}
}

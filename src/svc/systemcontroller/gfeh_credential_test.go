// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/storage"
)

func credentialTestManagers(t *testing.T) (account.Manager, account.SettingsManager) {
	t.Helper()
	return account.InitMockManager(), &mockSettingsManager{values: map[string]string{}}
}

// TestGfehServiceAccountIsCreatedOnce, and re-reads the same credential
// afterwards. A password that changed per boot would render a different config
// every time and restart every partition on the box on every reconcile.
func TestGfehServiceAccountIsCreatedOnce(t *testing.T) {
	acctMgr, settingsMgr := credentialTestManagers(t)

	user, pw, err := ensureGfehServiceAccount(acctMgr, settingsMgr)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if user != GfehServiceAccount {
		t.Errorf("username = %q, want %q", user, GfehServiceAccount)
	}
	if pw == "" {
		t.Fatal("no password was generated")
	}

	againUser, againPw, err := ensureGfehServiceAccount(acctMgr, settingsMgr)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if againUser != user || againPw != pw {
		t.Error("the credential changed between boots; every partition would restart on every reconcile")
	}
}

// TestGfehServiceAccountIsAnAdministrator. Provisioning a partition is
// admin-only, because it is also creating the root of a permission tree — a
// non-admin service account would fail every provision with a 403.
func TestGfehServiceAccountIsAnAdministrator(t *testing.T) {
	acctMgr, settingsMgr := credentialTestManagers(t)

	if _, _, err := ensureGfehServiceAccount(acctMgr, settingsMgr); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	acct, err := acctMgr.Get(GfehServiceAccount)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !acct.Admin {
		t.Error("the service account is not an administrator; it could not provision a partition")
	}
}

// TestGfehServiceAccountPasswordIsNotGuessable. It is an administrator, so the
// only thing standing between an attacker and admin is that nobody can guess
// this value.
func TestGfehServiceAccountPasswordIsNotGuessable(t *testing.T) {
	_, settingsMgr := credentialTestManagers(t)

	first, err := gfehServicePassword(settingsMgr)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(first) != 64 {
		t.Errorf("password is %d characters, want 64 (256 bits hex-encoded)", len(first))
	}

	// A second, independent manager must produce a different value.
	other := &mockSettingsManager{values: map[string]string{}}
	second, err := gfehServicePassword(other)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if first == second {
		t.Error("two boxes generated the same service password")
	}
}

// TestGfehServiceAccountIsReEnabled. Disabling it from the users page silently
// breaks provisioning on every partition, and nothing in the UI would explain
// why — so the reconcile puts it back rather than failing quietly.
func TestGfehServiceAccountIsReEnabled(t *testing.T) {
	acctMgr, settingsMgr := credentialTestManagers(t)

	if _, _, err := ensureGfehServiceAccount(acctMgr, settingsMgr); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := acctMgr.Disable(GfehServiceAccount); err != nil {
		t.Fatalf("disable: %v", err)
	}

	if _, _, err := ensureGfehServiceAccount(acctMgr, settingsMgr); err != nil {
		t.Fatalf("re-ensure: %v", err)
	}

	acct, err := acctMgr.Get(GfehServiceAccount)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if acct.Disabled {
		t.Error("the service account stayed disabled; every partition's provisioning would fail")
	}
}

// TestTheDaemonCredentialIsNotAUserCredential.
//
// These are two unrelated systems that happen to be rendered into one file, and
// conflating them produces the wrong answer about lifetime and blast radius:
// the daemon credential is a Town OS account with a bcrypt hash in the accounts
// table, and an SMB credential is an unsalted NT hash that Town OS's account
// system never authenticates anything against.
func TestTheDaemonCredentialIsNotAUserCredential(t *testing.T) {
	acctMgr, settingsMgr := credentialTestManagers(t)

	_, daemonPw, err := ensureGfehServiceAccount(acctMgr, settingsMgr)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	// An ordinary account enrols an SMB credential.
	if _, err := acctMgr.Create("alice", "hunter2hunter2", "a@example.com", "5551234", "Alice", false); err != nil {
		t.Fatalf("create: %v", err)
	}
	smbPw := "smbpassword"
	if _, err := acctMgr.Update("alice", account.UpdateFields{SMBPassword: &smbPw}); err != nil {
		t.Fatalf("enrol: %v", err)
	}
	alice, err := acctMgr.Get("alice")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if alice.SMBNTHash == daemonPw {
		t.Fatal("a user's SMB hash is the daemon's password")
	}

	// The service account has no SMB credential: it is not a person and never
	// mounts a share.
	svc, err := acctMgr.Get(GfehServiceAccount)
	if err != nil {
		t.Fatalf("get service account: %v", err)
	}
	if svc.SMBEnrolled {
		t.Error("the service account was enrolled for SMB; it has no business mounting a share")
	}
}

// TestServiceAccountDoesNotSatisfySetup is a regression test for a first-boot
// lockout, and it is worth stating the failure plainly because the symptom
// looks nothing like the cause.
//
// The service account is an enabled administrator and it is created during
// boot. Both "is this box set up" checks -- the setup flag in /status/ping and
// the bootstrap branch of POST /account/create -- scanned for any enabled admin
// account. So a brand-new system came up already claiming to be set up: the UI
// showed a login form for an account nobody had created, and account creation
// answered 401 asking for a token that could only come from logging in to that
// same nonexistent account. There is no way out of that from the UI, and no
// message anywhere says why.
func TestServiceAccountDoesNotSatisfySetup(t *testing.T) {
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("db.Close: %v", closeErr)
		}
	})

	acctMgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	settingsMgr, err := account.InitSettingsManager(db)
	if err != nil {
		t.Fatalf("InitSettingsManager: %v", err)
	}
	sessMgr, err := account.InitSessionManager(db, acctMgr, []byte("test-signing-key-for-sessions-32"))
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	// Exactly what boot does, on a box with no human accounts yet.
	if _, _, err := ensureGfehServiceAccount(acctMgr, settingsMgr); err != nil {
		t.Fatalf("ensureGfehServiceAccount: %v", err)
	}

	ts := InitTestServer(ServerConfig{
		Storage:     storage.InitBtrFSMock(),
		AccountMgr:  acctMgr,
		SessionMgr:  sessMgr,
		SettingsMgr: settingsMgr,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	ping, err := c.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if !ping.NeedsSetup {
		t.Error("a box with only the service account reported itself as set up")
	}

	// And the operator can still make the first account, with no token --
	// which is the half that actually bricks the box when it regresses.
	if _, err := c.CreateAccount(ctx, "admin", "password1", "a@b.com", "5551234", "Admin", true); err != nil {
		t.Fatalf("bootstrap account creation was refused: %v", err)
	}

	// Once a person exists, setup is genuinely done and the door closes again.
	ping, err = c.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if ping.NeedsSetup {
		t.Error("needs_setup stayed true after a human admin was created")
	}
	if _, err := c.CreateAccount(ctx, "second", "password1", "b@b.com", "5551234", "Second", true); err == nil {
		t.Error("bootstrap creation still open after a human admin existed")
	}
}

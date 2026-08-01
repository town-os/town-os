// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"gitea.com/town-os/town-os/src/account"
)

// The account gfehd authenticates to Town OS as.
//
// This is the credential in gfehd.yaml's `town_os:` section, and it exists for
// exactly the two things TOWNOS_CONTRACT.md scopes gfeh's use of Town OS to:
// provisioning the subvolume its partition lives in, and turning a Town OS
// account into a principal in its own ACL forest.
//
// It has nothing to do with the SMB, S3 or Drive credentials rendered into the
// same file. Those authenticate *end users* to gfeh's views and Town OS's
// account system never sees them. This one authenticates *the daemon* to the
// control plane. The two get confused easily and reasoning about one as if it
// were the other produces the wrong answer about lifetime, rotation and blast
// radius — so they are kept in separate files with separate names.

const (
	// GfehServiceAccount is the username gfehd authenticates as.
	//
	// An administrator, because provisioning a partition is admin-only: it is
	// also creating the root of a permission tree, which the contract reserves
	// for administrators.
	GfehServiceAccount = "gfeh"

	// SettingGfehServicePassword holds the generated password.
	//
	// A password rather than a session token, and the reason is Town OS's own
	// session model rather than a preference: the JWT signing key is 32 random
	// bytes regenerated on every start and InitSessionManager deletes every
	// session at startup, so a stored token is invalid the moment the
	// controller restarts. A daemon holding one would fail its next
	// authentication with no way to recover on its own. A password
	// re-authenticates on every start with no liveness coupling in either
	// direction.
	SettingGfehServicePassword = "gfeh_service_password" //nolint:gosec // G101 -- a settings key name, not a credential
)

// ensureGfehServiceAccount returns the credential gfehd should present,
// creating the account and its password on first boot.
//
// Idempotent: on every boot after the first it reads back what is already
// stored, so the rendered config is byte-identical and reconcile does not
// restart every partition on the box.
func ensureGfehServiceAccount(acctMgr account.Manager, settingsMgr account.SettingsManager) (username, password string, err error) {
	if acctMgr == nil || settingsMgr == nil {
		return "", "", errors.New("gfeh service account: no account or settings manager")
	}

	password, err = gfehServicePassword(settingsMgr)
	if err != nil {
		return "", "", err
	}

	existing, getErr := acctMgr.Get(GfehServiceAccount)
	if getErr == nil && existing != nil {
		// The account is there. Re-assert the password rather than trusting it:
		// a stored password and an account whose hash no longer matches it is a
		// daemon that cannot authenticate and cannot say why, and the only way
		// out would be manual. Update is idempotent when the password already
		// hashes to the stored value.
		if _, updErr := acctMgr.Update(GfehServiceAccount, account.UpdateFields{Password: &password}); updErr != nil {
			return "", "", fmt.Errorf("reset gfeh service password: %w", updErr)
		}
		if existing.Disabled {
			// Disabling it silently breaks every partition's provisioning, and
			// nothing else in the UI explains why. Re-enable rather than fail:
			// this account is infrastructure, not a person.
			if enErr := acctMgr.Enable(GfehServiceAccount); enErr != nil {
				return "", "", fmt.Errorf("re-enable gfeh service account: %w", enErr)
			}
		}
		return GfehServiceAccount, password, nil
	}

	if _, createErr := acctMgr.Create(
		GfehServiceAccount, password,
		// Contact details are required by the account validator and meaningless
		// here, so they say what the account is rather than pretending to be a
		// person's. The address still has to satisfy the validator's
		// user@domain.tld shape -- "gfeh@localhost" does not, and failing that
		// check is a partition that never provisions.
		GfehServiceAccount+"@service.town-os.local", "0000000000",
		"Object Storage Service",
		true,
	); createErr != nil {
		return "", "", fmt.Errorf("create gfeh service account: %w", createErr)
	}

	return GfehServiceAccount, password, nil
}

// isServiceAccount reports whether a username belongs to a Town OS daemon
// rather than to a person.
//
// Two places ask whether the box has an administrator: the setup flag in
// /status/ping, and the bootstrap branch of POST /account/create that lets the
// very first admin be created without a token. Both mean "has a *person* been
// set up", and both answered it by scanning for any enabled admin account.
//
// The gfeh service account is an enabled admin, and it is created during boot —
// before any human has touched the box. Counting it makes a brand-new system
// report that it is already set up, so the UI shows a login form instead of the
// setup form, and POST /account/create then demands a token that can only be
// obtained by logging in to an account that does not exist. The box is
// permanently unusable and nothing on the screen explains why. Hence the
// exclusion, and hence it lives next to the account it excludes.
//
// Deliberately narrow: it does not hide the account from the users list or the
// account totals, which are factual inventories and where an operator should be
// able to see that an administrative account exists on their system.
func isServiceAccount(username string) bool {
	return username == GfehServiceAccount
}

// hasHumanAdmin reports whether any enabled, non-service admin account exists.
// Shared by the two callers above so they cannot drift apart — one of them
// gating setup and the other gating bootstrap account creation, on different
// answers, is its own lockout.
func hasHumanAdmin(accounts []account.Account) bool {
	for _, a := range accounts {
		if !a.Disabled && a.Admin && !isServiceAccount(a.Username) {
			return true
		}
	}
	return false
}

// gfehServicePassword reads the stored password, generating one on first use.
func gfehServicePassword(settingsMgr account.SettingsManager) (string, error) {
	stored, err := settingsMgr.Get(SettingGfehServicePassword)
	if err == nil && strings.TrimSpace(stored) != "" {
		return stored, nil
	}

	// 256 bits from crypto/rand, hex-encoded. Nobody types this and nothing
	// displays it: it exists so the account cannot be logged into by guessing,
	// which matters because it is an administrator.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate gfeh service password: %w", err)
	}
	password := hex.EncodeToString(raw)

	if err := settingsMgr.Set(SettingGfehServicePassword, password); err != nil {
		return "", fmt.Errorf("store gfeh service password: %w", err)
	}
	return password, nil
}

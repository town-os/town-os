// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package account

import (
	"database/sql"
	"fmt"
)

// LegacyGfehServiceAccount is the username older Town OS releases created for
// the object-storage daemon to authenticate to the control plane with.
//
// The daemon no longer authenticates to anything: Town OS provisions each
// partition's subvolume and quota itself before the daemon starts, and creates
// principals over the partition's admin socket, so the credential paid for
// nothing. What it cost was an *enabled administrator account nobody created*
// sitting in the users list of every box, with a random password and enough
// privilege to uninstall everything — plus the special cases it forced
// everywhere else, since every "does this box have an admin?" question then had
// to mean "a human admin".
const LegacyGfehServiceAccount = "gfeh"

// LegacyGfehPasswordSetting is where that account's generated password was
// stored.
const LegacyGfehPasswordSetting = "gfeh_service_password" //nolint:gosec // G101 -- a settings key name, not a credential

// PurgeLegacyServiceAccounts deletes the daemon account and its stored password
// from a database written by an older release.
//
// Boxes that ran a version which created it would otherwise keep it forever:
// nothing re-creates it now, but nothing removes it either, and it would go on
// showing up in the users list and the admin count as an account the operator
// never made and cannot explain. Returns whether anything was actually removed,
// so the caller can say so once rather than logging on every boot.
//
// Deliberately SQL rather than a Manager method: Manager has no Delete, and an
// account-deletion capability is not something to introduce as a side effect of
// a cleanup — there is no route for it and no reason for one.
func PurgeLegacyServiceAccounts(db *sql.DB) (removed bool, err error) {
	if db == nil {
		return false, nil
	}

	ctx, cancel := dbCtx()
	defer cancel()

	res, err := db.ExecContext(ctx, "DELETE FROM accounts WHERE username = ?", LegacyGfehServiceAccount)
	if err != nil {
		return false, fmt.Errorf("delete legacy service account: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}

	// The password goes with it. Left behind it is a live administrator
	// credential for an account that no longer exists -- worthless to anything
	// legitimate, and exactly the kind of thing that gets reused if the account
	// name is ever recreated by hand.
	if _, err := db.ExecContext(ctx, "DELETE FROM settings WHERE key = ?", LegacyGfehPasswordSetting); err != nil {
		return n > 0, fmt.Errorf("delete legacy service password: %w", err)
	}

	// Its sessions go with it without being named here: sessions.username is
	// declared REFERENCES accounts(username) ON DELETE CASCADE and OpenDB turns
	// foreign keys on, so the delete above already took them.
	return n > 0, nil
}

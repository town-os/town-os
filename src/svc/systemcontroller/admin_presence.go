// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import "gitea.com/town-os/town-os/src/account"

// hasEnabledAdmin reports whether the box has an administrator who can log in.
//
// Two places ask this and they must never disagree: the setup flag in
// /status/ping, which decides whether the UI shows the setup form or the login
// form, and the bootstrap branch of POST /account/create, which lets the very
// first admin be created without a token. A box where one says "set up" and the
// other says "not set up" is a box nobody can get into — the UI demands a login
// for an account that does not exist, and account creation demands a token only
// that login could produce. Sharing one predicate is what stops that.
//
// Every enabled admin counts. There is no longer any such thing as a service
// account here: the object-storage daemon used to hold one, which is precisely
// why this had to mean "a *human* admin" and why a box could otherwise come up
// claiming it was already set up before anybody had touched it.
func hasEnabledAdmin(accounts []account.Account) bool {
	for _, a := range accounts {
		if !a.Disabled && a.Admin {
			return true
		}
	}
	return false
}

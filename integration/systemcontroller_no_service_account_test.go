// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"testing"
	"time"
)

// A box that nobody has set up has no accounts on it, of any kind.
//
// A release once created one on its own: an enabled administrator named `gfeh`,
// with a generated password in the settings table, for the object-storage daemon
// to authenticate as. It appeared in the users list on boxes that had never
// asked for it, it counted toward the admin total, and it meant a freshly
// installed machine already had an administrator nobody could log in as.
//
// The account is gone, the migration that used to delete it is gone, and the
// unit guard in src/account fails on any of its identifiers reappearing in the
// source. What that guard cannot do is prove the running system agrees -- it
// reads text, not a database. This asks the real control plane, over HTTP,
// against a real SQLite account store: with no operator setup, the account list
// is empty and the box says so.
func TestIntegrationAFreshBoxHasNoAccounts(t *testing.T) {
	t.Parallel()

	c, mgr := initBootstrapTest(t)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// Straight from the store, so nothing about the API's filtering or
	// pagination can hide a row.
	accounts, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(accounts) != 0 {
		for _, a := range accounts {
			t.Errorf("a box nobody set up already holds the account %q (admin=%t): "+
				"Town OS creates no account for any daemon, and gfehd in "+
				"particular authenticates to nothing -- its partition is "+
				"provisioned before it starts and its principals are created "+
				"over its admin socket", a.Username, a.Admin)
		}
	}

	// And the box reports itself as unconfigured. This is the half an
	// auto-created administrator broke without touching the users list:
	// needs_setup is "no enabled admin exists", so one invented account turns
	// a machine awaiting setup into one that claims to be already set up --
	// and then refuses the unauthenticated create that is the only way in.
	ping, err := c.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if !ping.NeedsSetup {
		t.Error("a box with no accounts reports needs_setup=false: something " +
			"created an enabled administrator, which is the one thing that " +
			"locks an operator out of their own first login")
	}
}

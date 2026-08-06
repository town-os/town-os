// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"testing"
	"time"
)

// Object storage creates no account, and this is the end-to-end proof of it.
//
// The reconcile used to seat the box's earliest-created account as a principal
// in the home partition every time it ran. Nobody asked for that user: it
// appeared in the Users tab as though an administrator had added it, and it was
// created by the platform reaching into the account table on its own initiative
// -- the same instinct that produced the `gfeh` service account before it.
//
// The unit tests pin the two routes it could come back by (no CreatePrincipal
// call outside the admin handler, no account manager on the reconcile config).
// Neither can show what a real daemon actually holds, because both stop at the
// edge of this process. This one asks gfehd: the partition is brought up
// through the production path -- real systemd, real btrfs, real podman, real
// gfehd -- and then its forest is read over the admin socket it really binds.
//
// A partition that has just been created must serve nobody at all.
func TestIntegrationAFreshPartitionHasNoPrincipals(t *testing.T) {
	t.Parallel()

	client := startRealPartition(t)

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	principals, err := client.ListPrincipals(ctx)
	if err != nil {
		t.Fatalf("ListPrincipals: %v", err)
	}
	if len(principals) != 0 {
		t.Errorf("a freshly reconciled partition already holds %d principal(s): %+v\n"+
			"Nothing may create an object-storage user on its own. A partition's "+
			"forest is populated by an administrator over /gfeh/principals/add "+
			"and by nothing else.", len(principals), principals)
	}
}

// The same partition, reconciled again.
//
// The seating that was removed ran on every pass, not only the first, so a
// reintroduction would most likely show up here rather than above -- the box
// looks clean until the hourly reconcile, or a network change, quietly puts a
// user back. Reconciling a partition that already exists must leave its forest
// exactly as empty as it found it.
func TestIntegrationReconcilingAgainSeatsNobody(t *testing.T) {
	t.Parallel()

	client, reconcile := startRealPartitionReconcilable(t)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	reconcile(ctx)

	principals, err := client.ListPrincipals(ctx)
	if err != nil {
		t.Fatalf("ListPrincipals after a second reconcile: %v", err)
	}
	if len(principals) != 0 {
		t.Errorf("a second reconcile seated %d principal(s): %+v\n"+
			"The removed seating ran on every pass; this is the shape a "+
			"reintroduction would take.", len(principals), principals)
	}
}

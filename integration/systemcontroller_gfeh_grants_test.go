// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// The grants that govern object-storage users, against a REAL gfehd.
//
// Not a stand-in: the whole question here is what the daemon does with a grant,
// and a fake answers whatever it was written to answer. Ceilings, clamping, and
// the lifetime of a grant when its principal goes away are gfehd's behaviour,
// so a test that stubs them proves only that Town OS can talk to itself.
//
// The partition is brought up through the production path — ReconcileGfeh over
// real systemd, real btrfs, real podman — so the unit, the config, the
// subvolume ownership and the admin socket are all the ones a real box gets.

// startRealPartition brings up one gfehd for a uniquely-named network and
// returns its admin client. The network name stays inside: every call below
// addresses the partition through the socket, never by name.
//
// Every derived name carries a per-test suffix — the network, the unit key, the
// podman network — so two of these can run at once, and alongside a production
// partition, without touching each other. IRON RULE.
func startRealPartition(t *testing.T) gfeh.Client {
	t.Helper()
	client, _ := startRealPartitionReconcilable(t)
	return client
}

// startRealPartitionReconcilable is startRealPartition plus a handle on the
// production reconcile that built the partition, so a caller can run it again
// against the same live daemon.
//
// Separate from re-calling startRealPartition, which would build a second
// partition on a second network and prove nothing about what a repeated pass
// does to an existing one.
func startRealPartitionReconcilable(t *testing.T) (gfeh.Client, func(context.Context)) {
	t.Helper()

	image := os.Getenv("GFEH_IMAGE")
	if image == "" {
		t.Skip("GFEH_IMAGE is empty; object storage is disabled in this build")
	}

	suffix := strconv.FormatUint(rand.Uint64(), 36) //nolint:gosec // G404 -- a test-local name, not a secret
	network := "gfehgrants" + suffix

	nm := account.InitMockNetworkManager()
	if _, err := nm.Create(&account.Network{Name: network, TLD: network, Enabled: true}); err != nil {
		t.Fatalf("create network: %v", err)
	}

	reg := systemcontroller.NewGfehRegistry(systemcontroller.ReconcileGfehConfig{
		NetworkMgr:    nm,
		Storage:       storage.InitBtrFS("/town-os"),
		Systemd:       systemd.NewManager(),
		SettingsMgr:   nil, // no quota; the partition is a scratch one
		BtrfsBasePath: "/town-os",
		Image:         image,
		// The harness loads the image into this container, so a pull would
		// only reach for a registry copy that need not exist.
		PullNever:   true,
		KeyPrefix:   "test-" + suffix + "-",
		NetworkName: "town-os-ingress-test-" + suffix,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	systemcontroller.ReconcileGfehRegistry(ctx, reg)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		for _, m := range reg.Managers() {
			if err := m.Remove(cleanupCtx); err != nil {
				t.Logf("remove partition: %v", err)
			}
		}
	})

	client, ok := reg.Clients()[network]
	if !ok {
		t.Fatalf("no client for %s; the partition never started. managers: %v", network, reg.Managers())
	}

	// Readiness is the daemon opening its partition and binding its socket.
	// Everything below is a call on that socket, so a test that skipped this
	// would report a daemon problem as a grant problem.
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if _, err := client.Health(ctx); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("gfehd never became ready: %v", err)
		}
		time.Sleep(time.Second)
	}
	return client, func(again context.Context) { systemcontroller.ReconcileGfehRegistry(again, reg) }
}

// An administrator projects the superuser ceiling, and a grant carrying the
// sub-user right survives against it. `all` admits everything by definition, so
// this is the one clamping outcome that does not depend on gfeh's table — which
// makes it the one worth asserting flatly.
func TestIntegrationCreateSubuserGrantSurvivesTheSuperuserCeiling(t *testing.T) {
	t.Parallel()
	client := startRealPartition(t)
	ctx := t.Context()

	if _, err := client.CreatePrincipal(ctx, gfeh.Principal{
		Name:    "root",
		Ceiling: gfeh.CeilingForAccount(true),
	}); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}

	created, err := client.CreateGrant(ctx, gfeh.Grant{
		Principal:   "root",
		Path:        "/",
		Perm:        []string{gfeh.PermCreateSubuser},
		Inheritable: true,
	})
	if err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}
	if !slices.Contains(created.Perm, gfeh.PermCreateSubuser) {
		t.Errorf("the daemon narrowed %q away from a superuser ceiling: %v",
			gfeh.PermCreateSubuser, created.Perm)
	}

	grants, err := client.ListGrants(ctx, "root")
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 1 || !slices.Contains(grants[0].Perm, gfeh.PermCreateSubuser) {
		t.Errorf("stored grants = %+v, want one carrying %q", grants, gfeh.PermCreateSubuser)
	}
}

// A projected ordinary account arrives with a ceiling and NOTHING else. This is
// the fact behind "there is no grant to create object-storage users": nobody
// can mint a sub-user until an administrator grants the right, and a future
// change that handed new principals a starter grant would fail here.
func TestIntegrationProjectedAccountStartsWithNoGrants(t *testing.T) {
	t.Parallel()
	client := startRealPartition(t)
	ctx := t.Context()

	created, err := client.CreatePrincipal(ctx, gfeh.Principal{
		Name:    "alice",
		Ceiling: gfeh.CeilingForAccount(false),
	})
	if err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}
	if slices.Contains(created.Ceiling, gfeh.PermAll) {
		t.Errorf("an ordinary account was given the superuser ceiling: %v", created.Ceiling)
	}

	grants, err := client.ListGrants(ctx, "alice")
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 0 {
		t.Errorf("a freshly projected account already holds %d grant(s): %+v", len(grants), grants)
	}
}

// Whatever the daemon decides about an ordinary ceiling, what it stored and
// what it reported must be the same thing.
//
// This is the property Town OS depends on and the one it can get wrong: the UI
// renders the perms that came back, so an administrator sees a narrowed grant
// as narrowed. A daemon that reported the request and stored something smaller
// would leave them believing they had handed out access nobody has. The
// assertion is deliberately not "create-subuser is absent" — that is gfeh's
// clamping table, which belongs to gfeh's own suite and would make this test a
// duplicate of it that fails whenever they change it.
func TestIntegrationGrantReportedMatchesGrantStored(t *testing.T) {
	t.Parallel()
	client := startRealPartition(t)
	ctx := t.Context()

	if _, err := client.CreatePrincipal(ctx, gfeh.Principal{
		Name:    "alice",
		Ceiling: gfeh.CeilingForAccount(false),
	}); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}

	asked := []string{gfeh.PermRead, gfeh.PermCreateSubuser}
	created, err := client.CreateGrant(ctx, gfeh.Grant{
		Principal: "alice", Path: "/", Perm: asked,
	})
	if err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	// Never MORE than was asked for. A clamp that widened a grant would be a
	// privilege escalation, and it is cheap to rule out here.
	for _, p := range created.Perm {
		if !slices.Contains(asked, p) {
			t.Errorf("the grant came back carrying %q, which was never requested: %v", p, created.Perm)
		}
	}

	grants, err := client.ListGrants(ctx, "alice")
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("stored grants = %+v, want exactly one", grants)
	}
	if !slices.Equal(grants[0].Perm, created.Perm) {
		t.Errorf("the daemon reported %v and stored %v; the UI would show a grant nobody holds",
			created.Perm, grants[0].Perm)
	}
}

// Revoking takes the right back for real. A revoke that reported success while
// leaving the row would leave sub-user creation with somebody an administrator
// believes they have cut off.
func TestIntegrationRevokingACreateSubuserGrantRemovesIt(t *testing.T) {
	t.Parallel()
	client := startRealPartition(t)
	ctx := t.Context()

	if _, err := client.CreatePrincipal(ctx, gfeh.Principal{
		Name: "root", Ceiling: gfeh.CeilingForAccount(true),
	}); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}
	created, err := client.CreateGrant(ctx, gfeh.Grant{
		Principal: "root", Path: "/", Perm: []string{gfeh.PermCreateSubuser},
	})
	if err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	if err := client.RevokeGrant(ctx, created.ID); err != nil {
		t.Fatalf("RevokeGrant: %v", err)
	}
	grants, err := client.ListGrants(ctx, "root")
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 0 {
		t.Errorf("the grant survived revocation: %+v", grants)
	}
}

// Removing the principal takes its grants with it. Otherwise a re-projected
// account of the same name inherits whatever the previous one held — including
// a sub-user right nobody re-granted.
func TestIntegrationRemovingAPrincipalTakesItsGrants(t *testing.T) {
	t.Parallel()
	client := startRealPartition(t)
	ctx := t.Context()

	if _, err := client.CreatePrincipal(ctx, gfeh.Principal{
		Name: "root", Ceiling: gfeh.CeilingForAccount(true),
	}); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}
	if _, err := client.CreateGrant(ctx, gfeh.Grant{
		Principal: "root", Path: "/", Perm: []string{gfeh.PermCreateSubuser},
	}); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	if err := client.DeletePrincipal(ctx, "root"); err != nil {
		t.Fatalf("DeletePrincipal: %v", err)
	}

	grants, err := client.ListGrants(ctx, "root")
	if err == nil && len(grants) != 0 {
		t.Errorf("grants outlived their principal: %+v", grants)
	}

	principals, err := client.ListPrincipals(ctx)
	if err != nil {
		t.Fatalf("ListPrincipals: %v", err)
	}
	for _, p := range principals {
		if p.Name == "root" {
			t.Errorf("the principal survived deletion: %+v", principals)
		}
	}
}

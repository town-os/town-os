// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/gfeh"
)

// The readiness sweep, against clients that actually consult the context.
//
// gfeh.MockClient ignores it — the right call for the collector tests, which
// are about what a partition contributes — but it makes the mock unable to
// express the failure this file is about, where the answer depends entirely on
// what is left of the caller's deadline.

// ctxAwareGfehClient answers Health from the context first, the way a client
// dialing a Unix socket does.
type ctxAwareGfehClient struct {
	*gfeh.MockClient

	down bool
}

func (c *ctxAwareGfehClient) Health(ctx context.Context) (gfeh.Health, error) {
	if err := ctx.Err(); err != nil {
		return gfeh.Health{}, err
	}
	if c.down {
		return gfeh.Health{}, errors.New("connect: connection refused")
	}
	return c.MockClient.Health(ctx)
}

func liveGfehClient(partition string) *ctxAwareGfehClient {
	return &ctxAwareGfehClient{MockClient: gfeh.NewMockClient(partition, partition)}
}

func downGfehClient(partition string) *ctxAwareGfehClient {
	return &ctxAwareGfehClient{MockClient: gfeh.NewMockClient(partition, partition), down: true}
}

// TestGfehReadyNetworksSurvivesAnExhaustedDeadline is the bug.
//
// The sweep's caller has almost always just run the reconcile, and a partition
// whose daemon never answers sits in WaitForReady until the shared context runs
// out. Every probe afterwards then fails on the expired deadline rather than on
// anything about the daemon, so a box whose other partitions are perfectly
// healthy reports that none of them are answering — and the poller republishes
// DNS and the ingress as though object storage had gone away.
func TestGfehReadyNetworksSurvivesAnExhaustedDeadline(t *testing.T) {
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{
		"home":   downGfehClient("home"),
		"office": liveGfehClient("office"),
	}}

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("precondition: ctx.Err() = %v, want DeadlineExceeded", ctx.Err())
	}

	ready := GfehReadyNetworks(ctx, reg)
	if len(ready) != 1 || ready[0] != "office" {
		t.Fatalf("ready = %v, want [office]: a spent deadline made a live partition "+
			"indistinguishable from a dead one", ready)
	}
}

// A cancelled context is a shutdown rather than a spent budget, and there is
// nothing to report then. Distinguishing the two is the whole reason the probe
// does not simply ignore the context it was handed.
func TestGfehReadyNetworksReportsNothingOnCancel(t *testing.T) {
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{
		"home": liveGfehClient("home"),
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if ready := GfehReadyNetworks(ctx, reg); len(ready) != 0 {
		t.Fatalf("ready = %v, want none: a cancelled context is a shutdown", ready)
	}
}

// The ordinary path: a live context, and the answer is the set of partitions
// that answer, sorted.
func TestGfehReadyNetworksListsAnsweringPartitionsSorted(t *testing.T) {
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{
		"office": liveGfehClient("office"),
		"attic":  liveGfehClient("attic"),
		"home":   downGfehClient("home"),
		"nilled": nil,
	}}

	ready := GfehReadyNetworks(context.Background(), reg)
	if len(ready) != 2 || ready[0] != "attic" || ready[1] != "office" {
		t.Fatalf("ready = %v, want [attic office]", ready)
	}
}

// A nil registry is a box built without object storage, not an error.
func TestGfehReadyNetworksNilRegistry(t *testing.T) {
	if ready := GfehReadyNetworks(context.Background(), nil); ready != nil {
		t.Fatalf("ready = %v, want nil", ready)
	}
}

// TestReconcileGfehDoesNotLetOneDeadPartitionStarveTheRest.
//
// The wait for a partition's socket used to happen inside the per-partition
// loop, so the first daemon that never answered spent the caller's whole
// remaining deadline in WaitForReady. Everything after it — the other
// partitions' Start, and the prune — then ran on an expired context, which on a
// real systemd means they simply do not happen. One network's dead daemon took
// the rest of object storage with it, in whatever order the names sorted.
//
// The reconcile now waits on no socket at all. The one thing that needed a live
// daemon was seating a first user, and that is gone — nothing is created in a
// partition's forest except by an administrator asking for it — so there is no
// longer any point in the reconcile where a silent daemon can cost time.
func TestReconcileGfehDoesNotLetOneDeadPartitionStarveTheRest(t *testing.T) {
	// attic sorts before home, so under the old shape the partition that ate
	// the budget was not even the one the wait existed for.
	reg, _, _, base := gfehTestRegistry(t, "attic", "home", "office")

	// Generous relative to the work: if any partition still waits on its own
	// socket, this is what it consumes.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	ReconcileGfeh(ctx, reg)
	elapsed := time.Since(start)

	// No wait at all, so this is slack against scheduling noise rather than a
	// budget the reconcile is entitled to spend.
	if elapsed > 3*time.Second {
		t.Errorf("reconcile took %s: a partition is still waiting on its own socket", elapsed)
	}
	if ctx.Err() != nil {
		t.Errorf("the reconcile exhausted its caller's context: %v", ctx.Err())
	}

	// And every partition was actually converged, including the ones after the
	// dead one in sort order.
	managers := reg.Managers()
	for _, network := range []string{"attic", "home", "office"} {
		if _, ok := managers[network]; !ok {
			t.Errorf("no partition registered for %q", network)
		}
		if _, err := os.Stat(gfeh.ConfigPath(base, network)); err != nil {
			t.Errorf("no rendered config for %q: %v", network, err)
		}
	}
}

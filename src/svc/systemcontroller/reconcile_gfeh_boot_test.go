// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/systemd"
)

// Object storage coming up at boot, and staying visible when it does not.
//
// The reported symptom was always the same sentence — "object storage isn't
// working on boot" — and the reason it was hard to act on is that the box had
// exactly one way to say it. Any failure anywhere in the partition reconcile
// dropped the network from the registry, GET /gfeh answered with an empty list,
// and the UI rendered "object storage is not configured": the message for a box
// built without it. A partition that was down, a daemon still pulling its image,
// and a deliberately disabled build were one indistinguishable screen.
//
// These tests pin the three things that make the difference: a failed start
// stays on the books, a down partition is reported as down, and something
// periodic tries again.

// --- A failed start must not erase the partition ---

// TestReconcileGfehKeepsAPartitionWhoseDaemonFailedToStart. The registry is what
// the UI, the DNS collector, and the ingress collector all read; dropping an
// entry because its daemon would not start is how a transient failure at boot
// became "object storage is not configured" until the next reboot.
func TestReconcileGfehKeepsAPartitionWhoseDaemonFailedToStart(t *testing.T) {
	reg, sd, _, _ := gfehTestRegistry(t, "home")
	// Every systemd transition fails, which is what a box with a broken podman,
	// an unpullable image, or a unit that will not enable looks like from here.
	sd.StatusErr = errors.New("failed to start unit")

	ReconcileGfeh(gfehReconcileCtx(t), reg)

	managers := reg.Managers()
	if _, ok := managers["home"]; !ok {
		t.Fatalf("a partition that failed to start vanished from the registry; "+
			"the UI would say object storage is not configured. managers = %v", managers)
	}
}

// TestReconcileGfehKeepsAPartitionWhoseVolumeFailedToProvision covers the other
// early return: storage. Same reasoning — a partition whose subvolume could not
// be made is a broken partition, not an absent feature.
func TestReconcileGfehKeepsAPartitionWhoseVolumeFailedToProvision(t *testing.T) {
	reg, sd, _, _ := gfehTestRegistry(t, "home")
	// No storage at all: ensureGfehPartitionVolume returns "storage unavailable"
	// before anything else runs.
	reg.cfg.Storage = nil

	ReconcileGfeh(gfehReconcileCtx(t), reg)

	if _, ok := reg.Managers()["home"]; !ok {
		t.Fatal("a partition whose volume could not be provisioned vanished from the registry")
	}
	// And nothing was started over a partition with no storage behind it: the
	// daemon would open a data directory that does not exist and die, which is a
	// worse thing to leave running than a unit that was never installed.
	unit := systemd.SystemServiceUnitName("test-" + gfeh.ServiceKey("home"))
	if _, err := sd.ReadUnit(unit); err == nil {
		t.Error("a unit was installed for a partition whose volume could not be provisioned")
	}
}

// TestReconcileGfehStillPrunesAPartitionWhoseNetworkIsGone is the boundary of
// the rule above: registering unconditionally must not make partitions
// immortal. A network that no longer exists still loses its daemon.
func TestReconcileGfehStillPrunesAPartitionWhoseNetworkIsGone(t *testing.T) {
	reg, sd, _, _ := gfehTestRegistry(t, "home", "office")
	sd.StatusErr = errors.New("failed to start unit")
	ctx := gfehReconcileCtx(t)

	ReconcileGfeh(ctx, reg)
	if _, ok := reg.Managers()["office"]; !ok {
		t.Fatal("precondition: office was never registered")
	}

	nm, ok := reg.cfg.NetworkMgr.(interface {
		Remove(context.Context, string) error
	})
	if !ok {
		t.Fatal("the mock network manager cannot remove")
	}
	if err := nm.Remove(t.Context(), "office"); err != nil {
		t.Fatalf("remove network: %v", err)
	}

	ReconcileGfeh(ctx, reg)

	if _, ok := reg.Managers()["office"]; ok {
		t.Error("a partition whose network is gone survived the prune")
	}
}

// --- The UI is told which state it is in ---

// TestGfehListReportsADownPartitionRatherThanAnEmptyList is the symptom itself,
// at the wire. A partition whose daemon does not answer must come back as a row
// with running=false — the panel renders "not configured" only for an empty
// list, so an omitted row is a lie about the box.
func TestGfehListReportsADownPartitionRatherThanAnEmptyList(t *testing.T) {
	// A client pointed at a socket nothing is listening on: exactly a
	// registered partition whose daemon has not come up.
	h, _ := gfehUITest(t, map[string]gfeh.Client{
		"home": gfeh.NewClient(t.TempDir() + "/nothing-listens-here.sock"),
	})

	rec := callGfeh(t, h.listGfeh, http.MethodGet, "/gfeh", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	var partitions []GfehPartitionView
	decodeJSON(t, rec, &partitions)
	if len(partitions) != 1 {
		t.Fatalf("a down partition was omitted; the UI would say object storage "+
			"is not configured. got %+v", partitions)
	}
	if partitions[0].Network != "home" {
		t.Errorf("network = %q, want home", partitions[0].Network)
	}
	if partitions[0].Running {
		t.Error("a partition whose socket nothing is listening on reported running")
	}
}

// --- Something has to try again ---

// TestGfehPollerIntervalDefault pins the five-minute cadence. It is
// deliberately far shorter than the hourly DNS drift tick: this is the pass that
// brings a partition up at all, and an hour of dead object storage after a cold
// boot is indistinguishable from broken.
func TestGfehPollerIntervalDefault(t *testing.T) {
	s := &serverBase{}
	if got := s.pollerGfehIntervalValue(); got != 5*time.Minute {
		t.Fatalf("default gfeh poller interval = %v, want 5m", got)
	}
}

func TestGfehPollerIntervalOverride(t *testing.T) {
	s := &serverBase{pollerGfehInterval: 10 * time.Millisecond}
	if got := s.pollerGfehIntervalValue(); got != 10*time.Millisecond {
		t.Fatalf("overridden gfeh poller interval = %v, want 10ms", got)
	}
}

// TestTickGfehPollWithNoRegistryIsANoOp: a box built without object storage
// (GFEH_IMAGE explicitly empty) must not have its poller panic or dial.
func TestTickGfehPollWithNoRegistryIsANoOp(t *testing.T) {
	s := &serverBase{}
	s.tickGfehPoll(context.Background())
}

// TestTickGfehPollRepublishesWhenAPartitionComesUp is the whole point of the
// tick.
//
// A partition that starts answering after the last rebuild contributes nothing
// to DNS or the ingress until the next one, and nothing else rebuilds the
// ingress on a timer — so before this, a partition that lost its race at boot
// stayed nameless even once its daemon was healthy.
//
// Note what brings the daemon up here: nothing in the tick. The registry is a
// stub, so the reconcile is a no-op, and the socket starts listening between the
// two ticks entirely on its own — which is how it happens in production, where
// the unit carries Restart=always and systemd owns the retry. A tick that
// compared the ready set before and against after its *own* reconcile would see
// no change and publish nothing, which is precisely the case this poller exists
// for.
func TestTickGfehPollRepublishesWhenAPartitionComesUp(t *testing.T) {
	socket := t.TempDir() + "/admin.sock"
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{"home": gfeh.NewClient(socket)}}

	var dnsCalls atomic.Int32
	ing := &ingress.MockClient{}
	s := &serverBase{
		ServerConfig: ServerConfig{
			GfehRegistry:  reg,
			IngressClient: ing,
		},
		pollerDNSReconciler: func(context.Context) error {
			dnsCalls.Add(1)
			return nil
		},
	}

	// Nothing is listening: the partition is registered but down. No republish,
	// because nothing changed and a rebuild tears down and re-pushes the whole
	// route set.
	s.tickGfehPoll(context.Background())
	if dnsCalls.Load() != 0 || setRoutesCalls(ing) != 0 {
		t.Fatalf("a tick over an unchanged (down) partition republished: dns=%d ingress=%d",
			dnsCalls.Load(), setRoutesCalls(ing))
	}

	// The daemon comes up between ticks, which is exactly the boot race.
	startGfehHealthSocket(t, socket)

	s.tickGfehPoll(context.Background())
	if dnsCalls.Load() == 0 {
		t.Error("a partition that came up did not get its DNS names republished")
	}
	if setRoutesCalls(ing) == 0 {
		t.Error("a partition that came up did not get an ingress route; " +
			"nothing else reprograms the ingress on a timer")
	}
}

// TestTickGfehPollDoesNotRepublishWhenNothingChanged. RebuildDNS wipes and
// rebuilds the zone and RebuildIngress pushes the whole route set; doing that
// every five minutes forever is a reload storm and a window in which names
// briefly do not resolve.
func TestTickGfehPollDoesNotRepublishWhenNothingChanged(t *testing.T) {
	socket := t.TempDir() + "/admin.sock"
	startGfehHealthSocket(t, socket)
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{"home": gfeh.NewClient(socket)}}

	var dnsCalls atomic.Int32
	ing := &ingress.MockClient{}
	s := &serverBase{
		ServerConfig:        ServerConfig{GfehRegistry: reg, IngressClient: ing},
		pollerDNSReconciler: func(context.Context) error { dnsCalls.Add(1); return nil },
	}

	// First tick sees it come up (nothing → home) and republishes.
	s.tickGfehPoll(context.Background())
	dnsAfterFirst := dnsCalls.Load()
	ingressAfterFirst := setRoutesCalls(ing)

	// Asserted, not assumed: without this the rest of the test passes trivially
	// on a poller that never republishes anything, which is exactly the bug the
	// sibling test caught.
	if dnsAfterFirst == 0 || ingressAfterFirst == 0 {
		t.Fatalf("precondition: the first tick published nothing (dns=%d ingress=%d)",
			dnsAfterFirst, ingressAfterFirst)
	}

	for range 3 {
		s.tickGfehPoll(context.Background())
	}

	if dnsCalls.Load() != dnsAfterFirst {
		t.Errorf("steady-state ticks rebuilt DNS: %d -> %d", dnsAfterFirst, dnsCalls.Load())
	}
	if setRoutesCalls(ing) != ingressAfterFirst {
		t.Errorf("steady-state ticks reprogrammed the ingress: %d -> %d",
			ingressAfterFirst, setRoutesCalls(ing))
	}
}

// TestGfehReadyNetworksOnlyCountsAnsweringPartitions is what the tick's change
// detection rests on. "Registered" and "answering" are different questions now
// that a failed start stays registered, and conflating them would either
// republish forever or never.
func TestGfehReadyNetworksOnlyCountsAnsweringPartitions(t *testing.T) {
	live := t.TempDir() + "/live.sock"
	startGfehHealthSocket(t, live)

	reg := stubGfehRegistry{clients: map[string]gfeh.Client{
		"home":   gfeh.NewClient(live),
		"office": gfeh.NewClient(t.TempDir() + "/dead.sock"),
	}}

	ready := GfehReadyNetworks(context.Background(), reg)
	if len(ready) != 1 || ready[0] != "home" {
		t.Errorf("ready = %v, want just the answering partition", ready)
	}
}

func TestGfehReadyNetworksWithNoRegistry(t *testing.T) {
	if got := GfehReadyNetworks(context.Background(), nil); got != nil {
		t.Errorf("GfehReadyNetworks(nil) = %v, want nil", got)
	}
}

// --- The unit the reconcile installs ---

// TestGfehUnitIsInstalledEvenWhenStartingFails. Start installs and enables
// before it starts, so a box whose podman is momentarily unavailable still has
// the unit on disk — systemd's Restart=always then keeps trying, and the
// partition can come up without the controller running at all.
func TestGfehUnitIsInstalledEvenWhenStartingFails(t *testing.T) {
	reg, sd, _, _ := gfehTestRegistry(t, "home")
	sd.StatusErr = errors.New("failed to start unit")

	ReconcileGfeh(gfehReconcileCtx(t), reg)

	unit := systemd.SystemServiceUnitName("test-" + gfeh.ServiceKey("home"))
	if _, err := sd.ReadUnit(unit); err != nil {
		t.Errorf("no unit was installed for a partition whose start failed: %v", err)
	}
}

// --- helpers ---

// setRoutesCalls counts full route-set pushes to the ingress.
//
// SetRoutes is the declarative push — it replaces everything — so its call
// count is exactly "how many times did we rebuild", which is what the tick's
// change guard is about.
func setRoutesCalls(m *ingress.MockClient) int { return len(m.SetCalls) }

// startGfehHealthSocket binds a Unix socket that answers GET /v1/health, which
// is the whole of what readiness means to Town OS.
//
// A real socket rather than a stubbed client: the distinction the poller turns
// on is "the daemon is answering" versus "the path exists but nothing is
// listening", and only a real dial can tell those apart. The path is the test's
// own temp dir, so concurrent runs cannot collide — IRON RULE.
func startGfehHealthSocket(t *testing.T, socket string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(socket), 0o750); err != nil {
		t.Fatalf("mkdir socket dir: %v", err)
	}

	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatalf("listen on %s: %v", socket, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if encErr := json.NewEncoder(w).Encode(gfeh.Health{Status: "ok", Partition: "home"}); encErr != nil {
			t.Errorf("encode health: %v", encErr)
		}
	})
	// Names is asked for by the collectors during a republish; an empty list is
	// a partition that has come up but published nothing yet, which is a valid
	// answer and keeps this helper to the one fact it is about.
	mux.HandleFunc("/v1/names", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if encErr := json.NewEncoder(w).Encode(gfeh.NameList{Partition: "home"}); encErr != nil {
			t.Errorf("encode names: %v", encErr)
		}
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if serveErr := srv.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			t.Errorf("serve: %v", serveErr)
		}
	}()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			t.Logf("shutdown fake gfehd: %v", err)
		}
	})
}

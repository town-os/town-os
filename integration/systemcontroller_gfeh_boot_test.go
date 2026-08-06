// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// Object storage as it behaves across a boot, end to end through the reconcile
// that runs at startup and the HTTP surface the UI reads.
//
// The standing complaint was "object storage isn't working on boot", and what
// made it stick is that a box had one way of saying so. Every failure in the
// partition reconcile dropped the network out of the registry; GET /gfeh then
// answered with an empty array; the panel rendered "object storage is not
// configured" — the sentence for a box built without it. Whether the daemon was
// down, still pulling, or genuinely absent, the screen said the same thing.
//
// These tests drive the real reconcile against a mock systemd and mock btrfs,
// then read the real handler, so the chain from "the daemon did not start" to
// "what the operator sees" is covered in one place.

// gfehBootReconcile builds a registry over the real reconcile path, wired to
// mocks. keyPrefix and podman network are unique per run so a test's units can
// never be mistaken for a production partition's — IRON RULE.
func gfehBootReconcile(t *testing.T, networks ...string) (*systemd.MockManager, *storage.BtrFS, account.NetworkManager, string) {
	t.Helper()

	sd := systemd.InitMockManager()
	st := storage.InitBtrFSMock()
	nm := account.InitMockNetworkManager()
	for _, name := range networks {
		nm.Seed(&account.Network{Name: name, TLD: name, Enabled: true})
	}
	return sd, st, nm, gfehBootTempDir(t)
}

// gfehBootTempDir is t.TempDir() with a short name, and the length is the whole
// point.
//
// A partition's admin socket lands at <base>/gfeh-control/<network>/run/admin.sock
// — 40-odd characters below whatever base it is given — and a Unix socket path
// cannot exceed sockaddr_un's 108-byte sun_path (107 usable). t.TempDir() builds
// its directory out of the *test's own name*, so a descriptively-named test here
// pushes the socket past the limit and bind fails with EINVAL, which reads as
// "invalid argument" and looks nothing like "your path is too long".
//
// MkdirTemp with a short prefix sits on the same filesystem t.TempDir() would
// have used and is still unique per run — IRON RULE — it just spends ~60 fewer
// characters getting there.
func gfehBootTempDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "gfehboot") //nolint:usetesting // t.TempDir() names the directory after the test, which overflows sun_path here
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove temp dir: %v", err)
		}
	})
	return dir
}

// TestIntegrationGfehPartitionIsReportedDownNotAbsent is the reported bug,
// reproduced through the real reconcile and read back through the real handler.
//
// A partition whose daemon will not start must come back from GET /gfeh as a
// row with running=false. An omitted row is what the UI turns into "object
// storage is not configured", which is never true of a box that has networks —
// and it is the reason the same report kept coming back with no way to tell
// which of three completely different things had gone wrong.
func TestIntegrationGfehPartitionIsReportedDownNotAbsent(t *testing.T) {
	t.Parallel()

	sd, st, nm, base := gfehBootReconcile(t, "home")
	// Every systemd transition fails: a box whose podman is not up yet, whose
	// image is not local, or whose unit will not enable.
	sd.StatusErr = errors.New("Failed to start town-os-system--gfeh-home.service")

	reg := systemcontroller.NewGfehRegistry(systemcontroller.ReconcileGfehConfig{
		NetworkMgr:    nm,
		Storage:       st,
		Systemd:       sd,
		BtrfsBasePath: base,
		Image:         "localhost/town-os-gfeh:test",
		KeyPrefix:     "test-",
		NetworkName:   "town-os-ingress-test",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	systemcontroller.ReconcileGfeh(ctx, reg)

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:      st,
		NetworkMgr:   nm,
		GfehRegistry: reg,
	})
	t.Cleanup(ts.Close)

	partitions := getGfehPartitions(t, ts.Server.URL)
	if len(partitions) != 1 {
		t.Fatalf("a partition whose daemon failed to start was omitted from GET /gfeh, "+
			"so the UI reports object storage as not configured. got %+v", partitions)
	}
	if partitions[0].Network != "home" {
		t.Errorf("network = %q, want home", partitions[0].Network)
	}
	if partitions[0].Running {
		t.Error("a partition whose daemon never started reported running")
	}
}

// TestIntegrationGfehComesUpOnASecondReconcile is the boot race itself.
//
// A partition can lose at boot for reasons that are all transient — podman not
// yet up, the ingress network not yet created, the image still landing. The
// contract is that a later pass fixes it without a restart, which is what the
// periodic tick is for; before it existed, boot was the only attempt and a lost
// race meant object storage stayed down until somebody rebooted the box.
func TestIntegrationGfehComesUpOnASecondReconcile(t *testing.T) {
	t.Parallel()

	sd, st, nm, base := gfehBootReconcile(t, "home")
	sd.StatusErr = errors.New("podman is not ready")

	reg := systemcontroller.NewGfehRegistry(systemcontroller.ReconcileGfehConfig{
		NetworkMgr:    nm,
		Storage:       st,
		Systemd:       sd,
		BtrfsBasePath: base,
		Image:         "localhost/town-os-gfeh:test",
		KeyPrefix:     "test-",
		NetworkName:   "town-os-ingress-test",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	systemcontroller.ReconcileGfeh(ctx, reg)

	// The precondition is that the partition lost: it is on the books, and no
	// daemon is answering for it. Note what is deliberately *not* asserted — that
	// the unit is absent. Start installs and enables before it starts, so a box
	// whose podman is momentarily away still has the unit on disk and systemd's
	// Restart=always keeps trying; that is the behavior, not the failure.
	unit := systemd.SystemServiceUnitName("test-" + gfeh.ServiceKey("home"))
	if _, ok := reg.Managers()["home"]; !ok {
		t.Fatal("precondition: the partition was dropped from the registry instead of registered down")
	}
	if ready := systemcontroller.GfehReadyNetworks(ctx, reg); len(ready) != 0 {
		t.Fatalf("precondition: a partition answered despite a failing systemd: %v", ready)
	}

	// Whatever was wrong clears, and the daemon starts answering.
	sd.StatusErr = nil
	startFakeGfehd(t, gfeh.SocketPath(base, "home"), namesFor("home", ""))

	systemcontroller.ReconcileGfeh(ctx, reg)

	if _, err := sd.ReadUnit(unit); err != nil {
		t.Errorf("the second pass did not install the unit: %v", err)
	}
	ready := systemcontroller.GfehReadyNetworks(ctx, reg)
	if len(ready) != 1 || ready[0] != "home" {
		t.Errorf("ready partitions = %v, want home; a partition that lost the boot race "+
			"never recovered", ready)
	}
}

// TestIntegrationGfehReadySetDistinguishesRegisteredFromAnswering. Now that a
// failed start stays registered, "there is a partition" and "the partition is
// answering" are different questions — and the periodic tick republishes on the
// difference between them. Conflating the two would either rebuild DNS and the
// ingress on every tick forever, or never rebuild them at all.
func TestIntegrationGfehReadySetDistinguishesRegisteredFromAnswering(t *testing.T) {
	t.Parallel()

	sd, st, nm, base := gfehBootReconcile(t, "home", "office")

	reg := systemcontroller.NewGfehRegistry(systemcontroller.ReconcileGfehConfig{
		NetworkMgr:    nm,
		Storage:       st,
		Systemd:       sd,
		BtrfsBasePath: base,
		Image:         "localhost/town-os-gfeh:test",
		KeyPrefix:     "test-",
		NetworkName:   "town-os-ingress-test",
	})

	// Only office gets a daemon. home is provisioned and registered, and never
	// answers — deliberately the non-default network that is up, so this also
	// proves the tracking is not special-cased to home.
	if err := os.MkdirAll(filepath.Dir(gfeh.SocketPath(base, "office")), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	startFakeGfehd(t, gfeh.SocketPath(base, "office"), namesFor("office", "office"))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	systemcontroller.ReconcileGfeh(ctx, reg)

	if got := len(reg.Managers()); got != 2 {
		t.Fatalf("registered %d partitions, want 2 (both networks have one, answering or not)", got)
	}
	ready := systemcontroller.GfehReadyNetworks(ctx, reg)
	if len(ready) != 1 || ready[0] != "office" {
		t.Errorf("ready = %v, want only the partition that is answering", ready)
	}
}

// TestIntegrationGfehListSaysNotConfiguredOnlyWithNoPartitions pins the other
// side of the contract: the empty array — the thing the UI renders as "object
// storage is not configured" — is reserved for a box that genuinely has none.
func TestIntegrationGfehListSaysNotConfiguredOnlyWithNoPartitions(t *testing.T) {
	t.Parallel()

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:    storage.InitBtrFSMock(),
		NetworkMgr: account.InitMockNetworkManager(),
		// No registry at all: GFEH_IMAGE was explicitly empty, or the ingress is
		// disabled. This is the one case where "not configured" is the truth.
	})
	t.Cleanup(ts.Close)

	if partitions := getGfehPartitions(t, ts.Server.URL); len(partitions) != 0 {
		t.Errorf("a box with no object storage listed partitions: %+v", partitions)
	}
}

// getGfehPartitions reads GET /gfeh, the route the UI panel polls.
//
// Raw HTTP rather than the typed client: there is no client method for this
// route, and the point of the assertion is the JSON array the browser actually
// receives — an empty one is what the panel turns into "not configured".
func getGfehPartitions(t *testing.T, baseURL string) []systemcontroller.GfehPartitionView {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/gfeh", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /gfeh: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /gfeh status %d", resp.StatusCode)
	}

	var partitions []systemcontroller.GfehPartitionView
	if err := json.NewDecoder(resp.Body).Decode(&partitions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return partitions
}

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// TestProgrammedSettingsSurviveARolodexRestart is the end-to-end proof for the
// half of runtime configuration that boot cannot cover.
//
// rolodex.yml is bootstrap-only now — the install image writes the binds and
// the metrics listener, and nothing else — so forwarders, resolution mode and
// both blocklists exist only in the running process. rolodex seeds them from
// that file at startup and persists nothing set over gRPC, which means a
// restart it performed on its own (a crash under Restart=always, a DHCP lease
// change bouncing the unit, an operator restarting it by hand) silently returns
// the box to rolodex's defaults: resolving a different way than the operator
// chose, and filtering nothing.
//
// Nothing cheaper catches this. Every unit test of ProgramRolodex passes
// against a mock that never forgets, and the boot path programs a server that
// has just started. The bug is a setting that is gone from a server which is
// still running fine, so this test programs a real rolodex, restarts it, and
// asks the server itself what it is holding.
func TestProgrammedSettingsSurviveARolodexRestart(t *testing.T) {
	t.Parallel()

	dataDir := rolodexTempDir(t, "rolodex-reprogram-*")
	sd := systemd.NewManager()
	key := rolodexTestKey()
	dnsPort := findFreePort(t)
	metricsPort := findFreePort(t)

	mgr := rolodex.NewManager(pinRolodexDiscovery(t, rolodex.Config{
		Systemd:        sd,
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: filepath.Join(dataDir, "rolodex.sock"),
		DNSPort:        dnsPort,
		MetricsPort:    metricsPort,
		Key:            key,
		// Deliberately not rolodex's own default: a mode that matched the
		// default would still read as "programmed" after a restart that
		// dropped it.
		ResolutionMode: rolodex.ResolutionModeRecursive,
	}))

	writeRolodexBootstrapConfig(t, dataDir, dnsPort, metricsPort)
	startRolodexUnit(t, sd, key, dataDir, "Rolodex DNS (reprogram test)")

	ctx := testContext(t, 5*time.Minute)

	client := waitForRolodexClient(t, ctx, mgr.SocketPath(), dataDir, key)
	t.Cleanup(func() { logCleanupf(t, client.Close(), "close rolodex client") })

	// The config file says nothing about the mode, so a freshly started rolodex
	// is on its own default. That is the state this whole mechanism exists to
	// correct, and asserting it here is what proves the mode observed later
	// came from Town OS rather than from the file.
	if mode, modeErr := client.GetResolutionMode(ctx); modeErr != nil {
		t.Fatalf("get resolution mode: %v", modeErr)
	} else if mode == rolodex.ResolutionModeRecursive {
		t.Fatalf("rolodex started already in %q; this test cannot tell programming from bootstrap", mode)
	}

	if progErr := systemcontroller.ProgramRolodex(ctx, client, mgr, nil); progErr != nil {
		t.Fatalf("ProgramRolodex: %v", progErr)
	}
	assertResolutionMode(ctx, t, client, rolodex.ResolutionModeRecursive, "after programming")

	// The restart. rolodex comes back reading the same bootstrap file, with
	// everything Town OS programmed gone.
	if err := sd.SetStatus(ctx, mgr.UnitName(), systemd.Restart); err != nil {
		t.Fatalf("restart rolodex: %v", err)
	}

	// Redialing is not enough on its own to know rolodex is back: rolodex.Dial
	// returns a lazy gRPC connection, so it succeeds against a socket file the
	// dead process left behind and the failure surfaces one RPC later as a
	// connection refused. waitForRolodexClient round-trips a real call.
	restarted := waitForRolodexClient(t, ctx, mgr.SocketPath(), dataDir, key)
	t.Cleanup(func() { logCleanupf(t, restarted.Close(), "close redialed rolodex client") })

	if mode, modeErr := restarted.GetResolutionMode(ctx); modeErr != nil {
		t.Fatalf("get resolution mode after restart: %v", modeErr)
	} else if mode == rolodex.ResolutionModeRecursive {
		t.Fatal("rolodex kept the programmed mode across a restart; it is documented as holding this in memory only, so this test is no longer testing anything")
	}

	// What the reprogramming tick does when it notices a new generation.
	if progErr := systemcontroller.ProgramRolodex(ctx, restarted, mgr, nil); progErr != nil {
		t.Fatalf("ProgramRolodex after restart: %v", progErr)
	}
	assertResolutionMode(ctx, t, restarted, rolodex.ResolutionModeRecursive, "after reprogramming")
}

// TestGenerationChangesOnlyWhenRolodexRestarts covers the signal the tick keys
// on. Too sticky and a restart goes unnoticed until the next boot; too loose
// and every tick reprograms a server that never moved.
func TestGenerationChangesOnlyWhenRolodexRestarts(t *testing.T) {
	t.Parallel()

	dataDir := rolodexTempDir(t, "rolodex-generation-*")
	sd := systemd.NewManager()
	key := rolodexTestKey()
	dnsPort := findFreePort(t)

	mgr := rolodex.NewManager(pinRolodexDiscovery(t, rolodex.Config{
		Systemd:        sd,
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: filepath.Join(dataDir, "rolodex.sock"),
		DNSPort:        dnsPort,
		MetricsPort:    findFreePort(t),
		Key:            key,
	}))

	if gen := mgr.Generation(); gen != "" {
		t.Fatalf("Generation() = %q before rolodex has ever run, want empty", gen)
	}

	writeRolodexBootstrapConfig(t, dataDir, dnsPort, mgr.MetricsPort())
	startRolodexUnit(t, sd, key, dataDir, "Rolodex DNS (generation test)")

	ctx := testContext(t, 3*time.Minute)

	first := mgr.Generation()
	if first == "" {
		t.Fatal("Generation() is empty with rolodex running")
	}
	if again := mgr.Generation(); again != first {
		t.Fatalf("Generation() changed without a restart: %q then %q", first, again)
	}

	if err := sd.SetStatus(ctx, mgr.UnitName(), systemd.Restart); err != nil {
		t.Fatalf("restart rolodex: %v", err)
	}

	// The generation is read off the socket, so it can only be compared once
	// the new rolodex has rebound one. Until then the file on disk is still the
	// dead process's, and the comparison below would be against a socket that
	// nothing is listening on — reporting "the tick would never reprogram"
	// about a restart that had not finished yet. SetStatus already waited for
	// the systemd job, which is the old process gone; this waits for the new
	// one to answer.
	client := waitForRolodexClient(t, ctx, mgr.SocketPath(), dataDir, key)
	logCleanupf(t, client.Close(), "close readiness client")

	if after := mgr.Generation(); after == first {
		t.Fatalf("Generation() = %q both before and after a restart; the tick would never reprogram", after)
	}
}

func assertResolutionMode(ctx context.Context, t *testing.T, client rolodex.Client, want, when string) {
	t.Helper()

	got, err := client.GetResolutionMode(ctx)
	if err != nil {
		t.Fatalf("get resolution mode %s: %v", when, err)
	}
	if got != want {
		t.Fatalf("resolution mode %s = %q, want %q", when, got, want)
	}
}

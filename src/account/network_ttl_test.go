// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package account

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// findPeer returns the peer with publicKey from a ListPeers result, or fails.
func findPeer(t *testing.T, peers []NetworkPeer, publicKey string) NetworkPeer {
	t.Helper()
	for _, p := range peers {
		if p.PublicKey == publicKey {
			return p
		}
	}
	t.Fatalf("peer %q not found in %+v", publicKey, peers)
	return NetworkPeer{}
}

// --- AddPeer records the TTL and creator; round-trips through the DB ---

func TestAddPeerStoresExpiryAndCreatedBy(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	expires := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{
		Network: "office", PublicKey: "TTL", AllowedIP: "10.90.12.2/32",
		CreatedBy: "portal", ExpiresAt: &expires,
	}); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	got := findPeer(t, mustListPeers(t, mgr, "office"), "TTL")
	if got.CreatedBy != "portal" {
		t.Errorf("CreatedBy = %q, want portal", got.CreatedBy)
	}
	if got.ExpiresAt == nil {
		t.Fatal("ExpiresAt is nil, want a TTL")
	}
	if !got.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, expires)
	}
}

// A peer added with no ExpiresAt is permanent: it reads back nil and the reaper
// never touches it.
func TestAddPeerWithoutExpiryIsPermanent(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{Network: "office", PublicKey: "PERM", AllowedIP: "10.90.12.2/32"}); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	got := findPeer(t, mustListPeers(t, mgr, "office"), "PERM")
	if got.ExpiresAt != nil {
		t.Errorf("ExpiresAt = %v, want nil (permanent)", got.ExpiresAt)
	}

	// Reaping far in the future must not remove a permanent peer.
	reaped, err := mgr.ReapExpiredPeers(t.Context(), time.Now().Add(1000 * time.Hour))
	if err != nil {
		t.Fatalf("ReapExpiredPeers: %v", err)
	}
	if len(reaped) != 0 {
		t.Errorf("permanent peer was reaped: %+v", reaped)
	}
	if len(mustListPeers(t, mgr, "office")) != 1 {
		t.Error("permanent peer disappeared")
	}
}

// --- RefreshPeer slides the expiry forward ---

func TestRefreshPeerExtendsExpiry(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	past := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{
		Network: "office", PublicKey: "HB", AllowedIP: "10.90.12.2/32", ExpiresAt: &past,
	}); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	future := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	if err := mgr.RefreshPeer(t.Context(), "office", "HB", future); err != nil {
		t.Fatalf("RefreshPeer: %v", err)
	}

	got := findPeer(t, mustListPeers(t, mgr, "office"), "HB")
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(future) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, future)
	}

	// A now-refreshed peer survives a reap at the present instant even though its
	// original TTL was in the past.
	reaped, err := mgr.ReapExpiredPeers(t.Context(), time.Now())
	if err != nil {
		t.Fatalf("ReapExpiredPeers: %v", err)
	}
	if len(reaped) != 0 {
		t.Errorf("refreshed peer was reaped: %+v", reaped)
	}
}

func TestRefreshPeerMissing(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := mgr.RefreshPeer(t.Context(), "office", "nope", time.Now()); !errors.Is(err, ErrNetworkPeerNotFound) {
		t.Fatalf("RefreshPeer missing: got %v, want ErrNetworkPeerNotFound", err)
	}
}

// --- ReapExpiredPeers removes only lapsed peers and returns them ---

func TestReapExpiredPeers(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	expired := now.Add(-time.Minute)
	live := now.Add(time.Hour)

	// One lapsed, one still-valid, one permanent.
	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{Network: "office", PublicKey: "DEAD", AllowedIP: "10.90.12.2/32", CreatedBy: "portal", ExpiresAt: &expired}); err != nil {
		t.Fatalf("AddPeer dead: %v", err)
	}
	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{Network: "office", PublicKey: "LIVE", AllowedIP: "10.90.12.3/32", ExpiresAt: &live}); err != nil {
		t.Fatalf("AddPeer live: %v", err)
	}
	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{Network: "office", PublicKey: "PERM", AllowedIP: "10.90.12.4/32"}); err != nil {
		t.Fatalf("AddPeer perm: %v", err)
	}

	reaped, err := mgr.ReapExpiredPeers(t.Context(), now)
	if err != nil {
		t.Fatalf("ReapExpiredPeers: %v", err)
	}
	if len(reaped) != 1 || reaped[0].PublicKey != "DEAD" {
		t.Fatalf("reaped = %+v, want just DEAD", reaped)
	}
	// The returned peer carries enough to tear down runtime state.
	if reaped[0].AllowedIP != "10.90.12.2/32" || reaped[0].CreatedBy != "portal" {
		t.Errorf("reaped peer missing detail: %+v", reaped[0])
	}

	remaining := mustListPeers(t, mgr, "office")
	keys := map[string]bool{}
	for _, p := range remaining {
		keys[p.PublicKey] = true
	}
	if !keys["LIVE"] || !keys["PERM"] || keys["DEAD"] {
		t.Errorf("remaining peers = %+v, want LIVE and PERM only", remaining)
	}
}

// A peer whose expiry is exactly now is reaped (boundary is inclusive).
func TestReapExpiredPeersBoundaryInclusive(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{Network: "office", PublicKey: "EDGE", AllowedIP: "10.90.12.2/32", ExpiresAt: &now}); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	reaped, err := mgr.ReapExpiredPeers(t.Context(), now)
	if err != nil {
		t.Fatalf("ReapExpiredPeers: %v", err)
	}
	if len(reaped) != 1 {
		t.Fatalf("expiry == now should reap, got %+v", reaped)
	}
}

// Reaping an empty / all-permanent table returns no rows and no error.
func TestReapExpiredPeersNoneExpired(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	reaped, err := mgr.ReapExpiredPeers(t.Context(), time.Now())
	if err != nil {
		t.Fatalf("ReapExpiredPeers empty: %v", err)
	}
	if len(reaped) != 0 {
		t.Errorf("reaped from empty table: %+v", reaped)
	}
}

// --- Migration from a pre-TTL network_peers table ---

func TestPeerMigrationFromPreTTLDB(t *testing.T) {
	db, err := OpenDB(t.Context(), filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	ctx := context.Background()
	// The networks table plus a network_peers schema that predates created_by /
	// expires_at (and even rolodex), with a legacy peer row inserted through it.
	if _, err := db.ExecContext(ctx, `CREATE TABLE networks (
		name TEXT PRIMARY KEY, tld TEXT NOT NULL, subnet TEXT NOT NULL DEFAULT '',
		address TEXT NOT NULL DEFAULT '', public_key TEXT NOT NULL DEFAULT '',
		private_key TEXT NOT NULL DEFAULT '', listen_port INTEGER NOT NULL DEFAULT 0,
		enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create old networks: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE network_peers (
		network_name TEXT NOT NULL REFERENCES networks(name) ON DELETE CASCADE,
		public_key TEXT NOT NULL, name TEXT NOT NULL DEFAULT '',
		allowed_ip TEXT NOT NULL DEFAULT '', endpoint TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL, PRIMARY KEY (network_name, public_key))`); err != nil {
		t.Fatalf("create old network_peers: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO networks (name, tld, created_at, updated_at) VALUES ('office','office','2020-01-01T00:00:00Z','2020-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert legacy network: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO network_peers (network_name, public_key, name, allowed_ip, endpoint, created_at)
		 VALUES ('office','LEGACY','oldphone','10.90.12.2/32','','2020-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert legacy peer: %v", err)
	}

	mgr, err := InitNetworkManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitNetworkManager (migration): %v", err)
	}

	got := findPeer(t, mustListPeers(t, mgr, "office"), "LEGACY")
	if got.ExpiresAt != nil {
		t.Errorf("legacy peer should be permanent (nil expiry), got %v", got.ExpiresAt)
	}
	if got.CreatedBy != "" {
		t.Errorf("legacy peer CreatedBy = %q, want empty", got.CreatedBy)
	}
	if got.Rolodex {
		t.Error("legacy peer should default Rolodex=false")
	}

	// The migrated table accepts a TTL'd peer, and the reaper works on it.
	past := time.Now().Add(-time.Hour)
	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{Network: "office", PublicKey: "NEW", AllowedIP: "10.90.12.3/32", CreatedBy: "portal", ExpiresAt: &past}); err != nil {
		t.Fatalf("AddPeer on migrated DB: %v", err)
	}
	reaped, err := mgr.ReapExpiredPeers(t.Context(), time.Now())
	if err != nil {
		t.Fatalf("ReapExpiredPeers: %v", err)
	}
	if len(reaped) != 1 || reaped[0].PublicKey != "NEW" {
		t.Errorf("reaped = %+v, want NEW; legacy peer must survive", reaped)
	}
}

// --- Mock parity: the mock enforces the same TTL/reaper semantics ---

func TestMockPeerTTLParity(t *testing.T) {
	mgr := InitMockNetworkManager()
	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	now := time.Now()
	expired := now.Add(-time.Minute)
	future := now.Add(time.Hour)

	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{Network: "office", PublicKey: "DEAD", ExpiresAt: &expired, CreatedBy: "portal"}); err != nil {
		t.Fatalf("AddPeer dead: %v", err)
	}
	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{Network: "office", PublicKey: "PERM"}); err != nil {
		t.Fatalf("AddPeer perm: %v", err)
	}

	// Refresh the dead peer into the future; it must then survive the reap.
	if err := mgr.RefreshPeer(t.Context(), "office", "DEAD", future); err != nil {
		t.Fatalf("RefreshPeer: %v", err)
	}
	if err := mgr.RefreshPeer(t.Context(), "office", "ghost", future); !errors.Is(err, ErrNetworkPeerNotFound) {
		t.Fatalf("RefreshPeer missing: got %v, want ErrNetworkPeerNotFound", err)
	}

	reaped, err := mgr.ReapExpiredPeers(t.Context(), now)
	if err != nil {
		t.Fatalf("ReapExpiredPeers: %v", err)
	}
	if len(reaped) != 0 {
		t.Errorf("nothing should be reaped after refresh, got %+v", reaped)
	}

	// Now let it lapse and confirm it reaps, leaving the permanent peer.
	reaped, err = mgr.ReapExpiredPeers(t.Context(), future.Add(time.Minute))
	if err != nil {
		t.Fatalf("ReapExpiredPeers: %v", err)
	}
	if len(reaped) != 1 || reaped[0].PublicKey != "DEAD" {
		t.Fatalf("reaped = %+v, want DEAD", reaped)
	}
	remaining, err := mgr.ListPeers(t.Context(), "office")
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(remaining) != 1 || remaining[0].PublicKey != "PERM" {
		t.Errorf("remaining = %+v, want PERM only", remaining)
	}
}

// The default peer_ttl setting is present and parses to two hours.
func TestPeerTTLDefaultSetting(t *testing.T) {
	if got := DefaultSettings["peer_ttl"]; got != "7200" {
		t.Errorf("peer_ttl default = %q, want 7200", got)
	}
	d, err := time.ParseDuration(DefaultSettings["peer_ttl"] + "s")
	if err != nil {
		t.Fatalf("parse peer_ttl: %v", err)
	}
	if d != 2*time.Hour {
		t.Errorf("peer_ttl = %v, want 2h", d)
	}
}

func mustListPeers(t *testing.T, mgr NetworkManager, network string) []NetworkPeer {
	t.Helper()
	peers, err := mgr.ListPeers(t.Context(), network)
	if err != nil {
		t.Fatalf("ListPeers(%q): %v", network, err)
	}
	return peers
}

// Guard against an accidental signature drift on the shared scanner used by both
// list and reap paths: both must surface identical columns.
func TestListAndReapReturnSameShape(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(t.Context(), sampleNetwork("lab")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	past := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	in := &NetworkPeer{Network: "lab", PublicKey: "K", Name: "n", AllowedIP: "10.90.12.2/32", Endpoint: "e:1", Rolodex: true, CreatedBy: "portal", ExpiresAt: &past}
	if _, err := mgr.AddPeer(t.Context(), in); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	listed := findPeer(t, mustListPeers(t, mgr, "lab"), "K")
	reaped, err := mgr.ReapExpiredPeers(t.Context(), time.Now())
	if err != nil {
		t.Fatalf("ReapExpiredPeers: %v", err)
	}
	if len(reaped) != 1 {
		t.Fatalf("expected one reaped peer, got %+v", reaped)
	}
	if !reflect.DeepEqual(listed, reaped[0]) {
		t.Errorf("list vs reap shape mismatch:\n list = %+v\n reap = %+v", listed, reaped[0])
	}
}

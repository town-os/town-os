// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package account

import (
	"errors"
	"path/filepath"
	"testing"
)

func initNetworkTestDB(t *testing.T) *SQLiteNetworkManager {
	t.Helper()
	db, err := OpenDB(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := InitNetworkManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitNetworkManager: %v", err)
	}
	return mgr
}

func sampleNetwork(name string) *Network {
	return &Network{
		Name:       name,
		TLD:        name,
		Subnet:     "10.90.12.0/24",
		Address:    "10.90.12.1/24",
		PublicKey:  "PUB",
		PrivateKey: "PRIV",
		ListenPort: 51820,
		Enabled:    true,
	}
}

func TestNetworkCreateGetList(t *testing.T) {
	mgr := initNetworkTestDB(t)

	created, err := mgr.Create(t.Context(), sampleNetwork("lab"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.CreatedAt.IsZero() {
		t.Error("created_at not set")
	}

	got, err := mgr.Get(t.Context(), "lab")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PrivateKey != "PRIV" || got.Subnet != "10.90.12.0/24" || !got.Enabled {
		t.Errorf("unexpected network: %+v", got)
	}

	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); err != nil {
		t.Fatalf("Create office: %v", err)
	}
	list, err := mgr.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// The seeded home network is in here too, and sorts first.
	if len(list) != 3 || list[0].Name != DefaultNetworkName || list[1].Name != "lab" || list[2].Name != "office" {
		t.Errorf("unexpected list: %+v", list)
	}
}

// The home network exists from the moment the table does, before anything has
// created one. Everything downstream is written assuming it is there -- the
// first account is scoped to it, the default TLD is its TLD, and gfeh gives it
// a partition -- so a box where it had to be created first has a window in
// which all of that is false.
func TestNetworkManagerSeedsTheDefaultNetwork(t *testing.T) {
	mgr := initNetworkTestDB(t)

	home, err := mgr.Get(t.Context(), DefaultNetworkName)
	if err != nil {
		t.Fatalf("the home network was not seeded: %v", err)
	}
	if home.TLD != DefaultNetworkName {
		t.Errorf("TLD = %q, want %q", home.TLD, DefaultNetworkName)
	}
	if !home.Enabled {
		t.Error("the home network came back disabled")
	}
	// DNS-only: it never gets a WireGuard interface, so carrying a subnet or a
	// keypair would be inventing transport nothing brings up.
	if home.Subnet != "" || home.Address != "" || home.PublicKey != "" || home.PrivateKey != "" || home.ListenPort != 0 {
		t.Errorf("the home network carries WireGuard transport it never uses: %+v", home)
	}
}

// Seeding runs on every InitNetworkManager, including against a database that
// already has the row -- an existing box opens its accounts DB on every boot.
func TestNetworkManagerSeedIsIdempotent(t *testing.T) {
	db, err := OpenDB(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	first, err := InitNetworkManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitNetworkManager: %v", err)
	}
	if err := first.SetTLD(t.Context(), DefaultNetworkName, "lan"); err != nil {
		t.Fatalf("SetTLD: %v", err)
	}

	second, err := InitNetworkManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitNetworkManager again: %v", err)
	}
	home, err := second.Get(t.Context(), DefaultNetworkName)
	if err != nil {
		t.Fatalf("Get home after reopen: %v", err)
	}
	// Re-seeding must not clobber a TLD the controller reconciled from dns_tld.
	if home.TLD != "lan" {
		t.Errorf("TLD = %q after reopen, want lan", home.TLD)
	}
	if n, err := second.Count(t.Context()); err != nil {
		t.Fatalf("Count: %v", err)
	} else if n != 1 {
		t.Errorf("count = %d after reopening, want 1", n)
	}
}

// SetTLD is how the controller reconciles the home network against dns_tld.
func TestNetworkSetTLD(t *testing.T) {
	mgr := initNetworkTestDB(t)

	if err := mgr.SetTLD(t.Context(), DefaultNetworkName, "lan"); err != nil {
		t.Fatalf("SetTLD: %v", err)
	}
	got, err := mgr.Get(t.Context(), DefaultNetworkName)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TLD != "lan" {
		t.Errorf("TLD = %q, want lan", got.TLD)
	}
	if err := mgr.SetTLD(t.Context(), "nope", "lan"); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("SetTLD on a missing network = %v, want ErrNetworkNotFound", err)
	}
}

func TestNetworkDuplicateRejected(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); !errors.Is(err, ErrDuplicateNetwork) {
		t.Fatalf("expected ErrDuplicateNetwork, got %v", err)
	}
	// Including the seeded one, which nothing may create a second time.
	if _, err := mgr.Create(t.Context(), sampleNetwork(DefaultNetworkName)); !errors.Is(err, ErrDuplicateNetwork) {
		t.Fatalf("creating the home network = %v, want ErrDuplicateNetwork", err)
	}
}

func TestNetworkInvalidName(t *testing.T) {
	mgr := initNetworkTestDB(t)
	for _, bad := range []string{"", "Home", "has space", "-lead", "trail-", "under_score", "dot.dot"} {
		if _, err := mgr.Create(t.Context(), sampleNetwork(bad)); err == nil {
			t.Errorf("expected error for name %q", bad)
		}
	}
}

func TestNetworkGetMissing(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Get(t.Context(), "nope"); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("expected ErrNetworkNotFound, got %v", err)
	}
}

func TestNetworkRemove(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := mgr.Remove(t.Context(), "office"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := mgr.Get(t.Context(), "office"); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("expected not found after remove, got %v", err)
	}
	if err := mgr.Remove(t.Context(), "office"); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("expected ErrNetworkNotFound, got %v", err)
	}
}

func TestNetworkDefaultProtected(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if err := mgr.Remove(t.Context(), "home"); !errors.Is(err, ErrNetworkProtected) {
		t.Fatalf("expected ErrNetworkProtected, got %v", err)
	}
}

func TestNetworkSetEnabled(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := mgr.SetEnabled(t.Context(), "office", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	got, err := mgr.Get(t.Context(), "office")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Enabled {
		t.Error("expected disabled")
	}
	if err := mgr.SetEnabled(t.Context(), "missing", true); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("expected ErrNetworkNotFound, got %v", err)
	}
}

func TestNetworkCount(t *testing.T) {
	mgr := initNetworkTestDB(t)
	n, err := mgr.Count(t.Context())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	// The seeded home network, before anything else is created.
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	for _, name := range []string{"office", "lab"} {
		if _, err := mgr.Create(t.Context(), sampleNetwork(name)); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	n, err = mgr.Count(t.Context())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 3 {
		t.Fatalf("count = %d, want 3", n)
	}
}

func TestNetworkPeers(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{Network: "office", PublicKey: "KEY2", Name: "laptop", AllowedIP: "10.90.12.3/32"}); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{Network: "office", PublicKey: "KEY1", Name: "phone", AllowedIP: "10.90.12.2/32"}); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	// Duplicate key on same network rejected.
	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{Network: "office", PublicKey: "KEY1", AllowedIP: "10.90.12.9/32"}); !errors.Is(err, ErrDuplicateNetworkPeer) {
		t.Fatalf("expected ErrDuplicateNetworkPeer, got %v", err)
	}
	// Peer on unknown network rejected.
	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{Network: "ghost", PublicKey: "KEY9"}); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("expected ErrNetworkNotFound, got %v", err)
	}

	peers, err := mgr.ListPeers(t.Context(), "office")
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 2 || peers[0].AllowedIP != "10.90.12.2/32" {
		t.Fatalf("unexpected peers order: %+v", peers)
	}

	if err := mgr.RemovePeer(t.Context(), "office", "KEY1"); err != nil {
		t.Fatalf("RemovePeer: %v", err)
	}
	peers, err = mgr.ListPeers(t.Context(), "office")
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 1 || peers[0].PublicKey != "KEY2" {
		t.Fatalf("unexpected peers after remove: %+v", peers)
	}
	if err := mgr.RemovePeer(t.Context(), "office", "KEY1"); !errors.Is(err, ErrNetworkPeerNotFound) {
		t.Fatalf("expected ErrNetworkPeerNotFound, got %v", err)
	}
}

func TestNetworkPeerRolodexFlag(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{Network: "office", PublicKey: "ROL", AllowedIP: "10.90.12.2/32", Rolodex: true}); err != nil {
		t.Fatalf("AddPeer rolodex: %v", err)
	}
	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{Network: "office", PublicKey: "PLAIN", AllowedIP: "10.90.12.3/32"}); err != nil {
		t.Fatalf("AddPeer plain: %v", err)
	}

	peers, err := mgr.ListPeers(t.Context(), "office")
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	got := map[string]bool{}
	for _, p := range peers {
		got[p.PublicKey] = p.Rolodex
	}
	if !got["ROL"] {
		t.Errorf("expected ROL peer to have Rolodex=true")
	}
	if got["PLAIN"] {
		t.Errorf("expected PLAIN peer to default Rolodex=false")
	}
}

func TestNetworkRemoveCascadesPeers(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{Network: "office", PublicKey: "KEY1", AllowedIP: "10.90.12.2/32"}); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	if err := mgr.Remove(t.Context(), "office"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	// Re-create and confirm no orphaned peers survived the cascade.
	if _, err := mgr.Create(t.Context(), sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	peers, err := mgr.ListPeers(t.Context(), "office")
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected peers cascade-deleted, got %+v", peers)
	}
}

// The home network is DNS-only: no subnet, no keypair, no interface. A peer row
// on it would describe a tunnel that does not exist, and every account is a
// member of home -- so the manager refuses it rather than leaving the rule to
// whichever caller happens to be in front of it.
func TestAddPeerRefusesHomeNetwork(t *testing.T) {
	mgr := initNetworkTestDB(t)

	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{
		Network:   DefaultNetworkName,
		PublicKey: "KEY1",
		AllowedIP: "10.90.12.2/32",
	}); !errors.Is(err, ErrNetworkDNSOnly) {
		t.Fatalf("AddPeer on the home network = %v, want ErrNetworkDNSOnly", err)
	}

	peers, err := mgr.ListPeers(t.Context(), DefaultNetworkName)
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("home network has peers after a refused AddPeer: %+v", peers)
	}
}

// The refusal must be about the home network specifically, not about peers.
func TestAddPeerStillWorksOnRealNetwork(t *testing.T) {
	mgr := initNetworkTestDB(t)

	if _, err := mgr.Create(t.Context(), sampleNetwork("lab")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := mgr.AddPeer(t.Context(), &NetworkPeer{
		Network:   "lab",
		PublicKey: "KEY1",
		AllowedIP: "10.90.12.2/32",
	}); err != nil {
		t.Fatalf("AddPeer on a real network: %v", err)
	}
}

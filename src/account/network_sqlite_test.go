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
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := InitNetworkManager(db)
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

	created, err := mgr.Create(sampleNetwork("home"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.CreatedAt.IsZero() {
		t.Error("created_at not set")
	}

	got, err := mgr.Get("home")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PrivateKey != "PRIV" || got.Subnet != "10.90.12.0/24" || !got.Enabled {
		t.Errorf("unexpected network: %+v", got)
	}

	if _, err := mgr.Create(sampleNetwork("office")); err != nil {
		t.Fatalf("Create office: %v", err)
	}
	list, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 || list[0].Name != "home" || list[1].Name != "office" {
		t.Errorf("unexpected list: %+v", list)
	}
}

func TestNetworkDuplicateRejected(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(sampleNetwork("home")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := mgr.Create(sampleNetwork("home")); !errors.Is(err, ErrDuplicateNetwork) {
		t.Fatalf("expected ErrDuplicateNetwork, got %v", err)
	}
}

func TestNetworkInvalidName(t *testing.T) {
	mgr := initNetworkTestDB(t)
	for _, bad := range []string{"", "Home", "has space", "-lead", "trail-", "under_score", "dot.dot"} {
		if _, err := mgr.Create(sampleNetwork(bad)); err == nil {
			t.Errorf("expected error for name %q", bad)
		}
	}
}

func TestNetworkGetMissing(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Get("nope"); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("expected ErrNetworkNotFound, got %v", err)
	}
}

func TestNetworkRemove(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := mgr.Remove("office"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := mgr.Get("office"); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("expected not found after remove, got %v", err)
	}
	if err := mgr.Remove("office"); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("expected ErrNetworkNotFound, got %v", err)
	}
}

func TestNetworkDefaultProtected(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(sampleNetwork("home")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := mgr.Remove("home"); !errors.Is(err, ErrNetworkProtected) {
		t.Fatalf("expected ErrNetworkProtected, got %v", err)
	}
}

func TestNetworkSetEnabled(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := mgr.SetEnabled("office", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	got, err := mgr.Get("office")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Enabled {
		t.Error("expected disabled")
	}
	if err := mgr.SetEnabled("missing", true); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("expected ErrNetworkNotFound, got %v", err)
	}
}

func TestNetworkCount(t *testing.T) {
	mgr := initNetworkTestDB(t)
	n, err := mgr.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Fatalf("count = %d, want 0", n)
	}
	for _, name := range []string{"home", "office", "lab"} {
		if _, err := mgr.Create(sampleNetwork(name)); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	n, err = mgr.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 3 {
		t.Fatalf("count = %d, want 3", n)
	}
}

func TestNetworkPeers(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := mgr.AddPeer(&NetworkPeer{Network: "office", PublicKey: "KEY2", Name: "laptop", AllowedIP: "10.90.12.3/32"}); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	if _, err := mgr.AddPeer(&NetworkPeer{Network: "office", PublicKey: "KEY1", Name: "phone", AllowedIP: "10.90.12.2/32"}); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	// Duplicate key on same network rejected.
	if _, err := mgr.AddPeer(&NetworkPeer{Network: "office", PublicKey: "KEY1", AllowedIP: "10.90.12.9/32"}); !errors.Is(err, ErrDuplicateNetworkPeer) {
		t.Fatalf("expected ErrDuplicateNetworkPeer, got %v", err)
	}
	// Peer on unknown network rejected.
	if _, err := mgr.AddPeer(&NetworkPeer{Network: "ghost", PublicKey: "KEY9"}); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("expected ErrNetworkNotFound, got %v", err)
	}

	peers, err := mgr.ListPeers("office")
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 2 || peers[0].AllowedIP != "10.90.12.2/32" {
		t.Fatalf("unexpected peers order: %+v", peers)
	}

	if err := mgr.RemovePeer("office", "KEY1"); err != nil {
		t.Fatalf("RemovePeer: %v", err)
	}
	peers, err = mgr.ListPeers("office")
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 1 || peers[0].PublicKey != "KEY2" {
		t.Fatalf("unexpected peers after remove: %+v", peers)
	}
	if err := mgr.RemovePeer("office", "KEY1"); !errors.Is(err, ErrNetworkPeerNotFound) {
		t.Fatalf("expected ErrNetworkPeerNotFound, got %v", err)
	}
}

func TestNetworkPeerRolodexFlag(t *testing.T) {
	mgr := initNetworkTestDB(t)
	if _, err := mgr.Create(sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := mgr.AddPeer(&NetworkPeer{Network: "office", PublicKey: "ROL", AllowedIP: "10.90.12.2/32", Rolodex: true}); err != nil {
		t.Fatalf("AddPeer rolodex: %v", err)
	}
	if _, err := mgr.AddPeer(&NetworkPeer{Network: "office", PublicKey: "PLAIN", AllowedIP: "10.90.12.3/32"}); err != nil {
		t.Fatalf("AddPeer plain: %v", err)
	}

	peers, err := mgr.ListPeers("office")
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
	if _, err := mgr.Create(sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := mgr.AddPeer(&NetworkPeer{Network: "office", PublicKey: "KEY1", AllowedIP: "10.90.12.2/32"}); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	if err := mgr.Remove("office"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	// Re-create and confirm no orphaned peers survived the cascade.
	if _, err := mgr.Create(sampleNetwork("office")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	peers, err := mgr.ListPeers("office")
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected peers cascade-deleted, got %+v", peers)
	}
}

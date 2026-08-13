// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// End-to-end refusal of WireGuard enrollment on the home network, through the
// real router and a real SQLite network manager.
//
// The home network is DNS-only (DESIGN.md, "The home network always exists") and
// every account is a member of it, so accepting a peer there would make
// membership alone a way onto a tunnel -- and the row would describe a tunnel
// that does not exist. The refusal used to be incidental: the handler fell
// through to netip.ParsePrefix on the seeded row's empty Subnet, which failed
// and surfaced as a 500. This pins the deliberate rejection and pins out the
// server error, over the wire rather than at the handler.
func TestHomeNetworkRefusesPeerEnrollment(t *testing.T) {
	env := initConnectedPeersTest(t)

	_, err := env.client.AddNetworkPeer(t.Context(), systemcontroller.AddNetworkPeerRequest{
		Network:   account.DefaultNetworkName,
		Name:      "phone",
		PublicKey: "SE9NRUtFWS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0=",
	})
	if err == nil {
		t.Fatal("AddNetworkPeer on the home network succeeded, want a refusal")
	}
	// The client turns a non-200 into a problem detail carrying the status, so
	// a 500 leaking back through here is visible in the message.
	if strings.Contains(err.Error(), "500") {
		t.Fatalf("home enrollment failed with a server error rather than a refusal: %v", err)
	}
	if !strings.Contains(err.Error(), "DNS-only") {
		t.Fatalf("refusal did not say why: %v", err)
	}

	// Nothing may have been persisted by the refused enrollment.
	peers, lerr := env.nm.ListPeers(t.Context(), account.DefaultNetworkName)
	if lerr != nil {
		t.Fatalf("ListPeers: %v", lerr)
	}
	if len(peers) != 0 {
		t.Fatalf("home network has peers after a refused enrollment: %+v", peers)
	}
}

// The same request against a real overlay must still succeed, so the guard
// cannot be satisfied by refusing enrollment everywhere.
func TestRealNetworkStillAcceptsPeerEnrollment(t *testing.T) {
	env := initConnectedPeersTest(t)

	if _, err := env.nm.Create(t.Context(), &account.Network{
		Name: "lab", TLD: "lab",
		Subnet: "10.90.13.0/24", Address: "10.90.13.1/24",
		PublicKey: "PUB", PrivateKey: "PRIV", ListenPort: 51821, Enabled: true,
	}); err != nil {
		t.Fatalf("Create lab: %v", err)
	}

	if _, err := env.client.AddNetworkPeer(t.Context(), systemcontroller.AddNetworkPeerRequest{
		Network:   "lab",
		Name:      "phone",
		PublicKey: "SE9NRUtFWS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0=",
	}); err != nil {
		t.Fatalf("AddNetworkPeer on a real network: %v", err)
	}

	peers, err := env.nm.ListPeers(t.Context(), "lab")
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("lab has %d peers after enrollment, want 1", len(peers))
	}
}

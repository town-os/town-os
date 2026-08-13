// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
)

// The home network is DNS-only (DESIGN.md, "The home network always exists"):
// no interface, no overlay subnet, no keypair. Every account is a member of it,
// so if peers/add accepted it, membership alone would amount to a way onto a
// tunnel -- and the stored peer would describe a tunnel that does not exist and
// never will.
//
// It used to be refused only by accident: the handler fell through to
// netip.ParsePrefix on the seeded row's empty Subnet, which failed and surfaced
// as a 500. That reads as a broken box rather than a refusal, and it would stop
// refusing the moment anything wrote a subnet onto that row. These tests pin the
// deliberate rejection at both layers, and pin out the 500 specifically.

// homePeerKey decodes to "HOMEKEY" plus dash padding. WireGuard keys are base64
// of 32 bytes and the API validates that before the network is consulted, so
// this has to be a real key -- otherwise these tests would pass on the wrong
// refusal.
const homePeerKey = "SE9NRUtFWS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0="

func TestAddPeerOnHomeNetworkRejected(t *testing.T) {
	e := initWireGuardTestEnv(t)

	code, body := e.do(t, http.MethodPost, "networks/peers/add", e.adminToken,
		`{"network":"`+account.DefaultNetworkName+`","name":"phone","public_key":"`+homePeerKey+`"}`)

	// Checked before the 400 assertion because a 500 is the specific
	// regression: it means the guard is gone and the empty-subnet parse is
	// carrying the refusal again.
	if code == http.StatusInternalServerError {
		t.Fatalf("home peers/add returned a server error, not a refusal: %s", body)
	}
	if code != http.StatusBadRequest {
		t.Fatalf("peers/add on the home network = %d (%s), want 400", code, body)
	}
	if !strings.Contains(body, "DNS-only") {
		t.Fatalf("refusal did not say why: %s", body)
	}
}

// The refusal must not depend on the caller supplying a key: the keyless path
// generates a server-side keypair first, and that must not become the thing
// that decides whether the enrollment is refused.
func TestAddPeerOnHomeNetworkRejectedWithoutSuppliedKey(t *testing.T) {
	e := initWireGuardTestEnv(t)

	code, body := e.do(t, http.MethodPost, "networks/peers/add", e.adminToken,
		`{"network":"`+account.DefaultNetworkName+`","name":"laptop"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("keyless peers/add on the home network = %d (%s), want 400", code, body)
	}
}

// No peer row may survive the refusal.
func TestHomeNetworkHasNoPeersAfterRejection(t *testing.T) {
	e := initWireGuardTestEnv(t)

	if code, body := e.do(t, http.MethodPost, "networks/peers/add", e.adminToken,
		`{"network":"`+account.DefaultNetworkName+`","name":"phone","public_key":"`+homePeerKey+`"}`); code != http.StatusBadRequest {
		t.Fatalf("peers/add = %d (%s), want 400", code, body)
	}

	code, body := e.do(t, http.MethodGet, "networks/peers?network="+account.DefaultNetworkName, e.adminToken, "")
	if code != http.StatusOK {
		t.Fatalf("GET /networks/peers?network=home = %d (%s), want 200", code, body)
	}
	if strings.Contains(body, homePeerKey) {
		t.Fatalf("a peer was stored on the home network despite the refusal: %s", body)
	}
}

// The guard must be specific to the home network: a real overlay still enrolls.
// Without this, a blanket "reject everything" would satisfy every assertion
// above and break the feature.
func TestAddPeerOnRealNetworkStillWorks(t *testing.T) {
	e := initWireGuardTestEnv(t)

	code, body := e.do(t, http.MethodPost, "networks/peers/add", e.adminToken,
		`{"network":"lab","name":"phone","public_key":"`+homePeerKey+`"}`)
	if code != http.StatusOK {
		t.Fatalf("peers/add on a real network = %d (%s), want 200", code, body)
	}
}

// The manager refuses on its own, so a caller holding a NetworkManager cannot
// write the row by going around the API.
func TestNetworkManagerAddPeerRefusesHomeNetwork(t *testing.T) {
	nm := account.InitMockNetworkManager()

	if _, err := nm.AddPeer(t.Context(), &account.NetworkPeer{
		Network:   account.DefaultNetworkName,
		PublicKey: homePeerKey,
		Name:      "phone",
		AllowedIP: "10.64.0.2/32",
	}); !errors.Is(err, account.ErrNetworkDNSOnly) {
		t.Fatalf("AddPeer on the home network = %v, want ErrNetworkDNSOnly", err)
	}
}

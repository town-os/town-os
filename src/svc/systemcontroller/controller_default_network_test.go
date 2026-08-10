// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"net/http"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/rolodex"
	"github.com/labstack/echo/v5"
)

// The home network exists before the controller does anything.
//
// The row is seeded by account.InitNetworkManager, so the invariant is really
// the account layer's; this pins the controller's half of it -- that boot
// reconcile leaves it alone rather than creating a second one or failing on the
// duplicate.
func TestEnsureDefaultNetworkFindsTheSeededRow(t *testing.T) {
	nm := account.InitMockNetworkManager()
	s := newDefaultNetworkHandler(nm, "home")

	if err := s.ensureDefaultNetwork(t.Context()); err != nil {
		t.Fatalf("ensureDefaultNetwork: %v", err)
	}

	nets, err := nm.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(nets) != 1 || nets[0].Name != account.DefaultNetworkName {
		t.Fatalf("networks = %+v, want just the home network", nets)
	}
}

// The seed cannot know the TLD -- dns_tld is a setting, and the account package
// has no settings manager -- so the row arrives carrying the bare default and
// boot reconcile repairs it. Without this, a box whose operator had set
// dns_tld=lan would hand rolodex.EnsureNetworkScope "home." and the home scope
// would own the wrong zone.
func TestEnsureDefaultNetworkReconcilesTheTLD(t *testing.T) {
	nm := account.InitMockNetworkManager()
	s := newDefaultNetworkHandler(nm, "lan")

	if err := s.ensureDefaultNetwork(t.Context()); err != nil {
		t.Fatalf("ensureDefaultNetwork: %v", err)
	}

	home, err := nm.Get(t.Context(), account.DefaultNetworkName)
	if err != nil {
		t.Fatalf("Get home: %v", err)
	}
	if home.TLD != "lan" {
		t.Errorf("home TLD = %q, want lan", home.TLD)
	}
}

// ... and it does not write when the two already agree, so a boot that changed
// nothing leaves the row's updated_at alone.
func TestEnsureDefaultNetworkIsQuietWhenTheTLDMatches(t *testing.T) {
	nm := account.InitMockNetworkManager()
	s := newDefaultNetworkHandler(nm, "home")

	if err := s.ensureDefaultNetwork(t.Context()); err != nil {
		t.Fatalf("ensureDefaultNetwork: %v", err)
	}

	for _, call := range nm.GetCalls() {
		if call.Method == "SetTLD" {
			t.Errorf("rewrote a TLD that already matched: %+v", nm.GetCalls())
			break
		}
	}
}

// Changing the TLD at runtime moves the home network with it. `POST /dns/tld`
// writes the setting and re-registers every package; leaving the network row on
// the old TLD would make the two disagree until the next boot repaired it, and
// the row is what applyNetworkTransport hands rolodex.
func TestSetDNSTLDRepointsTheHomeNetwork(t *testing.T) {
	nm := account.InitMockNetworkManager()
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}
	sb := &serverBase{ServerConfig: ServerConfig{
		NetworkMgr:    nm,
		SettingsMgr:   settings,
		RolodexClient: &rolodex.MockClient{},
	}}
	s := &SystemControllerHandlers{Controller: sb, ctx: context.Background()}

	e := echo.New()
	c, rec := postJSONContext(e, `{"tld":"lan"}`)
	if err := s.setDNSTLD(c); err != nil {
		t.Fatalf("setDNSTLD: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	home, err := nm.Get(t.Context(), account.DefaultNetworkName)
	if err != nil {
		t.Fatalf("Get home: %v", err)
	}
	if home.TLD != "lan" {
		t.Errorf("home TLD = %q after the change, want lan", home.TLD)
	}
}

// newDefaultNetworkHandler builds a handler over a network manager and a
// dns_tld setting, which is all ensureDefaultNetwork consults.
func newDefaultNetworkHandler(nm account.NetworkManager, tld string) *SystemControllerHandlers {
	sb := &serverBase{ServerConfig: ServerConfig{
		NetworkMgr:  nm,
		SettingsMgr: &mockSettingsManager{values: map[string]string{"dns_tld": tld}},
	}}
	return &SystemControllerHandlers{Controller: sb, ctx: context.Background()}
}

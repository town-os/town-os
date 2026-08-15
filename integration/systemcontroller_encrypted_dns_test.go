// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
)

const encryptedDNSLANIP = "192.168.122.62"

// encryptedDNSHarness is a controller wired to a REAL rolodex, with a real CA
// on a real btrfs base, which is what the encrypted-DNS publishers need: the
// DoT/DoQ leaf is a file on disk and its DANE pin is derived from that file.
type encryptedDNSHarness struct {
	client    *systemcontroller.SystemdClient
	rolo      rolodex.Client
	dnsPort   string
	settings  account.SettingsManager
	btrfsBase string
}

func initEncryptedDNSTest(t *testing.T, tld string) *encryptedDNSHarness {
	t.Helper()

	realClient, dnsPort := initRolodexRealTest(t)

	btrfsBase := t.TempDir()
	ca, err := townostls.EnsureCA(filepath.Join(btrfsBase, systemcontroller.TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}

	settings := &mockSettingsManager{values: map[string]string{"dns_tld": tld}}
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       storage.InitBtrFSMock(),
		Systemd:       systemd.InitMockManager(),
		RolodexClient: realClient,
		IngressClient: &ingress.MockClient{},
		SettingsMgr:   settings,
		PagesMgr:      account.InitMockPagesManager(),
		TLSCA:         ca,
		BtrfsBasePath: btrfsBase,
	})
	t.Cleanup(func() { ts.Server.Close() })
	ts.SetInternalIP(encryptedDNSLANIP)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	return &encryptedDNSHarness{
		client:    c,
		rolo:      realClient,
		dnsPort:   dnsPort,
		settings:  settings,
		btrfsBase: btrfsBase,
	}
}

// designationValues returns the SVCB values a real rolodex is holding at the
// DDR name.
func designationValues(t *testing.T, ctx context.Context, rc rolodex.Client) []string {
	t.Helper()
	records, err := rc.ListRecords(ctx, nil)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	var out []string
	for _, r := range records {
		if r.Name == systemcontroller.DDRDesignationName && r.RecordType == upstream.RecordTypeSVCB {
			out = append(out, r.Value)
		}
	}
	return out
}

// TestDDRDesignationRoundTripsThroughRealRolodex proves the designation a DDR
// client asks for actually lands in a live resolver.
//
// The unit tests assert the SVCB values against a mock, which cannot fail the
// way this can: SVCB is a record type this repo could not even NAME until the
// rolodex-dns pin moved past the commit that added it, and a server that does
// not know the type rejects the record. That rejection is best-effort and
// logged at debug — so the designation silently not existing is exactly what a
// box with a stale rolodex looks like, and no test above this level notices.
//
// It also pins the property that makes DDR safe: the designation lives in
// `arpa.`, which rolodex refuses to resolve upstream, so the only resolver that
// can answer for it is the one being asked.
func TestDDRDesignationRoundTripsThroughRealRolodex(t *testing.T) {
	t.Parallel()

	ctx := testContext(t, 3*time.Minute)
	h := initEncryptedDNSTest(t, "home")

	if err := systemcontroller.RebuildDNS(ctx, systemcontroller.ReconcileDNSConfig{
		Client:        h.rolo,
		SettingsMgr:   h.settings,
		InternalIP:    encryptedDNSLANIP,
		BtrfsBasePath: h.btrfsBase,
	}); err != nil {
		t.Fatalf("RebuildDNS: %v", err)
	}

	values := designationValues(t, ctx, h.rolo)
	if len(values) != 3 {
		t.Fatalf("designation values at %s = %v, want one per transport (DoH, DoT, DoQ)",
			systemcontroller.DDRDesignationName, values)
	}

	// Preference order is part of the record: a client walks the list and stops
	// at the first endpoint it can reach, and :443 survives the DPI that filters
	// :853.
	for i, want := range []string{"alpn=h2 port=443", "alpn=dot port=853", "alpn=doq port=853"} {
		if !strings.Contains(values[i], want) {
			t.Errorf("designation %d = %q, want it to carry %q", i, values[i], want)
		}
		if !strings.Contains(values[i], "dns.home.") {
			t.Errorf("designation %d = %q, want it to name dns.home.", i, values[i])
		}
	}

	// A second rebuild must leave exactly one designation. The DDR name is in no
	// zone this box owns, so RebuildDNS's teardown never reaches it — the
	// remove-then-write in publishDDRDesignation is the only thing keeping
	// copies from stacking up on every boot.
	if err := systemcontroller.RebuildDNS(ctx, systemcontroller.ReconcileDNSConfig{
		Client:        h.rolo,
		SettingsMgr:   h.settings,
		InternalIP:    encryptedDNSLANIP,
		BtrfsBasePath: h.btrfsBase,
	}); err != nil {
		t.Fatalf("RebuildDNS second pass: %v", err)
	}
	if again := designationValues(t, ctx, h.rolo); len(again) != 3 {
		t.Errorf("designation values after a second rebuild = %v, want still 3", again)
	}
}

// TestTLDChangeMovesTheEncryptedEndpointsOnRealDNS drives the real
// `POST /dns/tld` against a real rolodex and asks the live resolver what the
// box now advertises.
//
// The failure it guards is not a wrong record but a stale one: before this,
// changing the TLD moved the zone and left encrypted DNS entirely behind — the
// designation still naming `dns.<old>`, the DoT/DoQ certificate still carrying
// the old SANs, and the DANE pins still at the old owner. A client that checks
// DANE and finds no pin at the endpoint it reached REFUSES it, so this is the
// difference between encrypted DNS working and encrypted DNS being off.
func TestTLDChangeMovesTheEncryptedEndpointsOnRealDNS(t *testing.T) {
	t.Parallel()

	ctx := testContext(t, 3*time.Minute)
	h := initEncryptedDNSTest(t, "home")

	if err := h.client.SetupDNS(ctx); err != nil {
		t.Fatalf("SetupDNS: %v", err)
	}
	if err := h.client.SetDNSTLD(ctx, "fart"); err != nil {
		t.Fatalf("SetDNSTLD: %v", err)
	}

	// The name a client dials resolves, on the live resolver.
	addrs := resolveEventually(ctx, t, lanResolver(h.dnsPort), "dns.fart.")
	if !slices.Contains(addrs, encryptedDNSLANIP) {
		t.Errorf("dns.fart. resolved to %v, want the box's address %s", addrs, encryptedDNSLANIP)
	}

	// The designation advertises it, and nothing advertises the old name.
	for _, value := range designationValues(t, ctx, h.rolo) {
		if !strings.Contains(value, "dns.fart.") {
			t.Errorf("designation %q does not name the new TLD", value)
		}
		if strings.Contains(value, "dns.home.") {
			t.Errorf("designation %q still advertises the old TLD", value)
		}
	}

	// And the pin a DANE client checks moved with it.
	pinned := globalRecordsUnder(t, ctx, h.rolo, "dns.fart.")
	var sawTLSA bool
	for _, r := range pinned {
		if r.RecordType == upstream.RecordTypeTLSA {
			sawTLSA = true
		}
	}
	if !sawTLSA {
		t.Errorf("no DANE pin under dns.fart. after the rename: %s", summarizeRecords(pinned))
	}
	if left := globalRecordsUnder(t, ctx, h.rolo, "dns.home."); len(left) > 0 {
		t.Errorf("the old encrypted-DNS name survived the rename: %s", summarizeRecords(left))
	}
}

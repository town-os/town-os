// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"slices"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

const (
	dohRecordLANIP  = "192.168.122.61"
	dohRecordLANIP6 = "2001:db8:beef::61"
)

// TestDohEndpointResolvesOnRealDNS is the half of the DoH endpoint that
// TestDohResolvesThroughTheIngress deliberately skips: that test dials the
// ingress on a port it already knows, so it proves the transport and says
// nothing about how a client would ever find it.
//
// A real client has only the name. dns.<tld> has no package, no page and no
// object-storage partition behind it — only an ingress vhost — so the address
// records for it are published by nothing except the DNS rebuild. When they are
// missing, `https://dns.home/dns-query` fails at resolution, before TLS and
// before the ingress is reached, and the vhost and its leaf sit there serving a
// name nothing can look up.
//
// Against a REAL rolodex and its live resolver, because the mock cannot fail the
// way this does.
func TestDohEndpointResolvesOnRealDNS(t *testing.T) {
	t.Parallel()

	ctx := testContext(t, 3*time.Minute)
	realClient, dnsPort := initRolodexRealTest(t)

	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}
	cfg := systemcontroller.ReconcileDNSConfig{
		Client:       realClient,
		SettingsMgr:  settings,
		InternalIP:   dohRecordLANIP,
		InternalIPv6: dohRecordLANIP6,
	}

	// The boot rebuild, which is where every zone-wide name comes from.
	if err := systemcontroller.RebuildDNS(ctx, cfg); err != nil {
		t.Fatalf("RebuildDNS: %v", err)
	}

	const host = "dns.home."
	addrs := resolveEventually(ctx, t, lanResolver(dnsPort), host)
	if !slices.Contains(addrs, dohRecordLANIP) {
		t.Errorf("%s resolved to %v, want it to include the box's LAN address %s", host, addrs, dohRecordLANIP)
	}
	if !slices.Contains(addrs, dohRecordLANIP6) {
		t.Errorf("%s resolved to %v, want it to include the box's IPv6 %s — the ingress leaf carries both, so a v6 client has to find the box over v6", host, addrs, dohRecordLANIP6)
	}

	// The hourly pass deletes every A/AAAA in the zone it cannot account for. A
	// name published only by the rebuild survives the boot and vanishes an hour
	// later, which is the same outage as never publishing it and much harder to
	// see: the box works until it doesn't.
	if err := systemcontroller.ReconcileDNS(ctx, cfg); err != nil {
		t.Fatalf("ReconcileDNS: %v", err)
	}
	if left := globalRecordsUnder(t, ctx, realClient, host); len(left) == 0 {
		t.Fatal("the hourly reconcile deleted dns.home as an orphan; the DoH name is missing from its desired set")
	}
	if addrs := resolveEventually(ctx, t, lanResolver(dnsPort), host); !slices.Contains(addrs, dohRecordLANIP) {
		t.Errorf("%s resolved to %v after the hourly reconcile, want it to still include %s", host, addrs, dohRecordLANIP)
	}
}

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// localForwardersEnv is the wiring one of these tests runs against.
type localForwardersEnv struct {
	client  *systemcontroller.SystemdClient
	systemd *systemd.MockManager
	rolodex *rolodex.Manager
	rolCli  *rolodex.MockClient
}

// initLocalForwardersTest wires a real rolodex.Manager behind the real HTTP
// settings API, with a fake rolodex server so a test can see what the handler
// programs, and a mock systemd so it can see that it asks for no restart.
//
// Discovery is pointed at fixtures rather than the machine's own resolv.conf
// and routing table, and the probe is stubbed: the addresses a test asserts on
// must be a property of the test, not of whichever resolver the host running
// `make test-full` happens to have — and on a host whose /etc/resolv.conf holds
// only a loopback stub, real discovery would find nothing and the assertion
// would be about the wrong thing.
//
// The probe stub matters twice over. Left to the real one these tests would
// send DNS queries to the host's own gateway, which is a suite that reaches the
// network, and every candidate would be judged by whether the machine running
// CI can resolve through it.
//
// working is the set of candidate addresses the stubbed probe accepts.
func initLocalForwardersTest(t *testing.T, working ...string) localForwardersEnv {
	t.Helper()
	return initLocalForwardersTestMode(t, rolodex.ResolutionModeAuto, working...)
}

// initLocalForwardersTestMode is the same wiring with the resolution mode named
// rather than defaulted, because the mode now decides whether discovery happens
// at all: `auto` fills its own local tier with dns_local_forwarders never set,
// while `forward` — where that tier is the only upstream and takes every query
// always — keeps waiting to be asked. A test about the FLAG has to say
// `forward`, or it is asserting the default mode's behavior instead.
//
// The mode is set in three places because three things read it: the settings DB
// the handler consults, the manager that resolves the list, and the fake server
// standing in for a running rolodex.
func initLocalForwardersTestMode(t *testing.T, mode string, working ...string) localForwardersEnv {
	t.Helper()

	sd := systemd.InitMockManager()
	settings := &mockSettingsManager{values: map[string]string{
		"dns_tld":              "home",
		"dns_resolution_mode":  mode,
		"dns_local_forwarders": "false",
	}}

	dataDir := rolodexTempDir(t, "local-forwarders-*")
	resolvPath := filepath.Join(dataDir, "resolv.conf")
	if err := os.WriteFile(resolvPath, []byte("nameserver 192.168.77.1\n"), 0600); err != nil {
		t.Fatalf("write resolv.conf fixture: %v", err)
	}
	// A default route via 192.168.122.1, in the native-endian hex form
	// /proc/net/route prints. Present in every one of these tests because a
	// real box always has one, and discovery reading it is the whole point.
	routePath := filepath.Join(dataDir, "route")
	routeTable := "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
		"ens3\t00000000\t017AA8C0\t0003\t0\t0\t1024\t00000000\t0\t0\t0\n"
	if err := os.WriteFile(routePath, []byte(routeTable), 0600); err != nil {
		t.Fatalf("write route table fixture: %v", err)
	}

	if len(working) == 0 {
		working = []string{"192.168.77.1:53", "192.168.122.1:53"}
	}
	rolMgr := rolodex.NewManager(rolodex.Config{
		Systemd:         sd,
		DataDir:         dataDir,
		Image:           rolodexTestImage(),
		ResolutionMode:  mode,
		UnixSocketPath:  filepath.Join(dataDir, "rolodex.sock"),
		ResolvConfPaths: []string{resolvPath},
		RouteTablePath:  routePath,
		ForwarderProbe: func(_ context.Context, addr string) bool {
			return slices.Contains(working, addr)
		},
	})

	// No config file: rolodex.yml holds the install image's binds and metrics
	// listener, and the forwarder list is programmed into the running server.
	rolClient := &rolodex.MockClient{ResolutionMode: mode}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       storage.InitBtrFSMock(),
		Systemd:       sd,
		Rolodex:       rolMgr,
		RolodexClient: rolClient,
		SettingsMgr:   settings,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return localForwardersEnv{client: c, systemd: sd, rolodex: rolMgr, rolCli: rolClient}
}

// TestIntegrationSetDNSLocalForwardersProgramsRolodexWithoutRestarting drives
// the real settings endpoint and asserts the operator-visible contract:
// switching to the local resolvers must replace the forwarder list on the
// RUNNING rolodex — so a household stuck behind a network that blocks external
// DNS is resolving again NOW rather than after a reboot they have no reason to
// think would help — and must not restart it to do so.
//
// Pinned to `forward` deliberately, because that is the mode where the FLAG is
// what decides. Under `auto` the tier is discovered either way, so the toggle
// back to false would change nothing and the second half of this test would be
// asserting the default mode's behavior while looking like it asserted the
// flag's. That is intended rather than a regression, and it is stated directly
// by TestIntegrationAutoKeepsDiscoveringWithTheFlagOff below. The on-direction
// under auto is covered by the drop-what-cannot-resolve pair.
func TestIntegrationSetDNSLocalForwardersProgramsRolodexWithoutRestarting(t *testing.T) {
	t.Parallel()

	env := initLocalForwardersTestMode(t, rolodex.ResolutionModeForward)
	c, sd, rolMgr, rolCli := env.client, env.systemd, env.rolodex, env.rolCli
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_local_forwarders", "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	// Both discovered candidates answered, so both are programmed: the
	// nameserver the network handed out, then the default gateway.
	assertProgrammedForwarders(t, rolCli, []string{"192.168.77.1:53", "192.168.122.1:53"})

	if rolodexRestartRequested(sd, rolMgr.UnitName()) {
		t.Fatalf("rolodex was restarted to apply a runtime setting; systemd calls: %+v", sd.Calls)
	}

	// And back again — an operator who has left that network must be able to
	// stop handing it every name the household looks up.
	if err := c.SetSetting(ctx, "dns_local_forwarders", "false"); err != nil {
		t.Fatalf("SetSetting back to false: %v", err)
	}
	assertProgrammedForwarders(t, rolCli, rolodex.DefaultForwarders)
}

// TestIntegrationAutoKeepsDiscoveringWithTheFlagOff states, at the API rather
// than in the manager, the one case where dns_local_forwarders is inert: under
// `auto`, with no explicit dns_forwarders list, turning the flag OFF does not
// put the public defaults back.
//
// It cannot, and that is the point. The local tier in `auto` is reached only
// after the roots AND the encrypted upstreams have both failed, so the choice
// in front of it is never "the roots or the local resolver" — it is "the local
// resolver or SERVFAIL". Honoring "off" here would mean programming
// DefaultForwarders into a tier that exists only because everything else already
// failed, on precisely the networks that drop those addresses. An operator who
// wants no local resolver in the path asks for `recursive`.
func TestIntegrationAutoKeepsDiscoveringWithTheFlagOff(t *testing.T) {
	t.Parallel()

	env := initLocalForwardersTest(t)
	c, rolCli := env.client, env.rolCli
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_local_forwarders", "false"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	assertProgrammedForwarders(t, rolCli, []string{"192.168.77.1:53", "192.168.122.1:53"})
}

// The mode decides whether the local tier is consulted; the forwarder list only
// decides what is in it. Turning one on must not move the other, or an operator
// fixing DNS on a filtered network would silently give up root recursion
// everywhere else.
func TestIntegrationSetDNSLocalForwardersLeavesTheResolutionModeAlone(t *testing.T) {
	t.Parallel()

	env := initLocalForwardersTest(t)
	c, rolCli := env.client, env.rolCli
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_local_forwarders", "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	if rolCli.Called("SetResolutionMode") {
		t.Fatal("enabling local forwarders changed the resolution mode")
	}
}

// A value that will not parse is read as off everywhere it is consumed, so
// storing one would look accepted and change nothing. Reject it at the API.
func TestIntegrationSetDNSLocalForwardersRejectsGarbage(t *testing.T) {
	t.Parallel()

	env := initLocalForwardersTest(t)
	c, rolCli := env.client, env.rolCli
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_local_forwarders", "sometimes"); err == nil {
		t.Fatal("expected an unparseable local-forwarders value to be rejected")
	}

	// No half-applied change: nothing was pushed to the running server.
	if rolCli.Called("SetForwarders") {
		t.Fatal("an unparseable value still reached rolodex")
	}
}

// GET /dns/status is what the settings screen renders, and it must report the
// list that is actually programmed rather than echoing the setting back. The two
// disagree in both directions now, and this is the direction that appeared with
// auto managing its own tier: the switch reads as OFF while the effective list
// is the one discovery found, because in `auto` the flag is not what put it
// there. A status endpoint that inferred the list from the setting would show
// the public defaults on a box that is not using them.
func TestIntegrationDNSStatusReportsTheEffectiveForwarders(t *testing.T) {
	t.Parallel()

	c := initLocalForwardersTest(t).client
	ctx := context.Background()

	discovered := "192.168.77.1:53,192.168.122.1:53"

	status, err := c.DNSStatus(ctx)
	if err != nil {
		t.Fatalf("DNSStatus: %v", err)
	}
	if status.LocalForwarders {
		t.Fatal("DNSStatus reports local forwarders on before anything enabled them")
	}
	if strings.Join(status.Forwarders, ",") != discovered {
		t.Fatalf("DNSStatus forwarders = %v, want the discovered %v with the flag still off", status.Forwarders, discovered)
	}

	if err := c.SetSetting(ctx, "dns_local_forwarders", "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	status, err = c.DNSStatus(ctx)
	if err != nil {
		t.Fatalf("DNSStatus after enable: %v", err)
	}
	if !status.LocalForwarders {
		t.Fatal("DNSStatus still reports local forwarders off after enabling them")
	}
	if strings.Join(status.Forwarders, ",") != discovered {
		t.Fatalf("DNSStatus forwarders = %v, want %v", status.Forwarders, discovered)
	}
}

// TestIntegrationDNSStatusReportsTheDefaultsWhenNothingAnswers is the other
// direction of the same disagreement, and the older one: the switch reads as ON
// and the effective list is still the public defaults, because every candidate
// discovery turned up failed its probe. That is the case where an operator would
// otherwise be looking at a screen claiming a change that did not happen.
func TestIntegrationDNSStatusReportsTheDefaultsWhenNothingAnswers(t *testing.T) {
	t.Parallel()

	c := initLocalForwardersTest(t, "203.0.113.1:53").client
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_local_forwarders", "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	status, err := c.DNSStatus(ctx)
	if err != nil {
		t.Fatalf("DNSStatus: %v", err)
	}
	if !status.LocalForwarders {
		t.Fatal("DNSStatus reports local forwarders off after enabling them")
	}
	if strings.Join(status.Forwarders, ",") != strings.Join(rolodex.DefaultForwarders, ",") {
		t.Fatalf("DNSStatus forwarders = %v, want %v when nothing answered", status.Forwarders, rolodex.DefaultForwarders)
	}
}

// TestIntegrationLocalForwardersDropWhatCannotResolve is the berkeley network
// end to end, through the real settings endpoint: the resolver the network
// advertised does not answer, the default gateway does, and only the gateway is
// programmed. Before the probe, both went in — and the dead one sat in the
// local tier of "auto", which is reached only after the roots and the encrypted
// upstreams have failed, charging every query that got that far a full
// per-forwarder timeout on its way to SERVFAIL.
func TestIntegrationLocalForwardersDropWhatCannotResolve(t *testing.T) {
	t.Parallel()

	env := initLocalForwardersTest(t, "192.168.122.1:53")
	c, rolCli := env.client, env.rolCli
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_local_forwarders", "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	assertProgrammedForwarders(t, rolCli, []string{"192.168.122.1:53"})
}

// The control for the test above, and the reason the probe cannot simply prefer
// the gateway: on a network where nothing is filtered the advertised resolver
// answers too, and dropping it would throw away the resolver the network
// actually asked this box to use. Only the gateway answering is a property of
// the filtered case, not a rule.
func TestIntegrationLocalForwardersKeepTheAdvertisedResolverWhenItWorks(t *testing.T) {
	t.Parallel()

	env := initLocalForwardersTest(t, "192.168.77.1:53")
	c, rolCli := env.client, env.rolCli
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_local_forwarders", "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	assertProgrammedForwarders(t, rolCli, []string{"192.168.77.1:53"})
}

// Nothing answering is the "keep what was already configured" case, and it has
// to survive the probe: an empty list pushed to rolodex would delete the local
// tier outright, which is strictly worse than the public defaults it replaced.
func TestIntegrationLocalForwardersKeepTheDefaultsWhenNothingAnswers(t *testing.T) {
	t.Parallel()

	env := initLocalForwardersTest(t, "203.0.113.1:53")
	c, rolCli := env.client, env.rolCli
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_local_forwarders", "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	assertProgrammedForwarders(t, rolCli, rolodex.DefaultForwarders)
}

func assertProgrammedForwarders(t *testing.T, client *rolodex.MockClient, want []string) {
	t.Helper()

	if got := strings.Join(client.Forwarders, ","); got != strings.Join(want, ",") {
		t.Fatalf("rolodex is holding forwarders %v, want %v", client.Forwarders, want)
	}
}

// TestIntegrationSetDNSForwardersProgramsEncryptedUpstreams is the end of the
// thread this feature exists for: an operator can now put a DoH/DoT/DoQ
// upstream into the forwarder list and have it reach the RUNNING resolver.
//
// Before forwarders were typed this was impossible by construction. rolodex
// reads `secure_upstreams:` once at startup from a file the install image owns
// and exposes no setter for it, so on a network where only the encrypted
// transports work, the one tier that could answer was also the one tier nothing
// could reconfigure without restarting the box's only resolver.
func TestIntegrationSetDNSForwardersProgramsEncryptedUpstreams(t *testing.T) {
	t.Parallel()

	env := initLocalForwardersTest(t)
	c, rolCli := env.client, env.rolCli
	ctx := context.Background()

	want := "https://cloudflare-dns.com@1.1.1.1/dns-query,tls://dns.google@8.8.8.8:853"
	if err := c.SetSetting(ctx, "dns_forwarders", want); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	assertProgrammedForwarders(t, rolCli, []string{
		"https://cloudflare-dns.com@1.1.1.1/dns-query",
		"tls://dns.google@8.8.8.8:853",
	})
}

// Clearing the setting restores the defaults rather than leaving the resolver
// with no upstreams at all — an empty forwarder list would delete the tier.
//
// Run on a network where nothing answers, so the fallback under test is the one
// named: with the explicit list gone, `auto` reaches for discovery first (see
// TestIntegrationClearingDNSForwardersHandsAutoBackToDiscovery) and only lands
// on DefaultForwarders once that comes back empty. Left on the answering
// fixture this test would have asserted the defaults against a box that had
// something better, which is how it failed rather than how it should pass.
func TestIntegrationClearingDNSForwardersRestoresTheDefaults(t *testing.T) {
	t.Parallel()

	env := initLocalForwardersTest(t, "203.0.113.1:53")
	c, rolCli := env.client, env.rolCli
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_forwarders", "quic://dns.adguard.com@94.140.14.14:853"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	assertProgrammedForwarders(t, rolCli, []string{"quic://dns.adguard.com@94.140.14.14:853"})

	if err := c.SetSetting(ctx, "dns_forwarders", ""); err != nil {
		t.Fatalf("SetSetting to empty: %v", err)
	}
	assertProgrammedForwarders(t, rolCli, rolodex.DefaultForwarders)
}

// TestIntegrationClearingDNSForwardersHandsAutoBackToDiscovery is the other
// half, and the ordering the change turns on: an explicit dns_forwarders list
// outranks discovery, so setting one takes the tier back from it — and clearing
// that list hands it straight back rather than falling to the public defaults.
// Both steps go through the real endpoint, because the ordering only exists
// where the handler resolves the list.
func TestIntegrationClearingDNSForwardersHandsAutoBackToDiscovery(t *testing.T) {
	t.Parallel()

	env := initLocalForwardersTest(t)
	c, rolCli := env.client, env.rolCli
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_forwarders", "10.0.0.1:53"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	assertProgrammedForwarders(t, rolCli, []string{"10.0.0.1:53"})

	if err := c.SetSetting(ctx, "dns_forwarders", ""); err != nil {
		t.Fatalf("SetSetting to empty: %v", err)
	}
	assertProgrammedForwarders(t, rolCli, []string{"192.168.77.1:53", "192.168.122.1:53"})
}

// A spec that will not parse must be refused at the API, with nothing pushed.
// Accepting it would store a value that silently does not apply — and on the
// encrypted transports the difference between a validated name and a typo is
// the difference between an authenticated upstream and none.
func TestIntegrationSetDNSForwardersRejectsGarbage(t *testing.T) {
	t.Parallel()

	env := initLocalForwardersTest(t)
	c, rolCli := env.client, env.rolCli
	ctx := context.Background()

	if err := c.SetSetting(ctx, "dns_forwarders", "8.8.8.8:53,gopher://8.8.8.8:53"); err == nil {
		t.Fatal("expected an unparseable forwarder spec to be rejected")
	}

	if rolCli.Called("SetForwarders") {
		t.Fatal("a rejected forwarder list still reached rolodex")
	}
}

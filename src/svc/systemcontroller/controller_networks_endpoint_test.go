package systemcontroller

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"github.com/labstack/echo/v5"
)

// ctxWithHost builds an echo context for a request that arrived at host.
func ctxWithHost(t *testing.T, host string) *echo.Context {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/networks/peers/add", nil)
	req.Host = host
	return e.NewContext(req, httptest.NewRecorder())
}

// The Endpoint we hand a peer must be the address that peer just reached us on.
// Anything else is a guess, and a guessed endpoint fails silently: the peer's
// handshakes go nowhere, which is indistinguishable on the wire from a tunnel
// that is simply down.
func TestPeerEndpointHostUsesDialedAddress(t *testing.T) {
	for _, tt := range []struct {
		name string
		host string
		want string
	}{
		{"lan address with port", "192.168.8.174:5309", "192.168.8.174"},
		{"lan address without port", "192.168.8.174", "192.168.8.174"},
		{"public address", "203.0.113.9:5309", "203.0.113.9"},
		{"hostname", "box.example.com:5309", "box.example.com"},
		{"ipv6 literal", "[2001:db8::1]:5309", "2001:db8::1"},
		// Reached from the box itself: there is no remotely-dialable address to
		// advertise, so advertise none rather than one that cannot work.
		{"loopback", "127.0.0.1:5309", ""},
		{"ipv6 loopback", "[::1]:5309", ""},
		{"unspecified", "0.0.0.0:5309", ""},
		{"empty", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := peerEndpointHost(ctxWithHost(t, tt.host)); got != tt.want {
				t.Errorf("peerEndpointHost(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func testNetwork() *account.Network {
	return &account.Network{
		Name:       "fart",
		TLD:        "fart",
		Subnet:     "10.85.219.0/24",
		Address:    "10.85.219.1/24",
		PublicKey:  "SRVPUB",
		ListenPort: 52371,
		Enabled:    true,
	}
}

// The rendered config carries the dialed address, the network's UDP listen port,
// and the overlay resolver — not the box's own idea of where it lives.
func TestRenderPeerDeviceConfigEndpointFromDialedAddress(t *testing.T) {
	s := newNetworksHandler(account.InitMockNetworkManager())
	n := testNetwork()
	subnet := netip.MustParsePrefix(n.Subnet)
	peerAddr := netip.MustParseAddr("10.85.219.150")

	got := s.renderPeerDeviceConfig(n, subnet, peerAddr, "PRIV", "192.168.8.174")

	for _, want := range []string{
		"PrivateKey = PRIV",
		"Address = 10.85.219.150/32",
		"DNS = 10.85.219.1",
		"PublicKey = SRVPUB",
		"Endpoint = 192.168.8.174:52371",
		"AllowedIPs = 10.85.219.0/24",
		"PersistentKeepalive = 25",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config missing %q:\n%s", want, got)
		}
	}
}

// An IPv6 endpoint must be bracketed or wg-quick cannot parse the port.
func TestRenderPeerDeviceConfigBracketsIPv6Endpoint(t *testing.T) {
	s := newNetworksHandler(account.InitMockNetworkManager())
	n := testNetwork()
	got := s.renderPeerDeviceConfig(n, netip.MustParsePrefix(n.Subnet), netip.MustParseAddr("10.85.219.150"), "PRIV", "2001:db8::1")

	if !strings.Contains(got, "Endpoint = [2001:db8::1]:52371") {
		t.Errorf("ipv6 endpoint not bracketed:\n%s", got)
	}
}

// With no dialable address, omit Endpoint entirely. An absent Endpoint is a
// config the operator can complete; a wrong one just fails.
func TestRenderPeerDeviceConfigOmitsUnknownEndpoint(t *testing.T) {
	s := newNetworksHandler(account.InitMockNetworkManager())
	n := testNetwork()
	got := s.renderPeerDeviceConfig(n, netip.MustParsePrefix(n.Subnet), netip.MustParseAddr("10.85.219.150"), "PRIV", "")

	if strings.Contains(got, "Endpoint") {
		t.Errorf("expected no Endpoint line:\n%s", got)
	}
	if !strings.Contains(got, "PublicKey = SRVPUB") {
		t.Errorf("rest of the config should still render:\n%s", got)
	}
}

// writeRouteTable installs a fake /proc/net/route for the duration of the test.
func writeRouteTable(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "route")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write route table: %v", err)
	}
	orig := procNetRoute
	procNetRoute = path
	t.Cleanup(func() { procNetRoute = orig })
}

// The kernel writes destinations and masks as little-endian hex words; a route
// is only usable when RTF_UP is set. This is the table shape a live WireGuard
// overlay produces (10.85.219.0/24 dev towne30c).
func TestRouteCovers(t *testing.T) {
	const header = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n"
	writeRouteTable(t, header+
		// 10.85.219.0/24 dev towne30c — the live overlay route.
		"towne30c\t00DB550A\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n"+
		// 192.168.122.0/24 dev enp0s1 — the LAN route, covering nothing in the
		// overlay.
		"enp0s1\t007AA8C0\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n"+
		// 10.85.220.0/24, but the route is DOWN (RTF_UP clear) and so cannot
		// carry a packet.
		"downdev\t00DC550A\t00000000\t0000\t0\t0\t0\t00FFFFFF\t0\t0\t0\n")

	for _, tt := range []struct {
		name  string
		iface string
		ip    string
		want  bool
	}{
		{"overlay address is covered", "towne30c", "10.85.219.1", true},
		{"another address in the overlay subnet", "towne30c", "10.85.219.150", true},
		{"address outside the subnet", "towne30c", "10.85.220.1", false},
		{"interface has no route covering the address", "enp0s1", "10.85.219.1", false},
		{"lan address on the lan interface", "enp0s1", "192.168.122.50", true},
		{"route exists but is not up", "downdev", "10.85.220.1", false},
		{"interface absent from the table", "nosuch", "10.85.219.1", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := routeCovers(tt.iface, net.ParseIP(tt.ip)); got != tt.want {
				t.Errorf("routeCovers(%q, %s) = %v, want %v", tt.iface, tt.ip, got, tt.want)
			}
		})
	}
}

// An address that is on no interface is never bindable, so rolodex must not be
// pointed at it.
func TestHostAddrRoutedRejectsUnassignedAddress(t *testing.T) {
	if hostAddrRouted("203.0.113.7") {
		t.Error("hostAddrRouted(203.0.113.7) = true, want false: the address is on no interface")
	}
	if hostAddrRouted("not-an-ip") {
		t.Error("hostAddrRouted(not-an-ip) = true, want false")
	}
}

// The wait is bounded: an address that never appears must not hang reconcile.
func TestWaitForHostAddrTimesOut(t *testing.T) {
	start := time.Now()
	if waitForHostAddr(context.Background(), "203.0.113.7", 300*time.Millisecond) {
		t.Fatal("waitForHostAddr returned true for an address that does not exist")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("wait took %s, want it bounded near the 300ms timeout", elapsed)
	}
}

// A cancelled context ends the wait immediately rather than burning the timeout.
func TestWaitForHostAddrHonoursContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitForHostAddr(ctx, "203.0.113.7", time.Minute) {
		t.Fatal("waitForHostAddr returned true on a cancelled context")
	}
}

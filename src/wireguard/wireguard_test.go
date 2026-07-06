package wireguard

import (
	"encoding/base64"
	"net/netip"
	"strings"
	"testing"
)

func TestGenerateKeypairDerivesMatchingPublicKey(t *testing.T) {
	priv, pub, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}

	rawPriv, err := base64.StdEncoding.DecodeString(priv)
	if err != nil {
		t.Fatalf("decode private: %v", err)
	}
	if len(rawPriv) != 32 {
		t.Fatalf("private key = %d bytes, want 32", len(rawPriv))
	}
	rawPub, err := base64.StdEncoding.DecodeString(pub)
	if err != nil {
		t.Fatalf("decode public: %v", err)
	}
	if len(rawPub) != 32 {
		t.Fatalf("public key = %d bytes, want 32", len(rawPub))
	}

	// Private key must be clamped.
	if rawPriv[0]&7 != 0 || rawPriv[31]&128 != 0 || rawPriv[31]&64 == 0 {
		t.Fatalf("private key is not clamped: %x", rawPriv)
	}

	derived, err := PublicKeyFromPrivate(priv)
	if err != nil {
		t.Fatalf("PublicKeyFromPrivate: %v", err)
	}
	if derived != pub {
		t.Fatalf("derived public %q != generated %q", derived, pub)
	}
}

func TestGenerateKeypairUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		priv, _, err := GenerateKeypair()
		if err != nil {
			t.Fatalf("GenerateKeypair: %v", err)
		}
		if seen[priv] {
			t.Fatalf("duplicate private key generated: %s", priv)
		}
		seen[priv] = true
	}
}

func TestRenderInterfaceConfig(t *testing.T) {
	cfg := InterfaceConfig{
		PrivateKey: "PRIV",
		Address:    "10.90.12.1/24",
		ListenPort: 51820,
		Peers: []PeerConfig{
			{PublicKey: "PUB1", AllowedIPs: "10.90.12.2/32", Endpoint: "1.2.3.4:51821", Keepalive: 25},
			{PublicKey: "PUB2", AllowedIPs: "10.90.12.3/32"},
		},
	}
	got := RenderInterfaceConfig(cfg)

	for _, want := range []string{
		"[Interface]",
		"PrivateKey = PRIV",
		"Address = 10.90.12.1/24",
		"ListenPort = 51820",
		"PublicKey = PUB1",
		"AllowedIPs = 10.90.12.2/32",
		"Endpoint = 1.2.3.4:51821",
		"PersistentKeepalive = 25",
		"PublicKey = PUB2",
		"AllowedIPs = 10.90.12.3/32",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered config missing %q:\n%s", want, got)
		}
	}

	// The keyless peer must not emit an Endpoint or keepalive line.
	if strings.Count(got, "Endpoint = ") != 1 {
		t.Errorf("expected exactly one Endpoint line:\n%s", got)
	}
	if strings.Count(got, "PersistentKeepalive = ") != 1 {
		t.Errorf("expected exactly one keepalive line:\n%s", got)
	}
}

func TestSubnetForNetworkDeterministicAndScoped(t *testing.T) {
	const machineID = "abc123def456"

	a, err := SubnetForNetwork(machineID, "home")
	if err != nil {
		t.Fatalf("SubnetForNetwork: %v", err)
	}
	b, err := SubnetForNetwork(machineID, "home")
	if err != nil {
		t.Fatalf("SubnetForNetwork: %v", err)
	}
	if a != b {
		t.Fatalf("non-deterministic subnet: %s != %s", a, b)
	}
	if a.Bits() != 24 {
		t.Fatalf("subnet bits = %d, want 24", a.Bits())
	}
	octets := a.Addr().As4()
	if octets[0] != 10 || octets[1] < 64 || octets[1] > 127 {
		t.Fatalf("subnet %s not in 10.64.0.0/10", a)
	}

	// Different network name → (almost surely) different subnet.
	other, err := SubnetForNetwork(machineID, "office")
	if err != nil {
		t.Fatalf("SubnetForNetwork: %v", err)
	}
	if other == a {
		t.Fatalf("distinct networks produced identical subnet %s", a)
	}

	// Different machine → different subnet keyspace.
	otherMachine, err := SubnetForNetwork("zzz999", "home")
	if err != nil {
		t.Fatalf("SubnetForNetwork: %v", err)
	}
	if otherMachine == a {
		t.Fatalf("distinct machines produced identical subnet %s", a)
	}
}

func TestSubnetForNetworkRequiresMachineID(t *testing.T) {
	if _, err := SubnetForNetwork("", "home"); err == nil {
		t.Fatal("expected error for empty machine id")
	}
}

func TestLocalAddrAndCIDR(t *testing.T) {
	subnet := netip.MustParsePrefix("10.90.12.0/24")
	if got := LocalAddr(subnet).String(); got != "10.90.12.1" {
		t.Fatalf("LocalAddr = %s, want 10.90.12.1", got)
	}
	if got := AddressCIDR(subnet); got != "10.90.12.1/24" {
		t.Fatalf("AddressCIDR = %s, want 10.90.12.1/24", got)
	}
}

func TestAllocatePeerAddr(t *testing.T) {
	subnet := netip.MustParsePrefix("10.90.12.0/24")

	// Seeded allocation is stable and avoids reserved hosts.
	used := map[string]bool{}
	addr, err := AllocatePeerAddr(subnet, used, "device-machine-id")
	if err != nil {
		t.Fatalf("AllocatePeerAddr: %v", err)
	}
	again, err := AllocatePeerAddr(subnet, used, "device-machine-id")
	if err != nil {
		t.Fatalf("AllocatePeerAddr: %v", err)
	}
	if addr != again {
		t.Fatalf("seeded allocation not stable: %s != %s", addr, again)
	}
	host := addr.As4()[3]
	if host < 2 || host > 254 {
		t.Fatalf("host octet %d out of range 2..254", host)
	}

	// Collision probes forward to a different host.
	used[addr.String()] = true
	next, err := AllocatePeerAddr(subnet, used, "device-machine-id")
	if err != nil {
		t.Fatalf("AllocatePeerAddr: %v", err)
	}
	if next == addr {
		t.Fatalf("expected a different address when %s is used", addr)
	}
}

func TestAllocatePeerAddrExhaustion(t *testing.T) {
	subnet := netip.MustParsePrefix("10.90.12.0/24")
	used := map[string]bool{}
	for i := 2; i <= 254; i++ {
		a := subnet.Addr().As4()
		a[3] = byte(i)
		used[netip.AddrFrom4(a).String()] = true
	}
	if _, err := AllocatePeerAddr(subnet, used, ""); err == nil {
		t.Fatal("expected exhaustion error")
	}
}

func TestListenPortForIndex(t *testing.T) {
	if got := ListenPortForIndex(0); got != DefaultListenPortBase {
		t.Fatalf("index 0 port = %d, want %d", got, DefaultListenPortBase)
	}
	if got := ListenPortForIndex(3); got != DefaultListenPortBase+3 {
		t.Fatalf("index 3 port = %d, want %d", got, DefaultListenPortBase+3)
	}
}

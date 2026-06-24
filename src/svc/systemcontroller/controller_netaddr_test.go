package systemcontroller

import (
	"net"
	"testing"
)

// addr builds a *net.IPNet (which implements net.Addr) for the given CIDR-less
// IP string, mirroring what iface.Addrs() returns.
func addr(ip string) net.Addr {
	return &net.IPNet{IP: net.ParseIP(ip), Mask: nil}
}

func TestFirstIPv4(t *testing.T) {
	tests := []struct {
		name  string
		addrs []net.Addr
		want  string
	}{
		{"none", nil, ""},
		{"loopback skipped", []net.Addr{addr("127.0.0.1")}, ""},
		{"first non-loopback", []net.Addr{addr("127.0.0.1"), addr("192.168.1.5"), addr("192.168.1.6")}, "192.168.1.5"},
		{"ipv6 ignored", []net.Addr{addr("2001:db8::1"), addr("10.0.0.2")}, "10.0.0.2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstIPv4(tt.addrs); got != tt.want {
				t.Fatalf("firstIPv4 = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFirstGlobalIPv6(t *testing.T) {
	tests := []struct {
		name  string
		addrs []net.Addr
		want  string
	}{
		{"none", nil, ""},
		{"ipv4 ignored", []net.Addr{addr("192.168.1.5")}, ""},
		{"link-local excluded", []net.Addr{addr("fe80::1")}, ""},
		{"ula excluded", []net.Addr{addr("fd12:3456::1")}, ""},
		{"loopback excluded", []net.Addr{addr("::1")}, ""},
		{"global selected", []net.Addr{addr("2001:db8::1")}, "2001:db8::1"},
		{
			"first global among noise",
			[]net.Addr{addr("192.168.1.5"), addr("fe80::1"), addr("fd00::1"), addr("2001:db8::2"), addr("2001:db8::3")},
			"2001:db8::2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstGlobalIPv6(tt.addrs); got != tt.want {
				t.Fatalf("firstGlobalIPv6 = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsVirtualInterfacePkg(t *testing.T) {
	virtual := []string{"podman0", "podman1", "veth1234", "cni-podman0", "docker0", "br-abc123", "virbr0", "tailscale0"}
	physical := []string{"eth0", "enp1s0", "wlan0", "wlo1", "eno1"}
	for _, n := range virtual {
		if !IsVirtualInterface(n) {
			t.Errorf("IsVirtualInterface(%q) = false, want true", n)
		}
	}
	for _, n := range physical {
		if IsVirtualInterface(n) {
			t.Errorf("IsVirtualInterface(%q) = true, want false", n)
		}
	}
}

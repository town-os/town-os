package systemd

import (
	"strings"
	"testing"
)

func TestNetworkUnitName(t *testing.T) {
	if got := NetworkUnitName("home"); got != "town-os-network--home.service" {
		t.Fatalf("NetworkUnitName = %q", got)
	}
	if !IsNetworkUnit("town-os-network--office.service") {
		t.Fatal("IsNetworkUnit should match a network unit")
	}
	if IsNetworkUnit("town-os-system--prometheus.service") {
		t.Fatal("IsNetworkUnit should not match a system unit")
	}
}

func TestGenerateNetworkUnit(t *testing.T) {
	unit := GenerateNetworkUnit(NetworkUnitConfig{
		Name:       "office",
		ConfigPath: "/run/town-os/town1a2b.conf",
	})

	if unit.Name != "town-os-network--office.service" {
		t.Fatalf("unit name = %q", unit.Name)
	}

	for _, want := range []string{
		"Description=Town OS Network: office",
		"Type=oneshot",
		"RemainAfterExit=yes",
		"ExecStartPre=-/usr/bin/wg-quick down /run/town-os/town1a2b.conf",
		"ExecStart=/usr/bin/wg-quick up /run/town-os/town1a2b.conf",
		"ExecStop=-/usr/bin/wg-quick down /run/town-os/town1a2b.conf",
		"Restart=on-failure",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit.Content, want) {
			t.Errorf("unit content missing %q:\n%s", want, unit.Content)
		}
	}
}

package systemd

import (
	"slices"
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

// TestNetworkUnitExecPathsMatchUnit keeps NetworkUnitExecPaths honest against
// the unit it claims to describe. The integration side asserts that the image
// ships every path this returns, so a path that drifts out of the unit text
// would leave the real ExecStart unverified while the test still passed.
func TestNetworkUnitExecPathsMatchUnit(t *testing.T) {
	paths := NetworkUnitExecPaths()
	if len(paths) == 0 {
		t.Fatal("NetworkUnitExecPaths returned nothing; the network unit execs at least wg-quick")
	}

	unit := GenerateNetworkUnit(NetworkUnitConfig{
		Name:       "office",
		ConfigPath: "/run/town-os/town1a2b.conf",
	})

	for _, p := range paths {
		if !strings.HasPrefix(p, "/") {
			t.Errorf("NetworkUnitExecPaths returned %q; only absolute paths belong here, "+
				"since a PATH-resolved binary is not something the unit pins", p)
		}
		if !strings.Contains(unit.Content, p) {
			t.Errorf("NetworkUnitExecPaths claims %q but the generated unit never execs it:\n%s", p, unit.Content)
		}
	}

	// The reverse direction: every absolute path the unit execs must be covered,
	// or the image check silently stops covering it.
	for line := range strings.SplitSeq(unit.Content, "\n") {
		if !strings.HasPrefix(line, "ExecStart") && !strings.HasPrefix(line, "ExecStop") {
			continue
		}
		_, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		// Strip systemd's "-" prefix (failure-tolerant exec) before the path.
		bin, _, _ := strings.Cut(strings.TrimPrefix(value, "-"), " ")
		if !strings.HasPrefix(bin, "/") {
			continue
		}
		if !slices.Contains(paths, bin) {
			t.Errorf("unit execs %q but NetworkUnitExecPaths does not list it, so no test checks the image ships it", bin)
		}
	}
}

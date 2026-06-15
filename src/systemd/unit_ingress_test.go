// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemd

import (
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

// A package with an HTTP (ingress) port and a raw SSH port: the ingress port is
// fronted by the shared :443 ingress, so the per-package NC must not publish it,
// while SSH stays on the NC. The service container joins the ingress network.
func TestGeneratePackageUnitsIngressPortNotHostPublished(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:               "default",
		PkgName:                "gitea",
		Version:                "1.0",
		Image:                  "gitea/gitea:testtag",
		NetworkControllerImage: "localhost/town-os-networkcontroller:testtag",
		NetworkStatePath:       "/var/lib/town-os/net",
		External:               packages.PortMap{3000: 3000, 2222: 22},
		IngressPorts:           map[uint16]bool{3000: true},
	}

	units := GeneratePackageUnits(cfg)

	// Service container joins the shared ingress network.
	if !strings.Contains(units.Service.Content, "--network "+IngressNetworkName) {
		t.Fatalf("service should join the ingress network:\n%s", units.Service.Content)
	}

	// The NC exists for the SSH port and must forward 2222 but NOT 3000.
	if units.NetworkController == nil {
		t.Fatal("expected an NC unit for the non-ingress SSH port")
	}
	nc := units.NetworkController.Content
	if strings.Contains(nc, "-p 3000:3000") {
		t.Fatalf("ingress port 3000 must not be host-published by the NC:\n%s", nc)
	}
	if !strings.Contains(nc, "-p 2222:2222") {
		t.Fatalf("SSH port 2222 should be host-published by the NC:\n%s", nc)
	}

	// No socket-activation unit for the ingress port.
	for _, s := range units.Sockets {
		if strings.Contains(s.Content, "3000") {
			t.Fatalf("ingress port 3000 must not get a socket unit:\n%s", s.Content)
		}
	}
}

// An HTTP-only package still gets an NC (it owns the package's private network
// and lifecycle) but the NC publishes no host port — the :443 ingress fronts the
// HTTP port, and the container joins the ingress network so the ingress reaches
// it by name.
func TestGeneratePackageUnitsHTTPOnlyKeepsNCButPublishesNoPort(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:               "default",
		PkgName:                "blog",
		Version:                "1.0",
		Image:                  "nginx:testtag",
		NetworkControllerImage: "localhost/town-os-networkcontroller:testtag",
		NetworkStatePath:       "/var/lib/town-os/net",
		External:               packages.PortMap{8080: 80},
		IngressPorts:           map[uint16]bool{8080: true},
	}

	units := GeneratePackageUnits(cfg)

	if units.NetworkController == nil {
		t.Fatal("HTTP-only package should still get an NC unit to own its network")
	}
	if strings.Contains(units.NetworkController.Content, "-p 8080") {
		t.Fatalf("ingress HTTP port must not be host-published by the NC:\n%s", units.NetworkController.Content)
	}
	if len(units.Sockets) != 0 {
		t.Fatalf("ingress HTTP port must not get a socket unit, got %d", len(units.Sockets))
	}
	if !strings.Contains(units.Service.Content, "--network "+IngressNetworkName) {
		t.Fatalf("HTTP-only service should join the ingress network:\n%s", units.Service.Content)
	}
}

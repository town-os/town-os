// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemd

import (
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

// TestDirectPortPublishedByServiceNotNC verifies the escape hatch: a direct
// port is host-published by the service container itself, while the NC keeps
// publishing the remaining proxied ports.
func TestDirectPortPublishedByServiceNotNC(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:               "test-repo",
		PkgName:                "myapp",
		Version:                "1.0",
		Image:                  "myapp:1.0",
		Environment:            map[string]string{},
		External:               packages.PortMap{2222: 22, 443: 8080},
		Volumes:                map[string]packages.PackageVolume{},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
		DirectPorts:            map[uint16]bool{2222: true},
	}

	units := GeneratePackageUnits(cfg)

	svc := units.Service.Content
	if !strings.Contains(svc, "-p 2222:22") {
		t.Fatalf("service must host-publish the direct port 2222:22, got:\n%s", svc)
	}
	if strings.Contains(svc, "-p 443") {
		t.Fatalf("service must NOT publish the proxied port 443:\n%s", svc)
	}

	if units.NetworkController == nil {
		t.Fatal("expected an NC unit for the remaining proxied port")
	}
	nc := units.NetworkController.Content
	if !strings.Contains(nc, "-p 443:443") {
		t.Fatalf("NC must publish the proxied port 443, got:\n%s", nc)
	}
	if strings.Contains(nc, "-p 2222") {
		t.Fatalf("NC must NOT publish the direct port 2222, got:\n%s", nc)
	}

	// The service still owns the host-facing socket/firewall plumbing for the
	// direct port (it binds it now).
	if !strings.Contains(svc, "--add-port=2222/tcp") {
		t.Fatalf("service must open the firewall for the direct port:\n%s", svc)
	}
}

// TestAllDirectPortsNoNetworkController verifies that when every port is
// direct, no NC unit is generated and the service publishes them all.
func TestAllDirectPortsNoNetworkController(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:               "test-repo",
		PkgName:                "myapp",
		Version:                "1.0",
		Image:                  "myapp:1.0",
		Environment:            map[string]string{},
		External:               packages.PortMap{2222: 22},
		Internal:               packages.PortMap{9000: 9000},
		Volumes:                map[string]packages.PackageVolume{},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
		DirectPorts:            map[uint16]bool{2222: true, 9000: true},
	}

	units := GeneratePackageUnits(cfg)

	if units.NetworkController != nil {
		t.Fatalf("expected no NC unit when all ports are direct, got:\n%s", units.NetworkController.Content)
	}
	svc := units.Service.Content
	if !strings.Contains(svc, "-p 2222:22") || !strings.Contains(svc, "-p 9000:9000") {
		t.Fatalf("service must publish all direct ports, got:\n%s", svc)
	}
}

// TestDirectPortIgnoredForDependency verifies dependencies never host-publish,
// even when DirectPorts is populated (deps share the parent network).
func TestDirectPortIgnoredForDependency(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:               "test-repo",
		PkgName:                "myapp--dep--db",
		Version:                "1.0",
		Image:                  "postgres:16",
		Environment:            map[string]string{},
		External:               packages.PortMap{5432: 5432},
		Volumes:                map[string]packages.PackageVolume{},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
		DirectPorts:            map[uint16]bool{5432: true},
		// Dependency markers: shares the parent's network + NC.
		ParentNetwork:    "town-os-net--test-repo-myapp-1.0",
		ParentUnitName:   "town-os-package--test-repo-myapp-1.0.service",
		ParentNCUnitName: "town-os-package--test-repo-myapp-1.0-network.service",
	}

	units := GeneratePackageUnits(cfg)

	svc := units.Service.Content
	if strings.Contains(svc, "-p 5432") {
		t.Fatalf("dependency must never host-publish, got:\n%s", svc)
	}
	if units.NetworkController != nil {
		t.Fatal("dependency must not generate its own NC unit")
	}
}

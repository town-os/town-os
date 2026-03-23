// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemd

import (
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

func TestGenerateVMPackageUnitsBasic(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:                 "test-repo",
		PkgName:                  "debian-vm",
		Version:                  "1.0",
		Runtime:                  packages.RuntimeVM,
		VM:                       &packages.PackageVM{Image: "debian.raw", Memory: 2147483648, CPUs: 2},
		VMImagePath:              "/town-os/vm-images/debian.raw",
		External:                 packages.PortMap{8022: 22},
		Internal:                 packages.PortMap{},
		Environment:              map[string]string{},
		Volumes:                  map[string]packages.PackageVolume{},
		BtrfsBase:                "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:         "/var/run/town-os",
	}

	units := GeneratePackageUnits(cfg)

	svc := units.Service.Content

	// Should use qemu-system-x86_64.
	if !strings.Contains(svc, "qemu-system-x86_64") {
		t.Fatal("VM service missing qemu-system-x86_64")
	}

	// Should have correct memory (2048 MB).
	if !strings.Contains(svc, "-m 2048") {
		t.Fatalf("VM service missing -m 2048, got:\n%s", svc)
	}

	// Should have correct CPU count.
	if !strings.Contains(svc, "-smp 2") {
		t.Fatalf("VM service missing -smp 2, got:\n%s", svc)
	}

	// Should have KVM enabled.
	if !strings.Contains(svc, "-enable-kvm") {
		t.Fatal("VM service missing -enable-kvm")
	}

	// Should have nographic mode.
	if !strings.Contains(svc, "-nographic") {
		t.Fatal("VM service missing -nographic")
	}

	// Should have the disk drive.
	if !strings.Contains(svc, "-drive file=/town-os/vm-images/debian.raw,format=raw,if=virtio") {
		t.Fatalf("VM service missing disk drive, got:\n%s", svc)
	}

	// Should have port forwarding.
	if !strings.Contains(svc, "hostfwd=tcp::8022-:22") {
		t.Fatalf("VM service missing port forwarding, got:\n%s", svc)
	}

	// Should have virtio-net device.
	if !strings.Contains(svc, "virtio-net-pci,netdev=net0") {
		t.Fatal("VM service missing virtio-net device")
	}

	// Should NOT use podman.
	if strings.Contains(svc, "podman") {
		t.Fatal("VM service should not reference podman")
	}

	// Description should say VM.
	if !strings.Contains(svc, "Description=Town OS VM Service:") {
		t.Fatalf("VM service missing VM description, got:\n%s", svc)
	}

	// Should have socket units for ports.
	if len(units.Sockets) != 1 {
		t.Fatalf("expected 1 socket unit, got %d", len(units.Sockets))
	}
	if !strings.Contains(units.Sockets[0].Content, "ListenStream=8022") {
		t.Fatal("socket missing ListenStream=8022")
	}

	// Should have network controller.
	if units.NetworkController == nil {
		t.Fatal("expected network controller unit for external ports")
	}
}

func TestGenerateVMPackageUnitsNoPorts(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:                 "test-repo",
		PkgName:                  "headless-vm",
		Version:                  "1.0",
		Runtime:                  packages.RuntimeVM,
		VM:                       &packages.PackageVM{Image: "headless.raw", Memory: 1073741824, CPUs: 1},
		VMImagePath:              "/town-os/vm-images/headless.raw",
		External:                 packages.PortMap{},
		Internal:                 packages.PortMap{},
		Environment:              map[string]string{},
		Volumes:                  map[string]packages.PackageVolume{},
		BtrfsBase:                "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:         "/var/run/town-os",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	// No sockets.
	if len(units.Sockets) != 0 {
		t.Fatalf("expected 0 socket units, got %d", len(units.Sockets))
	}

	// No network controller.
	if units.NetworkController != nil {
		t.Fatal("expected no network controller unit")
	}

	// No firewall commands.
	if strings.Contains(svc, "firewall-cmd") {
		t.Fatal("VM service should not have firewall commands with no ports")
	}

	// No socket stop/start.
	if strings.Contains(svc, "systemctl stop") {
		t.Fatal("VM service should not have socket stop with no ports")
	}
	if strings.Contains(svc, "systemctl start") {
		t.Fatal("VM service should not have socket start with no ports")
	}

	// Default memory (1024 MB).
	if !strings.Contains(svc, "-m 1024") {
		t.Fatalf("VM service missing default -m 1024, got:\n%s", svc)
	}
}

func TestGenerateVMPackageUnitsMultiplePorts(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:                 "test-repo",
		PkgName:                  "multi-port-vm",
		Version:                  "1.0",
		Runtime:                  packages.RuntimeVM,
		VM:                       &packages.PackageVM{Image: "multi.raw", Memory: 4294967296, CPUs: 4},
		VMImagePath:              "/town-os/vm-images/multi.raw",
		External:                 packages.PortMap{8022: 22, 8080: 80},
		Internal:                 packages.PortMap{9090: 9090},
		Environment:              map[string]string{},
		Volumes:                  map[string]packages.PackageVolume{},
		BtrfsBase:                "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:         "/var/run/town-os",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	// Should have 3 socket units.
	if len(units.Sockets) != 3 {
		t.Fatalf("expected 3 socket units, got %d", len(units.Sockets))
	}

	// Should have all port forwarding rules.
	if !strings.Contains(svc, "hostfwd=tcp::8022-:22") {
		t.Fatalf("VM service missing port forwarding 8022:22, got:\n%s", svc)
	}
	if !strings.Contains(svc, "hostfwd=tcp::8080-:80") {
		t.Fatalf("VM service missing port forwarding 8080:80, got:\n%s", svc)
	}
	if !strings.Contains(svc, "hostfwd=tcp::9090-:9090") {
		t.Fatalf("VM service missing port forwarding 9090:9090, got:\n%s", svc)
	}

	// 4 GB = 4096 MB.
	if !strings.Contains(svc, "-m 4096") {
		t.Fatalf("VM service missing -m 4096, got:\n%s", svc)
	}

	// 4 CPUs.
	if !strings.Contains(svc, "-smp 4") {
		t.Fatalf("VM service missing -smp 4, got:\n%s", svc)
	}

	// Network controller should be generated for external ports.
	if units.NetworkController == nil {
		t.Fatal("expected network controller unit for external ports")
	}
}

func TestGenerateVMPackageUnitsWithDescription(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:    "test-repo",
		PkgName:     "debian-vm",
		Version:     "1.0",
		Description: "Debian virtual machine",
		Runtime:     packages.RuntimeVM,
		VM:          &packages.PackageVM{Image: "debian.raw", Memory: 1073741824, CPUs: 1},
		VMImagePath: "/town-os/vm-images/debian.raw",
		External:    packages.PortMap{},
		Internal:    packages.PortMap{},
		Environment: map[string]string{},
		Volumes:     map[string]packages.PackageVolume{},
		BtrfsBase:   "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if !strings.Contains(svc, "Description=Town OS VM: Debian virtual machine") {
		t.Fatalf("expected VM description with custom text, got:\n%s", svc)
	}
}

func TestContainerDispatchDoesNotGenerateVM(t *testing.T) {
	// Ensure that a container package (default runtime) does NOT dispatch to VM generation.
	cfg := PackageUnitConfig{
		RepoName:    "test-repo",
		PkgName:     "nginx",
		Version:     "1.0",
		Image:       "nginx:1.0",
		Environment: map[string]string{},
		External:    packages.PortMap{},
		Internal:    packages.PortMap{},
		Volumes:     map[string]packages.PackageVolume{},
		BtrfsBase:   "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if strings.Contains(svc, "qemu-system-x86_64") {
		t.Fatal("container service should not reference qemu")
	}
	if !strings.Contains(svc, "podman run") {
		t.Fatal("container service should use podman run")
	}
}

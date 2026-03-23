package systemd

import (
	"fmt"
	"strings"
)

// generateVMPackageUnits produces systemd unit files for a QEMU VM package.
// The main service unit launches qemu-system-x86_64 with the VM image.
// Networking uses port forwarding via the network controller, similar to
// container packages.
func generateVMPackageUnits(cfg PackageUnitConfig) PackageUnits {
	var units PackageUnits

	ports := allPorts(cfg.External, cfg.Internal)
	hasPorts := len(ports) > 0

	// VM packages: network controller is needed whenever there are any ports.
	needsNetworkController := hasPorts

	units.Service = generateVMServiceUnit(cfg, ports, needsNetworkController)

	if hasPorts {
		units.Sockets = make([]UnitFile, len(ports))
		for i, port := range ports {
			units.Sockets[i] = generateSocketUnit(cfg, port)
		}
	}

	if needsNetworkController {
		nc := generateNetworkControllerUnit(cfg)
		units.NetworkController = &nc
	}

	return units
}

func generateVMServiceUnit(cfg PackageUnitConfig, ports []uint16, needsNetworkController bool) UnitFile {
	var b strings.Builder

	// [Unit]
	b.WriteString("[Unit]\n")
	if cfg.Description != "" {
		fmt.Fprintf(&b, "Description=Town OS VM: %s\n", cfg.Description)
	} else {
		fmt.Fprintf(&b, "Description=Town OS VM Service: %s/%s@%s\n", cfg.RepoName, cfg.PkgName, cfg.Version)
	}
	if needsNetworkController {
		fmt.Fprintf(&b, "Wants=%s\n", NetworkControllerUnitName(cfg.RepoName, cfg.PkgName, cfg.Version))
	}
	b.WriteString("After=network-online.target\n")

	// [Service]
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")

	// Stop socket units to free ports before QEMU binds.
	if len(ports) > 0 {
		socketNames := make([]string, len(ports))
		for i, p := range ports {
			socketNames[i] = SocketUnitName(cfg.RepoName, cfg.PkgName, cfg.Version, p)
		}
		fmt.Fprintf(&b, "ExecStartPre=-/bin/systemctl stop %s\n", strings.Join(socketNames, " "))
	}

	// Firewall: open ports.
	if len(ports) > 0 {
		portArgs := make([]string, len(ports))
		for i, p := range ports {
			portArgs[i] = fmt.Sprintf("--add-port=%d/tcp", p)
		}
		fmt.Fprintf(&b, "ExecStartPre=-/bin/sh -c 'command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd %s || true'\n", strings.Join(portArgs, " "))
	}

	// VM memory and CPU settings.
	memory := uint64(1073741824) // 1GB default
	cpus := 1
	if cfg.VM != nil {
		if cfg.VM.Memory > 0 {
			memory = cfg.VM.Memory
		}
		if cfg.VM.CPUs > 0 {
			cpus = cfg.VM.CPUs
		}
	}
	memoryMB := memory / (1024 * 1024)

	// ExecStart: qemu-system-x86_64 with VM image.
	fmt.Fprintf(&b, "ExecStart=/usr/bin/qemu-system-x86_64 \\\n")
	fmt.Fprintf(&b, "  -m %d \\\n", memoryMB)
	fmt.Fprintf(&b, "  -smp %d \\\n", cpus)
	fmt.Fprintf(&b, "  -nographic \\\n")
	fmt.Fprintf(&b, "  -enable-kvm \\\n")

	// VM disk image.
	imagePath := cfg.VMImagePath
	if imagePath == "" && cfg.VM != nil {
		imagePath = cfg.VM.Image
	}
	fmt.Fprintf(&b, "  -drive file=%s,format=raw,if=virtio \\\n", imagePath)

	// Network: user-mode with port forwarding for each mapped port.
	var netdev strings.Builder
	netdev.WriteString("user,id=net0")
	for _, mapping := range allPortMappings(cfg.External, cfg.Internal) {
		parts := strings.SplitN(mapping, ":", 2)
		if len(parts) == 2 {
			fmt.Fprintf(&netdev, ",hostfwd=tcp::%s-:%s", parts[0], parts[1])
		}
	}
	fmt.Fprintf(&b, "  -netdev %s \\\n", netdev.String())
	b.WriteString("  -device virtio-net-pci,netdev=net0\n")

	// ExecStop is handled by systemd KillSignal (default SIGTERM).

	// Firewall: close ports.
	if len(ports) > 0 {
		portArgs := make([]string, len(ports))
		for i, p := range ports {
			portArgs[i] = fmt.Sprintf("--remove-port=%d/tcp", p)
		}
		fmt.Fprintf(&b, "ExecStopPost=-/bin/sh -c 'command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd %s || true'\n", strings.Join(portArgs, " "))
	}

	// Restart socket units after stop.
	if len(ports) > 0 {
		socketNames := make([]string, len(ports))
		for i, p := range ports {
			socketNames[i] = SocketUnitName(cfg.RepoName, cfg.PkgName, cfg.Version, p)
		}
		fmt.Fprintf(&b, "ExecStopPost=-/bin/systemctl start %s\n", strings.Join(socketNames, " "))
	}

	b.WriteString("Restart=on-failure\n")

	// [Install]
	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")

	return UnitFile{
		Name:    UnitName(cfg.RepoName, cfg.PkgName, cfg.Version),
		Content: b.String(),
	}
}

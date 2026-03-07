package systemd

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"gitea.com/town-os/town-os/src/packages"
)

// PackageUnitConfig holds all the information needed to generate systemd units
// for a package's podman container or QEMU VM service.
type PackageUnitConfig struct {
	RepoName                 string
	PkgName                  string
	Version                  string
	Description              string // package description; used in the service unit Description line
	Image                    string
	Command                  []string
	Environment              map[string]string
	External                 packages.PortMap
	Internal                 packages.PortMap
	Volumes                  map[string]packages.PackageVolume
	BtrfsBase                string
	NetworkControllerBinPath string
	NetworkStatePath         string
	NetworkMode              string // "" or "bridge" → -p mappings; "host" → --net host
	Runtime                  packages.RuntimeType
	VM                       *packages.PackageVM
	VMImagePath              string // resolved path to the raw VM disk image
}

// UnitFile represents a single systemd unit file with its name and content.
type UnitFile struct {
	Name    string
	Content string
}

// PackageUnits collects all the systemd unit files generated for a package.
type PackageUnits struct {
	Service           UnitFile
	Sockets           []UnitFile
	NetworkController *UnitFile
}

// allPorts returns a sorted slice of all unique host ports from both external
// and internal port maps.
func allPorts(external, internal packages.PortMap) []uint16 {
	seen := map[uint16]bool{}
	for p := range external {
		seen[p] = true
	}
	for p := range internal {
		seen[p] = true
	}

	ports := make([]uint16, 0, len(seen))
	for p := range seen {
		ports = append(ports, p)
	}
	slices.Sort(ports)
	return ports
}

// allPortMappings returns a sorted slice of "host:container" port mapping
// strings from both external and internal port maps.
func allPortMappings(external, internal packages.PortMap) []string {
	type mapping struct {
		host      uint16
		container uint16
	}

	var mappings []mapping
	for host, container := range external {
		mappings = append(mappings, mapping{host, container})
	}
	for host, container := range internal {
		mappings = append(mappings, mapping{host, container})
	}

	sort.Slice(mappings, func(i, j int) bool { return mappings[i].host < mappings[j].host })

	result := make([]string, len(mappings))
	for i, m := range mappings {
		result[i] = fmt.Sprintf("%d:%d", m.host, m.container)
	}
	return result
}

// GeneratePackageUnits produces the full set of systemd unit files for a
// package based on its configuration. For VM packages (Runtime == RuntimeVM),
// it generates a QEMU service unit instead of a podman container unit.
func GeneratePackageUnits(cfg PackageUnitConfig) PackageUnits {
	if cfg.Runtime == packages.RuntimeVM {
		return generateVMPackageUnits(cfg)
	}

	var units PackageUnits

	ports := allPorts(cfg.External, cfg.Internal)
	hasExternalPorts := len(cfg.External) > 0
	hasPorts := len(ports) > 0

	needsNetworkController := hasExternalPorts
	if !needsNetworkController && cfg.NetworkMode == "host" {
		for host, container := range cfg.Internal {
			if host != container {
				needsNetworkController = true
				break
			}
		}
	}

	// --- Main service unit ---
	units.Service = generateServiceUnit(cfg, ports, needsNetworkController)

	// --- Socket units (one per port) ---
	if hasPorts {
		units.Sockets = make([]UnitFile, len(ports))
		for i, port := range ports {
			units.Sockets[i] = generateSocketUnit(cfg, port)
		}
	}

	// --- Network controller unit (if external ports or internal port forwarding needed) ---
	if needsNetworkController {
		nc := generateNetworkControllerUnit(cfg)
		units.NetworkController = &nc
	}

	return units
}

// generateVMPackageUnits produces systemd unit files for a QEMU VM package.
// The main service unit launches qemu-system-x86_64 with the VM image.
// Networking uses port forwarding via the network controller, similar to
// container packages.
func generateVMPackageUnits(cfg PackageUnitConfig) PackageUnits {
	var units PackageUnits

	ports := allPorts(cfg.External, cfg.Internal)
	hasExternalPorts := len(cfg.External) > 0
	hasPorts := len(ports) > 0

	needsNetworkController := hasExternalPorts
	if !needsNetworkController {
		for host, container := range cfg.Internal {
			if host != container {
				needsNetworkController = true
				break
			}
		}
	}

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

func generateServiceUnit(cfg PackageUnitConfig, ports []uint16, needsNetworkController bool) UnitFile {
	var b strings.Builder

	containerName := ContainerName(cfg.RepoName, cfg.PkgName, cfg.Version)

	// [Unit]
	b.WriteString("[Unit]\n")
	if cfg.Description != "" {
		fmt.Fprintf(&b, "Description=Town OS: %s\n", cfg.Description)
	} else {
		fmt.Fprintf(&b, "Description=Town OS Package Service: %s/%s@%s\n", cfg.RepoName, cfg.PkgName, cfg.Version)
	}
	if needsNetworkController {
		fmt.Fprintf(&b, "Wants=%s\n", NetworkControllerUnitName(cfg.RepoName, cfg.PkgName, cfg.Version))
	}
	b.WriteString("After=network-online.target\n")

	// [Service]
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman stop -t 10 %s\n", containerName)
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman rm -f %s\n", containerName)

	// Stop socket units to free ports before podman binds via -p.
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

	// Volume names, sorted for deterministic output.
	volNames := make([]string, 0, len(cfg.Volumes))
	for name := range cfg.Volumes {
		volNames = append(volNames, name)
	}
	sort.Strings(volNames)

	// Chown volumes when UID/GID is set.
	for _, name := range volNames {
		vol := cfg.Volumes[name]
		if vol.UID != nil && vol.GID != nil {
			hostPath := fmt.Sprintf("%s/installed/%s/%s/%s/%s", cfg.BtrfsBase, cfg.RepoName, cfg.PkgName, cfg.Version, name)
			fmt.Fprintf(&b, "ExecStartPre=/bin/chown -R %d:%d %s\n", *vol.UID, *vol.GID, hostPath)
		}
	}

	// ExecStart: podman run with network configuration.
	fmt.Fprintf(&b, "ExecStart=/usr/bin/podman run --name %s --systemd=true", containerName)

	if cfg.NetworkMode == "host" {
		b.WriteString(" --net host")
	} else {
		for _, mapping := range allPortMappings(cfg.External, cfg.Internal) {
			b.WriteString(" \\\n  -p " + mapping)
		}
	}

	// Environment variables, sorted by key.
	envKeys := make([]string, 0, len(cfg.Environment))
	for k := range cfg.Environment {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		fmt.Fprintf(&b, " \\\n  -e %s=%s", k, cfg.Environment[k])
	}

	// Volume mounts.
	for _, name := range volNames {
		vol := cfg.Volumes[name]
		hostPath := fmt.Sprintf("%s/installed/%s/%s/%s/%s", cfg.BtrfsBase, cfg.RepoName, cfg.PkgName, cfg.Version, name)
		fmt.Fprintf(&b, " \\\n  -v %s:%s:rw,z", hostPath, vol.Mountpoint)
	}

	if len(cfg.Command) > 0 {
		b.WriteString(" \\\n  " + cfg.Image)
		for _, arg := range cfg.Command {
			b.WriteString(" \\\n  " + arg)
		}
		b.WriteString("\n")
	} else {
		fmt.Fprintf(&b, " \\\n  %s\n", cfg.Image)
	}

	// ExecStop
	fmt.Fprintf(&b, "ExecStop=/usr/bin/podman stop -t 10 %s\n", containerName)

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

func generateSocketUnit(cfg PackageUnitConfig, port uint16) UnitFile {
	svcName := UnitName(cfg.RepoName, cfg.PkgName, cfg.Version)
	content := fmt.Sprintf(`[Unit]
Description=Town OS Socket: %s/%s@%s port %d/tcp
PartOf=%s

[Socket]
ListenStream=%d
FreeBind=true

[Install]
WantedBy=sockets.target
`, cfg.RepoName, cfg.PkgName, cfg.Version, port, svcName, port)

	return UnitFile{
		Name:    SocketUnitName(cfg.RepoName, cfg.PkgName, cfg.Version, port),
		Content: content,
	}
}

func generateNetworkControllerUnit(cfg PackageUnitConfig) UnitFile {
	svcName := UnitName(cfg.RepoName, cfg.PkgName, cfg.Version)
	statePath := fmt.Sprintf("%s/%s-%s-%s.json", cfg.NetworkStatePath, cfg.RepoName, cfg.PkgName, cfg.Version)
	content := fmt.Sprintf(`[Unit]
Description=Town OS Network Controller: %s/%s@%s
BindsTo=%s
After=%s

[Service]
Type=simple
ExecStart=%s --state %s
Restart=on-failure

[Install]
WantedBy=multi-user.target
`, cfg.RepoName, cfg.PkgName, cfg.Version, svcName, svcName, cfg.NetworkControllerBinPath, statePath)

	return UnitFile{
		Name:    NetworkControllerUnitName(cfg.RepoName, cfg.PkgName, cfg.Version),
		Content: content,
	}
}

// SystemServiceUnitConfig holds the information needed to generate a systemd
// unit for a system service (e.g. a monitoring container).
type SystemServiceUnitConfig struct {
	Key         string   // unique service key (e.g. "prometheus")
	Description string   // human-readable description
	Image       string   // container image reference
	Args        []string // additional podman run arguments (before the image)
	Command     []string // command and arguments (after the image)
	VolumeDirs  []string // host directories to mkdir -p before starting
}

// GenerateSystemServiceUnit produces a systemd unit file for a system service.
func GenerateSystemServiceUnit(cfg SystemServiceUnitConfig) UnitFile {
	var b strings.Builder
	containerName := SystemServiceContainerName(cfg.Key)

	// [Unit]
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=Town OS System Service: %s\n", cfg.Description)
	b.WriteString("After=network-online.target\n")

	// [Service]
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman stop -t 10 %s\n", containerName)
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman rm -f %s\n", containerName)
	for _, dir := range cfg.VolumeDirs {
		fmt.Fprintf(&b, "ExecStartPre=/bin/mkdir -p %s\n", dir)
	}

	// ExecStart
	fmt.Fprintf(&b, "ExecStart=/usr/bin/podman run --name %s", containerName)
	for _, arg := range cfg.Args {
		fmt.Fprintf(&b, " \\\n  %s", arg)
	}
	fmt.Fprintf(&b, " \\\n  %s", cfg.Image)
	for _, cmd := range cfg.Command {
		fmt.Fprintf(&b, " \\\n  %s", cmd)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "ExecStop=/usr/bin/podman stop -t 10 %s\n", containerName)
	b.WriteString("Restart=always\n")

	// [Install]
	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")

	return UnitFile{
		Name:    SystemServiceUnitName(cfg.Key),
		Content: b.String(),
	}
}

// PackageUnitNames returns the list of all systemd unit names that would be
// generated for a package with the given port maps. This is used during
// uninstall to know which units to tear down.
func PackageUnitNames(repo, pkgName, version, networkMode string, external, internal packages.PortMap) []string {
	names := []string{UnitName(repo, pkgName, version)}

	ports := allPorts(external, internal)
	for _, p := range ports {
		names = append(names, SocketUnitName(repo, pkgName, version, p))
	}

	needsNC := len(external) > 0
	if !needsNC && networkMode == "host" {
		for host, container := range internal {
			if host != container {
				needsNC = true
				break
			}
		}
	}
	if needsNC {
		names = append(names, NetworkControllerUnitName(repo, pkgName, version))
	}

	return names
}

package systemd

import (
	"fmt"
	"sort"
	"strings"

	"gitea.com/town-os/town-os/src/packages"
)

// PackageUnitConfig holds all the information needed to generate systemd units
// for a package's podman container service.
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
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
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
// package based on its configuration.
func GeneratePackageUnits(cfg PackageUnitConfig) PackageUnits {
	var units PackageUnits

	ports := allPorts(cfg.External, cfg.Internal)
	hasExternalPorts := len(cfg.External) > 0
	hasPorts := len(ports) > 0

	// --- Main service unit ---
	units.Service = generateServiceUnit(cfg, ports, hasExternalPorts)

	// --- Socket units (one per port) ---
	if hasPorts {
		units.Sockets = make([]UnitFile, len(ports))
		for i, port := range ports {
			units.Sockets[i] = generateSocketUnit(cfg, port)
		}
	}

	// --- Network controller unit (only if external ports exist) ---
	if hasExternalPorts {
		nc := generateNetworkControllerUnit(cfg)
		units.NetworkController = &nc
	}

	return units
}

func generateServiceUnit(cfg PackageUnitConfig, ports []uint16, hasExternalPorts bool) UnitFile {
	var b strings.Builder

	containerName := ContainerName(cfg.RepoName, cfg.PkgName, cfg.Version)

	// [Unit]
	b.WriteString("[Unit]\n")
	if cfg.Description != "" {
		b.WriteString(fmt.Sprintf("Description=Town OS: %s\n", cfg.Description))
	} else {
		b.WriteString(fmt.Sprintf("Description=Town OS Package Service: %s/%s@%s\n", cfg.RepoName, cfg.PkgName, cfg.Version))
	}
	if hasExternalPorts {
		b.WriteString(fmt.Sprintf("Wants=%s\n", NetworkControllerUnitName(cfg.RepoName, cfg.PkgName, cfg.Version)))
	}
	b.WriteString("After=network-online.target\n")

	// [Service]
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	b.WriteString(fmt.Sprintf("ExecStartPre=-/usr/bin/podman stop -t 10 %s\n", containerName))
	b.WriteString(fmt.Sprintf("ExecStartPre=-/usr/bin/podman rm -f %s\n", containerName))

	// Stop socket units to free ports before podman binds via -p.
	if len(ports) > 0 {
		socketNames := make([]string, len(ports))
		for i, p := range ports {
			socketNames[i] = SocketUnitName(cfg.RepoName, cfg.PkgName, cfg.Version, p)
		}
		b.WriteString(fmt.Sprintf("ExecStartPre=-/bin/systemctl stop %s\n", strings.Join(socketNames, " ")))
	}

	// Firewall: open ports.
	if len(ports) > 0 {
		portArgs := make([]string, len(ports))
		for i, p := range ports {
			portArgs[i] = fmt.Sprintf("--add-port=%d/tcp", p)
		}
		b.WriteString(fmt.Sprintf("ExecStartPre=-/bin/sh -c 'command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd %s || true'\n", strings.Join(portArgs, " ")))
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
			b.WriteString(fmt.Sprintf("ExecStartPre=/bin/chown -R %d:%d %s\n", *vol.UID, *vol.GID, hostPath))
		}
	}

	// ExecStart: podman run with network configuration.
	b.WriteString(fmt.Sprintf("ExecStart=/usr/bin/podman run --name %s --systemd=true", containerName))

	if cfg.NetworkMode == "host" {
		b.WriteString(" --net host")
	} else {
		for _, mapping := range allPortMappings(cfg.External, cfg.Internal) {
			b.WriteString(fmt.Sprintf(" \\\n  -p %s", mapping))
		}
	}

	// Environment variables, sorted by key.
	envKeys := make([]string, 0, len(cfg.Environment))
	for k := range cfg.Environment {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		b.WriteString(fmt.Sprintf(" \\\n  -e %s=%s", k, cfg.Environment[k]))
	}

	// Volume mounts.
	for _, name := range volNames {
		vol := cfg.Volumes[name]
		hostPath := fmt.Sprintf("%s/installed/%s/%s/%s/%s", cfg.BtrfsBase, cfg.RepoName, cfg.PkgName, cfg.Version, name)
		b.WriteString(fmt.Sprintf(" \\\n  -v %s:%s:rw,z", hostPath, vol.Mountpoint))
	}

	if len(cfg.Command) > 0 {
		b.WriteString(fmt.Sprintf(" \\\n  %s", cfg.Image))
		for _, arg := range cfg.Command {
			b.WriteString(fmt.Sprintf(" \\\n  %s", arg))
		}
		b.WriteString("\n")
	} else {
		b.WriteString(fmt.Sprintf(" \\\n  %s\n", cfg.Image))
	}

	// ExecStop
	b.WriteString(fmt.Sprintf("ExecStop=/usr/bin/podman stop -t 10 %s\n", containerName))

	// Firewall: close ports.
	if len(ports) > 0 {
		portArgs := make([]string, len(ports))
		for i, p := range ports {
			portArgs[i] = fmt.Sprintf("--remove-port=%d/tcp", p)
		}
		b.WriteString(fmt.Sprintf("ExecStopPost=-/bin/sh -c 'command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd %s || true'\n", strings.Join(portArgs, " ")))
	}

	// Restart socket units after stop.
	if len(ports) > 0 {
		socketNames := make([]string, len(ports))
		for i, p := range ports {
			socketNames[i] = SocketUnitName(cfg.RepoName, cfg.PkgName, cfg.Version, p)
		}
		b.WriteString(fmt.Sprintf("ExecStopPost=-/bin/systemctl start %s\n", strings.Join(socketNames, " ")))
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

// PackageUnitNames returns the list of all systemd unit names that would be
// generated for a package with the given port maps. This is used during
// uninstall to know which units to tear down.
func PackageUnitNames(repo, pkgName, version string, external, internal packages.PortMap) []string {
	names := []string{UnitName(repo, pkgName, version)}

	ports := allPorts(external, internal)
	for _, p := range ports {
		names = append(names, SocketUnitName(repo, pkgName, version, p))
	}

	if len(external) > 0 {
		names = append(names, NetworkControllerUnitName(repo, pkgName, version))
	}

	return names
}

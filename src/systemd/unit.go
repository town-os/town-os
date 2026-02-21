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
	PkgName     string
	Version     string
	Image       string
	Command     []string
	Environment map[string]string
	External    packages.PortMap
	Internal    packages.PortMap
	Volumes     map[string]packages.PackageVolume
	BtrfsBase   string
	UPnPBinPath string
	NetworkMode string // "" or "bridge" → -p mappings; "host" → --net host
}

// UnitFile represents a single systemd unit file with its name and content.
type UnitFile struct {
	Name    string
	Content string
}

// PackageUnits collects all the systemd unit files generated for a package.
type PackageUnits struct {
	Service     UnitFile
	Sockets     []UnitFile
	Forwarders  []UnitFile // socat port-forwarding units (host mode only)
	UPnPService *UnitFile
	UPnPTimer   *UnitFile
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

// portPair holds an external→internal port mapping where the two differ.
type portPair struct {
	ext uint16
	int_ uint16
}

// forwardedPorts returns a sorted slice of port pairs where external != internal,
// collected from both port maps. These are the ports that need socat forwarding
// in host network mode.
func forwardedPorts(external, internal packages.PortMap) []portPair {
	var pairs []portPair
	for ext, int_ := range external {
		if ext != int_ {
			pairs = append(pairs, portPair{ext, int_})
		}
	}
	for ext, int_ := range internal {
		if ext != int_ {
			pairs = append(pairs, portPair{ext, int_})
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].ext < pairs[j].ext })
	return pairs
}

func generateForwarderUnit(pkgName string, extPort, intPort uint16) UnitFile {
	content := fmt.Sprintf(`[Unit]
Description=Town OS Port Forwarder: %s %d->%d/tcp
PartOf=town-os-%s.service
After=town-os-%s.service

[Service]
Type=simple
ExecStart=/usr/bin/socat TCP-LISTEN:%d,fork,reuseaddr TCP:127.0.0.1:%d
Restart=on-failure

[Install]
WantedBy=multi-user.target
`, pkgName, extPort, intPort, pkgName, pkgName, extPort, intPort)

	return UnitFile{
		Name:    ForwarderUnitName(pkgName, extPort),
		Content: content,
	}
}

// GeneratePackageUnits produces the full set of systemd unit files for a
// package based on its configuration.
func GeneratePackageUnits(cfg PackageUnitConfig) PackageUnits {
	var units PackageUnits

	ports := allPorts(cfg.External, cfg.Internal)
	hasExternalPorts := len(cfg.External) > 0
	hasPorts := len(ports) > 0

	// Compute forwarder pairs early so generateServiceUnit can reference them.
	var fwdPairs []portPair
	if cfg.NetworkMode == "host" {
		fwdPairs = forwardedPorts(cfg.External, cfg.Internal)
	}

	// --- Main service unit ---
	units.Service = generateServiceUnit(cfg, ports, hasExternalPorts, fwdPairs)

	// --- Socket units (one per port) ---
	if hasPorts {
		units.Sockets = make([]UnitFile, len(ports))
		for i, port := range ports {
			units.Sockets[i] = generateSocketUnit(cfg.PkgName, port)
		}
	}

	// --- Forwarder units (host mode, mismatched ports) ---
	if len(fwdPairs) > 0 {
		units.Forwarders = make([]UnitFile, len(fwdPairs))
		for i, p := range fwdPairs {
			units.Forwarders[i] = generateForwarderUnit(cfg.PkgName, p.ext, p.int_)
		}
	}

	// --- uPnP units (only if external ports exist) ---
	if hasExternalPorts {
		svc := generateUPnPServiceUnit(cfg)
		units.UPnPService = &svc
		tmr := generateUPnPTimerUnit(cfg.PkgName)
		units.UPnPTimer = &tmr
	}

	return units
}

func generateServiceUnit(cfg PackageUnitConfig, ports []uint16, hasExternalPorts bool, fwdPairs []portPair) UnitFile {
	var b strings.Builder

	// [Unit]
	b.WriteString("[Unit]\n")
	b.WriteString(fmt.Sprintf("Description=Town OS Package Service: %s@%s\n", cfg.PkgName, cfg.Version))
	if hasExternalPorts {
		b.WriteString(fmt.Sprintf("Wants=%s\n", UPnPTimerUnitName(cfg.PkgName)))
	}
	for _, p := range fwdPairs {
		b.WriteString(fmt.Sprintf("Wants=%s\n", ForwarderUnitName(cfg.PkgName, p.ext)))
	}
	b.WriteString("After=network-online.target\n")

	// [Service]
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	b.WriteString(fmt.Sprintf("ExecStartPre=-/usr/bin/podman stop -t 10 town-os-%s\n", cfg.PkgName))
	b.WriteString(fmt.Sprintf("ExecStartPre=-/usr/bin/podman rm -f town-os-%s\n", cfg.PkgName))

	// Stop socket units to free ports before podman binds via -p.
	if len(ports) > 0 {
		socketNames := make([]string, len(ports))
		for i, p := range ports {
			socketNames[i] = SocketUnitName(cfg.PkgName, p)
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

	// ExecStart: podman run with network configuration.
	b.WriteString(fmt.Sprintf("ExecStart=/usr/bin/podman run --name town-os-%s --systemd=true", cfg.PkgName))

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

	// Volume mounts, sorted by volume name.
	volNames := make([]string, 0, len(cfg.Volumes))
	for name := range cfg.Volumes {
		volNames = append(volNames, name)
	}
	sort.Strings(volNames)
	for _, name := range volNames {
		vol := cfg.Volumes[name]
		hostPath := fmt.Sprintf("%s/installed/%s/%s/%s", cfg.BtrfsBase, cfg.PkgName, cfg.Version, name)
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
	b.WriteString(fmt.Sprintf("ExecStop=/usr/bin/podman stop -t 10 town-os-%s\n", cfg.PkgName))

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
			socketNames[i] = SocketUnitName(cfg.PkgName, p)
		}
		b.WriteString(fmt.Sprintf("ExecStopPost=-/bin/systemctl start %s\n", strings.Join(socketNames, " ")))
	}

	b.WriteString("Restart=on-failure\n")

	// [Install]
	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")

	return UnitFile{
		Name:    UnitName(cfg.PkgName),
		Content: b.String(),
	}
}

func generateSocketUnit(pkgName string, port uint16) UnitFile {
	content := fmt.Sprintf(`[Unit]
Description=Town OS Socket: %s port %d/tcp
PartOf=town-os-%s.service

[Socket]
ListenStream=%d
FreeBind=true

[Install]
WantedBy=sockets.target
`, pkgName, port, pkgName, port)

	return UnitFile{
		Name:    SocketUnitName(pkgName, port),
		Content: content,
	}
}

func generateUPnPTimerUnit(pkgName string) UnitFile {
	content := fmt.Sprintf(`[Unit]
Description=Town OS uPnP Renewal: %s
PartOf=town-os-%s.service

[Timer]
OnBootSec=1min
OnUnitActiveSec=5min
Persistent=true

[Install]
WantedBy=timers.target
`, pkgName, pkgName)

	return UnitFile{
		Name:    UPnPTimerUnitName(pkgName),
		Content: content,
	}
}

func generateUPnPServiceUnit(cfg PackageUnitConfig) UnitFile {
	var b strings.Builder

	b.WriteString("[Unit]\n")
	b.WriteString(fmt.Sprintf("Description=Town OS uPnP Mapping: %s\n", cfg.PkgName))

	b.WriteString("\n[Service]\n")
	b.WriteString("Type=oneshot\n")

	// Build port arguments from external ports only.
	var pairs []portPair
	for ext, int_ := range cfg.External {
		pairs = append(pairs, portPair{ext, int_})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].ext < pairs[j].ext })

	args := make([]string, len(pairs))
	for i, p := range pairs {
		// In host mode the socat forwarder makes the external port reachable
		// on the host, so UPnP should advertise ext:ext (the host-visible port).
		if cfg.NetworkMode == "host" {
			args[i] = fmt.Sprintf("--port %d:%d", p.ext, p.ext)
		} else {
			args[i] = fmt.Sprintf("--port %d:%d", p.ext, p.int_)
		}
	}

	b.WriteString(fmt.Sprintf("ExecStart=%s add %s --ttl 600\n", cfg.UPnPBinPath, strings.Join(args, " ")))

	return UnitFile{
		Name:    UPnPServiceUnitName(cfg.PkgName),
		Content: b.String(),
	}
}

// PackageUnitNames returns the list of all systemd unit names that would be
// generated for a package with the given port maps. This is used during
// uninstall to know which units to tear down.
func PackageUnitNames(pkgName string, external, internal packages.PortMap) []string {
	names := []string{UnitName(pkgName)}

	ports := allPorts(external, internal)
	for _, p := range ports {
		names = append(names, SocketUnitName(pkgName, p))
	}

	// Include forwarder unit names for mismatched ports. These are safe to
	// include unconditionally (cleanup handles missing units gracefully).
	fwdPairs := forwardedPorts(external, internal)
	for _, p := range fwdPairs {
		names = append(names, ForwarderUnitName(pkgName, p.ext))
	}

	if len(external) > 0 {
		names = append(names, UPnPServiceUnitName(pkgName))
		names = append(names, UPnPTimerUnitName(pkgName))
	}

	return names
}

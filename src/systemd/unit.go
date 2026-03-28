// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

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
	NetworkControllerImage   string // container image for the network controller
	NetworkStatePath         string
	Runtime                  packages.RuntimeType
	VM                       *packages.PackageVM
	VMImagePath              string // resolved path to the raw VM disk image

	// ParentNetwork is the podman network name to join (for dependencies).
	// When set, the dependency joins this network instead of creating its own.
	// The dependency creates the network idempotently (in case it starts
	// before the parent) but does not remove it on stop.
	ParentNetwork string

	// ParentUnitName is the systemd unit name of the parent package service.
	// When set, adds PartOf= and Before= directives for dependency ordering.
	ParentUnitName string

	// DependencyUnitNames lists systemd unit names of direct dependency
	// services. When set, adds Wants= and After= directives on the parent
	// unit so dependencies start before the parent and stop after it.
	DependencyUnitNames []string

	// ParentNCUnitName is the network controller unit name of the parent
	// package. When set on a dependency, the dep adds After= for the NC so
	// the shared network exists before the dep starts.
	ParentNCUnitName string
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
	hasPorts := len(ports) > 0

	// Dependencies share the parent's NC — only standalone packages and
	// parents generate their own NC and host-facing socket/firewall rules.
	isDep := cfg.ParentNCUnitName != ""
	needsNetworkController := hasPorts && !isDep

	// --- Main service unit ---
	units.Service = generateServiceUnit(cfg, ports, needsNetworkController)

	// --- Socket units (one per port, host-facing only) ---
	if hasPorts && !isDep {
		units.Sockets = make([]UnitFile, len(ports))
		for i, port := range ports {
			units.Sockets[i] = generateSocketUnit(cfg, port)
		}
	}

	// --- Network controller unit (parent packages only) ---
	if needsNetworkController {
		nc := generateNetworkControllerUnit(cfg, ports)
		units.NetworkController = &nc
	}

	return units
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

	// Dependency ordering: deps get PartOf+Before parent; parent gets Wants+After deps.
	if cfg.ParentUnitName != "" {
		fmt.Fprintf(&b, "PartOf=%s\n", cfg.ParentUnitName)
		fmt.Fprintf(&b, "Before=%s\n", cfg.ParentUnitName)
	}

	// Collect all Wants and After targets.
	var wants, after []string
	for _, dep := range cfg.DependencyUnitNames {
		wants = append(wants, dep)
		after = append(after, dep)
	}
	if needsNetworkController {
		ncUnit := NetworkControllerUnitName(cfg.RepoName, cfg.PkgName, cfg.Version)
		wants = append(wants, ncUnit)
		after = append(after, ncUnit)
	}
	// Dependencies wait for the parent's NC (which creates the shared network).
	if cfg.ParentNCUnitName != "" {
		after = append(after, cfg.ParentNCUnitName)
	}
	if len(wants) > 0 {
		fmt.Fprintf(&b, "Wants=%s\n", strings.Join(wants, " "))
	}

	after = append(after, "network-online.target")
	fmt.Fprintf(&b, "After=%s\n", strings.Join(after, " "))

	// [Service]
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman container cleanup %s\n", containerName)
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman stop -t 10 %s\n", containerName)
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman rm -f %s\n", containerName)

	// Host-facing port operations (sockets, firewall) only apply to
	// standalone packages and parents. Dependencies' ports are internal-only
	// on the shared podman network.
	hostPorts := ports
	if cfg.ParentNCUnitName != "" {
		hostPorts = nil
	}

	// Stop socket units to free ports before podman binds via -p.
	if len(hostPorts) > 0 {
		socketNames := make([]string, len(hostPorts))
		for i, p := range hostPorts {
			socketNames[i] = SocketUnitName(cfg.RepoName, cfg.PkgName, cfg.Version, p)
		}
		fmt.Fprintf(&b, "ExecStartPre=-/bin/systemctl stop %s\n", strings.Join(socketNames, " "))
	}

	// Wait for the NC container to be running before starting the service.
	if needsNetworkController {
		ncContainerName := NetworkControllerContainerName(cfg.RepoName, cfg.PkgName, cfg.Version)
		fmt.Fprintf(&b, "ExecStartPre=/bin/sh -c 'for i in $(seq 1 30); do /usr/bin/podman container exists %s && exit 0; sleep 0.5; done; echo \"NC container %s not ready after 15s\"; exit 1'\n", ncContainerName, ncContainerName)
	} else if cfg.ParentNCUnitName != "" {
		// Dependency: wait for the parent's NC container to be running.
		parentNCContainer := NetworkControllerContainerNameFromUnit(cfg.ParentNCUnitName)
		fmt.Fprintf(&b, "ExecStartPre=/bin/sh -c 'for i in $(seq 1 30); do /usr/bin/podman container exists %s && exit 0; sleep 0.5; done; echo \"parent NC container %s not ready after 15s\"; exit 1'\n", parentNCContainer, parentNCContainer)
	}

	// Firewall: open ports (host-facing only).
	if len(hostPorts) > 0 {
		portArgs := make([]string, len(hostPorts))
		for i, p := range hostPorts {
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

	// Determine network name: dependencies join the parent's network,
	// standalone packages use their own.
	networkName := NetworkName(cfg.RepoName, cfg.PkgName, cfg.Version)
	isDep := cfg.ParentNetwork != ""
	if isDep {
		networkName = cfg.ParentNetwork
	}

	// Network lifecycle is owned by the NC when one exists. The NC creates
	// the network in its own ExecStartPre and removes it in ExecStopPost.
	// When no NC exists (package has no ports), the service unit manages
	// the network directly.
	if !needsNetworkController && cfg.ParentNCUnitName == "" {
		if !isDep && len(cfg.DependencyUnitNames) == 0 {
			// Standalone package without NC: remove stale network then create fresh.
			fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman network rm -f %s\n", networkName)
		}
		fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman network create %s\n", networkName)
	}

	// ExecStart: podman run on the shared/private network.
	fmt.Fprintf(&b, "ExecStart=/usr/bin/podman run --replace --name %s --systemd=true", containerName)
	fmt.Fprintf(&b, " \\\n  --net %s", networkName)

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

	// Firewall: close ports (host-facing only).
	if len(hostPorts) > 0 {
		portArgs := make([]string, len(hostPorts))
		for i, p := range hostPorts {
			portArgs[i] = fmt.Sprintf("--remove-port=%d/tcp", p)
		}
		fmt.Fprintf(&b, "ExecStopPost=-/bin/sh -c 'command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd %s || true'\n", strings.Join(portArgs, " "))
	}

	// Restart socket units after stop (host-facing only).
	if len(hostPorts) > 0 {
		socketNames := make([]string, len(hostPorts))
		for i, p := range hostPorts {
			socketNames[i] = SocketUnitName(cfg.RepoName, cfg.PkgName, cfg.Version, p)
		}
		fmt.Fprintf(&b, "ExecStopPost=-/bin/systemctl start %s\n", strings.Join(socketNames, " "))
	}

	// Clean up the podman network after the container stops.
	// When an NC exists, it owns network cleanup. Dependencies never remove
	// the shared network. Only standalone packages without NC clean up here.
	if !isDep && !needsNetworkController {
		fmt.Fprintf(&b, "ExecStopPost=-/usr/bin/podman network rm -f %s\n", networkName)
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

func generateNetworkControllerUnit(cfg PackageUnitConfig, ports []uint16) UnitFile {
	svcName := UnitName(cfg.RepoName, cfg.PkgName, cfg.Version)
	containerName := ContainerName(cfg.RepoName, cfg.PkgName, cfg.Version)
	ncContainerName := NetworkControllerContainerName(cfg.RepoName, cfg.PkgName, cfg.Version)
	networkName := NetworkName(cfg.RepoName, cfg.PkgName, cfg.Version)
	statePath := fmt.Sprintf("%s/%s-%s-%s.json", cfg.NetworkStatePath, cfg.RepoName, cfg.PkgName, cfg.Version)

	var b strings.Builder

	// [Unit] — NC starts first (Before service and deps), stops last.
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=Town OS Network Controller: %s/%s@%s\n", cfg.RepoName, cfg.PkgName, cfg.Version)
	fmt.Fprintf(&b, "PartOf=%s\n", svcName)

	// NC starts before the service and all dependencies.
	beforeTargets := []string{svcName}
	beforeTargets = append(beforeTargets, cfg.DependencyUnitNames...)
	fmt.Fprintf(&b, "Before=%s\n", strings.Join(beforeTargets, " "))

	// [Service]
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")

	// Create the podman network — NC owns the network lifecycle.
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman network create %s\n", networkName)

	// Clean up any stale NC container.
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman container cleanup %s\n", ncContainerName)
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman stop -t 10 %s\n", ncContainerName)
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman rm -f %s\n", ncContainerName)

	// Run the NC on the shared podman network with -p flags for host port
	// exposure. socat inside the NC resolves the service container by DNS
	// name on the shared network — no IP discovery needed.
	fmt.Fprintf(&b, "ExecStart=/usr/bin/podman run --replace --name %s", ncContainerName)
	fmt.Fprintf(&b, " \\\n  --net %s", networkName)
	for _, p := range ports {
		fmt.Fprintf(&b, " \\\n  -p %d:%d", p, p)
	}
	fmt.Fprintf(&b, " \\\n  -v %s:%s:ro", cfg.NetworkStatePath, cfg.NetworkStatePath)
	fmt.Fprintf(&b, " \\\n  %s --state %s --target-container %s\n", cfg.NetworkControllerImage, statePath, containerName)

	fmt.Fprintf(&b, "ExecStop=/usr/bin/podman stop -t 10 %s\n", ncContainerName)

	// NC owns network cleanup — remove the network after everything stops.
	fmt.Fprintf(&b, "ExecStopPost=-/usr/bin/podman network rm -f %s\n", networkName)

	b.WriteString("Restart=on-failure\n")

	// [Install]
	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")

	return UnitFile{
		Name:    NetworkControllerUnitName(cfg.RepoName, cfg.PkgName, cfg.Version),
		Content: b.String(),
	}
}

// SystemServiceUnitConfig holds the information needed to generate a systemd
// unit for a system service (e.g. a monitoring container).
type SystemServiceUnitConfig struct {
	Key            string   // unique service key (e.g. "prometheus")
	Description    string   // human-readable description
	Image          string   // container image reference
	Args           []string // additional podman run arguments (before the image)
	Command        []string // command and arguments (after the image)
	VolumeDirs     []string // host directories to mkdir -p before starting
	ExecStartPre   []string // additional ExecStartPre commands (after container cleanup and mkdir)
	ExecStopPost   []string // ExecStopPost commands (run after the service stops or fails)
}

// GenerateSystemServiceUnit produces a systemd unit file for a system service.
func GenerateSystemServiceUnit(cfg SystemServiceUnitConfig) UnitFile {
	var b strings.Builder
	containerName := SystemServiceContainerName(cfg.Key)

	// [Unit]
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=Town OS System Service: %s\n", cfg.Description)
	b.WriteString("After=network-online.target\n")
	b.WriteString("StartLimitIntervalSec=0\n")

	// [Service]
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman container cleanup %s\n", containerName)
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman stop -t 10 %s\n", containerName)
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman rm -f %s\n", containerName)
	for _, dir := range cfg.VolumeDirs {
		fmt.Fprintf(&b, "ExecStartPre=/bin/mkdir -p %s\n", dir)
	}
	for _, cmd := range cfg.ExecStartPre {
		fmt.Fprintf(&b, "ExecStartPre=%s\n", cmd)
	}

	// ExecStart
	fmt.Fprintf(&b, "ExecStart=/usr/bin/podman run --replace --name %s", containerName)
	for _, arg := range cfg.Args {
		fmt.Fprintf(&b, " \\\n  %s", arg)
	}
	fmt.Fprintf(&b, " \\\n  %s", cfg.Image)
	for _, cmd := range cfg.Command {
		fmt.Fprintf(&b, " \\\n  %s", cmd)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "ExecStop=/usr/bin/podman stop -t 10 %s\n", containerName)
	for _, cmd := range cfg.ExecStopPost {
		fmt.Fprintf(&b, "ExecStopPost=%s\n", cmd)
	}
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
func PackageUnitNames(repo, pkgName, version string, external, internal packages.PortMap) []string {
	names := []string{UnitName(repo, pkgName, version)}

	ports := allPorts(external, internal)
	for _, p := range ports {
		names = append(names, SocketUnitName(repo, pkgName, version, p))
	}

	// Network controller is present whenever there are any ports.
	if len(ports) > 0 {
		names = append(names, NetworkControllerUnitName(repo, pkgName, version))
	}

	return names
}

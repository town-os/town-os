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

// HostVolumeMount describes an arbitrary host-path to container-path volume
// mount (as opposed to btrfs-managed package volumes).
type HostVolumeMount struct {
	HostPath      string
	ContainerPath string
	Options       string // e.g. "ro", "rw,z" — defaults to "rw,z" if empty
}

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

	// SystemServiceKey, when non-empty, generates units with the system
	// service naming convention (town-os-system-- prefix) instead of the
	// package prefix. This allows system infrastructure (e.g. monitoring)
	// to use the same unit generation code path as packages.
	SystemServiceKey string

	// ExtraArgs are additional podman run arguments inserted before the image
	// name (e.g. "--pid", "host", "--cap-add", "SYS_TIME").
	ExtraArgs []string

	// HostVolumeMounts are arbitrary host:container volume mounts (in addition
	// to btrfs-managed Volumes). Applied after btrfs volumes.
	HostVolumeMounts []HostVolumeMount

	// ExecStartPreExtra are additional ExecStartPre commands appended after
	// standard container cleanup and volume preparation.
	ExecStartPreExtra []string

	// MkdirPaths are host directories to create (mkdir -p) before starting.
	MkdirPaths []string

	// RestartAlways uses Restart=always instead of Restart=on-failure.
	RestartAlways bool

	// PullNever adds --pull=never to the podman run command for local images.
	PullNever bool

	// StartLimitIntervalZero sets StartLimitIntervalSec=0 for unlimited retries.
	StartLimitIntervalZero bool
}

// serviceUnitName returns the systemd unit name for the main service.
func (cfg PackageUnitConfig) serviceUnitName() string {
	if cfg.SystemServiceKey != "" {
		return SystemServiceUnitName(cfg.SystemServiceKey)
	}
	return UnitName(cfg.RepoName, cfg.PkgName, cfg.Version)
}

// serviceContainerName returns the podman container name for the service.
func (cfg PackageUnitConfig) serviceContainerName() string {
	if cfg.SystemServiceKey != "" {
		return SystemServiceContainerName(cfg.SystemServiceKey)
	}
	return ContainerName(cfg.RepoName, cfg.PkgName, cfg.Version)
}

// serviceNetworkName returns the podman network name.
func (cfg PackageUnitConfig) serviceNetworkName() string {
	if cfg.SystemServiceKey != "" {
		return "town-os-net--system-" + cfg.SystemServiceKey
	}
	return NetworkName(cfg.RepoName, cfg.PkgName, cfg.Version)
}

// serviceNCUnitName returns the network controller unit name.
func (cfg PackageUnitConfig) serviceNCUnitName() string {
	if cfg.SystemServiceKey != "" {
		return fmt.Sprintf("%s%s-network.service", SystemServiceUnitPrefix, cfg.SystemServiceKey)
	}
	return NetworkControllerUnitName(cfg.RepoName, cfg.PkgName, cfg.Version)
}

// serviceNCContainerName returns the NC podman container name.
func (cfg PackageUnitConfig) serviceNCContainerName() string {
	if cfg.SystemServiceKey != "" {
		return fmt.Sprintf("%s%s-network", SystemServiceUnitPrefix, cfg.SystemServiceKey)
	}
	return NetworkControllerContainerName(cfg.RepoName, cfg.PkgName, cfg.Version)
}

// serviceSocketUnitName returns the socket unit name for a given port.
func (cfg PackageUnitConfig) serviceSocketUnitName(port uint16) string {
	if cfg.SystemServiceKey != "" {
		return fmt.Sprintf("%s%s-%d-tcp.socket", SystemServiceUnitPrefix, cfg.SystemServiceKey, port)
	}
	return SocketUnitName(cfg.RepoName, cfg.PkgName, cfg.Version, port)
}

// serviceNCStatePath returns the NC state file path.
func (cfg PackageUnitConfig) serviceNCStatePath() string {
	if cfg.SystemServiceKey != "" {
		return fmt.Sprintf("%s/system-%s.json", cfg.NetworkStatePath, cfg.SystemServiceKey)
	}
	return fmt.Sprintf("%s/%s-%s-%s.json", cfg.NetworkStatePath, cfg.RepoName, cfg.PkgName, cfg.Version)
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

	containerName := cfg.serviceContainerName()

	// [Unit]
	b.WriteString("[Unit]\n")
	if cfg.Description != "" {
		fmt.Fprintf(&b, "Description=Town OS: %s\n", cfg.Description)
	} else {
		fmt.Fprintf(&b, "Description=Town OS Package Service: %s/%s@%s\n", cfg.RepoName, cfg.PkgName, cfg.Version)
	}
	if cfg.StartLimitIntervalZero {
		b.WriteString("StartLimitIntervalSec=0\n")
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
		ncUnit := cfg.serviceNCUnitName()
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
			socketNames[i] = cfg.serviceSocketUnitName(p)
		}
		fmt.Fprintf(&b, "ExecStartPre=-/bin/systemctl stop %s\n", strings.Join(socketNames, " "))
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

	// Create host directories before starting.
	for _, dir := range cfg.MkdirPaths {
		fmt.Fprintf(&b, "ExecStartPre=/bin/mkdir -p %s\n", dir)
	}

	// Additional ExecStartPre commands.
	for _, cmd := range cfg.ExecStartPreExtra {
		fmt.Fprintf(&b, "ExecStartPre=%s\n", cmd)
	}

	// Determine network name: dependencies join the parent's network,
	// standalone packages use their own.
	networkName := cfg.serviceNetworkName()
	isDep := cfg.ParentNetwork != ""
	if isDep {
		networkName = cfg.ParentNetwork
	}

	// Standalone packages without an NC or parent: remove stale network
	// before creating fresh. The NC owns cleanup via ExecStopPost when present.
	if !needsNetworkController && cfg.ParentNCUnitName == "" && !isDep && len(cfg.DependencyUnitNames) == 0 {
		fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman network rm -f %s\n", networkName)
	}
	// Always create the network idempotently. Even when an NC exists, the
	// service must be able to start if the NC hasn't created it yet (boot
	// race). The NC also creates it — whoever gets there first wins.
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman network create %s\n", networkName)

	// ExecStart: podman run on the shared/private network.
	fmt.Fprintf(&b, "ExecStart=/usr/bin/podman run --replace --name %s --systemd=true", containerName)
	if cfg.PullNever {
		b.WriteString(" \\\n  --pull=never")
	}
	fmt.Fprintf(&b, " \\\n  --net %s", networkName)

	// Extra podman args (e.g. --pid host, --cap-add).
	for _, arg := range cfg.ExtraArgs {
		fmt.Fprintf(&b, " \\\n  %s", arg)
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

	// Host volume mounts (arbitrary host paths, not btrfs-managed).
	for _, hv := range cfg.HostVolumeMounts {
		opts := hv.Options
		if opts == "" {
			opts = "rw,z"
		}
		fmt.Fprintf(&b, " \\\n  -v %s:%s:%s", hv.HostPath, hv.ContainerPath, opts)
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
			socketNames[i] = cfg.serviceSocketUnitName(p)
		}
		fmt.Fprintf(&b, "ExecStopPost=-/bin/systemctl start %s\n", strings.Join(socketNames, " "))
	}

	// Clean up the podman network after the container stops.
	// When an NC exists, it owns network cleanup. Dependencies never remove
	// the shared network. Only standalone packages without NC clean up here.
	if !isDep && !needsNetworkController {
		fmt.Fprintf(&b, "ExecStopPost=-/usr/bin/podman network rm -f %s\n", networkName)
	}

	if cfg.RestartAlways {
		b.WriteString("Restart=always\n")
	} else {
		b.WriteString("Restart=on-failure\n")
	}

	// [Install]
	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")

	return UnitFile{
		Name:    cfg.serviceUnitName(),
		Content: b.String(),
	}
}

func generateSocketUnit(cfg PackageUnitConfig, port uint16) UnitFile {
	svcName := cfg.serviceUnitName()

	var descID string
	if cfg.SystemServiceKey != "" {
		descID = cfg.SystemServiceKey
	} else {
		descID = fmt.Sprintf("%s/%s@%s", cfg.RepoName, cfg.PkgName, cfg.Version)
	}

	content := fmt.Sprintf(`[Unit]
Description=Town OS Socket: %s port %d/tcp
PartOf=%s

[Socket]
ListenStream=%d
FreeBind=true

[Install]
WantedBy=sockets.target
`, descID, port, svcName, port)

	return UnitFile{
		Name:    cfg.serviceSocketUnitName(port),
		Content: content,
	}
}

func generateNetworkControllerUnit(cfg PackageUnitConfig, ports []uint16) UnitFile {
	svcName := cfg.serviceUnitName()
	containerName := cfg.serviceContainerName()
	ncContainerName := cfg.serviceNCContainerName()
	networkName := cfg.serviceNetworkName()
	statePath := cfg.serviceNCStatePath()

	var descID string
	if cfg.SystemServiceKey != "" {
		descID = cfg.SystemServiceKey
	} else {
		descID = fmt.Sprintf("%s/%s@%s", cfg.RepoName, cfg.PkgName, cfg.Version)
	}

	var b strings.Builder

	// [Unit] — NC starts first (Before service and deps), stops last.
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=Town OS Network Controller: %s\n", descID)
	fmt.Fprintf(&b, "PartOf=%s\n", svcName)
	// The system controller builds the NC image at startup, so NC units
	// must wait for it to finish before attempting to start.
	b.WriteString("After=town-os-systemcontroller.service\n")

	// NC starts before the service and all dependencies.
	beforeTargets := []string{svcName}
	beforeTargets = append(beforeTargets, cfg.DependencyUnitNames...)
	fmt.Fprintf(&b, "Before=%s\n", strings.Join(beforeTargets, " "))
	if cfg.StartLimitIntervalZero {
		b.WriteString("StartLimitIntervalSec=0\n")
	}

	// [Service]
	b.WriteString("\n[Service]\n")
	// Type=notify with --sdnotify=conmon: podman's conmon sends READY=1
	// to systemd when the container process actually starts. This lets
	// systemd's native Before=/After= ordering guarantee the NC is
	// running before dependent services start — no polling needed.
	b.WriteString("Type=notify\n")
	b.WriteString("NotifyAccess=all\n")

	// Wait for the NC image to be available. The systemcontroller builds
	// it at startup, so on boot the image may not exist yet. This check
	// combined with Restart=on-failure ensures the NC retries until the
	// image is ready.
	fmt.Fprintf(&b, "ExecStartPre=/usr/bin/podman image exists %s\n", cfg.NetworkControllerImage)

	// Create the podman network — NC owns the network lifecycle.
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman network create %s\n", networkName)

	// Clean up any stale NC container.
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman container cleanup %s\n", ncContainerName)
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman rm -f %s\n", ncContainerName)

	// Run the NC on the shared podman network with -p flags for host port
	// exposure. socat inside the NC resolves the service container by DNS
	// name on the shared network — no IP discovery needed.
	fmt.Fprintf(&b, "ExecStart=/usr/bin/podman run --replace --sdnotify=conmon --name %s", ncContainerName)
	fmt.Fprintf(&b, " \\\n  --net %s", networkName)
	for _, p := range ports {
		fmt.Fprintf(&b, " \\\n  -p %d:%d", p, p)
	}
	fmt.Fprintf(&b, " \\\n  -v %s:%s:ro", cfg.NetworkStatePath, cfg.NetworkStatePath)
	fmt.Fprintf(&b, " \\\n  %s /town-os-networkcontroller --state %s --target-container %s\n", cfg.NetworkControllerImage, statePath, containerName)

	fmt.Fprintf(&b, "ExecStop=/usr/bin/podman stop -t 10 %s\n", ncContainerName)

	// NC owns network cleanup — remove the network after everything stops.
	fmt.Fprintf(&b, "ExecStopPost=-/usr/bin/podman network rm -f %s\n", networkName)

	if cfg.RestartAlways {
		b.WriteString("Restart=always\n")
	} else {
		b.WriteString("Restart=on-failure\n")
	}

	// [Install]
	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")

	return UnitFile{
		Name:    cfg.serviceNCUnitName(),
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
	PullNever      bool     // when true, adds --pull=never to podman run (for local images)
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
	if cfg.PullNever {
		b.WriteString(" \\\n  --pull=never")
	}
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
	cfg := PackageUnitConfig{RepoName: repo, PkgName: pkgName, Version: version}
	names := []string{cfg.serviceUnitName()}

	ports := allPorts(external, internal)
	for _, p := range ports {
		names = append(names, cfg.serviceSocketUnitName(p))
	}

	// Network controller is present whenever there are any ports.
	if len(ports) > 0 {
		names = append(names, cfg.serviceNCUnitName())
	}

	return names
}

// SystemServicePackageUnitNames returns the list of all systemd unit names
// generated for a system service that uses GeneratePackageUnits (e.g. monitoring).
func SystemServicePackageUnitNames(key string, external, internal packages.PortMap) []string {
	cfg := PackageUnitConfig{SystemServiceKey: key}
	names := []string{cfg.serviceUnitName()}

	ports := allPorts(external, internal)
	for _, p := range ports {
		names = append(names, cfg.serviceSocketUnitName(p))
	}

	if len(ports) > 0 {
		names = append(names, cfg.serviceNCUnitName())
	}

	return names
}

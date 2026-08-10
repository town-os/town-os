// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemd

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"gitea.com/town-os/town-os/src/packages"
)

// encodeEntrypointJSON renders args as a JSON array for podman's
// --entrypoint flag. Since a []string cannot produce a json.Marshal error
// (unlike struct types with Marshaler interfaces), the error branch is
// unreachable; on the impossible path we fall back to an empty array so
// podman still sees a valid value and the caller's unit remains valid.
func encodeEntrypointJSON(args []string) string {
	b, err := json.Marshal(args)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// quoteCommandArg wraps arg in single quotes when it contains whitespace or
// shell metacharacters that systemd would otherwise tokenize — systemd's
// ExecStart parser splits on whitespace unless text is inside balanced "..."
// or '...' pairs (systemd.syntax(5)). Without this, a compiled package
// command like `["sh", "-c", "python foo && exec python bar"]` would emit
// the third element as three separate argv tokens to podman. Embedded
// single quotes are escaped POSIX-style as '\'' so a caller can pass any
// string verbatim. Unchanged args pass through so existing unit files
// stay byte-for-byte stable.
//
// Quoting is NOT sufficient for a newline, and used to be treated as though it
// were: '\n' is in the list below, so a value carrying one was wrapped in
// single quotes and emitted — but unit-file quoting does not span lines. The
// directive ends at the raw newline regardless of the open quote, and whatever
// followed is parsed as a new directive in the same section, which is how a
// package's environment value could add an ExecStartPre that runs on the host
// as root. packages.ValidateNoControlChars refuses those values at compile
// time; stripControlChars is the backstop for any path that reaches unit
// generation without having gone through Compile.
func quoteCommandArg(arg string) string {
	arg = stripControlChars(arg)
	if arg == "" {
		return "''"
	}
	needsQuote := false
	for _, r := range arg {
		switch r {
		case ' ', '\t', '"', '\'', '\\', '$', '&', ';', '|',
			'<', '>', '(', ')', '*', '?', '!', '#', '~', '`', '[', ']', '{', '}':
			needsQuote = true
		}
		if needsQuote {
			break
		}
	}
	if !needsQuote {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// stripControlChars removes the bytes that would end a unit-file directive
// early or truncate the file, leaving tab (legitimate whitespace that quoting
// does contain).
//
// It drops rather than escapes. systemd does resolve C-style escapes inside
// quotes, so `\n` would in principle round-trip — but relying on that puts the
// correctness of a security boundary on a parser detail, and there is no value
// in faithfully delivering a newline to podman that a package had no business
// sending. Dropping is the outcome that cannot be wrong.
//
// This should never fire: Compile refuses these values, so anything reaching
// here has bypassed it. It exists because unit generation has no error return
// and this is the last point before the bytes are written to
// /etc/systemd/system.
func stripControlChars(s string) string {
	if !strings.ContainsFunc(s, isUnitControlChar) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isUnitControlChar(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isUnitControlChar(r rune) bool {
	if r == '\t' {
		return false
	}
	return r < 0x20 || r == 0x7f
}

// HostVolumeMount describes an arbitrary host-path to container-path volume
// mount (as opposed to btrfs-managed package volumes). When both UID and GID
// are set, the unit generator emits an ExecStartPre chown to fix host-path
// ownership before the container starts, which is required for container
// images that run as a fixed uid (Grafana, Prometheus, etc.).
type HostVolumeMount struct {
	HostPath      string
	ContainerPath string
	Options       string  // e.g. "ro", "rw,z" — defaults to "rw,z" if empty
	UID           *uint32 // optional host-path owner; emits chown -R when set with GID
	GID           *uint32 // optional host-path group; emits chown -R when set with UID
}

// PackageUnitConfig holds all the information needed to generate systemd units
// for a package's podman container or QEMU VM service.
type PackageUnitConfig struct {
	RepoName                 string
	PkgName                  string
	Version                  string
	Description              string // package description; used in the service unit Description line
	Image                    string
	// Entrypoint, when non-empty, is emitted as `--entrypoint=<json-array>`
	// before the image so podman fully replaces the image's built-in
	// ENTRYPOINT. This is required for images that wrap a launcher script
	// around the real command (e.g. matrixdotorg/synapse's /start.py) and
	// reject arbitrary first-arg modes. Command remains the argv that
	// runs AFTER the entrypoint (i.e. the container CMD).
	Entrypoint               []string
	Command                  []string
	Environment              map[string]string
	External                 packages.PortMap
	Internal                 packages.PortMap
	// DirectPorts marks host ports the SERVICE container must publish itself
	// with `-p host:container`, bypassing the network controller proxy. The
	// NC skips these ports (it neither publishes nor forwards them) and, when
	// every port is direct, no NC unit is generated at all. Only honored for
	// standalone/parent packages — dependencies never host-publish.
	DirectPorts              map[uint16]bool
	// IngressPorts are host-port keys served by the shared :443 ingress instead
	// of this package's own NC. They are dropped from the NC's host publishing
	// (no -p, no socket) and the container joins the ingress network so the
	// ingress can reverse_proxy to it by name. Only set for standalone packages.
	IngressPorts             map[uint16]bool
	Volumes                  map[string]packages.PackageVolume
	BtrfsBase                string
	NetworkControllerImage   string // container image for the network controller
	NetworkStatePath         string
	Runtime                  packages.RuntimeType
	VM                       *packages.PackageVM
	VMImagePath              string // resolved path to the raw VM disk image

	// NetworkAliases are additional hostnames to attach to the container
	// on its podman network (emitted as one --network-alias per entry).
	// Used for dependency packages: the systemcontroller sets the alias
	// to the short dep key (e.g. "db", "ml") so parent packages can reach
	// the dep via an RFC-valid short hostname rather than the full
	// container name, which contains consecutive hyphens (reserved by
	// validator.js for IDN punycode) and is rejected by some strict URL
	// validators — notably the one in immich's SystemConfig Dto.
	NetworkAliases []string

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

	// TLSDir is the host path to the btrfs-backed TLS directory that
	// contains the local CA and every per-package leaf cert. When set,
	// the network controller container gets this tree bind-mounted
	// read-only so tls_proxy.go can read cert.pem/key.pem for each
	// package whose state file turns on the TLS flag. Leave empty to
	// skip the mount (tests and packages with no HTTP-supplying ports).
	TLSDir string
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

// IngressNetworkName is the shared podman network the :443 ingress Caddy and
// every HTTP-fronted package container join, so the ingress can reverse_proxy to
// each backend by container name via podman's built-in DNS.
const IngressNetworkName = "town-os-ingress"

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

	// Direct ports are host-published by the service container itself and the
	// NC never touches them. The NC is only needed for the remaining
	// (proxied) ports. Direct ports are ignored for dependencies, which never
	// host-publish regardless.
	isDep := cfg.ParentNCUnitName != ""
	// Proxied ports are those the per-package NC binds/forwards on the host:
	// everything except direct ports (the container host-publishes those) and
	// ingress ports (the shared :443 ingress fronts those).
	proxiedPorts := ports
	if !isDep && (len(cfg.DirectPorts) > 0 || len(cfg.IngressPorts) > 0) {
		proxiedPorts = make([]uint16, 0, len(ports))
		for _, p := range ports {
			if cfg.DirectPorts[p] || cfg.IngressPorts[p] {
				continue
			}
			proxiedPorts = append(proxiedPorts, p)
		}
	}

	// Dependencies share the parent's NC — only standalone packages and
	// parents generate their own NC and host-facing socket/firewall rules. A
	// package whose only ports are ingress ports still gets an NC: it owns the
	// package's private podman network and lifecycle even though the shared
	// :443 ingress, not the NC, terminates its HTTP traffic.
	needsNetworkController := !isDep && (len(proxiedPorts) > 0 || len(cfg.IngressPorts) > 0)

	// --- Main service unit ---
	units.Service = generateServiceUnit(cfg, ports, needsNetworkController)

	// --- Socket units (one per host-published port) ---
	// Ingress ports are not host-published by this package, so they get no
	// socket-activation unit (the ingress owns :443).
	if hasPorts && !isDep {
		for _, port := range ports {
			if cfg.IngressPorts[port] {
				continue
			}
			units.Sockets = append(units.Sockets, generateSocketUnit(cfg, port))
		}
	}

	// --- Network controller unit (parent packages only) ---
	// The NC binds and forwards only the proxied ports; direct ports are
	// bound by the service container.
	if needsNetworkController {
		nc := generateNetworkControllerUnit(cfg, proxiedPorts)
		units.NetworkController = &nc
	}

	return units
}

// directHostPorts returns the sorted subset of the given host ports that the
// service container must publish itself. Empty for dependencies (which never
// host-publish) and when no DirectPorts are configured.
func (cfg PackageUnitConfig) directHostPorts(ports []uint16) []uint16 {
	if cfg.ParentNCUnitName != "" || len(cfg.DirectPorts) == 0 {
		return nil
	}
	out := make([]uint16, 0, len(cfg.DirectPorts))
	for _, p := range ports {
		if cfg.DirectPorts[p] {
			out = append(out, p)
		}
	}
	return out
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
			hostPath := fmt.Sprintf("%s/installed/%s/%s/%s/%s", cfg.BtrfsBase, cfg.RepoName, packages.StoragePath(cfg.PkgName), cfg.Version, name)
			fmt.Fprintf(&b, "ExecStartPre=/bin/chown -R %d:%d %s\n", *vol.UID, *vol.GID, hostPath)
		}
	}

	// Create host directories before starting. Do this before chowning
	// host-volume mounts so the path exists when chown runs.
	for _, dir := range cfg.MkdirPaths {
		fmt.Fprintf(&b, "ExecStartPre=/bin/mkdir -p %s\n", dir)
	}

	// Chown host-volume mounts when UID/GID is set. This is the single
	// declarative source of ownership for bind-mounted host paths and
	// replaces per-service hand-rolled ExecStartPreExtra chown entries.
	//
	// The chown is intentionally non-recursive: only the top of the
	// bind-mount is touched. Containers that WRITE to their bind mount
	// (Grafana → /var/lib/grafana, Prometheus → /prometheus) need to
	// own the top directory so they can create files inside, but once
	// they've created children those children are already owned by the
	// container uid and never drift. Containers that only READ from
	// their mount don't need the chown at all — 0755/0644 host perms
	// already let any uid read the contents.
	for _, hv := range cfg.HostVolumeMounts {
		if hv.UID != nil && hv.GID != nil {
			fmt.Fprintf(&b, "ExecStartPre=/bin/chown %d:%d %s\n", *hv.UID, *hv.GID, hv.HostPath)
		}
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
	// Standalone packages with an ingress (HTTP) port also join the shared
	// ingress network so the :443 ingress can reach them by container name.
	// Created idempotently (the ingress unit creates it too — first one wins).
	joinIngress := len(cfg.IngressPorts) > 0 && !isDep
	if joinIngress {
		fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman network create %s\n", IngressNetworkName)
	}

	// ExecStart: podman run on the shared/private network.
	fmt.Fprintf(&b, "ExecStart=/usr/bin/podman run --replace --name %s --systemd=true", containerName)
	if cfg.PullNever {
		b.WriteString(" \\\n  --pull=never")
	}
	fmt.Fprintf(&b, " \\\n  --net %s", networkName)
	if joinIngress {
		fmt.Fprintf(&b, " \\\n  --network %s", IngressNetworkName)
	}
	for _, alias := range cfg.NetworkAliases {
		b.WriteString(" \\\n  --network-alias " + quoteCommandArg(alias))
	}

	// Direct ports: the service container publishes them on the host itself,
	// bypassing the NC proxy. The container port comes from the External /
	// Internal map. (Dependencies never reach here — directHostPorts is empty
	// for them.)
	for _, p := range cfg.directHostPorts(ports) {
		container := p
		if cp, ok := cfg.External[p]; ok {
			container = cp
		} else if cp, ok := cfg.Internal[p]; ok {
			container = cp
		}
		fmt.Fprintf(&b, " \\\n  -p %d:%d", p, container)
	}

	// Extra podman args (e.g. --pid host, --cap-add). Each element is
	// one argv token to podman, so any caller that passes a value with
	// whitespace — e.g. `--mount type=bind,source=/a b/c` — gets the
	// whole token single-quoted rather than split by systemd.
	for _, arg := range cfg.ExtraArgs {
		b.WriteString(" \\\n  " + quoteCommandArg(arg))
	}

	// Environment variables, sorted by key. The KEY=VALUE form must be a
	// single argv element to podman, so any value containing whitespace
	// (e.g. POSTGRES_INITDB_ARGS="--encoding=UTF8 --lc-collate=C ...")
	// gets quoted at the KEY=VALUE level — that way systemd does not
	// split the value into separate argv tokens that podman then sees
	// as runaway flags ("unknown flag: --lc-collate").
	envKeys := make([]string, 0, len(cfg.Environment))
	for k := range cfg.Environment {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		b.WriteString(" \\\n  -e " + quoteCommandArg(k+"="+cfg.Environment[k]))
	}

	// Volume mounts. Dependency pkg names translate to the nested
	// storage path so the podman -v source matches what the install
	// manager created on disk. The full `host:container:opts` triplet
	// is one argv token to podman, so quote the whole thing in case a
	// host or container path contains whitespace — systemd would
	// otherwise split it and podman would see a stray -v value.
	for _, name := range volNames {
		vol := cfg.Volumes[name]
		hostPath := fmt.Sprintf("%s/installed/%s/%s/%s/%s", cfg.BtrfsBase, cfg.RepoName, packages.StoragePath(cfg.PkgName), cfg.Version, name)
		b.WriteString(" \\\n  -v " + quoteCommandArg(fmt.Sprintf("%s:%s:rw,z", hostPath, vol.Mountpoint)))
	}

	// Host volume mounts (arbitrary host paths, not btrfs-managed).
	for _, hv := range cfg.HostVolumeMounts {
		opts := hv.Options
		if opts == "" {
			opts = "rw,z"
		}
		b.WriteString(" \\\n  -v " + quoteCommandArg(fmt.Sprintf("%s:%s:%s", hv.HostPath, hv.ContainerPath, opts)))
	}

	// Entrypoint override. Encoded as a JSON array and single-quoted so
	// systemd forwards it verbatim to podman; the JSON form covers both
	// single- and multi-arg entrypoints (podman accepts ["sh","-c"] as
	// an array and treats a single-element array as a single-arg entrypoint).
	if len(cfg.Entrypoint) > 0 {
		b.WriteString(" \\\n  --entrypoint=" + quoteCommandArg(encodeEntrypointJSON(cfg.Entrypoint)))
	}

	if len(cfg.Command) > 0 {
		b.WriteString(" \\\n  " + cfg.Image)
		for _, arg := range cfg.Command {
			b.WriteString(" \\\n  " + quoteCommandArg(arg))
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
	// Per-package leaf certs live under the TLS directory; the NC reloads
	// them on every TLS handshake so rotation is picked up without a
	// listener restart. Mounted read-only: only the systemcontroller ever
	// writes here.
	if cfg.TLSDir != "" {
		fmt.Fprintf(&b, " \\\n  -v %s:/etc/town-os/tls:ro", cfg.TLSDir)
	}
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
		b.WriteString(" \\\n  " + quoteCommandArg(arg))
	}
	fmt.Fprintf(&b, " \\\n  %s", cfg.Image)
	for _, cmd := range cfg.Command {
		b.WriteString(" \\\n  " + quoteCommandArg(cmd))
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

// NetworkUnitConfig configures a per-network WireGuard interface unit.
type NetworkUnitConfig struct {
	Name       string // network name (used in Description and unit name)
	ConfigPath string // absolute host path to the <interface>.conf wg-quick config
}

// GenerateNetworkUnit produces a host-level systemd unit that brings a
// WireGuard interface up via wg-quick. The interface is derived by wg-quick from
// the config filename. The unit is oneshot + RemainAfterExit so systemctl
// start/stop maps to interface up/down: stopping the unit (network disabled)
// tears the interface down, cutting remote access while local DNS and the
// containers keep running. The systemcontroller writes ConfigPath (0600, it
// contains the private key) to the host-shared network-state dir before the
// unit starts.
func GenerateNetworkUnit(cfg NetworkUnitConfig) UnitFile {
	var b strings.Builder

	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=Town OS Network: %s\n", cfg.Name)
	b.WriteString("After=network-online.target\n")
	b.WriteString("Wants=network-online.target\n")
	b.WriteString("StartLimitIntervalSec=0\n")

	b.WriteString("\n[Service]\n")
	b.WriteString("Type=oneshot\n")
	b.WriteString("RemainAfterExit=yes\n")
	// Tear down any stale interface from a previous run before bringing it up.
	fmt.Fprintf(&b, "ExecStartPre=-%s down %s\n", WireGuardQuickPath, cfg.ConfigPath)
	fmt.Fprintf(&b, "ExecStart=%s up %s\n", WireGuardQuickPath, cfg.ConfigPath)
	fmt.Fprintf(&b, "ExecStop=-%s down %s\n", WireGuardQuickPath, cfg.ConfigPath)
	b.WriteString("Restart=on-failure\n")

	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")

	return UnitFile{
		Name:    NetworkUnitName(cfg.Name),
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

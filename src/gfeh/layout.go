// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package gfeh

import (
	"path/filepath"
	"strings"
)

// Where a partition's things live, on the host and inside the container.
//
// One partition per Town OS network, named for it. Kept in this package rather
// than in gfehctl because the reconcile path, the DNS collector, and the UI
// proxy all need to resolve the same paths, and three copies of a join is how
// a socket ends up dialled at a path nothing is listening on.

const (
	// VolumePrefix is the reserved object-storage root every partition's
	// subvolume sits under. Mirrors GfehVolumePrefix in the systemcontroller
	// package, which is the one the storage handlers enforce.
	VolumePrefix = "gfeh"

	// ControlDirName holds per-partition control state — the rendered config
	// and the admin socket.
	//
	// Deliberately a sibling of the object-storage root rather than a
	// directory inside it: anything under VolumePrefix is a partition's data,
	// and a control file that landed there would be an object somebody could
	// list, fetch, or overwrite through S3.
	ControlDirName = "gfeh-control"
)

// DefaultNetworkName is the network a partition belongs to when none is
// named. Mirrors account.DefaultNetworkName, duplicated here so this package
// stays free of the account package (and so of the whole database layer) for
// one string.
const DefaultNetworkName = "home"

// SMBFallbackPrincipal is the principal an SMB session acts as when no
// credential table is configured.
//
// It is granted nothing at provision time and is expected to stay that way: a
// listener with no credentials verifies nothing, so anything this principal
// holds is available to every host that can reach the port.
const SMBFallbackPrincipal = "smb-unverified"

// IsDefaultNetwork reports whether a network is the default/home one.
//
// The empty string counts, matching Installer.LoadNetwork's convention that an
// unset network means the default.
func IsDefaultNetwork(network string) bool {
	return network == "" || network == DefaultNetworkName
}

// PartitionVolume is the btrfs subvolume name for a network's partition,
// relative to the btrfs base — the form storage.Storage takes.
func PartitionVolume(network string) string {
	return VolumePrefix + "/" + network
}

// PartitionDir is the host path of a network's partition subvolume.
func PartitionDir(btrfsBase, network string) string {
	return filepath.Join(btrfsBase, VolumePrefix, network)
}

// ControlDir is the host path holding a network's config and socket.
func ControlDir(btrfsBase, network string) string {
	return filepath.Join(btrfsBase, ControlDirName, network)
}

// ConfigDir is the host directory mounted read-only at ContainerConfigDir.
func ConfigDir(btrfsBase, network string) string {
	return filepath.Join(ControlDir(btrfsBase, network), "config")
}

// ConfigPath is the host path of a network's rendered gfehd.yaml.
func ConfigPath(btrfsBase, network string) string {
	return filepath.Join(ConfigDir(btrfsBase, network), ConfigName)
}

// RunDir is the host directory mounted read-write at ContainerRunDir.
func RunDir(btrfsBase, network string) string {
	return filepath.Join(ControlDir(btrfsBase, network), "run")
}

// SocketPath is the host path of a network's admin socket, which is what the
// systemcontroller dials.
func SocketPath(btrfsBase, network string) string {
	return filepath.Join(RunDir(btrfsBase, network), AdminSocketName)
}

// ContainerPartitionDir is where the partition is mounted inside the
// container, and therefore what gfehd's partition_dir() resolves to.
func ContainerPartitionDir(network string) string {
	return ContainerDataDir + "/" + network
}

// KeyPrefix distinguishes a gfeh unit from every other system service.
//
// The separator is a single dash rather than the double dash Town OS uses
// between the system prefix and a key: SystemServiceUnitName already prepends
// "town-os-system--", so a unit is town-os-system--gfeh-<network>.service and
// there is exactly one "--" in it.
const KeyPrefix = "gfeh-"

// ServiceKey is the system-service key for a network's gfehd.
//
// Round-trips through NetworkFromKey because network names are DNS-label-safe
// (lowercase alphanumeric and dashes, validated by the account package), so a
// name can neither contain the prefix's separator ambiguously nor collide with
// another service's key.
func ServiceKey(network string) string { return KeyPrefix + network }

// NetworkFromKey recovers the network a gfeh service key names.
//
// Used by the teardown pass, which has to decide from a unit name alone
// whether a running daemon still corresponds to a network that exists.
func NetworkFromKey(key string) (string, bool) {
	network, ok := strings.CutPrefix(key, KeyPrefix)
	if !ok || network == "" {
		return "", false
	}
	return network, true
}

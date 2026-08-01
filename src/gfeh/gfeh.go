// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

// Package gfeh is the Town OS side of the object-storage boundary: the config
// gfehd is given, and the client for the administrative surface it serves.
//
// The split of responsibility is not negotiable and is stated in CLAUDE.md:
// src/storage manages btrfs subvolumes and quotas, and gfeh owns objects,
// per-file metadata and permissions, the user/ACL forest, sharing, and every
// protocol view. Nothing in this package moves object bytes — gfeh does direct
// I/O on the bind-mounted subtree, and routing that through the system
// controller would mean a proxy hop per byte and an object API in Town OS that
// has no business existing.
//
// # Why Town OS asks gfeh for its names instead of gfeh registering them
//
// Both of Town OS's reconcilers destroy foreign state: RebuildIngress calls
// SetRoutes with the full derived set, and RebuildDNS calls TeardownTLD, which
// deletes every record under the zone. So a hostname gfeh programmed directly
// would survive exactly until the next reconcile. The direction is therefore
// inverted — gfeh *answers* for its names on GET /v1/names, and Town OS folds
// the answer into what it is about to derive. That asymmetry is the guarantee,
// and it is worth more as an absence of code than as a flag somebody can flip.
//
// This package is deliberately free of any dependency on src/systemd (and so
// of cgo, which src/systemd pulls in via sdjournal). The lifecycle controller
// that does need systemd lives in the gfehctl subpackage, the same split
// ingressctl makes for the same reason.
package gfeh

import "slices"

const (
	// UID and GID are the account gfehd runs as inside its container, and
	// therefore the owner a partition's subvolume must carry — a bind mount
	// passes host ownership straight through, so a subvolume owned by root is
	// one the daemon cannot write to.
	//
	// Unlike the Prometheus and Grafana uids, which are pinned by their
	// upstream images, this one is ours: Containerfile.gfeh creates the
	// account. Changing it means changing both, and chowning every existing
	// partition.
	UID uint32 = 2000
	GID uint32 = 2000

	// AdminSocketName is the Unix socket gfehd binds its administrative
	// surface to. A socket and never a port: the boundary between the admin
	// surface and the user surfaces is a routing property rather than a
	// conditional somebody can get wrong, so filesystem permissions are the
	// whole access control.
	AdminSocketName = "admin.sock"

	// ConfigName is the rendered gfehd configuration file.
	ConfigName = "gfehd.yaml"

	// ContainerDataDir is gfehd's data_dir. The partition's own subvolume is
	// mounted at ContainerDataDir/<partition>, which is exactly where gfehd
	// looks (partition_dir() is data_dir/partition) — so each container sees
	// its own partition and no other, rather than the whole object-storage
	// root with one directory it is supposed to stay inside.
	ContainerDataDir = "/data"

	// ContainerConfigDir holds the rendered gfehd.yaml, mounted read-only.
	// gfehd has no business rewriting a file Town OS derives on every
	// reconcile.
	ContainerConfigDir = "/etc/gfeh"

	// ContainerConfigPath is where gfehd reads its config. Baked into the
	// image's CMD, so no unit has to pass it and the image can keep CMD rather
	// than ENTRYPOINT.
	ContainerConfigPath = ContainerConfigDir + "/" + ConfigName

	// ContainerRunDir holds the admin socket, mounted read-write.
	//
	// It is backed by a host directory on the btrfs because that is the one
	// filesystem both the gfehd container and the systemcontroller container
	// can see — the same trick ingressctl uses for its gRPC socket, and what
	// lets the systemcontroller dial a socket a different container created.
	ContainerRunDir = "/run/gfeh"

	// ContainerSocketPath is the admin socket inside the container.
	ContainerSocketPath = ContainerRunDir + "/" + AdminSocketName
)

// View names, as gfehd reports them in the `view` field of a name entry.
const (
	ViewS3    = "s3"
	ViewHTTP  = "http"
	ViewDrive = "drive"
	ViewIPFS  = "ipfs"
	ViewSMB   = "smb"
)

// Container-side ports for the four HTTP views.
//
// These are fixed and identical for every partition, which is safe precisely
// because the views publish no host port: each partition's container has its
// own network namespace and the ingress reaches it by container name, exactly
// as it reaches a package. Two partitions can therefore both serve S3 on 9000
// without colliding — including under a concurrent `make test-full`, which is
// the IRON RULE.
//
// SMB is absent on purpose. It is not HTTP, so it cannot sit behind the
// ingress, and it is the one view that needs a real host port; gfehctl assigns
// that per network.
const (
	PortS3    = 9000
	PortHTTP  = 9001
	PortDrive = 9002
	PortIPFS  = 9003
)

// HTTPViews are the views the ingress can front, in the order they are
// published. SMB is excluded — see the port block above.
var HTTPViews = []string{ViewS3, ViewHTTP, ViewDrive, ViewIPFS}

// IsHTTPView reports whether a view named by gfehd is one the ingress fronts.
// An unknown view is treated as not-HTTP: contributing an ingress route for
// something that does not speak HTTP produces a vhost that accepts a TLS
// handshake and then fails, which is worse than no route at all.
func IsHTTPView(view string) bool {
	return slices.Contains(HTTPViews, view)
}

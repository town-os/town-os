// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package gfeh

// The wire types of gfehd's administrative surface, field-for-field with
// crates/gfehd/src/admin.rs. gfehd serves JSON over HTTP on a Unix socket
// (a documented divergence from the gRPC its design called for — the invariant
// that matters, admin-over-filesystem and users-over-network, is unchanged).

// Health is GET /v1/health.
type Health struct {
	Status      string `json:"status"`
	Partition   string `json:"partition"`
	PartitionID string `json:"partition_id"`
	// No town_os field is decoded. Town OS cannot render that section, so the
	// answer is always false and reporting it would only invite somebody to
	// branch on it.
}

// Name is one hostname gfeh asks Town OS to publish.
type Name struct {
	// Hostname is a LABEL, relative to whatever zone the partition's network
	// resolves under. It is never an FQDN: the zone follows from the network,
	// Town OS is the authority on that mapping, and a gfeh that composed the
	// fully-qualified name would be a gfeh with opinions about zones.
	Hostname string `json:"hostname"`
	// View is s3, http, drive, ipfs or smb.
	View string `json:"view"`
	// Port is what the view is bound to. Note this means two different things
	// depending on the view: for the four HTTP views it is a container-side
	// backend port the ingress proxies to (the published port is always 443),
	// and for SMB — which cannot sit behind an HTTP router — it is the real
	// host port. The asymmetry is confined to the collector that reads this.
	Port uint16 `json:"port"`
}

// NameList is GET /v1/names: everything Town OS needs to derive this
// partition's records and routes.
type NameList struct {
	Partition string `json:"partition"`
	// Network is absent, not empty, when unset, and absent means the default
	// network. A pointer because those are different requests — an empty
	// string would ask for a zone called "".
	Network *string `json:"network,omitempty"`
	// Names holds one entry per view actually being served. A view with no
	// bind address contributes nothing, and neither does one on an ephemeral
	// port: the list is what should exist, not what could.
	Names []Name `json:"names"`
}

// NetworkName resolves the pointer to the network name Town OS knows,
// substituting the default network for an absent value.
func (n NameList) NetworkName(defaultNetwork string) string {
	if n.Network == nil || *n.Network == "" {
		return defaultNetwork
	}
	return *n.Network
}

// Principal is a member of the partition's ACL forest. Town OS accounts are
// the roots; everything below a root is self-service inside gfeh.
type Principal struct {
	Name string `json:"name"`
	// Parent is the principal that created this one, absent for a root.
	Parent *string `json:"parent,omitempty"`
	// Ceiling is the most authority this principal can ever hold, by name.
	// gfeh clamps every grant to it.
	Ceiling []string `json:"ceiling"`
}

// Grant is one principal's authority over one subtree.
type Grant struct {
	// ID is the row identity, and the handle for revocation.
	ID int64 `json:"id"`
	// Principal is who holds it.
	Principal string `json:"principal"`
	// Path is the subtree it covers, relative to the partition root.
	Path string `json:"path"`
	// Perm is the rights granted, by name. On the way back this is what the
	// grant was clamped to, not what was asked for — an administrator has to
	// be able to see that a grant was narrowed, or they will believe they gave
	// access nobody has.
	Perm []string `json:"perm"`
	// Inheritable extends the grant beneath Path.
	Inheritable bool `json:"inheritable"`
}

// Exposure is a published file: a token that appears in a /f/<token> URL.
type Exposure struct {
	Token    string  `json:"token"`
	Path     string  `json:"path"`
	Filename *string `json:"filename,omitempty"`
	Enabled  bool    `json:"enabled"`
}

// Permission names gfeh understands, from crates/gfehd/src/perm.rs. Listed
// here so the UI can offer them without a round trip and so a typo is a
// compile error on this side rather than a 400 from the daemon.
const (
	PermRead          = "read"
	PermList          = "list"
	PermCreate        = "create"
	PermWrite         = "write"
	PermDelete        = "delete"
	PermMetaRead      = "meta-read"
	PermMetaWrite     = "meta-write"
	PermShare         = "share"
	PermAdminACL      = "admin-acl"
	PermCreateSubuser = "create-subuser"
	PermReadAudit     = "read-audit"
	PermFederate      = "federate"
	PermPublishHTTP   = "publish-http"
	PermPublishIPFS   = "publish-ipfs"
	PermSnapshot      = "snapshot"
	PermQuota         = "quota"
)

// Aggregate ceilings gfeh accepts wherever a permission list is taken.
const (
	PermReadOnly  = "read-only"
	PermReadWrite = "read-write"
	PermAll       = "all"
)

// AllPerms is every individual right, in the order the UI should offer them.
var AllPerms = []string{
	PermRead, PermList, PermCreate, PermWrite, PermDelete,
	PermMetaRead, PermMetaWrite, PermShare, PermAdminACL,
	PermCreateSubuser, PermReadAudit, PermFederate,
	PermPublishHTTP, PermPublishIPFS, PermSnapshot, PermQuota,
}

// CeilingForAccount is the authority a Town OS account projects into a
// partition. A Town OS administrator is a gfeh superuser — they create the
// roots of the forest — and an ordinary account gets an ordinary ceiling and
// no grants at all, which is deliberately useless until somebody grants it
// something. Authenticating is not authorization.
func CeilingForAccount(admin bool) []string {
	if admin {
		return []string{PermAll}
	}
	return []string{PermReadWrite}
}

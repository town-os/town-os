package account

import "context"

// DefaultSettings are seeded into the database on first init.
// Existing values are never overwritten. Build-tagged additions (e.g.
// proton_image when the `proton` tag is set) register themselves in init().
var DefaultSettings = map[string]string{
	"default_quota":          "53687091200", // 50 GB
	"max_archive_size":       "1073741824",  // 1 GB
	"archive_unpack_timeout": "600",         // seconds (10 min)
	"locale":                 "en-US",       // BCP 47 locale code
	"dns_tld":                "home",        // Default top-level domain for package DNS records
	// How rolodex resolves names it is not authoritative for:
	//   "auto"      -- try the root servers first, then DoH/DoT, then the
	//                  upstream forwarders, then a public resolver on :53,
	//                  sticking to whichever tier last worked.
	//   "recursive" -- iterate from the root servers and nothing else. No
	//                  fallback: on a network that filters or hijacks outbound
	//                  :53, every external name SERVFAILs.
	//   "forward"   -- forward to the upstream resolvers in rolodex.yml.
	// Auto is the default because it keeps recursion's privacy wherever the
	// network permits it and degrades instead of failing where it does not.
	"dns_resolution_mode": "auto",
	// Whether rolodex's forwarder list is taken from the resolvers this box's
	// own network handed it (its DHCP-provided servers) instead of the public
	// defaults written into rolodex.yml.
	//
	// It is what makes resolution survive a network that blocks external DNS:
	// a hotel, a captive portal, or an ISP that drops outbound :53 to anywhere
	// but its own servers still answers queries sent to the resolver it
	// advertised, while the public forwarders the default list names are
	// exactly the addresses such a network drops. In "auto" that tier is
	// reached only after the roots and the encrypted upstreams have failed —
	// which is that case and no other.
	//
	// Off by default because the local resolver sees every name the household
	// looks up, which is the thing resolving from the roots exists to avoid.
	// That is a trade an operator makes knowingly, not one a box makes for
	// them the first time a network misbehaves.
	"dns_local_forwarders": "false",
	// peer_ttl is how long, in seconds, a WireGuard peer enrollment stays valid
	// before the reaper removes it. Stored as raw seconds to match the other
	// duration settings (archive_unpack_timeout). A long-lived client (the
	// portal) refreshes its peer before this elapses; an abandoned device's
	// peer expires on its own, so the additive peers/add endpoint cannot silently
	// accumulate dead peers and burn overlay addresses.
	"peer_ttl": "7200", // 2 hours

	// The quota a gfeh partition's subvolume is created with, in bytes.
	//
	// Zero is unlimited, and is the default deliberately: a partition is the
	// box's object storage for a whole network, and capping it at the
	// per-user volume default would surprise somebody the first time a photo
	// library outgrew it. An operator who wants a limit sets one.
	"gfeh_partition_quota": "0",
}

// Object storage has no on/off setting. Storing files is what the box is for,
// so it runs the way DNS and the ingress run: as part of what Town OS is,
// not as a feature to be enabled. A switch bought only the chance to be found
// in the off position while somebody debugged why their files were gone, and
// an administrator who genuinely wants the daemons down can stop the services
// from the services panel, where every other system service is stopped.
//
// The dev-mode escape hatches remain, because they are about a *build* rather
// than about policy: an explicitly empty GFEH_IMAGE skips object storage, and
// it is skipped when the ingress is disabled, since the HTTP views are only
// reachable through it.

// SettingsManager reads and writes the key/value settings table.
//
// Every method takes a context, and that is not decoration. The SQLite managers
// used to open their own 30-second root context per query (dbCtx), so nothing a
// caller did could cancel or bound a query: an abandoned HTTP request kept
// working, and graceful shutdown could not interrupt one. With
// SetMaxOpenConns(1) — SQLite allows a single writer, so every query is
// serialized behind that one connection — a slow query held every other caller
// behind an uninterruptible 30-second ceiling.
//
// dbTimeout survives as a *ceiling*, applied on top of whatever the caller
// passed, so a caller with no deadline of its own still cannot hang forever.
//
// Handlers pass the request context. Background goroutines pass the
// server-scoped context, never a request's — the operation must outlive the
// request that triggered it (see the Performance Conventions in CLAUDE.md).
type SettingsManager interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
	List(ctx context.Context) (map[string]string, error)
}

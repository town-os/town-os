package account

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

type SettingsManager interface {
	Get(key string) (string, error)
	Set(key, value string) error
	List() (map[string]string, error)
}

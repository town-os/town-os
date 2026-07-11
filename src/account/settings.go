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
}

type SettingsManager interface {
	Get(key string) (string, error)
	Set(key, value string) error
	List() (map[string]string, error)
}

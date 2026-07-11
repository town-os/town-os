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
	//   "recursive" -- iterate from the root servers (private, no third party
	//                  sees your queries, but every cache miss pays a full
	//                  root -> TLD -> authoritative walk).
	//   "forward"   -- forward to the upstream resolvers in rolodex.yml
	//                  (one hop; much lower latency on a cold cache).
	// Recursive is the default so the box does not depend on, or leak queries
	// to, a third-party resolver out of the box.
	"dns_resolution_mode": "recursive",
}

type SettingsManager interface {
	Get(key string) (string, error)
	Set(key, value string) error
	List() (map[string]string, error)
}

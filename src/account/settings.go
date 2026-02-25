package account

// DefaultSettings are seeded into the database on first init.
// Existing values are never overwritten.
var DefaultSettings = map[string]string{
	"default_quota":          "53687091200", // 50 GB
	"max_archive_size":       "20971520",    // 20 MB
	"archive_unpack_timeout": "120",         // seconds
}

type SettingsManager interface {
	Get(key string) (string, error)
	Set(key, value string) error
	List() (map[string]string, error)
}

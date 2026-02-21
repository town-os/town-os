package account

// DefaultSettings are seeded into the database on first init.
// Existing values are never overwritten.
var DefaultSettings = map[string]string{
	"default_quota": "53687091200", // 50 GB
}

type SettingsManager interface {
	Get(key string) (string, error)
	Set(key, value string) error
	List() (map[string]string, error)
}

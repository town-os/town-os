package account

type SettingsManager interface {
	Get(key string) (string, error)
	Set(key, value string) error
	List() (map[string]string, error)
}

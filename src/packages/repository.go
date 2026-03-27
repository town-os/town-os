package packages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gitea.com/town-os/town-os/src/git"
	"github.com/sirupsen/logrus"
)

// DefaultRepoURL is the default package repository URL. It can be overridden
// at build time via ldflags (e.g. -ldflags "-X gitea.com/town-os/town-os/src/packages.DefaultRepoURL=...").
var DefaultRepoURL = "https://github.com/town-os/default-packages"

const RepositoriesFile = "repositories.json"
const LastRefreshedFile = "last_refreshed"
const DefaultRefreshInterval = 5 * time.Minute

// DefaultRepositories returns the default package repositories to seed when
// no repositories have been configured.
func DefaultRepositories() []Repository {
	u, err := url.Parse(DefaultRepoURL)
	if err != nil {
		panic("invalid default repository URL: " + err.Error())
	}
	return []Repository{
		{Name: "default", URL: *u},
	}
}

const (
	EnvTestRepoCoreURL   = "TOWN_OS_TEST_REPO_CORE_URL"
	EnvTestRepoExtrasURL = "TOWN_OS_TEST_REPO_EXTRAS_URL"
)

// TestRepositories returns the test package repositories used in development.
// URLs can be overridden via TOWN_OS_TEST_REPO_CORE_URL and
// TOWN_OS_TEST_REPO_EXTRAS_URL environment variables (e.g. to point at a
// local Gitea instance).
func TestRepositories() []Repository {
	coreRaw := os.Getenv(EnvTestRepoCoreURL)
	if coreRaw == "" {
		coreRaw = "https://github.com/town-os/test-packages-core"
	}
	extrasRaw := os.Getenv(EnvTestRepoExtrasURL)
	if extrasRaw == "" {
		extrasRaw = "https://github.com/town-os/test-packages-extras"
	}

	core, err := url.Parse(coreRaw)
	if err != nil {
		panic("invalid core repository URL: " + err.Error())
	}
	extras, err := url.Parse(extrasRaw)
	if err != nil {
		panic("invalid extras repository URL: " + err.Error())
	}
	return []Repository{
		{Name: "core", URL: *core},
		{Name: "extras", URL: *extras},
	}
}

type RepositoryManager interface {
	Add(repo Repository) error
	Remove(name string) error
	Get(name string) (Repository, bool)
	List() ([]Repository, error)
	Refresh()
	ForceRefresh()
	RefreshErrors() map[string]string
	LoadAllPackages() (PackageTable, error)
	ListPackages() ([]string, error)
	ListPackageVersions(name string) ([]string, error)
	LatestPackage(name string) (InputPackage, string, error)
	GetPackageQuestions(name string) (map[string]Question, error)
	FindRepoForPackage(name, version string) (string, error)
	ListPackagesByRepo() ([]RepoPackageGroup, error)
	Move(name string, position int) error
}

type RepositoryRoot struct {
	BaseDir         string
	Items           []Repository
	Errors          map[string]string
	LastRefreshed   time.Time
	RefreshInterval time.Duration
	Git             git.Client
	pkgCache        sync.Map // repo name → PackageTable
}

func RepositoryRootFromBase(baseDir string) (_ *RepositoryRoot, err error) {
	fn, err := SafePath(baseDir, RepositoriesFile)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(fn) //nolint:gosec // G304 -- fn from SafePath
	if err != nil {
		return nil, err
	}

	defer func() {
		err = errors.Join(err, f.Close())
	}()

	var items []Repository
	de := json.NewDecoder(f)
	err = de.Decode(&items)
	if err != nil {
		return nil, err
	}

	rr := &RepositoryRoot{
		BaseDir:         baseDir,
		Items:           items,
		RefreshInterval: DefaultRefreshInterval,
		Git:             &git.GoGitClient{Home: baseDir},
	}

	rr.loadLastRefreshed()

	return rr, nil
}

func (rr *RepositoryRoot) save() (err error) {
	lock, err := lockDir(rr.BaseDir)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, lock.Unlock())
	}()

	fn := filepath.Join(rr.BaseDir, RepositoriesFile)
	return atomicWriteJSON(fn, rr.Items)
}

func (rr *RepositoryRoot) Add(repo Repository) error {
	for _, r := range rr.Items {
		if r.Name == repo.Name {
			return fmt.Errorf("repository %s already exists", repo.Name)
		}
	}

	rr.Items = append(rr.Items, repo)
	return rr.save()
}

func (rr *RepositoryRoot) Remove(name string) error {
	for i, r := range rr.Items {
		if r.Name == name {
			rr.Items = append(rr.Items[:i], rr.Items[i+1:]...)
			return rr.save()
		}
	}

	return fmt.Errorf("repository %s not found", name)
}

func (rr *RepositoryRoot) Move(name string, position int) error {
	idx := -1
	for i, r := range rr.Items {
		if r.Name == name {
			idx = i
			break
		}
	}

	if idx == -1 {
		return fmt.Errorf("repository %s not found", name)
	}

	repo := rr.Items[idx]
	rr.Items = append(rr.Items[:idx], rr.Items[idx+1:]...)

	if position < 0 {
		position = 0
	}
	if position > len(rr.Items) {
		position = len(rr.Items)
	}

	rr.Items = append(rr.Items[:position], append([]Repository{repo}, rr.Items[position:]...)...)

	return rr.save()
}

func (rr *RepositoryRoot) List() ([]Repository, error) {
	out := make([]Repository, len(rr.Items))
	copy(out, rr.Items)
	return out, nil
}

func (rr *RepositoryRoot) Get(name string) (Repository, bool) {
	for _, r := range rr.Items {
		if r.Name == name {
			return r, true
		}
	}

	return Repository{}, false
}

func (rr *RepositoryRoot) Refresh() {
	if time.Since(rr.LastRefreshed) < rr.RefreshInterval {
		return
	}
	rr.forceRefresh()
}

func (rr *RepositoryRoot) ForceRefresh() {
	rr.forceRefresh()
}

// InvalidatePackageCache clears the cached package tables so the next
// LoadPackages call re-reads from disk.
func (rr *RepositoryRoot) InvalidatePackageCache() {
	rr.pkgCache.Range(func(key, _ any) bool {
		rr.pkgCache.Delete(key)
		return true
	})
}

func (rr *RepositoryRoot) forceRefresh() {
	rr.InvalidatePackageCache()
	rr.Errors = map[string]string{}
	for i := range rr.Items {
		err := rr.Items[i].init(rr.BaseDir, rr.Git)
		if err != nil {
			logrus.Warnf("repository %s: %v", rr.Items[i].Name, err)
			rr.Errors[rr.Items[i].Name] = err.Error()
		}
	}
	rr.LastRefreshed = time.Now()
	if err := rr.saveLastRefreshed(); err != nil {
		logrus.Warnf("failed to save last-refreshed timestamp: %v", err)
	}
}

func (rr *RepositoryRoot) loadLastRefreshed() {
	fn, err := SafePath(rr.BaseDir, LastRefreshedFile)
	if err != nil {
		return
	}
	data, err := os.ReadFile(fn) //nolint:gosec // G304 -- fn from SafePath
	if err != nil {
		return
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return
	}
	rr.LastRefreshed = t
}

func (rr *RepositoryRoot) saveLastRefreshed() error {
	fn := filepath.Join(rr.BaseDir, LastRefreshedFile)
	return os.WriteFile(fn, []byte(rr.LastRefreshed.Format(time.RFC3339)+"\n"), 0600)
}

func (rr *RepositoryRoot) RefreshErrors() map[string]string {
	return rr.Errors
}

type Repository struct {
	Name     string
	URL      url.URL
	Username string
	Password string //nolint:gosec // G117 -- credential field, not hardcoded
}

type repositoryJSON struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func (r *Repository) MarshalJSON() ([]byte, error) {
	return json.Marshal(repositoryJSON{Name: r.Name, URL: r.credentialURL()})
}

func (r *Repository) UnmarshalJSON(data []byte) error {
	// Try object format first.
	var obj repositoryJSON
	err := json.Unmarshal(data, &obj)
	if err == nil && obj.Name != "" {
		parsed, err := url.Parse(obj.URL)
		if err != nil {
			return fmt.Errorf("invalid repository URL: %w", err)
		}

		r.Name = obj.Name
		if parsed.User != nil {
			r.Username = parsed.User.Username()
			r.Password, _ = parsed.User.Password()
			parsed.User = nil
		}
		r.URL = *parsed
		return nil
	}

	// Legacy fallback: [name, credentialURL] array format.
	var pair [2]string
	err = json.Unmarshal(data, &pair)
	if err != nil {
		return errors.New("invalid repository JSON: expected {name, url} object or [name, url] array")
	}

	parsed, err := url.Parse(pair[1])
	if err != nil {
		return fmt.Errorf("invalid repository URL: %w", err)
	}

	r.Name = pair[0]
	if parsed.User != nil {
		r.Username = parsed.User.Username()
		r.Password, _ = parsed.User.Password()
		parsed.User = nil
	}
	r.URL = *parsed
	return nil
}

func (r *Repository) credentialURL() string {
	if r.Username == "" {
		return r.URL.String()
	}
	u := r.URL
	u.User = url.UserPassword(r.Username, r.Password)
	logrus.Println(SanitizeURL(u.String()))
	return u.String()
}

// SanitizeURL replaces userinfo credentials in a URL with placeholders.
func SanitizeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	username := parsed.User.Username()
	_, hasPassword := parsed.User.Password()
	if username == "" && !hasPassword {
		return raw
	}
	parsed.User = url.UserPassword("USERNAME", "PASSWORD")
	return parsed.String()
}

const (
	EnvRepoUsername = "TOWN_OS_REPO_USERNAME"
	EnvRepoPassword = "TOWN_OS_REPO_PASSWORD" //nolint:gosec // G101 -- env var name, not a credential
)

var ErrPartialCredentials = errors.New("both username and password must be provided together")

func NewRepository(baseDir, name string, u url.URL, username, password string, g git.Client) (*Repository, error) {
	if (username == "") != (password == "") {
		return nil, ErrPartialCredentials
	}
	if name == "" {
		name = strings.TrimSuffix(path.Base(u.Path), ".git")
	}
	r := &Repository{Name: name, URL: u, Username: username, Password: password}
	return r, r.init(baseDir, g)
}

func (r *Repository) init(baseDir string, g git.Client) error {
	target := filepath.Join(baseDir, r.Name)
	ctx := context.Background()

	s, err := os.Stat(target)
	switch {
	case os.IsNotExist(err):
		if err := g.Clone(ctx, baseDir, r.credentialURL(), r.Name); err != nil {
			return err
		}
	case err != nil:
		return err
	case !s.IsDir():
		return fmt.Errorf("sub-path %s is not a directory", target)
	default:
		// Check if this is a valid git repository by looking for .git.
		if _, err := os.Stat(filepath.Join(target, ".git")); os.IsNotExist(err) {
			// Directory exists but is not a git repo; remove and clone fresh.
			if err := os.RemoveAll(target); err != nil {
				return fmt.Errorf("remove non-git directory %s: %w", target, err)
			}
			return g.Clone(ctx, baseDir, r.credentialURL(), r.Name)
		}

		needsStash, err := g.Diff(ctx, target)
		if err != nil {
			return err
		}

		if needsStash {
			if err := g.Stash(ctx, target); err != nil {
				return err
			}
		}

		if err := g.Pull(ctx, target); err != nil {
			return err
		}

		if needsStash {
			if err := g.StashApply(ctx, target); err != nil {
				return err
			}
		}
	}

	return nil
}

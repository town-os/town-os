package packages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/git"
	"github.com/sirupsen/logrus"
	"go.yaml.in/yaml/v4"
)

const RepositoriesFile = "repositories.json"
const LastRefreshedFile = "last_refreshed"
const DefaultRefreshInterval = 5 * time.Minute

// DefaultRepositories returns the default package repositories to seed when
// no repositories have been configured.
func DefaultRepositories() []Repository {
	u, err := url.Parse("https://github.com/town-os/default-packages")
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
}

func RepositoryRootFromBase(baseDir string) (_ *RepositoryRoot, err error) {
	fn, err := SafePath(baseDir, RepositoriesFile)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(fn) //nolint:gosec // fn validated by SafePath above
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

func (rr *RepositoryRoot) forceRefresh() {
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
	data, err := os.ReadFile(fn) //nolint:gosec // fn validated by SafePath above
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
	Password string //nolint:gosec // G117: expected field name
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
	EnvRepoPassword = "TOWN_OS_REPO_PASSWORD" //nolint:gosec // not credentials, struct field name
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

const PackagesDir = "packages"

type PackageTable map[string]map[string]InputPackage

func (r *Repository) LoadPackages(baseDir string) (PackageTable, error) {
	pkgs := PackageTable{}

	packagesDir := filepath.Join(baseDir, r.Name, PackagesDir)
	names, err := os.ReadDir(packagesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return pkgs, nil
		}
		return nil, err
	}

	for _, name := range names {
		if !name.IsDir() {
			continue
		}

		nameDir := filepath.Join(packagesDir, name.Name())
		versions, err := os.ReadDir(nameDir)
		if err != nil {
			return nil, err
		}

		for _, version := range versions {
			if version.IsDir() {
				continue
			}

			fn := version.Name()
			if !strings.HasSuffix(fn, ".yaml") {
				continue
			}

			safeFn, err := SafePath(nameDir, fn)
			if err != nil {
				return nil, err
			}
			f, err := os.Open(safeFn) //nolint:gosec // safeFn validated by SafePath above
			if err != nil {
				return nil, err
			}

			var ip InputPackage
			de := yaml.NewDecoder(f)
			err = errors.Join(de.Decode(&ip), f.Close())
			if err != nil {
				return nil, fmt.Errorf("decoding %s/%s: %w", name.Name(), fn, err)
			}

			if pkgs[name.Name()] == nil {
				pkgs[name.Name()] = map[string]InputPackage{}
			}

			pkgs[name.Name()][strings.TrimSuffix(fn, ".yaml")] = ip
		}
	}

	return pkgs, nil
}

func (rr *RepositoryRoot) LoadAllPackages() (PackageTable, error) {
	all := PackageTable{}

	for _, repo := range rr.Items {
		pkgs, err := repo.LoadPackages(rr.BaseDir)
		if err != nil {
			return nil, fmt.Errorf("repository %s: %w", repo.Name, err)
		}

		for name, versions := range pkgs {
			if all[name] == nil {
				all[name] = map[string]InputPackage{}
			}

			maps.Copy(all[name], versions)
		}
	}

	return all, nil
}

// LoadPackage loads a single InputPackage from a repository by name and version.
func (rr *RepositoryRoot) LoadPackage(repoName, pkgName, version string) (_ InputPackage, err error) {
	fn, err := SafePath(rr.BaseDir, repoName, PackagesDir, pkgName, version+".yaml")
	if err != nil {
		return InputPackage{}, err
	}
	f, err := os.Open(fn) //nolint:gosec // fn validated by SafePath above
	if err != nil {
		return InputPackage{}, fmt.Errorf("package %s@%s not found: %w", pkgName, version, err)
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()

	var ip InputPackage
	err = yaml.NewDecoder(f).Decode(&ip)
	if err != nil {
		return InputPackage{}, fmt.Errorf("decoding %s@%s: %w", pkgName, version, err)
	}

	return ip, nil
}

// CompareVersions compares two dot-separated version strings.
// Each segment is compared numerically when both are valid integers,
// otherwise lexicographically. Returns -1, 0, or 1.
func CompareVersions(a, b string) int {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	maxParts := max(len(partsA), len(partsB))

	for i := range maxParts {
		var sa, sb string
		if i < len(partsA) {
			sa = partsA[i]
		}
		if i < len(partsB) {
			sb = partsB[i]
		}

		na, errA := strconv.ParseInt(sa, 10, 64)
		nb, errB := strconv.ParseInt(sb, 10, 64)

		if errA == nil && errB == nil {
			if na < nb {
				return -1
			}
			if na > nb {
				return 1
			}
		} else {
			if sa < sb {
				return -1
			}
			if sa > sb {
				return 1
			}
		}
	}

	return 0
}

// ListPackageVersions returns all available versions of a named package across
// all repositories, sorted highest-to-lowest.
func (rr *RepositoryRoot) ListPackageVersions(name string) ([]string, error) {
	seen := map[string]bool{}

	for _, repo := range rr.Items {
		pkgs, err := repo.LoadPackages(rr.BaseDir)
		if err != nil {
			return nil, fmt.Errorf("repository %s: %w", repo.Name, err)
		}

		versions, ok := pkgs[name]
		if !ok {
			continue
		}

		for version := range versions {
			seen[version] = true
		}
	}

	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}

	sort.Slice(out, func(i, j int) bool {
		return CompareVersions(out[i], out[j]) > 0
	})

	return out, nil
}

var ErrPackageNotFound = errors.New("package not found")

// LatestPackage finds the latest version of a named package across all
// repositories. Repositories are consulted in order; when two repositories
// carry the same highest version the one listed first wins.
func (rr *RepositoryRoot) LatestPackage(name string) (InputPackage, string, error) {
	var bestPkg InputPackage
	var bestVersion string
	found := false

	for _, repo := range rr.Items {
		pkgs, err := repo.LoadPackages(rr.BaseDir)
		if err != nil {
			return InputPackage{}, "", fmt.Errorf("repository %s: %w", repo.Name, err)
		}

		versions, ok := pkgs[name]
		if !ok {
			continue
		}

		for version, pkg := range versions {
			if !found || CompareVersions(version, bestVersion) > 0 {
				bestVersion = version
				bestPkg = pkg
				found = true
			}
		}
	}

	if !found {
		return InputPackage{}, "", ErrPackageNotFound
	}

	return bestPkg, bestVersion, nil
}

func (rr *RepositoryRoot) GetPackageQuestions(name string) (map[string]Question, error) {
	pkg, _, err := rr.LatestPackage(name)
	if err != nil {
		return nil, err
	}
	return pkg.Questions, nil
}

// FindRepoForPackage finds the repository that contains the given package
// name and version. When multiple repositories contain the same package the
// one listed first wins.
func (rr *RepositoryRoot) FindRepoForPackage(name, version string) (string, error) {
	for _, repo := range rr.Items {
		pkgs, err := repo.LoadPackages(rr.BaseDir)
		if err != nil {
			return "", fmt.Errorf("repository %s: %w", repo.Name, err)
		}

		versions, ok := pkgs[name]
		if !ok {
			continue
		}

		if _, ok := versions[version]; ok {
			return repo.Name, nil
		}
	}

	return "", ErrPackageNotFound
}

// ListPackages returns the latest version of every package across all
// repositories. Repositories are consulted in order; when two repositories
// carry the same highest version the one listed first wins.
// Returns "repo/name@version" strings.
func (rr *RepositoryRoot) ListPackages() ([]string, error) {
	type bestEntry struct {
		Repo    string
		Version string
	}
	best := map[string]bestEntry{}

	for _, repo := range rr.Items {
		pkgs, err := repo.LoadPackages(rr.BaseDir)
		if err != nil {
			return nil, fmt.Errorf("repository %s: %w", repo.Name, err)
		}

		for name, versions := range pkgs {
			for version := range versions {
				prev, exists := best[name]
				if !exists || CompareVersions(version, prev.Version) > 0 {
					best[name] = bestEntry{Repo: repo.Name, Version: version}
				}
			}
		}
	}

	out := make([]string, 0, len(best))
	for name, entry := range best {
		out = append(out, PackageIdentity{Repo: entry.Repo, Name: name, Version: entry.Version}.String())
	}

	sort.Strings(out)

	return out, nil
}

// RepoPackageGroup represents a repository and its packages, used by
// ListPackagesByRepo.
type RepoPackageGroup struct {
	Repo     string            `json:"repo"`
	Packages []PackageIdentity `json:"packages"`
	Featured []string          `json:"featured,omitempty"`
}

const FeaturedFile = "featured.json"

// LoadFeatured reads the featured.json file from a repository directory.
func (r *Repository) LoadFeatured(baseDir string) (_ []string, err error) {
	fn, err := SafePath(baseDir, r.Name, FeaturedFile)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(fn) //nolint:gosec // fn validated by SafePath above
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()

	var featured []string
	if err := json.NewDecoder(f).Decode(&featured); err != nil {
		return nil, err
	}
	return featured, nil
}

// ListPackagesByRepo returns packages grouped by repository in precedence
// order (most important repo first = reversed internal order). Within each
// repo, packages are sorted alphabetically and only the latest version of
// each package name is included.
func (rr *RepositoryRoot) ListPackagesByRepo() ([]RepoPackageGroup, error) {
	var groups []RepoPackageGroup

	// Iterate in reverse so highest-precedence repo comes first.
	for i := len(rr.Items) - 1; i >= 0; i-- {
		repo := rr.Items[i]
		pkgs, err := repo.LoadPackages(rr.BaseDir)
		if err != nil {
			return nil, fmt.Errorf("repository %s: %w", repo.Name, err)
		}

		// Pick latest version per package name.
		best := map[string]string{}
		for name, versions := range pkgs {
			for version := range versions {
				prev, exists := best[name]
				if !exists || CompareVersions(version, prev) > 0 {
					best[name] = version
				}
			}
		}

		if len(best) == 0 {
			continue
		}

		names := make([]string, 0, len(best))
		for name := range best {
			names = append(names, name)
		}
		sort.Strings(names)

		pkgList := make([]PackageIdentity, len(names))
		for j, name := range names {
			pkgList[j] = PackageIdentity{Repo: repo.Name, Name: name, Version: best[name]}
		}

		featured, err := repo.LoadFeatured(rr.BaseDir)
		if err != nil {
			logrus.Warnf("load featured for %s: %v", repo.Name, err)
		}
		groups = append(groups, RepoPackageGroup{Repo: repo.Name, Packages: pkgList, Featured: featured})
	}

	return groups, nil
}

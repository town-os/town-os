package packages

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"go.yaml.in/yaml/v4"
)

const RepositoriesFile = "repositories.json"

// DefaultRepositories returns the default package repositories to seed when
// no repositories have been configured.
func DefaultRepositories() []Repository {
	u, _ := url.Parse("https://github.com/town-os/default-packages")
	return []Repository{
		{Name: "default", URL: *u},
	}
}

// TestRepositories returns the test package repositories used in development.
func TestRepositories() []Repository {
	core, _ := url.Parse("https://github.com/town-os/test-packages-core")
	extras, _ := url.Parse("https://github.com/town-os/test-packages-extras")
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
	RefreshErrors() map[string]string
	LoadAllPackages() (PackageTable, error)
	ListPackages() ([]string, error)
	ListPackageVersions(name string) ([]string, error)
	LatestPackage(name string) (InputPackage, string, error)
	GetPackageQuestions(name string) (map[string]Question, error)
	FindRepoForPackage(name, version string) (string, error)
	Move(name string, position int) error
}

type RepositoryRoot struct {
	BaseDir string
	Items   []Repository
	Errors  map[string]string
}

func RepositoryRootFromBase(baseDir string) (_ *RepositoryRoot, err error) {
	fn := filepath.Join(baseDir, RepositoriesFile)
	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}

	defer func() {
		err = errors.Join(err, f.Close())
	}()

	var items []Repository
	de := json.NewDecoder(f)
	if err := de.Decode(&items); err != nil {
		return nil, err
	}

	return &RepositoryRoot{
		BaseDir: baseDir,
		Items:   items,
	}, nil
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
	rr.Errors = map[string]string{}
	for i := range rr.Items {
		if err := rr.Items[i].init(rr.BaseDir); err != nil {
			logrus.Warnf("repository %s: %v", rr.Items[i].Name, err)
			rr.Errors[rr.Items[i].Name] = err.Error()
		}
	}
}

func (rr *RepositoryRoot) RefreshErrors() map[string]string {
	return rr.Errors
}

type Repository struct {
	Name     string
	URL      url.URL
	Username string
	Password string
}

func (r Repository) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]string{r.Name, r.credentialURL()})
}

func (r *Repository) UnmarshalJSON(data []byte) error {
	var pair [2]string
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
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

func (r Repository) credentialURL() string {
	if r.Username == "" {
		return r.URL.String()
	}
	u := r.URL
	u.User = url.UserPassword(r.Username, r.Password)
	logrus.Println(u.String())
	return u.String()
}

func runGit(dir, home string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", fmt.Sprintf("HOME=%s", home))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %v\n%s", args[0], err, out)
	}
	return nil
}

const (
	EnvRepoUsername = "TOWN_OS_REPO_USERNAME"
	EnvRepoPassword = "TOWN_OS_REPO_PASSWORD"
)

var ErrPartialCredentials = fmt.Errorf("both username and password must be provided together")

func NewRepository(baseDir, name string, u url.URL, username, password string) (*Repository, error) {
	if (username == "") != (password == "") {
		return nil, ErrPartialCredentials
	}
	if name == "" {
		name = strings.TrimSuffix(path.Base(u.Path), ".git")
	}
	r := &Repository{Name: name, URL: u, Username: username, Password: password}
	return r, r.init(baseDir)
}

func (r *Repository) init(baseDir string) error {
	target := filepath.Join(baseDir, r.Name)

	s, err := os.Stat(target)
	if os.IsNotExist(err) {
		if err := runGit(baseDir, baseDir, "clone", r.credentialURL(), r.Name); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !s.IsDir() {
		return fmt.Errorf("sub-path %s is not a directory", target)
	} else {
		needsStash := runGit(target, baseDir, "diff", "--quiet", "HEAD") != nil

		if needsStash {
			if err := runGit(target, baseDir, "stash"); err != nil {
				return err
			}
		}

		if err := runGit(target, baseDir, "pull", "--rebase"); err != nil {
			return err
		}

		if needsStash {
			if err := runGit(target, baseDir, "stash", "apply"); err != nil {
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

			f, err := os.Open(filepath.Join(nameDir, fn))
			if err != nil {
				return nil, err
			}

			var ip InputPackage
			de := yaml.NewDecoder(f)
			err = errors.Join(de.Decode(&ip), f.Close())
			if err != nil {
				return nil, fmt.Errorf("decoding %s/%s: %v", name.Name(), fn, err)
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
			return nil, fmt.Errorf("repository %s: %v", repo.Name, err)
		}

		for name, versions := range pkgs {
			if all[name] == nil {
				all[name] = map[string]InputPackage{}
			}

			for version, pkg := range versions {
				all[name][version] = pkg
			}
		}
	}

	return all, nil
}

// LoadPackage loads a single InputPackage from a repository by name and version.
func (rr *RepositoryRoot) LoadPackage(repoName, pkgName, version string) (_ InputPackage, err error) {
	fn := filepath.Join(rr.BaseDir, repoName, PackagesDir, pkgName, fmt.Sprintf("%s.yaml", version))
	f, err := os.Open(fn)
	if err != nil {
		return InputPackage{}, fmt.Errorf("package %s@%s not found: %w", pkgName, version, err)
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()

	var ip InputPackage
	if err := yaml.NewDecoder(f).Decode(&ip); err != nil {
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

	max := len(partsA)
	if len(partsB) > max {
		max = len(partsB)
	}

	for i := 0; i < max; i++ {
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
			return nil, fmt.Errorf("repository %s: %v", repo.Name, err)
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

var ErrPackageNotFound = fmt.Errorf("package not found")

// LatestPackage finds the latest version of a named package across all
// repositories. Repositories are consulted in order; when two repositories
// carry the same highest version the one listed last wins.
func (rr *RepositoryRoot) LatestPackage(name string) (InputPackage, string, error) {
	var bestPkg InputPackage
	var bestVersion string
	found := false

	for _, repo := range rr.Items {
		pkgs, err := repo.LoadPackages(rr.BaseDir)
		if err != nil {
			return InputPackage{}, "", fmt.Errorf("repository %s: %v", repo.Name, err)
		}

		versions, ok := pkgs[name]
		if !ok {
			continue
		}

		for version, pkg := range versions {
			if !found || CompareVersions(version, bestVersion) >= 0 {
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
// one listed last wins.
func (rr *RepositoryRoot) FindRepoForPackage(name, version string) (string, error) {
	result := ""
	for _, repo := range rr.Items {
		pkgs, err := repo.LoadPackages(rr.BaseDir)
		if err != nil {
			return "", fmt.Errorf("repository %s: %v", repo.Name, err)
		}

		versions, ok := pkgs[name]
		if !ok {
			continue
		}

		if _, ok := versions[version]; ok {
			result = repo.Name
		}
	}

	if result == "" {
		return "", ErrPackageNotFound
	}

	return result, nil
}

// ListPackages returns the latest version of every package across all
// repositories. Repositories are consulted in order; when two repositories
// carry the same highest version the one listed last wins.
func (rr *RepositoryRoot) ListPackages() ([]string, error) {
	best := map[string]string{}

	for _, repo := range rr.Items {
		pkgs, err := repo.LoadPackages(rr.BaseDir)
		if err != nil {
			return nil, fmt.Errorf("repository %s: %v", repo.Name, err)
		}

		for name, versions := range pkgs {
			for version := range versions {
				prev, exists := best[name]
				if !exists || CompareVersions(version, prev) >= 0 {
					best[name] = version
				}
			}
		}
	}

	out := make([]string, 0, len(best))
	for name, version := range best {
		out = append(out, PackageIdentity{Name: name, Version: version}.String())
	}

	sort.Strings(out)

	return out, nil
}

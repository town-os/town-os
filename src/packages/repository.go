package packages

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

const RepositoriesFile = "repositories.json"

type RepositoryManager interface {
	Add(repo Repository) error
	Remove(name string) error
	Get(name string) (Repository, bool)
	List() ([]Repository, error)
	Refresh() error
	LoadAllPackages() (PackageTable, error)
	ListPackages() ([]string, error)
	LatestPackage(name string) (InputPackage, string, error)
}

type RepositoryRoot struct {
	BaseDir string
	Items   []Repository
}

func RepositoryRootFromBase(baseDir string) (*RepositoryRoot, error) {
	fn := filepath.Join(baseDir, RepositoriesFile)
	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}

	defer f.Close()

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

func (rr *RepositoryRoot) save() error {
	fn := filepath.Join(rr.BaseDir, RepositoriesFile)
	f, err := os.Create(fn)
	if err != nil {
		return err
	}

	defer f.Close()

	en := json.NewEncoder(f)
	en.SetIndent("", "  ")
	return en.Encode(rr.Items)
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

func (rr *RepositoryRoot) Refresh() error {
	for _, repo := range rr.Items {
		if _, err := NewRepository(rr.BaseDir, repo.URL); err != nil {
			return fmt.Errorf("repository %s: %v", repo.Name, err)
		}
	}

	return nil
}

type Repository struct {
	Name string
	URL  url.URL
}

func runGit(baseDir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = baseDir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %v\n%s", args[0], err, out)
	}
	return nil
}

func NewRepository(baseDir string, u url.URL) (*Repository, error) {
	name := strings.TrimSuffix(path.Base(u.Path), ".git")
	r := &Repository{Name: name, URL: u}
	return r, r.init(baseDir)
}

func (r *Repository) init(baseDir string) error {
	target := filepath.Join(baseDir, r.Name)

	s, err := os.Stat(target)
	if os.IsNotExist(err) {
		if err := runGit(baseDir, "clone", r.URL.String(), r.Name); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !s.IsDir() {
		return fmt.Errorf("sub-path %s is not a directory", target)
	} else {
		needsStash := runGit(target, "diff", "--quiet", "HEAD") != nil

		if needsStash {
			if err := runGit(target, "stash"); err != nil {
				return err
			}
		}

		if err := runGit(target, "pull", "--rebase"); err != nil {
			return err
		}

		if needsStash {
			if err := runGit(target, "stash", "apply"); err != nil {
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
			err = de.Decode(&ip)
			f.Close()
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

var ErrPackageNotFound = fmt.Errorf("package not found")

// LatestPackage finds the latest version of a named package across all
// repositories. Repositories are consulted in preferential order; when two
// repositories carry the same highest version the one listed first wins.
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

// ListPackages returns the latest version of every package across all
// repositories. Repositories are consulted in preferential order; when two
// repositories carry the same highest version the one listed first wins.
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
				if !exists || CompareVersions(version, prev) > 0 {
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

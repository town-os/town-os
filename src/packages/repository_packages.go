package packages

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"go.yaml.in/yaml/v4"
)

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

		// Reject package names that collide with Town OS reserved
		// identifiers (the --dep-- separator or the "subpackages"
		// encapsulator directory). Skipping instead of erroring lets the
		// rest of the repository load — a single bad directory must not
		// break every other package in the repo.
		if err := ValidatePackageName(name.Name()); err != nil {
			logrus.Warnf("skipping reserved package name %s/%s: %v", r.Name, name.Name(), err)
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
			f, err := os.Open(safeFn) //nolint:gosec // G304 -- safeFn from SafePath
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

// cachedLoadPackages returns the package table for a repository, using a
// cached copy if available. The cache is invalidated on repository refresh.
func (rr *RepositoryRoot) cachedLoadPackages(repo *Repository) (PackageTable, error) {
	if cached, ok := rr.pkgCache.Load(repo.Name); ok {
		if pt, valid := cached.(PackageTable); valid {
			return pt, nil
		}
	}
	pkgs, err := repo.LoadPackages(rr.BaseDir)
	if err != nil {
		return nil, err
	}
	rr.pkgCache.Store(repo.Name, pkgs)
	return pkgs, nil
}

func (rr *RepositoryRoot) LoadAllPackages() (PackageTable, error) {
	all := PackageTable{}

	for _, repo := range rr.Items {
		pkgs, err := rr.cachedLoadPackages(&repo)
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
	f, err := os.Open(fn) //nolint:gosec // G304 -- fn from SafePath
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

// LoadInstalledPackage loads a single InputPackage from the installed directory
// by name and version. This is needed for dependency packages whose effective
// name differs from the source package name in the repository. The installed
// YAML is a hard link to the repo source file. Dependency effective names are
// translated to the nested storage path (parent/subpackages/key/...) via
// StoragePath before the file open, so this loader stays in sync with the
// InstallManager's on-disk layout.
func (rr *RepositoryRoot) LoadInstalledPackage(repoName, pkgName, version string) (_ InputPackage, err error) {
	fn, err := SafePath(rr.BaseDir, InstalledDir, repoName, StoragePath(pkgName), version+".yaml")
	if err != nil {
		return InputPackage{}, err
	}
	f, err := os.Open(fn) //nolint:gosec // G304 -- fn from SafePath
	if err != nil {
		return InputPackage{}, fmt.Errorf("installed package %s@%s not found: %w", pkgName, version, err)
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()

	var ip InputPackage
	if err := yaml.NewDecoder(f).Decode(&ip); err != nil {
		return InputPackage{}, fmt.Errorf("decoding installed %s@%s: %w", pkgName, version, err)
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
		pkgs, err := rr.cachedLoadPackages(&repo)
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
		pkgs, err := rr.cachedLoadPackages(&repo)
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
		pkgs, err := rr.cachedLoadPackages(&repo)
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
		pkgs, err := rr.cachedLoadPackages(&repo)
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
	f, err := os.Open(fn) //nolint:gosec // G304 -- fn from SafePath
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
	for _, repo := range slices.Backward(rr.Items) {
		pkgs, err := rr.cachedLoadPackages(&repo)
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

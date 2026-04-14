package packages

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

const InstalledDir = "installed"

var (
	ErrNotInstalled     = errors.New("package not installed")
	ErrAlreadyInstalled = errors.New("package already installed")
)

const ResponsesDir = "responses"

type Installer interface {
	Install(repoName, pkgName, sourcePkgName, version string, responses Responses) error
	Uninstall(repoName, pkgName, version string) error
	ListInstalled() ([]string, error)
	GetInstalledVersion(repoName, pkgName string) (string, bool, error)
	GetResponses(repoName, pkgName, version string) (Responses, error)
	SetDisabled(repoName, pkgName string, disabled bool) error
	IsDisabled(repoName, pkgName string) (bool, error)
	SaveLastResponses(repoName, pkgName string, responses Responses) error
	LoadLastResponses(repoName, pkgName string) (Responses, error)
	ClearLastResponses(repoName, pkgName string) error
	SaveChildren(repoName, parentName string, children []string) error
	LoadChildren(repoName, parentName string) ([]string, error)
	IsPackageChanged(repoName, pkgName, version string) (bool, error)
	SaveDependencies(repoName, pkgName string, deps map[string]DependencyRecord) error
	LoadDependencies(repoName, pkgName string) (map[string]DependencyRecord, error)
}

type InstallManager struct {
	BaseDir string
}

func NewInstallManager(baseDir string) *InstallManager {
	return &InstallManager{BaseDir: baseDir}
}

func (m *InstallManager) dir() string {
	return filepath.Join(m.BaseDir, InstalledDir)
}

func (m *InstallManager) responsesDir() string {
	return filepath.Join(m.BaseDir, ResponsesDir)
}

// pkgDir returns the absolute directory that holds the install record
// (hardlinked yaml, disabled marker, dependencies.json, children.json,
// subpackages/ subdir) for a package. Dependency effective names are
// translated to nested on-disk form via StoragePath, so a dep
// "parent--dep--key" lives at
// "<baseDir>/installed/<repo>/parent/subpackages/key". Standalone names
// pass through unchanged.
func (m *InstallManager) pkgDir(repoName, pkgName string) string {
	return filepath.Join(m.dir(), repoName, StoragePath(pkgName))
}

// responsesPkgDir returns the absolute responses directory for a package,
// mirroring the nested installed/ layout so every dep's <version>.json
// sits next to its install record.
func (m *InstallManager) responsesPkgDir(repoName, pkgName string) string {
	return filepath.Join(m.responsesDir(), repoName, StoragePath(pkgName))
}

// safePkgPath returns SafePath(m.dir(), repo, StoragePath(pkgName), parts...)
// — the preferred entrypoint anywhere that previously called
// SafePath(m.dir(), repoName, pkgName, ...). Keeps traversal validation
// while routing through the nested layout.
func (m *InstallManager) safePkgPath(repoName, pkgName string, parts ...string) (string, error) {
	all := append([]string{repoName, StoragePath(pkgName)}, parts...)
	return SafePath(m.dir(), all...)
}

// safeResponsesPath is the responses-dir analog of safePkgPath.
func (m *InstallManager) safeResponsesPath(repoName, pkgName string, parts ...string) (string, error) {
	all := append([]string{repoName, StoragePath(pkgName)}, parts...)
	return SafePath(m.responsesDir(), all...)
}

// Install creates a hard link at installed/<repoName>/<pkgName>/<version>.yaml from
// the repository package file at <repoName>/packages/<sourcePkgName>/<version>.yaml.
// sourcePkgName is the original package name in the repository; pkgName is the
// effective name used for the installed record (these differ for dependencies).
// It also persists the responses to responses/<repoName>/<pkgName>/<version>.json.
func (m *InstallManager) Install(repoName, pkgName, sourcePkgName, version string, responses Responses) error {
	pkgDir := m.pkgDir(repoName, pkgName)
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		return err
	}

	link := filepath.Join(pkgDir, version+".yaml")

	_, err := os.Lstat(link)
	if err == nil {
		return fmt.Errorf("%s/%s@%s: %w", repoName, pkgName, version, ErrAlreadyInstalled)
	}

	source := filepath.Join(m.BaseDir, repoName, PackagesDir, sourcePkgName, version+".yaml")
	_, err = os.Stat(source)
	if err != nil {
		return fmt.Errorf("source package not found: %w", err)
	}

	if err := os.Link(source, link); err != nil {
		return err
	}

	// Persist responses.
	return m.SaveResponses(repoName, pkgName, version, responses)
}

// SetDisabled creates or removes a disabled marker file for the given package.
func (m *InstallManager) SetDisabled(repoName, pkgName string, disabled bool) error {
	marker, err := m.safePkgPath(repoName, pkgName, "disabled")
	if err != nil {
		return err
	}
	if disabled {
		pkgDir := m.pkgDir(repoName, pkgName)
		if err := os.MkdirAll(pkgDir, 0750); err != nil {
			return err
		}
		f, err := os.Create(marker) //nolint:gosec // G304 -- marker path validated by SafePath
		if err != nil {
			return err
		}
		return f.Close()
	}
	err = os.Remove(marker)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// IsDisabled returns true if the package has a disabled marker file.
func (m *InstallManager) IsDisabled(repoName, pkgName string) (bool, error) {
	marker := filepath.Join(m.pkgDir(repoName, pkgName), "disabled")
	_, err := os.Stat(marker)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// Uninstall removes the hard link for the given package version. If the package
// directory becomes empty it is removed as well. For dependency packages whose
// on-disk path is nested (parent/subpackages/key/...), the empty-dir cleanup
// walks upward removing each now-empty ancestor (key, subpackages) until it
// hits a non-empty directory or the installed/ root — without ever removing a
// directory that still holds a parent's own <version>.yaml.
func (m *InstallManager) Uninstall(repoName, pkgName, version string) error {
	pkgDir := m.pkgDir(repoName, pkgName)
	link := filepath.Join(pkgDir, version+".yaml")

	fi, err := os.Lstat(link)
	if os.IsNotExist(err) {
		return fmt.Errorf("%s/%s@%s: %w", repoName, pkgName, version, ErrNotInstalled)
	}
	if err != nil {
		return err
	}

	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s/%s@%s is not a regular file", repoName, pkgName, version)
	}

	if err := os.Remove(link); err != nil {
		return err
	}

	// Remove disabled marker so the directory can be cleaned up.
	marker := filepath.Join(pkgDir, "disabled")
	if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
		return err
	}

	// Walk up the installed tree, removing any empty ancestor directory
	// until we hit a non-empty one or the installed root.
	if err := removeEmptyAncestors(pkgDir, m.dir()); err != nil {
		return err
	}

	// Remove the specific response file and walk up the responses tree the
	// same way so that orphaned parent-dir chains (e.g. subpackages/<key>)
	// don't accumulate.
	respFile := filepath.Join(m.responsesPkgDir(repoName, pkgName), version+".json")
	if err := os.Remove(respFile); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := removeEmptyAncestors(m.responsesPkgDir(repoName, pkgName), m.responsesDir()); err != nil {
		return err
	}

	return nil
}

// removeEmptyAncestors removes dir and every parent directory up to (but not
// including) root, stopping at the first non-empty directory encountered. A
// non-existent dir is treated as already-removed and cleanup continues with
// its parent. Errors other than "empty-check failed" are surfaced.
func removeEmptyAncestors(dir, root string) error {
	cleanRoot := filepath.Clean(root)
	current := filepath.Clean(dir)
	for current != cleanRoot && strings.HasPrefix(current, cleanRoot+string(filepath.Separator)) {
		entries, err := os.ReadDir(current)
		if err != nil {
			if os.IsNotExist(err) {
				current = filepath.Dir(current)
				continue
			}
			return err
		}
		if len(entries) > 0 {
			return nil
		}
		if err := os.Remove(current); err != nil && !os.IsNotExist(err) {
			return err
		}
		current = filepath.Dir(current)
	}
	return nil
}

// GetInstalledVersion returns the installed version for a package by repo and
// name. It reads the pkgDir (nested via StoragePath for deps) directly rather
// than scanning all installed packages. Returns ("", false, nil) when not
// installed.
func (m *InstallManager) GetInstalledVersion(repoName, pkgName string) (string, bool, error) {
	nameDir := m.pkgDir(repoName, pkgName)
	entries, err := os.ReadDir(nameDir)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	for _, entry := range entries {
		fn := entry.Name()
		if !strings.HasSuffix(fn, ".yaml") {
			continue
		}
		return strings.TrimSuffix(fn, ".yaml"), true, nil
	}
	return "", false, nil
}

// ListInstalled returns all installed packages independently of the
// repositories. Results are sorted by repo, then name, then version.
// Each entry is formatted as "repo/name@version".
//
// Walks the nested layout recursively: at each package directory it emits
// one entry per <version>.yaml hard-link found, and descends into any
// `subpackages/<key>` child to discover deps. The effective name for a
// dep is reconstructed from the parent chain via DependencyName, so the
// returned Name field always carries the flat --dep-- form that callers
// (reconcile, systemd, DNS) already expect.
func (m *InstallManager) ListInstalled() ([]string, error) {
	installedDir := m.dir()

	repos, err := os.ReadDir(installedDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var items []PackageIdentity

	for _, repo := range repos {
		if !repo.IsDir() {
			continue
		}
		repoDir := filepath.Join(installedDir, repo.Name())
		names, err := os.ReadDir(repoDir)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if !name.IsDir() {
				continue
			}
			// Skip any directory at the repo root named subpackages
			// defensively — ValidatePackageName rejects it, but a
			// corrupted on-disk tree must not crash the walker.
			if name.Name() == SubpackagesDir {
				continue
			}
			if err := m.walkInstalledPackage(&items, repo.Name(), name.Name(), filepath.Join(repoDir, name.Name())); err != nil {
				return nil, err
			}
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Repo != items[j].Repo {
			return items[i].Repo < items[j].Repo
		}
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return CompareVersions(items[i].Version, items[j].Version) < 0
	})

	out := make([]string, len(items))
	for i, p := range items {
		out[i] = p.String()
	}

	return out, nil
}

// walkInstalledPackage emits install records for every <version>.yaml
// hard-link found directly under pkgDir and recurses into any
// subpackages/<key> child to discover deps. effectiveName carries the
// flat parent--dep--key chain accumulated so far; at each deeper level it
// is extended via DependencyName(effectiveName, depKey).
func (m *InstallManager) walkInstalledPackage(items *[]PackageIdentity, repo, effectiveName, pkgDir string) error {
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		if name == SubpackagesDir && entry.IsDir() {
			if err := m.walkSubpackages(items, repo, effectiveName, filepath.Join(pkgDir, name)); err != nil {
				return err
			}
			continue
		}
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(name, ".yaml") {
			continue
		}
		fi, err := os.Lstat(filepath.Join(pkgDir, name))
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			continue
		}
		*items = append(*items, PackageIdentity{
			Repo:    repo,
			Name:    effectiveName,
			Version: strings.TrimSuffix(name, ".yaml"),
		})
	}
	return nil
}

// walkSubpackages iterates the reserved `subpackages` directory's
// children, each of which is a dep key, and recurses the main walker
// into each dep's install dir.
func (m *InstallManager) walkSubpackages(items *[]PackageIdentity, repo, parentEffective, subpackagesDir string) error {
	keys, err := os.ReadDir(subpackagesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, k := range keys {
		if !k.IsDir() {
			continue
		}
		child := DependencyName(parentEffective, k.Name())
		if err := m.walkInstalledPackage(items, repo, child, filepath.Join(subpackagesDir, k.Name())); err != nil {
			return err
		}
	}
	return nil
}

// GetResponses reads the persisted responses for a given package version.
func (m *InstallManager) GetResponses(repoName, pkgName, version string) (_ Responses, err error) {
	respFile, err := m.safeResponsesPath(repoName, pkgName, version+".json")
	if err != nil {
		return nil, err
	}

	f, err := os.Open(respFile) //nolint:gosec // G304 -- respFile from SafePath
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%s/%s@%s: %w", repoName, pkgName, version, ErrNotInstalled)
	}
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()

	var resp Responses
	err = json.NewDecoder(f).Decode(&resp)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

const LastResponsesDir = "responses/last"

func (m *InstallManager) lastResponsesDir() string {
	return filepath.Join(m.BaseDir, LastResponsesDir)
}

// lastResponsesPath returns the last-responses JSON file path for a
// package, translating dependency effective names to the nested form so
// that "parent--dep--key" lives at
// "responses/last/<repo>/parent/subpackages/key.json". The trailing .json
// is appended to the final segment after StoragePath translation.
func (m *InstallManager) lastResponsesPath(repoName, pkgName string) string {
	storagePath := StoragePath(pkgName)
	return filepath.Join(m.lastResponsesDir(), repoName, storagePath+".json")
}

// SaveLastResponses persists the last responses for a package (keyed by repo/name).
func (m *InstallManager) SaveLastResponses(repoName, pkgName string, responses Responses) (err error) {
	lock, err := lockDir(m.BaseDir)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, lock.Unlock())
	}()

	fn := m.lastResponsesPath(repoName, pkgName)
	if err := os.MkdirAll(filepath.Dir(fn), 0700); err != nil {
		return err
	}

	return atomicWriteJSON(fn, responses)
}

// LoadLastResponses reads the last saved responses for a package.
func (m *InstallManager) LoadLastResponses(repoName, pkgName string) (_ Responses, err error) {
	fn, err := SafePath(m.lastResponsesDir(), repoName, StoragePath(pkgName)+".json")
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

	var resp Responses
	if err := json.NewDecoder(f).Decode(&resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ClearLastResponses removes the last saved responses for a package.
func (m *InstallManager) ClearLastResponses(repoName, pkgName string) error {
	fn := m.lastResponsesPath(repoName, pkgName)
	if err := os.Remove(fn); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

const ChildrenFile = "children.json"

// SaveChildren persists the list of child instance names for a parent package.
func (m *InstallManager) SaveChildren(repoName, parentName string, children []string) (err error) {
	lock, err := lockDir(m.BaseDir)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, lock.Unlock())
	}()

	dir := m.pkgDir(repoName, parentName)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	return atomicWriteJSON(filepath.Join(dir, ChildrenFile), children)
}

// LoadChildren reads the list of child instance names for a parent package.
func (m *InstallManager) LoadChildren(repoName, parentName string) (_ []string, err error) {
	fn, err := m.safePkgPath(repoName, parentName, ChildrenFile)
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

	var children []string
	if err := json.NewDecoder(f).Decode(&children); err != nil {
		return nil, err
	}
	return children, nil
}

// IsPackageChanged compares the inode of the installed package file with the
// repository copy. If they differ (or the repo file is missing), the package
// was updated upstream after installation.
func (m *InstallManager) IsPackageChanged(repoName, pkgName, version string) (bool, error) {
	installedPath := filepath.Join(m.pkgDir(repoName, pkgName), version+".yaml")
	repoPath := filepath.Join(m.BaseDir, repoName, PackagesDir, pkgName, version+".yaml")

	installedStat, err := os.Stat(installedPath)
	if err != nil {
		return false, fmt.Errorf("stat installed file: %w", err)
	}

	repoStat, err := os.Stat(repoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("stat repo file: %w", err)
	}

	installedSys, ok := installedStat.Sys().(*syscall.Stat_t)
	if !ok {
		return false, errors.New("cannot get inode for installed file")
	}

	repoSys, ok := repoStat.Sys().(*syscall.Stat_t)
	if !ok {
		return false, errors.New("cannot get inode for repo file")
	}

	return installedSys.Ino != repoSys.Ino, nil
}

// SaveDependencies persists the dependency records for a parent package.
// The file is written to installed/<repoName>/<StoragePath(pkgName)>/dependencies.json.
func (m *InstallManager) SaveDependencies(repoName, pkgName string, deps map[string]DependencyRecord) (err error) {
	lock, err := lockDir(m.BaseDir)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, lock.Unlock())
	}()

	dir := m.pkgDir(repoName, pkgName)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	return atomicWriteJSON(filepath.Join(dir, DependenciesFile), deps)
}

// LoadDependencies reads the persisted dependency records for a parent package.
// Returns nil (not an error) when no dependencies file exists.
func (m *InstallManager) LoadDependencies(repoName, pkgName string) (_ map[string]DependencyRecord, err error) {
	fn, err := m.safePkgPath(repoName, pkgName, DependenciesFile)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(fn) //nolint:gosec // G304 -- fn from SafePath
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil //nolint:nilnil // nil deps is the correct zero value when no file exists
		}
		return nil, err
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()

	var deps map[string]DependencyRecord
	if err := json.NewDecoder(f).Decode(&deps); err != nil {
		return nil, err
	}
	return deps, nil
}

// SaveResponses persists responses to disk using an atomic write under an
// exclusive file lock. The file is written to
// responses/<repoName>/<StoragePath(pkgName)>/<version>.json so dep responses
// sit next to the parent's responses tree instead of in a flat sibling dir.
func (m *InstallManager) SaveResponses(repoName, pkgName, version string, responses Responses) (err error) {
	lock, err := lockDir(m.BaseDir)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, lock.Unlock())
	}()

	respDir := m.responsesPkgDir(repoName, pkgName)
	err = os.MkdirAll(respDir, 0750)
	if err != nil {
		return err
	}

	respFile := filepath.Join(respDir, version+".json")
	return atomicWriteJSON(respFile, responses)
}

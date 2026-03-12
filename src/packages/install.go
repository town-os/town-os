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
	Install(repoName, pkgName, version string, responses Responses) error
	Uninstall(repoName, pkgName, version string) error
	ListInstalled() ([]string, error)
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

// Install creates a hard link at installed/<repoName>/<pkgName>/<version>.yaml from
// the repository package file at <repoName>/packages/<pkgName>/<version>.yaml.
// It also persists the responses to responses/<repoName>/<pkgName>/<version>.json.
func (m *InstallManager) Install(repoName, pkgName, version string, responses Responses) error {
	pkgDir := filepath.Join(m.dir(), repoName, pkgName)
	err := os.MkdirAll(pkgDir, 0750)
	if err != nil {
		return err
	}

	link := filepath.Join(pkgDir, version+".yaml")

	_, err = os.Lstat(link)
	if err == nil {
		return fmt.Errorf("%s/%s@%s: %w", repoName, pkgName, version, ErrAlreadyInstalled)
	}

	source := filepath.Join(m.BaseDir, repoName, PackagesDir, pkgName, version+".yaml")
	_, err = os.Stat(source)
	if err != nil {
		return fmt.Errorf("source package not found: %w", err)
	}

	err = os.Link(source, link)
	if err != nil {
		return err
	}

	// Persist responses.
	return m.SaveResponses(repoName, pkgName, version, responses)
}

// SetDisabled creates or removes a disabled marker file for the given package.
func (m *InstallManager) SetDisabled(repoName, pkgName string, disabled bool) error {
	marker, err := SafePath(m.dir(), repoName, pkgName, "disabled")
	if err != nil {
		return err
	}
	if disabled {
		pkgDir := filepath.Join(m.dir(), repoName, pkgName)
		err := os.MkdirAll(pkgDir, 0750)
		if err != nil {
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
	marker := filepath.Join(m.dir(), repoName, pkgName, "disabled")
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
// directory becomes empty it is removed as well.
func (m *InstallManager) Uninstall(repoName, pkgName, version string) error {
	link := filepath.Join(m.dir(), repoName, pkgName, version+".yaml")

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

	err = os.Remove(link)
	if err != nil {
		return err
	}

	// Remove disabled marker so the directory can be cleaned up.
	marker := filepath.Join(m.dir(), repoName, pkgName, "disabled")
	err = os.Remove(marker)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Clean up empty package directory.
	pkgDir := filepath.Join(m.dir(), repoName, pkgName)
	entries, err := os.ReadDir(pkgDir)
	if err == nil && len(entries) == 0 {
		err = os.Remove(pkgDir)
		if err != nil {
			return err
		}
	}

	// Clean up empty repo directory.
	repoDir := filepath.Join(m.dir(), repoName)
	repoEntries, err := os.ReadDir(repoDir)
	if err == nil && len(repoEntries) == 0 {
		err = os.Remove(repoDir)
		if err != nil {
			return err
		}
	}

	// Remove response file.
	respFile := filepath.Join(m.responsesDir(), repoName, pkgName, version+".json")
	err = os.Remove(respFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Clean up empty response directories.
	respPkgDir := filepath.Join(m.responsesDir(), repoName, pkgName)
	respPkgEntries, err := os.ReadDir(respPkgDir)
	if err == nil && len(respPkgEntries) == 0 {
		err = os.Remove(respPkgDir)
		if err != nil {
			return err
		}
	}

	respRepoDir := filepath.Join(m.responsesDir(), repoName)
	respRepoEntries, err := os.ReadDir(respRepoDir)
	if err == nil && len(respRepoEntries) == 0 {
		err = os.Remove(respRepoDir)
		if err != nil {
			return err
		}
	}

	return nil
}

// ListInstalled returns all installed packages independently of the
// repositories. Results are sorted by repo, then name, then version.
// Each entry is formatted as "repo/name@version".
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

			nameDir := filepath.Join(repoDir, name.Name())
			versions, err := os.ReadDir(nameDir)
			if err != nil {
				return nil, err
			}

			for _, version := range versions {
				fn := version.Name()
				if !strings.HasSuffix(fn, ".yaml") {
					continue
				}

				fi, err := os.Lstat(filepath.Join(nameDir, fn))
				if err != nil {
					return nil, err
				}
				if !fi.Mode().IsRegular() {
					continue
				}

				items = append(items, PackageIdentity{
					Repo:    repo.Name(),
					Name:    name.Name(),
					Version: strings.TrimSuffix(fn, ".yaml"),
				})
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

// GetResponses reads the persisted responses for a given package version.
func (m *InstallManager) GetResponses(repoName, pkgName, version string) (_ Responses, err error) {
	respFile, err := SafePath(m.responsesDir(), repoName, pkgName, version+".json")
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

// SaveLastResponses persists the last responses for a package (keyed by repo/name).
func (m *InstallManager) SaveLastResponses(repoName, pkgName string, responses Responses) (err error) {
	lock, err := lockDir(m.BaseDir)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, lock.Unlock())
	}()

	dir := filepath.Join(m.lastResponsesDir(), repoName)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	return atomicWriteJSON(filepath.Join(dir, pkgName+".json"), responses)
}

// LoadLastResponses reads the last saved responses for a package.
func (m *InstallManager) LoadLastResponses(repoName, pkgName string) (_ Responses, err error) {
	fn, err := SafePath(m.lastResponsesDir(), repoName, pkgName+".json")
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
	fn := filepath.Join(m.lastResponsesDir(), repoName, pkgName+".json")
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

	dir := filepath.Join(m.dir(), repoName, parentName)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	return atomicWriteJSON(filepath.Join(dir, ChildrenFile), children)
}

// LoadChildren reads the list of child instance names for a parent package.
func (m *InstallManager) LoadChildren(repoName, parentName string) (_ []string, err error) {
	fn, err := SafePath(m.dir(), repoName, parentName, ChildrenFile)
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
	installedPath := filepath.Join(m.dir(), repoName, pkgName, version+".yaml")
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
// The file is written to installed/<repoName>/<pkgName>/dependencies.json.
func (m *InstallManager) SaveDependencies(repoName, pkgName string, deps map[string]DependencyRecord) (err error) {
	lock, err := lockDir(m.BaseDir)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, lock.Unlock())
	}()

	dir := filepath.Join(m.dir(), repoName, pkgName)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	return atomicWriteJSON(filepath.Join(dir, DependenciesFile), deps)
}

// LoadDependencies reads the persisted dependency records for a parent package.
// Returns nil (not an error) when no dependencies file exists.
func (m *InstallManager) LoadDependencies(repoName, pkgName string) (_ map[string]DependencyRecord, err error) {
	fn, err := SafePath(m.dir(), repoName, pkgName, DependenciesFile)
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
// exclusive file lock. The file is written to responses/<repoName>/<pkgName>/<version>.json.
func (m *InstallManager) SaveResponses(repoName, pkgName, version string, responses Responses) (err error) {
	lock, err := lockDir(m.BaseDir)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, lock.Unlock())
	}()

	respDir := filepath.Join(m.responsesDir(), repoName, pkgName)
	err = os.MkdirAll(respDir, 0750)
	if err != nil {
		return err
	}

	respFile := filepath.Join(respDir, version+".json")
	return atomicWriteJSON(respFile, responses)
}

package packages

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const InstalledDir = "installed"

var (
	ErrNotInstalled     = fmt.Errorf("package not installed")
	ErrAlreadyInstalled = fmt.Errorf("package already installed")
)

const ResponsesDir = "responses"

type Installer interface {
	Install(repoName, pkgName, version string, responses Responses) error
	Uninstall(pkgName, version string) error
	ListInstalled() ([]string, error)
	GetResponses(pkgName, version string) (Responses, error)
	SetDisabled(pkgName string, disabled bool) error
	IsDisabled(pkgName string) (bool, error)
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

// Install creates a symlink at installed/<pkgName>/<version>.yaml pointing to
// the repository package file at <repoName>/packages/<pkgName>/<version>.yaml.
// It also persists the responses to responses/<pkgName>/<version>.json.
func (m *InstallManager) Install(repoName, pkgName, version string, responses Responses) error {
	pkgDir := filepath.Join(m.dir(), pkgName)
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		return err
	}

	link := filepath.Join(pkgDir, fmt.Sprintf("%s.yaml", version))

	if _, err := os.Lstat(link); err == nil {
		return fmt.Errorf("%s@%s: %w", pkgName, version, ErrAlreadyInstalled)
	}

	source := filepath.Join(m.BaseDir, repoName, PackagesDir, pkgName, fmt.Sprintf("%s.yaml", version))
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("source package not found: %v", err)
	}

	// Relative symlink so the tree stays relocatable.
	target := filepath.Join("..", "..", repoName, PackagesDir, pkgName, fmt.Sprintf("%s.yaml", version))

	if err := os.Symlink(target, link); err != nil {
		return err
	}

	// Persist responses.
	return m.SaveResponses(pkgName, version, responses)
}

// SetDisabled creates or removes a disabled marker file for the given package.
func (m *InstallManager) SetDisabled(pkgName string, disabled bool) error {
	marker := filepath.Join(m.dir(), pkgName, "disabled")
	if disabled {
		pkgDir := filepath.Join(m.dir(), pkgName)
		if err := os.MkdirAll(pkgDir, 0755); err != nil {
			return err
		}
		f, err := os.Create(marker)
		if err != nil {
			return err
		}
		return f.Close()
	}
	if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// IsDisabled returns true if the package has a disabled marker file.
func (m *InstallManager) IsDisabled(pkgName string) (bool, error) {
	marker := filepath.Join(m.dir(), pkgName, "disabled")
	_, err := os.Stat(marker)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// Uninstall removes the symlink for the given package version. If the package
// directory becomes empty it is removed as well.
func (m *InstallManager) Uninstall(pkgName, version string) error {
	link := filepath.Join(m.dir(), pkgName, fmt.Sprintf("%s.yaml", version))

	fi, err := os.Lstat(link)
	if os.IsNotExist(err) {
		return fmt.Errorf("%s@%s: %w", pkgName, version, ErrNotInstalled)
	}
	if err != nil {
		return err
	}

	if fi.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s@%s is not a symlink", pkgName, version)
	}

	if err := os.Remove(link); err != nil {
		return err
	}

	// Remove disabled marker so the directory can be cleaned up.
	marker := filepath.Join(m.dir(), pkgName, "disabled")
	if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
		return err
	}

	// Clean up empty package directory.
	pkgDir := filepath.Join(m.dir(), pkgName)
	entries, err := os.ReadDir(pkgDir)
	if err == nil && len(entries) == 0 {
		if err := os.Remove(pkgDir); err != nil {
			return err
		}
	}

	// Remove response file.
	respFile := filepath.Join(m.responsesDir(), pkgName, fmt.Sprintf("%s.json", version))
	if err := os.Remove(respFile); err != nil && !os.IsNotExist(err) {
		return err
	}

	// Clean up empty response directory.
	respDir := filepath.Join(m.responsesDir(), pkgName)
	respEntries, err := os.ReadDir(respDir)
	if err == nil && len(respEntries) == 0 {
		if err := os.Remove(respDir); err != nil {
			return err
		}
	}

	return nil
}

// ListInstalled returns all installed packages independently of the
// repositories. Results are sorted by name, then by version.
func (m *InstallManager) ListInstalled() ([]string, error) {
	installedDir := m.dir()

	names, err := os.ReadDir(installedDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var items []PackageIdentity

	for _, name := range names {
		if !name.IsDir() {
			continue
		}

		nameDir := filepath.Join(installedDir, name.Name())
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
			if fi.Mode()&os.ModeSymlink == 0 {
				continue
			}

			items = append(items, PackageIdentity{
				Name:    name.Name(),
				Version: strings.TrimSuffix(fn, ".yaml"),
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
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
func (m *InstallManager) GetResponses(pkgName, version string) (_ Responses, err error) {
	respFile := filepath.Join(m.responsesDir(), pkgName, fmt.Sprintf("%s.json", version))

	f, err := os.Open(respFile)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%s@%s: %w", pkgName, version, ErrNotInstalled)
	}
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

// SaveResponses persists responses to disk using an atomic write under an
// exclusive file lock. The file is written to responses/<pkgName>/<version>.json.
func (m *InstallManager) SaveResponses(pkgName, version string, responses Responses) (err error) {
	lock, err := lockDir(m.BaseDir)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, lock.Unlock())
	}()

	respDir := filepath.Join(m.responsesDir(), pkgName)
	if err := os.MkdirAll(respDir, 0755); err != nil {
		return err
	}

	respFile := filepath.Join(respDir, fmt.Sprintf("%s.json", version))
	return atomicWriteJSON(respFile, responses)
}

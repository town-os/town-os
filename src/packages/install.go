package packages

import (
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

type InstallManager struct {
	BaseDir string
}

func NewInstallManager(baseDir string) *InstallManager {
	return &InstallManager{BaseDir: baseDir}
}

func (m *InstallManager) dir() string {
	return filepath.Join(m.BaseDir, InstalledDir)
}

// Install creates a symlink at installed/<pkgName>/<version>.yaml pointing to
// the repository package file at <repoName>/packages/<pkgName>/<version>.yaml.
func (m *InstallManager) Install(repoName, pkgName, version string) error {
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

	return os.Symlink(target, link)
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

	// Clean up empty package directory.
	pkgDir := filepath.Join(m.dir(), pkgName)
	entries, err := os.ReadDir(pkgDir)
	if err == nil && len(entries) == 0 {
		os.Remove(pkgDir)
	}

	return nil
}

// ListInstalled returns all installed packages independently of the
// repositories. Results are sorted by name, then by version.
func (m *InstallManager) ListInstalled() ([]PackageIdentity, error) {
	installedDir := m.dir()

	names, err := os.ReadDir(installedDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []PackageIdentity

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

			out = append(out, PackageIdentity{
				Name:    name.Name(),
				Version: strings.TrimSuffix(fn, ".yaml"),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return CompareVersions(out[i].Version, out[j].Version) < 0
	})

	return out, nil
}

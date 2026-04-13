package systemcontroller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

const (
	PackagesVolumePrefix    = "installed"
	UninstalledVolumePrefix = "uninstalled"
	PagesVolumePrefix       = "pages"
	UserVolumePrefix        = "user"
)

// isReservedFilesystem returns true if the given name is one of the
// system-managed volume prefixes that users must not create, modify, or delete.
func isReservedFilesystem(name string) bool {
	if name == PackagesVolumePrefix || name == UninstalledVolumePrefix || name == ArchivesSubvolume || name == PagesVolumePrefix || name == VMImagesSubvolume || name == UserVolumePrefix || name == TLSSubvolume {
		return true
	}
	if strings.HasPrefix(name, PackagesVolumePrefix+"/") {
		return true
	}
	if strings.HasPrefix(name, UninstalledVolumePrefix+"/") {
		return true
	}
	if strings.HasPrefix(name, ArchivesSubvolume+"/") {
		return true
	}
	if strings.HasPrefix(name, PagesVolumePrefix+"/") {
		return true
	}
	if strings.HasPrefix(name, VMImagesSubvolume+"/") {
		return true
	}
	if strings.HasPrefix(name, UserVolumePrefix+"/") {
		return true
	}
	if strings.HasPrefix(name, TLSSubvolume+"/") {
		return true
	}
	return false
}

// isPackageVolume returns true when name is an installed or uninstalled package
// volume path (has an installed/ or uninstalled/ prefix followed by content).
func isPackageVolume(name string) bool {
	return strings.HasPrefix(name, PackagesVolumePrefix+"/") ||
		strings.HasPrefix(name, UninstalledVolumePrefix+"/")
}

// stripRepoComponent removes the leading repository segment from a package
// volume path. e.g. "default/nginx/2.0/data" becomes "nginx/2.0/data".
func stripRepoComponent(path string) string {
	if _, after, ok := strings.Cut(path, "/"); ok {
		return after
	}
	return path
}

// classifyFilesystem determines the state of a filesystem based on its name
// prefix. Returns the state ("user", "installed", "uninstalled") and the
// display name with internal prefixes and repository component stripped.
// Root subvolumes (installed, uninstalled, empty name) return empty state to
// signal they should be skipped.
func classifyFilesystem(name string) (state, displayName string) {
	if name == "" || name == PackagesVolumePrefix || name == UninstalledVolumePrefix || name == ArchivesSubvolume || name == PagesVolumePrefix || name == VMImagesSubvolume || name == UserVolumePrefix {
		return "", name
	}

	archivesPrefix := ArchivesSubvolume + "/"
	if strings.HasPrefix(name, archivesPrefix) {
		return "", name
	}

	pagesPrefix := PagesVolumePrefix + "/"
	if strings.HasPrefix(name, pagesPrefix) {
		return "", name
	}

	vmImagesPrefix := VMImagesSubvolume + "/"
	if strings.HasPrefix(name, vmImagesPrefix) {
		return "", name
	}

	installedPrefix := PackagesVolumePrefix + "/"
	uninstalledPrefix := UninstalledVolumePrefix + "/"

	if after, ok := strings.CutPrefix(name, installedPrefix); ok {
		return "installed", stripRepoComponent(after)
	}
	if after, ok := strings.CutPrefix(name, uninstalledPrefix); ok {
		return "uninstalled", stripRepoComponent(after)
	}

	userPrefix := UserVolumePrefix + "/"
	if after, ok := strings.CutPrefix(name, userPrefix); ok {
		return "user", after
	}

	return "", name
}

// serviceNameFromVolumePath derives the systemd service unit name from a
// volume internal name like "installed/repo/name/version/volName". Returns
// empty string if the path does not have enough components.
func serviceNameFromVolumePath(internalName string) string {
	parts := strings.SplitN(internalName, "/", 5)
	if len(parts) < 4 {
		return ""
	}
	// parts: [prefix, repo, name, version, ...]
	return systemd.UnitName(parts[1], parts[2], parts[3])
}

func packageVolumePath(repo, name, version, volName string) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", PackagesVolumePrefix, repo, name, version, volName)
}

func packagePrefix(repo, name string) string {
	return fmt.Sprintf("%s/%s/%s/", PackagesVolumePrefix, repo, name)
}

type FilesystemName struct {
	Name      string `json:"name"`
	SortBy    string `json:"sort_by"`
	SortOrder string `json:"sort_order"`
	State     string `json:"state,omitempty"`
	Limit     int    `json:"limit"`
	Offset    int    `json:"offset"`
	Search    string `json:"search"`
}

type ModifyFilesystemRequest struct {
	Name       string             `json:"name"`
	Filesystem storage.Filesystem `json:"filesystem"`
}

// resolveArchiveSubvolume applies the user/ prefix to subvolume names that
// don't already carry an internal prefix (installed/, uninstalled/, etc.).
func resolveArchiveSubvolume(name string) string {
	for _, prefix := range []string{
		PackagesVolumePrefix + "/",
		UninstalledVolumePrefix + "/",
		ArchivesSubvolume + "/",
		PagesVolumePrefix + "/",
		VMImagesSubvolume + "/",
		UserVolumePrefix + "/",
	} {
		if strings.HasPrefix(name, prefix) {
			return name
		}
	}
	return fmt.Sprintf("%s/%s", UserVolumePrefix, name)
}

// --- Storage handlers ---

func (s *SystemControllerHandlers) createFilesystem(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	fs := storage.Filesystem{}

	if err := de.Decode(&fs); err != nil {
		return err
	}

	if fs.State == "installed" || fs.State == "uninstalled" {
		return storage.ErrReservedFilesystem
	}

	if isReservedFilesystem(fs.Name) {
		return storage.ErrReservedFilesystem
	}

	fs.State = ""
	fs.Name = fmt.Sprintf("%s/%s", UserVolumePrefix, fs.Name)

	if err := s.Controller.GetStorage().CreateFilesystem(fs); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) removeFilesystem(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	fs := FilesystemName{}

	if err := de.Decode(&fs); err != nil {
		return err
	}

	if fs.State == "installed" || fs.State == "uninstalled" {
		return storage.ErrReservedFilesystem
	}

	if fs.Name == "" {
		return storage.ErrRootFilesystem
	}

	if isReservedFilesystem(fs.Name) {
		return storage.ErrReservedFilesystem
	}

	if err := s.Controller.GetStorage().RemoveFilesystem(fmt.Sprintf("%s/%s", UserVolumePrefix, fs.Name)); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) modifyFilesystem(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := ModifyFilesystemRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if req.Name == "" {
		return storage.ErrRootFilesystem
	}

	if isPackageVolume(req.Name) && req.Filesystem.Name != req.Name {
		return storage.ErrPackageVolumeRename
	}

	req.Filesystem.State = ""

	if !isPackageVolume(req.Name) {
		req.Name = fmt.Sprintf("%s/%s", UserVolumePrefix, req.Name)
		req.Filesystem.Name = fmt.Sprintf("%s/%s", UserVolumePrefix, req.Filesystem.Name)
	}

	if err := s.Controller.GetStorage().ModifyFilesystem(req.Name, req.Filesystem); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

// --- Package volume types ---

type PackageVolume struct {
	Name         string `json:"name"`          // volume sub-path e.g. "1.0/data"
	InternalName string `json:"internal_name"` // full path e.g. "installed/repo-a/nginx/1.0/data"
	Repo         string `json:"repo"`          // repository name
	Quota        uint64 `json:"quota"`
	State        string `json:"state"` // "installed" or "uninstalled"
}

type PackageVolumeGroup struct {
	Package string          `json:"package"` // display name, e.g. "nginx" or "repo-a/nginx" on collision
	Repo    string          `json:"repo"`    // always present for API calls
	Volumes []PackageVolume `json:"volumes"`
}

type PackageVolumesRequest struct {
	IncludeUninstalled bool `json:"include_uninstalled"`
}

type RemovePackageVolumeRequest struct {
	InternalName string `json:"internal_name"`
}

func (s *SystemControllerHandlers) listPackageVolumes(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageVolumesRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	list, err := s.Controller.GetStorage().ListFilesystems("")
	if err != nil {
		return err
	}

	// Group volumes by repo/name key.
	type groupKey struct {
		repo string
		name string
	}
	groups := map[groupKey][]PackageVolume{}
	nameToRepos := map[string]map[string]bool{} // package name → set of repos

	for _, f := range list {
		var prefix, state string
		if after, ok := strings.CutPrefix(f.Name, PackagesVolumePrefix+"/"); ok {
			prefix = after
			state = "installed"
		} else if after, ok := strings.CutPrefix(f.Name, UninstalledVolumePrefix+"/"); ok {
			prefix = after
			state = "uninstalled"
		} else {
			continue
		}

		if !req.IncludeUninstalled && state == "uninstalled" {
			continue
		}

		// Parse: repo/name/version/volName
		parts := strings.SplitN(prefix, "/", 4)
		if len(parts) < 4 {
			continue // intermediate subvolume, skip
		}

		repo := parts[0]
		pkgName := parts[1]
		subPath := parts[2] + "/" + parts[3] // version/volName

		key := groupKey{repo: repo, name: pkgName}
		groups[key] = append(groups[key], PackageVolume{
			Name:         subPath,
			InternalName: f.Name,
			Repo:         repo,
			Quota:        f.Quota,
			State:        state,
		})

		if nameToRepos[pkgName] == nil {
			nameToRepos[pkgName] = map[string]bool{}
		}
		nameToRepos[pkgName][repo] = true
	}

	// Build result, detecting name collisions.
	result := make([]PackageVolumeGroup, 0, len(groups))
	for key, vols := range groups {
		displayName := key.name
		if len(nameToRepos[key.name]) > 1 {
			displayName = key.repo + "/" + key.name
		}

		sort.Slice(vols, func(i, j int) bool {
			return vols[i].Name < vols[j].Name
		})

		result = append(result, PackageVolumeGroup{
			Package: displayName,
			Repo:    key.repo,
			Volumes: vols,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Package < result[j].Package
	})

	return c.JSON(200, result)
}

func (s *SystemControllerHandlers) removePackageVolume(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := RemovePackageVolumeRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if !strings.HasPrefix(req.InternalName, PackagesVolumePrefix+"/") &&
		!strings.HasPrefix(req.InternalName, UninstalledVolumePrefix+"/") {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "internal_name must start with installed/ or uninstalled/",
		})
	}

	if err := s.Controller.GetStorage().RemoveFilesystem(req.InternalName); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listFilesystems(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	fs := FilesystemName{}

	if err := de.Decode(&fs); err != nil {
		return err
	}

	prefix := fs.Name
	if prefix != "" {
		prefix = fmt.Sprintf("%s/%s", UserVolumePrefix, prefix)
	}

	list, err := s.Controller.GetStorage().ListFilesystems(prefix)
	if err != nil {
		return err
	}

	filtered := make([]storage.Filesystem, 0, len(list))
	for _, f := range list {
		state, displayName := classifyFilesystem(f.Name)
		if state == "" {
			continue
		}
		if fs.State != "" && state != fs.State {
			continue
		}
		if state == "installed" || state == "uninstalled" {
			f.InternalName = f.Name
		}
		f.Name = displayName
		f.State = state
		filtered = append(filtered, f)
	}

	filtered = filterSearch(filtered, fs.Search)
	sortSlice(filtered, fs.SortBy, fs.SortOrder)

	return c.JSON(200, paginate(filtered, fs.Limit, fs.Offset))
}

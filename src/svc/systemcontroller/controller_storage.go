package systemcontroller

import (
	"encoding/json"
	"fmt"
	"strings"

	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

const (
	PackagesVolumePrefix    = "installed"
	UninstalledVolumePrefix = "uninstalled"
	PagesVolumePrefix       = "pages"
)

// isReservedFilesystem returns true if the given name is one of the
// system-managed volume prefixes that users must not create, modify, or delete.
func isReservedFilesystem(name string) bool {
	if name == PackagesVolumePrefix || name == UninstalledVolumePrefix || name == ArchivesSubvolume || name == PagesVolumePrefix {
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
	return false
}

// isPackageVolume returns true when name is an installed or uninstalled package
// volume path (has an installed/ or uninstalled/ prefix followed by content).
func isPackageVolume(name string) bool {
	return strings.HasPrefix(name, PackagesVolumePrefix+"/") ||
		strings.HasPrefix(name, UninstalledVolumePrefix+"/")
}

// classifyFilesystem determines the state of a filesystem based on its name
// prefix. Returns the state ("user", "installed", "uninstalled") and the
// display name with internal prefixes stripped. Root subvolumes (installed,
// uninstalled, empty name) return empty state to signal they should be skipped.
func classifyFilesystem(name string) (state, displayName string) {
	if name == "" || name == PackagesVolumePrefix || name == UninstalledVolumePrefix || name == ArchivesSubvolume || name == PagesVolumePrefix {
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

	installedPrefix := PackagesVolumePrefix + "/"
	uninstalledPrefix := UninstalledVolumePrefix + "/"

	if after, ok := strings.CutPrefix(name, installedPrefix); ok {
		return "installed", after
	}
	if after, ok := strings.CutPrefix(name, uninstalledPrefix); ok {
		return "uninstalled", after
	}

	return "user", name
}

// serviceNameFromVolumePath derives the systemd service unit name from a
// volume display name like "repo/name/version/volName". Returns empty string
// if the path does not have enough components.
func serviceNameFromVolumePath(volumePath string) string {
	parts := strings.SplitN(volumePath, "/", 4)
	if len(parts) < 3 {
		return ""
	}
	return systemd.UnitName(parts[0], parts[1], parts[2])
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

	if fs.Quota == 0 {
		fs.Quota = s.defaultQuota()
	}

	fs.State = ""

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

	if err := s.Controller.GetStorage().RemoveFilesystem(fs.Name); err != nil {
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

	if err := s.Controller.GetStorage().ModifyFilesystem(req.Name, req.Filesystem); err != nil {
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

	list, err := s.Controller.GetStorage().ListFilesystems(fs.Name)
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
		f.Name = displayName
		f.State = state
		filtered = append(filtered, f)
	}

	filtered = filterSearch(filtered, fs.Search)
	sortSlice(filtered, fs.SortBy, fs.SortOrder)

	return c.JSON(200, paginate(filtered, fs.Limit, fs.Offset))
}

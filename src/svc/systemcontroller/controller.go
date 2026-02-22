package systemcontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

const (
	PackagesVolumePrefix    = "installed"
	UninstalledVolumePrefix = "uninstalled"
)

// isReservedFilesystem returns true if the given name is one of the
// system-managed volume prefixes that users must not create, modify, or delete.
func isReservedFilesystem(name string) bool {
	if name == PackagesVolumePrefix || name == UninstalledVolumePrefix {
		return true
	}
	if strings.HasPrefix(name, fmt.Sprintf("%s/", PackagesVolumePrefix)) {
		return true
	}
	if strings.HasPrefix(name, fmt.Sprintf("%s/", UninstalledVolumePrefix)) {
		return true
	}
	return false
}

// classifyFilesystem determines the state of a filesystem based on its name
// prefix. Returns the state ("user", "installed", "uninstalled") and the
// display name with internal prefixes stripped. Root subvolumes (installed,
// uninstalled, empty name) return empty state to signal they should be skipped.
func classifyFilesystem(name string) (state, displayName string) {
	if name == "" || name == PackagesVolumePrefix || name == UninstalledVolumePrefix {
		return "", name
	}

	installedPrefix := fmt.Sprintf("%s/", PackagesVolumePrefix)
	uninstalledPrefix := fmt.Sprintf("%s/", UninstalledVolumePrefix)

	if strings.HasPrefix(name, installedPrefix) {
		return "installed", strings.TrimPrefix(name, installedPrefix)
	}
	if strings.HasPrefix(name, uninstalledPrefix) {
		return "uninstalled", strings.TrimPrefix(name, uninstalledPrefix)
	}

	return "user", name
}

func packageVolumePath(name, version, volName string) string {
	return fmt.Sprintf("%s/%s/%s/%s", PackagesVolumePrefix, name, version, volName)
}

func packagePrefix(name string) string {
	return fmt.Sprintf("%s/%s/", PackagesVolumePrefix, name)
}

type systemControllerBackend interface {
	GetStorage() storage.Storage
	GetRepositoryRoot() *packages.RepositoryRoot
	GetInstaller() packages.Installer
	GetSystemdManager() systemd.Manager
	GetAccountManager() account.Manager
	GetSessionManager() account.SessionManager
	GetAuditManager() account.AuditManager
	GetSettingsManager() account.SettingsManager
	GetAllowedHosts() []string
	GetDefaultRepoCredentials() (string, string)
	GetBtrfsBasePath() string
	GetUPnPBinPath() string
	GetNetworkMode() string
}

type SystemController interface {
	systemControllerBackend
	Run() error
	Client() (*SystemdClient, error)
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

type AddRepositoryRequest struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type RepositoryNameRequest struct {
	Name string `json:"name"`
}

type MoveRepositoryRequest struct {
	Name     string `json:"name"`
	Position int    `json:"position"`
}

type RepositoryInfo struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Error    string `json:"error,omitempty"`
}

type PackageNameRequest struct {
	Name string `json:"name"`
}

type PackageIdentityRequest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InstallRequest struct {
	Name              string             `json:"name"`
	Version           string             `json:"version"`
	Responses         packages.Responses `json:"responses"`
	ReuseVolumes      bool               `json:"reuse_volumes"`
	ImportFromVersion string             `json:"import_from_version,omitempty"`
}

type UninstallRequest struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	PurgeVolumes bool   `json:"purge_volumes"`
}

type GetResponsesRequest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InstalledInfoResponse struct {
	Questions map[string]packages.Question `json:"questions"`
	Responses packages.Responses           `json:"responses"`
	Notes     map[string]string            `json:"notes"`
}

type UninstalledVolumesResponse struct {
	HasUninstalledVolumes bool     `json:"has_uninstalled_volumes"`
	UninstalledVersions   []string `json:"uninstalled_versions,omitempty"`
	InstalledVersions     []string `json:"installed_versions,omitempty"`
}

type SetStatusRequest struct {
	Name   string               `json:"name"`
	Action systemd.StatusAction `json:"action"`
}

type CreateAccountRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	RealName string `json:"real_name"`
	Admin    bool   `json:"admin"`
}

type GetAccountRequest struct {
	Username string `json:"username"`
}

type UpdateAccountRequest struct {
	Username string               `json:"username"`
	Fields   account.UpdateFields `json:"fields"`
}

type DisableAccountRequest struct {
	Username string `json:"username"`
}

type EnableAccountRequest struct {
	Username string `json:"username"`
}

type AuthenticateRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AuthenticateResponse struct {
	Token   string           `json:"token"`
	Account *account.Account `json:"account"`
}

type RevokeSessionRequest struct {
	SessionID string `json:"session_id"`
}

type SessionUsernameResponse struct {
	Username string `json:"username"`
}

type PingResponse struct {
	Status       string      `json:"status"`
	Filesystems  int         `json:"filesystems"`
	Repositories int         `json:"repositories"`
	Packages     int         `json:"packages"`
	Installed    int         `json:"installed"`
	Accounts     int         `json:"accounts"`
	Admins       int         `json:"admins"`
	Units        *UnitCounts `json:"units,omitempty"`
	RecentErrors int         `json:"recent_errors"`
	NeedsSetup   bool        `json:"needs_setup"`
}

type UnitCounts struct {
	Total  int `json:"total"`
	Active int `json:"active"`
	Failed int `json:"failed"`
}

type SystemControllerHandlers struct {
	Controller systemControllerBackend
}

func getHandler(sc systemControllerBackend) *SystemControllerHandlers {
	return &SystemControllerHandlers{Controller: sc}
}


// defaultQuota returns the system-wide default quota in bytes.
// If no settings manager is configured or the value is missing/invalid, it
// returns 0 (no quota).
func (s *SystemControllerHandlers) defaultQuota() uint64 {
	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return 0
	}

	val, err := mgr.Get("default_quota")
	if err != nil {
		return 0
	}

	q, err := strconv.ParseUint(val, 10, 64)
	if err != nil {
		return 0
	}

	return q
}

// installPackageUnits installs all systemd unit files for a package, enables
// socket and timer units, and starts the main service.
func (s *SystemControllerHandlers) installPackageUnits(ctx context.Context, sd systemd.Manager, units systemd.PackageUnits) error {
	// Install all unit files.
	if err := sd.InstallUnit(ctx, units.Service.Name, units.Service.Content); err != nil {
		return fmt.Errorf("install service unit: %w", err)
	}
	for _, sock := range units.Sockets {
		if err := sd.InstallUnit(ctx, sock.Name, sock.Content); err != nil {
			return fmt.Errorf("install socket unit %s: %w", sock.Name, err)
		}
	}
	for _, fwd := range units.Forwarders {
		if err := sd.InstallUnit(ctx, fwd.Name, fwd.Content); err != nil {
			return fmt.Errorf("install forwarder unit %s: %w", fwd.Name, err)
		}
	}
	if units.UPnPService != nil {
		if err := sd.InstallUnit(ctx, units.UPnPService.Name, units.UPnPService.Content); err != nil {
			return fmt.Errorf("install upnp service unit: %w", err)
		}
	}
	if units.UPnPTimer != nil {
		if err := sd.InstallUnit(ctx, units.UPnPTimer.Name, units.UPnPTimer.Content); err != nil {
			return fmt.Errorf("install upnp timer unit: %w", err)
		}
	}

	// Enable socket, forwarder, and timer units.
	for _, sock := range units.Sockets {
		if err := sd.SetStatus(ctx, sock.Name, systemd.Enable); err != nil {
			return fmt.Errorf("enable socket %s: %w", sock.Name, err)
		}
	}
	for _, fwd := range units.Forwarders {
		if err := sd.SetStatus(ctx, fwd.Name, systemd.Enable); err != nil {
			return fmt.Errorf("enable forwarder %s: %w", fwd.Name, err)
		}
	}
	if units.UPnPTimer != nil {
		if err := sd.SetStatus(ctx, units.UPnPTimer.Name, systemd.Enable); err != nil {
			return fmt.Errorf("enable upnp timer: %w", err)
		}
	}

	// Start the main service.
	if err := sd.SetStatus(ctx, units.Service.Name, systemd.Start); err != nil {
		return fmt.Errorf("start service: %w", err)
	}

	return nil
}

// uninstallPackageUnits stops, disables, and uninstalls all systemd units for
// a package by scanning installed unit files.
func (s *SystemControllerHandlers) uninstallPackageUnits(ctx context.Context, sd systemd.Manager, pkgName string) error {
	unitNames, err := sd.ListPackageUnitFiles(ctx, pkgName)
	if err != nil {
		return fmt.Errorf("list package unit files: %w", err)
	}

	for _, name := range unitNames {
		if err := sd.SetStatus(ctx, name, systemd.Stop); err != nil {
			slog.Debug(fmt.Sprintf("stop unit %s: %v", name, err))
		}
		if err := sd.SetStatus(ctx, name, systemd.Disable); err != nil {
			slog.Debug(fmt.Sprintf("disable unit %s: %v", name, err))
		}
		if err := sd.UninstallUnit(ctx, name); err != nil {
			slog.Debug(fmt.Sprintf("uninstall unit %s: %v", name, err))
		}
	}
	return nil
}

// packageUnitConfig builds a PackageUnitConfig from a compiled package and
// backend configuration.
func (s *SystemControllerHandlers) packageUnitConfig(pkgName, version string, compiled *packages.Package) systemd.PackageUnitConfig {
	return systemd.PackageUnitConfig{
		PkgName:     pkgName,
		Version:     version,
		Image:       compiled.Image,
		Command:     compiled.Command,
		Environment: compiled.Environment,
		External:    compiled.Network.External,
		Internal:    compiled.Network.Internal,
		Volumes:     compiled.Volumes,
		BtrfsBase:   s.Controller.GetBtrfsBasePath(),
		UPnPBinPath: s.Controller.GetUPnPBinPath(),
		NetworkMode: s.Controller.GetNetworkMode(),
	}
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

	if req.Filesystem.State == "installed" || req.Filesystem.State == "uninstalled" {
		return storage.ErrReservedFilesystem
	}

	if req.Name == "" {
		return storage.ErrRootFilesystem
	}

	if isReservedFilesystem(req.Name) {
		return storage.ErrReservedFilesystem
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

// --- Repository handlers ---

func (s *SystemControllerHandlers) addRepository(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := AddRepositoryRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	u, err := url.Parse(req.URL)
	if err != nil {
		return fmt.Errorf("invalid url: %v", err)
	}

	if req.Username == "" && req.Password == "" {
		req.Username, req.Password = s.Controller.GetDefaultRepoCredentials()
	}

	rr := s.Controller.GetRepositoryRoot()

	repo, err := packages.NewRepository(rr.BaseDir, req.Name, *u, req.Username, req.Password)
	if err != nil {
		return err
	}

	if err := rr.Add(*repo); err != nil {
		return err
	}

	rr.Refresh()

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) removeRepository(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := RepositoryNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()

	if err := rr.Remove(req.Name); err != nil {
		return err
	}

	rr.Refresh()

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) moveRepository(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := MoveRepositoryRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()

	if err := rr.Move(req.Name, req.Position); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) refreshRepositories(c *echo.Context) error {
	rr := s.Controller.GetRepositoryRoot()
	rr.Refresh()
	errs := rr.RefreshErrors()
	if len(errs) > 0 {
		return c.JSON(200, errs)
	}
	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listRepositories(c *echo.Context) error {
	rr := s.Controller.GetRepositoryRoot()

	repos, err := rr.List()
	if err != nil {
		return err
	}

	errs := rr.RefreshErrors()
	out := make([]RepositoryInfo, len(repos))
	for i, r := range repos {
		out[i] = RepositoryInfo{Name: r.Name, URL: r.URL.String(), Username: r.Username, Error: errs[r.Name]}
	}

	p := readListParams(c)
	out = filterSearch(out, p.Search)
	sortSlice(out, p.SortBy, p.SortOrder)

	return c.JSON(200, paginate(out, p.Limit, p.Offset))
}

// --- Package handlers ---

func (s *SystemControllerHandlers) listPackages(c *echo.Context) error {
	rr := s.Controller.GetRepositoryRoot()

	pkgs, err := rr.ListPackages()
	if err != nil {
		return err
	}

	// Merge installed packages that may not be the latest version.
	inst := s.Controller.GetInstaller()
	if inst != nil {
		installed, err := inst.ListInstalled()
		if err != nil {
			return err
		}

		known := map[string]bool{}
		for _, pkg := range pkgs {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			known[pi.Name] = true
		}

		for _, pkg := range installed {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			if !known[pi.Name] {
				pkgs = append(pkgs, pkg)
				known[pi.Name] = true
			}
		}
	}

	p := readListParams(c)
	pkgs = filterSearch(pkgs, p.Search)
	sort.Strings(pkgs)
	if strings.EqualFold(p.SortOrder, "desc") {
		sort.Sort(sort.Reverse(sort.StringSlice(pkgs)))
	}

	return c.JSON(200, paginate(pkgs, p.Limit, p.Offset))
}

func (s *SystemControllerHandlers) listPackageVersions(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()

	versions, err := rr.ListPackageVersions(req.Name)
	if err != nil {
		return err
	}

	return c.JSON(200, versions)
}

func (s *SystemControllerHandlers) getPackageQuestions(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()

	questions, err := rr.GetPackageQuestions(req.Name)
	if err != nil {
		return err
	}

	return c.JSON(200, questions)
}

func (s *SystemControllerHandlers) getPackageQuestionsByIdentity(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageIdentityRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()

	repoName, err := rr.FindRepoForPackage(req.Name, req.Version)
	if err != nil {
		return err
	}

	ip, err := rr.LoadPackage(repoName, req.Name, req.Version)
	if err != nil {
		return err
	}

	return c.JSON(200, ip.Questions)
}

// --- Install handlers ---

func (s *SystemControllerHandlers) installPackage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := InstallRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()
	repoName, err := rr.FindRepoForPackage(req.Name, req.Version)
	if err != nil {
		return err
	}

	// Load and compile the package to resolve volume definitions.
	ip, err := rr.LoadPackage(repoName, req.Name, req.Version)
	if err != nil {
		return err
	}

	compiled, err := ip.Compile(req.Responses)
	if err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	ctx := c.Request().Context()

	// Check for any installed version of the same package.
	installed, err := inst.ListInstalled()
	if err != nil {
		return err
	}

	var activeVersion string
	for _, pkg := range installed {
		pi, err := packages.ParsePackageIdentity(pkg)
		if err != nil {
			continue
		}
		if pi.Name == req.Name {
			activeVersion = pi.Version
			break
		}
	}

	if activeVersion != "" {
		// Stop and remove all systemd units for the currently active version.
		if sd := s.Controller.GetSystemdManager(); sd != nil {
			if err := s.uninstallPackageUnits(ctx, sd, req.Name); err != nil {
				return err
			}
		}

		if activeVersion == req.Version {
			// Same version reinstall: remove the install record (but not volumes).
			if err := inst.Uninstall(req.Name, req.Version); err != nil {
				return err
			}
		}
		// Different version: keep old install record and volumes; only the unit is stopped.
	}

	// Handle volume reuse/import and create or adjust storage volumes.
	if st := s.Controller.GetStorage(); st != nil {
		// If reusing volumes, rename uninstalled/<name> → installed/<name>.
		if req.ReuseVolumes {
			src := fmt.Sprintf("%s/%s", UninstalledVolumePrefix, req.Name)
			dst := fmt.Sprintf("%s/%s", PackagesVolumePrefix, req.Name)
			if err := st.RenameFilesystem(src, dst); err != nil {
				slog.Debug(fmt.Sprintf("reuse volumes: rename %s -> %s: %v", src, dst, err))
			}
		}

		defQuota := s.defaultQuota()
		for volName, vol := range compiled.Volumes {
			quota := vol.Quota
			if quota == 0 {
				quota = defQuota
			}
			fsName := packageVolumePath(req.Name, req.Version, volName)

			if req.ImportFromVersion != "" {
				// Import from another version: snapshot from the source version's volume.
				srcVol := packageVolumePath(req.Name, req.ImportFromVersion, volName)
				if err := st.SnapshotFilesystem(srcVol, fsName); err != nil {
					slog.Debug(fmt.Sprintf("import snapshot %s -> %s: %v", srcVol, fsName, err))
					// Fall through to create if snapshot fails.
					if err := st.CreateFilesystem(storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
						if err := st.ModifyFilesystem(fsName, storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
							return err
						}
					}
				} else {
					// Snapshot succeeded; adjust quota if needed.
					if err := st.ModifyFilesystem(fsName, storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
						slog.Debug(fmt.Sprintf("adjust quota on snapshot %s: %v", fsName, err))
					}
				}
			} else {
				if err := st.CreateFilesystem(storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
					// Volume already exists — adjust quota if needed.
					if err := st.ModifyFilesystem(fsName, storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
						return err
					}
				}
			}
		}
	}

	if err := inst.Install(repoName, req.Name, req.Version, req.Responses); err != nil {
		return err
	}

	if sd := s.Controller.GetSystemdManager(); sd != nil {
		cfg := s.packageUnitConfig(req.Name, req.Version, compiled)
		units := systemd.GeneratePackageUnits(cfg)
		if err := s.installPackageUnits(ctx, sd, units); err != nil {
			return err
		}
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) uninstallPackage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := UninstallRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	ctx := c.Request().Context()
	inst := s.Controller.GetInstaller()

	if sd := s.Controller.GetSystemdManager(); sd != nil {
		if err := s.uninstallPackageUnits(ctx, sd, req.Name); err != nil {
			return err
		}
	}

	if err := inst.SetDisabled(req.Name, false); err != nil {
		return err
	}
	if err := inst.Uninstall(req.Name, req.Version); err != nil {
		return err
	}

	// Volume handling after uninstall.
	if req.PurgeVolumes {
		if err := s.purgePackageVolumes(req.Name); err != nil {
			return err
		}
	} else if st := s.Controller.GetStorage(); st != nil {
		// Check if any other versions remain installed.
		installed, err := inst.ListInstalled()
		if err != nil {
			return err
		}

		otherVersionInstalled := false
		for _, pkg := range installed {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			if pi.Name == req.Name {
				otherVersionInstalled = true
				break
			}
		}

		if !otherVersionInstalled {
			// Move installed/<name> → uninstalled/<name>.
			src := fmt.Sprintf("%s/%s", PackagesVolumePrefix, req.Name)
			dst := fmt.Sprintf("%s/%s", UninstalledVolumePrefix, req.Name)
			if err := st.RenameFilesystem(src, dst); err != nil {
				slog.Debug(fmt.Sprintf("preserve volumes: rename %s -> %s: %v", src, dst, err))
			}
		}
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) purgePackageVolumes(name string) error {
	st := s.Controller.GetStorage()
	if st == nil {
		return nil
	}

	// Purge from installed/<name>/.
	if err := s.purgeVolumePrefix(st, packagePrefix(name)); err != nil {
		return err
	}

	// Remove the installed/<name> parent subvolume itself.
	parentPath := fmt.Sprintf("%s/%s", PackagesVolumePrefix, name)
	if err := st.RemoveFilesystem(parentPath); err != nil {
		slog.Debug(fmt.Sprintf("purge parent volume %s: %v", parentPath, err))
	}

	// Also purge from uninstalled/<name>/.
	uninstPrefix := fmt.Sprintf("%s/%s/", UninstalledVolumePrefix, name)
	if err := s.purgeVolumePrefix(st, uninstPrefix); err != nil {
		return err
	}

	uninstParent := fmt.Sprintf("%s/%s", UninstalledVolumePrefix, name)
	if err := st.RemoveFilesystem(uninstParent); err != nil {
		slog.Debug(fmt.Sprintf("purge uninstalled parent volume %s: %v", uninstParent, err))
	}

	return nil
}

func (s *SystemControllerHandlers) purgeVolumePrefix(st storage.Storage, prefix string) error {
	filesystems, err := st.ListFilesystems(prefix)
	if err != nil {
		return err
	}

	// Sort deepest-first so child subvolumes are removed before parents.
	sort.Slice(filesystems, func(i, j int) bool {
		return strings.Count(filesystems[i].Name, "/") > strings.Count(filesystems[j].Name, "/")
	})

	for _, fs := range filesystems {
		if err := st.RemoveFilesystem(fs.Name); err != nil {
			return err
		}
	}

	return nil
}

func (s *SystemControllerHandlers) purgeVolumes(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if err := s.purgePackageVolumes(req.Name); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listUninstalledVolumes(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	resp := UninstalledVolumesResponse{}

	st := s.Controller.GetStorage()
	if st != nil {
		// Check uninstalled/<name>/ for existing volume trees.
		uninstPrefix := fmt.Sprintf("%s/%s/", UninstalledVolumePrefix, req.Name)
		filesystems, err := st.ListFilesystems(uninstPrefix)
		if err != nil {
			return err
		}

		if len(filesystems) > 0 {
			resp.HasUninstalledVolumes = true
			// Extract unique versions from uninstalled/<name>/<version>/...
			versionSet := map[string]bool{}
			for _, fs := range filesystems {
				rest := strings.TrimPrefix(fs.Name, uninstPrefix)
				parts := strings.SplitN(rest, "/", 2)
				if len(parts) > 0 && parts[0] != "" {
					versionSet[parts[0]] = true
				}
			}
			for v := range versionSet {
				resp.UninstalledVersions = append(resp.UninstalledVersions, v)
			}
			sort.Strings(resp.UninstalledVersions)
		}

		// Extract installed versions from installed/<name>/<version>/...
		instPrefix := packagePrefix(req.Name)
		instFS, err := st.ListFilesystems(instPrefix)
		if err != nil {
			return err
		}

		instVersionSet := map[string]bool{}
		for _, fs := range instFS {
			rest := strings.TrimPrefix(fs.Name, instPrefix)
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) > 0 && parts[0] != "" {
				instVersionSet[parts[0]] = true
			}
		}
		for v := range instVersionSet {
			resp.InstalledVersions = append(resp.InstalledVersions, v)
		}
		sort.Strings(resp.InstalledVersions)
	}

	return c.JSON(200, resp)
}

func (s *SystemControllerHandlers) purgeUninstalledVolumes(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	st := s.Controller.GetStorage()
	if st == nil {
		c.Response().WriteHeader(200)
		return nil
	}

	uninstPrefix := fmt.Sprintf("%s/%s/", UninstalledVolumePrefix, req.Name)
	if err := s.purgeVolumePrefix(st, uninstPrefix); err != nil {
		return err
	}

	uninstParent := fmt.Sprintf("%s/%s", UninstalledVolumePrefix, req.Name)
	if err := st.RemoveFilesystem(uninstParent); err != nil {
		slog.Debug(fmt.Sprintf("purge uninstalled parent %s: %v", uninstParent, err))
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listInstalled(c *echo.Context) error {
	inst := s.Controller.GetInstaller()

	pkgs, err := inst.ListInstalled()
	if err != nil {
		return err
	}

	p := readListParams(c)
	pkgs = filterSearch(pkgs, p.Search)
	sort.Strings(pkgs)
	if strings.EqualFold(p.SortOrder, "desc") {
		sort.Sort(sort.Reverse(sort.StringSlice(pkgs)))
	}

	return c.JSON(200, paginate(pkgs, p.Limit, p.Offset))
}

func (s *SystemControllerHandlers) getResponses(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := GetResponsesRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	resp, err := inst.GetResponses(req.Name, req.Version)
	if err != nil {
		return err
	}

	return c.JSON(200, resp)
}

func (s *SystemControllerHandlers) getInstalledInfo(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageIdentityRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	responses, err := inst.GetResponses(req.Name, req.Version)
	if err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()

	repoName, err := rr.FindRepoForPackage(req.Name, req.Version)
	if err != nil {
		return err
	}

	ip, err := rr.LoadPackage(repoName, req.Name, req.Version)
	if err != nil {
		return err
	}

	notes := ip.CompileNotes(responses)

	return c.JSON(200, InstalledInfoResponse{
		Questions: ip.Questions,
		Responses: responses,
		Notes:     notes,
	})
}

// --- Systemd handlers ---

func (s *SystemControllerHandlers) listUnits(c *echo.Context) error {
	units, err := s.Controller.GetSystemdManager().ListUnits(c.Request().Context())
	if err != nil {
		return err
	}

	p := readListParams(c)
	units = filterSearch(units, p.Search)
	sortSlice(units, p.SortBy, p.SortOrder)

	return c.JSON(200, paginate(units, p.Limit, p.Offset))
}

func (s *SystemControllerHandlers) setUnitStatus(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := SetStatusRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if req.Action == systemd.Enable || req.Action == systemd.Disable {
		return echo.NewHTTPError(http.StatusBadRequest, "enable/disable not allowed")
	}

	if req.Action == systemd.Stop && req.Name == "town-os-systemcontroller.service" {
		return echo.NewHTTPError(http.StatusBadRequest, "cannot stop systemcontroller")
	}

	if err := s.Controller.GetSystemdManager().SetStatus(c.Request().Context(), req.Name, req.Action); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

type PackageToggleRequest struct {
	Name string `json:"name"`
}

func (s *SystemControllerHandlers) disablePackage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageToggleRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	if err := inst.SetDisabled(req.Name, true); err != nil {
		return err
	}

	if sd := s.Controller.GetSystemdManager(); sd != nil {
		unitName := systemd.UnitName(req.Name)
		if err := sd.SetStatus(c.Request().Context(), unitName, systemd.Stop); err != nil {
			return err
		}
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) enablePackage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageToggleRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	if err := inst.SetDisabled(req.Name, false); err != nil {
		return err
	}

	if sd := s.Controller.GetSystemdManager(); sd != nil {
		unitName := systemd.UnitName(req.Name)
		if err := sd.SetStatus(c.Request().Context(), unitName, systemd.Start); err != nil {
			return err
		}
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) logReplay(c *echo.Context) error {
	unit := c.QueryParam("unit")

	ch, err := s.Controller.GetSystemdManager().LogReplay(c.Request().Context(), unit)
	if err != nil {
		return err
	}

	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().WriteHeader(200)

	flusher, ok := c.Response().(http.Flusher)
	ctx := c.Request().Context()
	heartbeat := time.NewTicker(time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case entry, open := <-ch:
			if !open {
				return nil
			}
			if _, err := fmt.Fprint(c.Response(), "data: "); err != nil {
				return err
			}
			if err := json.NewEncoder(c.Response()).Encode(entry); err != nil {
				return err
			}
			if _, err := fmt.Fprint(c.Response(), "\n"); err != nil {
				return err
			}
			if ok {
				flusher.Flush()
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(c.Response(), ":\n"); err != nil {
				return err
			}
			if ok {
				flusher.Flush()
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *SystemControllerHandlers) logTail(c *echo.Context) error {
	unit := c.QueryParam("unit")

	lines := 100
	if v := c.QueryParam("lines"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid lines parameter: %w", err)
		}
		lines = n
	}

	params := systemd.LogTailParams{
		Unit:         unit,
		Lines:        lines,
		BeforeCursor: c.QueryParam("before"),
		AfterCursor:  c.QueryParam("after"),
		Grep:         c.QueryParam("grep"),
	}

	if v := c.QueryParam("since"); v != "" {
		sinceUnix, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid since parameter: %w", err)
		}
		params.Since = time.Unix(sinceUnix, 0)
	}

	if v := c.QueryParam("until"); v != "" {
		untilUnix, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid until parameter: %w", err)
		}
		params.Until = time.Unix(untilUnix, 0)
	}

	result, err := s.Controller.GetSystemdManager().LogTail(c.Request().Context(), params)
	if err != nil {
		return err
	}

	return c.JSON(200, result)
}

// --- Account handlers ---

func (s *SystemControllerHandlers) createAccount(c *echo.Context) error {
	sessMgr := s.Controller.GetSessionManager()
	if sessMgr != nil {
		accounts, err := s.Controller.GetAccountManager().List()
		if err != nil {
			return fmt.Errorf("list accounts: %w", err)
		}

		var adminUsernames []string
		for _, a := range accounts {
			if !a.Disabled && a.Admin {
				adminUsernames = append(adminUsernames, a.Username)
			}
		}

		if len(adminUsernames) > 0 {
			hasActiveSessions, err := sessMgr.HasActiveAdminSessions(adminUsernames)
			if err != nil {
				return fmt.Errorf("check active admin sessions: %w", err)
			}

			if hasActiveSessions {
				token := extractBearerToken(c.Request())
				if token == "" {
					return echo.NewHTTPError(401, "missing authorization token")
				}
				_, acct, err := sessMgr.Validate(token)
				if err != nil {
					return echo.NewHTTPError(401, "invalid session")
				}
				if !acct.Admin {
					return echo.NewHTTPError(403, "admin access required")
				}
			}
		}
	}

	de := json.NewDecoder(c.Request().Body)
	req := CreateAccountRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	acct, err := s.Controller.GetAccountManager().Create(req.Username, req.Password, req.Email, req.Phone, req.RealName, req.Admin)
	if err != nil {
		return err
	}

	return c.JSON(200, acct)
}

func (s *SystemControllerHandlers) getAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := GetAccountRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	acct, err := s.Controller.GetAccountManager().Get(req.Username)
	if err != nil {
		return err
	}

	return c.JSON(200, acct)
}

func (s *SystemControllerHandlers) updateAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := UpdateAccountRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if req.Fields.Admin != nil {
		return echo.NewHTTPError(403, "admin status cannot be changed after account creation")
	}

	acct, err := s.Controller.GetAccountManager().Update(req.Username, req.Fields)
	if err != nil {
		return err
	}

	return c.JSON(200, acct)
}


func (s *SystemControllerHandlers) disableAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := DisableAccountRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if err := s.Controller.GetAccountManager().Disable(req.Username); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) enableAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := EnableAccountRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if err := s.Controller.GetAccountManager().Enable(req.Username); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listAccounts(c *echo.Context) error {
	accounts, err := s.Controller.GetAccountManager().List()
	if err != nil {
		return err
	}

	p := readListParams(c)
	accounts = filterSearch(accounts, p.Search)
	sortSlice(accounts, p.SortBy, p.SortOrder)

	return c.JSON(200, paginate(accounts, p.Limit, p.Offset))
}

func (s *SystemControllerHandlers) authenticateAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := AuthenticateRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	acct, err := s.Controller.GetAccountManager().Authenticate(req.Username, req.Password)
	if err != nil {
		return echo.NewHTTPError(401, err.Error())
	}

	token, err := s.Controller.GetSessionManager().Create(req.Username)
	if err != nil {
		return err
	}

	return c.JSON(200, AuthenticateResponse{Token: token, Account: acct})
}

func (s *SystemControllerHandlers) revokeSession(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := RevokeSessionRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if err := s.Controller.GetSessionManager().Revoke(req.SessionID); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listSessions(c *echo.Context) error {
	token := extractBearerToken(c.Request())
	if token == "" {
		return echo.NewHTTPError(401, "missing authorization token")
	}

	sess, _, err := s.Controller.GetSessionManager().Validate(token)
	if err != nil {
		return echo.NewHTTPError(401, "invalid session")
	}

	sessions, err := s.Controller.GetSessionManager().List(sess.Username)
	if err != nil {
		return err
	}

	return c.JSON(200, sessions)
}

func (s *SystemControllerHandlers) sessionUsername(c *echo.Context) error {
	token := extractBearerToken(c.Request())
	if token == "" {
		return echo.NewHTTPError(401, "missing authorization token")
	}

	sess, _, err := s.Controller.GetSessionManager().Validate(token)
	if err != nil {
		return echo.NewHTTPError(401, "invalid session")
	}

	return c.JSON(200, SessionUsernameResponse{Username: sess.Username})
}

// --- Admin middleware ---

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(auth, "Bearer ")
}

func (s *SystemControllerHandlers) requireAdmin(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if s.Controller.GetSessionManager() == nil {
			return next(c)
		}

		token := extractBearerToken(c.Request())
		if token == "" {
			return echo.NewHTTPError(401, "missing authorization token")
		}

		_, acct, err := s.Controller.GetSessionManager().Validate(token)
		if err != nil {
			return echo.NewHTTPError(401, "invalid session")
		}

		if !acct.Admin {
			return echo.NewHTTPError(403, "admin access required")
		}

		return next(c)
	}
}

func (s *SystemControllerHandlers) requireAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if s.Controller.GetSessionManager() == nil {
			return next(c)
		}

		token := extractBearerToken(c.Request())
		if token == "" {
			return echo.NewHTTPError(401, "missing authorization token")
		}

		_, _, err := s.Controller.GetSessionManager().Validate(token)
		if err != nil {
			return echo.NewHTTPError(401, "invalid session")
		}

		return next(c)
	}
}

func (s *SystemControllerHandlers) auditMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		am := s.Controller.GetAuditManager()
		if am == nil {
			return next(c)
		}

		path := c.Request().URL.Path

		excluded := map[string]bool{
			"/account/sessions":              true,
			"/account/me":                    true,
			"/status/ping":                   true,
			"/audit/log":                     true,
			"/storage":                       true,
			"/repository":                    true,
			"/packages":                      true,
			"/packages/installed":            true,
			"/packages/responses":            true,
			"/packages/versions":             true,
			"/packages/questions":            true,
			"/packages/questions/identity":   true,
			"/packages/uninstalled-volumes":  true,
			"/systemd/units":                 true,
			"/systemd/logs":                  true,
			"/systemd/logs/tail":             true,
			"/account":                       true,
			"/settings":                      true,
			"/settings/get":                  true,
		}

		if excluded[path] {
			return next(c)
		}

		// Buffer the request body so we can capture it for audit detail
		// while still allowing the handler to read it.
		var detail string
		if c.Request().Body != nil {
			bodyBytes, err := io.ReadAll(c.Request().Body)
			if closeErr := c.Request().Body.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
			if err == nil && len(bodyBytes) > 0 {
				detail = sanitizeAuditDetail(bodyBytes)
				c.Request().Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}
		}

		handlerErr := next(c)

		var acctName string
		if path == "/account/authenticate" {
			acctName = ""
		} else {
			token := extractBearerToken(c.Request())
			if token != "" && s.Controller.GetSessionManager() != nil {
				_, acct, err := s.Controller.GetSessionManager().Validate(token)
				if err == nil {
					acctName = acct.Username
				}
			}
		}

		action := account.RouteActions[path]

		entry := account.AuditEntry{
			Account:   acctName,
			Action:    action,
			Path:      path,
			Detail:    detail,
			Success:   handlerErr == nil,
			CreatedAt: time.Now().UTC(),
		}
		if handlerErr != nil {
			entry.Error = handlerErr.Error()
		}

		// Best-effort audit logging; don't fail the request if logging fails
		_ = am.LogEntry(entry)

		return handlerErr
	}
}

// sanitizeAuditDetail parses a JSON request body, redacts sensitive fields,
// and returns a compact JSON string for audit logging.
func sanitizeAuditDetail(body []byte) string {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}

	redactSensitive(m)

	out, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(out)
}

// redactSensitive recursively removes keys named "password" from a map.
func redactSensitive(m map[string]any) {
	delete(m, "password")
	for _, v := range m {
		if nested, ok := v.(map[string]any); ok {
			redactSensitive(nested)
		}
	}
}

func (s *SystemControllerHandlers) listAuditLog(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	var opts account.AuditListOptions

	if err := de.Decode(&opts); err != nil {
		return err
	}

	am := s.Controller.GetAuditManager()
	if am == nil {
		return echo.NewHTTPError(500, "audit logging not configured")
	}

	page, err := am.List(opts)
	if err != nil {
		return err
	}

	return c.JSON(200, page)
}

// --- Settings handlers ---

type SetSettingRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type GetSettingRequest struct {
	Key string `json:"key"`
}

func (s *SystemControllerHandlers) getSettings(c *echo.Context) error {
	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return c.JSON(200, map[string]string{})
	}

	settings, err := mgr.List()
	if err != nil {
		return err
	}

	return c.JSON(200, settings)
}

func (s *SystemControllerHandlers) getSetting(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := GetSettingRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return echo.NewHTTPError(404, fmt.Sprintf("setting %q not found", req.Key))
	}

	value, err := mgr.Get(req.Key)
	if err != nil {
		return echo.NewHTTPError(404, fmt.Sprintf("setting %q not found", req.Key))
	}

	return c.JSON(200, map[string]string{"key": req.Key, "value": value})
}

// byteValueSettings are setting keys whose values represent byte counts
// and should be normalized through ParseBytes so that human-readable
// strings like "500GB" are stored as numeric byte values.
var byteValueSettings = map[string]bool{
	"default_quota": true,
}

func (s *SystemControllerHandlers) setSetting(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := SetSettingRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if req.Key == "" {
		return echo.NewHTTPError(400, "key is required")
	}

	value := req.Value

	if byteValueSettings[req.Key] {
		b, err := packages.ParseBytes(value)
		if err != nil {
			return echo.NewHTTPError(400, fmt.Sprintf("invalid byte value for %q: %v", req.Key, err))
		}
		value = strconv.FormatUint(b, 10)
	}

	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return echo.NewHTTPError(500, "settings manager not available")
	}

	if err := mgr.Set(req.Key, value); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

// --- Status handlers ---

func (s *SystemControllerHandlers) ping(c *echo.Context) error {
	resp := PingResponse{Status: "ok"}

	if st := s.Controller.GetStorage(); st != nil {
		fs, err := st.ListFilesystems("")
		if err != nil {
			return err
		}
		count := 0
		for _, f := range fs {
			state, _ := classifyFilesystem(f.Name)
			if state == "user" {
				count++
			}
		}
		resp.Filesystems = count
	}

	if rr := s.Controller.GetRepositoryRoot(); rr != nil {
		repos, err := rr.List()
		if err != nil {
			return err
		}
		resp.Repositories = len(repos)

		pkgs, err := rr.ListPackages()
		if err != nil {
			return err
		}
		resp.Packages = len(pkgs)
	}

	if inst := s.Controller.GetInstaller(); inst != nil {
		installed, err := inst.ListInstalled()
		if err != nil {
			return err
		}
		resp.Installed = len(installed)
	}

	if am := s.Controller.GetAccountManager(); am != nil {
		accounts, err := am.List()
		if err != nil {
			return err
		}
		resp.Accounts = len(accounts)
		var adminUsernames []string
		for _, a := range accounts {
			if !a.Disabled && a.Admin {
				resp.Admins++
				adminUsernames = append(adminUsernames, a.Username)
			}
		}

		if resp.Admins == 0 {
			resp.NeedsSetup = true
		} else if sm := s.Controller.GetSessionManager(); sm != nil {
			hasActive, err := sm.HasActiveAdminSessions(adminUsernames)
			if err != nil {
				return err
			}
			resp.NeedsSetup = !hasActive
		}
	}

	if sd := s.Controller.GetSystemdManager(); sd != nil {
		units, err := sd.ListUnits(c.Request().Context())
		if err != nil {
			return err
		}
		counts := &UnitCounts{}
		for _, u := range units {
			if !strings.HasPrefix(u.Name, "town-os-") {
				continue
			}
			counts.Total++
			switch u.ActiveState {
			case "active":
				counts.Active++
			case "failed":
				counts.Failed++
			}
		}
		resp.Units = counts
	}

	if am := s.Controller.GetAuditManager(); am != nil {
		n, err := am.CountRecentErrors(time.Now().Add(-5 * time.Minute))
		if err != nil {
			return err
		}
		resp.RecentErrors = n
	}

	return c.JSON(200, resp)
}

// --- Routes ---

func (s *SystemControllerHandlers) configureRoutes(e *echo.Echo) {
	// Public
	e.Add("GET", "/status/ping", s.ping)
	e.Add("POST", "/account/authenticate", s.authenticateAccount)

	// Self-authenticated (handlers do own token validation)
	e.Add("GET", "/account/sessions", s.listSessions)
	e.Add("GET", "/account/me", s.sessionUsername)
	e.Add("POST", "/account/session/revoke", s.revokeSession)

	// Authenticated (requireAuth)
	e.Add("POST", "/storage/create", s.createFilesystem, s.requireAuth)
	e.Add("POST", "/storage/modify", s.modifyFilesystem, s.requireAuth)
	e.Add("POST", "/storage/remove", s.removeFilesystem, s.requireAuth)
	e.Add("POST", "/storage", s.listFilesystems, s.requireAuth)

	e.Add("POST", "/repository/add", s.addRepository, s.requireAuth)
	e.Add("POST", "/repository/remove", s.removeRepository, s.requireAuth)
	e.Add("POST", "/repository/move", s.moveRepository, s.requireAuth)
	e.Add("POST", "/repository/refresh", s.refreshRepositories, s.requireAuth)
	e.Add("GET", "/repository", s.listRepositories, s.requireAuth)

	e.Add("GET", "/packages", s.listPackages, s.requireAuth)
	e.Add("POST", "/packages/versions", s.listPackageVersions, s.requireAuth)
	e.Add("GET", "/packages/installed", s.listInstalled, s.requireAuth)
	e.Add("POST", "/packages/installed/info", s.getInstalledInfo, s.requireAuth)
	e.Add("POST", "/packages/responses", s.getResponses, s.requireAuth)

	e.Add("GET", "/systemd/units", s.listUnits, s.requireAuth)
	e.Add("GET", "/systemd/logs", s.logReplay, s.requireAuth)
	e.Add("GET", "/systemd/logs/tail", s.logTail, s.requireAuth)

	e.Add("POST", "/account/create", s.createAccount)
	e.Add("POST", "/account", s.getAccount, s.requireAuth)
	e.Add("POST", "/account/update", s.updateAccount, s.requireAuth)
	e.Add("GET", "/account", s.listAccounts, s.requireAuth)

	// Admin (requireAdmin, which implies auth)
	e.Add("POST", "/packages/questions", s.getPackageQuestions, s.requireAdmin)
	e.Add("POST", "/packages/questions/identity", s.getPackageQuestionsByIdentity, s.requireAdmin)
	e.Add("POST", "/packages/install", s.installPackage, s.requireAdmin)
	e.Add("POST", "/packages/uninstall", s.uninstallPackage, s.requireAdmin)
	e.Add("POST", "/packages/purge-volumes", s.purgeVolumes, s.requireAdmin)
	e.Add("POST", "/packages/uninstalled-volumes", s.listUninstalledVolumes, s.requireAdmin)
	e.Add("POST", "/packages/purge-uninstalled-volumes", s.purgeUninstalledVolumes, s.requireAdmin)
	e.Add("POST", "/packages/disable", s.disablePackage, s.requireAdmin)
	e.Add("POST", "/packages/enable", s.enablePackage, s.requireAdmin)
	e.Add("POST", "/systemd/status", s.setUnitStatus, s.requireAdmin)
	e.Add("POST", "/account/disable", s.disableAccount, s.requireAdmin)
	e.Add("POST", "/account/enable", s.enableAccount, s.requireAdmin)
	e.Add("POST", "/audit/log", s.listAuditLog, s.requireAdmin)
	e.Add("GET", "/settings", s.getSettings, s.requireAdmin)
	e.Add("POST", "/settings/get", s.getSetting, s.requireAdmin)
	e.Add("POST", "/settings/set", s.setSetting, s.requireAdmin)
}

// --- Server infrastructure ---

type ServerConfig struct {
	Storage            storage.Storage
	RepositoryRoot     *packages.RepositoryRoot
	Installer          packages.Installer
	Systemd            systemd.Manager
	AccountMgr         account.Manager
	SessionMgr         account.SessionManager
	AuditMgr           account.AuditManager
	SettingsMgr        account.SettingsManager
	AllowedHosts       []string
	DefaultRepoUser    string
	DefaultRepoPass    string
	BtrfsBasePath      string
	UPnPBinPath        string
	NetworkMode        string
}

type contextHandler struct {
	ctx     context.Context
	handler http.Handler
}

func (h *contextHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	h.handler.ServeHTTP(w, r.WithContext(ctx))
}

type serverBase struct {
	ServerConfig
	cancel context.CancelFunc
}

func (s *serverBase) GetStorage() storage.Storage                 { return s.Storage }
func (s *serverBase) GetRepositoryRoot() *packages.RepositoryRoot { return s.RepositoryRoot }
func (s *serverBase) GetInstaller() packages.Installer            { return s.Installer }
func (s *serverBase) GetSystemdManager() systemd.Manager          { return s.Systemd }
func (s *serverBase) GetAccountManager() account.Manager          { return s.AccountMgr }
func (s *serverBase) GetSessionManager() account.SessionManager   { return s.SessionMgr }
func (s *serverBase) GetAuditManager() account.AuditManager       { return s.AuditMgr }
func (s *serverBase) GetSettingsManager() account.SettingsManager  { return s.SettingsMgr }
func (s *serverBase) GetAllowedHosts() []string                   { return s.AllowedHosts }
func (s *serverBase) GetDefaultRepoCredentials() (string, string) {
	return s.DefaultRepoUser, s.DefaultRepoPass
}
func (s *serverBase) GetBtrfsBasePath() string { return s.BtrfsBasePath }
func (s *serverBase) GetUPnPBinPath() string   { return s.UPnPBinPath }
func (s *serverBase) GetNetworkMode() string   { return s.NetworkMode }

func parseLogLevel() slog.Level {
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelError
	}
}

func configureRouter(sc systemControllerBackend) http.Handler {
	handlers := getHandler(sc)
	e := echo.New()
	e.HTTPErrorHandler = ProblemDetailHTTPErrorHandler()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseLogLevel()}))
	e.Logger = logger
	slog.SetDefault(logger)
	e.Use(middleware.RequestLogger())
	allowedHosts := sc.GetAllowedHosts()
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		UnsafeAllowOriginFunc: func(_ *echo.Context, origin string) (string, bool, error) {
			if os.Getenv("DEBUG") != "" {
				return origin, true, nil
			}
			u, err := url.Parse(origin)
			if err != nil {
				return "", false, nil
			}
			host := u.Hostname()
			for _, h := range allowedHosts {
				if strings.EqualFold(host, h) {
					return origin, true, nil
				}
			}
			return "", false, nil
		},
		AllowMethods:     []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "Accept"},
		ExposeHeaders:    []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
	}))
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if c.Request().Header.Get("Access-Control-Request-Private-Network") == "true" {
				c.Response().Header().Set("Access-Control-Allow-Private-Network", "true")
			}
			return next(c)
		}
	})
	e.Use(handlers.auditMiddleware)
	handlers.configureRoutes(e)
	return e
}

// NewHandler creates an http.Handler for the given ServerConfig.
// The system hostname is automatically added to AllowedHosts.
func NewHandler(cfg ServerConfig) http.Handler {
	cfg.AllowedHosts = append(cfg.AllowedHosts, "localhost")
	if hostname, err := os.Hostname(); err == nil {
		cfg.AllowedHosts = append(cfg.AllowedHosts, hostname)
	}
	sb := &serverBase{ServerConfig: cfg}
	return configureRouter(sb)
}

// --- TestServer ---

type TestServer struct {
	serverBase
	Server *httptest.Server
}

func InitTestServer(cfg ServerConfig) *TestServer {
	ts := &TestServer{}
	ts.ServerConfig = cfg
	ctx, cancel := context.WithCancel(context.Background())
	ts.cancel = cancel
	ts.Server = httptest.NewServer(&contextHandler{ctx: ctx, handler: configureRouter(ts)})
	return ts
}

func (ts *TestServer) Close() {
	ts.cancel()
	ts.Server.Close()
}

func (ts *TestServer) Run() error {
	ts.Server.Start()
	return nil
}

func (ts *TestServer) Client() (*SystemdClient, error) {
	return FromClient(ts.Server.Client(), ts.Server.URL)
}

// --- UnixServer ---

type UnixServer struct {
	serverBase
	Socket string
	server *http.Server
}

func InitUnixServer(sock string, cfg ServerConfig) *UnixServer {
	us := &UnixServer{Socket: sock}
	us.ServerConfig = cfg
	ctx, cancel := context.WithCancel(context.Background())
	us.cancel = cancel
	us.server = &http.Server{Handler: &contextHandler{ctx: ctx, handler: configureRouter(us)}}
	return us
}

func (us *UnixServer) Close() error {
	us.cancel()
	return us.server.Close()
}

func (us *UnixServer) Run() error {
	lis, err := net.Listen("unix", us.Socket)
	if err != nil {
		return fmt.Errorf("could not listen on unix socket %q: %v", us.Socket, err)
	}
	return us.server.Serve(lis)
}

func (us *UnixServer) Client() (*SystemdClient, error) {
	return InitClient(us.Socket)
}

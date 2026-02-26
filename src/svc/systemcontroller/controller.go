package systemcontroller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/networkcontroller"
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
	if name == PackagesVolumePrefix || name == UninstalledVolumePrefix || name == ArchivesSubvolume {
		return true
	}
	if strings.HasPrefix(name, fmt.Sprintf("%s/", PackagesVolumePrefix)) { //nolint:perfsprint // project convention
		return true
	}
	if strings.HasPrefix(name, fmt.Sprintf("%s/", UninstalledVolumePrefix)) { //nolint:perfsprint // project convention
		return true
	}
	if strings.HasPrefix(name, fmt.Sprintf("%s/", ArchivesSubvolume)) { //nolint:perfsprint // project convention
		return true
	}
	return false
}

// classifyFilesystem determines the state of a filesystem based on its name
// prefix. Returns the state ("user", "installed", "uninstalled") and the
// display name with internal prefixes stripped. Root subvolumes (installed,
// uninstalled, empty name) return empty state to signal they should be skipped.
func classifyFilesystem(name string) (state, displayName string) {
	if name == "" || name == PackagesVolumePrefix || name == UninstalledVolumePrefix || name == ArchivesSubvolume {
		return "", name
	}

	archivesPrefix := fmt.Sprintf("%s/", ArchivesSubvolume) //nolint:perfsprint // project convention
	if strings.HasPrefix(name, archivesPrefix) {
		return "", name
	}

	installedPrefix := fmt.Sprintf("%s/", PackagesVolumePrefix)   //nolint:perfsprint // project convention
	uninstalledPrefix := fmt.Sprintf("%s/", UninstalledVolumePrefix) //nolint:perfsprint // project convention

	if after, ok := strings.CutPrefix(name, installedPrefix); ok {
		return "installed", after
	}
	if after, ok := strings.CutPrefix(name, uninstalledPrefix); ok {
		return "uninstalled", after
	}

	return "user", name
}

func packageVolumePath(repo, name, version, volName string) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", PackagesVolumePrefix, repo, name, version, volName)
}

func packagePrefix(repo, name string) string {
	return fmt.Sprintf("%s/%s/%s/", PackagesVolumePrefix, repo, name)
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
	GetNetworkControllerBinPath() string
	GetNetworkStatePath() string
	GetNetworkMode() string
	GetExternalIP() string
	GetInternalIP() string
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
	Repo string `json:"repo"`
	Name string `json:"name"`
}

type PackageIdentityRequest struct {
	Repo    string `json:"repo"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InstallRequest struct {
	Repo              string             `json:"repo"`
	Name              string             `json:"name"`
	Version           string             `json:"version"`
	Responses         packages.Responses `json:"responses"`
	ReuseVolumes      bool               `json:"reuse_volumes"`
	ImportFromVersion string             `json:"import_from_version,omitempty"`
	SkipResponseReuse bool               `json:"skip_response_reuse,omitempty"`
	Instance          string             `json:"instance,omitempty"`
}

type UninstallRequest struct {
	Repo         string `json:"repo"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	PurgeVolumes bool   `json:"purge_volumes"`
	Instance     string `json:"instance,omitempty"`
}

type GetResponsesRequest struct {
	Repo    string `json:"repo"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ListChildrenRequest struct {
	Repo string `json:"repo"`
	Name string `json:"name"`
}

type InstalledInfoResponse struct {
	Questions map[string]packages.Question    `json:"questions"`
	Responses packages.Responses              `json:"responses"`
	Notes     map[string]string               `json:"notes"`
	NoteTypes map[string]packages.NoteType    `json:"note_types,omitempty"`
}

type UninstalledVolumesResponse struct {
	HasUninstalledVolumes bool     `json:"has_uninstalled_volumes"`
	UninstalledVersions   []string `json:"uninstalled_versions,omitempty"`
	InstalledVersions     []string `json:"installed_versions,omitempty"`
}

type InstallPreviewRequest struct {
	Repo    string `json:"repo"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type VolumePreview struct {
	Name       string `json:"name"`
	Mountpoint string `json:"mountpoint"`
	Quota      string `json:"quota,omitempty"`
	Migrated   bool   `json:"migrated"`
	Fresh      bool   `json:"fresh"`
}

type PortPreview struct {
	External uint16 `json:"external"`
	Internal uint16 `json:"internal"`
}

type InstallPreview struct {
	Repo             string             `json:"repo"`
	Name             string             `json:"name"`
	Version          string             `json:"version"`
	Description      string             `json:"description,omitempty"`
	Image            string             `json:"image"`
	Volumes          []VolumePreview    `json:"volumes"`
	ExternalPorts    []PortPreview      `json:"external_ports"`
	InternalPorts    []PortPreview      `json:"internal_ports"`
	UpgradingFrom    string             `json:"upgrading_from,omitempty"`
	HasQuestions     bool               `json:"has_questions"`
	DiskUsage        *storage.DiskUsage `json:"disk_usage,omitempty"`
	TotalQuota       uint64             `json:"total_quota"`
	QuotaExceedsDisk bool               `json:"quota_exceeds_disk"`
	Summary          string             `json:"summary"`
}

type PackageListEntry struct {
	Repo             string   `json:"repo"`
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	Description      string   `json:"description,omitempty"`
	Supplies         []string `json:"supplies,omitempty"`
	Installed        bool     `json:"installed"`
	InstalledVersion string   `json:"installed_version,omitempty"`
	Featured         bool     `json:"featured,omitempty"`
	Changed          bool     `json:"changed,omitempty"`
}

type PackageUpgrade struct {
	Repo             string `json:"repo"`
	Name             string `json:"name"`
	InstalledVersion string `json:"installed_version"`
	LatestVersion    string `json:"latest_version"`
	Changed          bool   `json:"changed"`
}

type ArchiveUploadResponse struct {
	NeedsRestart bool   `json:"needs_restart"`
	Message      string `json:"message"`
}

type DownloadArchiveRequest struct {
	Subvolume   string   `json:"subvolume"`
	Paths       []string `json:"paths,omitempty"`
	StopService string   `json:"stop_service,omitempty"`
}

type UnitListEntry struct {
	systemd.UnitStatus

	PackageIdentifier  string `json:"package_identifier,omitempty"`
	PackageDescription string `json:"package_description,omitempty"`
	NCFailed           bool   `json:"nc_failed,omitempty"`
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

// PingMinimalResponse is returned for unauthenticated ping requests.
type PingMinimalResponse struct {
	Status     string `json:"status"`
	NeedsSetup bool   `json:"needs_setup,omitempty"`
}

type PingResponse struct {
	Status             string             `json:"status"`
	Filesystems        int                `json:"filesystems"`
	Repositories       int                `json:"repositories"`
	Packages           int                `json:"packages"`
	Installed          int                `json:"installed"`
	Accounts           int                `json:"accounts"`
	Admins             int                `json:"admins"`
	Units              *UnitCounts        `json:"units,omitempty"`
	RecentErrors       int                `json:"recent_errors"`
	NeedsSetup         bool               `json:"needs_setup,omitempty"`
	ExternalIP         string             `json:"external_ip,omitempty"`
	InternalIP         string             `json:"internal_ip,omitempty"`
	Username           string             `json:"username,omitempty"`
	InstalledVolumes   int                `json:"installed_volumes"`
	UninstalledVolumes int                `json:"uninstalled_volumes"`
	DiskUsage          *storage.DiskUsage `json:"disk_usage,omitempty"`
	UpgradesAvailable  int                `json:"upgrades_available"`
	UpgradesDismissed  bool               `json:"upgrades_dismissed,omitempty"`
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
// socket and network controller units, and starts the main service.
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
	if units.NetworkController != nil {
		if err := sd.InstallUnit(ctx, units.NetworkController.Name, units.NetworkController.Content); err != nil {
			return fmt.Errorf("install network controller unit: %w", err)
		}
	}

	// Enable socket and network controller units.
	for _, sock := range units.Sockets {
		if err := sd.SetStatus(ctx, sock.Name, systemd.Enable); err != nil {
			return fmt.Errorf("enable socket %s: %w", sock.Name, err)
		}
	}
	if units.NetworkController != nil {
		if err := sd.SetStatus(ctx, units.NetworkController.Name, systemd.Enable); err != nil {
			return fmt.Errorf("enable network controller: %w", err)
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
func (s *SystemControllerHandlers) uninstallPackageUnits(ctx context.Context, sd systemd.Manager, repoName, pkgName, version string) error {
	unitNames, err := sd.ListPackageUnitFiles(ctx, repoName, pkgName, version)
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
func (s *SystemControllerHandlers) packageUnitConfig(repoName, pkgName, version, description string, compiled *packages.Package) systemd.PackageUnitConfig {
	return systemd.PackageUnitConfig{
		RepoName:                 repoName,
		PkgName:                  pkgName,
		Version:                  version,
		Description:              description,
		Image:                    compiled.Image,
		Command:                  compiled.Command,
		Environment:              compiled.Environment,
		External:                 compiled.Network.External,
		Internal:                 compiled.Network.Internal,
		Volumes:                  compiled.Volumes,
		BtrfsBase:                s.Controller.GetBtrfsBasePath(),
		NetworkControllerBinPath: s.Controller.GetNetworkControllerBinPath(),
		NetworkStatePath:         s.Controller.GetNetworkStatePath(),
		NetworkMode:              s.Controller.GetNetworkMode(),
	}
}

// writePackageNetworkState writes the per-package JSON state file consumed by
// the networkcontroller daemon.
func (s *SystemControllerHandlers) writePackageNetworkState(repoName, pkgName, version string, compiled *packages.Package) error {
	statePath := s.Controller.GetNetworkStatePath()
	if statePath == "" {
		return nil
	}

	state := networkcontroller.PackageNetworkState{
		Repo:        repoName,
		Package:     pkgName,
		Version:     version,
		NetworkMode: s.Controller.GetNetworkMode(),
	}

	for ext, int_ := range compiled.Network.External {
		forward := s.Controller.GetNetworkMode() == "host" && ext != int_
		state.Ports = append(state.Ports, networkcontroller.PortConfig{
			ExternalPort: ext,
			InternalPort: int_,
			UPnP:         true,
			Forward:      forward,
		})
	}

	for intHost, intContainer := range compiled.Network.Internal {
		if s.Controller.GetNetworkMode() == "host" && intHost != intContainer {
			state.Ports = append(state.Ports, networkcontroller.PortConfig{
				ExternalPort: intHost,
				InternalPort: intContainer,
				UPnP:         false,
				Forward:      true,
			})
		}
	}

	if len(state.Ports) == 0 {
		return nil
	}

	// Sort for deterministic output.
	sort.Slice(state.Ports, func(i, j int) bool {
		return state.Ports[i].ExternalPort < state.Ports[j].ExternalPort
	})

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal network state: %w", err)
	}

	filePath := fmt.Sprintf("%s/%s-%s-%s.json", statePath, repoName, pkgName, version)
	if err := os.MkdirAll(statePath, 0755); err != nil { //nolint:gosec // state directory
		return fmt.Errorf("create network state dir: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil { //nolint:gosec // state file
		return fmt.Errorf("write network state: %w", err)
	}

	return nil
}

// removePackageNetworkState removes the per-package network state file.
func (s *SystemControllerHandlers) removePackageNetworkState(repoName, pkgName, version string) {
	statePath := s.Controller.GetNetworkStatePath()
	if statePath == "" {
		return
	}

	filePath := fmt.Sprintf("%s/%s-%s-%s.json", statePath, repoName, pkgName, version)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		slog.Debug(fmt.Sprintf("remove network state %s: %v", filePath, err))
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
		return fmt.Errorf("invalid url: %w", err)
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

	rr.ForceRefresh()

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

	rr.ForceRefresh()

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
	rr.ForceRefresh()
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

	pkgStrings, err := rr.ListPackages()
	if err != nil {
		return err
	}

	// Build a map of installed repo/name keys to their installed version.
	installedVersions := map[string]string{}
	inst := s.Controller.GetInstaller()
	if inst != nil {
		installed, err := inst.ListInstalled()
		if err != nil {
			return err
		}

		for _, pkg := range installed {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			key := fmt.Sprintf("%s/%s", pi.Repo, pi.Name)
			installedVersions[key] = pi.Version
		}

		// Merge installed packages that may not be the latest version.
		known := map[string]bool{}
		for _, pkg := range pkgStrings {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			key := fmt.Sprintf("%s/%s", pi.Repo, pi.Name)
			known[key] = true
		}

		for _, pkg := range installed {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			key := fmt.Sprintf("%s/%s", pi.Repo, pi.Name)
			if !known[key] {
				pkgStrings = append(pkgStrings, pkg)
				known[key] = true
			}
		}
	}

	// Build a set of featured packages per repo.
	featuredSet := map[string]bool{}
	groups, grpErr := rr.ListPackagesByRepo()
	if grpErr == nil {
		for _, g := range groups {
			for _, f := range g.Featured {
				featuredSet[fmt.Sprintf("%s/%s", g.Repo, f)] = true
			}
		}
	}

	// Build structured entries with description/supplies from manifest.
	entries := make([]PackageListEntry, 0, len(pkgStrings))
	for _, pkg := range pkgStrings {
		pi, err := packages.ParsePackageIdentity(pkg)
		if err != nil {
			continue
		}

		key := fmt.Sprintf("%s/%s", pi.Repo, pi.Name)
		instVer, isInstalled := installedVersions[key]
		entry := PackageListEntry{
			Repo:             pi.Repo,
			Name:             pi.Name,
			Version:          pi.Version,
			Installed:        isInstalled,
			InstalledVersion: instVer,
			Featured:         featuredSet[key],
		}

		// Try to load manifest for description/supplies.
		ip, loadErr := rr.LoadPackage(pi.Repo, pi.Name, pi.Version)
		if loadErr == nil {
			entry.Description = ip.Description
			entry.Supplies = ip.Supplies
		}

		// Check if installed package file has changed.
		if isInstalled && inst != nil {
			changed, err := inst.IsPackageChanged(pi.Repo, pi.Name, instVer)
			if err == nil && changed {
				entry.Changed = true
			}
		}

		entries = append(entries, entry)
	}

	p := readListParams(c)
	entries = filterSearch(entries, p.Search)
	sortSlice(entries, p.SortBy, p.SortOrder)

	return c.JSON(200, paginate(entries, p.Limit, p.Offset))
}

func (s *SystemControllerHandlers) listPackagesByRepo(c *echo.Context) error {
	rr := s.Controller.GetRepositoryRoot()

	groups, err := rr.ListPackagesByRepo()
	if err != nil {
		return err
	}

	p := readListParams(c)
	if p.Search != "" {
		searchLower := strings.ToLower(p.Search)
		var filtered []packages.RepoPackageGroup
		for _, g := range groups {
			var matching []packages.PackageIdentity
			for _, pkg := range g.Packages {
				if strings.Contains(strings.ToLower(pkg.Name), searchLower) {
					matching = append(matching, pkg)
				}
			}
			if len(matching) > 0 {
				filtered = append(filtered, packages.RepoPackageGroup{Repo: g.Repo, Packages: matching})
			}
		}
		groups = filtered
	}

	return c.JSON(200, groups)
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

func (s *SystemControllerHandlers) listTimezones(c *echo.Context) error {
	return c.JSON(200, packages.ListTimezones())
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

	repoName := req.Repo
	if repoName == "" {
		var err error
		repoName, err = rr.FindRepoForPackage(req.Name, req.Version)
		if err != nil {
			return err
		}
	}

	ip, err := rr.LoadPackage(repoName, req.Name, req.Version)
	if err != nil {
		return err
	}

	return c.JSON(200, ip.Questions)
}

// --- Install handlers ---

func (s *SystemControllerHandlers) installPreview(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := InstallPreviewRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()
	if rr == nil {
		return errors.New("no repository root configured")
	}

	repoName, err := rr.FindRepoForPackage(req.Name, req.Version)
	if err != nil {
		return err
	}
	if req.Repo != "" && req.Repo != repoName {
		repoName = req.Repo
	}

	ip, err := rr.LoadPackage(repoName, req.Name, req.Version)
	if err != nil {
		return err
	}

	// Find currently installed version.
	inst := s.Controller.GetInstaller()
	var activeVersion string
	if inst != nil {
		installed, err := inst.ListInstalled()
		if err != nil {
			return err
		}
		for _, pkg := range installed {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			if pi.Repo == repoName && pi.Name == req.Name {
				activeVersion = pi.Version
				break
			}
		}
	}

	// Load old package volumes for migration detection.
	var oldVolumes map[string]packages.InputPackageVolume
	if activeVersion != "" && activeVersion != req.Version {
		oldIP, err := rr.LoadPackage(repoName, req.Name, activeVersion)
		if err == nil {
			oldVolumes = oldIP.Volumes
		}
	}

	// Build volume previews.
	var volumes []VolumePreview
	var totalQuota uint64
	for volName, vol := range ip.Volumes {
		migrated := false
		fresh := true
		if oldVolumes != nil {
			if _, exists := oldVolumes[volName]; exists {
				migrated = true
				fresh = false
			}
		}
		volumes = append(volumes, VolumePreview{
			Name:       volName,
			Mountpoint: vol.Mountpoint,
			Quota:      vol.Quota,
			Migrated:   migrated,
			Fresh:      fresh,
		})
		if vol.Quota != "" {
			q, err := packages.ParseBytes(vol.Quota)
			if err == nil {
				totalQuota += q
			}
		}
	}

	// Sort volumes by name for deterministic output.
	sort.Slice(volumes, func(i, j int) bool {
		return volumes[i].Name < volumes[j].Name
	})

	// Build port previews.
	var externalPorts []PortPreview
	for ext, intStr := range ip.Network.External {
		extPort, err := strconv.ParseUint(ext, 10, 16)
		if err != nil {
			continue
		}
		intPort, err := strconv.ParseUint(intStr, 10, 16)
		if err != nil {
			continue
		}
		externalPorts = append(externalPorts, PortPreview{
			External: uint16(extPort),
			Internal: uint16(intPort),
		})
	}
	sort.Slice(externalPorts, func(i, j int) bool {
		return externalPorts[i].External < externalPorts[j].External
	})

	var internalPorts []PortPreview
	for ext, intStr := range ip.Network.Internal {
		extPort, err := strconv.ParseUint(ext, 10, 16)
		if err != nil {
			continue
		}
		intPort, err := strconv.ParseUint(intStr, 10, 16)
		if err != nil {
			continue
		}
		internalPorts = append(internalPorts, PortPreview{
			External: uint16(extPort),
			Internal: uint16(intPort),
		})
	}
	sort.Slice(internalPorts, func(i, j int) bool {
		return internalPorts[i].External < internalPorts[j].External
	})

	preview := InstallPreview{
		Repo:          repoName,
		Name:          req.Name,
		Version:       req.Version,
		Description:   ip.Description,
		Image:         ip.Image,
		Volumes:       volumes,
		ExternalPorts: externalPorts,
		InternalPorts: internalPorts,
		HasQuestions:  len(ip.Questions) > 0,
		TotalQuota:    totalQuota,
	}

	if activeVersion != "" && activeVersion != req.Version {
		preview.UpgradingFrom = activeVersion
	}

	// Disk usage and quota warning.
	if st := s.Controller.GetStorage(); st != nil {
		du, err := st.DiskUsage()
		if err == nil {
			preview.DiskUsage = &du
			reserved := du.Total * 5 / 100
			var effectiveAvailable uint64
			if du.Available > reserved {
				effectiveAvailable = du.Available - reserved
			}
			if totalQuota > 0 && totalQuota > effectiveAvailable {
				preview.QuotaExceedsDisk = true
			}
		}
	}

	if preview.Volumes == nil {
		preview.Volumes = []VolumePreview{}
	}
	if preview.ExternalPorts == nil {
		preview.ExternalPorts = []PortPreview{}
	}
	if preview.InternalPorts == nil {
		preview.InternalPorts = []PortPreview{}
	}

	preview.Summary = buildInstallSummary(&preview)

	return c.JSON(200, preview)
}

// buildInstallSummary generates a human-readable summary of the install operation.
func buildInstallSummary(p *InstallPreview) string {
	var parts []string

	if p.UpgradingFrom != "" {
		parts = append(parts, fmt.Sprintf("Upgrade %s from %s to %s", p.Name, p.UpgradingFrom, p.Version))
	} else {
		parts = append(parts, fmt.Sprintf("Install %s %s", p.Name, p.Version))
	}

	parts = append(parts, fmt.Sprintf("Image: %s", p.Image)) //nolint:perfsprint // project convention

	if len(p.Volumes) > 0 {
		fresh := 0
		migrated := 0
		for _, v := range p.Volumes {
			if v.Migrated {
				migrated++
			}
			if v.Fresh {
				fresh++
			}
		}
		volParts := []string{fmt.Sprintf("%d volume(s)", len(p.Volumes))}
		if fresh > 0 {
			volParts = append(volParts, fmt.Sprintf("%d new", fresh))
		}
		if migrated > 0 {
			volParts = append(volParts, fmt.Sprintf("%d migrated", migrated))
		}
		parts = append(parts, strings.Join(volParts, ", "))
	} else {
		parts = append(parts, "No volumes")
	}

	if len(p.ExternalPorts) > 0 {
		portStrs := make([]string, len(p.ExternalPorts))
		for i, port := range p.ExternalPorts {
			portStrs[i] = fmt.Sprintf("%d->%d", port.External, port.Internal)
		}
		parts = append(parts, fmt.Sprintf("External ports: %s", strings.Join(portStrs, ", "))) //nolint:perfsprint // project convention
	}

	if p.HasQuestions {
		parts = append(parts, "Configuration required")
	}

	return strings.Join(parts, ". ") + "."
}

func (s *SystemControllerHandlers) installPackage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := InstallRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()
	repoName := req.Repo
	if repoName == "" {
		var err error
		repoName, err = rr.FindRepoForPackage(req.Name, req.Version)
		if err != nil {
			return err
		}
	}

	// When Instance is set, the effective name becomes parentName-instance.
	parentName := req.Name
	effectiveName := req.Name
	if req.Instance != "" {
		effectiveName = fmt.Sprintf("%s-%s", parentName, req.Instance)
	}

	// Load and compile the package to resolve volume definitions.
	// Always load from the parent package name.
	ip, err := rr.LoadPackage(repoName, parentName, req.Version)
	if err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	ctx := c.Request().Context()

	// Check for any installed version of the same repo/package.
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
		if pi.Repo == repoName && pi.Name == effectiveName {
			activeVersion = pi.Version
			break
		}
	}

	// Carry forward responses from the active version during upgrades.
	if activeVersion != "" && activeVersion != req.Version {
		oldResponses, err := inst.GetResponses(repoName, effectiveName, activeVersion)
		if err == nil {
			for key, val := range oldResponses {
				// Only carry forward if the question exists in the new version
				// and the user didn't provide a new response.
				if _, exists := ip.Questions[key]; exists {
					if _, provided := req.Responses[key]; !provided {
						if req.Responses == nil {
							req.Responses = packages.Responses{}
						}
						req.Responses[key] = val
					}
				}
			}
		}
	}

	// Load last responses when reusing volumes from a previous uninstall.
	if req.ReuseVolumes && !req.SkipResponseReuse {
		lastResp, err := inst.LoadLastResponses(repoName, effectiveName)
		if err == nil && len(lastResp) > 0 {
			for key, val := range lastResp {
				if _, exists := ip.Questions[key]; exists {
					if _, provided := req.Responses[key]; !provided {
						if req.Responses == nil {
							req.Responses = packages.Responses{}
						}
						req.Responses[key] = val
					}
				}
			}
		}
	}

	// Auto-generate port/hostname values for empty or "auto" responses.
	{
		excludedPorts := map[uint16]bool{}
		if inst != nil {
			allInstalled, _ := inst.ListInstalled()
			for _, pkg := range allInstalled {
				pi, err := packages.ParsePackageIdentity(pkg)
				if err != nil {
					continue
				}
				resp, err := inst.GetResponses(pi.Repo, pi.Name, pi.Version)
				if err != nil {
					continue
				}
				for _, v := range resp {
					if p, err := strconv.ParseUint(v, 10, 16); err == nil && p > 0 {
						excludedPorts[uint16(p)] = true
					}
				}
			}
		}

		for name, q := range ip.Questions {
			resp := req.Responses[name]
			if resp != "" && resp != "auto" {
				continue
			}

			switch q.Type {
			case packages.Port:
				port, err := packages.FindAvailablePort(excludedPorts)
				if err != nil {
					continue
				}
				if req.Responses == nil {
					req.Responses = packages.Responses{}
				}
				req.Responses[name] = strconv.FormatUint(uint64(port), 10)
				excludedPorts[port] = true
			case packages.Hostname:
				if req.Responses == nil {
					req.Responses = packages.Responses{}
				}
				req.Responses[name] = packages.GenerateHostname(effectiveName)
			default:
				if resp == "auto" || resp == "" {
					if q.Default != "" {
						if req.Responses == nil {
							req.Responses = packages.Responses{}
						}
						req.Responses[name] = q.Default
					}
				}
			}
		}
	}

	compiled, err := ip.CompileWithContext(req.Responses, packages.CompileContext{
		ExternalHost: s.Controller.GetExternalIP(),
		InternalHost: s.Controller.GetInternalIP(),
	})
	if err != nil {
		return err
	}

	if activeVersion != "" {
		// Stop and remove all systemd units for the currently active version.
		if sd := s.Controller.GetSystemdManager(); sd != nil {
			if err := s.uninstallPackageUnits(ctx, sd, repoName, effectiveName, activeVersion); err != nil {
				return err
			}
		}

		if activeVersion == req.Version {
			// Same version reinstall: remove the install record (but not volumes).
			if err := inst.Uninstall(repoName, effectiveName, req.Version); err != nil {
				return err
			}
		} else {
			// Different version (upgrade): move matching volumes from old to new
			// version path and remove the old install record after the new one
			// is created successfully.
			if st := s.Controller.GetStorage(); st != nil && req.ImportFromVersion == "" {
				// Load old package to discover its volume names.
				oldIP, loadErr := rr.LoadPackage(repoName, parentName, activeVersion)
				if loadErr == nil {
					for volName := range compiled.Volumes {
						if _, exists := oldIP.Volumes[volName]; exists {
							src := packageVolumePath(repoName, effectiveName, activeVersion, volName)
							dst := packageVolumePath(repoName, effectiveName, req.Version, volName)
							if err := st.RenameFilesystem(src, dst); err != nil {
								slog.Debug(fmt.Sprintf("upgrade move volume %s -> %s: %v", src, dst, err))
							}
						}
					}
				} else {
					slog.Debug(fmt.Sprintf("upgrade: load old package %s/%s@%s: %v", repoName, parentName, activeVersion, loadErr))
				}
			}
		}
	}

	// Handle volume reuse/import and create or adjust storage volumes.
	if st := s.Controller.GetStorage(); st != nil {
		// If reusing volumes, rename uninstalled/<repo>/<name> → installed/<repo>/<name>.
		if req.ReuseVolumes {
			src := fmt.Sprintf("%s/%s/%s", UninstalledVolumePrefix, repoName, effectiveName)
			dst := fmt.Sprintf("%s/%s/%s", PackagesVolumePrefix, repoName, effectiveName)
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
			fsName := packageVolumePath(repoName, effectiveName, req.Version, volName)

			if req.ImportFromVersion != "" {
				// Import from another version: snapshot from the source version's volume.
				srcVol := packageVolumePath(repoName, effectiveName, req.ImportFromVersion, volName)
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
					// Volume already exists (e.g. moved from old version) — adjust quota if needed.
					if err := st.ModifyFilesystem(fsName, storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
						return err
					}
				}
			}
		}
	}

	// Process auto-archives from package definition.
	if len(ip.Archives) > 0 {
		for _, archive := range ip.Archives {
			volPath := packageVolumePath(repoName, req.Name, req.Version, archive.Volume)
			if err := s.extractFromContainerImage(ctx, archive.Image, archive.Directory, volPath); err != nil {
				slog.Debug(fmt.Sprintf("auto-archive %s -> %s: %v", archive.Image, archive.Volume, err))
			}
		}
	}

	if err := inst.Install(repoName, effectiveName, req.Version, req.Responses); err != nil {
		return err
	}

	// Clear last responses after successful install.
	if err := inst.ClearLastResponses(repoName, effectiveName); err != nil {
		slog.Debug(fmt.Sprintf("clear last responses %s/%s: %v", repoName, effectiveName, err))
	}

	// Clean up old install record after successful new install.
	if activeVersion != "" && activeVersion != req.Version {
		if err := inst.Uninstall(repoName, effectiveName, activeVersion); err != nil {
			slog.Debug(fmt.Sprintf("remove old install record %s/%s@%s: %v", repoName, effectiveName, activeVersion, err))
		}
	}

	// Track child in parent's children list when installing an instance.
	if req.Instance != "" {
		children, _ := inst.LoadChildren(repoName, parentName)
		if !slices.Contains(children, req.Instance) {
			children = append(children, req.Instance)
			if err := inst.SaveChildren(repoName, parentName, children); err != nil {
				slog.Debug(fmt.Sprintf("save children %s/%s: %v", repoName, parentName, err))
			}
		}
	}

	if sd := s.Controller.GetSystemdManager(); sd != nil {
		cfg := s.packageUnitConfig(repoName, effectiveName, req.Version, ip.Description, compiled)
		units := systemd.GeneratePackageUnits(cfg)
		if err := s.writePackageNetworkState(repoName, req.Name, req.Version, compiled); err != nil {
			return err
		}
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

	// When Instance is set, the effective name becomes parentName-instance.
	parentName := req.Name
	effectiveName := req.Name
	if req.Instance != "" {
		effectiveName = fmt.Sprintf("%s-%s", parentName, req.Instance)
	}

	ctx := c.Request().Context()
	inst := s.Controller.GetInstaller()

	if sd := s.Controller.GetSystemdManager(); sd != nil {
		if err := s.uninstallPackageUnits(ctx, sd, req.Repo, effectiveName, req.Version); err != nil {
			return err
		}
	}

	s.removePackageNetworkState(req.Repo, effectiveName, req.Version)

	// Save last responses before uninstall for potential reuse.
	lastResp, err := inst.GetResponses(req.Repo, effectiveName, req.Version)
	if err == nil && len(lastResp) > 0 {
		if err := inst.SaveLastResponses(req.Repo, effectiveName, lastResp); err != nil {
			slog.Debug(fmt.Sprintf("save last responses %s/%s: %v", req.Repo, effectiveName, err))
		}
	}

	if err := inst.SetDisabled(req.Repo, effectiveName, false); err != nil {
		return err
	}
	if err := inst.Uninstall(req.Repo, effectiveName, req.Version); err != nil {
		return err
	}

	// Remove child from parent's children list when uninstalling an instance.
	if req.Instance != "" {
		children, _ := inst.LoadChildren(req.Repo, parentName)
		for i, ch := range children {
			if ch == req.Instance {
				children = append(children[:i], children[i+1:]...)
				break
			}
		}
		if err := inst.SaveChildren(req.Repo, parentName, children); err != nil {
			slog.Debug(fmt.Sprintf("save children %s/%s: %v", req.Repo, parentName, err))
		}
	}

	// Volume handling after uninstall.
	if req.PurgeVolumes {
		if err := s.purgePackageVolumes(req.Repo, effectiveName); err != nil {
			return err
		}
	} else if st := s.Controller.GetStorage(); st != nil {
		// Check if any other versions remain installed for this repo/name.
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
			if pi.Repo == req.Repo && pi.Name == effectiveName {
				otherVersionInstalled = true
				break
			}
		}

		if !otherVersionInstalled {
			// Move installed/<repo>/<name> → uninstalled/<repo>/<name>.
			src := fmt.Sprintf("%s/%s/%s", PackagesVolumePrefix, req.Repo, effectiveName)
			dst := fmt.Sprintf("%s/%s/%s", UninstalledVolumePrefix, req.Repo, effectiveName)
			if err := st.RenameFilesystem(src, dst); err != nil {
				slog.Debug(fmt.Sprintf("preserve volumes: rename %s -> %s: %v", src, dst, err))
			}
		}
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listChildren(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := ListChildrenRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	children, err := inst.LoadChildren(req.Repo, req.Name)
	if err != nil {
		return err
	}
	if children == nil {
		children = []string{}
	}

	return c.JSON(200, children)
}

func (s *SystemControllerHandlers) purgePackageVolumes(repo, name string) error {
	st := s.Controller.GetStorage()
	if st == nil {
		return nil
	}

	// Purge from installed/<repo>/<name>/.
	if err := s.purgeVolumePrefix(st, packagePrefix(repo, name)); err != nil {
		return err
	}

	// Remove the installed/<repo>/<name> parent subvolume itself.
	parentPath := fmt.Sprintf("%s/%s/%s", PackagesVolumePrefix, repo, name)
	if err := st.RemoveFilesystem(parentPath); err != nil {
		slog.Debug(fmt.Sprintf("purge parent volume %s: %v", parentPath, err))
	}

	// Also purge from uninstalled/<repo>/<name>/.
	uninstPrefix := fmt.Sprintf("%s/%s/%s/", UninstalledVolumePrefix, repo, name)
	if err := s.purgeVolumePrefix(st, uninstPrefix); err != nil {
		return err
	}

	uninstParent := fmt.Sprintf("%s/%s/%s", UninstalledVolumePrefix, repo, name)
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

	if err := s.purgePackageVolumes(req.Repo, req.Name); err != nil {
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
		// Check uninstalled/<repo>/<name>/ for existing volume trees.
		uninstPrefix := fmt.Sprintf("%s/%s/%s/", UninstalledVolumePrefix, req.Repo, req.Name)
		filesystems, err := st.ListFilesystems(uninstPrefix)
		if err != nil {
			return err
		}

		if len(filesystems) > 0 {
			resp.HasUninstalledVolumes = true
			// Extract unique versions from uninstalled/<repo>/<name>/<version>/...
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

		// Extract installed versions from installed/<repo>/<name>/<version>/...
		instPrefix := packagePrefix(req.Repo, req.Name)
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

	uninstPrefix := fmt.Sprintf("%s/%s/%s/", UninstalledVolumePrefix, req.Repo, req.Name)
	if err := s.purgeVolumePrefix(st, uninstPrefix); err != nil {
		return err
	}

	uninstParent := fmt.Sprintf("%s/%s/%s", UninstalledVolumePrefix, req.Repo, req.Name)
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
	resp, err := inst.GetResponses(req.Repo, req.Name, req.Version)
	if err != nil {
		if errors.Is(err, packages.ErrNotInstalled) {
			return c.JSON(200, packages.Responses{})
		}
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
	responses, err := inst.GetResponses(req.Repo, req.Name, req.Version)
	if err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()

	ip, err := rr.LoadPackage(req.Repo, req.Name, req.Version)
	if err != nil {
		return err
	}

	notes, err := ip.CompileNotes(responses)
	if err != nil {
		return err
	}

	var noteTypes map[string]packages.NoteType
	for k, note := range ip.Notes {
		if note.Type != "" {
			if noteTypes == nil {
				noteTypes = make(map[string]packages.NoteType)
			}
			noteTypes[k] = note.Type
		}
	}

	return c.JSON(200, InstalledInfoResponse{
		Questions: ip.Questions,
		Responses: responses,
		Notes:     notes,
		NoteTypes: noteTypes,
	})
}

// --- Systemd handlers ---

func (s *SystemControllerHandlers) listUnits(c *echo.Context) error {
	units, err := s.Controller.GetSystemdManager().ListUnits(c.Request().Context())
	if err != nil {
		return err
	}

	// Build lookup maps: filter main service units and index NC units.
	filtered := make([]systemd.UnitStatus, 0, len(units))
	ncUnitMap := map[string]systemd.UnitStatus{}
	for _, u := range units {
		if systemd.IsPackageServiceUnit(u.Name) {
			filtered = append(filtered, u)
		}
		if strings.HasSuffix(u.Name, "-network.service") && strings.HasPrefix(u.Name, systemd.PackageUnitPrefix) {
			ncUnitMap[u.Name] = u
		}
	}

	// Build unit name → package identity/description map.
	identityMap := map[string]string{}
	descriptionMap := map[string]string{}
	if inst := s.Controller.GetInstaller(); inst != nil {
		installed, listErr := inst.ListInstalled()
		if listErr == nil {
			rr := s.Controller.GetRepositoryRoot()
			for _, pkg := range installed {
				pi, parseErr := packages.ParsePackageIdentity(pkg)
				if parseErr != nil {
					continue
				}
				unitName := systemd.UnitName(pi.Repo, pi.Name, pi.Version)
				identityMap[unitName] = fmt.Sprintf("%s/%s@%s", pi.Repo, pi.Name, pi.Version)
				if rr != nil {
					ip, loadErr := rr.LoadPackage(pi.Repo, pi.Name, pi.Version)
					if loadErr == nil {
						descriptionMap[unitName] = ip.Description
					}
				}
			}
		}
	}

	// Enrich with package identity, description, and NC failure status.
	entries := make([]UnitListEntry, len(filtered))
	for i, u := range filtered {
		entry := UnitListEntry{
			UnitStatus:         u,
			PackageIdentifier:  identityMap[u.Name],
			PackageDescription: descriptionMap[u.Name],
		}

		// Check if the corresponding network controller unit has failed.
		ncName := fmt.Sprintf("%s-network.service", strings.TrimSuffix(u.Name, ".service")) //nolint:perfsprint // project convention
		if ncUnit, ok := ncUnitMap[ncName]; ok && ncUnit.ActiveState == "failed" {
			entry.NCFailed = true
			if entry.ActiveState != "failed" {
				entry.ActiveState = "failed"
			}
		}

		entries[i] = entry
	}

	p := readListParams(c)
	entries = filterSearch(entries, p.Search)
	sortSlice(entries, p.SortBy, p.SortOrder)

	return c.JSON(200, paginate(entries, p.Limit, p.Offset))
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
	Repo string `json:"repo"`
	Name string `json:"name"`
}

func (s *SystemControllerHandlers) disablePackage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageToggleRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	if err := inst.SetDisabled(req.Repo, req.Name, true); err != nil {
		return err
	}

	// Find the installed version(s) and stop the service.
	if sd := s.Controller.GetSystemdManager(); sd != nil {
		installed, _ := inst.ListInstalled()
		for _, pkg := range installed {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			if pi.Repo == req.Repo && pi.Name == req.Name {
				unitName := systemd.UnitName(req.Repo, req.Name, pi.Version)
				if err := sd.SetStatus(c.Request().Context(), unitName, systemd.Stop); err != nil {
					return err
				}
			}
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
	if err := inst.SetDisabled(req.Repo, req.Name, false); err != nil {
		return err
	}

	// Find the installed version(s) and start the service.
	if sd := s.Controller.GetSystemdManager(); sd != nil {
		installed, _ := inst.ListInstalled()
		for _, pkg := range installed {
			pi, err := packages.ParsePackageIdentity(pkg)
			if err != nil {
				continue
			}
			if pi.Repo == req.Repo && pi.Name == req.Name {
				unitName := systemd.UnitName(req.Repo, req.Name, pi.Version)
				if err := sd.SetStatus(c.Request().Context(), unitName, systemd.Start); err != nil {
					return err
				}
			}
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

	if v := c.QueryParam("priority"); v != "" {
		pri, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid priority parameter: %w", err)
		}
		params.Priority = pri
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
			"/packages/by-repo":              true,
			"/packages/installed":            true,
			"/packages/responses":            true,
			"/packages/versions":             true,
			"/packages/questions":            true,
			"/packages/questions/identity":   true,
			"/packages/timezones":            true,
			"/packages/children":             true,
			"/packages/uninstalled-volumes":  true,
			"/packages/upgrades":             true,
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
	"default_quota":    true,
	"max_archive_size": true,
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

	// Always compute NeedsSetup — it must be visible before any user exists.
	if am := s.Controller.GetAccountManager(); am != nil {
		accounts, err := am.List()
		if err != nil {
			return err
		}
		var adminUsernames []string
		for _, a := range accounts {
			if !a.Disabled && a.Admin {
				adminUsernames = append(adminUsernames, a.Username)
			}
		}
		if len(adminUsernames) == 0 {
			resp.NeedsSetup = true
		} else if sm := s.Controller.GetSessionManager(); sm != nil {
			hasActive, err := sm.HasActiveAdminSessions(adminUsernames)
			if err != nil {
				return err
			}
			resp.NeedsSetup = !hasActive
		}
	}

	// Check for optional auth — if a session manager is configured and no
	// valid token is provided, return minimal response (status + needs_setup).
	sm := s.Controller.GetSessionManager()
	if sm != nil {
		minimal := PingMinimalResponse{Status: resp.Status, NeedsSetup: resp.NeedsSetup}
		token := extractBearerToken(c.Request())
		if token == "" {
			return c.JSON(200, minimal)
		}
		sess, _, err := sm.Validate(token)
		if err != nil {
			return c.JSON(200, minimal)
		}
		resp.Username = sess.Username
	}

	if st := s.Controller.GetStorage(); st != nil {
		fs, err := st.ListFilesystems("")
		if err != nil {
			return err
		}
		userCount := 0
		installedVols := 0
		uninstalledVols := 0
		for _, f := range fs {
			state, _ := classifyFilesystem(f.Name)
			switch state {
			case "user":
				userCount++
			case "installed":
				installedVols++
			case "uninstalled":
				uninstalledVols++
			}
		}
		resp.Filesystems = userCount
		resp.InstalledVolumes = installedVols
		resp.UninstalledVolumes = uninstalledVols

		du, err := st.DiskUsage()
		if err == nil {
			resp.DiskUsage = &du
		}
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
		for _, a := range accounts {
			if !a.Disabled && a.Admin {
				resp.Admins++
			}
		}
	}

	if sd := s.Controller.GetSystemdManager(); sd != nil {
		units, err := sd.ListUnits(c.Request().Context())
		if err != nil {
			return err
		}
		counts := &UnitCounts{}
		for _, u := range units {
			if !systemd.IsPackageServiceUnit(u.Name) {
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

	resp.ExternalIP = s.Controller.GetExternalIP()
	resp.InternalIP = s.Controller.GetInternalIP()

	// Compute upgrade info.
	upgrades := s.computeUpgrades()
	resp.UpgradesAvailable = len(upgrades)
	if len(upgrades) > 0 {
		if mgr := s.Controller.GetSettingsManager(); mgr != nil {
			dismissed, err := mgr.Get("dismissed_upgrades_hash")
			if err == nil && dismissed == upgradesHash(upgrades) {
				resp.UpgradesDismissed = true
			}
		}
	}

	return c.JSON(200, resp)
}

// --- Upgrades handlers ---

func (s *SystemControllerHandlers) computeUpgrades() []PackageUpgrade {
	inst := s.Controller.GetInstaller()
	rr := s.Controller.GetRepositoryRoot()
	if inst == nil || rr == nil {
		return nil
	}

	installed, err := inst.ListInstalled()
	if err != nil {
		return nil
	}

	var upgrades []PackageUpgrade
	for _, pkg := range installed {
		pi, err := packages.ParsePackageIdentity(pkg)
		if err != nil {
			continue
		}

		_, latestVersion, err := rr.LatestPackage(pi.Name)
		if err != nil {
			continue
		}

		upgrade := packages.CompareVersions(latestVersion, pi.Version) > 0
		changed, _ := inst.IsPackageChanged(pi.Repo, pi.Name, pi.Version)

		if upgrade || changed {
			u := PackageUpgrade{
				Repo:             pi.Repo,
				Name:             pi.Name,
				InstalledVersion: pi.Version,
				LatestVersion:    latestVersion,
				Changed:          changed,
			}
			upgrades = append(upgrades, u)
		}
	}

	sort.Slice(upgrades, func(i, j int) bool {
		if upgrades[i].Repo != upgrades[j].Repo {
			return upgrades[i].Repo < upgrades[j].Repo
		}
		return upgrades[i].Name < upgrades[j].Name
	})

	return upgrades
}

func upgradesHash(upgrades []PackageUpgrade) string {
	h := sha256.New()
	for _, u := range upgrades {
		_, _ = fmt.Fprintf(h, "%s/%s@%s->%s\n", u.Repo, u.Name, u.InstalledVersion, u.LatestVersion)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (s *SystemControllerHandlers) listUpgrades(c *echo.Context) error {
	upgrades := s.computeUpgrades()
	if upgrades == nil {
		upgrades = []PackageUpgrade{}
	}
	return c.JSON(200, upgrades)
}

func (s *SystemControllerHandlers) dismissUpgrades(c *echo.Context) error {
	upgrades := s.computeUpgrades()
	hash := upgradesHash(upgrades)

	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return echo.NewHTTPError(500, "settings manager not available")
	}

	if err := mgr.Set("dismissed_upgrades_hash", hash); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
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

	e.Add("GET", "/packages/timezones", s.listTimezones, s.requireAuth)
	e.Add("GET", "/packages", s.listPackages, s.requireAuth)
	e.Add("GET", "/packages/by-repo", s.listPackagesByRepo, s.requireAuth)
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
	e.Add("POST", "/packages/children", s.listChildren, s.requireAuth)
	e.Add("POST", "/packages/install-preview", s.installPreview, s.requireAdmin)
	e.Add("POST", "/packages/install", s.installPackage, s.requireAdmin)
	e.Add("POST", "/packages/uninstall", s.uninstallPackage, s.requireAdmin)
	e.Add("POST", "/packages/purge-volumes", s.purgeVolumes, s.requireAdmin)
	e.Add("POST", "/packages/uninstalled-volumes", s.listUninstalledVolumes, s.requireAdmin)
	e.Add("POST", "/packages/purge-uninstalled-volumes", s.purgeUninstalledVolumes, s.requireAdmin)
	e.Add("GET", "/packages/upgrades", s.listUpgrades, s.requireAuth)
	e.Add("POST", "/packages/upgrades/dismiss", s.dismissUpgrades, s.requireAdmin)
	e.Add("POST", "/packages/disable", s.disablePackage, s.requireAdmin)
	e.Add("POST", "/packages/enable", s.enablePackage, s.requireAdmin)
	e.Add("POST", "/systemd/status", s.setUnitStatus, s.requireAdmin)
	e.Add("POST", "/account/disable", s.disableAccount, s.requireAdmin)
	e.Add("POST", "/account/enable", s.enableAccount, s.requireAdmin)
	e.Add("POST", "/audit/log", s.listAuditLog, s.requireAdmin)
	e.Add("GET", "/settings", s.getSettings, s.requireAdmin)
	e.Add("POST", "/settings/get", s.getSetting, s.requireAdmin)
	e.Add("POST", "/settings/set", s.setSetting, s.requireAdmin)
	e.Add("POST", "/storage/upload-archive", s.uploadArchive, s.requireAdmin)
	e.Add("POST", "/storage/download-archive", s.downloadArchive, s.requireAdmin)
}

// --- Server infrastructure ---

type ServerConfig struct {
	Storage                  storage.Storage
	RepositoryRoot           *packages.RepositoryRoot
	Installer                packages.Installer
	Systemd                  systemd.Manager
	AccountMgr               account.Manager
	SessionMgr               account.SessionManager
	AuditMgr                 account.AuditManager
	SettingsMgr              account.SettingsManager
	AllowedHosts             []string
	DefaultRepoUser          string
	DefaultRepoPass          string
	BtrfsBasePath            string
	NetworkControllerBinPath string
	NetworkStatePath         string
	NetworkMode              string
}

func withContext(parent context.Context, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		go func() {
			select {
			case <-parent.Done():
				cancel()
			case <-ctx.Done():
			}
		}()
		handler.ServeHTTP(w, r.WithContext(ctx)) //nolint:contextcheck // merging server lifecycle with request context
	})
}

type serverBase struct {
	ServerConfig

	ctx        context.Context //nolint:containedctx // server lifecycle context
	cancel     context.CancelFunc
	externalIP atomic.Value // stores string
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
func (s *serverBase) GetBtrfsBasePath() string            { return s.BtrfsBasePath }
func (s *serverBase) GetNetworkControllerBinPath() string { return s.NetworkControllerBinPath }
func (s *serverBase) GetNetworkStatePath() string         { return s.NetworkStatePath }
func (s *serverBase) GetNetworkMode() string              { return s.NetworkMode }
func (s *serverBase) GetExternalIP() string {
	v := s.externalIP.Load()
	if v == nil {
		return ""
	}
	ip, _ := v.(string)
	return ip
}

func (s *serverBase) GetInternalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return ""
}

func (s *serverBase) fetchExternalIP(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, "https://ipinfo.io/json", nil)
	if reqErr != nil {
		slog.Debug(fmt.Sprintf("fetchExternalIP: %v", reqErr))
		return
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		slog.Debug(fmt.Sprintf("fetchExternalIP: %v", err))
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Debug(fmt.Sprintf("fetchExternalIP: close body: %v", err))
		}
	}()

	if resp.StatusCode != http.StatusOK {
		slog.Debug(fmt.Sprintf("fetchExternalIP: status %d", resp.StatusCode))
		return
	}

	var result struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Debug(fmt.Sprintf("fetchExternalIP: decode: %v", err))
		return
	}

	if result.IP != "" {
		s.externalIP.Store(result.IP)
	}
}

func (s *serverBase) startExternalIPPoller(ctx context.Context) {
	s.fetchExternalIP(ctx)
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.fetchExternalIP(ctx)
			}
		}
	}()
}

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
				return "", false, nil //nolint:nilerr // unparseable origin is simply rejected
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
	ctx, cancel := context.WithCancel(context.Background())
	sb := &serverBase{ServerConfig: cfg, ctx: ctx, cancel: cancel}
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
	ts.ctx = ctx
	ts.cancel = cancel
	ts.Server = httptest.NewServer(withContext(ctx, configureRouter(ts)))
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
	us.ctx = ctx
	us.cancel = cancel
	us.server = &http.Server{Handler: withContext(ctx, configureRouter(us)), ReadHeaderTimeout: 10 * time.Second}
	return us
}

func (us *UnixServer) Close() error {
	us.cancel()
	return us.server.Close()
}

func (us *UnixServer) Run() error {
	us.startExternalIPPoller(us.ctx)
	lc := net.ListenConfig{}
	lis, err := lc.Listen(us.ctx, "unix", us.Socket)
	if err != nil {
		return fmt.Errorf("could not listen on unix socket %q: %w", us.Socket, err)
	}
	return us.server.Serve(lis)
}

func (us *UnixServer) Client() (*SystemdClient, error) {
	return InitClient(us.Socket)
}

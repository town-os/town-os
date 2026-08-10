package systemcontroller

import (
	"log/slog"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

// PingMinimalResponse is returned for unauthenticated ping requests.
type PingMinimalResponse struct {
	Status     string `json:"status"`
	NeedsSetup bool   `json:"needs_setup,omitempty"`
	// BootID is carried even in the minimal response: the refresh flow
	// polls ping across a controller restart, and during that window the
	// browser may briefly be unauthenticated (the boot stub answers no
	// tokens at all). Without it a client cannot distinguish the outgoing
	// process from the incoming one. It is a random per-process UUID and
	// discloses nothing about the system.
	BootID string `json:"boot_id,omitempty"`
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
	SystemServices     *UnitCounts        `json:"system_services,omitempty"`
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
	RepositoryErrors   map[string]string  `json:"repository_errors,omitempty"`
	TimezoneOffset     int                `json:"timezone_offset"`
	Locale             string             `json:"locale"`
	ProtonEnabled      bool               `json:"proton_enabled"`
	// BootID identifies this process incarnation; see BootStatus.id. The
	// boot stub's /status/ping reports the same field, so the refresh UI
	// can follow a restart across the stub → full-router handoff.
	BootID string `json:"boot_id,omitempty"`
}

type UnitCounts struct {
	Total  int `json:"total"`
	Active int `json:"active"`
	Failed int `json:"failed"`
}

func (s *SystemControllerHandlers) ping(c *echo.Context) error {
	resp := PingResponse{Status: "ok", BootID: s.Controller.GetBootID()}

	// List accounts once; used for NeedsSetup, the total count, and the
	// admin count. Previously this handler called am.List() twice.
	var accounts []account.Account
	if am := s.Controller.GetAccountManager(); am != nil {
		list, err := am.List()
		if err != nil {
			return err
		}
		accounts = list
		// Shared with the bootstrap branch of POST /account/create so the two
		// cannot answer "is this box set up" differently.
		resp.NeedsSetup = !hasEnabledAdmin(list)
	}

	// Check for optional auth — if a session manager is configured and no
	// valid token is provided, return minimal response (status + needs_setup).
	// Localhost requests always receive the full response regardless of auth.
	sm := s.Controller.GetSessionManager()
	if sm != nil {
		token := extractBearerToken(c.Request())
		if token != "" {
			sess, _, err := sm.Validate(token)
			if err == nil {
				resp.Username = sess.Username
			}
		}
		// Non-localhost requests without a valid token get the minimal response.
		if resp.Username == "" && !isLocalhost(c.Request()) {
			return c.JSON(200, PingMinimalResponse{
				Status:     resp.Status,
				NeedsSetup: resp.NeedsSetup,
				BootID:     resp.BootID,
			})
		}
	}

	if st := s.Controller.GetStorage(); st != nil {
		// Use FilesystemNames, not ListFilesystems: the ping only needs to
		// classify subvolumes by name. ListFilesystems invokes
		// `btrfs qgroup show` once per subvolume (plus a rootid lookup),
		// adding 2N fork+exec per call. At N ~= 30 that was ~1s of the
		// ping's latency budget — entirely wasted since we never use the
		// quota here.
		names, err := st.FilesystemNames("")
		if err != nil {
			slog.Error("listing filesystem names", "error", err)
		}
		userCount := 0
		installedVols := 0
		uninstalledVols := 0
		for _, name := range names {
			state, _ := classifyFilesystem(name)
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

		if errs := rr.RefreshErrors(); len(errs) > 0 {
			resp.RepositoryErrors = errs
		}
	}

	// Cache the installed-package list so we only read it once for the
	// count, the unit-name set below, and computeUpgrades().
	var installedPackages []string
	if inst := s.Controller.GetInstaller(); inst != nil {
		pkgs, err := inst.ListInstalled()
		if err != nil {
			return err
		}
		installedPackages = pkgs
		resp.Installed = len(pkgs)
	}

	resp.Accounts = len(accounts)
	for _, a := range accounts {
		if !a.Disabled && a.Admin {
			resp.Admins++
		}
	}

	if sd := s.Controller.GetSystemdManager(); sd != nil {
		units, err := sd.ListUnits(c.Request().Context())
		if err != nil {
			return err
		}

		// Build a set of unit names for installed packages so the
		// counts only reflect packages that are actually installed.
		// Reuse the list captured above to avoid a second filesystem scan.
		installedUnits := make(map[string]struct{}, len(installedPackages))
		for _, pkg := range installedPackages {
			pi, parseErr := packages.ParsePackageIdentity(pkg)
			if parseErr != nil {
				continue
			}
			installedUnits[systemd.UnitName(pi.Repo, pi.Name, pi.Version)] = struct{}{}
		}

		counts := &UnitCounts{}
		sysCounts := &UnitCounts{}
		for _, u := range units {
			if systemd.IsSystemServiceUnit(u.Name) {
				// Skip NC and socket units — only count main service units.
				if strings.HasSuffix(u.Name, "-network.service") || strings.HasSuffix(u.Name, ".socket") {
					continue
				}
				sysCounts.Total++
				switch u.ActiveState {
				case "active":
					sysCounts.Active++
				case "failed":
					sysCounts.Failed++
				}
				continue
			}
			if !systemd.IsPackageServiceUnit(u.Name) {
				continue
			}
			if _, ok := installedUnits[u.Name]; !ok {
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
		if sysCounts.Total > 0 {
			resp.SystemServices = sysCounts
		}
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
	resp.TimezoneOffset = packages.TimezoneOffset()
	resp.Locale = s.getLocale()
	resp.ProtonEnabled = packages.ProtonEnabled()

	// Compute upgrade info. Reuse the installed list gathered above to
	// avoid a second fs walk.
	upgrades := s.computeUpgradesFromList(installedPackages)
	resp.UpgradesAvailable = len(upgrades)
	if len(upgrades) > 0 {
		if mgr := s.Controller.GetSettingsManager(); mgr != nil {
			dismissed, err := mgr.Get(c.Request().Context(), "dismissed_upgrades_hash")
			if err == nil && dismissed == upgradesHash(upgrades) {
				resp.UpgradesDismissed = true
			}
		}
	}

	return c.JSON(200, resp)
}

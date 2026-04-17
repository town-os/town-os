package systemcontroller

import (
	"log/slog"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

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
	PagesEnabled       bool               `json:"pages_enabled"`
	ProtonEnabled      bool               `json:"proton_enabled"`
}

type UnitCounts struct {
	Total  int `json:"total"`
	Active int `json:"active"`
	Failed int `json:"failed"`
}

func (s *SystemControllerHandlers) ping(c *echo.Context) error {
	resp := PingResponse{Status: "ok"}

	// NeedsSetup is true only when no enabled admin account exists.
	if am := s.Controller.GetAccountManager(); am != nil {
		accounts, err := am.List()
		if err != nil {
			return err
		}
		hasAdmin := false
		for _, a := range accounts {
			if !a.Disabled && a.Admin {
				hasAdmin = true
				break
			}
		}
		resp.NeedsSetup = !hasAdmin
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
			return c.JSON(200, PingMinimalResponse{Status: resp.Status, NeedsSetup: resp.NeedsSetup})
		}
	}

	if st := s.Controller.GetStorage(); st != nil {
		fs, err := st.ListFilesystems("")
		if err != nil {
			slog.Error("listing filesystems", "error", err)
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

		if errs := rr.RefreshErrors(); len(errs) > 0 {
			resp.RepositoryErrors = errs
		}
	}

	// Cache the installed-package list so we only read it once for both
	// the count and the unit-name set below.
	var installedPackages []string
	if inst := s.Controller.GetInstaller(); inst != nil {
		pkgs, err := inst.ListInstalled()
		if err != nil {
			return err
		}
		installedPackages = pkgs
		resp.Installed = len(pkgs)
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
	resp.PagesEnabled = s.Controller.GetPagesManager() != nil
	resp.ProtonEnabled = packages.ProtonEnabled()

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

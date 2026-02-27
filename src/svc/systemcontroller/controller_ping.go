package systemcontroller

import (
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

		// Build a set of unit names for installed packages so the
		// counts only reflect packages that are actually installed.
		installedUnits := map[string]struct{}{}
		if inst := s.Controller.GetInstaller(); inst != nil {
			installed, listErr := inst.ListInstalled()
			if listErr == nil {
				for _, pkg := range installed {
					pi, parseErr := packages.ParsePackageIdentity(pkg)
					if parseErr != nil {
						continue
					}
					installedUnits[systemd.UnitName(pi.Repo, pi.Name, pi.Version)] = struct{}{}
				}
			}
		}

		counts := &UnitCounts{}
		for _, u := range units {
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

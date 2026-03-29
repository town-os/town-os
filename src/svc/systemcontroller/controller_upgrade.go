package systemcontroller

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"

	"gitea.com/town-os/town-os/src/i18n"
	"gitea.com/town-os/town-os/src/packages"
	"github.com/labstack/echo/v5"
)

type PackageUpgrade struct {
	Repo             string `json:"repo"`
	Name             string `json:"name"`
	InstalledVersion string `json:"installed_version"`
	LatestVersion    string `json:"latest_version"`
	Changed          bool   `json:"changed"`
}

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
		changed, err := inst.IsPackageChanged(pi.Repo, pi.Name, pi.Version)
		if err != nil {
			continue
		}

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
		if _, err := fmt.Fprintf(h, "%s/%s@%s->%s\n", u.Repo, u.Name, u.InstalledVersion, u.LatestVersion); err != nil {
			slog.Error(fmt.Sprintf("upgrade hash write: %v", err))
		}
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
		return echo.NewHTTPError(500, i18n.T(s.getLocale(), i18n.MsgUpgradeSettingsMissing))
	}

	if err := mgr.Set("dismissed_upgrades_hash", hash); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

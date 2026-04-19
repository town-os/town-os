// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

// RemovePackageVolumeGroupRequest drives the cascading delete that the
// storage UI wires to the non-leaf delete buttons on its package/version
// tree nodes.
//
// Semantics:
//   - Repo + Name are required; an empty Version targets every installed
//     version of (Repo, Name) (the package-level button).
//   - Every systemd unit in the target package's dependency tree is
//     stopped before any subvolume is removed, so a podman container still
//     holding the volume open cannot race the btrfs delete.
//   - Volumes are removed under `installed/<repo>/<storagePath>/[version/]`.
//     IncludeUninstalled additionally sweeps the matching `uninstalled/`
//     subtree — the UI wires this to the same "Show uninstalled" toggle
//     that drives ListPackageVolumes.
type RemovePackageVolumeGroupRequest struct {
	Repo               string `json:"repo"`
	Name               string `json:"name"`
	Version            string `json:"version,omitempty"`
	IncludeUninstalled bool   `json:"include_uninstalled,omitempty"`
}

func (s *SystemControllerHandlers) removePackageVolumeGroup(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := RemovePackageVolumeGroupRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}
	if req.Repo == "" || req.Name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "repo and name are required",
		})
	}

	unlock := s.lockPackage(req.Repo, req.Name)
	defer unlock()

	ctx := c.Request().Context()
	if err := s.stopVolumeGroupUnits(ctx, req.Repo, req.Name, req.Version); err != nil {
		return err
	}

	st := s.Controller.GetStorage()
	if st == nil {
		c.Response().WriteHeader(http.StatusOK)
		return nil
	}

	installed, uninst := s.volumeGroupPrefixes(req.Repo, req.Name, req.Version)
	for _, p := range installed {
		if err := s.purgeVolumePrefix(st, p); err != nil {
			return err
		}
	}
	if req.IncludeUninstalled {
		for _, p := range uninst {
			if err := s.purgeVolumePrefix(st, p); err != nil {
				return err
			}
		}
	}

	c.Response().WriteHeader(http.StatusOK)
	return nil
}

// volumeGroupPrefixes returns the installed/ and uninstalled/ prefixes the
// cascade should purge for (repo, name, version).
//
// Package-level (empty version) expands to a single wide prefix rooted at
// installed/<repo>/<storagePath>/ — deps live at
// installed/<repo>/<storagePath>/subpackages/<key>/... so a single
// deepest-first sweep catches every descendant across every installed
// version.
//
// Version-scoped requests have to walk the dep tree because deps are
// installed at their own version under subpackages/<key>/<depVersion>/,
// which need not match the parent's version string. Each (effectiveName,
// depVersion) tuple emits its own tightly-scoped prefix so a version-level
// delete of parent 1.0 never collaterals parent 2.0's copy of the same
// dep.
func (s *SystemControllerHandlers) volumeGroupPrefixes(repo, name, version string) (installed, uninstalled []string) {
	storagePath := packages.StoragePath(name)
	if version == "" {
		return []string{
				fmt.Sprintf("%s/%s/%s/", PackagesVolumePrefix, repo, storagePath),
			},
			[]string{
				fmt.Sprintf("%s/%s/%s/", UninstalledVolumePrefix, repo, storagePath),
			}
	}

	// Always include the parent's own version prefix — it exists even
	// when collectTreeUnits finds no install record (e.g. a forensic
	// cleanup after a half-uninstalled package).
	installed = append(installed,
		fmt.Sprintf("%s/%s/%s/%s/", PackagesVolumePrefix, repo, storagePath, version))
	uninstalled = append(uninstalled,
		fmt.Sprintf("%s/%s/%s/%s/", UninstalledVolumePrefix, repo, storagePath, version))

	tuples, err := s.collectDepVolumeTuples(repo, name, version)
	if err != nil {
		// A load failure here is a forensic warning — the prefixes we
		// already emitted cover the happy path, and the parent unit
		// was stopped above, so downgrade rather than fail the delete.
		return installed, uninstalled
	}
	seen := map[string]struct{}{
		installed[0]: {},
	}
	for _, tp := range tuples {
		inst := fmt.Sprintf("%s/%s/%s/%s/",
			PackagesVolumePrefix, tp.repo, packages.StoragePath(tp.effectiveName), tp.version)
		if _, dup := seen[inst]; dup {
			continue
		}
		seen[inst] = struct{}{}
		installed = append(installed, inst)
		uninstalled = append(uninstalled, fmt.Sprintf("%s/%s/%s/%s/",
			UninstalledVolumePrefix, tp.repo, packages.StoragePath(tp.effectiveName), tp.version))
	}
	return installed, uninstalled
}

// depVolumeTuple uniquely identifies a package install (parent or
// dependency) for prefix computation.
type depVolumeTuple struct {
	repo          string
	effectiveName string
	version       string
}

// collectDepVolumeTuples walks the dep tree rooted at (repo, name,
// version) and returns one tuple per node — the parent plus every
// transitive dependency. Missing dep records are silently skipped,
// matching walkTreeUnits' forensic-friendly stance.
func (s *SystemControllerHandlers) collectDepVolumeTuples(repo, name, version string) ([]depVolumeTuple, error) {
	var out []depVolumeTuple
	if err := s.walkDepVolumeTuples(repo, name, version, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SystemControllerHandlers) walkDepVolumeTuples(repo, name, version string, out *[]depVolumeTuple) error {
	*out = append(*out, depVolumeTuple{repo: repo, effectiveName: name, version: version})
	inst := s.Controller.GetInstaller()
	if inst == nil {
		return nil
	}
	deps, err := inst.LoadDependencies(repo, name)
	if err != nil {
		return err
	}
	for _, rec := range deps {
		if err := s.walkDepVolumeTuples(rec.Repo, rec.EffectiveName, rec.Version, out); err != nil {
			return err
		}
	}
	return nil
}

// stopVolumeGroupUnits halts every systemd service unit belonging to the
// targeted package and each of its transitive dependencies. When version
// is empty, every installed version of (repo, name) known to the install
// manager contributes a subtree. Duplicates are coalesced so a shared unit
// is only stopped once. The systemcontroller's own unit is always skipped
// even if somehow named in the tree.
func (s *SystemControllerHandlers) stopVolumeGroupUnits(ctx context.Context, repo, name, version string) error {
	sd := s.Controller.GetSystemdManager()
	if sd == nil {
		return nil
	}

	var targets [][3]string
	if version != "" {
		targets = append(targets, [3]string{repo, name, version})
	} else if inst := s.Controller.GetInstaller(); inst != nil {
		installed, err := inst.ListInstalled()
		if err != nil {
			return err
		}
		for _, pkg := range installed {
			pi, parseErr := packages.ParsePackageIdentity(pkg)
			if parseErr != nil {
				continue
			}
			if pi.Repo != repo || pi.Name != name {
				continue
			}
			targets = append(targets, [3]string{pi.Repo, pi.Name, pi.Version})
		}
	}

	seen := map[string]struct{}{}
	for _, t := range targets {
		units, err := s.collectTreeUnits(t[0], t[1], t[2])
		if err != nil {
			return err
		}
		// Stop root before descendants so PartOf cascades take effect —
		// collectTreeUnits returns leaves-first, so reverse.
		reverseUnits(units)
		for _, u := range units {
			if _, dup := seen[u]; dup {
				continue
			}
			seen[u] = struct{}{}
			if u == systemd.SystemControllerUnitName {
				continue
			}
			if err := sd.SetStatus(ctx, u, systemd.Stop); err != nil {
				return err
			}
		}
	}
	return nil
}

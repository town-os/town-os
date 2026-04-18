package systemcontroller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"gitea.com/town-os/town-os/src/i18n"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

// UnitTreeNode is a package service unit rendered as a node in a dependency
// tree. Children are the unit entries for packages installed as direct
// dependencies of this node's package; grandchildren are sub-dependencies,
// recursively. A package with no deps has no Children.
type UnitTreeNode struct {
	UnitListEntry

	// Repo, Name, Version round-trip through the package install APIs.
	// Name is the raw effective name (may contain "--dep--" for dep
	// nodes). DisplayIdentifier (embedded from UnitListEntry) is the
	// human-facing form the UI shows.
	Repo    string `json:"repo"`
	Name    string `json:"name"`
	Version string `json:"version"`

	Children []UnitTreeNode `json:"children,omitempty"`
}

// UnitTreeResponse wraps the top-level root entries with pagination metadata.
// Dependency descendants do not count against pagination; search and paging
// apply to root nodes only, so a tree always ships with its full dep subtree
// even at the last page boundary.
type UnitTreeResponse = PageResult[UnitTreeNode]

// TreeStatusRequest is the body of POST /systemd/status/tree. Name is the
// raw effective name of the root package (not the pretty form), so repeat
// callers can feed back values from the existing install APIs unchanged.
type TreeStatusRequest struct {
	Repo    string               `json:"repo"`
	Name    string               `json:"name"`
	Version string               `json:"version"`
	Action  systemd.StatusAction `json:"action"`
}

// listUnitsTree groups the flat /systemd/units list into a dependency tree.
// The shape mirrors /storage/package-volumes' grouping: root packages at
// the top, deps nested under their parent, all the way down. Unit status
// (active/failed, NC failure, description) is the same data the flat
// endpoint returns, so clients do not need a second fetch to enrich rows.
func (s *SystemControllerHandlers) listUnitsTree(c *echo.Context) error {
	entries, err := s.collectUnitEntries(c)
	if err != nil {
		return err
	}

	// Index entries by (repo, effectiveName, version) so we can look them
	// up while walking the dep graph. Only installed packages surface
	// here — orphan systemd units were already dropped by collectUnitEntries.
	byIdentity := map[string]UnitListEntry{}
	for _, e := range entries {
		byIdentity[e.PackageIdentifier] = e
	}

	roots := make([]UnitTreeNode, 0)
	inst := s.Controller.GetInstaller()
	for _, e := range entries {
		if e.IsDependency {
			continue
		}
		pi, parseErr := packages.ParsePackageIdentity(e.PackageIdentifier)
		if parseErr != nil {
			continue
		}
		roots = append(roots, s.buildTreeNode(inst, byIdentity, pi.Repo, pi.Name, pi.Version, e))
	}

	p := readListParams(c)
	roots = filterTreeSearch(roots, p.Search)
	sortTreeNodes(roots, p.SortBy, p.SortOrder)

	return c.JSON(200, paginate(roots, p.Limit, p.Offset))
}

// buildTreeNode recursively constructs a UnitTreeNode from an installed
// package's dependency records. Missing dep records are silently skipped
// (a dep's install record may have been stripped by a partial uninstall)
// so the tree still renders whatever is left instead of returning no
// tree at all.
func (s *SystemControllerHandlers) buildTreeNode(
	inst packages.Installer,
	byIdentity map[string]UnitListEntry,
	repo, name, version string,
	entry UnitListEntry,
) UnitTreeNode {
	node := UnitTreeNode{
		UnitListEntry: entry,
		Repo:          repo,
		Name:          name,
		Version:       version,
	}

	if inst == nil {
		return node
	}

	deps, err := inst.LoadDependencies(repo, name)
	if err != nil || len(deps) == 0 {
		return node
	}

	depKeys := make([]string, 0, len(deps))
	for k := range deps {
		depKeys = append(depKeys, k)
	}
	sort.Strings(depKeys)

	children := make([]UnitTreeNode, 0, len(depKeys))
	for _, k := range depKeys {
		rec := deps[k]
		childID := fmt.Sprintf("%s/%s@%s", rec.Repo, rec.EffectiveName, rec.Version)
		childEntry, ok := byIdentity[childID]
		if !ok {
			continue
		}
		children = append(children, s.buildTreeNode(inst, byIdentity, rec.Repo, rec.EffectiveName, rec.Version, childEntry))
	}

	node.Children = children
	return node
}

// collectUnitEntries runs the same enrichment pipeline as listUnits and
// returns the full (un-paginated) list of valid package unit entries. It
// exists as a shared helper so the tree endpoint can reuse the exact
// identification, description, and NC-status logic without duplicating it.
func (s *SystemControllerHandlers) collectUnitEntries(c *echo.Context) ([]UnitListEntry, error) {
	units, err := s.Controller.GetSystemdManager().ListUnits(c.Request().Context())
	if err != nil {
		return nil, err
	}

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

	identityMap := map[string]string{}
	displayMap := map[string]string{}
	isDepMap := map[string]bool{}
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
				displayMap[unitName] = fmt.Sprintf("%s/%s@%s", pi.Repo, packages.PrettyName(pi.Name), pi.Version)
				isDepMap[unitName] = packages.IsDependency(pi.Name)

				if rr != nil {
					ip, loadErr := rr.LoadPackage(pi.Repo, pi.Name, pi.Version)
					if loadErr == nil {
						descriptionMap[unitName] = ip.Description
					}
				}
			}
		}
	}

	entries := make([]UnitListEntry, 0, len(filtered))
	for _, u := range filtered {
		pkgID, ok := identityMap[u.Name]
		if !ok {
			continue
		}
		entry := UnitListEntry{
			UnitStatus:         u,
			PackageIdentifier:  pkgID,
			DisplayIdentifier:  displayMap[u.Name],
			IsDependency:       isDepMap[u.Name],
			PackageDescription: descriptionMap[u.Name],
		}
		ncName := strings.TrimSuffix(u.Name, ".service") + "-network.service"
		if ncUnit, ok := ncUnitMap[ncName]; ok {
			if ncUnit.ActiveState == "failed" {
				entry.NCFailed = true
				if entry.ActiveState != "failed" {
					entry.ActiveState = "failed"
				}
			}
			if ncUnit.ActiveState == "active" {
				entry.NCActive = true
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// filterTreeSearch filters root nodes whose own fields OR any descendant's
// fields match the search term. This keeps hit nodes visible even when the
// matched dep is buried several levels down; the UI then expands the whole
// subtree so the user can find the hit.
func filterTreeSearch(roots []UnitTreeNode, search string) []UnitTreeNode {
	if search == "" {
		return roots
	}
	term := strings.ToLower(search)
	out := make([]UnitTreeNode, 0, len(roots))
	for _, r := range roots {
		if treeNodeMatches(r, term) {
			out = append(out, r)
		}
	}
	return out
}

func treeNodeMatches(n UnitTreeNode, term string) bool {
	if matchesSearch(n.UnitListEntry, term) {
		return true
	}
	for _, c := range n.Children {
		if treeNodeMatches(c, term) {
			return true
		}
	}
	return false
}

// sortTreeNodes sorts root nodes by the named field. Children are always
// sorted alphabetically by dep key (done at build time); parent sort only
// reorders roots so the same tree structure always renders the same way.
func sortTreeNodes(roots []UnitTreeNode, sortBy, sortOrder string) {
	flat := make([]UnitListEntry, len(roots))
	idx := make(map[string]int, len(roots))
	for i, r := range roots {
		flat[i] = r.UnitListEntry
		idx[r.PackageIdentifier] = i
	}
	flat = sortSlice(flat, sortBy, sortOrder)

	sorted := make([]UnitTreeNode, len(roots))
	for i, e := range flat {
		sorted[i] = roots[idx[e.PackageIdentifier]]
	}
	copy(roots, sorted)
}

// setUnitStatusTree cascades the requested action across every unit in a
// package's dependency tree. The request identifies the root by (repo,
// name, version); the handler walks the persisted dependency records to
// discover every descendant and calls the action on each unit. Order is
// chosen so systemd ordering directives (PartOf / After=) are respected:
//
//   - start/restart: leaves first, root last (so parents see their deps up).
//   - stop:          root first, leaves last (PartOf cascades; the explicit
//     leaf calls are belt-and-braces against a dep that
//     somehow stayed up).
//
// Enable/Disable are rejected identically to /systemd/status because a
// cascade of Enable would double-enable deps that are already linked via
// PartOf, and the operation has no well-defined tree semantics anyway.
func (s *SystemControllerHandlers) setUnitStatusTree(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := TreeStatusRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	locale := s.getLocale()
	if req.Action == systemd.Enable || req.Action == systemd.Disable {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgUnitEnableDisableNotAllowed))
	}
	if req.Repo == "" || req.Name == "" || req.Version == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "repo, name, and version are required")
	}

	rootUnit := systemd.UnitName(req.Repo, req.Name, req.Version)
	if req.Action == systemd.Stop && rootUnit == systemd.SystemControllerUnitName {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgUnitCannotStopController))
	}

	units, err := s.collectTreeUnits(req.Repo, req.Name, req.Version)
	if err != nil {
		return err
	}

	// Pick traversal order based on the action. collectTreeUnits returns
	// units in leaves-first order (the natural order for start/restart).
	// Reverse for stop so we hit the root before its descendants.
	if req.Action == systemd.Stop {
		reverseUnits(units)
	}

	sd := s.Controller.GetSystemdManager()
	ctx := c.Request().Context()
	for _, u := range units {
		if req.Action == systemd.Stop && u == systemd.SystemControllerUnitName {
			continue
		}
		if err := sd.SetStatus(ctx, u, req.Action); err != nil {
			return err
		}
	}

	c.Response().WriteHeader(200)
	return nil
}

// collectTreeUnits returns the full list of systemd service unit names for
// a package and all its transitive dependencies, ordered leaves-first so
// callers can issue start actions bottom-up. Missing dep records are
// silently skipped (a half-installed tree still gets what it can).
func (s *SystemControllerHandlers) collectTreeUnits(repo, name, version string) ([]string, error) {
	var out []string
	if err := s.walkTreeUnits(repo, name, version, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SystemControllerHandlers) walkTreeUnits(repo, name, version string, out *[]string) error {
	inst := s.Controller.GetInstaller()
	if inst != nil {
		deps, err := inst.LoadDependencies(repo, name)
		if err != nil {
			return err
		}
		depKeys := make([]string, 0, len(deps))
		for k := range deps {
			depKeys = append(depKeys, k)
		}
		sort.Strings(depKeys)
		for _, k := range depKeys {
			rec := deps[k]
			if err := s.walkTreeUnits(rec.Repo, rec.EffectiveName, rec.Version, out); err != nil {
				return err
			}
		}
	}
	*out = append(*out, systemd.UnitName(repo, name, version))
	return nil
}

func reverseUnits(u []string) {
	for i, j := 0, len(u)-1; i < j; i, j = i+1, j-1 {
		u[i], u[j] = u[j], u[i]
	}
}

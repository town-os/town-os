package systemcontroller

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

// depKeyRefRegex matches sibling dependency references of the form
// @dep_KEY_host@, @dep_KEY_port_N@ (numeric container port), or
// @dep_KEY_port_NAME@ (semantic port name declared in the dep's
// network.external / network.internal YAML keys). Capture group 1 is
// the key; the port identifier (if any) is not captured separately
// because callers need only the key for topological ordering.
//
// The port suffix alternation distinguishes numeric (`\d+`) from named
// (`[a-zA-Z][a-zA-Z0-9_]*`) forms so the key's greedy `[a-zA-Z0-9_]+`
// cannot bleed into the port name — a name must start with a letter,
// which forces the regex engine to end the key at the last `_` before a
// letter-started port token.
var depKeyRefRegex = regexp.MustCompile(`@dep_([a-zA-Z0-9_]+)_(?:host|port_(?:\d+|[a-zA-Z][a-zA-Z0-9_]*))@`)

// extractDepKeyRefs returns the sibling dep keys referenced in a string
// via @dep_KEY_host@ or @dep_KEY_port_N@ template markers. Duplicates are
// preserved in the order they appear; callers that need a set should
// deduplicate.
func extractDepKeyRefs(val string) []string {
	matches := depKeyRefRegex.FindAllStringSubmatch(val, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) >= 2 {
			out = append(out, m[1])
		}
	}
	return out
}

// orderDependencies returns the dep keys in an order where any dep that
// references another sibling via @dep_KEY_host@ or @dep_KEY_port_N@ is
// placed after the dep it references. Deps with no sibling references sort
// alphabetically among themselves for determinism. References to keys that
// are not siblings (e.g. unrelated template vars, or names that happen to
// match the @dep_*@ pattern) are ignored. Self-references are ignored.
// Returns an error if a cycle is detected among sibling references.
func orderDependencies(deps map[string]packages.InputPackageDependency) ([]string, error) {
	known := make(map[string]bool, len(deps))
	for key := range deps {
		known[key] = true
	}

	inDegree := make(map[string]int, len(deps))
	edgesFrom := make(map[string][]string, len(deps))
	for key := range deps {
		inDegree[key] = 0
	}
	for key, dep := range deps {
		seen := map[string]bool{}
		for _, val := range dep.Responses {
			for _, ref := range extractDepKeyRefs(val) {
				if !known[ref] || ref == key || seen[ref] {
					continue
				}
				seen[ref] = true
				inDegree[key]++
				edgesFrom[ref] = append(edgesFrom[ref], key)
			}
		}
	}

	ready := make([]string, 0, len(deps))
	for key, deg := range inDegree {
		if deg == 0 {
			ready = append(ready, key)
		}
	}
	sort.Strings(ready)

	order := make([]string, 0, len(deps))
	for len(ready) > 0 {
		key := ready[0]
		ready = ready[1:]
		order = append(order, key)

		next := edgesFrom[key]
		sort.Strings(next)
		for _, n := range next {
			inDegree[n]--
			if inDegree[n] == 0 {
				ready = append(ready, n)
			}
		}
		sort.Strings(ready)
	}

	if len(order) != len(deps) {
		remaining := make([]string, 0, len(deps)-len(order))
		for k, d := range inDegree {
			if d > 0 {
				remaining = append(remaining, k)
			}
		}
		sort.Strings(remaining)
		return nil, fmt.Errorf("dependency cycle among sibling deps: %v", remaining)
	}

	return order, nil
}

// installDependencies resolves and installs all declared dependencies for a
// parent package. Dependencies are installed depth-first (leaves before
// parent). Each dependency gets a namespaced effective name, local-only
// networking (UPnP disabled), and namespaced storage. All dependencies
// share the parent's podman network and have systemd PartOf/Before
// ordering relative to the parent.
//
// The function returns dependency records for persistence and a map of
// environment variables to inject into the parent package.
func (s *SystemControllerHandlers) installDependencies(
	ctx context.Context,
	parentRepoName, parentEffectiveName, parentVersion string,
	parentNCUnitName string,
	deps map[string]packages.InputPackageDependency,
) (map[string]packages.DependencyRecord, map[string]string, error) {
	if len(deps) == 0 {
		return nil, nil, nil
	}

	// Order siblings so any dep that references another via @dep_KEY_host@
	// or @dep_KEY_port_N@ in its Responses is installed after the dep it
	// references. Without this, the referenced dep's host/port envVars are
	// not yet known when the referencing dep compiles, and if any of those
	// Responses feed a typed question (e.g. `type: port`), the dep's Compile
	// rejects the literal placeholder. Determinism also helps reproduce
	// install-time failures.
	order, err := orderDependencies(deps)
	if err != nil {
		return nil, nil, err
	}

	rr := s.Controller.GetRepositoryRoot()
	inst := s.Controller.GetInstaller()
	records := make(map[string]packages.DependencyRecord, len(deps))
	envVars := map[string]string{}

	for _, depKey := range order {
		dep := deps[depKey]
		effectiveName := packages.DependencyName(parentEffectiveName, depKey)

		// Resolve the repo: default to the parent's repo.
		depRepo := dep.Repo
		if depRepo == "" {
			depRepo = parentRepoName
		}

		// Resolve the version: use specified or find latest.
		depVersion := dep.Version
		if depVersion == "" {
			_, latestVersion, err := rr.LatestPackage(dep.Package)
			if err != nil {
				return nil, nil, fmt.Errorf("dependency %q: resolve latest version of %q: %w", depKey, dep.Package, err)
			}
			depVersion = latestVersion

			// Also resolve repo if not specified.
			if dep.Repo == "" {
				foundRepo, err := rr.FindRepoForPackage(dep.Package, depVersion)
				if err != nil {
					return nil, nil, fmt.Errorf("dependency %q: find repo for %q@%s: %w", depKey, dep.Package, depVersion, err)
				}
				depRepo = foundRepo
			}
		}

		// Load the dependency's InputPackage.
		depIP, err := rr.LoadPackage(depRepo, dep.Package, depVersion)
		if err != nil {
			return nil, nil, fmt.Errorf("dependency %q: load package %s/%s@%s: %w", depKey, depRepo, dep.Package, depVersion, err)
		}

		// Build responses from dependency declaration.
		depResponses := packages.Responses{}
		if dep.Responses != nil {
			maps.Copy(depResponses, dep.Responses)
		}

		// Resolve @dep_OTHER_host@ / @dep_OTHER_port_N@ in this dep's
		// Responses against envVars collected from already-installed
		// sibling deps. The topological order above guarantees any
		// referenced sibling has already populated envVars. Without this
		// substitution, dep Compile would see literal placeholders and
		// fail type validation on typed questions (e.g. `type: port`).
		applyDepTemplates(map[string]string(depResponses), envVars)

		// Auto-generate missing responses (ports, hostnames, secrets).
		if err := s.autoGenerateResponses(&depResponses, depIP.Questions, effectiveName); err != nil { //nolint:contextcheck // autoGenerateResponses does not accept context; pre-existing pattern
			return nil, nil, fmt.Errorf("dependency %q: auto-generate responses: %w", depKey, err)
		}

		// Compile the dependency.
		depCompiled, err := depIP.CompileWithContext(depResponses, packages.CompileContext{
			ExternalHost: s.Controller.GetExternalIP(),
			InternalHost: s.Controller.GetInternalIP(),
			PackageDNS:   effectiveName + "." + depRepo + "." + s.getDNSTLDValue(),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("dependency %q: compile: %w", depKey, err)
		}

		// Recursively install sub-dependencies first (depth-first).
		if len(depCompiled.Dependencies) > 0 {
			subRecords, _, err := s.installDependencies(ctx, depRepo, effectiveName, depVersion, parentNCUnitName, depCompiled.Dependencies)
			if err != nil {
				return nil, nil, fmt.Errorf("dependency %q: sub-dependencies: %w", depKey, err)
			}
			// Sub-dependency records are saved under the sub-dependency's own name.
			if len(subRecords) > 0 {
				if err := inst.SaveDependencies(depRepo, effectiveName, subRecords); err != nil {
					return nil, nil, fmt.Errorf("dependency %q: save sub-dependencies: %w", depKey, err)
				}
			}
		}

		// Provision volumes.
		if err := s.provisionVolumes(depRepo, effectiveName, depVersion, "", false, depCompiled); err != nil {
			return nil, nil, fmt.Errorf("dependency %q: provision volumes: %w", depKey, err)
		}

		// Seed volume data.
		s.seedVolumeData(ctx, &depIP, depCompiled, depRepo, dep.Package, effectiveName, depVersion)

		// Apply file templates.
		if len(depCompiled.Templates) > 0 {
			s.applyPackageTemplates(depCompiled, depResponses, depRepo, effectiveName, depVersion, depIP.Description)
		}

		// Create install record.
		if err := inst.Install(depRepo, effectiveName, dep.Package, depVersion, depResponses); err != nil {
			return nil, nil, fmt.Errorf("dependency %q: install record: %w", depKey, err)
		}

		// Dependencies share the parent's NC — no network state file needed.
		// Only the parent writes a state file for its NC to read.

		// Install and start systemd units. Dependencies share the parent's
		// podman network and have systemd ordering (PartOf/Before parent).
		if sd := s.Controller.GetSystemdManager(); sd != nil {
			cfg := s.packageUnitConfig(depRepo, effectiveName, depVersion, depIP.Description, depCompiled)
			cfg.ParentNetwork = systemd.NetworkName(parentRepoName, parentEffectiveName, parentVersion)
			cfg.ParentUnitName = systemd.UnitName(parentRepoName, parentEffectiveName, parentVersion)
			cfg.ParentNCUnitName = parentNCUnitName
			units := systemd.GeneratePackageUnits(cfg)
			if err := s.installPackageUnits(ctx, sd, units); err != nil {
				return nil, nil, fmt.Errorf("dependency %q: install units: %w", depKey, err)
			}
		}

		// Register DNS for the dependency.
		s.registerPackageDNS(ctx, depRepo, effectiveName, depCompiled.Network.Domains)

		// Record the dependency.
		records[depKey] = packages.DependencyRecord{
			EffectiveName: effectiveName,
			Package:       dep.Package,
			Repo:          depRepo,
			Version:       depVersion,
		}

		// Inject environment variables for the parent. Dependencies share
		// the parent's podman network, so the host is the container name
		// (resolvable via podman DNS) and ports use container-side values.
		// Both forms are emitted: TOWNOS_DEP_<KEY>_PORT_<N> (numeric) and
		// TOWNOS_DEP_<KEY>_PORT_<NAME> (uppercased semantic name) when the
		// dep declared a name for the port. Numeric form is always emitted
		// for back-compat with existing parents.
		upperKey := strings.ToUpper(depKey)
		envVars[fmt.Sprintf("TOWNOS_DEP_%s_HOST", upperKey)] = systemd.ContainerName(depRepo, effectiveName, depVersion)
		emitDepPortEnv(envVars, upperKey, depCompiled.Network.External, depCompiled.Network.ExternalNames)
		emitDepPortEnv(envVars, upperKey, depCompiled.Network.Internal, depCompiled.Network.InternalNames)
	}

	return records, envVars, nil
}

// uninstallDependencies removes metadata for every dependency of a parent
// package: DNS records, systemd units, per-package network state files,
// and install records (the hardlinked <version>.yaml, disabled marker,
// and dependencies.json under the dep's nested install dir).
//
// Volume operations are intentionally NOT performed here. Dependency
// volumes live physically nested under the parent's install tree
// (installed/<repo>/<parent>/subpackages/<key>/<version>/<vol>), so a
// cascade-time per-dep rename would conflict with the parent's own
// top-level rename one step later when both try to materialize the same
// uninstalled/<repo>/<parent> subtree. Instead, the top-level uninstall
// handler does a single atomic parent-dir rename (or a single recursive
// purge) that carries every nested dep subvolume with it in one op.
func (s *SystemControllerHandlers) uninstallDependencies(ctx context.Context, repoName, pkgName string) {
	inst := s.Controller.GetInstaller()

	deps, err := inst.LoadDependencies(repoName, pkgName)
	if err != nil {
		slog.Debug(fmt.Sprintf("load dependencies %s/%s: %v", repoName, pkgName, err))
		return
	}
	if len(deps) == 0 {
		return
	}

	for depKey, rec := range deps {
		// Recursively uninstall sub-dependencies first so that deep
		// trees are torn down leaves-up for metadata, matching the
		// Wants/Before=systemd ordering inside a dependency chain.
		s.uninstallDependencies(ctx, rec.Repo, rec.EffectiveName)

		s.unregisterPackageDNS(ctx, rec.Repo, rec.Package, rec.EffectiveName, rec.Version)

		if sd := s.Controller.GetSystemdManager(); sd != nil {
			if err := s.uninstallPackageUnits(ctx, sd, rec.Repo, rec.EffectiveName, rec.Version); err != nil {
				slog.Debug(fmt.Sprintf("uninstall dep %q units: %v", depKey, err))
			}
		}

		s.removePackageNetworkState(rec.Repo, rec.EffectiveName, rec.Version)

		if err := inst.Uninstall(rec.Repo, rec.EffectiveName, rec.Version); err != nil {
			slog.Debug(fmt.Sprintf("uninstall dep %q record: %v", depKey, err))
		}
	}
}

// emitDepPortEnv populates envVars with TOWNOS_DEP_<KEY>_PORT_* entries
// for each container port in ports. The numeric form
// TOWNOS_DEP_<KEY>_PORT_<N> is always emitted for back-compat. When a
// port has a semantic name in names, TOWNOS_DEP_<KEY>_PORT_<NAME>
// (uppercased) is also emitted with the same value, letting parents
// reference the dep's port by role in addition to by number.
func emitDepPortEnv(envVars map[string]string, upperKey string, ports packages.PortMap, names packages.PortNameMap) {
	for _, containerPort := range ports {
		val := strconv.FormatUint(uint64(containerPort), 10)
		envVars[fmt.Sprintf("TOWNOS_DEP_%s_PORT_%d", upperKey, containerPort)] = val
		if name, ok := names[containerPort]; ok && name != "" {
			envVars[fmt.Sprintf("TOWNOS_DEP_%s_PORT_%s", upperKey, strings.ToUpper(name))] = val
		}
	}
}

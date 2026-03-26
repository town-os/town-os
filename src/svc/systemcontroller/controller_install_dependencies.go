package systemcontroller

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strconv"
	"strings"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

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
	deps map[string]packages.InputPackageDependency,
) (map[string]packages.DependencyRecord, map[string]string, error) {
	if len(deps) == 0 {
		return nil, nil, nil
	}

	rr := s.Controller.GetRepositoryRoot()
	inst := s.Controller.GetInstaller()
	records := make(map[string]packages.DependencyRecord, len(deps))
	envVars := map[string]string{}

	for depKey, dep := range deps {
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
			subRecords, _, err := s.installDependencies(ctx, depRepo, effectiveName, depVersion, depCompiled.Dependencies)
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

		// Write network state (UPnP disabled via IsDependency check in
		// writePackageNetworkState).
		if err := s.writePackageNetworkState(depRepo, effectiveName, depVersion, depCompiled); err != nil {
			return nil, nil, fmt.Errorf("dependency %q: write network state: %w", depKey, err)
		}

		// Install and start systemd units. Dependencies share the parent's
		// podman network and have systemd ordering (PartOf/Before parent).
		if sd := s.Controller.GetSystemdManager(); sd != nil {
			cfg := s.packageUnitConfig(depRepo, effectiveName, depVersion, depIP.Description, depCompiled)
			cfg.ParentNetwork = systemd.NetworkName(parentRepoName, parentEffectiveName, parentVersion)
			cfg.ParentUnitName = systemd.UnitName(parentRepoName, parentEffectiveName, parentVersion)
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
		upperKey := strings.ToUpper(depKey)
		envVars[fmt.Sprintf("TOWNOS_DEP_%s_HOST", upperKey)] = systemd.ContainerName(depRepo, effectiveName, depVersion)
		for containerPort := range depCompiled.Network.External {
			envVars[fmt.Sprintf("TOWNOS_DEP_%s_PORT_%d", upperKey, containerPort)] = strconv.FormatUint(uint64(containerPort), 10)
		}
		for containerPort := range depCompiled.Network.Internal {
			envVars[fmt.Sprintf("TOWNOS_DEP_%s_PORT_%d", upperKey, containerPort)] = strconv.FormatUint(uint64(containerPort), 10)
		}
	}

	return records, envVars, nil
}

// uninstallDependencies removes all dependencies for a parent package by
// reading the persisted dependency records. Dependencies are uninstalled in
// reverse order (parent first, then dependencies).
func (s *SystemControllerHandlers) uninstallDependencies(ctx context.Context, repoName, pkgName string, purgeVolumes bool) {
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
		// Recursively uninstall sub-dependencies first.
		s.uninstallDependencies(ctx, rec.Repo, rec.EffectiveName, purgeVolumes)

		// Unregister DNS.
		s.unregisterPackageDNS(ctx, rec.Repo, rec.Package, rec.EffectiveName, rec.Version)

		// Uninstall systemd units.
		if sd := s.Controller.GetSystemdManager(); sd != nil {
			if err := s.uninstallPackageUnits(ctx, sd, rec.Repo, rec.EffectiveName, rec.Version); err != nil {
				slog.Debug(fmt.Sprintf("uninstall dep %q units: %v", depKey, err))
			}
		}

		// Remove network state.
		s.removePackageNetworkState(rec.Repo, rec.EffectiveName, rec.Version)

		// Uninstall package record.
		if err := inst.Uninstall(rec.Repo, rec.EffectiveName, rec.Version); err != nil {
			slog.Debug(fmt.Sprintf("uninstall dep %q record: %v", depKey, err))
		}

		// Handle volumes.
		if purgeVolumes {
			if err := s.purgePackageVolumes(rec.Repo, rec.EffectiveName); err != nil {
				slog.Debug(fmt.Sprintf("purge dep %q volumes: %v", depKey, err))
			}
		} else if st := s.Controller.GetStorage(); st != nil {
			src := fmt.Sprintf("%s/%s/%s", PackagesVolumePrefix, rec.Repo, rec.EffectiveName)
			dst := fmt.Sprintf("%s/%s/%s", UninstalledVolumePrefix, rec.Repo, rec.EffectiveName)
			if err := st.RenameFilesystem(src, dst); err != nil {
				slog.Debug(fmt.Sprintf("preserve dep %q volumes: rename %s -> %s: %v", depKey, src, dst, err))
			}
		}
	}
}

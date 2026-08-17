package systemcontroller

import (
	"context"

	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

// buildDepEnvVarsFromRecords reconstructs TOWNOS_DEP_* environment variables
// from persisted dependency records, returning both the flat env map (used by
// the systemd unit generator) and a structured map[string]TemplateDep (used
// by the file-template renderer so package YAML can reference dep host/ports
// via {{.Dep.key.Host}} / {{index .Dep.key.Ports "sql"}}). During reconcile
// the parent's compiled environment does not include these values (they were
// injected at install time), so both are rebuilt here from the records.
func buildDepEnvVarsFromRecords(
	ctx context.Context,
	depRecs map[string]packages.DependencyRecord,
	rr *packages.RepositoryRoot,
	inst packages.Installer,
	settingsMgr account.SettingsManager,
	extIP, intIP string,
) (map[string]string, map[string]packages.TemplateDep) {
	envVars := map[string]string{}
	depMap := map[string]packages.TemplateDep{}
	tld := reconcileDNSTLD(ctx, settingsMgr)

	for depKey, rec := range depRecs {
		// Load and compile the dependency to discover its ports.
		depIP, err := rr.LoadPackage(rec.Repo, rec.Package, rec.Version)
		if err != nil {
			depIP, err = rr.LoadInstalledPackage(rec.Repo, rec.EffectiveName, rec.Version)
			if err != nil {
				slog.Debug(fmt.Sprintf("buildDepEnvVars: load %s/%s@%s: %v", rec.Repo, rec.Package, rec.Version, err))
				continue
			}
		}

		depResponses, err := inst.GetResponses(rec.Repo, rec.EffectiveName, rec.Version)
		if err != nil {
			slog.Debug(fmt.Sprintf("buildDepEnvVars: responses %s/%s@%s: %v", rec.Repo, rec.EffectiveName, rec.Version, err))
			continue
		}

		depCompiled, err := depIP.CompileWithContext(depResponses, packages.CompileContext{
			ExternalHost: extIP,
			InternalHost: intIP,
			PackageDNS:   rec.EffectiveName + "." + rec.Repo + "." + tld,
		})
		if err != nil {
			slog.Debug(fmt.Sprintf("buildDepEnvVars: compile %s/%s@%s: %v", rec.Repo, rec.EffectiveName, rec.Version, err))
			continue
		}

		containerName := systemd.ContainerName(rec.Repo, rec.EffectiveName, rec.Version)
		upperKey := strings.ToUpper(depKey)
		envVars[fmt.Sprintf("TOWNOS_DEP_%s_HOST", upperKey)] = containerName
		emitDepPortEnv(envVars, upperKey, depCompiled.Network.External, depCompiled.Network.ExternalNames)
		emitDepPortEnv(envVars, upperKey, depCompiled.Network.Internal, depCompiled.Network.InternalNames)
		depMap[depKey] = buildTemplateDepEntry(containerName, depCompiled)
	}

	return envVars, depMap
}

// buildTemplateDepEntry builds a single packages.TemplateDep for a dep
// whose compiled package and container name are known. Ports is populated
// with both numeric container ports (e.g. "5432") and any semantic port
// names (lowercased, e.g. "sql") declared on the dep's network entries.
// Under current wiring the value is always the container-side port; the
// map keeps room for future remapping without API churn. Volumes is
// populated with the in-dep mountpoint of every volume the dep author
// opted in to share (`shareable: true`); non-shareable volumes are
// omitted so parent file templates cannot reach data that was not
// declared as cross-package data.
func buildTemplateDepEntry(containerName string, depCompiled *packages.Package) packages.TemplateDep {
	entry := packages.TemplateDep{
		Host:    containerName,
		Ports:   map[string]string{},
		Volumes: map[string]string{},
	}
	addPorts := func(ports packages.PortMap, names packages.PortNameMap) {
		for _, containerPort := range ports {
			val := strconv.FormatUint(uint64(containerPort), 10)
			entry.Ports[val] = val
			if name, ok := names[containerPort]; ok && name != "" {
				entry.Ports[strings.ToLower(name)] = val
			}
		}
	}
	addPorts(depCompiled.Network.External, depCompiled.Network.ExternalNames)
	addPorts(depCompiled.Network.Internal, depCompiled.Network.InternalNames)
	for volName, vol := range depCompiled.Volumes {
		if vol.Shareable {
			entry.Volumes[volName] = vol.Mountpoint
		}
	}
	return entry
}

// depTemplateResponses turns the flat TOWNOS_DEP_* env map into the response
// namespace the @dep_KEY_*@ markers are written against: strip the TOWNOS_
// prefix and lowercase, so TOWNOS_DEP_DB_HOST becomes dep_db_host.
func depTemplateResponses(depEnvVars map[string]string) packages.Responses {
	responses := make(packages.Responses, len(depEnvVars))
	for k, v := range depEnvVars {
		templateKey := strings.ToLower(strings.TrimPrefix(k, "TOWNOS_"))
		responses[templateKey] = v
	}
	return responses
}

// applyDepTemplatesSlice is applyDepTemplates for an ordered list rather than
// a keyed map — post_install commands, where there is no key to skip and every
// entry is a candidate.
//
// It is called even when depEnvVars is empty, for the same reason the parent's
// environment is: ApplyTemplates is also what collapses `@@` → `@`, and
// compile deliberately leaves that escape intact on any field that still has a
// runtime substitution pass ahead of it. Skipping the empty case would ship
// `@@` through to `sh -c`.
func applyDepTemplatesSlice(cmds []string, depEnvVars map[string]string) {
	responses := depTemplateResponses(depEnvVars)
	for i, v := range cmds {
		if resolved := packages.ApplyTemplates(v, responses); resolved != v {
			cmds[i] = resolved
		}
	}
}

// applyDepTemplates resolves @dep_KEY_host@ and @dep_KEY_port_N@ template
// variables in environment values using the dependency environment variables.
// This mirrors the compile-time template substitution described in the spec:
// TOWNOS_DEP_DB_HOST becomes template key dep_db_host, etc.
func applyDepTemplates(env map[string]string, depEnvVars map[string]string) {
	responses := depTemplateResponses(depEnvVars)

	for k, v := range env {
		// Skip the TOWNOS_DEP_* vars themselves.
		if strings.HasPrefix(k, "TOWNOS_DEP_") {
			continue
		}
		if resolved := packages.ApplyTemplates(v, responses); resolved != v {
			env[k] = resolved
		}
	}
}

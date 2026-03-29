package systemcontroller

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

// buildDepEnvVarsFromRecords reconstructs TOWNOS_DEP_* environment variables
// from persisted dependency records. During reconcile, the parent's compiled
// environment does not include these variables (they were injected at install
// time), so they must be rebuilt from the dependency records.
func buildDepEnvVarsFromRecords(
	depRecs map[string]packages.DependencyRecord,
	rr *packages.RepositoryRoot,
	inst packages.Installer,
	settingsMgr account.SettingsManager,
	extIP, intIP string,
) map[string]string {
	envVars := map[string]string{}
	tld := reconcileDNSTLD(settingsMgr)

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

		upperKey := strings.ToUpper(depKey)
		envVars[fmt.Sprintf("TOWNOS_DEP_%s_HOST", upperKey)] = systemd.ContainerName(rec.Repo, rec.EffectiveName, rec.Version)
		for containerPort := range depCompiled.Network.External {
			envVars[fmt.Sprintf("TOWNOS_DEP_%s_PORT_%d", upperKey, containerPort)] = strconv.FormatUint(uint64(containerPort), 10)
		}
		for containerPort := range depCompiled.Network.Internal {
			envVars[fmt.Sprintf("TOWNOS_DEP_%s_PORT_%d", upperKey, containerPort)] = strconv.FormatUint(uint64(containerPort), 10)
		}
	}

	return envVars
}

// applyDepTemplates resolves @dep_KEY_host@ and @dep_KEY_port_N@ template
// variables in environment values using the dependency environment variables.
// This mirrors the compile-time template substitution described in the spec:
// TOWNOS_DEP_DB_HOST becomes template key dep_db_host, etc.
func applyDepTemplates(env map[string]string, depEnvVars map[string]string) {
	// Build template responses: strip TOWNOS_ prefix and lowercase.
	responses := packages.Responses{}
	for k, v := range depEnvVars {
		templateKey := strings.ToLower(strings.TrimPrefix(k, "TOWNOS_"))
		responses[templateKey] = v
	}

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

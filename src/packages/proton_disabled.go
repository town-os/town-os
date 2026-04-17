//go:build !proton

package packages

import "errors"

// ErrProtonNotEnabled is returned when a package manifest declares a
// `proton:` block in a build that does not include the Proton runner.
// Enable the runner by rebuilding with `-tags proton`
// (or `make PROTON_ENABLED=1 ...`).
var ErrProtonNotEnabled = errors.New("proton support is not built in")

// ProtonEnabled reports whether the Proton/Wine runner support is compiled
// into this build. Controlled by the `proton` build tag.
func ProtonEnabled() bool { return false }

// checkProtonAllowed rejects any package that declares a proton block in a
// build without the `proton` tag. Called from Validate before other
// proton-specific checks so the failure surfaces early with a clear error.
func checkProtonAllowed(i *InputPackage) error {
	if i.Proton != nil {
		return ErrProtonNotEnabled
	}
	return nil
}

// validateProtonCompile is a no-op when proton is disabled: Validate has
// already rejected any manifest with a proton block, so i.Proton is nil by
// the time this runs.
func validateProtonCompile(i *InputPackage) error { return nil }

// compileProton is a no-op when proton is disabled. Returns the command
// unchanged and a nil PackageProton.
func compileProton(i *InputPackage, command []string) ([]string, *PackageProton) {
	return command, nil
}

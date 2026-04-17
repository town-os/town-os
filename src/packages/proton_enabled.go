//go:build proton

package packages

import "fmt"

// ProtonEnabled reports whether the Proton/Wine runner support is compiled
// into this build. Controlled by the `proton` build tag.
func ProtonEnabled() bool { return true }

// checkProtonAllowed is invoked from Validate before any proton-specific
// validation runs. When proton is enabled it is a no-op.
func checkProtonAllowed(i *InputPackage) error { return nil }

// validateProtonCompile is invoked from Compile after Validate to run the
// spec-level checks that depend on other compiled state (e.g. the volume
// map). Returns nil when i.Proton is unset.
func validateProtonCompile(i *InputPackage) error {
	if i.Proton == nil {
		return nil
	}
	if err := ValidateProtonSpec(*i.Proton, i.Volumes); err != nil {
		return fmt.Errorf("proton: %w", err)
	}
	return nil
}

// compileProton derives the container command and compiled PackageProton
// struct from the input spec. Returns the original command and a nil
// PackageProton when i.Proton is unset.
func compileProton(i *InputPackage, command []string) ([]string, *PackageProton) {
	if i.Proton == nil {
		return command, nil
	}
	// Auto-generate command: ["proton", "run", exe, ...args]
	cmd := append([]string{"proton", "run", i.Proton.Exe}, i.Proton.Args...)
	proton := &PackageProton{
		AppImage:     NormalizeImageURL(i.Proton.AppImage),
		AppDirectory: i.Proton.AppDirectory,
		Volume:       i.Proton.Volume,
		Exe:          i.Proton.Exe,
		Args:         i.Proton.Args,
	}
	return cmd, proton
}

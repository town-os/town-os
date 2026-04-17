//go:build !proton

package systemcontroller

import (
	"context"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
)

// resolveProtonImage is a stub when proton is not built in. Package
// validation rejects any manifest with a proton block before this runs, so
// compiled.Proton is guaranteed nil — returning compiled.Image is correct.
func (s *SystemControllerHandlers) resolveProtonImage(compiled *packages.Package) string {
	return compiled.Image
}

// resolveReconcileImage is the reconcile-path counterpart of
// resolveProtonImage. Stubbed when proton is not built in.
func resolveReconcileImage(compiled *packages.Package, _ account.SettingsManager) string {
	return compiled.Image
}

// resolvePreviewProtonImage is stubbed when proton is not built in. Install
// preview validates the input package before this runs, so ip.Proton is nil.
func (s *SystemControllerHandlers) resolvePreviewProtonImage(previewImage string, _ *packages.InputPackage) string {
	return previewImage
}

// seedProtonApp is a no-op when proton is not built in.
func (s *SystemControllerHandlers) seedProtonApp(_ context.Context, _ *packages.Package, _, _, _ string) {
}

// reconcileProtonApp is a no-op when proton is not built in.
func reconcileProtonApp(_ context.Context, _ *packages.Package, _, _, _, _ string) {}

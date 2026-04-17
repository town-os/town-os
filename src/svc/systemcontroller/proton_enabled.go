//go:build proton

package systemcontroller

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
)

// protonImage returns the system-wide Proton runner container image from
// the proton_image setting. Returns "" when the setting manager is unset
// or the setting has not been seeded.
func (s *SystemControllerHandlers) protonImage() string {
	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return ""
	}
	val, err := mgr.Get(account.ProtonImageKey)
	if err != nil {
		return ""
	}
	return val
}

// reconcileProtonImage is the reconcile-path counterpart of protonImage. It
// takes the settings manager directly since reconcile runs off a
// ReconcileConfig rather than the live handler.
func reconcileProtonImage(mgr account.SettingsManager) string {
	if mgr == nil {
		return ""
	}
	val, err := mgr.Get(account.ProtonImageKey)
	if err != nil {
		return ""
	}
	return val
}

// resolveProtonImage returns the image to use for a compiled package. For
// proton packages without an explicit image URL it falls back to the
// configured proton_image setting; otherwise it returns the compiled image.
func (s *SystemControllerHandlers) resolveProtonImage(compiled *packages.Package) string {
	if compiled.Image != "" || compiled.Proton == nil {
		return compiled.Image
	}
	return s.protonImage()
}

// resolveReconcileImage is the reconcile-path counterpart of
// resolveProtonImage.
func resolveReconcileImage(compiled *packages.Package, mgr account.SettingsManager) string {
	if compiled.Image != "" || compiled.Proton == nil {
		return compiled.Image
	}
	return reconcileProtonImage(mgr)
}

// resolvePreviewProtonImage returns the image preview for a package. Called
// from install-preview where no compiled package exists yet — the input
// package's image URL is used directly and the proton_image setting fills
// in for proton-only packages.
func (s *SystemControllerHandlers) resolvePreviewProtonImage(previewImage string, ip *packages.InputPackage) string {
	if previewImage != "" || ip.Proton == nil {
		return previewImage
	}
	return s.protonImage()
}

// seedProtonApp extracts the Windows application files from the proton
// app_image into the designated volume. No-op when the package has no
// proton block.
func (s *SystemControllerHandlers) seedProtonApp(ctx context.Context, compiled *packages.Package, repoName, effectiveName, version string) {
	if compiled.Proton == nil {
		return
	}
	volPath := packageVolumePath(repoName, effectiveName, version, compiled.Proton.Volume)
	if err := s.extractFromContainerImage(ctx, compiled.Proton.AppImage, compiled.Proton.AppDirectory, volPath); err != nil {
		slog.Debug(fmt.Sprintf("proton app-extract %s -> %s: %v", compiled.Proton.AppImage, compiled.Proton.Volume, err))
	}
}

// reconcileProtonApp re-runs proton app extraction during reconcile when the
// target volume is empty. No-op when the package has no proton block.
func reconcileProtonApp(ctx context.Context, compiled *packages.Package, btrfsBase, repoName, effectiveName, version string) {
	if compiled.Proton == nil {
		return
	}
	volPath := packageVolumePath(repoName, effectiveName, version, compiled.Proton.Volume)
	targetPath := fmt.Sprintf("%s/%s", btrfsBase, volPath)
	entries, err := os.ReadDir(targetPath)
	if err != nil || len(entries) > 0 {
		return
	}
	if err := reconcileExtractFromImage(ctx, compiled.Proton.AppImage, compiled.Proton.AppDirectory, targetPath); err != nil {
		slog.Debug(fmt.Sprintf("reconcile proton app-extract %s -> %s: %v", compiled.Proton.AppImage, compiled.Proton.Volume, err))
	}
}

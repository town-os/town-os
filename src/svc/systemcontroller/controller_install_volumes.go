package systemcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"github.com/labstack/echo/v5"
)

// writePackageNetworkState writes the per-package JSON state file consumed by
// the networkcontroller daemon. Dependencies (names containing --dep--)
// always have UPnP disabled to keep their ports local-only.
func (s *SystemControllerHandlers) writePackageNetworkState(repoName, pkgName, version string, compiled *packages.Package) error {
	statePath := s.Controller.GetNetworkStatePath()
	if statePath == "" {
		return nil
	}

	isDep := packages.IsDependency(pkgName)

	state := networkcontroller.PackageNetworkState{
		Repo:    repoName,
		Package: pkgName,
		Version: version,
	}

	// All ports get Forward=true — the NC handles all host port exposure
	// via socat from the host to the package container's private network IP.
	for ext, int_ := range compiled.Network.External {
		state.Ports = append(state.Ports, networkcontroller.PortConfig{
			ExternalPort: ext,
			InternalPort: int_,
			UPnP:         !isDep,
			Forward:      true,
		})
	}

	for intHost, intContainer := range compiled.Network.Internal {
		state.Ports = append(state.Ports, networkcontroller.PortConfig{
			ExternalPort: intHost,
			InternalPort: intContainer,
			UPnP:         false,
			Forward:      true,
		})
	}

	if len(state.Ports) == 0 {
		return nil
	}

	// Sort for deterministic output.
	sort.Slice(state.Ports, func(i, j int) bool {
		return state.Ports[i].ExternalPort < state.Ports[j].ExternalPort
	})

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal network state: %w", err)
	}

	filePath := fmt.Sprintf("%s/%s-%s-%s.json", statePath, repoName, pkgName, version)
	if err := os.MkdirAll(statePath, 0700); err != nil {
		return fmt.Errorf("create network state dir: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("write network state: %w", err)
	}

	return nil
}

// removePackageNetworkState removes the per-package network state file.
func (s *SystemControllerHandlers) removePackageNetworkState(repoName, pkgName, version string) {
	statePath := s.Controller.GetNetworkStatePath()
	if statePath == "" {
		return
	}

	filePath := fmt.Sprintf("%s/%s-%s-%s.json", statePath, repoName, pkgName, version)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		slog.Debug(fmt.Sprintf("remove network state %s: %v", filePath, err))
	}
}

// prepareActiveVersion stops running units for the active version and
// migrates volumes when upgrading.
func (s *SystemControllerHandlers) prepareActiveVersion(ctx context.Context, rr *packages.RepositoryRoot, inst packages.Installer, repoName, parentName, effectiveName, activeVersion, newVersion, importFromVersion string, compiled *packages.Package) error {
	// Stop and remove all systemd units for the currently active version.
	if sd := s.Controller.GetSystemdManager(); sd != nil {
		if err := s.uninstallPackageUnits(ctx, sd, repoName, effectiveName, activeVersion); err != nil {
			return err
		}
	}

	if activeVersion == newVersion {
		// Same version reinstall: remove the install record (but not volumes).
		return inst.Uninstall(repoName, effectiveName, newVersion)
	}

	// Different version (upgrade): move matching volumes.
	if st := s.Controller.GetStorage(); st != nil && importFromVersion == "" {
		oldIP, loadErr := rr.LoadPackage(repoName, parentName, activeVersion)
		if loadErr == nil {
			for volName := range compiled.Volumes {
				if _, exists := oldIP.Volumes[volName]; exists {
					src := packageVolumePath(repoName, effectiveName, activeVersion, volName)
					dst := packageVolumePath(repoName, effectiveName, newVersion, volName)
					if err := st.RenameFilesystem(src, dst); err != nil {
						slog.Debug(fmt.Sprintf("upgrade move volume %s -> %s: %v", src, dst, err))
					}
				}
			}
		} else {
			slog.Debug(fmt.Sprintf("upgrade: load old package %s/%s@%s: %v", repoName, parentName, activeVersion, loadErr))
		}
	}
	return nil
}

// provisionVolumes creates, reuses, or imports storage volumes for the package.
func (s *SystemControllerHandlers) provisionVolumes(repoName, effectiveName, version, importFromVersion string, reuseVolumes bool, compiled *packages.Package) error {
	st := s.Controller.GetStorage()
	if st == nil {
		return nil
	}

	if reuseVolumes {
		src := fmt.Sprintf("%s/%s/%s", UninstalledVolumePrefix, repoName, effectiveName)
		dst := fmt.Sprintf("%s/%s/%s", PackagesVolumePrefix, repoName, effectiveName)
		if err := st.RenameFilesystem(src, dst); err != nil {
			slog.Debug(fmt.Sprintf("reuse volumes: rename %s -> %s: %v", src, dst, err))
		}
	}

	defQuota := s.defaultQuota()
	for volName, vol := range compiled.Volumes {
		quota := vol.Quota
		if quota == 0 {
			quota = defQuota
		}
		fsName := packageVolumePath(repoName, effectiveName, version, volName)

		if importFromVersion != "" {
			srcVol := packageVolumePath(repoName, effectiveName, importFromVersion, volName)
			if err := st.SnapshotFilesystem(srcVol, fsName); err != nil {
				slog.Debug(fmt.Sprintf("import snapshot %s -> %s: %v", srcVol, fsName, err))
				if err := st.CreateFilesystem(storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
					if err := st.ModifyFilesystem(fsName, storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
						return err
					}
				}
			} else {
				if err := st.ModifyFilesystem(fsName, storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
					slog.Debug(fmt.Sprintf("adjust quota on snapshot %s: %v", fsName, err))
				}
			}
		} else {
			if err := st.CreateFilesystem(storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
				if err := st.ModifyFilesystem(fsName, storage.Filesystem{Name: fsName, Quota: quota}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// seedVolumeData populates volumes with auto-archives, proton app extraction,
// git seeds, and git_sources after they are created. For VM packages, volume
// seeding is skipped because storage downloading is not supported for QEMU.
func (s *SystemControllerHandlers) seedVolumeData(ctx context.Context, ip *packages.InputPackage, compiled *packages.Package, repoName, parentName, effectiveName, version string) {
	if compiled.Runtime == packages.RuntimeVM {
		return
	}
	if len(ip.Archives) > 0 {
		for _, archive := range ip.Archives {
			volPath := packageVolumePath(repoName, parentName, version, archive.Volume)
			if err := s.extractFromContainerImage(ctx, archive.Image, archive.Directory, volPath); err != nil {
				slog.Debug(fmt.Sprintf("auto-archive %s -> %s: %v", archive.Image, archive.Volume, err))
			}
		}
	}

	// Proton app extraction: extract from app_image into the designated volume.
	if compiled.Proton != nil {
		volPath := packageVolumePath(repoName, effectiveName, version, compiled.Proton.Volume)
		if err := s.extractFromContainerImage(ctx, compiled.Proton.AppImage, compiled.Proton.AppDirectory, volPath); err != nil {
			slog.Debug(fmt.Sprintf("proton app-extract %s -> %s: %v", compiled.Proton.AppImage, compiled.Proton.Volume, err))
		}
	}

	for volName, vol := range compiled.Volumes {
		if vol.Git == "" {
			continue
		}
		volPath := packageVolumePath(repoName, effectiveName, version, volName)
		targetPath := filepath.Join(s.Controller.GetBtrfsBasePath(), volPath)
		entries, err := os.ReadDir(targetPath)
		if err != nil || len(entries) > 0 {
			continue
		}
		if err := gitCloneIntoPath(ctx, vol.Git, targetPath); err != nil {
			slog.Debug(fmt.Sprintf("git-seed %s -> %s: %v", vol.Git, volName, err))
		}
	}

	if len(ip.GitSources) > 0 {
		cloner := s.Controller.GetGitCloner()
		for _, gs := range ip.GitSources {
			volPath := packageVolumePath(repoName, effectiveName, version, gs.Volume)
			targetDir := filepath.Join(s.Controller.GetBtrfsBasePath(), volPath)
			if err := cloner.Clone(targetDir, gs.URL, gs.Branch); err != nil {
				slog.Debug(fmt.Sprintf("git-source clone %s -> %s: %v", gs.URL, gs.Volume, err))
			}
		}
	}
}

// ensureVMImage downloads and converts a VM image if it is a remote URL and
// does not already exist in the local cache.
func (s *SystemControllerHandlers) ensureVMImage(ctx context.Context, vmImage string) error {
	if !strings.HasPrefix(vmImage, "http://") && !strings.HasPrefix(vmImage, "https://") {
		return nil // local image, nothing to download
	}

	basePath := s.Controller.GetBtrfsBasePath()
	if basePath == "" {
		return nil
	}

	// Ensure vm-images subvolume exists.
	if st := s.Controller.GetStorage(); st != nil {
		if err := st.CreateFilesystem(storage.Filesystem{Name: VMImagesSubvolume}); err != nil {
			slog.Debug(fmt.Sprintf("create vm-images subvolume: %v", err))
		}
	}

	rawPath := resolveVMImagePath(basePath, vmImage)
	if _, err := os.Stat(rawPath); err == nil { //nolint:gosec // G703 -- path from resolveVMImagePath
		return nil // already cached
	}

	dir := filepath.Join(basePath, VMImagesSubvolume)
	if err := os.MkdirAll(dir, 0750); err != nil { //nolint:gosec // G703 -- path from trusted basePath
		return fmt.Errorf("create vm-images dir: %w", err)
	}

	parts := strings.Split(vmImage, "/")
	name := parts[len(parts)-1]
	downloadPath := filepath.Join(dir, name+".download")

	if err := downloadFile(ctx, vmImage, downloadPath); err != nil {
		return err
	}

	if err := convertVMImage(ctx, downloadPath, rawPath); err != nil {
		if rmErr := os.Remove(downloadPath); rmErr != nil { //nolint:gosec // G703 -- path from trusted basePath
			slog.Debug(fmt.Sprintf("remove download file: %v", rmErr))
		}
		return err
	}

	if err := os.Remove(downloadPath); err != nil { //nolint:gosec // G703 -- path from trusted basePath
		slog.Debug(fmt.Sprintf("remove download file: %v", err))
	}

	return nil
}

func (s *SystemControllerHandlers) purgePackageVolumes(repo, name string) error {
	st := s.Controller.GetStorage()
	if st == nil {
		return nil
	}

	// Purge from installed/<repo>/<name>/.
	if err := s.purgeVolumePrefix(st, packagePrefix(repo, name)); err != nil {
		return err
	}

	// Remove the installed/<repo>/<name> parent subvolume itself.
	parentPath := fmt.Sprintf("%s/%s/%s", PackagesVolumePrefix, repo, name)
	if err := st.RemoveFilesystem(parentPath); err != nil {
		slog.Debug(fmt.Sprintf("purge parent volume %s: %v", parentPath, err))
	}

	// Also purge from uninstalled/<repo>/<name>/.
	uninstPrefix := fmt.Sprintf("%s/%s/%s/", UninstalledVolumePrefix, repo, name)
	if err := s.purgeVolumePrefix(st, uninstPrefix); err != nil {
		return err
	}

	uninstParent := fmt.Sprintf("%s/%s/%s", UninstalledVolumePrefix, repo, name)
	if err := st.RemoveFilesystem(uninstParent); err != nil {
		slog.Debug(fmt.Sprintf("purge uninstalled parent volume %s: %v", uninstParent, err))
	}

	return nil
}

func (s *SystemControllerHandlers) purgeVolumePrefix(st storage.Storage, prefix string) error {
	filesystems, err := st.ListFilesystems(prefix)
	if err != nil {
		return err
	}

	// Sort deepest-first so child subvolumes are removed before parents.
	sort.Slice(filesystems, func(i, j int) bool {
		return strings.Count(filesystems[i].Name, "/") > strings.Count(filesystems[j].Name, "/")
	})

	for _, fs := range filesystems {
		if err := st.RemoveFilesystem(fs.Name); err != nil {
			return err
		}
	}

	return nil
}

func (s *SystemControllerHandlers) purgeVolumes(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	unlock := s.lockPackage(req.Repo, req.Name)
	defer unlock()

	if err := s.purgePackageVolumes(req.Repo, req.Name); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

package systemcontroller

import (
	"net/http"

	"gitea.com/town-os/town-os/src/packages"
	"github.com/labstack/echo/v5"
)

// ExportedVolumesResponse is the body of POST /storage/exported-volumes.
type ExportedVolumesResponse struct {
	Volumes []packages.ExportedVolume `json:"volumes"`
}

// compiledPackageLoaderFor returns the loader the exported-volume machinery
// uses to read a producer's compiled volume list.
func (s *SystemControllerHandlers) compiledPackageLoaderFor() compiledPackageLoader {
	return newCompiledPackageLoader(s.Controller.GetRepositoryRoot())
}

// exportedVolumeResolverFor returns the installer, or nil when there is none.
// Returning the interface directly off a nil *InstallManager would produce a
// non-nil interface holding a nil pointer, and every caller's nil check would
// pass while the first method call panicked.
func (s *SystemControllerHandlers) exportedVolumeResolverFor() exportedVolumeResolver {
	inst := s.Controller.GetInstaller()
	if inst == nil {
		return nil
	}
	return inst
}

// exportedVolumes serves the picker behind a `shared_volume` question: every
// volume an installed package currently offers to the rest of the box.
//
// requireAuth rather than requireAdmin, matching the rest of /storage: the
// listing names volumes and their sizes, which any account can already see
// through POST /storage, and picking one still requires installing a package.
func (s *SystemControllerHandlers) exportedVolumes(c *echo.Context) error {
	vols, err := listExportedVolumes(s.exportedVolumeResolverFor(), s.compiledPackageLoaderFor())
	if err != nil {
		return err
	}
	if vols == nil {
		vols = []packages.ExportedVolume{}
	}
	return c.JSON(http.StatusOK, ExportedVolumesResponse{Volumes: vols})
}

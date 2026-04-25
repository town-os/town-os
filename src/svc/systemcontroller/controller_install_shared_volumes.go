package systemcontroller

import (
	"fmt"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

// sharedMountOptions returns the podman -v options string for a shared
// volume mount. Read-only mounts use "ro" without the SELinux relabel; the
// SELinux ":z" suffix flags the volume as shared between containers and
// already implies read-write semantics, so a separate read-write mount uses
// "rw,z" to match the rest of the unit generator.
func sharedMountOptions(readOnly bool) string {
	if readOnly {
		return "ro"
	}
	return "rw,z"
}

// resolveExposeMounts walks parent.Dependencies for each dep's Expose: block
// and returns the parent-side HostVolumeMount entries. depRecs maps each dep
// key to its persisted DependencyRecord; loadDepPkg resolves a record to a
// compiled package so the function can verify the named volume is declared
// `shareable: true` on the dep before emitting a mount. Volumes that are not
// shareable, deps that are missing from the records map, and volume names
// that the dep does not declare all produce errors — silent failures here
// would yield containers running without the bind mounts the parent's YAML
// asked for.
func resolveExposeMounts(
	btrfsBase string,
	parentDeps map[string]packages.InputPackageDependency,
	depRecs map[string]packages.DependencyRecord,
	loadDepPkg func(packages.DependencyRecord) (*packages.Package, error),
) ([]systemd.HostVolumeMount, error) {
	var mounts []systemd.HostVolumeMount
	for depKey, dep := range parentDeps {
		if len(dep.Expose) == 0 {
			continue
		}
		rec, ok := depRecs[depKey]
		if !ok {
			return nil, fmt.Errorf("dependency %q expose: no install record", depKey)
		}
		depPkg, err := loadDepPkg(rec)
		if err != nil {
			return nil, fmt.Errorf("dependency %q expose: load: %w", depKey, err)
		}
		for volName, exp := range dep.Expose {
			vol, ok := depPkg.Volumes[volName]
			if !ok {
				return nil, fmt.Errorf("dependency %q expose: volume %q not declared by %s", depKey, volName, rec.Package)
			}
			if !vol.Shareable {
				return nil, fmt.Errorf("dependency %q expose: volume %q is not marked shareable on %s", depKey, volName, rec.Package)
			}
			hostPath := fmt.Sprintf("%s/%s", btrfsBase, packageVolumePath(rec.Repo, rec.EffectiveName, rec.Version, volName))
			mounts = append(mounts, systemd.HostVolumeMount{
				HostPath:      hostPath,
				ContainerPath: exp.Path,
				Options:       sharedMountOptions(exp.ExposeReadOnly()),
			})
		}
	}
	return mounts, nil
}

// resolveConsumeMounts produces the HostVolumeMount entries for one dep's
// consume: list. siblings provides the persisted records for every sibling
// (including this dep itself, though self-consume is rejected at validation
// time so the lookup is a no-op for the calling dep). loadDepPkg resolves a
// sibling record to its compiled package so we can verify the named volume
// is `shareable: true`.
func resolveConsumeMounts(
	btrfsBase string,
	thisDepKey string,
	consume []packages.InputDepConsume,
	siblings map[string]packages.DependencyRecord,
	loadDepPkg func(packages.DependencyRecord) (*packages.Package, error),
) ([]systemd.HostVolumeMount, error) {
	if len(consume) == 0 {
		return nil, nil
	}
	var mounts []systemd.HostVolumeMount
	for idx, cons := range consume {
		if cons.From == thisDepKey {
			return nil, fmt.Errorf("dependency %q consume[%d]: self-reference rejected", thisDepKey, idx)
		}
		rec, ok := siblings[cons.From]
		if !ok {
			return nil, fmt.Errorf("dependency %q consume[%d]: sibling %q has no install record", thisDepKey, idx, cons.From)
		}
		sibPkg, err := loadDepPkg(rec)
		if err != nil {
			return nil, fmt.Errorf("dependency %q consume[%d]: load %q: %w", thisDepKey, idx, cons.From, err)
		}
		vol, ok := sibPkg.Volumes[cons.Volume]
		if !ok {
			return nil, fmt.Errorf("dependency %q consume[%d]: volume %q not declared by %s", thisDepKey, idx, cons.Volume, rec.Package)
		}
		if !vol.Shareable {
			return nil, fmt.Errorf("dependency %q consume[%d]: volume %q is not marked shareable on %s", thisDepKey, idx, cons.Volume, rec.Package)
		}
		hostPath := fmt.Sprintf("%s/%s", btrfsBase, packageVolumePath(rec.Repo, rec.EffectiveName, rec.Version, cons.Volume))
		mounts = append(mounts, systemd.HostVolumeMount{
			HostPath:      hostPath,
			ContainerPath: cons.Path,
			Options:       sharedMountOptions(cons.ConsumeReadOnly()),
		})
	}
	return mounts, nil
}

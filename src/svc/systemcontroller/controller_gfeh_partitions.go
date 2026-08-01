// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"

	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/i18n"
	"gitea.com/town-os/town-os/src/storage"
	"github.com/labstack/echo/v5"
)

// The /gfeh/partitions/* routes exist because POST /storage/create cannot
// produce a partition: createFilesystem rewrites every submitted name to
// user/<name> unconditionally, so that route can only ever make user volumes.
// Making the prefix caller-supplied would be a breaking change to a public
// route for the benefit of one consumer, so partitions get their own handlers,
// serviced in-process against storage.Storage — which also keeps
// reserved-prefix enforcement, quota policy, and the audit log in one place
// instead of duplicating them inside gfeh.
//
// These four routes are a CONTRACT, not an internal API. gfeh's Rust client
// (crates/gfeh-townos/src/client.rs) parses these exact shapes, and gfeh's
// `make check-townos-sync` verifies them against this tree. The failure mode
// for getting a shape wrong is specifically nasty: every gfeh test stays green
// against its emulator while the real integration returns 422. Change nothing
// here without re-running that check and updating TOWNOS_CONTRACT.md.
//
// UI-facing routes live in controller_gfeh.go. They are deliberately separate
// so this file stays a literal transcription of the contract.

// PartitionRequest is the body of create, modify, and remove.
//
// Name carries NO prefix — it is "photos", not "gfeh/photos". The prefix is an
// artifact of the volume namespace rather than part of a partition's identity,
// so it is applied here and stripped on the way out, in one place, which is
// what keeps a round trip stable. Remove sends quota 0 and ignores it.
type PartitionRequest struct {
	Name  string `json:"name"`
	Quota uint64 `json:"quota"`
}

// partitionVolume is the on-disk subvolume name for a partition.
func partitionVolume(name string) string {
	return GfehVolumePrefix + "/" + name
}

// partitionName is the inverse: the partition identity inside a volume name.
func partitionName(volume string) string {
	return strings.TrimPrefix(volume, GfehVolumePrefix+"/")
}

// applyGfehOwner declares the uid gfehd runs as on a partition's subvolume.
//
// A bind mount passes host ownership straight through, so a subvolume created
// by the root systemcontroller is one the daemon — which runs unprivileged —
// cannot write to. Setting it here rather than chowning after the fact keeps
// the whole ownership decision in the storage layer, which is what the
// contract asks for and what preserves the per-partition qgroup quota that a
// single chowned parent directory would forfeit.
func applyGfehOwner(fs *storage.Filesystem) {
	uid, gid := gfeh.UID, gfeh.GID
	fs.UID = &uid
	fs.GID = &gid
}

// decodePartitionRequest reads and validates the shared request body.
func (s *SystemControllerHandlers) decodePartitionRequest(c *echo.Context) (PartitionRequest, error) {
	req := PartitionRequest{}
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return req, echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return req, echo.NewHTTPError(http.StatusBadRequest, i18n.T(s.getLocale(), i18n.MsgGfehNameRequired))
	}
	// A partition name is one path component. Rejecting the separator here is
	// what stops "../user/something" from addressing a volume outside the
	// object-storage root, and it matches gfehd's own rule (a partition name
	// cannot contain a separator, config.rs).
	if strings.Contains(req.Name, "/") {
		return req, echo.NewHTTPError(http.StatusBadRequest, storage.ErrInvalidName.Error())
	}
	if err := storage.ValidateFilesystemName(partitionVolume(req.Name)); err != nil {
		return req, echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return req, nil
}

// partitionExists reports whether the partition's subvolume is present.
//
// FilesystemNames rather than ListFilesystems: the latter runs `btrfs qgroup
// show` plus a rootid lookup per subvolume, and existence does not need a quota.
func (s *SystemControllerHandlers) partitionExists(name string) (bool, error) {
	st := s.Controller.GetStorage()
	if st == nil {
		return false, echo.NewHTTPError(http.StatusServiceUnavailable, i18n.T(s.getLocale(), i18n.MsgGfehNotConfigured))
	}
	names, err := st.FilesystemNames(GfehVolumePrefix)
	if err != nil {
		return false, err
	}
	return slices.Contains(names, partitionVolume(name)), nil
}

// createGfehPartition handles POST /gfeh/partitions/create (requireAdmin).
//
// Admin-only because provisioning a partition is also creating the root of a
// permission tree; everything below that root is self-service inside gfeh.
// Answers 409 when it already exists, which is what lets gfehd's provisioning
// be a create-or-resize rather than a create — a daemon whose own partition
// already existed on every start but the first would otherwise only ever be
// startable once.
func (s *SystemControllerHandlers) createGfehPartition(c *echo.Context) error {
	req, err := s.decodePartitionRequest(c)
	if err != nil {
		return err
	}

	exists, err := s.partitionExists(req.Name)
	if err != nil {
		return err
	}
	if exists {
		return echo.NewHTTPError(http.StatusConflict, i18n.T(s.getLocale(), i18n.MsgGfehPartitionExists))
	}

	fs := storage.Filesystem{Name: partitionVolume(req.Name), Quota: req.Quota}
	applyGfehOwner(&fs)
	if err := s.Controller.GetStorage().CreateFilesystem(fs); err != nil {
		if errors.Is(err, storage.ErrInvalidName) {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return err
	}

	return c.JSON(http.StatusOK, storage.Filesystem{Name: fs.Name, Quota: req.Quota})
}

// modifyGfehPartition handles POST /gfeh/partitions/modify (requireAdmin).
//
// Quota only. The name in the request is the partition being resized, not a
// rename target: renaming a partition would move the directory out from under
// a running daemon and every name published for it.
func (s *SystemControllerHandlers) modifyGfehPartition(c *echo.Context) error {
	req, err := s.decodePartitionRequest(c)
	if err != nil {
		return err
	}

	exists, err := s.partitionExists(req.Name)
	if err != nil {
		return err
	}
	if !exists {
		return echo.NewHTTPError(http.StatusNotFound, i18n.T(s.getLocale(), i18n.MsgGfehPartitionNotFound))
	}

	volume := partitionVolume(req.Name)
	if err := s.Controller.GetStorage().ModifyFilesystem(volume, storage.Filesystem{Name: volume, Quota: req.Quota}); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, storage.Filesystem{Name: volume, Quota: req.Quota})
}

// removeGfehPartition handles POST /gfeh/partitions/remove (requireAdmin).
//
// This destroys the subvolume and everything in it. The caller is expected to
// have stopped the daemon serving it first; Town OS does that itself when the
// partition's network goes away (see ReconcileGfeh).
func (s *SystemControllerHandlers) removeGfehPartition(c *echo.Context) error {
	req, err := s.decodePartitionRequest(c)
	if err != nil {
		return err
	}

	exists, err := s.partitionExists(req.Name)
	if err != nil {
		return err
	}
	if !exists {
		return echo.NewHTTPError(http.StatusNotFound, i18n.T(s.getLocale(), i18n.MsgGfehPartitionNotFound))
	}

	if err := s.Controller.GetStorage().RemoveFilesystem(partitionVolume(req.Name)); err != nil {
		return err
	}

	c.Response().WriteHeader(http.StatusOK)
	return nil
}

// listGfehPartitions handles POST /gfeh/partitions (requireAuth).
//
// Answers a PLAIN JSON ARRAY, not a paginated PageResult like the other list
// endpoints. That is the contract: gfeh's client deserializes Vec<Filesystem>
// directly, and there is no pagination on either side. Names are returned WITH
// the gfeh/ prefix — Partition::from_volume strips it.
func (s *SystemControllerHandlers) listGfehPartitions(c *echo.Context) error {
	st := s.Controller.GetStorage()
	if st == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, i18n.T(s.getLocale(), i18n.MsgGfehNotConfigured))
	}

	volumes, err := st.ListFilesystems(GfehVolumePrefix)
	if err != nil {
		return err
	}

	// Always a JSON array, never null: an empty body of `null` is a decode
	// error on the Rust side, where the field is Vec<Filesystem>.
	out := make([]storage.Filesystem, 0, len(volumes))
	for _, v := range volumes {
		// The root itself is not a partition. Anything nested deeper than one
		// component is not one either — a partition is exactly gfeh/<name>.
		if v.Name == GfehVolumePrefix || strings.Contains(partitionName(v.Name), "/") {
			continue
		}
		if !strings.HasPrefix(v.Name, GfehVolumePrefix+"/") {
			continue
		}
		out = append(out, storage.Filesystem{Name: v.Name, Quota: v.Quota})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return c.JSON(http.StatusOK, out)
}

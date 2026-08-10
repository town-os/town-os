// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// The archive unpack runs as root with no ownership or permission flags:
//
//	unpackCmd := exec.CommandContext(ctx, "tar", "-xf", "-", "-C", targetPath)
//
// GNU tar extracting as root honours the uid, gid and mode recorded in the
// archive unless told otherwise (--no-same-owner / --no-same-permissions), so
// an uploaded tarball decides who owns the files it lands and whether any of
// them is setuid-root. The target is a btrfs subvolume, and package volumes are
// bind-mounted into package containers -- so a setuid-root binary or a file
// owned by a uid the container runs as is a step from "can upload an archive"
// toward "can escalate inside, or out of, a container".
//
// Separately, validateUnpackedPaths runs AFTER the extraction has completed:
//
//	return validateUnpackedPaths(targetPath)
//
// It reports an escaping symlink, but by then the symlink is on disk, and the
// handler returns the error without removing it. The volume is left carrying
// exactly the thing the check exists to refuse.
//
// Both endpoints are admin-only, so this is defence in depth. It is still the
// difference between an archive being data and an archive being a way to place
// privileged objects on the filesystem.
//
// These tests assert the SECURE behaviour and fail against the current code.

// tarEntry is one member of a crafted archive, including the ownership and mode
// bits makeTarGz deliberately does not expose.
type tarEntry struct {
	name     string
	content  string
	mode     int64
	uid, gid int
	typeflag byte
	linkname string
}

// makeCraftedTarGz builds a tar.gz whose headers say exactly what the caller
// asked, so a test can model an archive an attacker wrote by hand rather than
// one produced by `tar czf`.
func makeCraftedTarGz(t *testing.T, entries []tarEntry) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for _, e := range entries {
		typeflag := e.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     e.mode,
			Uid:      e.uid,
			Gid:      e.gid,
			Typeflag: typeflag,
			Linkname: e.linkname,
		}
		if typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.content))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar write header %s: %v", e.name, err)
		}
		if typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.content)); err != nil {
				t.Fatalf("tar write body %s: %v", e.name, err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return &buf
}

// uploadVolume creates a user subvolume, registers its cleanup, and returns its
// on-disk path. Named from the test so concurrent runs never share one.
func uploadVolume(t *testing.T, c *systemcontroller.SystemdClient, suffix string) (string, string) {
	t.Helper()

	name := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")) + "-" + suffix
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
		t.Fatalf("CreateFilesystem(%q): %v", name, err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), name); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", name, err)
		}
	})
	return name, filepath.Join("/town-os", "user", name)
}

func TestArchiveUploadDoesNotPreserveArchiveOwnership(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTestWithBtrfsBase(t)

	subvol, path := uploadVolume(t, c, "owner")

	// A uid/gid that exists nowhere on the box, so a match can only have come
	// from the archive header.
	const archiveUID, archiveGID = 31337, 31338
	archive := makeCraftedTarGz(t, []tarEntry{
		{name: "owned.txt", content: "x", mode: 0o644, uid: archiveUID, gid: archiveGID},
	})

	if _, err := c.UploadArchive(context.TODO(), subvol, archive, "test.tar.gz", "", ""); err != nil {
		t.Fatalf("UploadArchive: %v", err)
	}

	info, err := os.Stat(filepath.Join(path, "owned.txt"))
	if err != nil {
		t.Fatalf("Stat owned.txt: %v", err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("stat does not carry ownership on this platform")
	}

	if st.Uid == archiveUID {
		t.Errorf("unpacked file is owned by uid %d, taken from the archive header; "+
			"tar needs --no-same-owner so an upload cannot choose who owns what it lands", st.Uid)
	}
	if st.Gid == archiveGID {
		t.Errorf("unpacked file is owned by gid %d, taken from the archive header", st.Gid)
	}
}

func TestArchiveUploadDoesNotPreserveSetuidBits(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTestWithBtrfsBase(t)

	subvol, path := uploadVolume(t, c, "setuid")

	// 04755: setuid, owner-executable. Extracted as root without
	// --no-same-permissions, this lands as a setuid-root file.
	archive := makeCraftedTarGz(t, []tarEntry{
		{name: "escalate", content: "#!/bin/sh\nid\n", mode: 0o4755},
	})

	if _, err := c.UploadArchive(context.TODO(), subvol, archive, "test.tar.gz", "", ""); err != nil {
		t.Fatalf("UploadArchive: %v", err)
	}

	info, err := os.Stat(filepath.Join(path, "escalate"))
	if err != nil {
		t.Fatalf("Stat escalate: %v", err)
	}

	if info.Mode()&os.ModeSetuid != 0 {
		t.Errorf("unpacked file kept its setuid bit (mode %v); package volumes are bind-mounted into containers, "+
			"so tar needs --no-same-permissions", info.Mode())
	}
	if info.Mode()&os.ModeSetgid != 0 {
		t.Errorf("unpacked file kept its setgid bit (mode %v)", info.Mode())
	}
}

// validateUnpackedPaths correctly reports an escaping symlink -- but it runs
// after `tar` has finished, and the handler returns its error without undoing
// the extraction. So the rejected archive's symlink is still in the volume,
// which is bind-mounted into the package's container.
func TestArchiveUploadLeavesNoEscapingSymlinkBehind(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTestWithBtrfsBase(t)

	subvol, path := uploadVolume(t, c, "symlink")

	archive := makeCraftedTarGz(t, []tarEntry{
		{name: "escape", typeflag: tar.TypeSymlink, linkname: "/etc", mode: 0o777},
	})

	// The upload is expected to fail; what matters is the state it leaves.
	if _, err := c.UploadArchive(context.TODO(), subvol, archive, "test.tar.gz", "", ""); err == nil {
		t.Error("UploadArchive accepted an archive whose symlink escapes the volume")
	}

	linkPath := filepath.Join(path, "escape")
	target, err := os.Readlink(linkPath)
	if err == nil {
		t.Errorf("a rejected upload left an escaping symlink on disk: %s -> %s; "+
			"validateUnpackedPaths runs after extraction and the handler does not roll back", linkPath, target)
	} else if !os.IsNotExist(err) {
		t.Fatalf("Readlink %s: %v", linkPath, err)
	}
}

// The counterpart: an ordinary archive must still unpack, with contents intact
// and readable, so none of the above can be satisfied by refusing uploads.
func TestArchiveUploadOrdinaryArchiveStillWorks(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTestWithBtrfsBase(t)

	subvol, path := uploadVolume(t, c, "ok")

	archive := makeCraftedTarGz(t, []tarEntry{
		{name: "index.html", content: "<h1>hi</h1>", mode: 0o644},
		{name: "run.sh", content: "#!/bin/sh\n", mode: 0o755},
	})

	if _, err := c.UploadArchive(context.TODO(), subvol, archive, "test.tar.gz", "", ""); err != nil {
		t.Fatalf("UploadArchive: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(path, "index.html"))
	if err != nil {
		t.Fatalf("ReadFile index.html: %v", err)
	}
	if string(got) != "<h1>hi</h1>" {
		t.Fatalf("index.html = %q, want %q", string(got), "<h1>hi</h1>")
	}

	info, err := os.Stat(filepath.Join(path, "run.sh"))
	if err != nil {
		t.Fatalf("Stat run.sh: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("run.sh lost its executable bit (mode %v); dropping setuid must not drop ordinary modes", info.Mode())
	}
}

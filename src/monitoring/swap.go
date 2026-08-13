package monitoring

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Reason codes explaining why a box has no swapfile. These are CODES, not
// sentences: the UI renders them through its own catalogs, so the wording lives
// with the other ~50 locales instead of being pinned to English here.
const (
	// SwapReasonMultiDevice — the pool spans more than one block device.
	SwapReasonMultiDevice = "multi_device"
	// SwapReasonDataProfile — the pool's data is not a single "single"
	// profile (RAID, or two profiles at once mid-convert).
	SwapReasonDataProfile = "data_profile"
	// SwapReasonProbeFailed — the layout could not be determined, so no
	// claim is made either way.
	SwapReasonProbeFailed = "probe_failed"
)

// swapFileName is where the image puts the swapfile inside the pool. It is
// fixed by scripts/swapfile.sh in the install repo (a "swap" subvolume holding
// a "swapfile"); the two must agree or this reports usage for a file nothing
// creates.
const swapFileName = "swapfile"

// swapSubvolume is the dedicated subvolume the swapfile lives in — dedicated
// because btrfs refuses to snapshot a subvolume containing an active swapfile,
// and that restriction should land on a subvolume nothing snapshots rather than
// on the pool root.
const swapSubvolume = "swap"

// SwapCapability answers "can this box have swap, does it, and how much".
//
// The distinction that makes this worth reporting at all: on a multi-disk Town
// OS the answer is a permanent NO, and nothing about the running system makes
// that visible. btrfs will only swap on a file whose extents it can pin to one
// device — btrfs(5) requires a single-device filesystem with a single data
// profile — and ttyforce builds every multi-disk pool as a multi-device btrfs
// (raid1 at two disks, raid5 at three or more). So a user with three disks has
// no swap, cannot get swap, and would otherwise have no way to learn why.
type SwapCapability struct {
	// Supported is whether a swapfile can exist on this pool at all.
	Supported bool `json:"supported"`
	// Reason is a machine-readable code, set only when Supported is false.
	Reason string `json:"reason,omitempty"`
	// Devices is how many block devices back the pool.
	Devices int `json:"devices"`
	// DataProfiles are the pool's distinct data block-group profiles.
	DataProfiles []string `json:"data_profiles,omitempty"`
	// Path is where the swapfile lives, or would.
	Path string `json:"path,omitempty"`
	// Active reports whether the kernel is currently swapping to Path.
	// Supported without Active is normal and transient: the unit that
	// creates the file runs at boot and the file is made once.
	Active bool `json:"active"`
	// SizeBytes and UsedBytes are meaningful only when Active.
	SizeBytes uint64 `json:"size_bytes,omitempty"`
	UsedBytes uint64 `json:"used_bytes,omitempty"`
}

// ProbeSwapCapability determines the STATIC half of the answer — the part fixed
// by how the pool was formatted. It shells out (via BtrfsDataProfiles) and so
// is meant to be called ONCE at startup and cached, not per request: the UI
// polls the status endpoint continuously, and a `btrfs filesystem df` per poll
// would be a subprocess every few seconds for a value that cannot change
// without re-formatting the pool.
//
// devices is the already-discovered device list (see BtrfsDevices), passed in
// rather than re-probed so startup does the sysfs walk once.
func ProbeSwapCapability(mountpoint string, devices []string) SwapCapability {
	// Only probe the profile when the device count has not already settled
	// it — on a multi-device pool the answer is no regardless of profile, and
	// there is no reason to spend a subprocess confirming it.
	var profiles []string
	var err error
	if len(devices) == 1 {
		profiles, err = BtrfsDataProfiles(mountpoint)
	}
	return swapCapabilityFrom(mountpoint, devices, profiles, err)
}

// swapCapabilityFrom is the decision itself, split out from the probing so
// every branch — including the supported one — is reachable in a test without
// root and without a real single-device btrfs to point at.
func swapCapabilityFrom(mountpoint string, devices, profiles []string, profileErr error) SwapCapability {
	sc := SwapCapability{
		Devices: len(devices),
		Path:    filepath.Join(mountpoint, swapSubvolume, swapFileName),
	}

	if len(devices) == 0 {
		sc.Reason = SwapReasonProbeFailed
		return sc
	}
	if len(devices) > 1 {
		sc.Reason = SwapReasonMultiDevice
		return sc
	}
	if profileErr != nil {
		sc.Reason = SwapReasonProbeFailed
		return sc
	}
	sc.DataProfiles = profiles

	// Exactly one profile, and it must be "single". Both halves matter: a
	// filesystem mid-`balance -dconvert` carries two data profiles at once,
	// and "the first one happens to be single" is not the same as "all data
	// is single".
	if len(profiles) != 1 || !strings.EqualFold(profiles[0], "single") {
		sc.Reason = SwapReasonDataProfile
		return sc
	}

	sc.Supported = true
	return sc
}

// SwapUsage reports whether the kernel is swapping to path right now, and how
// much of it is in use. This is the DYNAMIC half — it changes constantly, so it
// is read per request. It is a single small procfs read, no subprocess.
func SwapUsage(path string) (active bool, sizeBytes, usedBytes uint64, err error) {
	data, err := os.ReadFile("/proc/swaps")
	if err != nil {
		return false, 0, 0, fmt.Errorf("monitoring: read /proc/swaps: %w", err)
	}
	active, sizeBytes, usedBytes = parseProcSwaps(data, path)
	return active, sizeBytes, usedBytes, nil
}

// parseProcSwaps finds path in /proc/swaps content. Lines look like:
//
//	Filename                                Type            Size    Used    Priority
//	/town-os/swap/swapfile                  file            2097148 0       -2
//
// Size and Used are in KiB, so both are scaled to bytes here — a caller
// comparing a raw value against a byte count would be off by 1024x.
//
// Pure parser so it is testable without touching procfs.
func parseProcSwaps(out []byte, path string) (active bool, sizeBytes, usedBytes uint64) {
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		// The kernel octal-escapes whitespace in this column; the swapfile
		// path this looks for has none, so a literal compare is enough.
		if fields[0] != path {
			continue
		}
		sizeKiB, err := strconv.ParseUint(fields[2], 10, 64)
		if err != nil {
			return true, 0, 0
		}
		usedKiB, err := strconv.ParseUint(fields[3], 10, 64)
		if err != nil {
			return true, sizeKiB * 1024, 0
		}
		return true, sizeKiB * 1024, usedKiB * 1024
	}
	return false, 0, 0
}

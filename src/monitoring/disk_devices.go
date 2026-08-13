package monitoring

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// btrfsShowTimeout caps the `btrfs filesystem show` fallback and the
// `btrfs filesystem df` probe below.
const btrfsShowTimeout = 10 * time.Second

// BtrfsDevices returns the kernel device basenames (e.g., "sda3",
// "nvme0n1p3", "dm-0") of every block device backing the btrfs filesystem
// mounted at mountpoint. The names match the `device` label that
// node_exporter emits for `node_disk_*` metrics from `/proc/diskstats`,
// so callers can build PromQL queries like
// `sum(rate(node_disk_read_bytes_total{device=~"sda3|nvme0n1p3"}[5m]))`.
//
// The result is sorted for determinism. The primary path reads
// /sys/fs/btrfs/<uuid>/devices, which is the only place the kernel
// exposes the full device list for a multi-device btrfs (/proc/mounts
// reports only one device per mount). On any sysfs failure the function
// falls back to parsing `btrfs filesystem show --raw <mountpoint>`.
func BtrfsDevices(mountpoint string) ([]string, error) {
	if mountpoint == "" {
		return nil, errors.New("monitoring: empty mountpoint")
	}

	devices, sysErr := btrfsDevicesFromSysfs(mountpoint)
	if sysErr == nil && len(devices) > 0 {
		return devices, nil
	}

	devices, cliErr := btrfsDevicesFromCLI(mountpoint)
	if cliErr == nil && len(devices) > 0 {
		return devices, nil
	}

	return nil, fmt.Errorf("monitoring: btrfs devices for %s: sysfs=%w cli=%w", mountpoint, sysErr, cliErr)
}

// btrfsDevicesFromSysfs resolves the btrfs UUID for mountpoint by
// matching the mount's st_dev against /sys/fs/btrfs/<uuid>/devices/<name>/dev,
// then returns every device basename under that UUID.
func btrfsDevicesFromSysfs(mountpoint string) ([]string, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(mountpoint, &st); err != nil {
		return nil, fmt.Errorf("stat %s: %w", mountpoint, err)
	}
	wantMajor, wantMinor := devMajorMinor(st.Dev)

	uuids, err := os.ReadDir("/sys/fs/btrfs")
	if err != nil {
		return nil, fmt.Errorf("read /sys/fs/btrfs: %w", err)
	}
	for _, u := range uuids {
		if !u.IsDir() {
			continue
		}
		devicesDir := filepath.Join("/sys/fs/btrfs", u.Name(), "devices")
		entries, err := os.ReadDir(devicesDir)
		if err != nil {
			continue
		}
		matched := false
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			devFile := filepath.Join(devicesDir, e.Name(), "dev")
			//nolint:gosec // G304 -- devFile is constructed from sysfs entries under /sys/fs/btrfs, not user input
			data, err := os.ReadFile(devFile)
			if err != nil {
				continue
			}
			maj, mnr, ok := parseMajorMinor(strings.TrimSpace(string(data)))
			if !ok {
				continue
			}
			if maj == wantMajor && mnr == wantMinor {
				matched = true
			}
			names = append(names, e.Name())
		}
		if matched && len(names) > 0 {
			sort.Strings(names)
			return names, nil
		}
	}
	return nil, fmt.Errorf("no btrfs filesystem in /sys/fs/btrfs matched %s", mountpoint)
}

// btrfsDevicesFromCLI shells out to `btrfs filesystem show --raw <mountpoint>`
// and parses the device paths from its output. Used only as a fallback
// when sysfs parsing fails.
func btrfsDevicesFromCLI(mountpoint string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), btrfsShowTimeout)
	defer cancel()
	//nolint:gosec // G204 -- mountpoint comes from the systemcontroller's -btrfs flag, not user input
	out, err := exec.CommandContext(ctx, "btrfs", "filesystem", "show", "--raw", mountpoint).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("btrfs filesystem show: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return parseBtrfsShowOutput(out), nil
}

// parseBtrfsShowOutput extracts device basenames from `btrfs filesystem show`
// output. Each device line looks like:
//
//	devid    1 size 1000000000 used 100000000 path /dev/sda3
//
// The function returns just the basename of each `path` value. Pure
// parser so it's testable without a real btrfs.
func parseBtrfsShowOutput(out []byte) []string {
	var devices []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		for i := range len(fields) - 1 {
			if fields[i] == "path" {
				devices = append(devices, filepath.Base(fields[i+1]))
				break
			}
		}
	}
	sort.Strings(devices)
	return devices
}

// BtrfsDataProfiles returns every DISTINCT data block-group profile in use on
// the btrfs filesystem mounted at mountpoint — "single", "RAID1", "RAID5" and
// so on — sorted for determinism.
//
// It returns a slice rather than one string because a filesystem caught
// part-way through a `btrfs balance -dconvert` genuinely carries two data
// profiles at once, and a caller that assumed one would silently read only the
// first. Anything that cares about swap must treat that as "not a single data
// profile", which is only expressible if the plural case survives the API.
//
// There is no sysfs equivalent to read the way BtrfsDevices has one: the
// allocation profile per block-group type is only reported by the CLI.
func BtrfsDataProfiles(mountpoint string) ([]string, error) {
	if mountpoint == "" {
		return nil, errors.New("monitoring: empty mountpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), btrfsShowTimeout)
	defer cancel()
	//nolint:gosec // G204 -- mountpoint comes from the systemcontroller's -btrfs flag, not user input
	out, err := exec.CommandContext(ctx, "btrfs", "filesystem", "df", mountpoint).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("monitoring: btrfs filesystem df %s: %w (%s)", mountpoint, err, strings.TrimSpace(string(out)))
	}
	profiles := parseBtrfsDataProfiles(out)
	if len(profiles) == 0 {
		return nil, fmt.Errorf("monitoring: no data block groups reported for %s", mountpoint)
	}
	return profiles, nil
}

// parseBtrfsDataProfiles extracts the distinct data profiles from
// `btrfs filesystem df` output, whose lines look like:
//
//	Data, single: total=1.15TiB, used=1.13TiB
//	Metadata, RAID1: total=1.00GiB, used=25.00MiB
//
// Only the data rows are of interest, and "Data+Metadata" counts as one of
// them: a filesystem made with mixed block groups (which mkfs.btrfs still does
// for small devices) reports data that way, and its profile governs data
// extents just the same.
//
// Pure parser so it is testable without a real btrfs.
func parseBtrfsDataProfiles(out []byte) []string {
	seen := map[string]struct{}{}
	var profiles []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		kind, rest, ok := strings.Cut(scanner.Text(), ",")
		if !ok {
			continue
		}
		kind = strings.TrimSpace(kind)
		if kind != "Data" && kind != "Data+Metadata" {
			continue
		}
		profile, _, ok := strings.Cut(rest, ":")
		if !ok {
			continue
		}
		profile = strings.TrimSpace(profile)
		if profile == "" {
			continue
		}
		if _, dup := seen[profile]; dup {
			continue
		}
		seen[profile] = struct{}{}
		profiles = append(profiles, profile)
	}
	sort.Strings(profiles)
	return profiles
}

// parseMajorMinor parses a "major:minor" string as written in the
// /sys/fs/btrfs/<uuid>/devices/<name>/dev sysfs file.
func parseMajorMinor(s string) (uint64, uint64, bool) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	maj, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return 0, 0, false
	}
	mnr, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return 0, 0, false
	}
	return maj, mnr, true
}

// devMajorMinor extracts the major and minor numbers from a Linux dev_t.
// The kernel encoding (per <linux/kdev_t.h>) puts bits 8..19 + 32..63 in
// the major and bits 0..7 + 20..31 in the minor.
func devMajorMinor(dev uint64) (uint64, uint64) {
	maj := (dev >> 8) & 0xfff
	maj |= (dev >> 32) & 0xfffff000
	mnr := dev & 0xff
	mnr |= (dev >> 12) & 0xffffff00
	return maj, mnr
}

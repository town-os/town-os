// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/monitoring"
)

// TestBtrfsDevicesOnTownOSMount runs inside the privileged integration
// test container, which mounts a loopback btrfs filesystem at /town-os.
// It verifies that BtrfsDevices reports a non-empty list of kernel
// device basenames for that mount, and that each entry looks like a
// real `/proc/diskstats` device name (no slashes, no /dev/ prefix).
func TestBtrfsDevicesOnTownOSMount(t *testing.T) {
	if _, err := os.Stat("/town-os"); err != nil {
		t.Skipf("/town-os not mounted in this environment: %v", err)
	}

	devices, err := monitoring.BtrfsDevices("/town-os")
	if err != nil {
		t.Fatalf("BtrfsDevices(/town-os): %v", err)
	}
	if len(devices) == 0 {
		t.Fatal("expected at least one btrfs device backing /town-os")
	}
	for _, d := range devices {
		if d == "" {
			t.Errorf("empty device basename in result %v", devices)
		}
		if strings.ContainsAny(d, "/ ") {
			t.Errorf("device name %q should be a basename, not a path", d)
		}
	}
}

// TestWriteGrafanaProvisioningFilesEmbedsDiskRegex verifies that the
// dashboard JSON emitted by WriteGrafanaProvisioningFiles contains the
// device regex built from the supplied disk device list. The Disk I/O
// panel in the running Grafana iframe relies on this substitution to
// produce non-empty Read/Write series.
func TestWriteGrafanaProvisioningFilesEmbedsDiskRegex(t *testing.T) {
	t.Parallel()

	btrfsBase := t.TempDir()
	devices := []string{"sda3", "nvme0n1p3"}

	if err := monitoring.WriteGrafanaProvisioningFiles(btrfsBase, devices); err != nil {
		t.Fatalf("WriteGrafanaProvisioningFiles: %v", err)
	}

	dashFile := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning", "dashboard-json", "town-os-overview.json")
	data, err := os.ReadFile(dashFile) //nolint:gosec // file is under t.TempDir()
	if err != nil {
		t.Fatalf("read dashboard JSON: %v", err)
	}
	content := string(data)

	wantSubstrings := []string{
		`sum(rate(node_disk_read_bytes_total{device=~\"sda3|nvme0n1p3\"}[$__rate_interval]))`,
		`sum(rate(node_disk_written_bytes_total{device=~\"sda3|nvme0n1p3\"}[$__rate_interval]))`,
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(content, s) {
			t.Errorf("dashboard JSON missing %q", s)
		}
	}
}

package monitoring

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDiskDeviceRegex(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"empty", nil, NoBtrfsDevicesSentinel},
		{"empty slice", []string{}, NoBtrfsDevicesSentinel},
		{"single", []string{"sda3"}, "sda3"},
		{"multi", []string{"nvme0n1p3", "sdb"}, "nvme0n1p3|sdb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DiskDeviceRegex(tc.in); got != tc.want {
				t.Fatalf("DiskDeviceRegex = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTownOSOverviewDashboardWithDevices(t *testing.T) {
	t.Parallel()
	out := TownOSOverviewDashboard([]string{"sda3", "nvme0n1p3"})

	if !json.Valid([]byte(out)) {
		t.Fatalf("dashboard is not valid JSON:\n%s", out)
	}

	wantSubstrings := []string{
		`"uid": "town-os-overview"`,
		`"title": "Disk I/O (/town-os)"`,
		`sum(rate(node_disk_read_bytes_total{device=~\"sda3|nvme0n1p3\"}[$__rate_interval]))`,
		`sum(rate(node_disk_written_bytes_total{device=~\"sda3|nvme0n1p3\"}[$__rate_interval]))`,
		`"legendFormat": "Read"`,
		`"legendFormat": "Write"`,
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(out, s) {
			t.Errorf("dashboard JSON missing %q", s)
		}
	}
}

func TestTownOSOverviewDashboardEmpty(t *testing.T) {
	t.Parallel()
	out := TownOSOverviewDashboard(nil)

	if !json.Valid([]byte(out)) {
		t.Fatalf("dashboard is not valid JSON:\n%s", out)
	}
	if !strings.Contains(out, NoBtrfsDevicesSentinel) {
		t.Errorf("expected sentinel %q in dashboard", NoBtrfsDevicesSentinel)
	}
}

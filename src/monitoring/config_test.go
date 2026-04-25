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
		`"refresh": "30s"`,
		// Network panel must keep only interfaces whose operstate is up.
		`and on (device) (node_network_up == 1)`,
		// Compact list legend keeps device labels on-screen at 1080p.
		`"displayMode": "list"`,
	}
	if strings.Contains(out, `"displayMode": "table"`) {
		t.Errorf("dashboard JSON should not contain legacy table-mode legend")
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

// TestGrafanaDatasourceYAMLPinsUID asserts the provisioning YAML pins the
// datasource uid that dashboard panel targets refer to. Without this pin,
// Grafana assigns a random uid on first provision and panel refs that
// hardcode the expected uid break.
func TestGrafanaDatasourceYAMLPinsUID(t *testing.T) {
	t.Parallel()
	yaml := GrafanaDatasourceYAML("host.containers.internal")
	if !strings.Contains(yaml, "uid: "+GrafanaDatasourceUID) {
		t.Errorf("datasource YAML missing pinned uid %q:\n%s", GrafanaDatasourceUID, yaml)
	}
}

// TestTownOSOverviewDashboardUsesObjectDatasourceRefs walks every panel
// target in the rendered dashboard and asserts it references the
// datasource with the object form {"type":"prometheus","uid":...} and
// the pinned uid. Grafana 13+ cannot resolve the legacy string form
// ("datasource": "Prometheus") in panel targets — panels render "No
// data" silently — so this pins the shape that actually works.
func TestTownOSOverviewDashboardUsesObjectDatasourceRefs(t *testing.T) {
	t.Parallel()
	out := TownOSOverviewDashboard([]string{"sda3"})

	if strings.Contains(out, `"datasource": "Prometheus"`) {
		t.Fatalf("dashboard still contains legacy string-form datasource ref; Grafana 13 cannot resolve it")
	}

	var dash struct {
		Panels []struct {
			Targets []struct {
				Datasource *struct {
					Type string `json:"type"`
					UID  string `json:"uid"`
				} `json:"datasource"`
			} `json:"targets"`
		} `json:"panels"`
	}
	if err := json.Unmarshal([]byte(out), &dash); err != nil {
		t.Fatalf("unmarshal dashboard: %v", err)
	}
	if len(dash.Panels) == 0 {
		t.Fatal("dashboard has no panels")
	}
	for i, p := range dash.Panels {
		if len(p.Targets) == 0 {
			t.Errorf("panel %d has no targets", i)
			continue
		}
		for j, tgt := range p.Targets {
			if tgt.Datasource == nil {
				t.Errorf("panel %d target %d has no datasource object", i, j)
				continue
			}
			if tgt.Datasource.Type != "prometheus" {
				t.Errorf("panel %d target %d datasource.type = %q, want prometheus", i, j, tgt.Datasource.Type)
			}
			if tgt.Datasource.UID != GrafanaDatasourceUID {
				t.Errorf("panel %d target %d datasource.uid = %q, want %q", i, j, tgt.Datasource.UID, GrafanaDatasourceUID)
			}
		}
	}
}

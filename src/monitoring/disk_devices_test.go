package monitoring

import (
	"reflect"
	"testing"
)

func TestParseBtrfsShowOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "single device",
			in: `Label: 'town-os'  uuid: 12345678-1234-1234-1234-123456789012
	Total devices 1 FS bytes used 1073741824
	devid    1 size 1099511627776 used 2147483648 path /dev/sda3
`,
			want: []string{"sda3"},
		},
		{
			name: "multi device raid1",
			in: `Label: 'town-os'  uuid: aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa
	Total devices 2 FS bytes used 1073741824
	devid    1 size 1099511627776 used 2147483648 path /dev/nvme0n1p3
	devid    2 size 1099511627776 used 2147483648 path /dev/sdb
`,
			want: []string{"nvme0n1p3", "sdb"},
		},
		{
			name: "empty output",
			in:   "",
			want: nil,
		},
		{
			name: "no path lines",
			in:   "Label: 'town-os'  uuid: x\n\tTotal devices 0\n",
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parseBtrfsShowOutput([]byte(tc.in))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseBtrfsShowOutput = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseMajorMinor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in        string
		maj, min  uint64
		ok        bool
	}{
		{"8:3", 8, 3, true},
		{"259:0", 259, 0, true},
		{"0:0", 0, 0, true},
		{"bogus", 0, 0, false},
		{"8", 0, 0, false},
		{":3", 0, 0, false},
		{"8:bogus", 0, 0, false},
	}
	for _, tc := range cases {
		maj, mnr, ok := parseMajorMinor(tc.in)
		if ok != tc.ok || maj != tc.maj || mnr != tc.min {
			t.Errorf("parseMajorMinor(%q) = (%d, %d, %v), want (%d, %d, %v)", tc.in, maj, mnr, ok, tc.maj, tc.min, tc.ok)
		}
	}
}

func TestBtrfsDevicesEmptyMountpoint(t *testing.T) {
	t.Parallel()
	if _, err := BtrfsDevices(""); err == nil {
		t.Fatal("expected error for empty mountpoint")
	}
}

func TestParseBtrfsDataProfiles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "single device, single profile",
			in: `Data, single: total=1.15TiB, used=1.13TiB
System, single: total=32.00MiB, used=144.00KiB
Metadata, single: total=12.00GiB, used=6.45GiB
GlobalReserve, single: total=512.00MiB, used=0.00B
`,
			want: []string{"single"},
		},
		{
			name: "raid5 data, raid1c3 metadata",
			in: `Data, RAID5: total=150.00GiB, used=1.80GiB
System, RAID1C3: total=32.00MiB, used=16.00KiB
Metadata, RAID1C3: total=1.00GiB, used=25.00MiB
GlobalReserve, single: total=512.00MiB, used=0.00B
`,
			// Only the Data row counts: metadata and GlobalReserve profiles
			// are irrelevant to where a swapfile's extents land, and the
			// GlobalReserve row is "single" on a RAID filesystem, so reading
			// any row but Data would call this swappable.
			want: []string{"RAID5"},
		},
		{
			name: "mid-convert filesystem carrying two data profiles",
			in: `Data, single: total=10.00GiB, used=9.00GiB
Data, RAID1: total=20.00GiB, used=1.00GiB
Metadata, RAID1: total=1.00GiB, used=25.00MiB
`,
			want: []string{"RAID1", "single"},
		},
		{
			name: "mixed block groups report Data+Metadata",
			in: `Data+Metadata, single: total=8.00MiB, used=1.00MiB
System, single: total=4.00MiB, used=16.00KiB
`,
			want: []string{"single"},
		},
		{
			name: "repeated identical profile is reported once",
			in: `Data, single: total=10.00GiB, used=9.00GiB
Data, single: total=20.00GiB, used=1.00GiB
`,
			want: []string{"single"},
		},
		{
			name: "empty output",
			in:   "",
			want: nil,
		},
		{
			name: "no data rows",
			in:   "Metadata, RAID1: total=1.00GiB, used=25.00MiB\n",
			want: nil,
		},
		{
			name: "malformed rows are ignored",
			in:   "Data single total=1\nData,\ngarbage\n",
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parseBtrfsDataProfiles([]byte(tc.in))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseBtrfsDataProfiles = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBtrfsDataProfilesEmptyMountpoint(t *testing.T) {
	t.Parallel()
	if _, err := BtrfsDataProfiles(""); err == nil {
		t.Fatal("expected an error for an empty mountpoint")
	}
}

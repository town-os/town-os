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

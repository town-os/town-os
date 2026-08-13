package monitoring

import (
	"errors"
	"reflect"
	"testing"
)

func TestSwapCapabilityFrom(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		devices       []string
		profiles      []string
		profileErr    error
		wantSupported bool
		wantReason    string
	}{
		{
			name:          "single device, single profile",
			devices:       []string{"sda1"},
			profiles:      []string{"single"},
			wantSupported: true,
		},
		{
			name:       "two devices is raid1 and can never swap",
			devices:    []string{"sda1", "sdb1"},
			profiles:   []string{"RAID1"},
			wantReason: SwapReasonMultiDevice,
		},
		{
			name:       "four devices, the dev VM default",
			devices:    []string{"sda1", "sdb1", "sdc1", "sdd1"},
			wantReason: SwapReasonMultiDevice,
		},
		{
			name:       "one device but a RAID data profile",
			devices:    []string{"sda1"},
			profiles:   []string{"RAID1"},
			wantReason: SwapReasonDataProfile,
		},
		{
			name: "one device carrying two data profiles mid-convert",
			// The single/RAID1 pair is exactly what a filesystem looks
			// like part-way through `balance -dconvert`. Reading only the
			// first profile would call this swappable.
			devices:    []string{"sda1"},
			profiles:   []string{"RAID1", "single"},
			wantReason: SwapReasonDataProfile,
		},
		{
			name:          "profile case is not significant",
			devices:       []string{"sda1"},
			profiles:      []string{"Single"},
			wantSupported: true,
		},
		{
			name:       "device discovery failed",
			devices:    nil,
			wantReason: SwapReasonProbeFailed,
		},
		{
			name:       "profile probe failed",
			devices:    []string{"sda1"},
			profileErr: errors.New("btrfs: boom"),
			wantReason: SwapReasonProbeFailed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := swapCapabilityFrom("/town-os", tc.devices, tc.profiles, tc.profileErr)
			if got.Supported != tc.wantSupported {
				t.Fatalf("Supported = %v, want %v (reason %q)", got.Supported, tc.wantSupported, got.Reason)
			}
			if got.Reason != tc.wantReason {
				t.Fatalf("Reason = %q, want %q", got.Reason, tc.wantReason)
			}
			if got.Devices != len(tc.devices) {
				t.Fatalf("Devices = %d, want %d", got.Devices, len(tc.devices))
			}
			if got.Path != "/town-os/swap/swapfile" {
				t.Fatalf("Path = %q, want /town-os/swap/swapfile", got.Path)
			}
			// A reason is only meaningful as an explanation of a "no".
			if got.Supported && got.Reason != "" {
				t.Fatalf("supported capability carries reason %q", got.Reason)
			}
		})
	}
}

func TestParseProcSwaps(t *testing.T) {
	t.Parallel()

	const header = "Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n"

	cases := []struct {
		name     string
		in       string
		path     string
		wantOn   bool
		wantSize uint64
		wantUsed uint64
	}{
		{
			name:     "active swapfile reports KiB scaled to bytes",
			in:       header + "/town-os/swap/swapfile\t\tfile\t\t2097148\t\t1068\t\t-2\n",
			path:     "/town-os/swap/swapfile",
			wantOn:   true,
			wantSize: 2097148 * 1024,
			wantUsed: 1068 * 1024,
		},
		{
			name:   "no swap at all",
			in:     header,
			path:   "/town-os/swap/swapfile",
			wantOn: false,
		},
		{
			name: "a different swap is active, ours is not",
			// The box may legitimately have other swap; only the file we
			// manage counts as ours.
			in:     header + "/dev/sda2\t\t\tpartition\t4194300\t\t0\t\t-2\n",
			path:   "/town-os/swap/swapfile",
			wantOn: false,
		},
		{
			name:     "ours among several",
			in:       header + "/dev/sda2\t\tpartition\t4194300\t0\t-2\n/town-os/swap/swapfile\tfile\t1048572\t512\t-3\n",
			path:     "/town-os/swap/swapfile",
			wantOn:   true,
			wantSize: 1048572 * 1024,
			wantUsed: 512 * 1024,
		},
		{
			name:   "prefix must not match",
			in:     header + "/town-os/swap/swapfile.old\tfile\t1048572\t0\t-2\n",
			path:   "/town-os/swap/swapfile",
			wantOn: false,
		},
		{
			name:   "empty input",
			in:     "",
			path:   "/town-os/swap/swapfile",
			wantOn: false,
		},
		{
			name:   "truncated line is ignored",
			in:     header + "/town-os/swap/swapfile\tfile\n",
			path:   "/town-os/swap/swapfile",
			wantOn: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			on, size, used := parseProcSwaps([]byte(tc.in), tc.path)
			got := []any{on, size, used}
			want := []any{tc.wantOn, tc.wantSize, tc.wantUsed}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("parseProcSwaps = %v, want %v", got, want)
			}
		})
	}
}

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"testing"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/storage"
)

// pingWithSwap stands a server up with a known swap capability already probed,
// which is what boot does for real. The capability is injected rather than
// discovered because discovering it needs a real btrfs and root, and neither is
// available to the test suite.
func pingWithSwap(t *testing.T, sc monitoring.SwapCapability) *PingResponse {
	t.Helper()
	ts := InitTestServer(ServerConfig{Storage: storage.InitBtrFSMock(), SwapCapability: sc})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if ping.Swap == nil {
		t.Fatal("ping did not report swap capability")
	}
	return ping
}

func TestHTTPPingReportsSwapUnsupported(t *testing.T) {
	// The shape of every multi-disk Town OS: btrfs will not swap on a
	// multi-device filesystem, so the user must be told why rather than just
	// finding no swap.
	ping := pingWithSwap(t, monitoring.SwapCapability{
		Devices:      4,
		DataProfiles: []string{"RAID5"},
		Reason:       monitoring.SwapReasonMultiDevice,
		Path:         "/town-os/swap/swapfile",
	})

	if ping.Swap.Supported {
		t.Fatal("a 4-device pool must not report swap as supported")
	}
	if ping.Swap.Reason != monitoring.SwapReasonMultiDevice {
		t.Fatalf("Reason = %q, want %q", ping.Swap.Reason, monitoring.SwapReasonMultiDevice)
	}
	// The layout has to survive the round trip too: "unsupported" alone does
	// not tell the user which of their disks caused it.
	if ping.Swap.Devices != 4 {
		t.Fatalf("Devices = %d, want 4", ping.Swap.Devices)
	}
	if len(ping.Swap.DataProfiles) != 1 || ping.Swap.DataProfiles[0] != "RAID5" {
		t.Fatalf("DataProfiles = %v, want [RAID5]", ping.Swap.DataProfiles)
	}
}

func TestHTTPPingReportsSwapSupported(t *testing.T) {
	ping := pingWithSwap(t, monitoring.SwapCapability{
		Supported:    true,
		Devices:      1,
		DataProfiles: []string{"single"},
		Path:         "/town-os/swap/swapfile",
	})

	if !ping.Swap.Supported {
		t.Fatalf("single-device single-profile pool reported unsupported: %q", ping.Swap.Reason)
	}
	if ping.Swap.Reason != "" {
		t.Fatalf("supported capability carries reason %q", ping.Swap.Reason)
	}
	// Usage is filled per request from /proc/swaps. The test host is not
	// swapping to this path, so Active must be false rather than inheriting
	// whatever was injected.
	if ping.Swap.Active {
		t.Fatal("Active should be read from /proc/swaps, not assumed")
	}
}

package rolodex

import (
	"slices"
	"testing"
)

// The forwarder list is no longer rendered into a file — it is programmed into
// the running rolodex over gRPC — so these tests assert what the manager
// RESOLVES the list to. That value is what ProgramRolodex pushes, at boot and
// after every rolodex restart.

func TestForwardersDefaultToThePublicResolvers(t *testing.T) {
	t.Parallel()

	m := NewManager(Config{})
	if !slices.Equal(m.Forwarders(), DefaultForwarders) {
		t.Fatalf("Forwarders() = %v, want %v", m.Forwarders(), DefaultForwarders)
	}
	if m.LocalForwarders() {
		t.Fatal("LocalForwarders() = true, want false by default")
	}
}

func TestLocalForwardersReplaceTheForwarderList(t *testing.T) {
	t.Parallel()

	resolv := writeResolvConf(t, "resolv.conf", "nameserver 192.168.4.1\n")
	m := NewManager(Config{
		LocalForwarders: true,
		ResolvConfPaths: []string{resolv},
	})

	want := []string{"192.168.4.1:53"}
	if !slices.Equal(m.Forwarders(), want) {
		t.Fatalf("Forwarders() = %v, want %v", m.Forwarders(), want)
	}
}

// TestLocalForwardersFallBackWhenDiscoveryFindsNothing covers a box with no
// lease yet, or one whose only nameserver line is a loopback stub. An empty
// discovery result must keep the previous addresses: pushing an empty list to
// rolodex would delete the local tier of the auto chain outright, which is the
// tier that exists for networks where everything else is filtered.
func TestLocalForwardersFallBackWhenDiscoveryFindsNothing(t *testing.T) {
	t.Parallel()

	resolv := writeResolvConf(t, "resolv.conf", "nameserver 127.0.0.53\n")
	configured := []string{"10.0.0.1:53"}
	m := NewManager(Config{
		LocalForwarders: true,
		Forwarders:      configured,
		ResolvConfPaths: []string{resolv},
	})

	if !slices.Equal(m.Forwarders(), configured) {
		t.Fatalf("Forwarders() = %v, want the configured list %v kept", m.Forwarders(), configured)
	}
}

func TestLocalForwardersFallBackToTheDefaultsWithNothingConfigured(t *testing.T) {
	t.Parallel()

	resolv := writeResolvConf(t, "resolv.conf", "# nothing usable here\n")
	m := NewManager(Config{
		LocalForwarders: true,
		ResolvConfPaths: []string{resolv},
	})

	if !slices.Equal(m.Forwarders(), DefaultForwarders) {
		t.Fatalf("Forwarders() = %v, want %v", m.Forwarders(), DefaultForwarders)
	}
}

// TestLocalForwardersLeaveTheResolutionModeAlone is the invariant that keeps
// two independent settings independent: the flag changes WHICH addresses the
// local tier holds, and the mode decides whether that tier is consulted at all.
// Turning it on must never move the mode — in recursive the list is unused, and
// silently switching to forward would send every query to the local resolver.
func TestLocalForwardersLeaveTheResolutionModeAlone(t *testing.T) {
	t.Parallel()

	resolv := writeResolvConf(t, "resolv.conf", "nameserver 192.168.4.1\n")
	m := NewManager(Config{
		ResolutionMode:  ResolutionModeRecursive,
		ResolvConfPaths: []string{resolv},
	})

	m.SetLocalForwarders(true)
	if got := m.ResolutionMode(); got != ResolutionModeRecursive {
		t.Fatalf("ResolutionMode() = %q after enabling local forwarders, want recursive", got)
	}
	m.SetLocalForwarders(false)
	if got := m.ResolutionMode(); got != ResolutionModeRecursive {
		t.Fatalf("ResolutionMode() = %q after disabling local forwarders, want recursive", got)
	}
}

// TestSetLocalForwardersSwitchesBothWays asserts the toggle is not one-way:
// turning it off has to restore the configured or default list, since that is
// what gets programmed into rolodex on the way back.
func TestSetLocalForwardersSwitchesBothWays(t *testing.T) {
	t.Parallel()

	resolv := writeResolvConf(t, "resolv.conf", "nameserver 192.168.4.1\n")
	m := NewManager(Config{ResolvConfPaths: []string{resolv}})

	m.SetLocalForwarders(true)
	if want := []string{"192.168.4.1:53"}; !slices.Equal(m.Forwarders(), want) {
		t.Fatalf("Forwarders() = %v after enabling, want %v", m.Forwarders(), want)
	}
	if !m.LocalForwarders() {
		t.Fatal("LocalForwarders() = false after enabling")
	}

	m.SetLocalForwarders(false)
	if !slices.Equal(m.Forwarders(), DefaultForwarders) {
		t.Fatalf("Forwarders() = %v after disabling, want %v", m.Forwarders(), DefaultForwarders)
	}
	if m.LocalForwarders() {
		t.Fatal("LocalForwarders() = true after disabling")
	}
}

// TestLocalForwardersRediscoverOnEveryRead is why the toggle does not cache:
// with the flag already on, the discovered addresses themselves move (a new
// lease, a different network), and the programming path reads this on every
// pass so the new address reaches rolodex without anything else happening.
func TestLocalForwardersRediscoverOnEveryRead(t *testing.T) {
	t.Parallel()

	first := writeResolvConf(t, "first.conf", "nameserver 192.168.4.1\n")
	second := writeResolvConf(t, "second.conf", "nameserver 10.9.9.9\n")

	m := NewManager(Config{LocalForwarders: true, ResolvConfPaths: []string{first}})
	if want := []string{"192.168.4.1:53"}; !slices.Equal(m.Forwarders(), want) {
		t.Fatalf("Forwarders() = %v, want %v", m.Forwarders(), want)
	}

	m2 := NewManager(Config{LocalForwarders: true, ResolvConfPaths: []string{second}})
	if want := []string{"10.9.9.9:53"}; !slices.Equal(m2.Forwarders(), want) {
		t.Fatalf("Forwarders() = %v, want %v", m2.Forwarders(), want)
	}
}

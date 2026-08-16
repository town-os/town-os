package rolodex

import (
	"context"
	"slices"
	"testing"
	"time"
)

// The forwarder list is no longer rendered into a file — it is programmed into
// the running rolodex over gRPC — so these tests assert what the manager
// RESOLVES the list to. That value is what ProgramRolodex pushes, at boot and
// after every rolodex restart.
//
// Every manager here that has LocalForwarders on sets ForwarderProbe. Left nil
// the probe is a real DNS query to whatever the test machine's gateway is,
// which would make these tests depend on the network they ran on — and pass on
// any box whose gateway resolves, whatever the code under them did.

// acceptAll is the probe for a network where every candidate answers.
func acceptAll(context.Context, string) bool { return true }

// rejectAll is the probe for a network where none of them do.
func rejectAll(context.Context, string) bool { return false }

// acceptOnly is the probe for the case this feature exists for: some of the
// candidates answer and the rest are being black-holed by a filter.
func acceptOnly(working ...string) func(context.Context, string) bool {
	return func(_ context.Context, addr string) bool {
		return slices.Contains(working, addr)
	}
}

func TestForwardersDefaultToThePublicResolvers(t *testing.T) {
	t.Parallel()

	m := NewManager(Config{})
	got := m.Forwarders(t.Context())
	if !slices.Equal(got, DefaultForwarders) {
		t.Fatalf("Forwarders() = %v, want %v", got, DefaultForwarders)
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
		RouteTablePath:  writeRouteTable(t, ""),
		ForwarderProbe:  acceptAll,
	})

	want := []string{"192.168.4.1:53"}
	if got := m.Forwarders(t.Context()); !slices.Equal(got, want) {
		t.Fatalf("Forwarders() = %v, want %v", got, want)
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
		RouteTablePath:  writeRouteTable(t, ""),
		ForwarderProbe:  acceptAll,
	})

	if got := m.Forwarders(t.Context()); !slices.Equal(got, configured) {
		t.Fatalf("Forwarders() = %v, want the configured list %v kept", got, configured)
	}
}

// TestLocalForwardersFallBackWhenNothingDiscoveredAnswers is the same fallback
// reached the other way, and is the case the probe was added for: discovery
// found an address, the address does not resolve. Keeping it would put a dead
// resolver in the local tier, where "auto" reaches it only after the roots and
// the encrypted upstreams have failed — so it is not a harmless spare, it is a
// per-forwarder timeout charged to every query on its way to SERVFAIL.
func TestLocalForwardersFallBackWhenNothingDiscoveredAnswers(t *testing.T) {
	t.Parallel()

	resolv := writeResolvConf(t, "resolv.conf", "nameserver 192.168.4.1\n")
	configured := []string{"10.0.0.1:53"}
	m := NewManager(Config{
		LocalForwarders: true,
		Forwarders:      configured,
		ResolvConfPaths: []string{resolv},
		RouteTablePath:  writeRouteTable(t, ""),
		ForwarderProbe:  rejectAll,
	})

	if got := m.Forwarders(t.Context()); !slices.Equal(got, configured) {
		t.Fatalf("Forwarders() = %v, want the configured list %v kept when nothing answers", got, configured)
	}
}

// TestLocalForwardersKeepOnlyTheCandidatesThatAnswer is the control for the two
// tests above: a probe that accepts everything and one that rejects everything
// both produce a defensible list, so neither proves the probe is consulted per
// candidate. This pins that the surviving list is the working SUBSET — the
// gateway answers, the resolv.conf entry does not, and only the gateway is kept.
func TestLocalForwardersKeepOnlyTheCandidatesThatAnswer(t *testing.T) {
	t.Parallel()

	resolv := writeResolvConf(t, "resolv.conf", "nameserver 192.168.4.1\n")
	routes := writeRouteTable(t, defaultRouteLine("ens3", "0101A8C0"))
	m := NewManager(Config{
		LocalForwarders: true,
		ResolvConfPaths: []string{resolv},
		RouteTablePath:  routes,
		ForwarderProbe:  acceptOnly("192.168.1.1:53"),
	})

	want := []string{"192.168.1.1:53"}
	if got := m.Forwarders(t.Context()); !slices.Equal(got, want) {
		t.Fatalf("Forwarders() = %v, want only the candidate that answered %v", got, want)
	}
}

func TestLocalForwardersFallBackToTheDefaultsWithNothingConfigured(t *testing.T) {
	t.Parallel()

	resolv := writeResolvConf(t, "resolv.conf", "# nothing usable here\n")
	m := NewManager(Config{
		LocalForwarders: true,
		ResolvConfPaths: []string{resolv},
		RouteTablePath:  writeRouteTable(t, ""),
		ForwarderProbe:  acceptAll,
	})

	if got := m.Forwarders(t.Context()); !slices.Equal(got, DefaultForwarders) {
		t.Fatalf("Forwarders() = %v, want %v", got, DefaultForwarders)
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
		RouteTablePath:  writeRouteTable(t, ""),
		ForwarderProbe:  acceptAll,
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
	m := NewManager(Config{
		ResolvConfPaths: []string{resolv},
		RouteTablePath:  writeRouteTable(t, ""),
		ForwarderProbe:  acceptAll,
	})

	m.SetLocalForwarders(true)
	if want := []string{"192.168.4.1:53"}; !slices.Equal(m.Forwarders(t.Context()), want) {
		t.Fatalf("Forwarders() = %v after enabling, want %v", m.Forwarders(t.Context()), want)
	}
	if !m.LocalForwarders() {
		t.Fatal("LocalForwarders() = false after enabling")
	}

	m.SetLocalForwarders(false)
	if got := m.Forwarders(t.Context()); !slices.Equal(got, DefaultForwarders) {
		t.Fatalf("Forwarders() = %v after disabling, want %v", got, DefaultForwarders)
	}
	if m.LocalForwarders() {
		t.Fatal("LocalForwarders() = true after disabling")
	}
}

// TestLocalForwardersRediscoverForANewNetwork is why the list is derived rather
// than frozen at construction: with the flag already on, the discovered
// addresses themselves move (a new lease, a different network), and the
// programming path reads this so the new address reaches rolodex without
// anything else happening.
func TestLocalForwardersRediscoverForANewNetwork(t *testing.T) {
	t.Parallel()

	first := writeResolvConf(t, "first.conf", "nameserver 192.168.4.1\n")
	second := writeResolvConf(t, "second.conf", "nameserver 10.9.9.9\n")
	routes := writeRouteTable(t, "")

	m := NewManager(Config{
		LocalForwarders: true,
		ResolvConfPaths: []string{first},
		RouteTablePath:  routes,
		ForwarderProbe:  acceptAll,
	})
	if want := []string{"192.168.4.1:53"}; !slices.Equal(m.Forwarders(t.Context()), want) {
		t.Fatalf("Forwarders() = %v, want %v", m.Forwarders(t.Context()), want)
	}

	m2 := NewManager(Config{
		LocalForwarders: true,
		ResolvConfPaths: []string{second},
		RouteTablePath:  routes,
		ForwarderProbe:  acceptAll,
	})
	if want := []string{"10.9.9.9:53"}; !slices.Equal(m2.Forwarders(t.Context()), want) {
		t.Fatalf("Forwarders() = %v, want %v", m2.Forwarders(t.Context()), want)
	}
}

// TestForwardersReuseAValidatedListWithinTheTTL pins the cache. ProgramRolodex
// re-pushes the forwarders on every tick; without the cache each tick would
// re-probe, which on a box where nothing answers means paying the probe timeout
// on a schedule forever.
func TestForwardersReuseAValidatedListWithinTheTTL(t *testing.T) {
	t.Parallel()

	var probes int
	resolv := writeResolvConf(t, "resolv.conf", "nameserver 192.168.4.1\n")
	m := NewManager(Config{
		LocalForwarders: true,
		ResolvConfPaths: []string{resolv},
		RouteTablePath:  writeRouteTable(t, ""),
		ForwarderProbe: func(context.Context, string) bool {
			probes++
			return true
		},
	})

	for range 5 {
		m.Forwarders(t.Context())
	}
	if probes != 1 {
		t.Fatalf("probed %d times across 5 reads, want 1 — the validated list is cached", probes)
	}
}

// TestSetLocalForwardersDropsTheValidatedList is the control for the TTL test:
// a cache that is never invalidated would satisfy it just as well. An operator
// toggling the setting has just told the box something about its network, so
// the next read has to probe again rather than serve a list from up to
// DefaultForwarderProbeTTL ago. It is also what gives RefreshDNSLocalForwarders
// — which deliberately does not short-circuit on an unchanged flag — something
// new to find.
func TestSetLocalForwardersDropsTheValidatedList(t *testing.T) {
	t.Parallel()

	var probes int
	resolv := writeResolvConf(t, "resolv.conf", "nameserver 192.168.4.1\n")
	m := NewManager(Config{
		LocalForwarders: true,
		ResolvConfPaths: []string{resolv},
		RouteTablePath:  writeRouteTable(t, ""),
		ForwarderProbe: func(context.Context, string) bool {
			probes++
			return true
		},
	})

	m.Forwarders(t.Context())
	m.SetLocalForwarders(true)
	m.Forwarders(t.Context())

	if probes != 2 {
		t.Fatalf("probed %d times, want 2 — SetLocalForwarders must drop the cached list", probes)
	}
}

// TestForwardersReprobeAfterTheTTLExpires is the other half of the cache
// contract: it is a TTL, not a one-shot. A box whose network changed under it
// has to pick the new resolver up on its own.
func TestForwardersReprobeAfterTheTTLExpires(t *testing.T) {
	t.Parallel()

	var probes int
	resolv := writeResolvConf(t, "resolv.conf", "nameserver 192.168.4.1\n")
	m := NewManager(Config{
		LocalForwarders:   true,
		ResolvConfPaths:   []string{resolv},
		RouteTablePath:    writeRouteTable(t, ""),
		ForwarderProbeTTL: time.Nanosecond,
		ForwarderProbe: func(context.Context, string) bool {
			probes++
			return true
		},
	})

	m.Forwarders(t.Context())
	m.Forwarders(t.Context())

	if probes != 2 {
		t.Fatalf("probed %d times, want 2 — an expired list must be re-probed", probes)
	}
}

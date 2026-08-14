package ingressctl

import (
	"strconv"
	"testing"

	"gitea.com/town-os/town-os/src/ingress"
)

// TestUnitConfigPublishesMetricsOnLoopbackOnly is the privacy half of the
// endpoint. The scrape lists every hostname the box serves and flags the ones
// with no certificate yet; published on the wildcard it would be readable by
// anything on the LAN or the overlay, with no authentication anywhere in front
// of it.
func TestUnitConfigPublishesMetricsOnLoopbackOnly(t *testing.T) {
	m := NewManager(Config{
		Image:        "localhost/ingress:test",
		DataDir:      "/data/ingress",
		TLSHostDir:   "/data/tls",
		HostPort:     8443,
		HTTPHostPort: 8080,
		MetricsPort:  39146,
	})
	uc := m.unitConfig()

	if !hasPublish(uc.Args, MetricsHost+":39146:39146") {
		t.Fatalf("expected -p %s:39146:39146 publish, got args: %v", MetricsHost, uc.Args)
	}
	// A bare "39146:39146" would bind every interface. Assert the spelling that
	// does that is absent rather than merely that the loopback one is present.
	if hasPublish(uc.Args, "39146:39146") {
		t.Fatalf("metrics port must not be published on the wildcard, got args: %v", uc.Args)
	}
	if !hasFlagValue(uc.Command, "--metrics-port", "39146") {
		t.Fatalf("expected --metrics-port 39146 in command, got: %v", uc.Command)
	}
}

// TestUnitConfigDefaultMetricsPort covers a real box, which sets none of these:
// the unit must still publish and pass the documented default rather than
// nothing at all.
func TestUnitConfigDefaultMetricsPort(t *testing.T) {
	m := NewManager(Config{Image: "localhost/ingress:test", DataDir: "/d", TLSHostDir: "/t"})
	uc := m.unitConfig()

	port := strconv.Itoa(ingress.DefaultMetricsPort)
	if want := MetricsHost + ":" + port; MetricsAddrFor(0) != want {
		t.Fatalf("MetricsAddrFor(0) = %q, want the default %q", MetricsAddrFor(0), want)
	}
	if !hasPublish(uc.Args, MetricsHost+":"+port+":"+port) {
		t.Fatalf("default unit does not publish the default metrics port, got args: %v", uc.Args)
	}
	if !hasFlagValue(uc.Command, "--metrics-port", port) {
		t.Fatalf("default unit does not pass the default metrics port, got: %v", uc.Command)
	}
}

// TestMetricsAddrMatchesPublishedPort is the invariant the scrape depends on: a
// relocated ingress must be scraped where its unit actually publishes. Two
// concurrent test boxes and a dev box share the host namespace, so an address
// recomposed from the default would scrape whichever ingress happened to own
// 9146 — IRON RULE.
func TestMetricsAddrMatchesPublishedPort(t *testing.T) {
	m := NewManager(Config{
		Image:       "localhost/ingress:test",
		DataDir:     "/data/ingress",
		TLSHostDir:  "/data/tls",
		MetricsPort: 41146,
	})

	addr := m.MetricsAddr()
	if addr != MetricsHost+":41146" {
		t.Fatalf("MetricsAddr = %q, want %s:41146", addr, MetricsHost)
	}
	if addr == MetricsAddrFor(0) {
		t.Fatalf("relocated ingress still advertises the default port: %q", addr)
	}
	// The published spec is derived from the same port the address is, so the
	// scrape target and the listener cannot drift.
	if !hasPublish(m.unitConfig().Args, addr+":41146") {
		t.Fatalf("unit does not publish what MetricsAddr advertises (%q): %v", addr, m.unitConfig().Args)
	}
}

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package main

import (
	"testing"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/rolodex"
)

// TestEnvPortUnsetIsEmpty asserts an unset variable yields "" so every caller
// falls through to its documented default. This is the case a real box hits.
func TestEnvPortUnsetIsEmpty(t *testing.T) {
	if got := envPort("TOWN_OS_PORT_TEST_DEFINITELY_UNSET"); got != "" {
		t.Errorf("unset variable should yield \"\", got %q", got)
	}
}

// TestEnvPortValid asserts a well-formed port survives, normalized through
// strconv so surrounding whitespace cannot leak into a systemd unit.
func TestEnvPortValid(t *testing.T) {
	cases := []struct{ in, want string }{
		{"9090", "9090"},
		{" 41337 ", "41337"},
		{"1", "1"},
		{"65535", "65535"},
	}
	for _, tc := range cases {
		t.Setenv("TOWN_OS_PORT_TEST", tc.in)
		if got := envPort("TOWN_OS_PORT_TEST"); got != tc.want {
			t.Errorf("envPort(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestEnvPortInvalidFallsBack asserts a malformed or out-of-range value is
// rejected rather than passed through. A value like "0" or "70000" would render
// a unit systemd starts and the service immediately dies on; falling back to the
// default at least leaves a working (if colliding) box.
func TestEnvPortInvalidFallsBack(t *testing.T) {
	for _, in := range []string{"", "   ", "abc", "0", "-1", "65536", "8080a", "80.5"} {
		t.Setenv("TOWN_OS_PORT_TEST", in)
		if got := envPort("TOWN_OS_PORT_TEST"); got != "" {
			t.Errorf("envPort(%q) = %q, want \"\" (fall back to default)", in, got)
		}
	}
}

// TestEnvPortIntMapsEmptyToZero asserts the ingress form uses 0 for "default",
// which is what ingressctl.Config treats as unset.
func TestEnvPortIntMapsEmptyToZero(t *testing.T) {
	t.Setenv("TOWN_OS_PORT_TEST", "")
	if got := envPortInt("TOWN_OS_PORT_TEST"); got != 0 {
		t.Errorf("empty should map to 0, got %d", got)
	}
	t.Setenv("TOWN_OS_PORT_TEST", "8443")
	if got := envPortInt("TOWN_OS_PORT_TEST"); got != 8443 {
		t.Errorf("got %d, want 8443", got)
	}
}

// TestMonitoringPortsFromEnvUnsetIsZeroValue asserts an unconfigured box gets
// monitoring.Ports{} — which withDefaults turns into 9100/9090/5308. This is
// the guard that keeps the harness plumbing from changing production behavior.
func TestMonitoringPortsFromEnvUnsetIsZeroValue(t *testing.T) {
	t.Setenv(EnvNodeExporterPort, "")
	t.Setenv(EnvPrometheusPort, "")
	t.Setenv(EnvMonitoringPort, "")
	if got := monitoringPortsFromEnv(); got != (monitoring.Ports{}) {
		t.Errorf("unset environment should yield the zero value, got %+v", got)
	}
}

// TestMonitoringPortsFromEnvReadsEachVariable asserts each variable lands in
// its own field — a transposition here would be invisible until two services
// tried to bind the same port.
func TestMonitoringPortsFromEnvReadsEachVariable(t *testing.T) {
	t.Setenv(EnvNodeExporterPort, "39100")
	t.Setenv(EnvPrometheusPort, "39090")
	t.Setenv(EnvMonitoringPort, "35308")
	want := monitoring.Ports{NodeExporter: "39100", Prometheus: "39090", External: "35308"}
	if got := monitoringPortsFromEnv(); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestDNSPortFromEnv asserts the rolodex port is read from its own variable.
func TestDNSPortFromEnv(t *testing.T) {
	t.Setenv(EnvDNSPort, "35353")
	if got := dnsPortFromEnv(); got != "35353" {
		t.Errorf("got %q, want %q", got, "35353")
	}
}

// TestDNSPortIsDefault asserts the predicate that gates systemd-resolved
// routing. resolved can only forward a domain to a resolver on :53 (a per-domain
// DNS server address carries no port), so an explicit 53 and an unset value must
// both count as default, and anything else must not — pointing resolved at
// DNSLoopback for a rolodex on 35353 would blackhole every .tld query.
func TestDNSPortIsDefault(t *testing.T) {
	for _, in := range []string{"", rolodex.DefaultDNSPort} {
		if !dnsPortIsDefault(in) {
			t.Errorf("dnsPortIsDefault(%q) = false, want true", in)
		}
	}
	for _, in := range []string{"35353", "5353", "1053"} {
		if dnsPortIsDefault(in) {
			t.Errorf("dnsPortIsDefault(%q) = true, want false", in)
		}
	}
}

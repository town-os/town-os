// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"os"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/systemd"
)

// The environment variables the harness sets to relocate the fixed host ports
// the system services would otherwise bind. Duplicated here as literals rather
// than imported from package main (which is not importable) — the unit tests in
// src/svc/systemcontroller/cmd/systemcontroller/ports_test.go pin the constants
// these must match.
const (
	envDNSPort          = "TOWN_OS_DNS_PORT"
	envNodeExporterPort = "TOWN_OS_NODE_EXPORTER_PORT"
	envPrometheusPort   = "TOWN_OS_PROMETHEUS_PORT"
	envMonitoringPort   = "TOWN_OS_MONITORING_PORT"
	envIngressHTTPSPort = "INGRESS_HTTPS_PORT"
	envIngressHTTPPort  = "INGRESS_HTTP_PORT"
)

// requireHarnessPort returns the ephemeral port the harness assigned, or skips
// when the suite is being run outside `make` (a bare `go test` against a
// hand-started container has no allocated ports and would otherwise fail on
// something that is not a defect).
func requireHarnessPort(t *testing.T, envVar string) string {
	t.Helper()
	v := strings.TrimSpace(os.Getenv(envVar))
	if v == "" {
		t.Skipf("%s not set; run via make so the harness allocates system-service ports", envVar)
	}
	return v
}

// readLiveUnit returns the on-disk content of an installed system-service unit.
func readLiveUnit(t *testing.T, key string) string {
	t.Helper()
	name := systemd.SystemServiceUnitName(key)
	content, err := systemd.NewManager().ReadUnit(name)
	if err != nil {
		t.Fatalf("ReadUnit(%s): %v", name, err)
	}
	return content
}

// TestSystemServicesBindHarnessAssignedPorts is the end-to-end guard for the
// dev/test-full collision.
//
// The test container runs --net host, so every system service binds in the same
// network namespace a `make dev` box binds in. Before the port overrides existed
// they both took 127.0.0.2:53, :9100, :9090, :5308, :443 and :80, and whichever
// lost the race crash-looped forever under Restart=always. This asserts the
// units the *running* controller actually installed carry the harness-assigned
// ephemeral ports — the unit tests can only prove the config functions honour a
// Ports value, not that the boot path passes one.
func TestSystemServicesBindHarnessAssignedPorts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		envVar string
		key    string
		// want is a format-free substring that must appear in the unit with
		// the assigned port substituted for %s.
		want string
		// forbidden is the default port spelling that must NOT appear.
		forbidden string
	}{
		{
			name:      "node-exporter",
			envVar:    envNodeExporterPort,
			key:       "node-exporter",
			want:      "--web.listen-address=127.0.0.1:%s",
			forbidden: "--web.listen-address=127.0.0.1:" + monitoring.NodeExporterPort,
		},
		{
			name:      "prometheus",
			envVar:    envPrometheusPort,
			key:       "prometheus",
			want:      "--web.listen-address=127.0.0.1:%s",
			forbidden: "--web.listen-address=127.0.0.1:" + monitoring.PrometheusPort,
		},
		{
			name:      "monitoring-ui",
			envVar:    envMonitoringPort,
			key:       "monitoring-ui",
			want:      "TCP-LISTEN:%s,",
			forbidden: "TCP-LISTEN:" + monitoring.MonitoringExternalPort + ",",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			port := requireHarnessPort(t, tc.envVar)
			content := readLiveUnit(t, tc.key)
			want := strings.Replace(tc.want, "%s", port, 1)
			if !strings.Contains(content, want) {
				t.Errorf("%s unit does not bind the harness port %s (want %q):\n%s", tc.name, port, want, content)
			}
			if strings.Contains(content, tc.forbidden) {
				t.Errorf("%s unit still binds the default port (%q):\n%s", tc.name, tc.forbidden, content)
			}
		})
	}
}

// TestRolodexConfigUsesHarnessAssignedDNSPort asserts rolodex.yml was rendered
// for the harness port rather than :53. Two rolodex daemons on 127.0.0.2:53
// is the single most disruptive collision between a test box and a dev box:
// the loser cannot serve DNS at all, so every package name stops resolving.
func TestRolodexConfigUsesHarnessAssignedDNSPort(t *testing.T) {
	t.Parallel()

	port := requireHarnessPort(t, envDNSPort)
	raw, err := os.ReadFile("/town-os/rolodex/rolodex.yml")
	if err != nil {
		t.Fatalf("read rolodex.yml: %v", err)
	}
	got := string(raw)
	// The closing quote matters: rolodex.yml renders the bind as
	// "127.0.0.2:<port>", and a bare "127.0.0.2:53" check would match an
	// assigned port of 53421 and report a passing config as broken.
	if !strings.Contains(got, rolodex.DNSLoopback+":"+port+`"`) {
		t.Errorf("rolodex.yml does not bind %s:%s:\n%s", rolodex.DNSLoopback, port, got)
	}
	if strings.Contains(got, rolodex.DNSLoopback+":"+rolodex.DefaultDNSPort+`"`) {
		t.Errorf("rolodex.yml still binds the default DNS port:\n%s", got)
	}
}

// TestIngressPublishesHarnessAssignedPorts asserts the ingress unit publishes
// the harness ports instead of the privileged 443/80. Those two are special:
// they are privileged and singular, so a collision there cannot be worked
// around by retrying — the second binder simply never starts.
func TestIngressPublishesHarnessAssignedPorts(t *testing.T) {
	t.Parallel()

	httpsPort := requireHarnessPort(t, envIngressHTTPSPort)
	httpPort := requireHarnessPort(t, envIngressHTTPPort)
	content := readLiveUnit(t, "ingress")

	for _, want := range []string{httpsPort + ":" + httpsPort, httpPort + ":" + httpPort} {
		if !strings.Contains(content, want) {
			t.Errorf("ingress unit does not publish %s:\n%s", want, content)
		}
	}
	for _, forbidden := range []string{" 443:443", " 80:80"} {
		if strings.Contains(content, forbidden) {
			t.Errorf("ingress unit still publishes the privileged port (%q):\n%s", forbidden, content)
		}
	}
}

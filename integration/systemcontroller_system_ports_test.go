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
	envRolodexMetrics   = "TOWN_OS_ROLODEX_METRICS_PORT"
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

// TestRolodexDNSPortOverrideReachesTheBootPath asserts the boot saw
// TOWN_OS_DNS_PORT and took the branch that goes with it.
//
// It used to read the bind out of `/town-os/rolodex/rolodex.yml`. That file no
// longer exists on a booted box that Town OS put up: rolodex.yml belongs to the
// install image, Town OS writes no config at all, and in this container nothing
// stands in for the install image — so the assertion was against a file whose
// absence is now correct.
//
// What is left to observe is the other half of the override, and it is the half
// that can silently break DNS. systemd-resolved can only route a domain to a
// resolver on :53, because a per-domain DNS server address carries no port. So
// a relocated rolodex must NOT get a resolved drop-in: one written anyway would
// point resolved at 127.0.0.2:53, where this box has deliberately bound nothing,
// and every query for the TLD would be blackholed rather than falling through to
// the normal resolver path. The drop-in's absence is therefore the boot path
// reading the variable — and the harness's own IRON RULE check (no town-os.conf
// on the host after a run) is the same assertion from the outside.
func TestRolodexDNSPortOverrideReachesTheBootPath(t *testing.T) {
	t.Parallel()

	port := requireHarnessPort(t, envDNSPort)
	if port == rolodex.DefaultDNSPort {
		t.Fatalf("%s=%s is the default port; the harness must relocate rolodex off :53 (IRON RULE)",
			envDNSPort, port)
	}

	// The container has its own /etc, so this is the controller's own drop-in
	// and never a `make dev` box's.
	raw, err := os.ReadFile(rolodex.ResolvedDropInPath)
	if err == nil {
		t.Errorf("boot wrote %s while rolodex serves DNS on %s rather than %s; resolved would send every query for the TLD to %s:%s, where nothing is listening:\n%s",
			rolodex.ResolvedDropInPath, port, rolodex.DefaultDNSPort,
			rolodex.DNSLoopback, rolodex.DefaultDNSPort, raw)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", rolodex.ResolvedDropInPath, err)
	}
}

// TestRolodexMetricsPortOverrideReachesTheScrapeConfig is the same guard for
// the Prometheus endpoint, which is a second listener in the same host
// namespace as the DNS one. It proves the boot path threads the variable
// through at all: the unit tests can show rolodex.Manager honours a
// MetricsPort, not that main.go reads the environment and hands it over.
//
// The half that asserted the bind in rolodex.yml is gone with the file — that
// config belongs to the install image now, and Town OS writes none — so what is
// checked here is the end Town OS still owns and still gets wrong on its own:
// the scrape target it generates. A relocated listener that Prometheus still
// scrapes at 9153 is not a collision, it is a rolodex that silently reads as
// down, and prometheus.yml is where that shows.
func TestRolodexMetricsPortOverrideReachesTheScrapeConfig(t *testing.T) {
	t.Parallel()

	port := requireHarnessPort(t, envRolodexMetrics)
	addr := rolodex.DNSLoopback + ":" + port

	promRaw, err := os.ReadFile("/town-os/monitoring/prometheus-config/prometheus.yml")
	if err != nil {
		t.Fatalf("read prometheus.yml: %v", err)
	}
	prom := string(promRaw)
	if !strings.Contains(prom, `- job_name: "`+monitoring.RolodexJobName+`"`) {
		t.Errorf("prometheus.yml carries no %s job:\n%s", monitoring.RolodexJobName, prom)
	}
	if !strings.Contains(prom, `targets: ["`+addr+`"]`) {
		t.Errorf("prometheus.yml does not scrape rolodex at %s:\n%s", addr, prom)
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

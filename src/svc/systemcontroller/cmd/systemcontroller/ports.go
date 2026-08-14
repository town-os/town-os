package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gitea.com/town-os/town-os/src/ingress/ingressctl"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// Environment variables that relocate the fixed host ports the system services
// bind. Every one defaults to the documented production port, so an unset
// environment reproduces today's boot exactly.
//
// These exist because the systemcontroller and every system service it starts
// run --net host: they bind in whatever network namespace the controller is in.
// On a real box that is the host and the defaults are correct. Inside the
// integration harness it is *also* the host namespace — the test container runs
// --net host too, deliberately, so builds keep working on captive networks where
// bridge-network DNS is broken. A test box and a `make dev` box therefore fight
// over 127.0.0.2:53, 127.0.0.2:9153, 9100, 9090, 5308, 443 and 80, and lose
// under Restart=always: whichever service loses the race crash-loops forever.
//
// The harness sets these to ephemeral ports the same way it already sets
// TOWN_OS_LISTEN. `make dev` sets none of them and keeps the production ports,
// because dev is meant to mirror a real box — redirect_host_dns needs rolodex on
// :53 and a browser needs the ingress on :443 — IRON RULE.
const (
	// EnvNodeExporterPort relocates node-exporter's loopback metrics port.
	EnvNodeExporterPort = "TOWN_OS_NODE_EXPORTER_PORT"
	// EnvPrometheusPort relocates Prometheus's loopback HTTP API port.
	EnvPrometheusPort = "TOWN_OS_PROMETHEUS_PORT"
	// EnvMonitoringPort relocates the single LAN-facing monitoring port.
	EnvMonitoringPort = "TOWN_OS_MONITORING_PORT"
	// EnvDNSPort relocates the port rolodex serves DNS on (on DNSLoopback).
	EnvDNSPort = "TOWN_OS_DNS_PORT"
	// EnvRolodexMetricsPort relocates the port rolodex serves its Prometheus
	// /metrics endpoint on (also on DNSLoopback).
	EnvRolodexMetricsPort = "TOWN_OS_ROLODEX_METRICS_PORT"
	// EnvIngressHTTPSPort relocates the ingress's published HTTPS port.
	EnvIngressHTTPSPort = "INGRESS_HTTPS_PORT"
	// EnvIngressHTTPPort relocates the ingress's published HTTP port.
	EnvIngressHTTPPort = "INGRESS_HTTP_PORT"
	// EnvIngressMetricsPort relocates the loopback port the ingress serves its
	// Prometheus /metrics endpoint on.
	EnvIngressMetricsPort = "INGRESS_METRICS_PORT"
)

// envPort reads a port number from the named environment variable.
//
// It returns "" when the variable is unset, empty, or does not hold a valid
// port, so every caller can treat "" as "use the documented default". An
// invalid value is reported on stderr rather than silently ignored: a typo in
// the harness would otherwise look identical to not setting the variable at
// all, and the resulting port collision only shows up much later as a
// crash-looping unit.
func envPort(name string) string {
	raw, ok := os.LookupEnv(name)
	if !ok {
		return ""
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 65535 {
		fmt.Fprintf(os.Stderr, "%s=%q is not a valid port (1-65535); using the default\n", name, raw)
		return ""
	}
	return strconv.Itoa(n)
}

// envPortInt is envPort for the ingress, whose config takes an int and treats
// 0 as "use the default".
func envPortInt(name string) int {
	v := envPort(name)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		// Unreachable: envPort only returns strconv.Itoa output.
		return 0
	}
	return n
}

// monitoringPortsFromEnv resolves the three monitoring host ports. Every unset
// field stays empty and monitoring.Ports.withDefaults fills in the production
// value, so an unset environment yields today's 9100/9090/5308.
func monitoringPortsFromEnv() monitoring.Ports {
	return monitoring.Ports{
		NodeExporter: envPort(EnvNodeExporterPort),
		Prometheus:   envPort(EnvPrometheusPort),
		External:     envPort(EnvMonitoringPort),
	}
}

// dnsPortFromEnv resolves rolodex's DNS port, defaulting to
// rolodex.DefaultDNSPort when unset.
func dnsPortFromEnv() string {
	return envPort(EnvDNSPort)
}

// rolodexMetricsPortFromEnv resolves rolodex's Prometheus endpoint port. An
// empty result means rolodex.DefaultMetricsPort, exactly as with the DNS port.
func rolodexMetricsPortFromEnv() string {
	return envPort(EnvRolodexMetricsPort)
}

// ingressMetricsPortFromEnv resolves the ingress's Prometheus endpoint port. An
// empty result is 0, which both ingressctl.Config and ingressctl.MetricsAddrFor
// read as "use the default" — so the unit and the scrape target agree even when
// nothing is set.
func ingressMetricsPortFromEnv() int {
	return envPortInt(EnvIngressMetricsPort)
}

// ingressMetricsTarget returns the address Prometheus should scrape the ingress
// at, or "" when the ingress is not running on this box.
//
// The empty case is a real one rather than a defensive branch: `make dev` and
// any INGRESS_IMAGE="" boot skip the ingress entirely, and a job aimed at a
// service that was never started sits permanently down — which reads as a
// broken router, the same misreading the rolodex and controller omissions exist
// to avoid.
func ingressMetricsTarget(ingressImage string) string {
	if ingressImage == "" {
		return ""
	}
	return ingressctl.MetricsAddrFor(ingressMetricsPortFromEnv())
}

// withScrapeTargets fills in the three scrape targets that are addresses rather
// than ports this stack binds: rolodex's Prometheus endpoint, the ingress's, and
// the controller's own. Everything else in ports is passed through untouched.
//
// This is a function rather than four lines inline in main() because it is the
// single point at which the box decides to collect its own metrics at all, and
// inline it was covered by nothing: every test of the scrape config feeds
// monitoring.WritePrometheusConfig a Ports value directly, so deleting the
// assignments left both jobs silently absent from a real boot with the whole
// suite still green.
//
// Neither address is defaulted when it cannot be derived. A job aimed at a
// guessed address sits permanently down and reads as a broken rolodex or a
// broken controller, which is worse than an absent scrape — see the omit
// branches in monitoring.WritePrometheusConfig.
//
// tls is what the listener was OBSERVED to speak (systemcontroller.
// ListenerSpeaksTLS), not what the environment asked for. A scheme derived a
// second time from TOWN_OS_TLS can disagree with the socket, and when it did,
// every controller scrape failed with "server gave HTTP response to HTTPS
// client" and nothing on the box reported it.
func withScrapeTargets(ports monitoring.Ports, rolMgr *rolodex.Manager, listenAddr, ingressMetrics string, tls bool) monitoring.Ports {
	// Taken from the manager rather than recomposed from the port, so the
	// target is by construction the same string rolodex.yml binds — the same
	// single-source-of-truth reason PackageNetworkState.FQDN exists.
	if rolMgr != nil {
		ports.RolodexMetrics = rolMgr.MetricsAddr()
	}
	// Built by ingressctl from the same port the unit publishes, and empty when
	// the ingress is disabled — see ingressMetricsTarget.
	ports.IngressMetrics = ingressMetrics
	// Derived from the same -listen value the server binds, so the target
	// cannot drift from the listener — and so a relocated harness instance
	// scrapes itself rather than whichever process happens to hold :5309.
	ports.ControllerMetrics = systemcontroller.MetricsScrapeTarget(listenAddr)
	// Only alongside a target: a scheme on a job that is never emitted is
	// dead config, and pinning it would make an unparseable -listen look like
	// a TLS decision.
	if tls && ports.ControllerMetrics != "" {
		ports.ControllerMetricsScheme = "https"
	}
	return ports
}

// dnsPortIsDefault reports whether rolodex is serving DNS on the standard port.
//
// systemd-resolved can only forward a routing domain to a resolver on :53 — a
// per-domain DNS server address carries no port. So when the harness relocates
// rolodex off :53, resolved routing is not merely unnecessary, it is
// unexpressible, and pointing resolved at DNSLoopback would silently blackhole
// every .tld query instead of leaving them to the normal resolver path.
func dnsPortIsDefault(port string) bool {
	return port == "" || port == rolodex.DefaultDNSPort
}

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/rolodex"
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

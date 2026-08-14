// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/ingress/ingressctl"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/systemd"
)

// envIngressMetricsPort is the harness override for the ingress's Prometheus
// endpoint. Duplicated as a literal for the same reason the other port
// variables in this package are: package main is not importable, and
// ports_test.go pins the constant this must match.
const envIngressMetricsPort = "INGRESS_METRICS_PORT"

// TestIngressMetricsPortIsHarnessAssigned is the both-ends assertion. A
// relocated listener that Prometheus still scrapes at 9146 is not a collision —
// it is an ingress that silently reads as down, or worse, a concurrent dev box's
// ingress answering under this box's job name. The only way to catch that is to
// assert the unit and the scrape config agree.
func TestIngressMetricsPortIsHarnessAssigned(t *testing.T) {
	t.Parallel()

	port := requireHarnessPort(t, envIngressMetricsPort)
	addr := ingressctl.MetricsHost + ":" + port

	// The generator puts every podman argument on its own continuation line, so
	// these are matched as tokens rather than as "-p <spec>" / "--metrics-port
	// <port>" pairs.
	content := readLiveUnit(t, "ingress")
	if want := addr + ":" + port; !strings.Contains(content, want) {
		t.Errorf("ingress unit does not publish %q:\n%s", want, content)
	}
	if got := unitArgAfter(content, "--metrics-port"); got != port {
		t.Errorf("ingress unit passes --metrics-port %q, want %q:\n%s", got, port, content)
	}
	// The default spelling must be gone entirely, not merely accompanied by the
	// assigned one.
	if def := strconv.Itoa(ingress.DefaultMetricsPort); strings.Contains(content, ":"+def+":"+def) {
		t.Errorf("ingress unit still publishes the default metrics port %s:\n%s", def, content)
	}

	promRaw, err := os.ReadFile("/town-os/monitoring/prometheus-config/prometheus.yml")
	if err != nil {
		t.Fatalf("read prometheus.yml: %v", err)
	}
	prom := string(promRaw)
	if !strings.Contains(prom, `- job_name: "`+monitoring.IngressJobName+`"`) {
		t.Errorf("prometheus.yml carries no %s job:\n%s", monitoring.IngressJobName, prom)
	}
	if !strings.Contains(prom, `targets: ["`+addr+`"]`) {
		t.Errorf("prometheus.yml does not scrape the ingress at %s:\n%s", addr, prom)
	}
}

// unitArgAfter returns the token following the given flag in a generated unit,
// or "" when the flag is absent. GenerateSystemServiceUnit writes one argument
// per continuation line ("  --metrics-port \\" then "  39146 \\"), so a flag and
// its value are never adjacent on one line.
func unitArgAfter(content, flag string) string {
	next := false
	for line := range strings.SplitSeq(content, "\n") {
		tok := strings.TrimSuffix(strings.TrimSpace(line), " \\")
		tok = strings.TrimSpace(tok)
		if next {
			return tok
		}
		if tok == flag {
			next = true
		}
	}
	return ""
}

// TestIngressServesPrometheusMetrics is the end-to-end half: it boots a REAL
// ingress container from the shipped image, programs a route over gRPC, and
// scrapes the endpoint the way Prometheus will.
//
// The unit tests can only prove the handler renders what we meant. They cannot
// prove the binary in the image binds the port at all, that the systemd unit
// publishes it where the scrape config points, or — the one most likely to ship
// silently — that caddy's admin API is reachable from inside the container, so
// the caddy_* families make it into the response. An ingress that got any of
// those wrong routes traffic perfectly while the job sits down.
//
// HARNESS-ONLY, and fully isolated: unique unit key, dedicated network, and
// ephemeral ports for all three listeners, so it never touches the booted
// production ingress — IRON RULE.
func TestIngressServesPrometheusMetrics(t *testing.T) {
	ingressImage := os.Getenv("INGRESS_IMAGE")
	if ingressImage == "" {
		t.Skip("harness-only: INGRESS_IMAGE unset (run via make test-integration)")
	}
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not available")
	}

	ctx := context.Background()
	uniq := strconv.FormatInt(time.Now().UnixNano(), 36)
	netName := "town-os-ingress-metrics-" + uniq

	mustPodman(t, "network", "create", netName)
	t.Cleanup(func() { _ = podman("network", "rm", "-f", netName) })

	dataDir := filepath.Join("/run/town-os/ingress-metrics", uniq)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	metricsPort := e2eFreePort(t)
	mgr := ingressctl.NewManager(ingressctl.Config{
		Systemd:      systemd.NewManager(),
		DataDir:      dataDir,
		TLSHostDir:   "/town-os/tls",
		Image:        ingressImage,
		PullNever:    true,
		Key:          "ingress-metrics-" + uniq,
		NetworkName:  netName,
		HostPort:     e2eFreePort(t),
		HTTPHostPort: e2eFreePort(t),
		MetricsPort:  metricsPort,
		// No UI in this test, so no fallback vhost.
		DefaultBackend: "-",
	})
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("start ingress container: %v", err)
	}
	container := systemd.SystemServiceContainerName(mgr.Key())
	t.Cleanup(func() {
		sd := systemd.NewManager()
		unit := systemd.SystemServiceUnitName(mgr.Key())
		logCleanupf(t, sd.SetStatus(context.Background(), unit, systemd.Stop), "SetStatus(stop)")
		logCleanupf(t, sd.UninstallUnit(context.Background(), unit), "UninstallUnit")
		_ = podman("rm", "-f", container)
	})
	if err := mgr.WaitForReady(ctx); err != nil {
		t.Fatalf("ingress gRPC socket not ready: %v", err)
	}

	// A page-shaped route: served over plain HTTP with no leaf, so the config
	// caddy loads needs no certificate on disk while still exercising a real
	// route through the renderer, the reload, and the collector.
	const fqdn = "metrics.local.home"
	ic, err := ingress.Dial(ctx, mgr.SocketPath())
	if err != nil {
		t.Fatalf("dial ingress: %v", err)
	}
	defer func() { logCleanupf(t, ic.Close(), "close ingress client") }()
	if err := ic.SetRoutes(ctx, []*ingresspb.Route{{
		Hostname:  fqdn,
		Backend:   "127.0.0.1:80",
		ServeHttp: true,
	}}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	// Scrape the address MetricsAddr advertises, not a recomposed one — that
	// equality is what the Prometheus job depends on.
	scrapeURL := "http://" + mgr.MetricsAddr() + ingress.MetricsPath
	var body string
	if !e2ePoll(func() bool {
		got, scrapeErr := scrapeOnce(ctx, scrapeURL)
		if scrapeErr != nil {
			return false
		}
		body = got
		return true
	}) {
		e2eDumpIngressDiag(t, container, "127.0.0.1")
		t.Fatalf("no successful scrape of %s before deadline", scrapeURL)
	}

	for _, want := range []string{
		"# TYPE townos_ingress_up gauge",
		"townos_ingress_up 1",
		"# TYPE townos_ingress_reloads_total counter",
		`townos_ingress_routes{tls="pending"} 1`,
		`townos_ingress_route_https_ready{hostname="` + fqdn + `"} 0`,
		`townos_ingress_route_changes_total{op="set"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape does not carry %q; got %d bytes:\n%s", want, len(body), truncateForLog(body))
		}
	}

	// The caddy passthrough. Reaching the child's admin API from inside the
	// shipped image is the part no unit test can stand in for, and
	// townos_ingress_caddy_up is what an operator would see if it broke.
	if !strings.Contains(body, "townos_ingress_caddy_up 1") {
		e2eDumpIngressDiag(t, container, "127.0.0.1")
		t.Errorf("the ingress could not reach its caddy child's admin metrics:\n%s", truncateForLog(body))
	}
	if !strings.Contains(body, "caddy_admin_http_requests_total") {
		t.Errorf("caddy's own families were not passed through:\n%s", truncateForLog(body))
	}
}

// TestIngressMetricsAreNotPublishedToTheLAN is the privacy assertion, made
// against the running container rather than the generated unit: the scrape names
// every hostname the box serves and flags the ones with no certificate, and
// nothing authenticates it.
func TestIngressMetricsAreNotPublishedToTheLAN(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not available")
	}
	port := requireHarnessPort(t, envIngressMetricsPort)

	container := systemd.SystemServiceContainerName("ingress")
	out, err := exec.CommandContext(context.Background(), "podman", "port", container).CombinedOutput()
	if err != nil {
		t.Skipf("podman port %s: %v (%s)", container, err, out)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		// `podman port` prints "<container-port>/tcp -> <host-ip>:<host-port>".
		host, found := strings.CutPrefix(line, port+"/tcp -> ")
		if !found {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(host), ingressctl.MetricsHost+":") {
			t.Errorf("the metrics port is published on %q, not %s only:\n%s",
				strings.TrimSpace(host), ingressctl.MetricsHost, out)
		}
	}
}

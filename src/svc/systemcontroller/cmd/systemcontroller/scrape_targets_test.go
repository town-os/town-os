// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/ingress/ingressctl"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/rolodex"
)

// newScrapeTestManager builds a rolodex manager with nothing but the fields
// MetricsAddr reads. It installs no units and starts no container: the boot
// wiring only ever asks the manager for the address it will write into
// rolodex.yml, and that answer is pure.
func newScrapeTestManager(t *testing.T, metricsPort string) *rolodex.Manager {
	t.Helper()
	dir := t.TempDir()
	return rolodex.NewManager(rolodex.Config{
		DataDir:        dir,
		UnixSocketPath: filepath.Join(dir, "rolodex.sock"),
		MetricsPort:    metricsPort,
	})
}

// TestWithScrapeTargetsSetsEveryTarget is the regression this file exists for.
// Every other test of the scrape config hands WritePrometheusConfig a Ports
// value already carrying these fields, so nothing failed when the boot never
// populated them — the box simply stopped collecting its own metrics.
func TestWithScrapeTargetsSetsEveryTarget(t *testing.T) {
	t.Parallel()
	ingressAddr := ingressctl.MetricsAddrFor(0)
	got := withScrapeTargets(monitoring.Ports{}, newScrapeTestManager(t, ""), ":5309", ingressAddr, false)

	if want := rolodex.DNSLoopback + ":" + rolodex.DefaultMetricsPort; got.RolodexMetrics != want {
		t.Errorf("RolodexMetrics = %q, want %q", got.RolodexMetrics, want)
	}
	if got.ControllerMetrics != "localhost:5309" {
		t.Errorf("ControllerMetrics = %q, want %q", got.ControllerMetrics, "localhost:5309")
	}
	if got.IngressMetrics != ingressAddr {
		t.Errorf("IngressMetrics = %q, want %q", got.IngressMetrics, ingressAddr)
	}
}

// A box with no ingress must produce no ingress job. The ingress is switched off
// by INGRESS_IMAGE="" (dev mode), and a target invented for it would sit
// permanently down — indistinguishable from a router that crashed.
func TestWithScrapeTargetsOmitsDisabledIngress(t *testing.T) {
	t.Parallel()
	got := withScrapeTargets(monitoring.Ports{}, newScrapeTestManager(t, ""), ":5309", ingressMetricsTarget(""), false)

	if got.IngressMetrics != "" {
		t.Errorf("IngressMetrics = %q, want empty with the ingress disabled", got.IngressMetrics)
	}
	if got.ControllerMetrics == "" {
		t.Error("the controller target must survive an absent ingress")
	}
}

// The ingress target must come from ingressctl rather than a string composed
// here, for the same reason the rolodex one comes from its manager: that
// function is what the unit's published port is built from too, so a relocated
// harness ingress is scraped where it actually listens — IRON RULE.
func TestIngressMetricsTargetFollowsRelocatedPort(t *testing.T) {
	t.Setenv(EnvIngressMetricsPort, "39146")

	got := ingressMetricsTarget("quay.io/town/ingress:testtag")
	if want := ingressctl.MetricsAddrFor(39146); got != want {
		t.Errorf("ingressMetricsTarget = %q, want %q", got, want)
	}
	if got == ingressctl.MetricsAddrFor(0) {
		t.Errorf("relocated ingress still scraped on the default port: %q", got)
	}
}

// The rolodex target must come from the manager, not from a port recomposed
// here: the manager is what writes the bind address into rolodex.yml, and the
// two agreeing is the whole point of MetricsAddr.
func TestWithScrapeTargetsRolodexMatchesManagerAddr(t *testing.T) {
	t.Parallel()
	mgr := newScrapeTestManager(t, "39153")
	got := withScrapeTargets(monitoring.Ports{}, mgr, ":5309", "127.0.0.1:9146", false)

	if got.RolodexMetrics != mgr.MetricsAddr() {
		t.Errorf("RolodexMetrics = %q, want the manager's own %q", got.RolodexMetrics, mgr.MetricsAddr())
	}
	if strings.Contains(got.RolodexMetrics, ":"+rolodex.DefaultMetricsPort) {
		t.Errorf("relocated rolodex still scraped on the default port: %q", got.RolodexMetrics)
	}
}

// A relocated harness instance must scrape ITSELF. A controller target pinned
// to the default port would collect whichever process happened to hold :5309 —
// a test box reporting a dev box's metrics, or neither. IRON RULE.
func TestWithScrapeTargetsFollowsRelocatedListener(t *testing.T) {
	t.Parallel()
	got := withScrapeTargets(monitoring.Ports{}, newScrapeTestManager(t, "39153"), "127.0.0.1:41337", "127.0.0.1:9146", false)

	if got.ControllerMetrics != "127.0.0.1:41337" {
		t.Errorf("ControllerMetrics = %q, want the relocated listener", got.ControllerMetrics)
	}
	if strings.Contains(got.ControllerMetrics, ":5309") {
		t.Errorf("relocated controller still scraped on the default port: %q", got.ControllerMetrics)
	}
}

// TLS on the controller's own listener means its leaf is issued by the box's
// own CA. Without the https scheme reaching the config, every scrape of the
// controller fails — and it fails as a connection error, which reads as a
// broken controller rather than a scheme mismatch.
func TestWithScrapeTargetsTLSSetsHTTPSScheme(t *testing.T) {
	t.Parallel()
	got := withScrapeTargets(monitoring.Ports{}, newScrapeTestManager(t, ""), ":5309", "127.0.0.1:9146", true)

	if got.ControllerMetricsScheme != "https" {
		t.Errorf("ControllerMetricsScheme = %q, want https", got.ControllerMetricsScheme)
	}
}

// The cleartext default must emit no scheme: https against a plaintext
// listener fails every scrape just as surely as the reverse.
func TestWithScrapeTargetsPlaintextLeavesSchemeEmpty(t *testing.T) {
	t.Parallel()
	got := withScrapeTargets(monitoring.Ports{}, newScrapeTestManager(t, ""), ":5309", "127.0.0.1:9146", false)

	if got.ControllerMetricsScheme != "" {
		t.Errorf("ControllerMetricsScheme = %q, want empty for a cleartext listener", got.ControllerMetricsScheme)
	}
}

// A scheme without a target is dead config, and setting it anyway would make an
// underivable -listen read as a TLS decision.
func TestWithScrapeTargetsNoSchemeWithoutTarget(t *testing.T) {
	t.Parallel()
	got := withScrapeTargets(monitoring.Ports{}, newScrapeTestManager(t, ""), "not-an-address", "127.0.0.1:9146", true)

	if got.ControllerMetrics != "" {
		t.Errorf("ControllerMetrics = %q, want empty for an underivable listen address", got.ControllerMetrics)
	}
	if got.ControllerMetricsScheme != "" {
		t.Errorf("ControllerMetricsScheme = %q, want empty with no target", got.ControllerMetricsScheme)
	}
}

// The three host ports the harness relocates travel in the same struct. Adding
// the scrape targets must not reset them — a clobbered Prometheus port sends
// every monitoring service back to the production defaults and collides with a
// concurrent run. IRON RULE.
func TestWithScrapeTargetsPreservesHostPorts(t *testing.T) {
	t.Parallel()
	in := monitoring.Ports{NodeExporter: "39100", Prometheus: "39090", External: "35308"}
	got := withScrapeTargets(in, newScrapeTestManager(t, "39153"), ":41337", "127.0.0.1:9146", false)

	if got.NodeExporter != in.NodeExporter || got.Prometheus != in.Prometheus || got.External != in.External {
		t.Errorf("host ports clobbered: got %+v, want the %+v that came in", got, in)
	}
}

// A nil manager means rolodex was never constructed, which is not a licence to
// invent an address for it: the job would sit permanently down and read as
// broken DNS. The controller's own target is independent and must survive.
func TestWithScrapeTargetsNilRolodexOmitsOnlyRolodex(t *testing.T) {
	t.Parallel()
	got := withScrapeTargets(monitoring.Ports{}, nil, ":5309", "127.0.0.1:9146", false)

	if got.RolodexMetrics != "" {
		t.Errorf("RolodexMetrics = %q, want empty with no manager", got.RolodexMetrics)
	}
	if got.ControllerMetrics != "localhost:5309" {
		t.Errorf("ControllerMetrics = %q, want the controller scraped regardless", got.ControllerMetrics)
	}
}

// TestWithScrapeTargetsRendersEveryJob closes the loop the individual field
// assertions leave open: it carries the boot's own Ports value all the way
// through to the YAML Prometheus actually reads. Asserting the fields alone
// would still pass if the two halves disagreed about what a populated target
// means.
func TestWithScrapeTargetsRendersEveryJob(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	mgr := newScrapeTestManager(t, "39153")
	ingressAddr := ingressctl.MetricsAddrFor(39146)
	ports := withScrapeTargets(monitoringPortsFromEnv(), mgr, "127.0.0.1:41337", ingressAddr, false)

	if err := monitoring.WritePrometheusConfig(base, ports); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(base, "monitoring", "prometheus-config", "prometheus.yml"))
	if err != nil {
		t.Fatalf("read prometheus.yml: %v", err)
	}
	raw := string(b)

	for _, want := range []string{
		`- job_name: "` + monitoring.RolodexJobName + `"`,
		`targets: ["` + mgr.MetricsAddr() + `"]`,
		`- job_name: "` + monitoring.ControllerJobName + `"`,
		`targets: ["127.0.0.1:41337"]`,
		`- job_name: "` + monitoring.IngressJobName + `"`,
		`targets: ["` + ingressAddr + `"]`,
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("prometheus.yml missing %s:\n%s", want, raw)
		}
	}
}

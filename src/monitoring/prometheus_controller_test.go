package monitoring

import (
	"strings"
	"testing"
)

// TestWritePrometheusConfigScrapesController is the ingestion contract for the
// system controller's own metrics: given its address, the generated scrape
// config must carry a job targeting exactly that address under the stable job
// name dashboards and alerts select on.
func TestWritePrometheusConfigScrapesController(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := WritePrometheusConfig(base, Ports{ControllerMetrics: "localhost:5309"}); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	got := readPrometheusConfig(t, base)
	for _, want := range []string{
		`- job_name: "` + ControllerJobName + `"`,
		`targets: ["localhost:5309"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prometheus.yml missing %s:\n%s", want, got)
		}
	}
	// The controller job must not displace the two that were always there.
	for _, want := range []string{
		`targets: ["localhost:` + PrometheusPort + `"]`,
		`targets: ["localhost:` + NodeExporterPort + `"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prometheus.yml lost a pre-existing job (%s):\n%s", want, got)
		}
	}
}

// An unset address omits the job rather than aiming it at a guessed default: a
// target nobody configured sits permanently down and reads as a broken
// controller instead of an absent scrape.
func TestWritePrometheusConfigOmitsControllerJobWhenUnset(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := WritePrometheusConfig(base, Ports{}); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	if got := readPrometheusConfig(t, base); strings.Contains(got, ControllerJobName) {
		t.Errorf("controller job emitted with no address configured:\n%s", got)
	}
}

// With TOWN_OS_TLS the controller's listener is terminated by the box's local
// CA, which this Prometheus has no reason to trust and no clean way to be
// handed. Without both the https scheme and the verification skip the job fails
// every scrape — and it fails as a TLS error, which reads as a broken
// controller rather than a trust problem.
func TestWritePrometheusConfigControllerHTTPS(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := WritePrometheusConfig(base, Ports{
		ControllerMetrics:       "localhost:5309",
		ControllerMetricsScheme: "https",
	}); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	got := readPrometheusConfig(t, base)
	for _, want := range []string{"scheme: https", "insecure_skip_verify: true"} {
		if !strings.Contains(got, want) {
			t.Errorf("prometheus.yml missing %q:\n%s", want, got)
		}
	}
}

// Plain HTTP is the default and must not emit a scheme or a tls_config block —
// an https scheme against a cleartext listener fails every scrape.
func TestWritePrometheusConfigControllerHTTPHasNoTLSBlock(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := WritePrometheusConfig(base, Ports{ControllerMetrics: "localhost:5309"}); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	got := readPrometheusConfig(t, base)
	if strings.Contains(got, "scheme: https") || strings.Contains(got, "insecure_skip_verify") {
		t.Errorf("cleartext controller got a TLS block:\n%s", got)
	}
}

// Both extra jobs must coexist: they are independent subsystems and one being
// configured must not suppress the other.
func TestWritePrometheusConfigScrapesBothRolodexAndController(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := WritePrometheusConfig(base, Ports{
		RolodexMetrics:    "127.0.0.2:9153",
		ControllerMetrics: "localhost:5309",
	}); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	got := readPrometheusConfig(t, base)
	for _, want := range []string{RolodexJobName, ControllerJobName} {
		if !strings.Contains(got, `- job_name: "`+want+`"`) {
			t.Errorf("missing job %q:\n%s", want, got)
		}
	}
}

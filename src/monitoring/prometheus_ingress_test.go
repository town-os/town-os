package monitoring

import (
	"strings"
	"testing"
)

// TestWritePrometheusConfigScrapesIngress is the ingestion contract for the
// ingress's own endpoint: given its address, the generated config must carry a
// job targeting exactly that address under the stable job name every ingress
// query selects on.
func TestWritePrometheusConfigScrapesIngress(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := WritePrometheusConfig(base, Ports{IngressMetrics: "127.0.0.1:9146"}); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	got := readPrometheusConfig(t, base)
	for _, want := range []string{
		`- job_name: "` + IngressJobName + `"`,
		`targets: ["127.0.0.1:9146"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prometheus.yml missing %s:\n%s", want, got)
		}
	}
	// The ingress job must not displace the two that are always there.
	for _, want := range []string{
		`targets: ["localhost:` + PrometheusPort + `"]`,
		`targets: ["localhost:` + NodeExporterPort + `"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prometheus.yml lost pre-existing target %s:\n%s", want, got)
		}
	}
}

// TestWritePrometheusConfigIngressRelocated covers the harness: the test box's
// ingress serves its endpoint on an ephemeral port, and a job pinned to 9146
// would scrape a concurrent dev box's ingress — or nothing — and report this
// one's routes as absent. IRON RULE.
func TestWritePrometheusConfigIngressRelocated(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := WritePrometheusConfig(base, Ports{IngressMetrics: "127.0.0.1:39146"}); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	got := readPrometheusConfig(t, base)
	if !strings.Contains(got, `targets: ["127.0.0.1:39146"]`) {
		t.Errorf("prometheus.yml does not scrape the relocated ingress:\n%s", got)
	}
	if strings.Contains(got, `targets: ["127.0.0.1:9146"]`) {
		t.Errorf("prometheus.yml still scrapes the default ingress port:\n%s", got)
	}
}

// TestWritePrometheusConfigOmitsAbsentIngress is the omit-rather-than-guess
// rule. A box can run with the ingress switched off entirely (INGRESS_IMAGE=""),
// and a job aimed at a guessed address would sit permanently down — reading as a
// router that died rather than one that was never started.
func TestWritePrometheusConfigOmitsAbsentIngress(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := WritePrometheusConfig(base, Ports{}); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	got := readPrometheusConfig(t, base)
	if strings.Contains(got, IngressJobName) {
		t.Errorf("prometheus.yml carries an ingress job with no address:\n%s", got)
	}
}

package monitoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readPrometheusConfig(t *testing.T, base string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(base, "monitoring", "prometheus-config", "prometheus.yml"))
	if err != nil {
		t.Fatalf("read prometheus.yml: %v", err)
	}
	return string(raw)
}

// TestWritePrometheusConfigScrapesRolodex is the ingestion contract: given
// rolodex's metrics address, the generated scrape config must carry a job that
// targets exactly that address under the stable job name dashboards select on.
func TestWritePrometheusConfigScrapesRolodex(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := WritePrometheusConfig(base, Ports{RolodexMetrics: "127.0.0.2:9153"}); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	got := readPrometheusConfig(t, base)
	for _, want := range []string{
		`- job_name: "` + RolodexJobName + `"`,
		`targets: ["127.0.0.2:9153"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prometheus.yml missing %s:\n%s", want, got)
		}
	}
	// The rolodex job must not displace the two that were already there.
	for _, want := range []string{
		`targets: ["localhost:` + PrometheusPort + `"]`,
		`targets: ["localhost:` + NodeExporterPort + `"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prometheus.yml lost pre-existing target %s:\n%s", want, got)
		}
	}
}

// TestWritePrometheusConfigRolodexRelocated covers the harness: a relocated
// rolodex must be scraped where it actually listens. A job pinned to 9153 while
// rolodex serves an ephemeral port would report the box's DNS as down on every
// concurrent test run — IRON RULE.
func TestWritePrometheusConfigRolodexRelocated(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := WritePrometheusConfig(base, Ports{RolodexMetrics: "127.0.0.2:39153"}); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	got := readPrometheusConfig(t, base)
	if !strings.Contains(got, `targets: ["127.0.0.2:39153"]`) {
		t.Errorf("prometheus.yml does not scrape the relocated rolodex:\n%s", got)
	}
	if strings.Contains(got, ":9153") {
		t.Errorf("prometheus.yml still references the default metrics port:\n%s", got)
	}
}

// TestWritePrometheusConfigOmitsRolodexWhenUnset asserts the job is omitted
// rather than aimed at a guessed default. A target nobody configured would sit
// permanently down and read as a broken rolodex instead of an absent one.
func TestWritePrometheusConfigOmitsRolodexWhenUnset(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := WritePrometheusConfig(base, Ports{}); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	got := readPrometheusConfig(t, base)
	if strings.Contains(got, RolodexJobName) {
		t.Errorf("prometheus.yml should carry no rolodex job when unconfigured:\n%s", got)
	}
	if strings.Count(got, "job_name") != 2 {
		t.Errorf("expected exactly the two default jobs:\n%s", got)
	}
}

// TestPortsWithDefaultsLeavesRolodexEmpty pins that RolodexMetrics is not
// defaulted. It is an address chosen by another service, not a port this stack
// binds, so there is nothing here that could legitimately guess it.
func TestPortsWithDefaultsLeavesRolodexEmpty(t *testing.T) {
	t.Parallel()
	if got := (Ports{}).withDefaults().RolodexMetrics; got != "" {
		t.Errorf("withDefaults() invented a rolodex target %q", got)
	}
	if got := (Ports{RolodexMetrics: "127.0.0.2:9153"}).withDefaults().RolodexMetrics; got != "127.0.0.2:9153" {
		t.Errorf("withDefaults() clobbered the rolodex target: %q", got)
	}
}

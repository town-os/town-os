// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package monitoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testPorts is a fully-overridden Ports value. Every field is deliberately far
// from its default so a test that accidentally asserts on a default value
// cannot pass by coincidence.
var testPorts = Ports{
	NodeExporter: "39100",
	Prometheus:   "39090",
	External:     "35308",
}

// TestPortsZeroValueIsProductionDefaults pins the contract every caller relies
// on: Ports{} must reproduce today's boot exactly. Everything that does not
// care about ports passes the zero value, so if this drifts, a real box
// silently moves its monitoring ports.
func TestPortsZeroValueIsProductionDefaults(t *testing.T) {
	t.Parallel()
	got := Ports{}.withDefaults()
	if got.NodeExporter != NodeExporterPort {
		t.Errorf("NodeExporter: got %q, want %q", got.NodeExporter, NodeExporterPort)
	}
	if got.Prometheus != PrometheusPort {
		t.Errorf("Prometheus: got %q, want %q", got.Prometheus, PrometheusPort)
	}
	if got.External != MonitoringExternalPort {
		t.Errorf("External: got %q, want %q", got.External, MonitoringExternalPort)
	}
}

// TestPortsWithDefaultsFillsOnlyEmptyFields asserts partial overrides survive:
// setting one port must not reset the other two to their defaults, and must not
// be reset itself.
func TestPortsWithDefaultsFillsOnlyEmptyFields(t *testing.T) {
	t.Parallel()
	got := Ports{Prometheus: "39090"}.withDefaults()
	if got.Prometheus != "39090" {
		t.Errorf("explicit Prometheus overwritten: got %q", got.Prometheus)
	}
	if got.NodeExporter != NodeExporterPort || got.External != MonitoringExternalPort {
		t.Errorf("unset fields not defaulted: %+v", got)
	}
}

// TestNodeExporterUnitConfigHonoursPorts asserts the override reaches the
// --web.listen-address flag and that the default port is gone. Asserting the
// default is absent is the half that catches a config which appends the new
// port while still binding the old one.
func TestNodeExporterUnitConfigHonoursPorts(t *testing.T) {
	t.Parallel()
	cmd := strings.Join(NodeExporterUnitConfig(testPorts).Command, " ")
	if !strings.Contains(cmd, "--web.listen-address=127.0.0.1:39100") {
		t.Errorf("node-exporter should bind the override port, got: %s", cmd)
	}
	if strings.Contains(cmd, ":"+NodeExporterPort) {
		t.Errorf("node-exporter still references the default port %s: %s", NodeExporterPort, cmd)
	}
}

// TestPrometheusUnitConfigHonoursPorts asserts Prometheus binds the overridden
// port rather than the 9090 that used to be baked into the unit.
func TestPrometheusUnitConfigHonoursPorts(t *testing.T) {
	t.Parallel()
	cmd := strings.Join(PrometheusUnitConfig(t.TempDir(), testPorts).Command, " ")
	if !strings.Contains(cmd, "--web.listen-address=127.0.0.1:39090") {
		t.Errorf("prometheus should bind the override port, got: %s", cmd)
	}
	if strings.Contains(cmd, ":"+PrometheusPort) {
		t.Errorf("prometheus still references the default port %s: %s", PrometheusPort, cmd)
	}
}

// TestWritePrometheusConfigHonoursBothPorts covers the scrape config, which
// names *two* ports: node-exporter's (already parameterized) and Prometheus's
// own self-scrape target, which was hardcoded to 9090. A relocated Prometheus
// that still self-scrapes :9090 loses its own metrics silently.
func TestWritePrometheusConfigHonoursBothPorts(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := WritePrometheusConfig(base, testPorts); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(base, "monitoring", "prometheus-config", "prometheus.yml"))
	if err != nil {
		t.Fatalf("read prometheus.yml: %v", err)
	}
	got := string(raw)
	for _, want := range []string{`targets: ["localhost:39090"]`, `targets: ["localhost:39100"]`} {
		if !strings.Contains(got, want) {
			t.Errorf("prometheus.yml missing %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, "localhost:"+PrometheusPort) {
		t.Errorf("self-scrape still targets the default port %s:\n%s", PrometheusPort, got)
	}
}

// TestWritePrometheusConfigZeroPortsKeepsDefaults is the counterpart: an
// unconfigured box must still scrape 9090/9100.
func TestWritePrometheusConfigZeroPortsKeepsDefaults(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := WritePrometheusConfig(base, Ports{}); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(base, "monitoring", "prometheus-config", "prometheus.yml"))
	if err != nil {
		t.Fatalf("read prometheus.yml: %v", err)
	}
	got := string(raw)
	for _, want := range []string{
		`targets: ["localhost:` + PrometheusPort + `"]`,
		`targets: ["localhost:` + NodeExporterPort + `"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prometheus.yml missing %s:\n%s", want, got)
		}
	}
}

// TestUPlotUnitConfigHonoursPorts asserts the socat forwarder listens on the
// overridden external port and dials the overridden Prometheus port. Both ends
// move independently, so both are checked.
func TestUPlotUnitConfigHonoursPorts(t *testing.T) {
	t.Parallel()
	cmd := strings.Join(UPlotUnitConfig("nc:test", testPorts).Command, " ")
	if !strings.Contains(cmd, "TCP-LISTEN:35308,") {
		t.Errorf("socat should listen on the override external port, got: %s", cmd)
	}
	if !strings.Contains(cmd, "TCP:127.0.0.1:39090") {
		t.Errorf("socat should forward to the override prometheus port, got: %s", cmd)
	}
}

// TestGrafanaUnitConfigHonoursExternalPort asserts Grafana binds the overridden
// LAN port via GF_SERVER_HTTP_PORT.
func TestGrafanaUnitConfigHonoursExternalPort(t *testing.T) {
	t.Parallel()
	args := strings.Join(GrafanaUnitConfig(t.TempDir(), testPorts).Args, " ")
	if !strings.Contains(args, "GF_SERVER_HTTP_PORT=35308") {
		t.Errorf("grafana should bind the override external port, got: %s", args)
	}
	if strings.Contains(args, "GF_SERVER_HTTP_PORT="+MonitoringExternalPort) {
		t.Errorf("grafana still binds the default external port: %s", args)
	}
}

// TestGrafanaDatasourceYAMLHonoursPrometheusPort asserts the provisioned
// datasource URL follows a relocated Prometheus. A datasource pointing at a
// port nothing listens on renders every panel as "No data" with no error.
func TestGrafanaDatasourceYAMLHonoursPrometheusPort(t *testing.T) {
	t.Parallel()
	got := GrafanaDatasourceYAML("127.0.0.1", "39090")
	if !strings.Contains(got, "url: http://127.0.0.1:39090") {
		t.Errorf("datasource should use the override port:\n%s", got)
	}
	if def := GrafanaDatasourceYAML("127.0.0.1", ""); !strings.Contains(def, "url: http://127.0.0.1:"+PrometheusPort) {
		t.Errorf("empty port should fall back to %s:\n%s", PrometheusPort, def)
	}
}

// TestWriteGrafanaProvisioningFilesHonoursPorts asserts the port reaches the
// file actually written to disk, not just the string builder.
func TestWriteGrafanaProvisioningFilesHonoursPorts(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := WriteGrafanaProvisioningFiles(base, []string{"sda3"}, testPorts); err != nil {
		t.Fatalf("WriteGrafanaProvisioningFiles: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(base, "monitoring", "grafana-provisioning", "datasources", "prometheus.yml"))
	if err != nil {
		t.Fatalf("read datasource: %v", err)
	}
	if !strings.Contains(string(raw), "http://127.0.0.1:39090") {
		t.Errorf("provisioned datasource ignores the override port:\n%s", raw)
	}
}

// TestSystemServiceMetadataHonoursPorts asserts /system-services reports the
// ports the units actually bind. Reporting 9090 for a Prometheus on 39090
// would send the UI (and an operator reading the panel) to a dead port.
func TestSystemServiceMetadataHonoursPorts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"node-exporter", NodeExporterSystemService(testPorts).Port, "39100"},
		{"prometheus", PrometheusSystemService(testPorts).Port, "39090"},
		{"monitoring-ui", MonitoringUISystemService(BackendUPlot, "nc:test", testPorts).Port, "35308"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: reported port %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/ingress/ingressctl"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// promQueryResult is the slice of Prometheus's query API this test reads: the
// job label that identifies the target, and the sample value of `up`.
type promQueryResult struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Value  []json.RawMessage `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// TestPrometheusScrapesControllerAndRolodex is the end-to-end proof that the
// box collects its own metrics: a real Prometheus, started from the config this
// repo generates, scraping a real rolodex container and a real system
// controller, asserted through `up` — Prometheus's own verdict on whether the
// scrape succeeded.
//
// Every cheaper test stops short of this. The unit tests prove the YAML names
// the jobs; promtool proves Prometheus will parse it. Neither can catch a job
// that parses fine and then fails every scrape — a target that resolves to
// nothing, an endpoint behind auth that answers 403, an https/cleartext
// mismatch — and that failure is invisible in exactly the way that matters:
// Prometheus boots, the dashboards render, and the two series are simply never
// there.
func TestPrometheusScrapesControllerAndRolodex(t *testing.T) {
	t.Parallel()

	// The controller under test. httptest binds 127.0.0.1 on an ephemeral
	// port, which a --net host Prometheus reaches over the same loopback it
	// reaches node-exporter on — no podman-network hop, exactly like a real
	// box. /metrics is localhost-or-admin, and a scrape from 127.0.0.1 is
	// what makes it reachable without a token; a Prometheus on a bridge
	// network would arrive from another address and be refused.
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: storage.InitBtrFSMock()})
	t.Cleanup(ts.Close)
	controllerTarget := hostPortFromURL(t, ts.Server.URL)

	// The rolodex under test, started the same way the real boot starts it.
	rolodexTarget := startRolodexForScrape(t)

	// The config is the one the product writes, not a fixture: this test is
	// worthless if it validates YAML the box would never produce.
	base := prometheusScrapeDir(t)
	promPort := findFreePort(t)
	if err := monitoring.WritePrometheusConfig(base, monitoring.Ports{
		Prometheus: promPort,
		// node-exporter is not started here; its job is expected to be down
		// and is deliberately not asserted on. Pointing it at a free port
		// keeps it from scraping whatever else holds 9100 in the host
		// namespace during a concurrent run. IRON RULE.
		NodeExporter:      findFreePort(t),
		RolodexMetrics:    rolodexTarget,
		ControllerMetrics: controllerTarget,
	}); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}

	startPrometheusForScrape(t, base, promPort)

	// Poll rather than sleep: the config's scrape_interval is 15s, so the
	// first scrape of each target lands somewhere inside the first interval
	// after Prometheus finishes starting.
	up := waitForScrapedJobs(t, promPort,
		monitoring.ControllerJobName,
		monitoring.RolodexJobName,
	)

	for _, job := range []string{monitoring.ControllerJobName, monitoring.RolodexJobName} {
		if up[job] != "1" {
			t.Errorf("up{job=%q} = %q, want 1 — Prometheus is not scraping it", job, up[job])
		}
	}
}

// TestPrometheusConfigWithAllJobsIsValid runs promtool over the config carrying
// every job a real boot emits, including the https/tls_config block the TLS
// branch adds.
//
// It is separate from the scrape test because it catches a different failure
// and catches it harder: Prometheus does not skip a stanza it cannot parse, it
// exits. So a malformed tls_config does not cost the controller job — it takes
// down node-exporter, rolodex, and every dashboard on the box. The existing
// promtool test covers the rolodex job only, which leaves the two branches
// added since then unvalidated.
func TestPrometheusConfigWithAllJobsIsValid(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		scheme string
	}{
		{name: "cleartext"},
		{name: "https", scheme: "https"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base := t.TempDir()
			if err := monitoring.WritePrometheusConfig(base, monitoring.Ports{
				RolodexMetrics:          rolodex.DNSLoopback + ":" + findFreePort(t),
				ControllerMetrics:       "localhost:" + findFreePort(t),
				ControllerMetricsScheme: tc.scheme,
				IngressMetrics:          ingressctl.MetricsHost + ":" + findFreePort(t),
			}); err != nil {
				t.Fatalf("WritePrometheusConfig: %v", err)
			}
			promtoolCheckConfig(t, filepath.Join(base, "monitoring", "prometheus-config"))
		})
	}
}

// testContext returns a context bounded by the smaller of the supplied budget
// and the test's own deadline, so a hung container fails as this test's own
// timeout rather than as the whole package's.
func testContext(t *testing.T, budget time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	t.Cleanup(cancel)
	if dl, ok := t.Deadline(); ok {
		ctx, cancel = context.WithDeadline(ctx, dl.Add(-15*time.Second))
		t.Cleanup(cancel)
	}
	return ctx
}

// hostPortFromURL reduces a base URL to the "host:port" a scrape target is.
func hostPortFromURL(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse test server URL %q: %v", raw, err)
	}
	return u.Host
}

// prometheusScrapeDir returns a disk-backed directory for Prometheus's config
// and TSDB. It is deliberately NOT t.TempDir(): /tmp is tmpfs on Arch, Manjaro,
// and Fedora, and nothing this suite writes may land on RAM-backed storage.
// /town-os is the btrfs mount the integration container already carries.
func prometheusScrapeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/town-os", "prom-scrape-*") //nolint:usetesting // /tmp may be tmpfs; see comment
	if err != nil {
		t.Fatalf("MkdirTemp under /town-os: %v", err)
	}
	t.Cleanup(func() {
		logCleanupf(t, os.RemoveAll(dir), "RemoveAll %s", dir)
	})
	return dir
}

// startRolodexForScrape starts a real rolodex container from the config this
// repo renders and returns the address Prometheus should scrape — the
// manager's own MetricsAddr, never a recomposed one, since the two agreeing is
// the property under test.
func startRolodexForScrape(t *testing.T) string {
	t.Helper()

	dataDir := rolodexTempDir(t, "rolodex-scrape-*")
	sd := systemd.NewManager()
	key := rolodexTestKey()

	dnsPort := findFreePort(t)
	metricsPort := findFreePort(t)
	mgr := rolodex.NewManager(rolodex.Config{
		Systemd:        sd,
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: filepath.Join(dataDir, "rolodex.sock"),
		DNSPort:        dnsPort,
		MetricsPort:    metricsPort,
		Key:            key,
	})
	writeRolodexBootstrapConfig(t, dataDir, dnsPort, metricsPort)

	startRolodexUnit(t, sd, key, dataDir, "Rolodex DNS (scrape test)")

	return mgr.MetricsAddr()
}

// startPrometheusForScrape runs a real Prometheus against the generated config.
//
// It runs podman directly rather than through a systemd unit because the unit
// generator pins the container name and the data path per key, and this test
// needs neither — what it needs is a throwaway Prometheus on an ephemeral port
// that cannot collide with the box's own. --replace is mandatory repo-wide.
func startPrometheusForScrape(t *testing.T, base, port string) {
	t.Helper()

	ctx := testContext(t, 3*time.Minute)

	ensureImagePulled(monitoring.PrometheusImage)

	configDir := filepath.Join(base, "monitoring", "prometheus-config")
	dataDir := filepath.Join(base, "monitoring", "prometheus-data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create prometheus data dir: %v", err)
	}

	name := "town-os-prom-scrape-" + strings.TrimPrefix(filepath.Base(base), "prom-scrape-")

	// --user 0:0 for the same reason the promtool test needs it: the image
	// declares USER nobody and the bind-mount source is root-owned, and a bind
	// mount passes host ownership straight through.
	cmd := exec.CommandContext(ctx, "podman", "run", "-d", "--replace",
		"--name", name,
		"--net", "host",
		"--user", "0:0",
		"-v", configDir+":/etc/prometheus:ro",
		"-v", dataDir+":/prometheus",
		monitoring.PrometheusImage,
		"--config.file=/etc/prometheus/prometheus.yml",
		"--storage.tsdb.path=/prometheus",
		"--web.listen-address=127.0.0.1:"+port,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("start prometheus (%v):\n%s", err, out)
	}

	t.Cleanup(func() {
		// Cleanup is exempt from the never-swallow-exit-codes rule.
		_ = exec.Command("podman", "rm", "-f", name).Run() //nolint:noctx // best-effort cleanup
	})
}

// waitForScrapedJobs polls Prometheus's query API until every named job has an
// `up` sample, and returns the value of each. Waiting for the sample to EXIST
// separates "not scraped yet" from "scraped and failed" — without it a run that
// polls once during the first scrape interval reports every target down and
// looks exactly like the bug this test is for.
func waitForScrapedJobs(t *testing.T, port string, jobs ...string) map[string]string {
	t.Helper()

	ctx := testContext(t, 3*time.Minute)

	queryURL := "http://127.0.0.1:" + port + "/api/v1/query?query=up"
	deadline := time.Now().Add(2 * time.Minute)
	var last map[string]string
	var lastErr error

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Fatalf("context cancelled waiting for scrapes: %v", ctx.Err())
		case <-time.After(2 * time.Second):
		}

		got, err := queryUp(ctx, queryURL)
		if err != nil {
			lastErr = err
			continue
		}
		last, lastErr = got, nil

		complete := true
		for _, job := range jobs {
			if _, ok := got[job]; !ok {
				complete = false
			}
		}
		if complete {
			return got
		}
	}

	t.Fatalf("timed out waiting for jobs %v to report up; last=%v lastErr=%v", jobs, last, lastErr)
	return nil
}

// queryUp runs the `up` query and returns job label -> sample value.
func queryUp(ctx context.Context, queryURL string) (result map[string]string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build query request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query prometheus: %w", err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read query response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus query returned %d: %s", resp.StatusCode, truncateForLog(string(body)))
	}

	var parsed promQueryResult
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode query response: %w", err)
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("prometheus query status %q", parsed.Status)
	}

	out := make(map[string]string, len(parsed.Data.Result))
	for _, r := range parsed.Data.Result {
		job, ok := r.Metric["job"]
		if !ok {
			continue
		}
		// A sample is [timestamp, "value"]; the value is a JSON string.
		if len(r.Value) != 2 {
			continue
		}
		var v string
		if err := json.Unmarshal(r.Value[1], &v); err != nil {
			continue
		}
		out[job] = v
	}
	return out, nil
}

// promtoolCheckConfig runs promtool over a generated config directory.
func promtoolCheckConfig(t *testing.T, configDir string) {
	t.Helper()

	ensureImagePulled(monitoring.PrometheusImage)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// --entrypoint and --user 0:0 are both required; see the equivalent call in
	// systemcontroller_rolodex_metrics_test.go for why.
	out, err := exec.CommandContext(ctx, "podman", "run", "--rm",
		"--net", "host",
		"--user", "0:0",
		"--entrypoint", "/bin/promtool",
		"-v", configDir+":/etc/prometheus:ro",
		monitoring.PrometheusImage,
		"check", "config", "/etc/prometheus/prometheus.yml",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("promtool rejected the generated config (%v):\n%s", err, out)
	}
	if !strings.Contains(string(out), "SUCCESS") {
		t.Errorf("promtool did not report success:\n%s", out)
	}
}

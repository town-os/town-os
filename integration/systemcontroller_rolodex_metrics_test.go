// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/systemd"
)

// TestRolodexServesPrometheusMetrics is the end-to-end half of the ingestion
// change: it starts a real rolodex container from the config this repo renders
// and scrapes the endpoint the way Prometheus will.
//
// The unit tests can only prove the YAML says what we meant. They cannot prove
// rolodex *accepts* the `metrics:` key or that the listener actually comes up on
// the address we tell Prometheus to scrape — and that is exactly the failure
// mode that would ship silently, because a rolodex that ignores the section
// boots perfectly and serves DNS perfectly while the scrape target sits down.
func TestRolodexServesPrometheusMetrics(t *testing.T) {
	t.Parallel()

	dataDir := rolodexTempDir(t, "rolodex-metrics-*")
	sd := systemd.NewManager()
	key := rolodexTestKey()
	// Both listeners get their own ephemeral port: they are separate sockets in
	// the host namespace, and the test container runs --net host.
	dnsPort := findFreePort(t)
	metricsPort := findFreePort(t)

	mgr := rolodex.NewManager(pinRolodexDiscovery(t, rolodex.Config{
		Systemd:        sd,
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: filepath.Join(dataDir, "rolodex.sock"),
		DNSPort:        dnsPort,
		MetricsPort:    metricsPort,
		Key:            key,
	}))

	writeRolodexBootstrapConfig(t, dataDir, dnsPort, metricsPort)

	ctx := context.Background()
	if dl, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, dl.Add(-15*time.Second))
		t.Cleanup(cancel)
	}

	unitName := systemd.SystemServiceUnitName(key)
	_ = sd.SetStatus(ctx, unitName, systemd.Stop)
	_ = sd.UninstallUnit(ctx, unitName)

	uf := systemd.GenerateSystemServiceUnit(systemd.SystemServiceUnitConfig{
		Key:         key,
		Description: "Rolodex DNS (metrics test)",
		Image:       rolodexTestImage(),
		Args: []string{
			"--net", "host",
			"-v", dataDir + ":/data",
		},
		Command:    []string{"/usr/local/bin/rolodex-dns", "--config", "/data/rolodex.yml"},
		VolumeDirs: []string{dataDir},
	})

	if err := sd.InstallUnit(ctx, uf.Name, uf.Content); err != nil {
		t.Fatalf("InstallUnit: %v", err)
	}
	if err := sd.SetStatus(ctx, uf.Name, systemd.Enable); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := sd.SetStatus(ctx, uf.Name, systemd.Restart); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		logCleanupf(t, sd.SetStatus(cleanupCtx, unitName, systemd.Stop), "SetStatus(stop)")
		logCleanupf(t, sd.UninstallUnit(cleanupCtx, unitName), "UninstallUnit")
	})

	// Scrape the address the Prometheus job is built from, not a recomposed
	// one — that equality is the whole point of MetricsAddr.
	scrapeURL := "http://" + mgr.MetricsAddr() + "/metrics"
	body := scrapeUntilReady(ctx, t, scrapeURL, dataDir, key)

	// rolodex_dns_build_info is emitted by every version that has the endpoint
	// at all, so it proves the listener is rolodex's and not something else
	// that happened to be on the port.
	for _, want := range []string{"rolodex_dns_build_info", "rolodex_dns_queries_total"} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape did not carry %s; got %d bytes:\n%s", want, len(body), truncateForLog(body))
		}
	}

	// Every metric family the DNS dashboards query must actually be
	// exported by the rolodex image this repo pins. A panel naming a
	// family the daemon does not emit is invisible: Grafana and the uPlot
	// frontend both render an empty chart, which is exactly what an idle
	// resolver looks like — so the failure ships silently and is only
	// noticed by whoever needed the panel during an outage.
	//
	// Matched on the TYPE line rather than by substring, so a family whose
	// name is a prefix of another cannot vouch for a missing one.
	for _, family := range monitoring.RolodexDashboardMetrics() {
		if !strings.Contains(body, "# TYPE "+family+" ") {
			t.Errorf("rolodex does not export %s, which the DNS dashboards query", family)
		}
	}
}

// TestPrometheusConfigWithRolodexIsValid runs promtool over the config this
// repo generates. A scrape config Prometheus refuses to parse takes down ALL
// monitoring, not just the rolodex job — Prometheus exits rather than skipping
// the bad stanza — so the generated YAML being well-formed is load-bearing for
// metrics that were working before this change.
func TestPrometheusConfigWithRolodexIsValid(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	if err := monitoring.WritePrometheusConfig(base, monitoring.Ports{
		RolodexMetrics: rolodex.DNSLoopback + ":" + findFreePort(t),
	}); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	configDir := filepath.Join(base, "monitoring", "prometheus-config")

	ensureImagePulled(monitoring.PrometheusImage)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// --entrypoint is required, not stylistic: the upstream Prometheus image
	// declares ENTRYPOINT=/bin/prometheus, so a trailing command is appended as
	// server arguments rather than replacing it, and prometheus exits with
	// "unexpected /bin/promtool". Town OS's own images use CMD precisely so
	// this is unnecessary; this one is not ours.
	//
	// --user 0:0 is required for the same reason: the image declares
	// USER nobody, and the mount source lives under t.TempDir(), which is 0700
	// and owned by root inside the test container — a bind mount passes host
	// ownership straight through, so nobody cannot even traverse into it.
	// Widening the temp dir instead would be the wrong trade: this container
	// reads one file and exits.
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

// scrapeUntilReady polls the metrics endpoint until it answers 200. The
// container has to start and bind before anything is there to scrape, so a
// single GET would race the unit; a failure at the deadline dumps the unit's
// journal, since "rolodex refused the config key" and "rolodex is still
// starting" look identical from the client side.
func scrapeUntilReady(ctx context.Context, t *testing.T, url, dataDir, key string) string {
	t.Helper()

	deadline := time.Now().Add(90 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Fatalf("context cancelled while waiting for %s: %v", url, ctx.Err())
		default:
		}

		body, err := scrapeOnce(ctx, url)
		if err == nil {
			return body
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}

	dumpRolodexDiagnostics(ctx, t, dataDir, key)
	if raw, err := os.ReadFile(filepath.Join(dataDir, "rolodex.yml")); err == nil {
		t.Logf("rolodex.yml:\n%s", raw)
	}
	t.Fatalf("no successful scrape of %s before deadline: %v", url, lastErr)
	return ""
}

func scrapeOnce(ctx context.Context, url string) (out string, err error) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	// A dedicated client so a connection to a listener that is not up yet
	// cannot linger in the shared default transport's pool.
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext:       (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
			DisableKeepAlives: true,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if readErr != nil {
		return "", readErr
	}
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("scrape returned status " + resp.Status)
	}
	return string(body), nil
}

// truncateForLog keeps a failed-scrape dump readable: the exposition format
// runs to tens of kilobytes and the interesting part is the head.
func truncateForLog(s string) string {
	const limit = 2048
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "\n... (truncated)"
}

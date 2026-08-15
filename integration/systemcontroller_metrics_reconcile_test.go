// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// TestPrometheusScrapesTheSchemeTheListenerActuallySpeaks is the end-to-end
// proof for the other half: the controller job must be written with the scheme
// the socket answers, not the one the boot believed it had configured.
//
// The "cleartext" case is the one that shipped. prometheus.yml carried
// `scheme: https` against a plain :5309, so Prometheus opened a TLS connection,
// got a plaintext HTTP response, and failed every scrape — for the life of the
// config, with the only trace a job sitting `down` in Prometheus's own target
// list, which nothing on the box surfaces. `insecure_skip_verify` does not help:
// it relaxes certificate validation, and there was no handshake at all.
//
// The "tls" case guards the repair from overcorrecting. Prometheus has never
// actually scraped through the tls_config block — promtool only proves it
// parses — so a probe that reported every listener cleartext would "fix" the
// bug by breaking TLS boxes just as silently.
func TestPrometheusScrapesTheSchemeTheListenerActuallySpeaks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// tlsListener is what the socket really does; configured is what the
		// boot thought it had set up. They disagree in the cleartext case,
		// which is precisely the deployed state.
		tlsListener bool
		configured  bool
	}{
		{name: "cleartext-listener-with-tls-configured", tlsListener: false, configured: true},
		{name: "tls-listener", tlsListener: true, configured: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			target := startMetricsListener(t, tc.tlsListener)

			ctx := testContext(t, 3*time.Minute)
			observed := systemcontroller.ListenerSpeaksTLS(ctx, target, tc.configured)
			if observed != tc.tlsListener {
				t.Fatalf("ListenerSpeaksTLS = %v, want %v — the probe misread the socket", observed, tc.tlsListener)
			}

			scheme := ""
			if observed {
				scheme = "https"
			}

			base := prometheusScrapeDir(t)
			promPort := findFreePort(t)
			if err := monitoring.WritePrometheusConfig(base, monitoring.Ports{
				Prometheus: promPort,
				// Not started; its job is expected down and is not asserted on.
				// A free port keeps it off whatever else holds 9100 in the host
				// namespace during a concurrent run. IRON RULE.
				NodeExporter:            findFreePort(t),
				ControllerMetrics:       target,
				ControllerMetricsScheme: scheme,
			}); err != nil {
				t.Fatalf("WritePrometheusConfig: %v", err)
			}

			startPrometheusForScrape(t, base, promPort)

			up := waitForScrapedJobs(t, promPort, monitoring.ControllerJobName)
			got, ok := up[monitoring.ControllerJobName]
			if !ok {
				t.Fatalf("no up sample for job %q", monitoring.ControllerJobName)
			}
			if got != "1" {
				t.Errorf("up{job=%q} = %q, want 1 — Prometheus could not scrape the %s listener over %q",
					monitoring.ControllerJobName, got, tc.name, scheme)
			}
		})
	}
}

// startMetricsListener returns the "host:port" of a listener serving the
// controller's /metrics route.
//
// The cleartext arm is the real controller — the exact server, router and
// localhost-or-admin rule a box runs, since that is the arm reproducing the
// deployed failure. The TLS arm is an httptest TLS server over a minimal
// exposition body: what it has to prove is that Prometheus completes a
// handshake and a scrape through the generated tls_config, and the controller's
// metric families are already covered by TestPrometheusScrapesControllerAndRolodex.
func startMetricsListener(t *testing.T, useTLS bool) string {
	t.Helper()

	if !useTLS {
		ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: storage.InitBtrFSMock()})
		t.Cleanup(ts.Close)
		return hostPortFromURL(t, ts.Server.URL)
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		if _, err := w.Write([]byte("# TYPE townos_scheme_probe gauge\ntownos_scheme_probe 1\n")); err != nil {
			t.Errorf("write exposition: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return hostPortFromURL(t, srv.URL)
}

// startRolodexUnit installs, enables and starts a rolodex system-service unit
// over dataDir, waits until the server inside answers, and tears it all down at
// the end of the test.
//
// The wait is the part that is easy to leave out and expensive to leave out.
// `systemctl restart` returns when systemd has started the *podman* command, and
// what the caller wants is a rolodex that has read its config, opened its
// database and bound its gRPC socket — a second or so later. Without the wait,
// the first thing every caller does (dial the socket, stat it for a generation,
// query the resolver) lands in that gap and fails on a box that is merely slow,
// which reads as a broken feature rather than a premature test.
//
// It derives its own context rather than taking one: rolodexTestImage may pull,
// and both that and the teardown outlive any context a caller would hand in.
func startRolodexUnit(t *testing.T, sd systemd.Manager, key, dataDir, description string) {
	t.Helper()

	ctx := testContext(t, 3*time.Minute)

	uf := systemd.GenerateSystemServiceUnit(systemd.SystemServiceUnitConfig{
		Key:         key,
		Description: description,
		Image:       rolodexTestImage(),
		Args: []string{
			"--net", "host",
			"-v", dataDir + ":/data",
		},
		Command:    []string{"/usr/local/bin/rolodex-dns", "--config", "/data/rolodex.yml"},
		VolumeDirs: []string{dataDir},
	})

	if err := sd.InstallUnit(ctx, uf.Name, uf.Content); err != nil {
		t.Fatalf("rolodex InstallUnit: %v", err)
	}
	if err := sd.SetStatus(ctx, uf.Name, systemd.Enable); err != nil {
		t.Fatalf("rolodex Enable: %v", err)
	}
	if err := sd.SetStatus(ctx, uf.Name, systemd.Restart); err != nil {
		t.Fatalf("rolodex Restart: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		logCleanupf(t, sd.SetStatus(cleanupCtx, uf.Name, systemd.Stop), "rolodex stop")
		logCleanupf(t, sd.UninstallUnit(cleanupCtx, uf.Name), "rolodex uninstall")
	})

	// The socket the config above names, as the HOST sees it: the container's
	// /data is this dataDir.
	client := waitForRolodexClient(t, ctx, filepath.Join(dataDir, "rolodex.sock"), dataDir, key)
	logCleanupf(t, client.Close(), "close readiness client")
}

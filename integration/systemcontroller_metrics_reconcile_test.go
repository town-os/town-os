// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// legacyRolodexConfig is the rendering a pre-9689461 controller wrote: correct
// for its day, and missing both sections added since. It is spelled out
// verbatim rather than generated, because generating it from today's template
// is exactly the assumption under test.
const legacyRolodexConfig = `database_path: /data/rolodex.db
dns:
  bind:
    - udp: "%s:%s"
    - tcp: "%s:%s"
grpc:
  tcp_bind: ""
  unix_socket: /data/rolodex.sock
  shared_secret: ""
forwarders:
  - "8.8.8.8:53"
  - "8.8.4.4:53"
resolution:
  mode: auto
`

// TestUpgradedControllerOpensRolodexMetricsListener is the end-to-end proof for
// the freeze that cost a deployed box every DNS panel it had.
//
// A rolodex.yml written by an older controller sits on disk, newer than the
// binary that finds it — which is the state EVERY boot leaves behind, since the
// file a boot writes is always newer than the image that wrote it. WriteConfig
// used to treat that as a hand edit and skip, so the file froze at its first
// rendering for the life of the box and no image update could move it. rolodex
// was never told to open its Prometheus listener, Prometheus scraped
// 127.0.0.2:9153 every 15s for weeks, nothing was ever bound there, and the DNS
// dashboards rendered empty charts rather than errors.
//
// Nothing cheaper catches this. The template has emitted `metrics:` since
// 9689461 and every unit test of it passes; the bug lives entirely in the
// decision not to write the file, and its symptom is a listener that never
// opens. So this test starts a real rolodex from the reconciled config and
// scrapes it.
func TestUpgradedControllerOpensRolodexMetricsListener(t *testing.T) {
	t.Parallel()

	dataDir := rolodexTempDir(t, "rolodex-upgrade-*")
	sd := systemd.NewManager()
	key := rolodexTestKey()
	dnsPort := findFreePort(t)
	metricsPort := findFreePort(t)

	// Plant the legacy config, then age it FORWARD. A file in the future is
	// newer than any binary, which is what the old guard keyed on.
	configPath := filepath.Join(dataDir, "rolodex.yml")
	legacy := fmt.Sprintf(legacyRolodexConfig,
		rolodex.DNSLoopback, dnsPort, rolodex.DNSLoopback, dnsPort)
	if err := os.WriteFile(configPath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("plant legacy rolodex.yml: %v", err)
	}
	future := time.Now().Add(24 * time.Hour)
	if err := os.Chtimes(configPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	mgr := rolodex.NewManager(rolodex.Config{
		Systemd:        sd,
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: filepath.Join(dataDir, "rolodex.sock"),
		DNSPort:        dnsPort,
		MetricsPort:    metricsPort,
		Key:            key,
	})

	// The boot-time entry point, unchanged from what main.go calls.
	written, err := mgr.WriteConfig()
	if err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	// The boolean is load-bearing, not decoration: boot restarts rolodex only
	// when the file changed, so a false here means the new listener would not
	// come up until something else happened to bounce the unit.
	if !written {
		t.Fatal("WriteConfig left the legacy config in place; rolodex will never open its metrics listener")
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read reconciled rolodex.yml: %v", err)
	}
	if want := "metrics:\n  bind: \"" + mgr.MetricsAddr() + "\"\n"; !strings.Contains(string(raw), want) {
		t.Fatalf("reconciled config missing %q:\n%s", want, raw)
	}

	startRolodexUnit(t, sd, key, dataDir, "Rolodex DNS (upgrade test)")

	ctx := testContext(t, 3*time.Minute)

	// Scrape MetricsAddr itself — the same string the config was rendered from
	// and the same string the Prometheus job carries. Recomposing it here would
	// let the two drift and still pass.
	body := scrapeUntilReady(ctx, t, "http://"+mgr.MetricsAddr()+"/metrics", dataDir, key)
	if !strings.Contains(body, "rolodex_dns_build_info") {
		t.Errorf("scrape did not carry rolodex_dns_build_info; got %d bytes:\n%s", len(body), truncateForLog(body))
	}
}

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
// over dataDir, and tears it down at the end of the test.
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
}

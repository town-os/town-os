// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"slices"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// TestMonitoringStatusReportsADownScrapeJob is the end-to-end proof for the one
// failure this box keeps shipping and never surfacing: a scrape job that has
// never worked.
//
// Both metrics bugs fixed so far had every unit active, an empty `systemctl
// --failed`, and a dashboard drawing an empty chart — which is exactly what an
// idle service looks like. The only place the difference existed was
// Prometheus's own target list, which nothing outside Prometheus ever read.
//
// So this runs a REAL Prometheus, scraping a real system controller that
// answers and a node-exporter job pointed at a port with nothing behind it, and
// asks the controller's own `/monitoring/status` which jobs are down. A unit
// test can prove the JSON is parsed; only this can prove the endpoint reports a
// live Prometheus's verdict, through the same code path a box runs.
func TestMonitoringStatusReportsADownScrapeJob(t *testing.T) {
	t.Parallel()

	promPort := findFreePort(t)
	// Nothing is started here: this is the job that must come back down, with
	// Prometheus's own reason attached.
	deadPort := findFreePort(t)

	// The mock systemd reports the monitoring units active, which is what the
	// handler gates the target query on. Prometheus itself runs from podman
	// below rather than from a unit, for the reason startPrometheusForScrape
	// gives: a unit pins the container name and data path per key, and this
	// test needs a throwaway on an ephemeral port.
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("monitoring-ui"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:           storage.InitBtrFSMock(),
		Systemd:           sd,
		MonitoringBackend: monitoring.BackendUPlot,
		MonitoringPorts:   monitoring.Ports{Prometheus: promPort, NodeExporter: deadPort},
		// ScrapeTargetsFunc is deliberately left nil: the real query against
		// the real Prometheus below is the thing under test.
	})
	t.Cleanup(ts.Close)
	controllerTarget := hostPortFromURL(t, ts.Server.URL)

	base := prometheusScrapeDir(t)
	if err := monitoring.WritePrometheusConfig(base, monitoring.Ports{
		Prometheus:        promPort,
		NodeExporter:      deadPort,
		ControllerMetrics: controllerTarget,
	}); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	startPrometheusForScrape(t, base, promPort)

	// Wait for both jobs to have been scraped at least once — "not scraped
	// yet" and "scraped and failed" are different answers, and polling before
	// the first interval would report every job down.
	waitForScrapedJobs(t, promPort, monitoring.ControllerJobName, "node-exporter")

	ctx := testContext(t, 3*time.Minute)
	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	status, err := c.MonitoringStatus(ctx)
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	if status.ScrapeTargetsError != "" {
		t.Fatalf("could not read Prometheus's targets: %s", status.ScrapeTargetsError)
	}
	if !slices.Contains(status.DownJobs, "node-exporter") {
		t.Errorf("down_jobs = %v, want it to include node-exporter (nothing is listening on its port)", status.DownJobs)
	}
	if slices.Contains(status.DownJobs, monitoring.ControllerJobName) {
		t.Errorf("down_jobs = %v, but the controller is being scraped successfully", status.DownJobs)
	}

	var sawController, sawDeadWithReason bool
	for _, target := range status.ScrapeTargets {
		switch target.Job {
		case monitoring.ControllerJobName:
			sawController = true
			if target.Down() {
				t.Errorf("the controller job is down: %s", target.LastError)
			}
		case "node-exporter":
			if target.Down() && target.LastError != "" {
				sawDeadWithReason = true
			}
		}
	}
	if !sawController {
		t.Errorf("no target for job %q; the endpoint reported %+v", monitoring.ControllerJobName, status.ScrapeTargets)
	}
	if !sawDeadWithReason {
		t.Error("the down job carried no error message; 'down' with no reason still leaves the operator opening Prometheus by hand")
	}
}

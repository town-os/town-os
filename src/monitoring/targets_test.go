package monitoring

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// promTargetsFixture is a Prometheus /api/v1/targets document with one healthy
// job, one failing job, and one Prometheus has not reached yet — the three
// states an operator can be in.
const promTargetsFixture = `{
  "status": "success",
  "data": {
    "activeTargets": [
      {
        "labels": {"job": "systemcontroller", "instance": "127.0.0.1:5309"},
        "scrapeUrl": "https://127.0.0.1:5309/metrics",
        "health": "up",
        "lastError": "",
        "lastScrape": "2026-08-15T10:00:00.000Z"
      },
      {
        "labels": {"job": "rolodex", "instance": "127.0.0.2:9153"},
        "scrapeUrl": "http://127.0.0.2:9153/metrics",
        "health": "down",
        "lastError": "Get \"http://127.0.0.2:9153/metrics\": dial tcp 127.0.0.2:9153: connect: connection refused",
        "lastScrape": "2026-08-15T10:00:00.000Z"
      },
      {
        "labels": {"job": "node-exporter", "instance": "127.0.0.1:9100"},
        "scrapeUrl": "http://127.0.0.1:9100/metrics",
        "health": "",
        "lastError": ""
      }
    ]
  }
}`

// targetsServer stands in for Prometheus on the loopback address
// FetchScrapeTargets queries, and returns the Ports that point at it.
//
// httptest picks an ephemeral port on 127.0.0.1, so nothing here claims a
// well-known port in the host namespace. IRON RULE.
func targetsServer(t *testing.T, handler http.HandlerFunc) Ports {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server URL %q: %v", srv.URL, err)
	}
	return Ports{Prometheus: u.Port()}
}

func targetsContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestFetchScrapeTargetsReadsHealthAndTheReason is the contract: a failing job
// comes back named, with Prometheus's own message for why. Without the message
// the endpoint reports a fact with no next step — the operator still has to
// open Prometheus, which is what this exists to avoid.
func TestFetchScrapeTargetsReadsHealthAndTheReason(t *testing.T) {
	t.Parallel()

	var gotPath string
	ports := targetsServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(promTargetsFixture)); err != nil {
			t.Errorf("write fixture: %v", err)
		}
	})

	targets, err := FetchScrapeTargets(targetsContext(t), nil, ports)
	if err != nil {
		t.Fatalf("FetchScrapeTargets: %v", err)
	}
	if gotPath != "/api/v1/targets" {
		t.Errorf("queried %q, want /api/v1/targets", gotPath)
	}
	if len(targets) != 3 {
		t.Fatalf("got %d targets, want 3: %+v", len(targets), targets)
	}

	// Sorted by job, so this order is the contract rather than Prometheus's
	// internal one: node-exporter, rolodex, systemcontroller.
	if targets[0].Job != "node-exporter" || targets[1].Job != RolodexJobName || targets[2].Job != ControllerJobName {
		t.Fatalf("targets are not sorted by job: %s, %s, %s", targets[0].Job, targets[1].Job, targets[2].Job)
	}

	// A target Prometheus has not scraped yet reports an empty health, which
	// must become "unknown" and NOT count as down — every target is in that
	// state for the first interval after a restart.
	if got := targets[0].Health; got != TargetHealthUnknown {
		t.Errorf("unscraped target health = %q, want %q", got, TargetHealthUnknown)
	}
	if targets[0].Down() {
		t.Error("a target Prometheus has not reached yet reported as down")
	}

	if !targets[1].Down() {
		t.Error("the failing rolodex target did not report as down")
	}
	if targets[1].LastError == "" {
		t.Error("the failing target came back with no error message")
	}
	if got, want := targets[2].ScrapeURL, "https://127.0.0.1:5309/metrics"; got != want {
		t.Errorf("scrape URL = %q, want %q — the scheme is the thing that was wrong on a real box", got, want)
	}
	if targets[2].LastScrape.IsZero() {
		t.Error("last_scrape did not decode")
	}
}

// A Prometheus that answers with an error envelope is an error here, not an
// empty target list: "nothing to report" and "could not ask" must never be the
// same value.
func TestFetchScrapeTargetsSurfacesAnErrorEnvelope(t *testing.T) {
	t.Parallel()

	ports := targetsServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(`{"status":"error","error":"query timed out"}`)); err != nil {
			t.Errorf("write body: %v", err)
		}
	})

	targets, err := FetchScrapeTargets(targetsContext(t), nil, ports)
	if err == nil {
		t.Fatalf("expected an error, got %d targets", len(targets))
	}
	if targets != nil {
		t.Errorf("targets returned alongside an error: %+v", targets)
	}
}

// A non-200 is an error for the same reason — including the 404 a Prometheus
// too old for this API would return.
func TestFetchScrapeTargetsSurfacesAnHTTPError(t *testing.T) {
	t.Parallel()

	ports := targetsServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	if _, err := FetchScrapeTargets(targetsContext(t), nil, ports); err == nil {
		t.Fatal("expected an error on HTTP 404")
	}
}

// DownJobs is what a banner says: each broken job once, in a stable order,
// and nothing for a target that has simply not been scraped yet.
func TestDownJobsIsDeduplicatedAndSorted(t *testing.T) {
	t.Parallel()

	jobs := DownJobs([]ScrapeTarget{
		{Job: RolodexJobName, Instance: "a", Health: TargetHealthDown},
		{Job: "node-exporter", Instance: "b", Health: TargetHealthDown},
		{Job: RolodexJobName, Instance: "c", Health: TargetHealthDown},
		{Job: ControllerJobName, Instance: "d", Health: TargetHealthUp},
		{Job: "ingress", Instance: "e", Health: TargetHealthUnknown},
	})

	want := []string{"node-exporter", RolodexJobName}
	if len(jobs) != len(want) {
		t.Fatalf("down jobs = %v, want %v", jobs, want)
	}
	for i := range want {
		if jobs[i] != want[i] {
			t.Fatalf("down jobs = %v, want %v", jobs, want)
		}
	}

	if got := DownJobs(nil); len(got) != 0 {
		t.Errorf("DownJobs(nil) = %v, want empty", got)
	}
}

package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"time"
)

// Target health values, as Prometheus reports them in /api/v1/targets.
const (
	// TargetHealthUp means the last scrape succeeded.
	TargetHealthUp = "up"
	// TargetHealthDown means the last scrape failed; LastError says why.
	TargetHealthDown = "down"
	// TargetHealthUnknown means Prometheus has not scraped the target yet —
	// the state every target is in for the first interval after a restart, and
	// therefore not a failure.
	TargetHealthUnknown = "unknown"
)

// targetsFetchTimeout bounds the call to Prometheus. /monitoring/status is a
// poll the UI runs on a timer, so a Prometheus that accepts connections and
// then stalls must not hold a request open behind it.
const targetsFetchTimeout = 3 * time.Second

// ScrapeTarget is one Prometheus scrape target's health, reduced to what an
// operator needs to see: which job it belongs to, what address it points at,
// whether the last scrape worked, and the error if it did not.
type ScrapeTarget struct {
	Job      string `json:"job"`
	Instance string `json:"instance"`
	// Health is TargetHealthUp, TargetHealthDown or TargetHealthUnknown.
	Health string `json:"health"`
	// LastError is Prometheus's own message for a failed scrape (connection
	// refused, certificate error, a 404 on /metrics). Empty for a healthy
	// target, and it is the entire value of this endpoint: without it, "down"
	// is a fact with no next step.
	LastError string `json:"last_error,omitempty"`
	// ScrapeURL is the URL Prometheus actually requested, which is not always
	// the address the config named — a job with a scheme or metrics path set
	// differs, and both have been wrong in this repo before.
	ScrapeURL string `json:"scrape_url,omitempty"`
	// LastScrape is when Prometheus last tried. Zero when it never has —
	// omitzero rather than omitempty, which does nothing for a struct field.
	LastScrape time.Time `json:"last_scrape,omitzero"`
}

// Down reports whether this target's last scrape failed. A target Prometheus
// has not reached yet is not down — it is new.
func (t ScrapeTarget) Down() bool { return t.Health == TargetHealthDown }

// promTargetsResponse is the subset of Prometheus's /api/v1/targets envelope
// this package reads. Everything else in that document (discovered labels,
// dropped targets, scrape pools) describes how a target was found rather than
// whether it answers, which is the only question here.
type promTargetsResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		ActiveTargets []struct {
			Labels struct {
				Job      string `json:"job"`
				Instance string `json:"instance"`
			} `json:"labels"`
			ScrapeURL  string    `json:"scrapeUrl"`
			Health     string    `json:"health"`
			LastError  string    `json:"lastError"`
			LastScrape time.Time `json:"lastScrape"`
		} `json:"activeTargets"`
	} `json:"data"`
}

// FetchScrapeTargets asks the Prometheus on the host loopback which of its
// scrape jobs are answering.
//
// This exists because a scrape job that has never worked is invisible from
// everywhere except Prometheus's own target list. Both metrics failures this
// box has shipped were of exactly that shape: the scheme in prometheus.yml
// disagreed with the controller's listener, and rolodex's metrics section never
// reached its config file. In each case every unit was active, `systemctl
// --failed` was empty, and the only symptom was a panel that drew an empty
// chart — indistinguishable from a service with nothing to report.
//
// Targets come back sorted by job then instance, so a caller rendering them
// gets a stable order rather than Prometheus's internal one.
func FetchScrapeTargets(ctx context.Context, client *http.Client, ports Ports) ([]ScrapeTarget, error) {
	ports = ports.withDefaults()
	if client == nil {
		client = &http.Client{Timeout: targetsFetchTimeout}
	}

	reqCtx, cancel := context.WithTimeout(ctx, targetsFetchTimeout)
	defer cancel()

	url := "http://" + net.JoinHostPort("127.0.0.1", ports.Prometheus) + "/api/v1/targets?state=active"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build targets request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query prometheus targets: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus targets: HTTP %d", resp.StatusCode)
	}

	// Bounded: this is a local daemon, but it is still a body of unknown size
	// being read into a status response.
	var parsed promTargetsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode prometheus targets: %w", err)
	}
	if parsed.Status != "success" {
		if parsed.Error != "" {
			return nil, fmt.Errorf("prometheus targets: %s", parsed.Error)
		}
		return nil, errors.New("prometheus targets: response was not a success")
	}

	targets := make([]ScrapeTarget, 0, len(parsed.Data.ActiveTargets))
	for _, at := range parsed.Data.ActiveTargets {
		health := at.Health
		if health == "" {
			health = TargetHealthUnknown
		}
		targets = append(targets, ScrapeTarget{
			Job:        at.Labels.Job,
			Instance:   at.Labels.Instance,
			Health:     health,
			LastError:  at.LastError,
			ScrapeURL:  at.ScrapeURL,
			LastScrape: at.LastScrape,
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Job != targets[j].Job {
			return targets[i].Job < targets[j].Job
		}
		return targets[i].Instance < targets[j].Instance
	})
	return targets, nil
}

// DownJobs returns the job names with at least one failing target, sorted and
// de-duplicated. It is what a status line says out loud ("scraping is broken
// for rolodex") without making the reader scan a table.
func DownJobs(targets []ScrapeTarget) []string {
	seen := map[string]bool{}
	var jobs []string
	for _, t := range targets {
		if !t.Down() || seen[t.Job] {
			continue
		}
		seen[t.Job] = true
		jobs = append(jobs, t.Job)
	}
	sort.Strings(jobs)
	return jobs
}

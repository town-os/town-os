package metrics

import (
	"math"
	"strings"
	"sync"
	"testing"
)

func TestCounterVecCountsPerLabelTuple(t *testing.T) {
	c := NewCounterVec("reqs", "h", "method", "status")
	c.Inc("GET", "2xx")
	c.Inc("GET", "2xx")
	c.Inc("POST", "4xx")

	m := c.Collect()
	if m.Type != TypeCounter {
		t.Errorf("type = %q, want counter", m.Type)
	}
	got := map[string]float64{}
	for _, s := range m.Samples {
		key := s.Labels[0].Value + "/" + s.Labels[1].Value
		got[key] = s.Value
	}
	if got["GET/2xx"] != 2 || got["POST/4xx"] != 1 {
		t.Errorf("unexpected tallies: %v", got)
	}
}

func TestCounterVecLabelNamesInDeclaredOrder(t *testing.T) {
	c := NewCounterVec("reqs", "h", "method", "status")
	c.Inc("GET", "2xx")
	s := c.Collect().Samples[0]
	if s.Labels[0].Name != "method" || s.Labels[1].Name != "status" {
		t.Errorf("labels out of declared order: %+v", s.Labels)
	}
}

// Go map iteration is random. An unsorted scrape would reshuffle on every
// request for no reason, making two captures impossible to diff.
func TestCounterVecCollectIsSorted(t *testing.T) {
	c := NewCounterVec("reqs", "h", "method")
	for _, m := range []string{"PUT", "GET", "POST", "DELETE"} {
		c.Inc(m)
	}
	first := c.Collect()
	for range 5 {
		next := c.Collect()
		for i := range first.Samples {
			if first.Samples[i].Labels[0].Value != next.Samples[i].Labels[0].Value {
				t.Fatalf("collect order is not stable: %v vs %v", first.Samples, next.Samples)
			}
		}
	}
}

// A wrong arity is dropped rather than panicking or being stored under a
// truncated key. A panic in the audit or request middleware would take down the
// request it is observing, and a truncated key silently merges unrelated series
// into one number nobody can interpret.
func TestCounterVecIgnoresWrongArity(t *testing.T) {
	c := NewCounterVec("reqs", "h", "method", "status")
	c.Inc("GET")
	c.Inc("GET", "2xx", "extra")
	if len(c.Collect().Samples) != 0 {
		t.Errorf("wrong-arity increments were recorded: %+v", c.Collect().Samples)
	}
}

// The null-byte separator cannot appear in any real label value, so two label
// tuples can never collide into one counter — which would silently merge series.
func TestCounterVecSeparatorCannotCollide(t *testing.T) {
	c := NewCounterVec("m", "h", "a", "b")
	c.Inc("x", "y")
	c.Inc("x\x00y", "")
	if len(c.Collect().Samples) != 2 {
		t.Errorf("expected two distinct series, got %+v", c.Collect().Samples)
	}
}

// A nil receiver is what a handler set built without counters would have; it
// must be a no-op rather than a nil dereference in a middleware.
func TestCounterVecNilIsSafe(t *testing.T) {
	var c *CounterVec
	c.Inc("anything")
	if len(c.Collect().Samples) != 0 {
		t.Error("nil CounterVec produced samples")
	}
}

func TestCounterVecConcurrentInc(t *testing.T) {
	c := NewCounterVec("m", "h", "k")
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			for range 20 {
				c.Inc("v")
			}
		})
	}
	wg.Wait()
	samples := c.Collect().Samples
	if len(samples) != 1 || samples[0].Value != 1000 {
		t.Errorf("lost increments under concurrency: %+v", samples)
	}
}

// Not every counter counts events: a cumulative duration is a counter in
// exactly the same sense, and rounding each observation to a whole second would
// make the average request duration — the only reason the family exists — read
// as zero on a control plane that answers in milliseconds.
func TestCounterVecAccumulatesFractions(t *testing.T) {
	c := NewCounterVec("townos_http_request_seconds_total", "h", "method")
	c.Add(0.004, "GET")
	c.Add(0.006, "GET")

	samples := c.Collect().Samples
	if len(samples) != 1 {
		t.Fatalf("got %d samples, want 1: %+v", len(samples), samples)
	}
	if math.Abs(samples[0].Value-0.01) > 1e-9 {
		t.Errorf("accumulated %v, want 0.01", samples[0].Value)
	}
}

// A counter that went backwards reads to Prometheus as a process restart:
// rate() treats the step down as a reset and invents a spike out of the samples
// that follow. A NaN is worse still — every later addition to it is NaN, so one
// bad observation poisons the tally for the life of the process.
func TestCounterVecRejectsBackwardsAndNonFiniteAdds(t *testing.T) {
	c := NewCounterVec("m", "h", "k")
	c.Add(5, "v")
	c.Add(-3, "v")
	c.Add(math.NaN(), "v")
	c.Add(math.Inf(1), "v")

	samples := c.Collect().Samples
	if len(samples) != 1 || samples[0].Value != 5 {
		t.Errorf("unexpected tally after bad adds: %+v", samples)
	}
}

func TestCounterVecRendersAsCounter(t *testing.T) {
	c := NewCounterVec("townos_audit_events_total", "Audit events.", "result")
	c.Inc("success")
	c.Inc("failure")
	var b strings.Builder
	if err := Render(&b, []Metric{c.Collect()}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := b.String()
	if !strings.Contains(got, "# TYPE townos_audit_events_total counter") {
		t.Errorf("missing counter type: %q", got)
	}
	if !strings.Contains(got, `townos_audit_events_total{result="failure"} 1`) {
		t.Errorf("missing failure sample: %q", got)
	}
}

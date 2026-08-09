package metrics

import (
	"maps"
	"sort"
	"strings"
	"sync"
)

// labelSeparator joins label values into a map key. A null byte is used because
// it cannot appear in any value that reaches here (an HTTP method, a status
// code, an audit outcome), so two distinct label tuples can never collide into
// one counter — which would silently merge two series.
const labelSeparator = "\x00"

// CounterVec is a set of monotonically increasing counters keyed by label
// values, safe for concurrent use.
//
// These are process-lifetime tallies held in memory, not persisted. That is the
// right shape for what they count: a counter that survived a restart would
// describe the box's whole history rather than this process's, and Prometheus
// already understands a counter reset. It also keeps a scrape — and the audit
// middleware that feeds it — off the database entirely, so recording that an
// event happened can never become a write that itself fails.
type CounterVec struct {
	name       string
	help       string
	labelNames []string

	mu     sync.Mutex
	values map[string]uint64
}

// NewCounterVec creates a counter family. labelNames fixes both the arity and
// the emitted order of every sample's labels.
func NewCounterVec(name, help string, labelNames ...string) *CounterVec {
	return &CounterVec{
		name:       name,
		help:       help,
		labelNames: labelNames,
		values:     map[string]uint64{},
	}
}

// Inc adds one to the counter for the given label values.
//
// A mismatched number of values is dropped rather than panicking or recorded
// under a truncated key. Both alternatives are worse: a panic in an audit or
// request middleware takes down the request it is observing, and a truncated
// key silently merges unrelated series into one number nobody can interpret.
func (c *CounterVec) Inc(labelValues ...string) {
	c.Add(1, labelValues...)
}

// Add increases the counter for the given label values by n.
func (c *CounterVec) Add(n uint64, labelValues ...string) {
	if c == nil || len(labelValues) != len(c.labelNames) {
		return
	}
	key := strings.Join(labelValues, labelSeparator)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] += n
}

// Collect renders the current tallies as a metric family.
//
// Samples are sorted by key so a scrape is byte-stable while nothing changes;
// Go map iteration order is random, and an unsorted scrape would reshuffle on
// every request for no reason, making two captures impossible to diff.
func (c *CounterVec) Collect() Metric {
	if c == nil {
		return Metric{}
	}
	c.mu.Lock()
	keys := make([]string, 0, len(c.values))
	for k := range c.values {
		keys = append(keys, k)
	}
	snapshot := make(map[string]uint64, len(c.values))
	maps.Copy(snapshot, c.values)
	c.mu.Unlock()

	sort.Strings(keys)
	samples := make([]Sample, 0, len(keys))
	for _, k := range keys {
		parts := strings.Split(k, labelSeparator)
		labels := make([]Label, 0, len(c.labelNames))
		for i, name := range c.labelNames {
			if i < len(parts) {
				labels = append(labels, Label{Name: name, Value: parts[i]})
			}
		}
		samples = append(samples, Sample{Labels: labels, Value: float64(snapshot[k])})
	}
	return Metric{Name: c.name, Help: c.help, Type: TypeCounter, Samples: samples}
}

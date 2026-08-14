// Package metrics renders the Prometheus text exposition format.
//
// This is a few hundred lines rather than a dependency on
// prometheus/client_golang deliberately, for the same reason `errgroup` was
// kept out: the exposition format is a stable, documented, line-oriented text
// protocol, and what Town OS exports is a snapshot assembled per scrape from
// managers that already hold the numbers. The client library's value is its
// registry, its collector interface, and its histogram/summary machinery —
// none of which is used here, while its transitive tree (prometheus/common,
// procfs, protobuf) is real and lands in an image that boots from RAM.
//
// Format: https://prometheus.io/docs/instrumenting/exposition_formats/
package metrics

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Type is a metric family's Prometheus type. Only the two that describe a
// point-in-time snapshot or a process-lifetime tally are supported; a histogram
// needs bucket bookkeeping this package deliberately does not carry.
type Type string

const (
	// TypeGauge is a value that goes up and down — how many units are active.
	TypeGauge Type = "gauge"
	// TypeCounter is a value that only increases for the life of the process —
	// how many audit events have been recorded since boot. A counter resets to
	// zero on restart, which is expected and which Prometheus handles: rate()
	// detects the reset rather than reading it as a huge negative jump.
	TypeCounter Type = "counter"
)

// Label is one dimension of a sample.
type Label struct {
	Name  string
	Value string
}

// Sample is one measurement within a metric family.
type Sample struct {
	// Labels are emitted in the order given. Callers order them so a scrape is
	// byte-identical between calls when nothing has changed, which makes a diff
	// of two scrapes readable.
	Labels []Label
	Value  float64
}

// Metric is one metric family: a name, its documentation, its type, and every
// labelled sample under it.
type Metric struct {
	Name    string
	Help    string
	Type    Type
	Samples []Sample
}

// Gauge builds a single-sample gauge family, the common case.
func Gauge(name, help string, value float64) Metric {
	return Metric{Name: name, Help: help, Type: TypeGauge, Samples: []Sample{{Value: value}}}
}

// Counter builds a single-sample counter family, for a cumulative total that
// is read from the outside rather than tallied here — process CPU time, which
// the kernel already counts, is the case this exists for. A CounterVec is the
// right shape when the process is doing the counting itself.
func Counter(name, help string, value float64) Metric {
	return Metric{Name: name, Help: help, Type: TypeCounter, Samples: []Sample{{Value: value}}}
}

// GaugeVec builds a gauge family from ordered label/value pairs. The samples
// are emitted in the order given.
func GaugeVec(name, help string, samples []Sample) Metric {
	return Metric{Name: name, Help: help, Type: TypeGauge, Samples: samples}
}

// Labelled builds one sample from alternating label name/value strings, so a
// collector reads as data rather than as struct literals. An odd number of
// strings drops the trailing one rather than panicking: a metrics endpoint must
// never be able to take the process down, and a missing label is a far smaller
// failure than a crashed control plane.
func Labelled(value float64, labelPairs ...string) Sample {
	s := Sample{Value: value}
	for i := 0; i+1 < len(labelPairs); i += 2 {
		s.Labels = append(s.Labels, Label{Name: labelPairs[i], Value: labelPairs[i+1]})
	}
	return s
}

// Render writes the families in the text exposition format.
//
// Families are sorted by name and each is emitted at most once, because the
// format forbids interleaving samples of different families and a duplicated
// HELP line makes Prometheus reject the whole scrape. A family with no samples
// is skipped entirely rather than emitted as a bare header, which some parsers
// treat as malformed.
func Render(w io.Writer, families []Metric) error {
	sorted := make([]Metric, 0, len(families))
	seen := make(map[string]struct{}, len(families))
	for _, m := range families {
		if len(m.Samples) == 0 {
			continue
		}
		if _, dup := seen[m.Name]; dup {
			continue
		}
		seen[m.Name] = struct{}{}
		sorted = append(sorted, m)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var b strings.Builder
	for _, m := range sorted {
		if m.Help != "" {
			fmt.Fprintf(&b, "# HELP %s %s\n", m.Name, escapeHelp(m.Help))
		}
		if m.Type != "" {
			fmt.Fprintf(&b, "# TYPE %s %s\n", m.Name, m.Type)
		}
		for _, s := range m.Samples {
			b.WriteString(m.Name)
			writeLabels(&b, s.Labels)
			b.WriteByte(' ')
			b.WriteString(formatValue(s.Value))
			b.WriteByte('\n')
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// writeLabels emits the {k="v",...} clause, omitted entirely when there are no
// labels (a bare `name value` line, which is what the format wants).
func writeLabels(b *strings.Builder, labels []Label) {
	if len(labels) == 0 {
		return
	}
	b.WriteByte('{')
	for i, l := range labels {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(l.Name)
		b.WriteString(`="`)
		b.WriteString(escapeLabelValue(l.Value))
		b.WriteByte('"')
	}
	b.WriteByte('}')
}

// escapeLabelValue escapes a label value per the exposition format: backslash,
// double quote, and newline. The backslash must be replaced first, or the
// escape characters introduced by the later replacements get escaped again.
//
// This is load-bearing rather than defensive. Label values here carry operator
// input — a repository name, a package name, a systemd unit — and an unescaped
// quote in one of them does not corrupt a single line, it makes Prometheus
// reject the entire scrape, so one oddly-named package would silently take all
// monitoring down.
func escapeLabelValue(v string) string {
	if !strings.ContainsAny(v, "\\\"\n") {
		return v
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return r.Replace(v)
}

// escapeHelp escapes a HELP string, where only backslash and newline are
// special — a quote is literal in HELP text.
func escapeHelp(v string) string {
	if !strings.ContainsAny(v, "\\\n") {
		return v
	}
	r := strings.NewReplacer(`\`, `\\`, "\n", `\n`)
	return r.Replace(v)
}

// formatValue renders a sample value. Prometheus spells the non-finite values
// as bare words, and Go's default formatting ("+Inf" is right, but "NaN" comes
// out of FormatFloat as "NaN" only by luck of the verb) is not something to
// rely on, so they are written explicitly.
func formatValue(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// ContentType is the exposition format's media type, which Prometheus expects
// on the response.
const ContentType = "text/plain; version=0.0.4; charset=utf-8"

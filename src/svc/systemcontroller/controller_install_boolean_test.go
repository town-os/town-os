package systemcontroller

import (
	"context"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

func booleanHandlers() *SystemControllerHandlers {
	inst := packages.InitMockInstallManager()
	sb := &serverBase{ServerConfig: ServerConfig{Installer: inst}}
	return &SystemControllerHandlers{Controller: sb, ctx: context.Background()}
}

// A boolean question is rendered as a checkbox, and an unchecked checkbox
// submits nothing at all. The same gap exists for a dependency whose boolean
// question the parent never answers. Both must resolve to a concrete answer
// rather than tripping Compile's empty-response validation.
func TestAutoGenerateResponsesBoolean(t *testing.T) {
	t.Parallel()

	for name, item := range map[string]struct {
		question packages.Question
		response string
		present  bool
		expected string
	}{
		"missing answer, no default": {
			question: packages.Question{Query: "Open registration?", Type: packages.Boolean},
			expected: "false",
		},
		"empty answer, no default": {
			question: packages.Question{Query: "Open registration?", Type: packages.Boolean},
			response: "",
			present:  true,
			expected: "false",
		},
		"missing answer, true default": {
			question: packages.Question{Query: "Open registration?", Type: packages.Boolean, Default: "true"},
			expected: "true",
		},
		"missing answer, false default": {
			question: packages.Question{Query: "Open registration?", Type: packages.Boolean, Default: "false"},
			expected: "false",
		},
		"default normalized": {
			question: packages.Question{Query: "Open registration?", Type: packages.Boolean, Default: "1"},
			expected: "true",
		},
		// An unchecked box submits the literal "false", which must survive
		// even when the package's default says otherwise — otherwise the user
		// could never turn off a default-on option.
		"explicit false beats true default": {
			question: packages.Question{Query: "Open registration?", Type: packages.Boolean, Default: "true"},
			response: "false",
			present:  true,
			expected: "false",
		},
		"explicit true kept": {
			question: packages.Question{Query: "Open registration?", Type: packages.Boolean},
			response: "true",
			present:  true,
			expected: "true",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			responses := packages.Responses{}
			if item.present {
				responses["open"] = item.response
			}

			s := booleanHandlers()
			questions := map[string]packages.Question{"open": item.question}
			if err := s.autoGenerateResponses(&responses, questions, "pkg"); err != nil {
				t.Fatalf("autoGenerateResponses: %v", err)
			}

			if got := responses["open"]; got != item.expected {
				t.Fatalf("response = %q, want %q", got, item.expected)
			}

			// Whatever we filled in must survive compilation; a boolean that
			// Compile rejects would abort the install after the questions
			// dialog has already closed.
			ip := packages.InputPackage{
				Image:       packages.InputPackageImage{URL: "debian:latest"},
				Environment: map[string]string{"OPEN": "@open@"},
				Questions:   questions,
			}
			compiled, err := ip.Compile(responses)
			if err != nil {
				t.Fatalf("Compile with auto-generated response: %v", err)
			}
			if compiled.Environment["OPEN"] != item.expected {
				t.Fatalf("compiled OPEN = %q, want %q", compiled.Environment["OPEN"], item.expected)
			}
		})
	}
}

// A package YAML that declares a boolean default Go cannot parse is a package
// bug; it must surface as an install error rather than silently installing with
// the option off.
func TestAutoGenerateResponsesBooleanInvalidDefault(t *testing.T) {
	t.Parallel()

	responses := packages.Responses{}
	questions := map[string]packages.Question{
		"open": {Query: "Open registration?", Type: packages.Boolean, Default: "maybe"},
	}

	s := booleanHandlers()
	if err := s.autoGenerateResponses(&responses, questions, "pkg"); err == nil {
		t.Fatal("expected an error for an unparseable boolean default")
	}
}

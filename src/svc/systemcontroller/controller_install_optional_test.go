package systemcontroller

import (
	"context"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

func optionalHandlers() *SystemControllerHandlers {
	inst := packages.InitMockInstallManager()
	sb := &serverBase{ServerConfig: ServerConfig{Installer: inst}}
	return &SystemControllerHandlers{Controller: sb, ctx: context.Background()}
}

// An optional question left blank must stay blank. Auto-generation exists to
// fill in what the user cannot reasonably supply -- a free port, a random secret
// -- but applying it to an optional question defeats the point of the question:
// an unconfigured SMTP password would reach the application as a random string,
// and the application would try to authenticate with it rather than skip mail.
func TestAutoGenerateResponsesOptional(t *testing.T) {
	t.Parallel()

	for name, item := range map[string]struct {
		question packages.Question
		response string
		present  bool
		expected string
	}{
		"missing answer stays empty": {
			question: packages.Question{Query: "SMTP host", Optional: true},
			expected: "",
		},
		"empty answer stays empty": {
			question: packages.Question{Query: "SMTP host", Optional: true},
			response: "",
			present:  true,
			expected: "",
		},
		// The generators are keyed off the type; optional has to win over all of
		// them, or a blank optional secret comes back as a 256-bit random string.
		"optional secret is not generated": {
			question: packages.Question{Query: "SMTP password", Type: packages.Secret, Optional: true},
			expected: "",
		},
		"optional port is not allocated": {
			question: packages.Question{Query: "SMTP port", Type: packages.Port, Optional: true},
			expected: "",
		},
		"blank optional falls back to its default": {
			question: packages.Question{Query: "SMTP port", Type: packages.Port, Optional: true, Default: "587"},
			expected: "587",
		},
		"an answer is left alone": {
			question: packages.Question{Query: "SMTP host", Optional: true},
			response: "mail.example.com",
			present:  true,
			expected: "mail.example.com",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			responses := packages.Responses{}
			if item.present {
				responses["q"] = item.response
			}
			questions := map[string]packages.Question{"q": item.question}

			s := optionalHandlers()
			if err := s.autoGenerateResponses(&responses, questions, "pkg"); err != nil {
				t.Fatalf("autoGenerateResponses: %v", err)
			}
			if got := responses["q"]; got != item.expected {
				t.Fatalf("response = %q, want %q", got, item.expected)
			}

			// Whatever was filled in has to survive Compile, which is what
			// rejects an empty response for every non-optional question.
			ip := packages.InputPackage{
				Image:       packages.InputPackageImage{URL: "debian:latest"},
				Environment: map[string]string{"VALUE": "@q@"},
				Questions:   questions,
			}
			compiled, err := ip.Compile(responses)
			if err != nil {
				t.Fatalf("Compile after autoGenerateResponses: %v", err)
			}
			if got := compiled.Environment["VALUE"]; got != item.expected {
				t.Fatalf("compiled VALUE = %q, want %q", got, item.expected)
			}
		})
	}
}

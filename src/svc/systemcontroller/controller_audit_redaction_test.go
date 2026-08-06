// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"encoding/json"
	"strings"
	"testing"
)

// The audit sanitizer used to delete a key literally named "password" from the
// top-level map and from nested maps. Everything else went into the log
// verbatim: other spellings, anything inside an array, and above all the
// `responses` map of a package install — which is where a package's generated
// `type: secret` answers and its `type: oauth` tokens live.

func TestSanitizeAuditDetailRedactsCredentialKeys(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		secret string
	}{
		{"top-level password", `{"username":"alice","password":"s3cret-value"}`, "s3cret-value"},
		{"nested password", `{"fields":{"password":"s3cret-value"}}`, "s3cret-value"},
		{"deeply nested password", `{"a":{"b":{"c":{"password":"s3cret-value"}}}}`, "s3cret-value"},
		{"prefixed key", `{"smtp_password":"s3cret-value"}`, "s3cret-value"},
		{"secret suffix", `{"registration_secret":"s3cret-value"}`, "s3cret-value"},
		{"bare token", `{"token":"s3cret-value"}`, "s3cret-value"},
		{"api key", `{"api_key":"s3cret-value"}`, "s3cret-value"},
		{"apikey one word", `{"apikey":"s3cret-value"}`, "s3cret-value"},
		{"private key", `{"private_key":"s3cret-value"}`, "s3cret-value"},
		{"access token", `{"access_token":"s3cret-value"}`, "s3cret-value"},
		{"passphrase", `{"passphrase":"s3cret-value"}`, "s3cret-value"},
		{"uppercase key", `{"PASSWORD":"s3cret-value"}`, "s3cret-value"},
		{"inside an array", `{"peers":[{"name":"phone","token":"s3cret-value"}]}`, "s3cret-value"},
		{"deep inside arrays", `{"a":[[{"credentials":"s3cret-value"}]]}`, "s3cret-value"},
		{"install responses", `{"repo":"core","name":"gitea","responses":{"dbpass":"s3cret-value"}}`, "s3cret-value"},
		{"install responses nested", `{"responses":{"nested":{"anything":"s3cret-value"}}}`, "s3cret-value"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := sanitizeAuditDetail([]byte(tc.body))
			if out == "" {
				t.Fatalf("sanitizeAuditDetail(%s) returned empty", tc.body)
			}
			if strings.Contains(out, tc.secret) {
				t.Fatalf("sanitizeAuditDetail(%s) = %s, still contains %q", tc.body, out, tc.secret)
			}
		})
	}
}

// Redaction has to leave the entry useful: an auditor needs to know which
// package was installed and by which request, and a mask says "withheld"
// where a delete said nothing at all.
func TestSanitizeAuditDetailKeepsNonSensitiveFields(t *testing.T) {
	out := sanitizeAuditDetail([]byte(`{"repo":"core","name":"gitea","version":"1.0","responses":{"dbpass":"x"}}`))

	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("result is not JSON (%s): %v", out, err)
	}

	for key, want := range map[string]string{"repo": "core", "name": "gitea", "version": "1.0"} {
		got, ok := m[key].(string)
		if !ok {
			t.Fatalf("%s missing or not a string in %s", key, out)
		}
		if got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}

	redacted, ok := m["responses"].(string)
	if !ok {
		t.Fatalf("responses is not the redaction marker in %s", out)
	}
	if redacted != auditRedacted {
		t.Errorf("responses = %q, want %q", redacted, auditRedacted)
	}
}

// An account update's `fields` keeps its non-credential members: a grant change
// is precisely what an auditor reads this log to find, and redacting the whole
// subtree would hide it.
func TestSanitizeAuditDetailKeepsGrantChangesVisible(t *testing.T) {
	out := sanitizeAuditDetail([]byte(`{"username":"alice","fields":{"password":"s3cret-value","grants":["gfeh"],"networks":["office"]}}`))

	for _, want := range []string{"alice", "gfeh", "office"} {
		if !strings.Contains(out, want) {
			t.Errorf("audit detail %s is missing %q", out, want)
		}
	}
	if strings.Contains(out, "s3cret-value") {
		t.Errorf("audit detail %s still contains the password", out)
	}
}

// A WireGuard public key is public by construction and is the field that says
// which device was enrolled. Redacting it — which a bare "key" entry in the
// suffix rule would do — would hide the answer to the question an auditor opens
// the peer-enrollment log to ask.
func TestSanitizeAuditDetailKeepsPublicKeysVisible(t *testing.T) {
	out := sanitizeAuditDetail([]byte(
		`{"network":"office","name":"phone","public_key":"PUBLICKEYVALUE","private_key":"PRIVATEKEYVALUE"}`))

	for _, want := range []string{"office", "phone", "PUBLICKEYVALUE"} {
		if !strings.Contains(out, want) {
			t.Errorf("audit detail %s is missing %q", out, want)
		}
	}
	if strings.Contains(out, "PRIVATEKEYVALUE") {
		t.Errorf("audit detail %s still contains the private key", out)
	}
}

func TestSanitizeAuditDetailHandlesNonObjectBodies(t *testing.T) {
	for _, body := range []string{``, `not json`, `[1,2,3]`, `null`} {
		if out := sanitizeAuditDetail([]byte(body)); out != "" {
			t.Errorf("sanitizeAuditDetail(%q) = %q, want empty", body, out)
		}
	}
}

func TestKeySuffix(t *testing.T) {
	cases := map[string]string{
		"password":      "password",
		"smtp_password": "password",
		"a_b_secret":    "secret",
		"trailing_":     "trailing_",
		"_leading":      "leading",
	}
	for in, want := range cases {
		if got := keySuffix(in); got != want {
			t.Errorf("keySuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"testing"
)

func TestOAuthQuestionNamesPicksOnlyDeviceFlowQuestions(t *testing.T) {
	questions := map[string]Question{
		"plextoken": {Type: Oauth},
		"mediasize": {Type: Bytes},
		"dbpass":    {Type: Secret},
		"plain":     {},
	}

	names := OAuthQuestionNames(questions)
	if len(names) != 1 || names[0] != "plextoken" {
		t.Fatalf("OAuthQuestionNames = %v, want [plextoken]", names)
	}
}

// A purge has to reach every place an answer was kept: each version's file and
// the package-wide last-responses file that an uninstall writes. Missing any one
// of them resurrects the credential on the next install.
func TestForgetResponseKeysClearsEveryStore(t *testing.T) {
	m := NewInstallManager(t.TempDir())

	full := Responses{"plextoken": "acct-token", "mediasize": "10G"}
	for _, version := range []string{"1.0", "2.0"} {
		if err := m.SaveResponses("core", "plex", version, full); err != nil {
			t.Fatalf("SaveResponses %s: %v", version, err)
		}
	}
	if err := m.SaveLastResponses("core", "plex", full); err != nil {
		t.Fatalf("SaveLastResponses: %v", err)
	}

	if err := m.ForgetResponseKeys("core", "plex", []string{"plextoken"}); err != nil {
		t.Fatalf("ForgetResponseKeys: %v", err)
	}

	for _, version := range []string{"1.0", "2.0"} {
		got, err := m.GetResponses("core", "plex", version)
		if err != nil {
			t.Fatalf("GetResponses %s: %v", version, err)
		}
		if _, ok := got["plextoken"]; ok {
			t.Fatalf("version %s still holds the oauth answer: %v", version, got)
		}
		// Everything else is an operator preference and must survive, or a
		// purge turns into a full re-interrogation.
		if got["mediasize"] != "10G" {
			t.Fatalf("version %s lost mediasize: %v", version, got)
		}
	}

	last, err := m.LoadLastResponses("core", "plex")
	if err != nil {
		t.Fatalf("LoadLastResponses: %v", err)
	}
	if _, ok := last["plextoken"]; ok {
		t.Fatalf("last responses still hold the oauth answer: %v", last)
	}
	if last["mediasize"] != "10G" {
		t.Fatalf("last responses lost mediasize: %v", last)
	}
}

// The common case is a package with no oauth question at all, which must not be
// an error and must not rewrite anything.
func TestForgetResponseKeysIsANoOpWithoutKeys(t *testing.T) {
	m := NewInstallManager(t.TempDir())

	if err := m.SaveResponses("core", "nginx", "1.0", Responses{"port": "80"}); err != nil {
		t.Fatalf("SaveResponses: %v", err)
	}

	if err := m.ForgetResponseKeys("core", "nginx", nil); err != nil {
		t.Fatalf("ForgetResponseKeys with no keys: %v", err)
	}

	got, err := m.GetResponses("core", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}
	if got["port"] != "80" {
		t.Fatalf("responses changed: %v", got)
	}
}

// A package that was never installed has nothing stored. Forgetting from it is
// not an error -- the uninstall path calls this unconditionally.
func TestForgetResponseKeysToleratesAnUnknownPackage(t *testing.T) {
	m := NewInstallManager(t.TempDir())

	if err := m.ForgetResponseKeys("core", "never-installed", []string{"plextoken"}); err != nil {
		t.Fatalf("ForgetResponseKeys on an unknown package: %v", err)
	}
}

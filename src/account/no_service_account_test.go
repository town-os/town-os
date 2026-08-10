// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package account

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Town OS has no service accounts, and this is the test that keeps it that way.
//
// A release once gave the object-storage daemon its own account: an enabled
// administrator named `gfeh`, with a generated password stored in the settings
// table, created automatically on boxes that had never asked for it. It sat in
// every user list with enough privilege to uninstall the machine, and it made
// "does this box have an administrator?" a question that could be answered yes
// about something no human could log in as.
//
// It bought nothing even at the time: a partition's subvolume and quota are
// provisioned from the Town OS side before the daemon starts, and its
// principals are created over the admin socket, so the daemon never needed a
// control-plane credential at all.
//
// It is gone -- the account, the password setting, the config section that
// carried it to the daemon, and the migration that used to delete it. What
// remains is this test, because every one of those was easier to add back than
// it was to notice.

// forbidden is what may not reappear anywhere in the source.
//
// Spelled as split literals so this file does not match itself, and so a
// careless grep for the strings does not simply find the guard and stop.
var forbidden = []struct {
	needle string
	why    string
}{
	{"gfeh_service" + "_password", "the settings key the daemon's generated password was stored under"},
	{"PurgeLegacy" + "ServiceAccounts", "the migration that deleted the account; nothing should need deleting"},
	{"LegacyGfeh" + "ServiceAccount", "the account's username constant"},
	{"TownOS" + "Config", "the gfehd config section naming a Town OS account for the daemon"},
}

// TestNoServiceAccountSurvivesInSource walks every Go file under src/ and fails
// on any of the identifiers above.
//
// A source scan rather than a behavioural assertion because the thing being
// prevented is a *reintroduction*, and there is no behaviour left to observe:
// the code is deleted, so the only way it comes back is somebody writing it
// again. This fails the moment they do, in a file that says why.
func TestNoServiceAccountSurvivesInSource(t *testing.T) {
	t.Parallel()

	root := srcRoot(t)
	self := filepath.Base(mustAbs(t, "no_service_account_test.go"))

	var found []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || filepath.Base(path) == self {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		for _, f := range forbidden {
			if strings.Contains(string(body), f.needle) {
				found = append(found, rel+": "+f.needle+" -- "+f.why)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	for _, hit := range found {
		t.Errorf("the object-storage service account is back: %s\n"+
			"Town OS creates no account for any daemon. gfehd authenticates to "+
			"nothing: its partition is provisioned before it starts and its "+
			"principals are created over its admin socket.", hit)
	}
}

// srcRoot is the src/ directory this package lives under.
func srcRoot(t *testing.T) string {
	t.Helper()
	// The test runs in src/account, so src/ is one level up. Resolved rather
	// than hardcoded so a moved package fails loudly instead of silently
	// scanning nothing and passing.
	root := mustAbs(t, "..")
	if filepath.Base(root) != "src" {
		t.Fatalf("expected this package to live under src/, got %s", root)
	}
	return root
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("abs %s: %v", path, err)
	}
	return abs
}

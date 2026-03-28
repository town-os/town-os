// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"os"
	"testing"
)

// TestBootDirectoriesCreated verifies that the systemcontroller creates all
// required directories on startup. The Containerfiles no longer pre-create
// these paths — the systemcontroller itself must ensure they exist.
func TestBootDirectoriesCreated(t *testing.T) {
	t.Parallel()
	dirs := []string{
		"/town-os",         // -btrfs flag
		"/data/db",         // parent of -db /data/db/dev.db
		"/data/repos",      // -repo-dir flag
		"/var/run/town-os", // -network-state default
	}

	for _, d := range dirs {
		fi, err := os.Stat(d)
		if err != nil {
			t.Errorf("directory %s does not exist: %v", d, err)
			continue
		}
		if !fi.IsDir() {
			t.Errorf("%s exists but is not a directory", d)
		}
	}
}

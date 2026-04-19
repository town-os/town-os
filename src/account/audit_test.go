package account

import (
	"testing"
)

// TestRouteActionKeysPopulated guards against silently merging a new
// audited admin endpoint without an i18n action description. The audit
// middleware logs every non-excluded route via RouteActions[path]; if a
// path is missing from RouteActionKeys it shows up in the audit log
// with an empty Action column.
func TestRouteActionKeysPopulated(t *testing.T) {
	t.Parallel()

	// Routes that historically shipped with no description and were
	// added in the audit-description backfill. Pinning them here turns
	// any future regression into a test failure.
	required := []string{
		"/storage/remove-package-volume",
		"/storage/remove-package-volume-group",
		"/packages/clear-last-responses",
		"/system-services/status",
		"/system-services/refresh",
	}

	for _, path := range required {
		key, ok := RouteActionKeys[path]
		if !ok {
			t.Errorf("RouteActionKeys is missing audited route %q", path)
			continue
		}
		if key == "" {
			t.Errorf("RouteActionKeys[%q] has an empty i18n key", path)
		}
		desc := RouteActions[path]
		if desc == "" {
			t.Errorf("RouteActions[%q] resolved to an empty description", path)
		}
		if desc == key {
			t.Errorf("RouteActions[%q] = %q is the raw key (no en-US translation)", path, desc)
		}
	}
}

// TestRouteActionsAllPathsResolved is a defense-in-depth check that
// every entry in RouteActionKeys has a non-empty en-US translation, so
// adding a new key without a translation in en_us.go fails fast.
func TestRouteActionsAllPathsResolved(t *testing.T) {
	t.Parallel()

	for path, key := range RouteActionKeys {
		desc := RouteActions[path]
		if desc == "" || desc == key {
			t.Errorf("RouteActions[%q] (key %q) has no en-US translation", path, key)
		}
	}
}

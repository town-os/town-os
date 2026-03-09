// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

func TestHTTPListUnits(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "foo", Version: "1.0"},
		{Repo: "repo", Name: "bar", Version: "2.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-foo-1.0.service", Description: "Foo", LoadState: "loaded", ActiveState: "active", SubState: "running", UnitFileState: "enabled"},
		{Name: "town-os-package--repo-bar-2.0.service", Description: "Bar", LoadState: "loaded", ActiveState: "inactive", SubState: "dead", UnitFileState: "disabled"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units.Entries))
	}

	if units.Entries[0].Name != "town-os-package--repo-foo-1.0.service" {
		t.Fatalf("expected first unit %q, got %q", "town-os-package--repo-foo-1.0.service", units.Entries[0].Name)
	}
	if units.Entries[0].UnitFileState != "enabled" {
		t.Fatalf("expected first unit UnitFileState %q, got %q", "enabled", units.Entries[0].UnitFileState)
	}
	if units.Entries[1].Name != "town-os-package--repo-bar-2.0.service" {
		t.Fatalf("expected second unit %q, got %q", "town-os-package--repo-bar-2.0.service", units.Entries[1].Name)
	}
	if units.Entries[1].UnitFileState != "disabled" {
		t.Fatalf("expected second unit UnitFileState %q, got %q", "disabled", units.Entries[1].UnitFileState)
	}
}

func TestHTTPListUnitsEmpty(t *testing.T) {
	c, _, _ := initSystemdTestClient(t)

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 0 {
		t.Fatalf("expected 0 units, got %d", len(units.Entries))
	}
}

func TestHTTPListUnitsFiltersNonPackageServices(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "nginx", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-nginx-1.0-8080-tcp.socket", ActiveState: "active"},
		{Name: "town-os-package--repo-nginx-1.0-upnp.service", ActiveState: "active"},
		{Name: "town-os-package--repo-nginx-1.0-upnp.timer", ActiveState: "active"},
		{Name: "town-os-package--repo-nginx-1.0-fwd-8080-tcp.service", ActiveState: "active"},
		{Name: "sshd.service", ActiveState: "active"},
		{Name: "town-os-systemcontroller.service", ActiveState: "active"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit (only main package service), got %d", len(units.Entries))
	}
	if units.Entries[0].Name != "town-os-package--repo-nginx-1.0.service" {
		t.Fatalf("expected town-os-package--repo-nginx-1.0.service, got %s", units.Entries[0].Name)
	}
}

func TestHTTPListUnitsFiltersUninstalledUnits(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	// Only foo is installed; bar has a matching systemd unit but no install record.
	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "foo", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-foo-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-bar-2.0.service", ActiveState: "active"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit (only installed package), got %d", len(units.Entries))
	}
	if units.Entries[0].Name != "town-os-package--repo-foo-1.0.service" {
		t.Fatalf("expected town-os-package--repo-foo-1.0.service, got %s", units.Entries[0].Name)
	}
	if units.Entries[0].PackageIdentifier != "repo/foo@1.0" {
		t.Fatalf("expected package_identifier %q, got %q", "repo/foo@1.0", units.Entries[0].PackageIdentifier)
	}
}

func TestHTTPSetUnitStatusStart(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	if err := c.SetUnitStatus(context.TODO(), "test.service", systemd.Start); err != nil {
		t.Fatalf("SetUnitStatus(start): %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "SetStatus" {
		t.Fatalf("expected method SetStatus, got %q", calls[0].Method)
	}
	unitName, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if unitName != "test.service" {
		t.Fatalf("expected unit %q, got %v", "test.service", calls[0].Args[0])
	}
	startAction, ok := calls[0].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if startAction != systemd.Start {
		t.Fatalf("expected action %q, got %v", systemd.Start, calls[0].Args[1])
	}
}

func TestHTTPSetUnitStatusStop(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	if err := c.SetUnitStatus(context.TODO(), "test.service", systemd.Stop); err != nil {
		t.Fatalf("SetUnitStatus(stop): %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	stopAction, ok := calls[0].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if stopAction != systemd.Stop {
		t.Fatalf("expected action %q, got %v", systemd.Stop, calls[0].Args[1])
	}
}

func TestHTTPSetUnitStatusRestart(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	if err := c.SetUnitStatus(context.TODO(), "test.service", systemd.Restart); err != nil {
		t.Fatalf("SetUnitStatus(restart): %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	restartAction, ok := calls[0].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if restartAction != systemd.Restart {
		t.Fatalf("expected action %q, got %v", systemd.Restart, calls[0].Args[1])
	}
}

func TestHTTPSetUnitStatusEnableRejected(t *testing.T) {
	c, _, _ := initSystemdTestClient(t)

	err := c.SetUnitStatus(context.TODO(), "test.service", systemd.Enable)
	if err == nil {
		t.Fatal("expected error for enable action")
	}
}

func TestHTTPSetUnitStatusDisableRejected(t *testing.T) {
	c, _, _ := initSystemdTestClient(t)

	err := c.SetUnitStatus(context.TODO(), "test.service", systemd.Disable)
	if err == nil {
		t.Fatal("expected error for disable action")
	}
}

func TestHTTPSetUnitStatusStopSystemcontrollerRejected(t *testing.T) {
	c, _, _ := initSystemdTestClient(t)

	err := c.SetUnitStatus(context.TODO(), "town-os-systemcontroller.service", systemd.Stop)
	if err == nil {
		t.Fatal("expected error stopping systemcontroller")
	}
}

func TestHTTPSetUnitStatusInvalidAction(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	sd.StatusErr = errors.New("injected error")

	err := c.SetUnitStatus(context.TODO(), "test.service", systemd.Start)
	if err == nil {
		t.Fatal("expected error from SetUnitStatus with injected error")
	}
}

func TestHTTPListUnitsPagination(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "a", Version: "1.0"},
		{Repo: "repo", Name: "b", Version: "1.0"},
		{Repo: "repo", Name: "c", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-a-1.0.service", Description: "A", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "town-os-package--repo-b-1.0.service", Description: "B", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "town-os-package--repo-c-1.0.service", Description: "C", LoadState: "loaded", ActiveState: "active", SubState: "running"},
	}

	// First page: limit=2, offset=0
	page, err := c.ListUnits(context.TODO(), ListParams{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListUnits page 0: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}
	if page.TotalPages != 2 {
		t.Fatalf("expected 2 total pages, got %d", page.TotalPages)
	}

	// Second page: limit=2, offset=2
	page, err = c.ListUnits(context.TODO(), ListParams{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListUnits page 1: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false")
	}
}

func TestHTTPListUnitsSearch(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "nginx", Version: "1.0"},
		{Repo: "repo", Name: "redis", Version: "7.0"},
		{Repo: "repo", Name: "postgres", Version: "16.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", Description: "NGINX web server", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "town-os-package--repo-redis-7.0.service", Description: "Redis cache", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "town-os-package--repo-postgres-16.0.service", Description: "PostgreSQL database", LoadState: "loaded", ActiveState: "active", SubState: "running"},
	}

	// Search for "nginx"
	page, err := c.ListUnits(context.TODO(), ListParams{Search: "nginx"})
	if err != nil {
		t.Fatalf("ListUnits search: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if page.Entries[0].Name != "town-os-package--repo-nginx-1.0.service" {
		t.Fatalf("expected town-os-package--repo-nginx-1.0.service, got %s", page.Entries[0].Name)
	}

	// Search with pagination
	page, err = c.ListUnits(context.TODO(), ListParams{Search: "service", Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListUnits search+page: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}

	// No match
	page, err = c.ListUnits(context.TODO(), ListParams{Search: "mysql"})
	if err != nil {
		t.Fatalf("ListUnits search no match: %v", err)
	}
	if len(page.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(page.Entries))
	}
}

func TestHTTPListUnitsNCFailedPropagation(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "nginx", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-nginx-1.0-network.service", ActiveState: "failed"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units.Entries))
	}
	if !units.Entries[0].NCFailed {
		t.Fatal("expected NCFailed=true when network controller unit has failed")
	}
	if units.Entries[0].ActiveState != "failed" {
		t.Fatalf("expected ActiveState %q when NC failed, got %q", "failed", units.Entries[0].ActiveState)
	}
}

func TestHTTPListUnitsAllUninstalledReturnsEmpty(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	// No packages installed, but systemd units exist.
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-orphan-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-stale-2.0.service", ActiveState: "failed"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 0 {
		t.Fatalf("expected 0 units when nothing is installed, got %d", len(units.Entries))
	}
}

func TestHTTPListUnitsNCFailedExcludedForUninstalledUnit(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	// Only foo is installed; bar is not.
	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "foo", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-foo-1.0.service", ActiveState: "active"},
		// Uninstalled unit with a failed NC should not appear at all.
		{Name: "town-os-package--repo-bar-2.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-bar-2.0-network.service", ActiveState: "failed"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units.Entries))
	}
	if units.Entries[0].Name != "town-os-package--repo-foo-1.0.service" {
		t.Fatalf("expected town-os-package--repo-foo-1.0.service, got %s", units.Entries[0].Name)
	}
	if units.Entries[0].NCFailed {
		t.Fatal("expected NCFailed=false for installed unit without failed NC")
	}
}

func TestHTTPListUnitsListInstalledErrorReturnsEmpty(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.ListErr = errors.New("disk error")

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-foo-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-bar-2.0.service", ActiveState: "active"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 0 {
		t.Fatalf("expected 0 units when ListInstalled fails, got %d", len(units.Entries))
	}
}

func TestHTTPListUnitsPaginationAfterFiltering(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	// Only 2 of 4 units are installed.
	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "a", Version: "1.0"},
		{Repo: "repo", Name: "c", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-a-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-b-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-c-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-d-1.0.service", ActiveState: "active"},
	}

	// Request page with limit=1: should see 2 total entries (after filtering).
	page, err := c.ListUnits(context.TODO(), ListParams{Limit: 1, Offset: 0})
	if err != nil {
		t.Fatalf("ListUnits page 0: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}
	if page.TotalPages != 2 {
		t.Fatalf("expected 2 total pages, got %d", page.TotalPages)
	}

	// Second page.
	page, err = c.ListUnits(context.TODO(), ListParams{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("ListUnits page 1: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false")
	}
}

func TestHTTPListUnitsFiltersDegenerateDoubleDashUnits(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "nginx", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		// Valid unit with a matching installed package.
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		// Degenerate unit: bare prefix with no repo/name/version.
		// Passes IsPackageServiceUnit but has no installed package match.
		{Name: "town-os-package--.service", ActiveState: "active"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit (degenerate double-dash unit filtered), got %d", len(units.Entries))
	}
	if units.Entries[0].Name != "town-os-package--repo-nginx-1.0.service" {
		t.Fatalf("expected town-os-package--repo-nginx-1.0.service, got %s", units.Entries[0].Name)
	}
	if units.Entries[0].PackageIdentifier == "" {
		t.Fatal("expected non-empty PackageIdentifier for valid unit")
	}
}

func TestHTTPListUnitsFiltersAllDegenerateDoubleDashUnits(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	// No packages installed. Only degenerate units exist.
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--.service", ActiveState: "active"},
		{Name: "town-os-package--x.service", ActiveState: "active"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 0 {
		t.Fatalf("expected 0 units when only degenerate units exist, got %d", len(units.Entries))
	}
}

func TestHTTPListUnitsPaginationExcludesDegenerateUnits(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "a", Version: "1.0"},
		{Repo: "repo", Name: "b", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-a-1.0.service", ActiveState: "active"},
		// Degenerate unit interspersed between valid units.
		{Name: "town-os-package--.service", ActiveState: "active"},
		{Name: "town-os-package--repo-b-1.0.service", ActiveState: "active"},
	}

	// Paginate with limit=1: total should be 2 (not 3).
	page, err := c.ListUnits(context.TODO(), ListParams{Limit: 1, Offset: 0})
	if err != nil {
		t.Fatalf("ListUnits page 0: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}
	if page.TotalPages != 2 {
		t.Fatalf("expected 2 total pages, got %d", page.TotalPages)
	}
}

func TestHTTPListUnitsSearchExcludesDegenerateUnits(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "nginx", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		// Degenerate unit whose name contains "package" which could match a broad search.
		{Name: "town-os-package--.service", ActiveState: "active"},
	}

	// Search for "package" — the degenerate unit structurally matches but should be excluded.
	page, err := c.ListUnits(context.TODO(), ListParams{Search: "package"})
	if err != nil {
		t.Fatalf("ListUnits search: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry (degenerate excluded from search results), got %d", len(page.Entries))
	}
	if page.Entries[0].Name != "town-os-package--repo-nginx-1.0.service" {
		t.Fatalf("expected town-os-package--repo-nginx-1.0.service, got %s", page.Entries[0].Name)
	}
}

func TestHTTPListUnitsDegenerateWithNCUnit(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "nginx", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-nginx-1.0-network.service", ActiveState: "active"},
		// Degenerate main unit and a degenerate NC-like unit.
		{Name: "town-os-package--.service", ActiveState: "active"},
		{Name: "town-os-package---network.service", ActiveState: "failed"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units.Entries))
	}
	if units.Entries[0].Name != "town-os-package--repo-nginx-1.0.service" {
		t.Fatalf("expected town-os-package--repo-nginx-1.0.service, got %s", units.Entries[0].Name)
	}
	// The valid unit's NC is active (not failed), so NCFailed should be false.
	if units.Entries[0].NCFailed {
		t.Fatal("expected NCFailed=false for valid unit with active NC")
	}
}

func TestHTTPListUnitsDegenerateVariations(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "valid", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-valid-1.0.service", ActiveState: "active"},
		// Various degenerate patterns that pass IsPackageServiceUnit but have no install record.
		{Name: "town-os-package--.service", ActiveState: "active"},
		{Name: "town-os-package--x.service", ActiveState: "active"},
		{Name: "town-os-package----.service", ActiveState: "active"},
		{Name: "town-os-package--unknown-pkg-0.0.service", ActiveState: "active"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit (all degenerate variations filtered), got %d", len(units.Entries))
	}
	if units.Entries[0].Name != "town-os-package--repo-valid-1.0.service" {
		t.Fatalf("expected town-os-package--repo-valid-1.0.service, got %s", units.Entries[0].Name)
	}
}

func TestHTTPListUnitsSortExcludesDegenerateUnits(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "beta", Version: "1.0"},
		{Repo: "repo", Name: "alpha", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-beta-1.0.service", ActiveState: "active"},
		// Degenerate units interspersed — should not affect sort results.
		{Name: "town-os-package--.service", ActiveState: "active"},
		{Name: "town-os-package--x.service", ActiveState: "active"},
		{Name: "town-os-package--repo-alpha-1.0.service", ActiveState: "inactive"},
	}

	// Sort by package_identifier ascending: alpha should come before beta.
	page, err := c.ListUnits(context.TODO(), ListParams{SortBy: "package_identifier", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries after filtering degenerate units, got %d", len(page.Entries))
	}
	if page.Entries[0].PackageIdentifier != "repo/alpha@1.0" {
		t.Fatalf("expected alpha first in asc sort, got %s", page.Entries[0].PackageIdentifier)
	}
	if page.Entries[1].PackageIdentifier != "repo/beta@1.0" {
		t.Fatalf("expected beta second in asc sort, got %s", page.Entries[1].PackageIdentifier)
	}
}

func TestHTTPListUnitsSearchDoubleDashOnlyReturnsInstalled(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "nginx", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		// Degenerate units contain "--" in their name but should be excluded.
		{Name: "town-os-package--.service", ActiveState: "active"},
		{Name: "town-os-package----.service", ActiveState: "active"},
	}

	// Search for "--" — all units structurally match, but only the installed one should appear.
	page, err := c.ListUnits(context.TODO(), ListParams{Search: "--"})
	if err != nil {
		t.Fatalf("ListUnits search: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry when searching for '--', got %d", len(page.Entries))
	}
	if page.Entries[0].Name != "town-os-package--repo-nginx-1.0.service" {
		t.Fatalf("expected town-os-package--repo-nginx-1.0.service, got %s", page.Entries[0].Name)
	}
}

func TestHTTPListUnitsMultipleValidWithDegenerateMixed(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "alpha", Version: "1.0"},
		{Repo: "repo", Name: "beta", Version: "2.0"},
		{Repo: "repo", Name: "gamma", Version: "3.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-alpha-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--.service", ActiveState: "active"},
		{Name: "town-os-package--repo-beta-2.0.service", ActiveState: "inactive"},
		{Name: "town-os-package--x.service", ActiveState: "active"},
		{Name: "town-os-package----.service", ActiveState: "failed"},
		{Name: "town-os-package--repo-gamma-3.0.service", ActiveState: "active"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 3 {
		t.Fatalf("expected 3 units (all degenerate filtered), got %d", len(units.Entries))
	}

	// Verify all valid units have correct package identifiers.
	expected := map[string]string{
		"town-os-package--repo-alpha-1.0.service": "repo/alpha@1.0",
		"town-os-package--repo-beta-2.0.service":  "repo/beta@2.0",
		"town-os-package--repo-gamma-3.0.service": "repo/gamma@3.0",
	}
	for _, e := range units.Entries {
		wantID, ok := expected[e.Name]
		if !ok {
			t.Fatalf("unexpected unit in results: %s", e.Name)
		}
		if e.PackageIdentifier != wantID {
			t.Fatalf("unit %s: expected PackageIdentifier %q, got %q", e.Name, wantID, e.PackageIdentifier)
		}
	}
}

func TestHTTPListUnitsDescriptionEnrichmentWithRepoRoot(t *testing.T) {
	c, sd, inst, rr := initSystemdTestClientWithRepoRoot(t)

	writeTestPackage(t, rr.BaseDir, "repo", "nginx", "1.0", "image: nginx:1.0\ndescription: A fast web server\n")
	writeTestPackage(t, rr.BaseDir, "repo", "redis", "7.0", "image: redis:7.0\ndescription: In-memory data store\n")

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "nginx", Version: "1.0"},
		{Repo: "repo", Name: "redis", Version: "7.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-redis-7.0.service", ActiveState: "active"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units.Entries))
	}

	descMap := map[string]string{}
	for _, e := range units.Entries {
		descMap[e.PackageIdentifier] = e.PackageDescription
	}

	if descMap["repo/nginx@1.0"] != "A fast web server" {
		t.Fatalf("expected nginx description %q, got %q", "A fast web server", descMap["repo/nginx@1.0"])
	}
	if descMap["repo/redis@7.0"] != "In-memory data store" {
		t.Fatalf("expected redis description %q, got %q", "In-memory data store", descMap["repo/redis@7.0"])
	}
}

func TestHTTPListUnitsDescriptionEmptyWithoutRepoRoot(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "nginx", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units.Entries))
	}
	if units.Entries[0].PackageDescription != "" {
		t.Fatalf("expected empty description without repo root, got %q", units.Entries[0].PackageDescription)
	}
}

func TestHTTPListUnitsDescriptionPartialLoadError(t *testing.T) {
	c, sd, inst, rr := initSystemdTestClientWithRepoRoot(t)

	// Only write one package; the other is missing from disk.
	writeTestPackage(t, rr.BaseDir, "repo", "nginx", "1.0", "image: nginx:1.0\ndescription: Web server\n")

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "nginx", Version: "1.0"},
		{Repo: "repo", Name: "missing", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-missing-1.0.service", ActiveState: "active"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units.Entries))
	}

	descMap := map[string]string{}
	for _, e := range units.Entries {
		descMap[e.PackageIdentifier] = e.PackageDescription
	}

	if descMap["repo/nginx@1.0"] != "Web server" {
		t.Fatalf("expected nginx description %q, got %q", "Web server", descMap["repo/nginx@1.0"])
	}
	if descMap["repo/missing@1.0"] != "" {
		t.Fatalf("expected empty description for missing package, got %q", descMap["repo/missing@1.0"])
	}
}

func TestHTTPListUnitsDegenerateNCDoesNotCorruptValidNC(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "app", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-app-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-app-1.0-network.service", ActiveState: "active"},
		// Degenerate main unit and its degenerate NC, both failed.
		{Name: "town-os-package--.service", ActiveState: "failed"},
		{Name: "town-os-package---network.service", ActiveState: "failed"},
		// Another degenerate with a different NC pattern.
		{Name: "town-os-package--x.service", ActiveState: "active"},
		{Name: "town-os-package--x-network.service", ActiveState: "failed"},
	}

	units, err := c.ListUnits(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units.Entries))
	}
	if units.Entries[0].Name != "town-os-package--repo-app-1.0.service" {
		t.Fatalf("expected town-os-package--repo-app-1.0.service, got %s", units.Entries[0].Name)
	}
	// The valid unit's NC is active, so NCFailed should be false despite degenerate NCs being failed.
	if units.Entries[0].NCFailed {
		t.Fatal("expected NCFailed=false — degenerate NC failures should not affect valid unit")
	}
	if units.Entries[0].ActiveState != "active" {
		t.Fatalf("expected ActiveState %q, got %q", "active", units.Entries[0].ActiveState)
	}
}

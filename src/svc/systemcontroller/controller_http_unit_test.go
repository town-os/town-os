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

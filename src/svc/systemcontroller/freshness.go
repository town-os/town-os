package systemcontroller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

// WriteRestartPendingMarker drops the `town-os-restart-pending` marker in
// baseDir so that the next systemcontroller boot runs the freshness
// stage. Returns nil when baseDir is empty (e.g. in-memory test mode)
// rather than erroring, matching the "refresh succeeds, freshness skips"
// behaviour of the calling handler.
func WriteRestartPendingMarker(baseDir string) error {
	if baseDir == "" {
		return nil
	}
	markerPath := filepath.Join(baseDir, RestartPendingMarkerFilename)
	body := time.Now().UTC().Format(time.RFC3339) + "\n"
	if err := os.WriteFile(markerPath, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write restart-pending marker: %w", err)
	}
	return nil
}

// FreshnessReporter is the subset of *BootStatus that RunFreshnessStage
// needs. Narrowing the interface lets tests drive the stage with a fake
// that just records step names without standing up a full BootStatus.
type FreshnessReporter interface {
	Step(step string)
}

// FreshnessRestarter is the subset of systemd.Manager that
// RunFreshnessStage calls. The mock systemd manager in tests already
// satisfies this via SetStatus.
type FreshnessRestarter interface {
	SetStatus(ctx context.Context, unit string, action systemd.StatusAction) error
}

// FreshnessLister is the subset of *packages.InstallManager the stage
// needs — just the installed-identity listing. Mock install manager also
// satisfies this.
type FreshnessLister interface {
	ListInstalled() ([]string, error)
}

// RunFreshnessStage checks for the restart-pending marker in baseDir and,
// if present, iterates every installed package identifier, emits a
// "refreshing_<repo>/<name>" step on the reporter, and restarts the
// unit. Per-package errors are logged but never abort the loop — one
// stuck package must not block the controller from swapping to its full
// handler. The marker is removed only after the loop completes so a
// crash leaves the marker in place and the next boot retries.
//
// When baseDir is empty or the marker does not exist, this is a no-op.
// Returns the list of package identities that failed to restart (may be
// empty) and any non-restart error (I/O errors reading the filesystem).
func RunFreshnessStage(
	ctx context.Context,
	bs FreshnessReporter,
	inst FreshnessLister,
	sd FreshnessRestarter,
	baseDir string,
) (failed []string, err error) {
	if baseDir == "" {
		return nil, nil
	}
	markerPath := filepath.Join(baseDir, RestartPendingMarkerFilename)
	if _, statErr := os.Stat(markerPath); statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat restart-pending marker: %w", statErr)
	}

	bs.Step("refresh_packages")

	installed, listErr := inst.ListInstalled()
	if listErr != nil {
		return nil, fmt.Errorf("list installed for freshness: %w", listErr)
	}

	for _, ident := range installed {
		repo, name, version, ok := parseIdentity(ident)
		if !ok {
			slog.Warn("skip unparseable install identity in freshness stage",
				slog.String("ident", ident))
			continue
		}
		bs.Step("refreshing_" + repo + "/" + name)
		unit := systemd.UnitName(repo, name, version)
		if rErr := sd.SetStatus(ctx, unit, systemd.Restart); rErr != nil {
			slog.Warn("freshness restart failed",
				slog.String("unit", unit),
				slog.String("err", rErr.Error()))
			failed = append(failed, ident)
		}
	}

	if rmErr := os.Remove(markerPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		slog.Warn("remove restart-pending marker",
			slog.String("path", markerPath),
			slog.String("err", rmErr.Error()))
	}
	return failed, nil
}

// parseIdentity splits "<repo>/<name>@<version>" into its three parts.
// Returns ok=false for any shape that does not match so callers can log
// and skip rather than panic. Mirrors InstallManager.ListInstalled's
// output format (see src/packages/install.go).
func parseIdentity(ident string) (repo, name, version string, ok bool) {
	repo, rest, ok := strings.Cut(ident, "/")
	if !ok {
		return "", "", "", false
	}
	// The name segment may itself contain "/" (for pretty-form deps like
	// "gitea/postgres"), but ListInstalled emits the flat --dep-- form
	// where the name has no slashes. SplitN on "@" covers both: the
	// version is always after the final "@".
	atIdx := strings.LastIndex(rest, "@")
	if atIdx < 0 {
		return "", "", "", false
	}
	return repo, rest[:atIdx], rest[atIdx+1:], true
}

// Compile-time proof that the concrete managers satisfy the narrow
// interfaces declared above, so RunFreshnessStage can be wired up from
// main.go without a translation layer.
var (
	_ FreshnessLister    = (*packages.InstallManager)(nil)
	_ FreshnessRestarter = (*systemd.SystemdManager)(nil)
)

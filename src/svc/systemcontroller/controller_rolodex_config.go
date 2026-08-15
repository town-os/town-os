package systemcontroller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/rolodex"
)

// ProgramRolodex pushes every setting Town OS owns into a RUNNING rolodex:
// the forwarder list, the resolution mode, and both blocklists.
//
// This is the whole of how those settings reach rolodex. They are deliberately
// not written into rolodex.yml — that file is bootstrap-only and belongs to the
// install image, which renders the two things that cannot be set over the API
// (the bind list, from host addresses only it can enumerate, and the metrics
// listener, which rolodex opens once at startup). Everything else is a runtime
// call, which is what makes changing a setting cost nothing: rewriting the file
// meant restarting rolodex, and restarting the box's only resolver is a DNS
// outage for everything on it.
//
// It must run again after every rolodex restart. rolodex holds all of this in
// memory only — it seeds from its config file at startup and persists nothing
// set over gRPC — so a crash under Restart=always, a DHCP lease change bouncing
// the unit, or an operator restarting it by hand drops the lot back to
// defaults. See Manager.Generation for how a restart is noticed.
//
// Failures are collected rather than short-circuited: three of these four are
// independent, and a rolodex that refuses one is no reason to leave the other
// three unset.
func ProgramRolodex(ctx context.Context, client rolodex.Client, mgr *rolodex.Manager, settings account.SettingsManager) error {
	if client == nil || mgr == nil {
		return nil
	}

	var errs []error

	// Pushed unconditionally rather than diffed: there is no GetForwarders in
	// the API, and rolodex's setter is a plain store — no cache flush, no
	// upstream reconnection — so an identical push costs one RPC and changes
	// nothing observable.
	if err := client.SetForwarders(ctx, mgr.Forwarders()); err != nil {
		errs = append(errs, fmt.Errorf("forwarders: %w", err))
	}

	// The mode IS diffed, because it is the one setting where re-asserting has
	// a cost: switching into auto restarts the tier discovery, and doing that
	// on a schedule would keep throwing away a settled tier on a box that is
	// already exactly where it should be.
	want := mgr.ResolutionMode()
	live, err := client.GetResolutionMode(ctx)
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("resolution mode: get: %w", err))
	case live != want:
		if err := client.SetResolutionMode(ctx, want); err != nil {
			errs = append(errs, fmt.Errorf("resolution mode: set %q: %w", want, err))
		} else {
			slog.Info("programmed rolodex resolution mode", "mode", want, "was", live)
		}
	}

	// Already a readback-and-diff of its own, and already the path an operator
	// edit takes; calling it here is what extends it to cover a restart.
	if err := ReconcileBlocklists(ctx, client, settings); err != nil {
		errs = append(errs, fmt.Errorf("blocklists: %w", err))
	}

	return errors.Join(errs...)
}

// reconcileRolodexProgramming reprograms rolodex when it is running a different
// process than the one Town OS last programmed.
//
// The generation check is what keeps this cheap enough to run often: one stat
// of the gRPC socket, and no RPCs at all in the steady state. Programming
// itself is only worth doing when rolodex has actually restarted, because
// nothing else can lose the settings — Town OS is the only writer, and every
// operator-initiated change programs the server directly at the time.
//
// The generation is recorded only on a fully successful pass. A partial
// failure leaves the old value in place so the next tick tries again, rather
// than recording the run as programmed and leaving a box with, say, its
// blocklists off until the next restart.
func (s *serverBase) reconcileRolodexProgramming(ctx context.Context) {
	if s.Rolodex == nil {
		return
	}

	gen := s.Rolodex.Generation()
	if gen == "" {
		// rolodex is not running, or has not bound its socket yet. Not a
		// change: recording it would make the first real generation look
		// already-programmed.
		return
	}
	if previous, ok := s.rolodexGeneration.Load().(string); ok && previous == gen {
		return
	}

	client := s.GetRolodexClient() //nolint:contextcheck // GetRolodexClient uses its own short-lived dial context; see onInternalIPChange.
	if client == nil {
		return
	}

	if err := ProgramRolodex(ctx, client, s.Rolodex, s.SettingsMgr); err != nil {
		slog.Error("programming rolodex after restart", "error", err)
		return
	}

	if _, seen := s.rolodexGeneration.Load().(string); seen {
		// Not the first pass of this controller's life, so rolodex genuinely
		// restarted underneath us rather than this being the boot-time
		// programming.
		slog.Info("rolodex restarted; reprogrammed its runtime settings")
	}
	s.rolodexGeneration.Store(gen)
}

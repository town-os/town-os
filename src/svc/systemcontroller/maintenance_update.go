package systemcontroller

import "strings"

// The daily update contract, as seen from the controller's side.
//
// The timer itself is NOT here. `town-os-update.timer` and its service ship
// with the installer (../install, systemd/town-os-update.{timer,service}) and
// are enabled at image build time, so a box has them from first boot rather
// than acquiring them from a controller that has to be running first. What
// lives here is the half the controller owns: the marker that identifies a
// scheduled call, and the rule for what the setting's stored value means.
//
// The two halves have to agree on one string, ScheduledRefreshQuery, and
// nothing but a matching literal in the shipped unit enforces it. If that unit
// stops sending the marker the failure is quiet in the direction that matters
// least — a scheduled refresh would ignore the setting and update anyway,
// rather than silently stop updating.

// ScheduledRefreshQuery marks a refresh as the timer's rather than an
// operator's. Only a scheduled refresh honours the auto_update_enabled setting;
// an explicit admin refresh always runs, because a switch labelled "update
// automatically" should not disable the update button.
const ScheduledRefreshQuery = "scheduled"

// autoUpdateDisabledValue reports whether a stored auto_update_enabled value
// means "off".
//
// Off is an explicit list and everything else is on, rather than the reverse.
// The setting is written by hand, by the UI, and by whatever an operator types
// into the settings API, so the spellings people actually use for false have to
// work — but an unrecognised value must not silently stop a box updating. The
// consequence of guessing "off" wrongly is a box that quietly stops acquiring
// its own services; the consequence of guessing "on" wrongly is one extra pull.
func autoUpdateDisabledValue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "off", "no":
		return true
	default:
		return false
	}
}

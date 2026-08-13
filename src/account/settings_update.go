package account

// AutoUpdateKey is the DefaultSettings key controlling the daily automatic
// image update. The entry itself is registered in the DefaultSettings literal in
// settings.go rather than from an init() here: the setting is unconditional, so
// there is nothing for an init to decide. (settings_proton.go does use one, but
// only because it is build-tag-gated and must not register anything unless the
// `proton` tag is active.)
//
// The timer that reads it ships with the installer as
// town-os-update.timer; the controller's half of that contract is
// ScheduledRefreshQuery in the systemcontroller's maintenance_update.go.
const AutoUpdateKey = "auto_update_enabled"

// AutoUpdateDefault is "1": a box updates itself unless told not to.
//
// On by default because the installer ships only the systemcontroller and
// rolodex images — every other system-service image (UI, ingress, network
// controller, object storage) is pulled by the controller on this box. A box
// that never pulled would be a box missing most of its services, so opting out
// is a deliberate choice an operator makes, not the resting state.
const AutoUpdateDefault = "1"

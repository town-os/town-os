// bootSteps is the canonical ordered list of stages the systemcontroller
// emits during boot via bs.Step() in
// src/svc/systemcontroller/cmd/systemcontroller/main.go, plus
// restart_packages, emitted by the freshness stage in
// src/svc/systemcontroller/freshness.go.
//
// There are deliberately only five, and they are phrased for a person
// watching a self-update rather than for someone reading main.go: which
// of the controller, DNS, the system services, or their own packages is
// currently holding things up. The backend used to emit ~25 internal
// stages (one per manager constructor, one per reconcile pass); those are
// now folded into these five.
//
// The freshness stage additionally emits one dynamic
// "restarting_<repo>/<name>" event per installed package. Those are NOT
// listed here — the stepper appends a row for each as it arrives (see
// PACKAGE_PREFIX in BootStatusStepper.jsx), so every package is
// represented individually and with the same weight as the five stages.
//
// IMPORTANT: this list MUST stay in sync with the exact set and order of
// bs.Step("...") literals in main.go. The stepper classifies a stage by
// its INDEX here (see stateFor in BootStatusStepper.jsx); a step the
// backend emits that is missing from this list resolves to indexOf === -1,
// which the stepper would otherwise treat as "nothing done yet" and blank
// out every already-completed row. The Go test
// TestBootStepsFrontendInSyncWithBackend
// (src/svc/systemcontroller/boot_steps_sync_test.go) fails if main.go and
// this list drift apart.
//
// Lives in its own module so the stepper component file can satisfy
// the react-refresh/only-export-components lint rule (which rejects
// non-component exports from component files).
export const bootSteps = [
  'boot_controller',
  'boot_dns',
  'boot_services',
  'restart_packages',
  'ready',
]

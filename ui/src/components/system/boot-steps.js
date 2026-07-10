// bootSteps is the canonical ordered list of stages the
// systemcontroller emits during boot via bs.Step() in
// src/svc/systemcontroller/cmd/systemcontroller/main.go (plus
// refresh_packages, emitted by the freshness stage in
// src/svc/systemcontroller/freshness.go). The stepper component renders
// one row per entry so the user can see what is coming next, and uses
// the index to classify each stage as pending / in-progress / done when
// live events arrive.
//
// IMPORTANT: this list MUST stay in sync with the exact set and order of
// bs.Step("...") literals in main.go. The stepper classifies a stage by
// its INDEX here (see stateFor in BootStatusStepper.jsx); a step the
// backend emits that is missing from this list resolves to indexOf === -1,
// which the stepper would otherwise treat as "nothing done yet" and blank
// out every already-completed row. The Go test
// TestBootStepsFrontendInSyncWithBackend
// (src/svc/systemcontroller/boot_steps_sync_test.go) fails if main.go and
// this list drift apart. Conditional steps (start_ingress, start_pages,
// reconcile_ingress) are still listed: when skipped in dev mode they are
// simply passed over and shown as done, which is harmless.
//
// Lives in its own module so the stepper component file can satisfy
// the react-refresh/only-export-components lint rule (which rejects
// non-component exports from component files).
export const bootSteps = [
  'setup_temp_dir',
  'create_dirs',
  'open_db',
  'init_account_mgr',
  'init_session_mgr',
  'init_audit_mgr',
  'init_settings_mgr',
  'init_pages_mgr',
  'init_network_mgr',
  'seed_repositories',
  'init_repo_root',
  'write_rolodex_config',
  'wait_rolodex_dns',
  'pull_images',
  'start_monitoring',
  'start_ingress',
  'start_pages',
  'reconcile',
  'reconcile_dns',
  'reconcile_networks',
  'reconcile_ingress',
  'start_ui_container',
  'refresh_packages',
  'build_handler',
  'ready',
]

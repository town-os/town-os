// bootSteps is the canonical ordered list of stages the
// systemcontroller emits during boot via bs.Step() in
// src/svc/systemcontroller/cmd/systemcontroller/main.go. The stepper
// component renders one row per entry so the user can see what is
// coming next, and uses the index to classify each stage as
// pending / in-progress / done when live events arrive.
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
  'seed_repositories',
  'init_repo_root',
  'write_rolodex_config',
  'wait_rolodex_dns',
  'pull_images',
  'start_monitoring',
  'reconcile',
  'reconcile_dns',
  'start_ui_container',
  'refresh_packages',
  'build_handler',
  'ready',
]

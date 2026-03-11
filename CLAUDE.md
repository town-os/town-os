CLAUDE, YOU ARE NOT ALLOWED TO EDIT THIS FILE FOR ANY REASON.

- **MOST IMPORTANT**:
    - DO NOT RUN `make test-full`
    - ONLY RUN `make lint`, `make test`, `make test-integration` for running independent segments. Do not run compiler tools or other build tools.
    - DO NOT FORCE PUSH FOR ANY REASON EVER.
    - when you need to push, push to "origin" only.
    - BEFORE YOU PUSH, ALWAYS "git pull --rebase" and fix any merge issues.

- **Concurrent safety** — `make test-full` must always be able to run simultaneously in the same repository without conflicting. Nothing else matters more than this.

- Add tests for everything you do.

- **`--replace` on all `podman run --name`** — no exceptions, anywhere in the repo.

- **Fail fast** — if any make subtask or script launched by a make subtask fails, stop immediately. Do not continue to the next phase.

- **Never swallow exit codes** — scripts that run make/test commands must never swallow exit codes. No `|| rc=$?`, no `|| true` on test invocations. Let `set -e` do its job. Cleanup commands (podman rm, rm -f) are exempt.

- **No hardcoded shared resources in tests** — all test temp files, sockets, directories, and ports must use unique-per-run paths (`t.TempDir()`, `filepath.Join`, `findFreePort`, etc.). Never use fixed paths like `/tmp/foo.sock`.

- **DO NOT RUN TESTS UNLESS TOLD**

- **DO NOT RUN COMPILER / TEST TOOLS BY HAND. ONLY USE MAKE.**

- **Ephemeral state in /tmp** — all port files, btrfs volumes, dev data, and per-run artifacts
  live in `/tmp/town-os-$(INSTANCE_ID)/`, not the working directory.

- **Only commit or push when told** — never run `git commit` or `git push` unless the user explicitly asks. Never force push (`--force` or `--force-with-lease`).

- systemcontroller should never call os.Exit unless the service is actually being terminated - critical errors should be addressed with fatal logging

- please check all errors. do not underscore or skip error checking for any reason in any part of code ever

- **Test services use random high ports** — integration tests that start network services (DNS, HTTP, gRPC, etc.) must bind to random high ports via `findFreePort`, never well-known ports like 53 or 80. This prevents conflicts when multiple test runs execute simultaneously.

- Please fix all warnings in tests that can be fixed as they arrive

- Package variables should always be translated as a part of the compile step. Fixed package variables should always be tested.

- Ensure all files are organized by api. They should be scoped by subsection name, hierarchically. The metric for line count should be about 500 or so.

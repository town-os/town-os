CLAUDE, YOU ARE NOT ALLOWED TO EDIT THIS FILE FOR ANY REASON.

1. **Concurrent safety** — `make test-full` must always be able to run simultaneously in the same repository without conflicting. Nothing else matters more than this.

2. **`--replace` on all `podman run --name`** — no exceptions, anywhere in the repo.

3. **Fail fast** — if any make subtask or script launched by a make subtask fails, stop immediately. Do not continue to the next phase.

4. **Never swallow exit codes** — scripts that run make/test commands must never swallow exit codes. No `|| rc=$?`, no `|| true` on test invocations. Let `set -e` do its job. Cleanup commands (podman rm, rm -f) are exempt.

5. **No hardcoded shared resources in tests** — all test temp files, sockets, directories, and ports must use unique-per-run paths (`t.TempDir()`, `filepath.Join`, `findFreePort`, etc.). Never use fixed paths like `/tmp/foo.sock`.

6. If running tests is required, **only use make tasks**. Do not invoke compilers, testing harnesses, etc by yourself.

7. **Ephemeral state in /tmp** — all port files, btrfs volumes, dev data, and per-run artifacts
   live in `/tmp/town-os-$(INSTANCE_ID)/`, not the working directory.

8. **Only commit or push when told** — never run `git commit` or `git push` unless the user explicitly asks. Never force push (`--force` or `--force-with-lease`).

9. systemcontroller should never call os.Exit unless the service is actually being terminated - critical errors should be addressed with fatal logging

10. please check all errors. do not underscore or skip error checking for any reason in any part of code ever

11. **Test services use random high ports** — integration tests that start network services (DNS, HTTP, gRPC, etc.) must bind to random high ports via `findFreePort`, never well-known ports like 53 or 80. This prevents conflicts when multiple test runs execute simultaneously.

12. Please fix all warnings in tests that can be fixed as they arrive

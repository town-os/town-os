# Iron Rules (PERMANENT — always follow)

1. **Concurrent safety** — `make test-full` must always be able to run simultaneously in the same repository without conflicting. Nothing else matters more than this.

2. **Iron rule comment** — every `*_test.go` file and build system file (`make/*.sh`, `Makefile`) must have the iron rule comment at the top.
    - Shell/Makefile: `# IRON RULE: make test-full must always be able to run simultaneously in the same repository without conflicting. Nothing else matters more than this.`
    - Go: `// IRON RULE: make test-full must always be able to run simultaneously in the same repository without conflicting. Nothing else matters more than this.`

3. **`--replace` on all `podman run --name`** — no exceptions, anywhere in the repo.

4. **Fail fast** — if any make subtask or script launched by a make subtask fails, stop immediately. Do not continue to the next phase.

5. **Never swallow exit codes** — scripts that run make/test commands must never swallow exit codes. No `|| rc=$?`, no `|| true` on test invocations. Let `set -e` do its job. Cleanup commands (podman rm, rm -f) are exempt.

6. **No hardcoded shared resources in tests** — all test temp files, sockets, directories, and ports must use unique-per-run paths (`t.TempDir()`, `filepath.Join`, `findFreePort`, etc.). Never use fixed paths like `/tmp/foo.sock`.

7. **No unnecessary mount propagation** — do not use `shared` or `rslave` mount propagation on podman volume mounts unless actually required. Simple data directories use `:rw`, not `:shared,rw`.

8. **All tests must pass `make test-full`** — run it after all changes. Only use other make targets to diagnose issues.

9. **Ephemeral state in /tmp** — all port files, btrfs volumes, dev data, and per-run artifacts
   live in `/tmp/town-os-$(INSTANCE_ID)/`, not the working directory. Only the image cache
   (`/var/cache/town-os/images`) and Go build caches (`.cache/go-*`) persist in the repo.

10. **DNS provider continuity** — rolodex always binds to 127.0.0.2:53 (coexisting with
    systemd-resolved) plus optionally the public LAN address. Never disable or re-enable systemd-resolved.
    Production Start: write resolv.conf stub pointing at 127.0.0.2, start rolodex. On failure,
    restore resolv.conf. Production Stop: restore resolv.conf BEFORE stopping rolodex.

11. **Only commit or push when told** — never run `git commit` or `git push` unless the user explicitly asks. Never force push (`--force` or `--force-with-lease`).

12. systemcontroller should never call os.Exit unless the service is actually being terminated - critical errors should be addressed with fatal logging

13. please check all errors. do not underscore or skip error checking for any reason in any part of code ever

14. **Test services use random high ports** — integration tests that start network services (DNS, HTTP, gRPC, etc.) must bind to random high ports via `findFreePort`, never well-known ports like 53 or 80. This prevents conflicts when multiple test runs execute simultaneously.

15. Please fix all warnings in tests that can be fixed as they arrive

CLAUDE, YOU ARE NOT ALLOWED TO EDIT THIS FILE UNLESS I TELL YOU TO.

**This file is build instructions and code style only.** How the system actually
works — architecture, subsystem behavior, API surface, boot ordering, settings,
and the invariants that hold them together — lives in [DESIGN.md](DESIGN.md).
Read DESIGN.md when you need to know what Town OS does; read this when you need
to know how to build it, how to test it, and how to write code in it. When a
change alters behavior, DESIGN.md is the file that needs updating with it.

Translations of this file: Chinese [zh-CN](CLAUDE.zh-CN.md) (Simplified) and
[zh-TW](CLAUDE.zh-TW.md) (Traditional); Spanish [es-ES](CLAUDE.es-ES.md)
(Spain) and [es-MX](CLAUDE.es-MX.md) (Mexico); Japanese
[ja-JP](CLAUDE.ja-JP.md). See also
DESIGN.md ([zh-CN](DESIGN.zh-CN.md), [zh-TW](DESIGN.zh-TW.md),
[es-ES](DESIGN.es-ES.md), [es-MX](DESIGN.es-MX.md), [ja-JP](DESIGN.ja-JP.md)) and
README.md ([zh-CN](README.zh-CN.md), [zh-TW](README.zh-TW.md),
[es-ES](README.es-ES.md), [es-MX](README.es-MX.md), [ja-JP](README.ja-JP.md)).
**This file is authoritative** — when a rule here changes, change it here, and
the translations follow.

- **MOST IMPORTANT**:
    - **Use `make`, not raw compiler/test tools.** Never run `go build`, `go test`, `go vet`, `golangci-lint`, `bun test`, `vitest`, or any equivalent directly. Always go through a make target so the repo's wrappers (cleanup traps, btrfs lifecycle, per-run instance IDs) apply.
    - **Make targets you may run whenever you need them** (fast, idempotent, no remote side effects):
      `make help`, `make lint`, `make check-*` (bun / go / podman / runc / btrfs / libsystemd / golangci-lint). Use these freely to validate changes — you don't need to ask first.
    - **If a make target isn't in either list above, ask first.**
    - DO NOT FORCE PUSH FOR ANY REASON EVER.
    - when you need to push, push to "origin" only.
    - BEFORE YOU PUSH, ALWAYS "git pull --rebase" and fix any merge issues.
    - DO NOT TOUCH GPG IN ANY WAY. Just `git commit` normally. If signing fails, stop and ask the user. Never kill gpg-agent, never use --no-gpg-sign, never try to fix GPG yourself.
    - DO NOT COMMIT WITHOUT SIGNING.
    - DO NOT MESS WITH GPG AGENT FOR ANYTHING

- when parameters are supplied, ensure they are used in the calling function

- **Concurrent safety** — `make test-full` must always be able to run simultaneously in the same repository without conflicting. Nothing else matters more than this.

- context.TODO and context.Background should not be used in golang programs. where possible, please use timeout and cancel contexts to ensure nothing is waiting on a context forever.

- Add tests for everything you do. **Every behavioral change must have both unit tests and integration tests.** Unit tests verify logic in isolation; integration tests verify the feature works end-to-end inside the test container with real systemd, btrfs, and podman. If an integration test cannot be written (e.g., pure UI change), document why in the commit message.

- check all type assertions before using the result

- **Use CMD instead of ENTRYPOINT in container images** — all Containerfiles and inline Containerfile strings must use `CMD` instead of `ENTRYPOINT`. This allows `podman run <image> <command>` to override the default command without `--entrypoint`. Applies to the systemcontroller image, NC image, and any dynamically generated Containerfiles.

- **Every runtime container image must ship a system CA bundle** — any Containerfile (or inline Containerfile string) whose final stage runs Town OS code making outbound HTTPS calls must install `ca-certificates` (debian/ubuntu: `apt-get install ca-certificates`; alpine: `apk add ca-certificates`) unless the base image already provides it (e.g. `caddy`, `oven/bun`). Without a CA bundle, Go's TLS stack fails every HTTPS call with `x509: certificate signed by unknown authority` and failures in background pollers are invisible at default log level (see `fetchExternalIP` silently dropping `ipinfo.io` responses). When adding a new Containerfile, verify the final image has `/etc/ssl/certs/ca-certificates.crt` before considering the image shippable.

- **`--replace` on all `podman run --name`** — no exceptions, anywhere in the repo.

- **Everything podman in the make pipeline runs ROOTFUL via `${SUDO}`** — `SUDO="sudo HOME=$HOME"` in `make/lib.sh`, and **every** `podman` invocation in the make scripts (`build.sh`, `images.sh`, `test.sh`, `dev.sh`, `registry.sh`, `gitea.sh`, `lib.sh`) MUST be `${SUDO} podman`. Rootful and rootless podman have **separate image stores**: base images are pulled/loaded into the root store (`/var/lib/containers`), and the rootless user store is empty. A bare (non-`${SUDO}`) `podman` call therefore hits the empty rootless store and fails with `image not known` under `--pull=never` — even though `${SUDO} podman image exists` reports the image present (different store). When adding any podman command to a make script, always prefix it with `${SUDO}`; never run `make` targets that build/load images under a rootless podman, and do not set `CONTAINER_HOST` to a rootless socket for host-side builds (it would route `${SUDO} podman` to the wrong store). The lone exceptions are availability probes (`command -v podman` in `check.sh`/`preflight.sh`) and the literal `podman` package name in `deps.sh` install lists.

- **No hardcoded public DNS in builds; podman builds use `--network=host`** — every `podman build` in the make pipeline runs with `--network=host` so name resolution goes through the host's resolver (systemd-resolved). Container-network builds get a public resolver substituted for the host's loopback stub, and captive networks (coffee shops, hotels) block direct queries to 1.1.1.1/8.8.8.8 — stalling `bun install`, `apt-get`, and `apk add` indefinitely. For the same reason the NC image used by tests and dev is built **on the host** (`nc-image` / `nc-image-dev` targets → `localhost/town-os-networkcontroller:<INSTANCE_ID>`, binary extracted from the production/dev-base image so it always matches the systemcontroller) and loaded into the containers via the image cache — never built inside them with `--dns`.

- **All test-suite `podman run` containers use `--net host`** — test, UI backend, UI test runner, dev, registry, and gitea containers all run with host networking. Registry and gitea bind their per-instance random port directly via `REGISTRY_HTTP_ADDR` / `GITEA__server__HTTP_PORT` instead of `-p` mappings, and gitea SSH is disabled (`DISABLE_SSH=true`) so nothing tries to bind host port 22. Rationale: bridge-network containers get broken DNS on captive networks, and both registry (Docker Hub pull-through fallback) and gitea (repository migration) make their own outbound calls. The single deliberate exception is the `preflight-dev` nginx container, whose `-p` mapping exists precisely to verify bridge networking works.

- **Image tags are partitioned per architecture** — every pushed tag carries an arch suffix in the raw `uname -m` form (`<arch>` is `x86_64` or `aarch64`). This tag suffix is deliberately distinct from the OCI platform name `amd64`/`arm64`: Go maps `runtime.GOARCH` to the suffix via `archTag()`, make uses `HOST_ARCH` (normalized to `x86_64`/`aarch64`), and shell uses `host_arch_tag` in `make/lib.sh`. The plain `host_arch` / `runtime.GOARCH` values stay `amd64`/`arm64` because podman needs them for `podman pull --platform linux/<arch>` and `.Architecture` comparisons — never feed `x86_64`/`aarch64` to `--platform`. `push-rc` pushes `rc.<date>-<arch>` / `rc.latest-<arch>`; `push-release` pushes `release.<date>-<arch>` / `latest-<arch>`; `push-tag PUSH_TAG=<tag>` pushes `<tag>-<arch>` — always the native arch of the host running the push. **This includes the operator's own tag**: `push-tag` used to push `PUSH_TAG` verbatim, so `make TARGET=x86_64 push-tag` followed by the aarch64 run left quay holding whichever ran second under a name that named no architecture. The plain names (`rc.latest`, `latest`, the date tags, and a custom `PUSH_TAG`) exist ONLY as multi-arch manifest lists assembled by `manifest-rc` / `manifest-release` / `manifest-tag` after every arch in `ARCHES` (`x86_64 aarch64`) has pushed; never push a plain name as a single-arch tag. `TestEveryPushNamesAnArchitecture` asserts every `podman push` in `build.sh` carries `${ARCH}`, so this class cannot return through a new arm — the plain names still ship, but only through `build_manifest`'s `podman manifest push`, which is a different command. The runtime fallback when no tag was baked in is `defaultVersionTag()` in `main.go` (`rc.latest-<arch>`, the `archTag()`-mapped GOARCH). Rationale: a plain single-arch tag pushed from one host fails on the other architecture with `exec format error` (or worse, spuriously passes status-poll tests while crash-looping under `Restart=always`).

- **Plain convenience tags must NEVER be used for testing** — no test, test harness, dev container, or fixture may reference a *plain* (no-arch-suffix) `quay.io/town/*:rc.latest` or `:latest` image (they may not exist or may be stale multi-arch manifests). The per-arch suffixed forms ARE allowed and are the default. Tests use: the host's per-arch rc tag for rolodex (`rc.latest-<arch>`, i.e. `rc.latest-x86_64` / `rc.latest-aarch64`), a locally built UI image (`make ui-image` → `localhost/town-os-ui:<INSTANCE_ID>`), a locally built NC image (`make nc-image`), and neutral fake tags (e.g. `:testtag`) in mocked unit tests where the image is never pulled or run.

- **Test and dev build `localhost/` images; push targets always build a fresh release image** — the `*-local` arms in `make/build.sh` produce `localhost/town-os-*:$(INSTANCE_ID)` for the test and dev harnesses; the `release-*` arms produce `quay.io/town/*`. **No push target may build, tag from, or depend on a `localhost/*` image**, and every push target must build a *new* release image rather than re-tagging whatever the local store happens to hold. This applies to every image, without exception. Rationale: re-tagging a local test image ships bits built for the harness — per-instance tags, `--pull=never` bases, host-arch only, never cross-built — under a release name. On a fresh checkout that fails; on a developer's box it succeeds and ships the wrong bits, which is worse.

- **A local image whose content comes from outside the repo needs an explicit cache-bust** — most `*-local` images build from repo source, so a source change busts their layer cache and they cannot drift from their release counterpart. One whose content is fetched at build time (`Containerfile.gfeh` runs an unversioned `cargo install gfehd`) sits behind a byte-identical `RUN` line, so its layer is a permanent cache hit and it freezes on whatever was current the first time it was built on that machine. Release builds pass `--no-cache`; local fixtures pass a day-granularity build-arg (`GFEH_CACHE_DATE`) so they refresh daily without recompiling on every run. Without one, the integration suite quietly tests a daemon Town OS can no longer run.

- **Fail fast** — if any make subtask or script launched by a make subtask fails, stop immediately. Do not continue to the next phase.

- **Never swallow exit codes** — scripts that run make/test commands must never swallow exit codes. No `|| rc=$?`, no `|| true` on test invocations. Let `set -e` do its job. Cleanup commands (podman rm, rm -f) are exempt.

- **No hardcoded shared resources in tests** — all test temp files, sockets, directories, and ports must use unique-per-run paths (`t.TempDir()`, `filepath.Join`, `findFreePort`, etc.). Never use fixed paths like `/tmp/foo.sock`.

- **Running the allowed make targets is fine without being asked; anything else in the "requires permission" list above needs an explicit OK.** Never invoke `go`, `go test`, `go vet`, `golangci-lint`, `bun test`, `vitest`, etc. directly — always go through make.

- **Nothing in test or build code may use tmpfs** — no file written by any make target, make script, or test harness may live on a tmpfs (RAM-backed) filesystem. This is non-negotiable and absolute: it applies to btrfs loopback backing images, container/volume data, archives, downloads, port files, tracking files, and every other per-run artifact. The reason is fatal, not cosmetic: the test btrfs filesystem is a 50G loopback file, and a loop device backed by tmpfs **deadlocks the host kernel** under memory pressure — tmpfs pages can only be reclaimed to swap, but the loop writeback path needs to allocate memory to drain them, so once tmpfs fills RAM the machine hard-locks and the firmware/watchdog reboots it (observed on Manjaro, where systemd mounts `/tmp` as tmpfs sized at 50% of RAM with near-zero swap). `/tmp` is tmpfs on common dev distros (Arch/Manjaro/Fedora), so **do not assume `/tmp` is disk-backed**. Test/build code that creates a backing file, loop device, or any sizable write target MUST resolve its directory to a real disk-backed filesystem first (e.g. check `findmnt -no FSTYPE <dir>` is not `tmpfs`/`ramfs`, or place the data under a known-disk path like `/var/tmp`) and fail loudly if it cannot. When adding any new path to a make script, verify it is not on tmpfs before writing to it.

- **Ephemeral state location** — per-run bookkeeping (port files, `.disk`/`.loop`/`.mount` tracking files, dev metadata) is scoped per instance under `/tmp/town-os-$(INSTANCE_ID)/`, but any *data-bearing* artifact — above all the btrfs loopback backing image — MUST be placed on a disk-backed path, never tmpfs (see the no-tmpfs rule above). Never put a loopback/disk image, container volume data, or large download on `/tmp` without first confirming `/tmp` is not tmpfs.

- **Only commit or push when told** — never run `git commit` or `git push` unless the user explicitly asks. Never force push (`--force` or `--force-with-lease`).

- systemcontroller should never call os.Exit unless the service is actually being terminated - critical errors should be addressed with fatal logging

- please check all errors. do not underscore or skip error checking for any reason in any part of code ever

- **Always check the `ok` of a comma-ok expression.** Any expression that returns a `value, ok` pair — type assertions (`v, ok := x.(T)`), map index (`v, ok := m[k]`), and channel receive (`v, ok := <-ch`) — must check `ok` before using `value`; never discard it with `_` and never assume the assertion/lookup succeeded. Prefer the comma-ok form over the single-value type assertion `v := x.(T)` (which panics on mismatch): use `v, ok := x.(T)` and handle `!ok` explicitly. This applies to test code too. (Cleanly-typed switch cases — `switch v := x.(type)` — and a deliberate `_ = m[k]` membership write are the only exceptions.)

- Always use inline error syntax in if statements when possible (e.g., `if err := foo(); err != nil {`)

- **Test services use random high ports** — integration tests that start network services (DNS, HTTP, gRPC, etc.) must bind to random high ports via `findFreePort`, never well-known ports like 53 or 80. This prevents conflicts when multiple test runs execute simultaneously.

- **DNS in tests must NEVER touch the host.** No test, test harness, or anything a make test target launches may alter the host's name resolution or occupy the host's DNS port. Specifically, a test run must never:
    - rewrite `/etc/resolv.conf` (that is `redirect_host_dns` in `make/dev.sh`, and it belongs to `make dev` alone),
    - write `/etc/systemd/resolved.conf.d/town-os.conf` or otherwise call `rolodex.ConfigureResolvedRouting`,
    - signal or restart `systemd-resolved` (`pkill -HUP systemd-resolved`),
    - bind **`127.0.0.2:53`**, or any `:53`, in the host network namespace.

  The test container runs `--net host` deliberately (bridge-network DNS breaks on captive networks), so every port a system service binds lands in the **host** namespace. That is exactly why `TOWN_OS_DNS_PORT` is allocated per run into `$(STATE_DIR)/.dns-port` and passed in by `system_port_env` (`make/lib.sh`), and why `main.go` skips resolved routing whenever `dnsPortIsDefault()` is false — a per-domain resolved server address carries no port, so pointing resolved at `DNSLoopback` on a relocated rolodex would blackhole every query for that TLD.

  Treat a test run that leaves `127.0.0.2:53` bound, or a `town-os.conf` drop-in on the host, as a **bug in the harness, not a flaky test**: it means the port override did not reach the container and rolodex fell back to the default. Verify with `ss -lnup | grep 127.0.0.2` and `ls /etc/systemd/resolved.conf.d/` — the only listener on the host's `:53` should be the machine's own resolver, never ours. `make dev` is the sole exception and is opt-in by the operator, because it is meant to mirror a real box.

- **Never write tests that push to remote Gitea or GitHub.**

- **When I tell you to do something, do not argue.**

- **Test git operations should prefer local repos over remote repos when it doesn't matter** — e.g., populate-repos should clone from a local sibling directory if it exists rather than fetching from GitHub.

- Please fix all warnings in tests that can be fixed as they arrive

- Package variables should always be translated as a part of the compile step. Fixed package variables should always be tested.

- Ensure all files are organized by api. They should be scoped by subsection name, hierarchically. The metric for line count should be about 500 or so.


## Release Image Architecture

**Two architectures must be able to build at the same time, in the same
checkout. One build per architecture at a time is all that has to work — an
x86_64 build and an aarch64 build running concurrently is the case that must
never corrupt either one.**

That reduces to a single rule:

- **Nothing a release build produces may be named without its architecture, and
  no push target may tag from a name that lacks one.** Build to
  `$(staged_ref "$IMAGE")` (`make/lib.sh`), which is `<image>:local-<arch>`, and
  tag every published tag from it with `tag_from_staged` — never
  `podman tag "${RELEASE_X_IMAGE}" ...`, which names no architecture and so
  resolves to `:latest`.

WHY, because this shipped: every release image except the systemcontroller was
built as the bare `quay.io/town/<name>` — one `:latest` slot per image, shared
by both architectures — and the push targets re-tagged whatever sat in that slot
at that instant. An aarch64 build and an x86_64 build in the same checkout
overwrite each other's slot, so `rc.latest-x86_64` was published holding arm64
binaries for **ingress, networkcontroller and ui**. Every one of those services
crash-looped on boot with `exec container process: Exec format error`, and
nothing failed at push time to say so. The systemcontroller escaped only because
`push-rc` happened to rebuild it straight into the arch-suffixed tag.

`tag_from_staged` calls `assert_image_arch` before every tag, so a regression of
this class fails at push with the arch it found and the arch it wanted, rather
than on somebody's box. Adding a new release image means adding it to the
staged-ref pattern; a `podman tag` from a bare `${RELEASE_*_IMAGE}` is the bug.

Shared *caches* are fine to keep shared (`.cache/go-mod`, `.cache/go-build`,
`.cache/cargo-registry`, the bun cache): Go and cargo lock their own caches, and
the image cache tars are already keyed by architecture. It is the image *names*
that collide.

The same rule reaches the *base* images a build consumes, not just the ones it
produces:

- **A base image is staged at the architecture the current build wants it at,
  never blanket at the host's.** `BASE_IMAGES_RUNTIME` (the Makefile) is the
  subset of `BASE_IMAGES` that a cross-buildable Containerfile names with a bare
  `FROM` — the stages that ship — and those follow `TARGET`. Everything else is a
  toolchain base that every cross Containerfile pins with
  `FROM --platform=$BUILDPLATFORM` because it runs HERE and cross-compiles, so
  the host arch is correct for it under any `TARGET`. Staging a toolchain base at
  the target arch is this same bug pointed the other way.

WHY: `load-base` is a prerequisite of nearly every build target, `release-image`
under `TARGET=aarch64` included, and it looped `BASE_IMAGES` calling
`ensure_image` with no architecture. So a cross build's own prerequisites forced
`debian:bookworm-slim` back to amd64 moments before the release arm needed arm64,
which staged it back, which the next invocation's prerequisites undid. Every
cross invocation paid an `rmi` plus a load in each direction — and a network pull
whenever the tar it wanted was missing — while `podman image inspect` reported
the host arch throughout, which reads exactly like the target-arch staging never
happening at all.

Two consequences worth keeping:

- **Each build arm stages its own bases, local arms included**, rather than
  leaning on `load-base`'s global pass. `gfeh-image` and `release-gfeh-image`
  have no `.images-pulled` prerequisite, so before this nothing had ever staged
  `debian:bookworm-slim` for them and they resolved against whatever the last
  build happened to leave behind.
- **The implicit `FROM` fetch is not free.** Cross builds drop `--pull=never` so
  podman can fetch the target-arch runtime base itself, and that fetch goes
  nowhere near `ensure_image` — it neither reads the cache nor writes one.
  Staging the base through `ensure_image` *before* the build asks for it is what
  makes the first cross build of a base the last one that costs anything.

The guards are derived rather than listed, so a new stage cannot quietly opt out:
`TestBaseImagesRuntimeMatchesTheContainerfiles` computes the expected membership
from the cross-buildable Containerfiles and fails in both directions, and
`TestBuildArmsStageEveryRuntimeBase` does the same per arm.

Two more rules follow from the same place — a cross release is not cross all the
way through:

- **The test phase of a release is NATIVE, whatever `TARGET` the artifacts are
  for.** `release-build` depends on `release-test`, which recurses with `TARGET=`
  cleared, never on `test-full` directly. `test-full` builds the integration
  harness and *runs* it here, so each of those arms calls `require_native_target`
  and refuses a foreign `TARGET` — naming `test-full` directly meant
  `make TARGET=aarch64 release-build` died on `make/build.sh ui-integration`
  before it built a single release image, and `push-release` died with it. The
  tests validate the SOURCE, on the machine running them; the cross part of a
  release is the artifacts built afterwards.
  (`TestReleaseBuildRunsItsTestsNatively`.)

- **An image that exists for one architecture is SKIPPED on the others, not
  attempted.** The Proton runner is x86_64 by construction (GE-Proton ships
  x86_64 Wine), so `release-proton-image` refuses any other `TARGET` — correct
  for the single target and wrong for an aggregate that names it
  unconditionally. The Makefile drops it via `$(PROTON_RELEASE_TARGET)`, and
  **every arm has to agree**: a push arm that still tagged it would reach for a
  staged image nothing built, and `build_manifest` over `ARCHES` would look for a
  `-aarch64` tag that was deliberately never pushed. Hence `build_manifest`'s
  optional arch list, which defaults to `ARCHES` and is passed `x86_64` for
  Proton. Adding another single-arch image means repeating all three.
  (`TestProtonStaysOnItsOwnArchitecture`.)


## Performance Conventions

- **Use `strings.Builder` for string construction** — never build strings character-by-character with `string(append([]byte(s), c))`. Use `strings.Builder` with `WriteByte`/`WriteString` for O(n) instead of O(n²) allocations. See `src/packages/packages_compile.go` (`applyTemplate`, `applyTemplates`).

- **Pre-allocate slices when size is known** — use `make([]T, 0, capacity)` when the result size or an upper bound is known (e.g., `limit` from pagination). Avoid `var items []T` followed by unbounded `append` in hot paths.

- **Single-query pagination with `COUNT(*) OVER()`** — paginated list endpoints must use the SQLite window function `COUNT(*) OVER()` in the SELECT column list instead of running a separate `COUNT(*)` query. Scan the total alongside each row.

- **Index columns used in WHERE clauses** — every SQLite column used in a `WHERE` filter (especially `created_at`, `success`, `account`) must have an appropriate index. Composite indexes should match common filter combinations (e.g., `(success, created_at)` for `CountRecentErrors`).

- **Cache expensive repeated lookups** — `RepositoryRoot.LoadPackages()` results are cached in a `sync.Map` per repo name, invalidated on `ForceRefresh()`. Callers must use `cachedLoadPackages()` instead of `LoadPackages()` directly. Similarly, `GetInternalIP()` caches the result in an `atomic.Value` instead of calling `net.InterfaceAddrs()` per request.

- **Direct lookups over full scans** — use `GetInstalledVersion(repo, name)` (reads `installed/<repo>/<name>/` directly) instead of `ListInstalled()` + linear search when checking a single package.

- **Parallel I/O for independent operations** — container image pulls in `refreshSystemServices` use goroutines with a semaphore (max 3 concurrent) instead of a sequential loop. Use `sync.WaitGroup` + channel semaphore; do not add `errgroup` dependency.

- **Server-scoped context for background goroutines** — background goroutines (pages git clone, image extraction) must use the server-scoped context (`s.ctx`) instead of `context.Background()` so they respect graceful shutdown. They must NOT use the HTTP request context (the operation must outlive the request).

- **Batch dependency loading in reconcile** — dependency records for all packages are pre-loaded into a map before the reconcile loop, not loaded per-package inside the loop.


## Development Prerequisites

Building Town OS from source requires:

- **Go 1.25+** -- with CGO enabled for the system controller (links against libsystemd).
- **libsystemd-dev** -- C development headers for the systemd journal and dbus bindings, required by the `go-systemd/v22` dependency.
- **Bun** -- JavaScript runtime for the UI build and tests.
- **Podman** -- rootful (`sudo`), used for container operations.
- **btrfs-progs** -- provides `mkfs.btrfs` for creating test and dev btrfs volumes.
- **golangci-lint** -- for Go linting.
- **QEMU** -- `qemu-system-x86_64` for running VM packages; `qemu-img` for converting VM disk images to raw format.

### Bootstrap

`make deps` installs every host dependency (Go, podman, runc, btrfs-progs,
libsystemd headers, golangci-lint, bun, qemu, build tools) on a fresh Arch
or Ubuntu/Debian machine. It is implemented in `make/deps.sh`, detects the
distro from `/etc/os-release`, and is safe to re-run.

`make help` (the default target) prints a grouped list of every user-facing
make target. Implemented in `make/help.sh`. Keep both scripts in sync when
adding or renaming targets in `make/include.mk`.

### Preflight Checks

The Makefile provides a `preflight-dev` target that validates the development environment before running tests or starting the dev server. It checks:

- **podman** -- verifies the `podman` command is available in PATH.
- **btrfs-progs** -- verifies the `mkfs.btrfs` command is available in PATH.
- **Repository credentials** -- verifies `TOWN_OS_REPO_USERNAME` and `TOWN_OS_REPO_PASSWORD` environment variables are set.
- **Bridge networking** -- starts a test nginx container with a port binding to verify podman's `-p` flag works correctly.

Each check prints a descriptive error message and exits with a non-zero status on failure. All checks must pass before the message "All preflight checks passed." is displayed.

### Ubuntu / Debian Installation

On Ubuntu or Debian systems, install the system dependencies with:

```
sudo apt-get install -y libsystemd-dev btrfs-progs podman runc qemu-system-x86 qemu-utils
```

Go, Bun, and golangci-lint must be installed separately (see their respective upstream documentation).

## Code Quality

### Error Handling

All Go error return values must be explicitly checked. The `errcheck` linter is enabled project-wide and the blank identifier (`_ =`) must not be used to discard errors.

In production code, cleanup errors in deferred functions are combined with the primary error using `errors.Join()` via named return values (e.g., `defer func() { err = errors.Join(err, f.Close()) }()`). Non-critical best-effort operations log errors rather than discarding them.

In test code, cleanup errors are reported via `t.Errorf` or `t.Logf` depending on severity, or explicitly suppressed with a `//nolint:errcheck` annotation and justification comment.

All `//nolint` directives require a justification comment (enforced by `nolintlint`).

## Integration Testing

### Local Docker Registry

Integration tests run against a local `registry:2` container to avoid Docker Hub rate limits and ensure reproducibility. The process is:

1. **Image discovery** -- the `discover-images` tool scans all test package repositories for `docker.io` image references, including main images and archive images. Results are deduplicated and written to `.cache/.registry-images`.
2. **Registry start** -- a `registry:2` container is started on a random port.
3. **Image mirroring** -- each discovered image is pulled from Docker Hub, re-tagged with the local registry address, and pushed to the local registry (TLS verification disabled for localhost).
4. **Registry configuration** -- a `registries.conf` file is generated that redirects `docker.io` pulls to the local mirror. This is mounted into the test container at `/etc/containers/registries.conf.d/`.
5. **Transparent operation** -- no code changes are needed; podman automatically uses the local mirror. The mirror falls back to Docker Hub for uncached images.

Each working directory gets its own registry instance (via `INSTANCE_ID`) so concurrent test runs do not conflict.

### Local Gitea Server

Integration tests use a local Gitea instance to avoid GitHub rate limits for git operations. The process mirrors the local Docker registry pattern:

1. **Server start** -- a `gitea/gitea:latest` container is started on a random port with installation pre-locked. An admin user (`town-os`) is created automatically.
2. **Repository migration** -- the `populate-repos` tool migrates test package repositories (`test-packages-core`, `test-packages-extras`) from GitHub into the local Gitea instance using the Gitea migration API. Migration is idempotent: existing non-empty repositories are skipped; empty repositories from failed migrations are deleted and retried.
3. **Transparent operation** -- tests receive local Gitea URLs via environment variables (`TOWN_OS_TEST_REPO_CORE_URL`, `TOWN_OS_TEST_REPO_EXTRAS_URL`). When these are not set, tests fall back to the default GitHub URLs.

Each working directory gets its own Gitea instance (via `INSTANCE_ID`) so concurrent test runs do not conflict. Image discovery reads from local Gitea repositories when available.

### Container Cleanup

The `test-full` target runs `clean-integration` and `clean-btrfs` after integration tests complete, ensuring all test containers (test, registry, gitea, ui-backend, ui-integration) and btrfs loopback mounts are torn down even when tests fail. The `clean-dev` target removes all `town-os-dev` containers before cleaning caches. A `clean-containers` target removes all Town OS containers (matching `town-os-*` and `preflight-test-*` patterns) from any instance or working directory. The `clean-integration` target uses error-tolerant container removal for idempotent cleanup. The `clean-all` target uses `clean-containers` for comprehensive cleanup across instances. Monitoring images are pre-loaded into integration test containers from the image cache.

### Btrfs Loopback Cleanup

Test targets (`test-integration`, `test-ui-integration`, `test-full`) use shell EXIT traps to guarantee btrfs cleanup runs regardless of test success, failure, or signal interruption. Recipes are organized in shell scripts under `make/`. Btrfs volume creation is performed inside the test scripts after the EXIT trap is registered, ensuring loop devices cannot leak even if creation or subsequent steps fail.

The `clean-btrfs` target performs best-effort cleanup (no `set -e`): unmounts the btrfs filesystem, detaches loop devices found via `losetup -j` for the disk image file, and removes state tracking files (`town-os.disk`, `town-os.loop`, `town-os.mount`). A safety net scans all active loop devices (`losetup -a`) for any backed by btrfs image files in the current directory and detaches orphaned devices even when tracking files are missing.

### Test File Organization

Integration test files are organized by component and subfunctionality. Each file focuses on a specific area: btrfs operations, git operations, repository management, and system controller subsystems. System controller tests are further split into separate files for archives, bootstrap, filesystems, installation (mock and real systemd), multi-repo scenarios, networking, packages, pages, reconciliation, repositories, settings, systemd units, and volumes. Common test initialization and helper functions are centralized in a dedicated helpers file.

### Test Environment

Integration tests run inside privileged podman containers with systemd, btrfs, and the full test binary. The container includes podman and runc for running package containers. Tests exercise real systemd unit lifecycle, btrfs volume management, and container operations.


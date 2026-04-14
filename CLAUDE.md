CLAUDE, YOU ARE NOT ALLOWED TO EDIT THIS FILE UNLESS I TELL YOU TO.

- **MOST IMPORTANT**:
    - DO NOT RUN `make test-full`
    - ONLY RUN `make lint`, `make test`, `make test-integration` for running independent segments. Do not run compiler tools or other build tools.
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

- Always use inline error syntax in if statements when possible (e.g., `if err := foo(); err != nil {`)

- **Test services use random high ports** — integration tests that start network services (DNS, HTTP, gRPC, etc.) must bind to random high ports via `findFreePort`, never well-known ports like 53 or 80. This prevents conflicts when multiple test runs execute simultaneously.

- **Never write tests that push to remote Gitea or GitHub.**

- **When I tell you to do something, do not argue.**

- **Test git operations should prefer local repos over remote repos when it doesn't matter** — e.g., populate-repos should clone from a local sibling directory if it exists rather than fetching from GitHub.

- Please fix all warnings in tests that can be fixed as they arrive

- Package variables should always be translated as a part of the compile step. Fixed package variables should always be tested.

- Ensure all files are organized by api. They should be scoped by subsection name, hierarchically. The metric for line count should be about 500 or so.

- **Pages feature requires `TOWN_OS_PAGES` env var** — the pages subsystem (static site hosting via Caddy) is disabled at runtime unless `TOWN_OS_PAGES` is set to a non-empty value. When unset, the pages manager is nil and all pages API endpoints return "pages not configured". The code and tests are not removed; tests configure pages directly via `ServerConfig.PagesMgr` and are unaffected by this gate. The env var is intended to be baked into the container image at build time via `ENV TOWN_OS_PAGES=1` in the Containerfile.

- **Version change detection and unit restart** — the systemcontroller detects image upgrades by comparing the running container's image SHA (from `/proc/1/cgroup` → `podman inspect`) against a persisted version file at `<btrfsPath>/town-os-version`. On version change: (1) all container images are pulled, (2) the NC image is rebuilt, (3) reconcile regenerates all systemd units, (4) units whose content changed are restarted in order: NC units first (they own networks), then dependency services, then parent/standalone services, (5) post-update commands (`post_update` field) are executed via `podman exec` for container packages whose units changed. The version file is written after successful reconcile. Unit content is compared before/after via `ReadUnit()` to avoid unnecessary restarts when content hasn't changed.

- **Network controller image is built as the first boot operation** — the NC container image (`localhost/town-os-networkcontroller:local`) is built via `podman build` as the very first runtime operation in the systemcontroller startup, before rolodex config, image pulls, or anything else. The systemcontroller container shares the host's podman storage (`/var/lib/containers`) so images built inside the container are directly available to host systemd units. The NC binary (`/town-os-networkcontroller`) is baked into the systemcontroller image at build time, guaranteeing the NC always matches the running systemcontroller. The `alpine:latest` base image is pulled via `ensureImage` if not already loaded. The build uses `--dns=8.8.8.8` so it does not depend on rolodex DNS. The build is non-fatal; if the network is unavailable at boot, the system controller starts without per-package networking and the image is built on the next restart. Never cache or skip the NC image build.

- **All monitoring services are system services** — Prometheus, Node Exporter, and the Monitoring UI all run under the system service namespace (`town-os-system--` prefix), started directly from `main.go` before reconcile. They are never installed through the package repository system; there is no installable "monitoring" package. The three services are: `town-os-system--node-exporter.service` (host networking, port 9100), `town-os-system--prometheus.service` (port 9090, bind-mount config/data from `{btrfsBase}/monitoring/`), and `town-os-system--monitoring-ui.service` (port 5308). The monitoring UI service runs either a socat forwarder (uPlot mode, default) or Grafana (grafana mode), controlled by the `monitoring_backend` setting. Prometheus config is written directly to disk. Prometheus, Grafana, and the uPlot socat forwarder are generated via `systemd.GeneratePackageUnits` with `PackageUnitConfig.SystemServiceKey` set, so they get a full network controller, socket activation, and a private podman network — the same plumbing as regular packages, but with the system service naming.

- **Grafana storage writability is self-healing** — the Grafana container runs as uid:gid 472:472 and aborts at startup with `GF_PATHS_DATA='/var/lib/grafana' is not writable` when the bind-mounted data directory is owned by anyone else. The monitoring-ui service must recover automatically from wrong ownership on every boot, without manual `chown`. The fix has three layers, all authoritative:
  1. **`EnsureGrafanaStorage` (Go, boot-time)** in `src/monitoring/monitoring_ui.go` creates `monitoring/grafana-data` and `monitoring/grafana-provisioning` as btrfs subvolumes via the `storage.Storage` interface. It is idempotent: existing paths are left alone. It falls back to `os.MkdirAll` when subvolume creation fails (for example when the parent `monitoring/` directory already exists as a plain directory because `StartPrometheus` created it via `ExecStartPre=/bin/mkdir -p` earlier in boot). After creation, it recursively `os.Lchown`s the tree to 472:472.
  2. **Proactive chown after provisioning writes** — `StartMonitoringUI` re-runs `chownGrafanaTree` after `WriteGrafanaProvisioningFiles` so the freshly-written `prometheus.yml` datasource and `default.yml` dashboard provider (which inherit the systemcontroller's uid from `os.WriteFile`) are owned by Grafana.
  3. **`ExecStartPre=/bin/chown -R 472:472`** on both `grafana-data` and `grafana-provisioning` in the generated systemd unit, so every restart (including systemd's own restart-loop recovery) re-fixes ownership before Grafana starts.

  The Go-side chown is best-effort (per-entry failures logged at debug, not returned) so tests running as non-root do not break; the ExecStartPre chown runs as root via systemd and is the authoritative fix. All three layers must be present — removing any one of them means a broken ownership state can persist across boots. The uid/gid constants live in `monitoring_ui.go` as `grafanaUID = 472`, `grafanaGID = 472`; do not change these without matching the upstream Grafana image.

- **Network state directory must be host-shared** — the `-network-state` default is `/run/town-os` (`DefaultNetworkStatePath` in `src/svc/systemcontroller/cmd/systemcontroller/main.go`). The systemcontroller runs inside a container but creates NC containers on the host via `CONTAINER_HOST`, so the bind-mount source path (`-v /run/town-os:/run/town-os:ro` in every NC unit) must exist on the host filesystem. The install-repo systemcontroller systemd unit must bind-mount `/run/town-os:/run/town-os` and ensure the host directory exists before the systemcontroller starts (`ExecStartPre=/usr/bin/mkdir -p /run/town-os` or `RuntimeDirectory=town-os`). Without that mount, the systemcontroller's `os.MkdirAll` and state-file writes land inside the container's tmpfs, the host directory does not exist, and NC containers fail to start with `Error: statfs /run/town-os: no such file or directory` — taking down Prometheus, the monitoring UI, and every package with networking. Never default to `/var/run/town-os` or any path under `/var/run` or `/tmp`; the path must live under `/run` (or another host-shared bind mount) and must be the same path on both sides of the mount.

## System Controller Boot Sequence

The system controller startup in `src/svc/systemcontroller/cmd/systemcontroller/main.go` follows this exact order. Each step that says **(non-fatal)** logs to stderr and continues; everything else is fatal and aborts startup.

1. **Set `CONTAINER_HOST`** — `setupPodmanEnv()` sets `CONTAINER_HOST=unix:///run/podman/podman.sock` so every subsequent `podman` invocation (and child process) routes through the host's podman socket instead of the systemcontroller container's isolated storage.
2. **Parse CLI flags and env vars** — `-db`, `-btrfs`, `-repo-dir`, `-network-state`, `-listen`. Env overrides: `TOWN_OS_LISTEN`.
3. **Create directories** — temp working dir, btrfs base, network state dir, DB parent dir.
4. **Build network controller image** **(non-fatal)** — the very first runtime operation that touches podman, before DB open, manager init, or anything else. Copies the NC binary from `/town-os-networkcontroller`, pulls `alpine:latest` via `ensureImage`, runs `podman build`. The SC container shares the host's podman storage (via `CONTAINER_HOST`) so the image is immediately available to host systemd units. Uses `--dns=8.8.8.8` so it does not depend on rolodex.
5. **Open SQLite database** — persistent if `-db` is set, otherwise ephemeral temp file.
6. **Init account manager** — creates accounts table.
7. **Generate ephemeral JWT signing key** — 32 random bytes via `crypto/rand`, overridable with `TOWN_OS_SIGNING_KEY`. Init session manager, which clears all prior sessions (old tokens are invalid with the new key).
8. **Init audit manager** — creates audit log table.
9. **Init settings manager** — creates settings table with defaults (`default_quota`, `max_archive_size`, `locale`, etc.).
10. **Init pages manager** — only if `TOWN_OS_PAGES` env var is set.
11. **Seed repositories** — if `repositories.json` does not exist, write default repos (or test repos if `TOWN_OS_TEST`/`DEBUG`). Apply `TOWN_OS_REPO_USERNAME`/`TOWN_OS_REPO_PASSWORD` credentials.
12. **Init repository root and force refresh** — clones/fetches all configured repos via go-git.
13. **Init install manager, btrfs storage, systemd manager**.
14. **Read version tag** — from `/town-os.tag` file, or compile-time `Version` ldflags var, or fallback `rc.latest`. Used to derive image tags for sibling services.
15. **Start background repo refresh** — goroutine polls every 5 minutes.
16. **Write Rolodex config and restart if changed** **(non-fatal)** — Rolodex is a boot service managed by systemd. The systemcontroller writes `rolodex.yml` (idempotent: skips if file is newer than the binary or content is unchanged) and restarts the service only when the file was written. The rolodex container runs with `--net host` and binds DNS to `127.0.0.2:{port}` directly (not via podman port mappings). The DNS port defaults to 53 but can be overridden via the `DNSPort` config field for tests.
17. **Wait for Rolodex DNS readiness** **(non-fatal)** — polls until the DNS TCP port accepts connections.
18. **Read monitoring backend setting** — `monitoring_backend` from settings DB, default `uplot`.
19. **Pull container images** **(non-fatal)** — Prometheus, Node Exporter, UI image, optionally Grafana. Uses `ensureImage` (skips pull if already loaded). The SC shares the host's podman storage so pulled images are available to host systemd units.
20. **Start monitoring system services** **(all non-fatal)** — Node Exporter (host networking, port 9100), Prometheus (port 9090, bind-mount config/data, `ExecStartPre` chowns data dir to uid 65534), Monitoring UI (port 5308, socat with `--pull=never` from NC image, or Grafana).
21. **Detect version change** — compare running container's image SHA (`/proc/1/cgroup` → `podman inspect`) against `<btrfsPath>/town-os-version`. Sets `versionChanged` flag for reconcile.
22. **Reconcile** — iterates all installed packages and restores runtime state:
    - Creates root btrfs subvolumes (`installed`, `uninstalled`, `archives`, `pages`, `vm-images`, `user`).
    - For each installed package (latest version per repo/name): loads YAML, compiles with saved responses, creates btrfs volumes with quotas, seeds empty volumes from archives/git/proton, applies file templates, writes network state files, generates and installs systemd units (service + NC + sockets), starts services.
    - If `versionChanged`: restarts units whose content changed (NC first, then deps, then services), then runs `post_update` commands.
    - Reconciles pages: ensures subvolumes, symlinks, Caddyfile, and Caddy unit.
23. **Persist version SHA** — writes current image SHA to `<btrfsPath>/town-os-version`.
24. **Reconcile DNS** — connects to Rolodex gRPC socket (retries up to 30s), sets up TLD zone, registers A records for all installed packages.
25. **Start UI container** **(non-fatal)** — installs and starts `town-os-system--ui.service` (Caddy, host networking, port 80).
26. **Create HTTP handler** — wires all managers into `ServerConfig`, starts external IP poller (ipinfo.io, 1-hour interval), configures Echo router with CORS, auth middleware, audit middleware.
27. **Start HTTP server** — listens on `:5309` (or `TOWN_OS_LISTEN`). Prints `systemcontroller: listening on :5309` to stderr. **System is now ready.**
28. **Graceful shutdown** — on SIGINT: cancel context, shutdown HTTP server with 30s timeout. All background goroutines exit via context cancellation.

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

# Town OS Functional Specification

Town OS is a self-hosted cloud platform for home users. It runs entirely from a USB drive in RAM, using all system storage for user data. Packaging, storage, and networking are fully integrated. A web UI provides management for non-technical users.

## Git Library

All internal git operations use a pure-Go library (`go-git/go-git/v5`) rather than shelling out to the `git` CLI.

### Client Interface

The `git.Client` interface abstracts all git operations:

- **Clone** -- clone a repository into a named subdirectory of a parent directory.
- **Pull** -- pull with rebase.
- **Diff** -- report whether the working tree has uncommitted changes.
- **Stash / StashApply** -- stash and re-apply uncommitted changes.
- **Fetch** -- fetch from the origin remote.
- **Checkout** -- check out a branch, tag, or commit hash.
- **Init** -- initialize a new repository. Returns an error if the parent directory does not exist.
- **Add** -- stage files by pathspec (supports `"."` for all files).
- **Commit** -- create a commit using the local git user config (falls back to `Town OS <town-os@localhost>`).
- **RevParse** -- resolve a reference to a SHA hash.
- **Run** -- dispatch arbitrary git subcommands (`config`, `branch`, `rev-parse --abbrev-ref`, `log`, `init`, `status`).

### Implementation

`GoGitClient` implements the interface using `go-git`. It supports:

- Embedded URL credentials (`scheme://user:pass@host/...`), extracted and passed as `http.BasicAuth`.
- Context-based timeouts and cancellation on all operations.
- A `Home` field that overrides the HOME directory for isolated operations.

### Mock Client

`MockClient` provides a thread-safe mock implementation for unit testing. It records all method calls with arguments and supports injectable errors and return values per method.

### Usage

- **Package repositories**: clone, pull (with stash/apply around dirty trees), and fetch for repository refresh (via `GoGitClient`).
- **Volume seeding**: clone git repositories into empty volumes during install and reconcile (via `GoGitClient`).
- **Pages**: clone and update static site repositories (via `GoGitClient`).
- **Git source rebuild**: update installed package git volumes and restart the dependent service (via `GoGitClient`).

## Repository Management

### Repository Model

Repositories are defined by a name, URL, and optional credentials (username and password). They are stored in a `repositories.json` file in the base directory. A default repository is seeded if none are configured.

### Repository API

- `POST /repository/add` (auth required) -- add a new repository. Accepts name, URL, and optional username/password credentials. If no credentials are provided, system-default credentials are used. The repository is cloned via go-git and a refresh is triggered.
- `POST /repository/remove` (auth required) -- remove a repository by name and trigger a refresh.
- `POST /repository/move` (admin required) -- change the priority position of a repository. Accepts name and target position index.
- `POST /repository/refresh` (auth required) -- force-refresh all repositories. Returns any refresh errors.
- `GET /repository` (auth required) -- list all repositories with search, sorting, and pagination. Each entry includes name, URL, username, and any refresh error.

### Repository Refresh

Repositories are refreshed periodically (default 5-minute interval) by fetching from origin via go-git. Stash/apply is used around dirty trees during refresh. Refresh errors are tracked per repository and exposed via the list and status ping endpoints.

## Package System

### Package Definition

Packages are defined in YAML with the following structure:

- `image` -- container image reference (mutually exclusive with `vm`).
- `vm` -- virtual machine configuration (mutually exclusive with `image`). See **VM Configuration** below.
- `proton` -- Proton/Wine runner configuration for Windows executables (mutually exclusive with `vm` and `command`). See **Proton Configuration** below.
- `command` -- container entrypoint override (container runtime only; mutually exclusive with `proton`).
- `environment` -- key-value environment variables (supports template substitution; container runtime only).
- `network` -- external and internal port mappings (supports template substitution).
- `volumes` -- named volumes with mountpoint, optional quota, optional archive source, optional git seed URL, and optional UID/GID.
- `questions` -- named questions presented to the user during installation.
- `notes` -- typed metadata (URL, phone, email) displayed after installation. Types are validated during compilation: URLs must parse as valid URLs, emails must match `user@domain.tld` format, and phone numbers must match digits with optional formatting characters.
- `description` -- human-readable package description.
- `supplies` -- list of capabilities this package provides.
- `archives` -- list of container image archives to populate volumes at install time (container runtime only).
- `templates` -- named file templates rendered into volumes via Go text/template. Each template specifies a target volume, file path, and template content.
- `post_update` -- list of shell commands to execute inside the running container after an image SHA change is detected during reconcile (container runtime only; not supported for VM packages). See **Post-Update Commands** below.

### Runtime Type

Each package has a runtime type: `container` (default) or `vm`. The runtime is determined by which top-level field is present: `image` (or `proton`) selects the container runtime (podman), `vm` selects the VM runtime (QEMU). A package must specify exactly one of `image`/`proton` or `vm`; specifying both or neither is a validation error. Proton packages are a specialized form of container package -- they use the container runtime but auto-generate the command and extract Windows application files from a separate container image.

### VM Configuration

The `vm` section configures a QEMU virtual machine:

- `image` -- VM disk image URL or local filename (required). Can be an HTTP/HTTPS URL for remote images or a filename referencing a cached image in the `vm-images` subvolume. Supports `@variable@` template substitution.
- `memory` -- VM memory as a human-readable byte string (e.g., `2gb`, `512mb`). Defaults to `1gb`. Supports `@variable@` template substitution.
- `cpus` -- number of virtual CPUs. Defaults to `1`. Must be non-negative.

### Proton Configuration

The `proton` section configures a Windows application to run via the Proton/Wine compatibility layer:

- `app_image` -- container image reference containing the Windows application files (required). Normalized during compilation. Supports `@variable@` template substitution.
- `app_directory` -- absolute path inside the container where the application is installed (required, e.g., `/app`). Supports `@variable@` template substitution.
- `volume` -- name of a defined package volume where the application files will be extracted (required). Supports `@variable@` template substitution.
- `exe` -- path to the Windows executable to run (required, e.g., `/app/myapp.exe`). Supports `@variable@` template substitution.
- `args` -- optional command-line arguments passed to the executable. Each element supports `@variable@` template substitution.

At install time, the system pulls `app_image`, extracts `app_directory` into the named volume, and auto-generates the container command as `proton run <exe> [args]`. The container image used to run the application is the system-wide `proton_image` setting (`quay.io/town/proton:latest` by default), which can be overridden per-package by setting `image`. During reconcile, app extraction is repeated only if the target volume is empty.

### Template Variables

Template substitution uses `@variable_name@` syntax. Variables are replaced with question responses during package compilation. Substitution applies to: environment values, network port names and destinations, volume mountpoints, volume quotas, volume archive references, volume git URLs, VM image URLs, and VM memory values. Two built-in variables are also available: `@LOCAL_EXTERNAL_HOST@` and `@LOCAL_INTERNAL_HOST@`.

The `@@` sequence is a literal `@` escape. To produce a literal `@` followed by a template variable, use three `@` signs: `@@@variable@`. For example, `ssh://git@@@PACKAGE_DNS@:@sshport@` resolves to `ssh://git@gitea.default.home:2222`. A standalone `@@` resolves to `@` (e.g., `admin@@example.com` → `admin@example.com`).

Note compilation uses a single-pass resolver (`ApplyTemplates`) that merges context variables (`PACKAGE_DNS`, `LOCAL_EXTERNAL_HOST`, `LOCAL_INTERNAL_HOST`) and user responses into one pass, correctly handling `@@` escapes. Other fields (environment, ports, volumes) use a per-key resolver (`applyTemplate`) that preserves `@@` through multiple passes, with a final `@@` → `@` resolution at the end of `Compile`.

### Questions

Questions prompt the user during package installation. Each question has a `query` (display text), an optional `type` (output type for validation), and an optional `default` value. Question names must be alphanumeric only.

#### Output Types

- **port** -- validated port number (1--65535). Auto-generates a random available port in the range 10000--60000 when the response is empty or `"auto"`.
- **hostname** -- lowercase alphanumeric with dashes. Auto-generates `<package-name>-<4-char-hex>` when empty.
- **volume** -- alphanumeric with dashes and underscores.
- **bytes** -- human-readable byte sizes (`mb`, `gb`, `tb` suffixes).
- **archive** -- archive file name.
- **duration** -- time durations (`s`, `m`, `h`, `d` suffixes).
- **secret** -- auto-generates a cryptographically secure value when the response is empty or `"auto"`. Generates 32 bytes via `crypto/rand`, returned as a 64-character hex string (256 bits of entropy). Suitable for passwords, encryption key salts, and other secret values. Users can override by providing an explicit response.

### Compilation

Compilation validates all responses, applies type-specific validation, substitutes all template variables, normalizes container image URLs, and produces a resolved `Package` struct. For VM packages, memory strings are parsed to byte counts and CPU defaults are applied. Post-update commands are trimmed of leading/trailing whitespace. Validation errors are collected and returned together.

### Post-Update Commands

The `post_update` field is a list of shell command strings executed inside the running container after the system controller detects an image SHA change during reconcile. This enables automated migration tasks (e.g., `pg_upgrade` after a PostgreSQL container updates).

- **Container-only** -- `post_update` is rejected during validation for VM packages (`ErrPostUpdateVMNotSupported`).
- **Template substitution** -- each command supports `@variable@` substitution from question responses, identical to environment and network fields.
- **Whitespace trimming** -- each command is trimmed of leading/trailing whitespace during compilation. Empty or whitespace-only commands are rejected during validation.
- **Execution trigger** -- commands execute only when `ReconcileConfig.VersionChanged` is true AND the package's systemd unit content differs from the previously installed unit. If either condition is false, no commands run.
- **Execution order** -- commands run sequentially after all version-change restarts complete (NC units first, then dependencies, then services, then post-update commands). Within a package, commands run in list order.
- **Execution method** -- each command is run via `podman exec <container-name> sh -c '<command>'` with a 5-minute timeout. The `PostUpdateExec` function on `ReconcileConfig` provides the execution mechanism; nil disables post-update execution.
- **Non-fatal** -- command failures are logged but do not stop reconcile or prevent subsequent commands from running.

Example package YAML:

```yaml
image: postgres:16
post_update:
  - "pg_upgrade --check"
  - "pg_upgrade"
  - "vacuumdb --all --analyze-in-stages"
```

### File Templates

Templates are named objects in the package YAML with three fields: `volume` (target volume name), `path` (file path within the volume), and `content` (Go text/template string).

The template data context provides three namespaces:

- `.Responses.key` -- question response values (keyed by question name).
- `.Package.Name`, `.Package.Version`, `.Package.Repo`, `.Package.Image`, `.Package.Description` -- package metadata.
- `.System.Hostname`, `.System.ExternalIP`, `.System.InternalIP` -- system-level information.

The `volume` and `path` fields support `@variable@` substitution (the same mechanism used by environment, network, and volume fields). The `content` field uses Go `text/template` syntax with `{{.Responses.key}}`, `{{.Package.Name}}`, etc.

Templates are applied after volume seeding (archives, git clones) but before service boot. During reconcile, templates are re-rendered but existing files are never overwritten, preserving data from archive uploads or previous runs.

Validation enforces: template names follow the volume naming convention (alphanumeric with dots, dashes, and underscores), paths must be relative with no directory traversal, the volume must reference a defined package volume (unless the volume field contains template variables), and content must parse as valid Go `text/template`.

### Image Normalization

Container image references are normalized during compilation:
- Single name (`nginx`) becomes `docker.io/library/nginx:latest`.
- Two components (`user/app`) becomes `docker.io/user/app:latest`.
- Full references are preserved; `:latest` is appended if no tag is present.

### Response Persistence

Responses are saved per version at `responses/<repo>/<pkg>/<version>.json`. A `last` copy is saved at `responses/last/<repo>/<pkg>.json` for reuse during upgrades and reinstallation from uninstalled volumes. Last responses are cleared after a successful install.

Two API endpoints manage last responses:

- `POST /packages/last-responses` (auth required) -- retrieve cached last responses for a package (by repo and name).
- `POST /packages/clear-last-responses` (admin required) -- delete the cached last responses file.

### Installation Questions UI

When a user installs a package, the questions dialog loads existing responses (from a current install) and, if none exist, cached last responses (from a previous uninstall). Current responses take precedence over last responses.

**Cached responses** are displayed as read-only styled containers with a muted background, showing the saved value (passwords display as `********`). A hidden form input preserves the value for submission. Each cached field has a clear button (X icon) with a tooltip ("Clear to enter a new value") that, when clicked, replaces the read-only display with an editable input. The clear button uses a ghost style that turns red on hover.

**Defaults** are shown in two ways when no cached value is present: as placeholder text in the input (e.g., "Default: 8080") and as helper text below the input in muted text with the value in monospace. Type-specific placeholders are shown when no default is defined: "Auto-assigned if empty" for ports, "Auto-generated if empty" for hostnames, and "e.g. 30s, 5m, 2h, 1d" for durations.

**Validation errors** from the server are displayed per-field as red text below the input, and the input receives a red border.

### Package Info Dialog

The package info dialog displays notes as a labeled list. Notes are rendered based on their type: URL notes are clickable hyperlinks that open in a new tab (`target="_blank"`), email notes are `mailto:` links that open the user's email client, and phone notes are `tel:` links. Untyped notes are rendered as plain code blocks without links.

### Package Manifest API

`POST /packages/manifest` (auth required) returns the raw YAML definition of a package. Accepts repo, name, and version. Returns the file content with `Content-Type: text/x-yaml; charset=utf-8`. Returns 404 if the package file does not exist.

### Package Actions Dropdown

In the packages list UI, each package row has a `...` dropdown menu (both flat and grouped-by-repo views). The dropdown contains:

- **Info** (installed packages only) -- opens the package info dialog showing questions, responses, and compiled notes.
- **Manifest** -- opens a dialog displaying the raw YAML package definition with a copy button.
- **Version/Repository** -- displayed as a disabled item showing the version and repo name.
- **Uninstall** (installed packages only) -- triggers the uninstall confirmation dialog.

### Featured Packages

Each repository can include a `featured.json` file containing a JSON array of package names. These are loaded by `LoadFeatured` and returned alongside the package list in `RepoPackageGroup`. The flat package list API sets a `featured` boolean on each entry. The grouped package list API preserves the `Featured` array on each group even when search filtering reduces the package list.

- `GET /packages` (auth required) -- list packages with search, sorting, pagination, and optional `featured_only` and `installed_only` filters.
- `GET /packages/featured` (auth required) -- list featured packages across all repositories.
- `GET /packages/by-repo` (auth required) -- list packages grouped by repository. Accepts `search` and `featured_only` query parameters.

#### Featured Packages Filter

The flat package list API (`GET /packages`) and the grouped package list API (`GET /packages/by-repo`) accept a `featured_only` query parameter. When set to `"true"`, only packages marked as featured are returned. The filter intersects with `installed_only` -- both can be active simultaneously. In the UI, a "Featured only" checkbox toggles the filter. The default state for the featured filter is `true` (showing only featured packages on first visit). Filter preferences (`pkg_group_by_repo`, `pkg_installed_only`, `pkg_featured_only`) are persisted in `localStorage`.

### Installed Packages Filter

The flat package list API (`GET /packages`) accepts an `installed_only` query parameter. When set to `"true"`, only installed packages are returned. Filtering is applied server-side before search, sorting, and pagination, ensuring correct page counts and offsets. In the UI, an "Installed only" checkbox toggles the filter and resets pagination to the first page.

### Package Installation and Uninstallation

#### Install API

`POST /packages/install` (admin required) installs a package. Accepts repo, name, version, responses, and optional flags:

- `reuse_volumes` -- reuse volumes from a previous uninstalled version.
- `import_from_version` -- import volumes from a specific prior version.
- `skip_response_reuse` -- do not auto-populate answers from previous installs.

Installation creates a hard link from the repository package file to the installed directory, persists responses, creates volumes with quotas and optional UID/GID, seeds volumes from archives and git (container runtime only), applies file templates, generates systemd unit files, writes network state files, installs and starts systemd units, and clears last responses on success. Last responses are saved before install for recovery on uninstall. For VM packages, the VM disk image is downloaded and converted to raw format (if a remote URL) before unit generation; volume seeding (archives, git clones) is skipped.

#### Uninstall API

`POST /packages/uninstall` (admin required) uninstalls a package. Accepts repo, name, version, and optional flags:

- `purge_volumes` -- delete all associated volumes immediately.

When not purging, volumes are moved from the `installed/` prefix to the `uninstalled/` prefix. The network state file is removed and systemd units are stopped, disabled, and uninstalled.

**Dependency cascade.** Uninstalling a parent package recursively uninstalls every dependency it owns. The cascade reads the persisted dependency records (`LoadDependencies`) for the parent and walks each child depth-first, repeating the lookup at every level so nested sub-dependencies (`parent--dep--child--dep--grandchild`) are removed too. For each dependency the cascade unregisters its DNS records, uninstalls its systemd units (service + NC + sockets), removes its network state file, calls `inst.Uninstall` to drop the install record, and either purges its volumes (when `purge_volumes` is set) or moves them to the `uninstalled/` prefix. The cascade is implemented in `uninstallDependencies` (`src/svc/systemcontroller/controller_install_dependencies.go`) and runs after the parent's own uninstall completes. There is no reference counting: each dependency belongs to exactly one parent (its install record lives at `installed/<repo>/<parent--dep--key>/`), so a shared dependency installed under two parents has two independent records, and uninstalling one parent only removes its own copy.

#### Installed Package Info

`POST /packages/installed/info` (auth required) returns questions, responses, compiled notes, and note types for an installed package.

#### Package Versions

`POST /packages/versions` (auth required) lists available versions of a package by name.

#### Package Questions

Two endpoints retrieve package questions:

- `POST /packages/questions` (admin required) -- get questions by package name (latest version).
- `POST /packages/questions/identity` (admin required) -- get questions by repo, name, and version.

### Timezone Handling

The UI maintains a static copy of common IANA timezone names with a `getTimezoneOffsetMinutes()` utility that computes UTC offsets client-side using the browser's `Intl` API. The server exposes the local system's UTC offset in minutes via the status ping response.

### Install Preview

- `POST /packages/install-preview` (auth required) -- preview what would be created if a package were installed. Accepts repo, name, and version. Returns repo, name, version, description, image, volumes, ports, upgrade info, runtime type, and whether the package has questions. For VM packages, the preview also includes VM configuration (image URL, human-readable memory, and CPU count).

### Package Children

- `POST /packages/children` (auth required) -- list child package names for a given repo and package name.

### Uninstalled Volumes Listing

- `POST /packages/uninstalled-volumes` (auth required) -- check whether a package has leftover volumes from a previous uninstall. Returns whether uninstalled volumes exist, the list of uninstalled versions, and the list of installed versions.

### Installed Package Management

- `GET /packages/installed` (auth required) -- list all installed packages with search, sorting, and pagination.
- `POST /packages/responses` (auth required) -- get saved responses for an installed package by repo, name, and version.
- `POST /packages/purge-volumes` (admin required) -- permanently delete volumes for an installed package.

### Package Enable/Disable

- `POST /packages/disable` (admin required) -- disable a package. Sets the disabled flag and stops all associated systemd services.
- `POST /packages/enable` (admin required) -- re-enable a disabled package. Clears the disabled flag and starts all associated systemd services.

The `Installer` interface supports `SetDisabled`, `IsDisabled`, and `IsPackageChanged` in addition to the core `Install`, `Uninstall`, `ListInstalled`, and `GetResponses` methods.

### Uninstalled Volume Management

- `POST /packages/purge-uninstalled-volumes` (admin required) -- permanently delete all uninstalled volumes for a package.

## Storage

Storage uses btrfs subvolumes with quota enforcement.

### Filesystem Operations

The `Storage` interface provides:

- **CreateFilesystem** -- create a new btrfs subvolume with optional quota.
- **ModifyFilesystem** -- change the name and/or quota of a volume.
- **RemoveFilesystem** -- delete a volume.
- **ListFilesystems** -- list volumes with filtering by prefix and state (`user`, `installed`, `uninstalled`), sorting, pagination, and search. Returns an empty list (not an error) when the btrfs mount is not found.
- **RenameFilesystem** -- rename a volume.
- **SnapshotFilesystem** -- create a btrfs snapshot.
- **DiskUsage** -- report disk usage statistics.

Quotas are enforced at the btrfs qgroup level. A quota of 0 means unlimited.

### Storage API

- `POST /storage/create` (auth required) -- create a new user filesystem with name and optional quota.
- `POST /storage` (auth required) -- list filesystems with filtering by prefix and state, sorting, pagination, and search.
- `POST /storage/modify` (auth required) -- modify a volume's name and/or quota. Renaming is only allowed for user filesystems; package volumes cannot be renamed.
- `POST /storage/remove` (auth required) -- delete a user filesystem.
- `POST /storage/package-volumes` (auth required) -- list package volumes grouped by package, with optional inclusion of uninstalled volumes.
- `POST /storage/remove-package-volume` (admin required) -- delete a specific package volume by internal name.
- `POST /storage/upload-archive` (admin required) -- upload and unpack an archive into a volume.
- `POST /storage/download-archive` (admin required) -- download a volume as a compressed archive.

### Volume Namespacing

- **User volumes** -- `user/<name>` on disk. The `user/` prefix is prepended transparently by the create, remove, modify, and list handlers, and stripped in API responses so the API consumer sees only the bare name. The `user` root subvolume is created on boot by reconcile.
- **Installed package volumes** -- `installed/<repo>/<name>/<version>/<volname>`.
- **Uninstalled package volumes** -- `uninstalled/<repo>/<name>/<version>/<volname>`.
- **Archive storage** -- `archives/` prefix (system-managed).
- **VM images** -- `vm-images/` subvolume (system-managed). Stores cached raw VM disk images.

All prefix root names (`installed`, `uninstalled`, `archives`, `pages`, `vm-images`, `user`) are reserved and cannot be directly created, modified, or deleted by users. Archive upload and download resolve subvolume names that lack an internal prefix by prepending `user/`.

### Archive Format Detection

Archive compression format is detected by inspecting magic bytes at the start of the upload stream. The first 6 bytes are peeked via a buffered reader and matched against known signatures:

- **gzip** -- `0x1f 0x8b`
- **bzip2** -- `0x42 0x5a 0x68` (`BZh`)
- **xz** -- `0xfd 0x37 0x7a 0x58 0x5a 0x00` (`\xfd7zXZ\x00`)

Unrecognized signatures are rejected immediately. The filename extension is also validated independently to confirm the format.

### Archive Stream Validation

After format detection, the decompressed stream is validated as a tar archive using `io.TeeReader`. One side of the tee feeds Go's `archive/tar` reader to validate tar headers; the other side feeds the real `tar -xf` unpack process. If validation detects an invalid tar stream, the unpack is interrupted. Decompression uses parallel implementations where available: `pigz` for gzip, `lbzip2` for bzip2, and `xz` for xz.

### Archive Upload

`POST /storage/upload-archive` (admin required) accepts a multipart form:

- `subvolume` (required) -- target subvolume path.
- `archive` (required) -- archive file. Supported formats: `.tar`, `.tar.gz`/`.tgz`, `.tar.bz2`/`.tbz2`, `.tar.xz`/`.txz`.
- `subpath` (optional) -- relative path within the volume for unpacking; created on demand.
- `stop_service` (optional) -- systemd unit name to stop before unpacking and restart after completion.

Archives are streamed directly without temporary files. Path traversal is validated after unpacking (symlink resolution). Maximum upload size defaults to 1 GB (`max_archive_size` setting). Unpack timeout defaults to 600 seconds (`archive_unpack_timeout` setting).

### Archive Download

`POST /storage/download-archive` (admin required) accepts a JSON body:

- `subvolume` (required) -- source subvolume path.
- `paths` (optional) -- array of specific paths within the subvolume to include.
- `stop_service` (optional) -- systemd unit name to stop during archiving and restart after.
- `format` (optional) -- compression format: `tar.gz` (default), `tar.bz2`, or `tar.xz`.
- `filename` (optional) -- custom base name for the downloaded file. The server sanitizes the value (strips path separators and control characters), removes any existing archive extension to prevent doubling, and appends the appropriate extension for the chosen format. Defaults to `download` when not provided or when sanitization produces an empty string.

Returns a streamed archive in the requested format. Compression uses `pigz`, `lbzip2`, or `xz` respectively. Content-Type and Content-Disposition filename headers are set to match the chosen format and custom filename. When `paths` is provided, only matching paths are included.

### Auto-Archive from Container Images

Package definitions can include an `archives` section referencing container images. During install and reconcile, empty volumes are populated by pulling the image, creating a temporary container, and copying the specified directory into the volume.

### Git Volume Seed

Volumes can specify a `git` field with a repository URL. During install and reconcile, empty volumes are seeded by cloning the repository (5-minute timeout). The URL can reference template variables, allowing users to override the repository via a question response. Existing data is never overwritten. Clone failures are logged and skipped (non-fatal).

### Git Source Rebuild

`POST /packages/rebuild-git` (admin required) updates git-seeded volumes for an installed package. It pulls latest changes for each git volume via go-git, then restarts the dependent service. Requires the package repo, name, and version. Template variables are re-evaluated against saved responses before rebuilding.

### VM Image Management

VM packages require disk images in raw format. Remote images are downloaded and converted using `qemu-img convert -O raw`; the converted `.raw` file is cached in the `vm-images` subvolume. Subsequent installs reuse the cached image. Local image references are resolved directly from the `vm-images` subvolume.

- `GET /vm-images` (auth required) -- list cached VM disk images. Returns name and file size for each image.
- `POST /vm-images/upload` (admin required) -- download a VM image from a URL and convert it to raw format. Accepts a URL and optional name. The name defaults to the URL's filename with a `.raw` extension. Downloads have a 30-minute timeout. The converted image is stored in the `vm-images` subvolume.
- `POST /vm-images/delete` (admin required) -- remove a cached VM image by name.

### Display Name Stripping

API responses for installed and uninstalled package volumes strip the leading repository segment from the path (e.g., `default/nginx/2.0/data` becomes `nginx/2.0/data`). The full on-disk path is preserved in an `internal_name` field for operations that need it (e.g., deriving the systemd service name for stop/start during archive operations).

### Storage UI

The storage management screen has two sections:

**User Filesystems** -- a paginated, sortable, searchable data table. Each row has Modify (name and quota) and Delete buttons. The create dialog pre-populates the quota field from the system `default_quota` setting.

**Package Volumes** -- a hierarchical tree organized by package. Each package is a collapsible tree heading showing: total volume count, version count, aggregate quota, and installation state badges. When a package has multiple versions, version sub-headings are shown with per-version quota and state. Uninstalled volumes are included when a "Show uninstalled volumes" toggle is active.

Each leaf volume row displays quota and state, and provides three actions:

- **Download** (icon button) -- opens a dialog with an optional filename field (base name for the downloaded file; the archive extension is appended automatically), a compression format selector (gzip, bzip2, xz), optional comma-separated path filter, and a checkbox to stop the dependent service during download. Uses the File System Access API for streaming saves with a fallback to blob download.
- **Upload** (icon button) -- opens a dialog for selecting an archive file (`.tar`, `.tar.gz`, `.tgz`, `.tar.bz2`, `.tbz2`, `.tar.xz`, `.txz`) with optional subpath for extraction and a checkbox to stop the dependent service during upload.
- **Modify** (button) -- opens a dialog showing the volume name, state, and associated service name, with a field to change the quota. The name field is not editable for package volumes.

## Pages

Pages is a static site hosting feature supporting three content source types: archive uploads, container images, and git repositories. Users assign a domain or subdomain, and the system serves the content via a Caddy container. Updates are triggered manually via rebuild or re-upload.

### Data Model

Each page site has: a unique name (primary key), source type (`archive`, `container_image`, or `git`; default: `archive`), repository URL (required for git), branch (defaults to `main`), container image reference (required for container_image), image directory (required for container_image), domain (defaults to the page name), status (`pending`, `active`, or `error`), and creation/update timestamps. Pages are stored in a SQLite table.

Pages content is stored in btrfs subvolumes under a `pages/` prefix. Each page gets a subvolume at `pages/{name}` and a symlink at `pages-webroot/{name}` pointing to `/data/pages/{name}`. The `pages` prefix is reserved and cannot be renamed or deleted via the general storage API.

### Pages API

All mutation endpoints require admin authentication; the list endpoint requires regular authentication.

- `POST /pages/create` (admin required) -- create a new page. Accepts name, source type, repo URL, branch, domain, container image, and image directory. Source type defaults to `archive`. Validation varies by source type: git requires repo URL; container image requires both image and image directory. Creates a btrfs subvolume and webroot symlink. Git and container image pages are provisioned asynchronously (clone or image extraction); status transitions from `pending` to `active` on success or `error` on failure. Archive pages remain in `pending` status until content is uploaded via `/pages/upload`. Domain defaults to the page name if not provided.
- `POST /pages/upload` (admin required) -- upload content for an archive-type page. Accepts multipart form with `name` and `archive` file. Only valid for pages with source type `archive`; returns 400 for other source types. Uses the same magic-byte format detection, extension validation, and stream validation as storage archive uploads. Unpacks directly into the page's btrfs subvolume. Sets status to `active` on success or `error` on failure.
- `POST /pages/update` (admin required) -- partial update of a page's repo URL, branch, domain, source type, container image, or image directory. Only provided fields are changed.
- `POST /pages/remove` (admin required) -- delete a page from the database, remove the webroot symlink, and delete the btrfs subvolume.
- `POST /pages/rebuild` (admin required) -- behavior varies by source type: git pages pull latest changes (or fresh clone if `.git` is missing); container image pages re-extract from the image via podman; archive pages return 400 (re-upload via `/pages/upload` instead).
- `GET /pages` (auth required) -- list all pages with sorting, search, and pagination. Sortable by name, repo URL, branch, domain, source type, status, and timestamps.

### Pages UI

The pages management screen displays a paginated, sortable, searchable data table with columns for name, domain, source type, repository URL, branch, and status. Source type is shown as a badge. Status is shown as a color-coded badge (default for active, red for error, secondary with a spinning loader icon and "Provisioning..." text for pending).

The create dialog has a source type dropdown at the top (Archive Upload / Container Image / Git Repository, default: Archive Upload). Fields change dynamically based on the selected source type: git shows repository URL and branch; container image shows image reference and image directory; archive shows an optional file upload input. For git and container image pages, submitting the form triggers provisioning: all inputs are disabled, the submit button shows a spinner with "Provisioning..." text, and the dialog cannot be closed. The UI polls page status every 2 seconds for up to 60 seconds. For archive pages with a file selected, the upload happens synchronously after creation.

Actions per row vary by source type: archive pages show an Upload button; git and container image pages show a Rebuild button (with confirmation). All pages have Edit and Delete actions. The edit dialog shows fields appropriate to the page's source type.

## Services

### Service Unit Filtering

The systemd unit query is scoped to the `town-os-package--*` pattern at the dbus level, fetching only Town OS package units rather than all units on the system. System service units (`town-os-system--*`) are identified separately via `IsSystemServiceUnit()`. The result set further excludes network controllers (`-network.service`), uPnP helpers (`-upnp.service`), and port forwards (`-fwd-`). Network controller units are retained internally for failure detection but excluded from the user-facing list.

### Service Description Enrichment

Package descriptions are loaded in batch using one `LoadPackages` call per repository, rather than individual per-package YAML reads. Descriptions are matched to service units by constructing the expected unit name from each package identity.

### Service Unit Generation

Systemd service units are generated differently based on the package runtime type.

**Container packages** generate podman-based units with `podman run` for start and `podman stop` for stop, including port mappings (`-p`), environment variables (`-e`), and volume mounts (`-v`).

**VM packages** generate QEMU-based units using `qemu-system-x86_64` with:

- `-m {MB}` -- memory in megabytes (converted from the compiled byte value).
- `-smp {cpus}` -- virtual CPU count.
- `-nographic` -- headless operation (no display output).
- `-enable-kvm` -- KVM hardware acceleration.
- `-drive file={image},format=raw,if=virtio` -- raw disk image as a virtio block device.
- `-netdev user,id=net0` with `hostfwd=tcp::{external}-:{internal}` for each port mapping -- QEMU user-mode networking with host-to-guest port forwarding.
- `-device virtio-net-pci,netdev=net0` -- paravirtualized network device.

VM units also manage firewall ports via `firewall-cmd` in pre-start and post-stop hooks, and coordinate with socket units to avoid port conflicts.

### Service Unit API

- `GET /systemd/units` (localhost or auth) -- list all package service units. Returns unit status enriched with package identifier, package description, and network controller failure flag.
- `POST /systemd/status` (admin required) -- change a service unit's status. Accepts unit name and action (start, stop, restart, enable, disable).

### Service Management UI

The services screen shows a paginated data table of installed package systemd units. Each row displays the package identifier, description, active state, sub-state, and an actions dropdown.

#### Service Actions

The actions dropdown for each service provides:

- **Start** -- start the service (with confirmation).
- **Stop** -- stop the service (with confirmation; disabled for the system controller itself).
- **Restart** -- restart the service (with confirmation).
- **Service Logs** -- open the journal viewer for this service's unit.
- **Network Logs** -- open the journal viewer for this service's network controller unit (unit name with `-network.service` suffix).

### Advanced Logs

An "Advanced Logs" button below the services table opens a modal with:

- **Controller Logs** -- view logs for `town-os-systemcontroller.service`.
- **System Logs** -- view system-wide logs (all units).
- **Journal Errors** -- view system logs filtered to priority level 3 (errors and above, equivalent to `journalctl -p 3`).
- **Custom service name** -- text input to view logs for any arbitrary systemd unit.

### Journal Viewer

The journal viewer dialog provides:

- Dynamic title showing unit name, "System Logs", or "Journal Errors" depending on context.
- Status badge showing the unit's active state and sub-state (when viewing a specific unit).
- Real-time search with debounced filtering (300 ms).
- Time range filtering by date and hour.
- Follow mode toggle for continuous log tailing with auto-scroll (automatically disabled when search or time filters are active).
- Initial scroll-to-bottom: when the viewer opens, the log container is scrolled to the end once entries have finished loading. The scroll-to-bottom effect is gated on `journalEntries.length > 0` so it cannot be consumed on the empty first render before entries arrive; a trailing `requestAnimationFrame` re-pins scrollTop after layout settles in case the expanded tree grows between commit and paint.
- Tree view toggle for grouping entries by minute. Tree view is the default and each minute group is **expanded by default**. The expand-state map stores only explicit collapses: an undefined entry is treated as expanded, so first-time toggles collapse rather than expand.
- Copy-to-clipboard for all displayed log entries.
- ANSI color code rendering in log output.
- Structured field highlighting (`name=value` pairs).

### Log API

Two endpoints serve log data:

- `GET /systemd/logs` (localhost or auth) -- streams historical journal entries via Server-Sent Events. The `unit` query parameter selects the service; empty or `__system__` returns system-wide logs.
- `GET /systemd/logs/tail` (localhost or auth) -- returns a JSON page of journal entries. Supports parameters: `unit`, `lines` (default 100), `before`/`after` (cursor pagination), `grep` (case-insensitive search), `since`/`until` (Unix timestamps), and `priority` (syslog severity filter, 0 = no filter).

## Account Management

### Account Model

Each account has: username (primary key), password hash (never exposed in JSON), email, phone, real name, admin flag, disabled flag, and creation/update timestamps. Accounts are stored in a SQLite table.

### Validation Rules

- **Password** -- minimum 8 characters.
- **Email** -- standard email format (`user@domain.tld`).
- **Phone** -- digits with optional formatting (`+`, spaces, dashes, parentheses).
- **Contact info** -- email, phone, and real name are all required (non-empty).

### Account API

- `POST /account/create` -- create a new account. In bootstrap mode (no enabled admin accounts exist), unauthenticated access is allowed; otherwise admin authentication is required. Duplicate username errors return a generic failure message to prevent user enumeration.
- `POST /account` -- get account by username (auth required).
- `GET /account` -- list all accounts with pagination and search (auth required).
- `POST /account/update` -- update account fields (auth required). Admin status cannot be changed after account creation.
- `POST /account/disable` -- disable an account, preventing authentication (admin required).
- `POST /account/enable` -- re-enable a disabled account (admin required).

### Account Management UI

The users management screen (`/dashboard/users`) displays a paginated, sortable, searchable data table of accounts. Each row shows username, email, phone, real name, admin/user role badge, and enabled/disabled status. Actions per row include an Edit button (opens a dialog for updating password, email, phone, and real name) and an Enable/Disable toggle with confirmation. A link navigates to a dedicated create user page (`/dashboard/users/create`) with a registration form.

### Session Management

Sessions use JWT tokens (HS256) with claims for session ID (UUID), username, and issued timestamp. The signing key is ephemeral: 32 random bytes generated via `crypto/rand` on every service start, never persisted to disk. When `InitSessionManager` runs at startup, all existing sessions are cleared (`DELETE FROM sessions`) since prior tokens are invalid with the new key. The `TOWN_OS_SIGNING_KEY` environment variable can override the generated key. Sessions expire after 7 days from last use. A background cleanup task periodically removes expired sessions.

The `SessionManager` interface provides: `Create`, `Validate`, `Revoke`, `RevokeAllForUser`, `Cleanup`, `List`, `GetUsername`, `HasActiveAdminSessions`, and `StartCleanup`.

Session API endpoints:

- `POST /account/authenticate` -- username/password login (public). Returns a JWT token and account object. Authentication failures (wrong password, nonexistent user, disabled account) all return the same generic "invalid credentials" error to prevent user enumeration.
- `GET /account/sessions` -- list the authenticated user's sessions (auth required).
- `GET /account/me` -- get the authenticated user's username (auth required).
- `POST /account/session/revoke` -- revoke a specific session by ID (auth required).

### Audit Logging

All administrative actions are recorded in an audit log. Each entry has: auto-increment ID, account (username), action description, request path, sanitized detail (passwords redacted), success flag, error message, and creation timestamp.

Tracked actions include: create/modify/remove filesystem, add/remove/move/refresh repository, install/uninstall package, purge volumes, disable/enable package, set unit status, create/update/disable account, authenticate, revoke session, update setting, dismiss upgrades, upload/download archive, create/update/remove/rebuild page, upload/delete VM image.

Read-only endpoints are explicitly excluded from audit logging. Excluded paths include the root path (`/`), all GET list/query endpoints, info endpoints (`/packages/installed/info`), response retrieval (`/packages/last-responses`, `/packages/responses`), install preview (`/packages/install-preview`), version/question lookups, timezone listing, the pages list endpoint, status ping, system services listing (`/system-services`), audit log queries, settings reads, and log streaming endpoints.

- `POST /audit/log` (localhost or auth) -- query the audit log with cursor-based or offset pagination, account filtering, sorting, and search.

### Settings Management

Key-value settings are stored in SQLite. Default settings include `default_quota` (50 GB), `max_archive_size` (1 GB), `archive_unpack_timeout` (600 seconds), `locale` (en-US), `proton_image` (quay.io/town/proton:latest), and `dns_tld` (home).

- `GET /settings` -- get all settings (admin required).
- `POST /settings/get` -- get a specific setting by key (admin required).
- `POST /settings/set` -- set a setting value (admin required, audit logged). Byte-value settings (`default_quota`, `max_archive_size`) accept human-readable strings (e.g., "500GB", "10MB") which are parsed and stored as numeric byte counts.

### Settings UI

The system settings screen provides admin-configurable controls for all system-wide settings. Each setting is displayed in a bordered section with a heading, a description showing the current value in human-readable format, and a form with a numeric input, a unit selector, and a save button.

- **Default Volume Quota** -- configurable in GB, MB, or bytes. Displays "0 (no quota)" when set to zero.
- **Max Archive Size** -- configurable in GB, MB, or bytes. Controls the maximum file size allowed for archive uploads.
- **Archive Unpack Timeout** -- configurable in seconds, minutes, or hours. Controls the maximum time allowed for unpacking an uploaded archive.
- **Language** -- a dropdown showing common languages with native-script names. An expandable section reveals extended locales. Unpopulated locales are shown with an asterisk and disabled.
- **Proton Image** -- an editable text input for the Proton runner container image reference (e.g., `quay.io/town/proton:latest`).

Current values are decomposed into the most appropriate unit for display (e.g., 1073741824 bytes displays as "1 GB", 120 seconds displays as "2 minutes"). Input validation rejects negative and non-numeric values.

## Package Upgrades

### Upgrade Detection

The upgrade system compares installed package versions against the latest available versions in configured repositories. A package is flagged for upgrade when a newer version exists or when local modifications are detected.

- `GET /packages/upgrades` (auth required) -- list available upgrades. Each entry includes repo, name, installed version, latest version, and a changed flag.
- `POST /packages/upgrades/dismiss` (admin required) -- mark current upgrades as dismissed. Computes a SHA256 hash of the current upgrade set and stores it as the `dismissed_upgrades_hash` setting.

The status ping response includes `upgrades_available` (count) and `upgrades_dismissed` (boolean, true if the hash matches).

## Networking

### UPnP Port Mapping

The `upnp.Manager` interface provides `AddPortMapping` and `RemovePortMapping` for managing TCP port forwarding on the local network gateway via UPnP/IGD. The implementation discovers the Internet Gateway Device via SSDP and uses WANIPConnection2 SOAP methods. Local IP is detected by connecting to an external address (8.8.8.8:80 UDP).

### Network Controller

The network controller manages per-package port forwarding and UPnP mappings. Each package with networking requirements has a JSON state file specifying ports with external/internal mappings, UPnP flag, and forward flag.

- **Socat forwarding** (when `forward=true`) -- runs `socat TCP-LISTEN:{externalPort},fork,reuseaddr TCP:127.0.0.1:{internalPort}` to forward traffic.
- **UPnP mapping** (when `upnp=true`) -- maps ports on the gateway. When `forward=true`, maps external-to-external (socat listens); when `forward=false`, maps external-to-internal (podman bridge handles it).
- **Reconciliation** -- monitors state files via fsnotify, stopping/starting forwarders and mappings as needed.
- **Renewal** -- UPnP mappings are renewed every 10 minutes with a 1800-second TTL.
- **Shutdown** -- removes all UPnP mappings and kills all socat processes on context cancellation.

### Dependency Shared Networking

Package dependencies share the parent package's podman network. This allows containers in the same dependency tree to communicate directly by container name (via podman's built-in DNS on the shared network) rather than through host port forwarding.

- **Idempotent network creation** -- every service unit includes `ExecStartPre=-/usr/bin/podman network create {network}` regardless of whether a network controller (NC) exists. This is a boot-ordering safety net: if the NC hasn't created the network yet (e.g. image not built, systemd race), the service can still start. The NC also creates the network — whoever gets there first wins, the other is a no-op.
- **Network ownership** -- the parent package owns the podman network (`town-os-net--{repo}-{name}-{version}`). The NC creates the network in `ExecStartPre` and removes it (`podman network rm -f`) in `ExecStopPost`.
- **Dependencies join the parent network** -- dependency service units use `--net {parent-network}` instead of creating their own. They create the network idempotently in `ExecStartPre` (in case they start before the parent) but never remove it.
- **Standalone packages without ports** follow the original pattern: `podman network rm -f` then `podman network create` in `ExecStartPre`, and `podman network rm -f` in `ExecStopPost`. Only standalone packages without an NC or parent NC perform `rm -f` before `create`.
- **Parents with dependencies** do NOT `rm -f` before `create` in `ExecStartPre` because dependencies may already be running on the network (they start first via `Before=` ordering).

### Dependency Systemd Ordering

Systemd units for dependencies have ordering directives that ensure correct start/stop sequencing relative to the parent:

- **Dependency units**: `PartOf={parent-service}` (stopping the parent cascades to deps) and `Before={parent-service}` (dep starts before parent, stops after parent).
- **Parent units**: `Wants={dep1} {dep2} ...` and `After={dep1} {dep2} ...` (parent wants deps and waits for them before starting).
- **Network controller**: the existing `Wants=` for the NC is merged with dependency `Wants=` targets.

This is configured via `PackageUnitConfig` fields: `ParentNetwork`, `ParentUnitName` (for dependencies), and `DependencyUnitNames` (for parents). Reconcile computes these from dependency records and `ParentName()`.

### Dependency Environment Variables

Parent packages receive environment variables for reaching their dependencies on the shared network:

- `TOWNOS_DEP_{KEY}_HOST` -- the dependency's podman container name (resolvable via podman DNS on the shared network).
- `TOWNOS_DEP_{KEY}_PORT_{containerPort}` -- the container-side port number (since parent and dep are on the same network, no host port mapping is needed).

### Dependency Template Variables

In addition to the runtime environment variables above, dependency host and port values are also available as `@variable@` template markers during package compilation. This allows parent packages to reference dependencies in their `environment` field values at compile time, and also allows **sibling dependencies** to reference each other in the `dependencies.<key>.responses` block.

- `@dep_KEY_host@` -- resolves to the dependency's podman container name (resolvable via podman DNS on the shared network).
- `@dep_KEY_port_N@` -- resolves to port N for the dependency.

Template keys are derived from the `TOWNOS_DEP_*` runtime environment variable names by stripping the `TOWNOS_` prefix and lowercasing the remainder. For example, `TOWNOS_DEP_DB_HOST` becomes template key `dep_db_host`, and `TOWNOS_DEP_DB_PORT_5432` becomes `dep_db_port_5432`.

On the **parent** side, these variables are resolved after dependency installation, when the dependency's container name and ports are known. They are applied to parent environment values during unit generation. Reconcile also rebuilds dependency environment variables so that systemd units stay correct across restarts and version changes.

On the **dependency** side (responses declared under `dependencies.<key>.responses` that reference another sibling key), resolution happens during `installDependencies` via a topological sort:

- `orderDependencies` in `src/svc/systemcontroller/controller_install_dependencies.go` parses each sibling dep's `Responses` for `@dep_KEY_host@` / `@dep_KEY_port_N@` markers and builds a DAG. Sibling deps with no references run first; referencing siblings run after the sibling(s) they name. Tie-breaking among equally-ready deps is alphabetical for determinism (Go map iteration is random, so a sort is mandatory for reproducibility).
- A cycle among sibling deps is a hard error and aborts the install before any dep is provisioned.
- For each dep in that order, `applyDepTemplates` is called on the dep's `Responses` **before** `depIP.CompileWithContext` runs, substituting `@dep_OTHER_*@` markers with the container name / port values accumulated from already-installed siblings. Without this pre-compile substitution, a typed question in the dep's YAML (e.g. `type: port` or any type whose `Output` runs `strconv.ParseUint`) would reject the literal placeholder with `ErrInvalidResponseType`, aborting mid-install and leaving a half-installed parent on disk.
- Self-references (`dep X references @dep_X_host@`) are ignored, not treated as cycles. References to names that are not declared sibling keys are treated as external template vars and ignored for ordering.
- The install handler streams errors via SSE and returns `nil` from the HTTP handler, so the audit log always records `success=true` regardless of whether the install actually completed. This means partial-install failures (half-installed dep trees, orphaned btrfs volumes under `installed/<repo>/<parent>/<version>/`) are only visible in the SSE stream and the systemd unit list — not in `/audit/log`.

Example: a package with a dependency key `db` (a Postgres container exposing port 5432) can use `@dep_db_host@` and `@dep_db_port_5432@` in its environment section instead of hardcoding `127.0.0.1`:

```yaml
environment:
  DB_HOST: "@dep_db_host@"
  DB_PORT: "@dep_db_port_5432@"
```

Example with sibling-to-sibling refs (the jitsi shape): `jitsi` depends on `prosody`, `jicofo`, and `jvb`. `jicofo` and `jvb` each need prosody's container name and internal XMPP port, so the parent YAML threads them through the `responses` block of each referencing dep. `orderDependencies` installs `prosody` first, then `jicofo` and `jvb` (alphabetical among the two), each with the placeholder substituted to prosody's concrete container name and port 5222:

```yaml
dependencies:
  prosody:
    package: prosody
  jicofo:
    package: jicofo
    responses:
      xmpphost: "@dep_prosody_host@"
      xmppport: "@dep_prosody_port_5222@"
  jvb:
    package: jvb
    responses:
      xmpphost: "@dep_prosody_host@"
      xmppport: "@dep_prosody_port_5222@"
```

## DNS Management (Rolodex)

Town OS includes an integrated local DNS resolver powered by a `rolodex-dns` container. The rolodex server manages zone files and records for installed packages, providing local name resolution via a gRPC Unix socket interface.

### Rolodex Manager

Rolodex itself is a boot service installed and supervised by systemd — the systemcontroller does not install, start, stop, or restart it at the container level. The `rolodex.Manager` instead:

- **`WriteConfig`** -- writes `rolodex.yml` into `DataDir`. Idempotent: skips writing when the file exists, is newer than the systemcontroller binary, and already matches the expected content. Returns a boolean indicating whether the file was written (so the caller can decide whether to restart the systemd unit).
- **`WaitForDNSReady`** -- polls `DNSLoopback:{port}` over TCP until it accepts a connection or the 30-second deadline passes. Called at startup before any operation that depends on DNS (e.g., image pulls).
- **`SystemServices`** -- returns metadata for the rolodex system service (key, display name, image, port, unit name) so it surfaces alongside other system services in status responses and the UI.
- **`Status`** -- queries the systemd unit state to report whether rolodex is running.

The rolodex container runs with `--net host` and binds DNS to `DNSLoopback` (`127.0.0.2`) on the configured port (default `53`, overridable via `DNSPort` for tests). The image tag is derived from the system controller's release tag (`quay.io/town/rolodex:<tag>`), overridable via the `ROLODEX_IMAGE` environment variable.

### DNS API

- `GET /dns/status` (auth required) -- returns DNS status including enabled flag, running state, TLD, and record count.
- `GET /dns/records` (auth required) -- list all DNS records.
- `POST /dns/records/add` (admin required) -- add a DNS record. Accepts name, record type, value, and TTL.
- `POST /dns/records/remove` (admin required) -- remove a DNS record by name and type.
- `GET /dns/tld` (auth required) -- get the current top-level domain.
- `POST /dns/tld` (admin required) -- set the TLD. Changes the existing TLD and re-registers all installed packages.
- `POST /dns/setup` (admin required) -- initialize DNS and register all installed packages.

DNS read-only endpoints (`/dns/status`, `/dns/records`, `GET /dns/tld`) are excluded from audit logging.

### DNS Management UI

The DNS management screen displays DNS status (enabled, running, TLD, record count), a DNS records table, and provides dialogs for adding records (types: A, AAAA, CNAME, MX, TXT, SRV, PTR), removing records, changing the TLD, and initial DNS setup.

## Status Endpoint

`GET /status/ping` (public) returns system status including: filesystem counts (user, installed, uninstalled), repository and package counts, installed package count, account and admin counts, service unit counts (total, active, failed), system service unit counts (total, active, failed), recent audit errors (last 5 minutes), setup status (`needs_setup` is true only when no enabled admin account exists; the login page is shown when admins exist regardless of session state), external IP (fetched hourly from ipinfo.io), internal IP (first non-loopback IPv4 address), disk usage statistics, upgrade availability, the server's UTC timezone offset in minutes, the current locale, and the authenticated username if a valid token is provided.

Service unit counts are split into two fields: `units` counts only package service units (those matching `town-os-package--*`), while `system_services` counts system service units (those matching `town-os-system--*`). Leftover systemd units from uninstalled packages are excluded from the package count. The installed package list is cross-referenced with discovered systemd units by constructing the expected unit name from each package identity.

Unauthenticated requests from non-localhost origins receive a minimal response containing only `needs_setup` and basic status fields. Authenticated requests and all localhost requests receive the full response with all fields listed above, plus `repository_errors` (a map of repository name to error string tracking per-repository refresh failures).

### External IP Polling

The system controller fetches the server's public (external) IP address from `https://ipinfo.io/json`. The poller is started automatically when the HTTP handler is created (`NewHandler`) and when the Unix socket server starts. It fetches the IP immediately on startup, then polls every 1 hour. Each fetch has a 10-second HTTP timeout. The result is cached in an atomic value and included in authenticated ping responses as `external_ip`. Fetch failures are logged at debug level and do not affect the rest of the system; the field is omitted from the response when no IP has been fetched.

## Monitoring

An integrated Prometheus + Node Exporter monitoring stack provides system metrics. The `monitoring.Manager` manages the stack as systemd-supervised podman containers (system services) with `Restart=always`, using the `town-os-system--` naming prefix. The dashboard frontend is configurable via the `monitoring_backend` setting.

### Monitoring Port

Port **5308** is the dedicated monitoring dashboard port. The active backend determines what listens on this port:

- **uPlot mode** (default): a socat forwarder (`socat TCP-LISTEN:5308,fork,reuseaddr TCP:localhost:9090`) exposes the Prometheus HTTP API on port 5308. The React UI queries Prometheus's `/api/v1/query_range` directly and renders charts via uPlot.
- **Grafana mode**: Grafana listens on port 5308 directly (via podman port mapping). The React UI embeds a Grafana iframe.

There are **no reverse proxies** through the systemcontroller (port 5309). The browser talks to port 5308 directly for all monitoring data.

### Monitoring Backend Setting

The `monitoring_backend` system setting controls which dashboard frontend is used:

- `"uplot"` (default) -- lightweight built-in charts rendered in the React UI using uPlot (~35 KB). Queries Prometheus on port 5308 via the socat forwarder. Grafana is not pulled or started, saving ~771 MB on first boot.
- `"grafana"` -- full Grafana dashboards. The Grafana container image is pulled and started on port 5308. Pre-provisioned with a Prometheus datasource and two dashboards.

Changing the setting takes effect immediately: switching to `"grafana"` pulls the Grafana image and starts the container (stopping the socat forwarder); switching to `"uplot"` stops Grafana and starts the socat forwarder.

### Monitoring Containers

- **Node Exporter** (`quay.io/prometheus/node-exporter:latest`, host port 9100) -- collects host system metrics. Runs with host PID namespace, `SYS_TIME` capability, and a read-only bind mount of the host root filesystem at `/host`.
- **Prometheus** (`quay.io/prometheus/prometheus:latest`, host port 9090) -- scrapes Node Exporter and itself at 15-second intervals. Data is stored with 30-day retention in a persistent data directory. Configuration and data volumes are bind-mounted from a monitoring data directory. The systemd unit includes `ExecStartPre` mkdir directives to pre-create volume directories on boot.
- **Grafana** (`docker.io/grafana/grafana:latest`, host port 5308) -- optional dashboarding UI, only started when `monitoring_backend` is `"grafana"`. Uses a light theme (`GF_USERS_DEFAULT_THEME=light`). Anonymous viewing is enabled with the Viewer role, iframe embedding is allowed. The systemd unit includes `ExecStartPre` mkdir directives to pre-create volume directories on boot. Pre-provisioned with a Prometheus datasource and two dashboards (all panels have `transparent: true`):
  - **System Overview** (`town-os-system-overview`) -- high-level percentages: CPU Usage, Memory Usage, Disk Usage (root mountpoint), and Network I/O (bytes/sec per device, excluding loopback). Uses smooth lines with fill, table legends showing mean and last values, 1-hour default range with 30-second auto-refresh.
  - **Town OS Overview** (`town-os-overview`) -- platform-specific metrics: Disk I/O (read/write throughput per block device), Free Storage Space (absolute bytes for `/trunk` btrfs mount and root `/`), Network Stats (bits/sec per device, excluding loopback and virtual interfaces), and CPU % Usage (total non-idle/non-iowait). Uses linear lines without fill, list legends, 6-hour default range. This is the default dashboard loaded by the monitoring UI iframe.
- **Socat forwarder** (`town-os-system--monitoring-proxy`) -- only started when `monitoring_backend` is `"uplot"`. Forwards port 5308 to Prometheus on port 9090.

### uPlot Dashboard Panels

The built-in uPlot dashboard replicates the Town OS Overview with four panels:

1. **Disk I/O** -- read/write throughput in bytes/sec for physical block devices on the `/town-os` mount.
2. **Network (External)** -- receive/transmit in bits/sec per physical network device (excludes loopback, veth, podman, cni, tailscale, bridge, docker interfaces).
3. **CPU Usage** -- stacked by mode (user, system, iowait, irq, softirq, steal, nice) with a total overlay line. 0--100% scale.
4. **Memory Usage** -- total, used, and available bytes.

All panels use the same PromQL queries as the Grafana dashboard JSON, with `5m` substituted for `$__rate_interval`.

### Lifecycle

Prometheus and Node Exporter are always started on boot. The monitoring backend setting determines whether Grafana or the socat forwarder is also started. Startup failures are non-fatal; the system continues without monitoring. Systemd handles restarts via its `Restart=always` policy. The `Stop()` method is a no-op because system services persist across controller restarts.

### Monitoring API

- `GET /monitoring/status` (auth required) -- returns container status (name, image, running state, port) for each service, plus a `backend` field (`"uplot"` or `"grafana"`). Returns `{"status": "disabled"}` when monitoring is not configured.

### Monitoring UI

The monitoring tab in the sidebar navigation opens a dashboard page. Rendering depends on the `backend` field from the status response:

- **uPlot mode**: four chart panels rendered directly in React using uPlot, querying Prometheus on port 5308. Time range selector and auto-refresh are built into the component.
- **Grafana mode**: an embedded Grafana iframe targeting port 5308 in kiosk mode with light theme.

When required services are not running, a warning banner and placeholder message are shown instead.

## UI Container

The system controller manages a standalone UI container (`quay.io/town/ui`) as a system service via `ui.Manager`. The image tag is derived from the system controller's release tag (`quay.io/town/ui:<tag>`), overridable via the `UI_IMAGE` environment variable. Startup failures are non-fatal; the system continues without the UI container.

## Web UI Layout

### Dashboard Services Panel

The dashboard home page displays a full-width installed services panel above the stat card grid. The panel lists all package service units fetched from `GET /systemd/units`. Each service row shows:

- A status icon: green check circle for active, red X circle for failed, gray circle for inactive.
- The package name (parsed from the `package_identifier` field).
- The active state as text.
- The package description (if available).
- Compiled notes from `POST /packages/installed/info`, rendered inline with type-aware links (URL, email, phone).

Clicking any service row navigates to `/dashboard/system`. The panel is hidden when no services are installed. Notes are fetched once per service and cached.

### Layout

The dashboard uses a two-panel layout: a sticky left sidebar and a right content area with a sticky top header bar.

**Sidebar** -- a 256px-wide (`w-56`) vertical panel with the Town OS logo and brand text in a gray banner at the top, followed by vertically stacked navigation buttons (each with an icon and label). Active routes use `variant="secondary"`, inactive use `variant="ghost"`.

**Top status bar** -- a right-aligned horizontal bar showing: connection status pill (loading/offline/online), system services failure count (red pill badge linking to `/dashboard/system?expand=system` when `system_services.failed > 0`), logged-in username with admin badge, and logout button.

## System Services

System services are systemd-managed infrastructure containers (distinct from user-installed package services). They use the `town-os-system--` unit name prefix.

### System Service Unit Generation

`GenerateSystemServiceUnit` produces podman-based systemd units with `Restart=always`. The unit config supports a `VolumeDirs` field listing host directories to pre-create via `ExecStartPre=/bin/mkdir -p <dir>` lines, preventing mount failures when containers start on reboot before the system controller has run.

### System Service API

- `GET /system-services` (localhost or auth) -- list system services with live unit status. Each entry includes key, display name, image, port, and systemd unit status fields. Returns an empty list when monitoring is not configured. Excluded from audit logging.
- `POST /system-services/status` (admin required) -- change a system service's status. Accepts key and action (`start`, `stop`, `restart`). The `enable` and `disable` actions are rejected.
- `POST /system-services/refresh` (admin required) -- refresh system service status.

## Web UI Production Image

An independent UI container image (`quay.io/town/ui`) is built from `Containerfile.ui`. It uses a two-stage build: `oven/bun:latest` builds the UI static files, then `docker.io/library/caddy:latest` serves them on port 80 with SPA routing (`try_files {path} /index.html`).

## Proton Runner Image

The Proton runner image (`quay.io/town/proton`) is built from `Containerfile.proton`. It uses a two-stage build: a downloader stage fetches the GE-Proton release tarball (pinned via `GE_PROTON_VERSION` build arg), and the runtime stage installs Wine/Proton dependencies (64-bit + 32-bit), Xvfb for headless operation, and a wrapper script at `/usr/local/bin/proton` that starts a virtual framebuffer and configures the Proton environment before executing the application.

The make pipeline provides: `release-proton-image` (build), `push-proton-rc` (push release candidate with date tag + `rc.latest`), and `push-proton-release` (push release with date tag + `latest`). The proton image is also included in the full `push-rc` and `push-release` flows.

## Web UI API Client

The browser determines the API base URL at runtime from `window.location`, using the current protocol and hostname with port 5309 (e.g., `https://myhost:5309`). No server-side proxy is involved; the browser talks directly to the system controller API.

The `VITE_API_URL` environment variable overrides the browser-derived URL when set. This is useful during development when the API server runs on a different host or port.

The monitoring dashboard derives its monitoring port URL (port 5308) from the current hostname. When `VITE_API_URL` is set, the hostname is extracted from it; otherwise `window.location.hostname` is used.

## Web UI Accessibility

All dialog components include a `DialogDescription` element providing a concise description of the dialog's purpose. This satisfies the Radix UI accessibility requirement for screen readers and eliminates `aria-describedby` warnings. Descriptions are placed inside the dialog header after the title and are visible to all users.

## Internationalization

All user-facing strings (UI labels, error messages, toast notifications, audit log action descriptions) are translatable via a message catalog pattern.

### Backend

The `i18n` package provides a `T(locale, key, args...)` function that resolves translation keys. The fallback chain is: requested locale, then `en-US`, then the raw key string. When `args` are provided, `fmt.Sprintf` formatting is applied. Only `en-US` is currently populated; all other locales fall back to the English catalog. Message keys use dot-separated namespaces (e.g., `auth.login_failed`, `pages.toast_provisioned`).

### Locale Lists

BCP 47 locale codes are used throughout. Two curated lists are provided:

- **CommonLanguages** (21 entries) -- Arabic (ar-SA), Bengali (bn-BD), Chinese (zh-CN), Dutch (nl-NL), English (en-US), French (fr-FR), German (de-DE), Hindi (hi-IN), Italian (it-IT), Japanese (ja-JP), Korean (ko-KR), Polish (pl-PL), Portuguese (pt-BR), Russian (ru-RU), Sanskrit (sa-IN), Spanish (es-ES), Swedish (sv-SE), Thai (th-TH), Turkish (tr-TR), Ukrainian (uk-UA), Vietnamese (vi-VN). Each entry includes the native-script name and English name.
- **ExtendedLocales** (87 entries) -- comprehensive list of country-specific locale variants (e.g., de-AT, en-GB, es-MX, fr-CA, pt-PT, zh-TW).

### Frontend

A React context provider (`I18nProvider`) wraps the application and exposes a `useI18n()` hook returning `{ locale, setLocale, t }`. The `t` function resolves keys against the frontend catalog with the same fallback chain as the backend. Parameter interpolation uses `{name}` placeholders (e.g., `t('greeting', { name: 'Alice' })`).

### Locale Storage and Sync

The current locale is stored as a system setting (key: `locale`, default: `en-US`). It is a system-wide setting, not per-user. The status ping response includes a `locale` field; the UI syncs the locale from the ping on each poll.

### Locale API

- `GET /locales` (auth required) -- returns the current locale, list of populated locales, common languages, and extended locales. Excluded from audit logging.

### Settings UI

The system settings page includes a language picker. Common languages are shown in a dropdown with native-script names. An expandable section reveals the extended locales list. Unpopulated locales (those without a translation catalog) are displayed with an asterisk suffix and are disabled in the selector, preventing selection.

## System Controller Configuration

### Startup Sequence

The authoritative step-by-step boot ordering lives in [System Controller Boot Sequence](#system-controller-boot-sequence). In summary:

1. `setupPodmanEnv()` points `CONTAINER_HOST` at the host podman socket.
2. Flag parsing, directory creation, and NC image build (the first podman operation).
3. Database, account, session, audit, settings, and (optional) pages managers.
4. Repository seeding, repository root force-refresh.
5. Install manager, btrfs storage, systemd manager.
6. Rolodex config write + readiness wait (rolodex itself is supervised by systemd).
7. Monitoring image pulls and system service starts (Prometheus, Node Exporter, Monitoring UI).
8. Version-change detection, reconcile, post-update commands.
9. DNS reconcile, UI container start.
10. HTTP server listen.

Startup failures for monitoring, Rolodex config, the network controller image build, and the UI container are non-fatal; the system continues without them. All container image pulls use the `ensureImage` helper which checks `podman image exists` before pulling, avoiding redundant pulls in test/dev environments where images are pre-loaded. Pull failures for non-essential services are logged to stderr and do not prevent startup, allowing the system to boot even when the network is temporarily unavailable.

### Version Tag Detection

The system controller reads an optional `/town-os.tag` file at startup to derive matching image tags for sibling services (UI, Rolodex). The fallback chain is: `/town-os.tag` file, then compile-time `Version` variable (set via ldflags), then `rc.latest`. This tag constructs image references like `quay.io/town/ui:<tag>` and `quay.io/town/rolodex:<tag>`.

### Error Format

All API errors are returned as RFC 9457 Problem Detail objects (structured JSON with type, title, status, and detail fields). A custom `ProblemDetailHTTPErrorHandler` is set as the Echo error handler.

### Request Logging

Echo's `RequestLogger()` middleware is enabled globally, logging all HTTP requests to stderr. The verbosity is controlled by the `LOG_LEVEL` environment variable.

### CORS

In `DEBUG` mode, all origins are allowed. Otherwise, cross-port requests from the same hostname are permitted (e.g., browser on port 80 talks to API on port 5309). Allowed methods: GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS. Credentials are allowed with a 3600-second max age. The Private Network Access API is supported (`Access-Control-Allow-Private-Network` header).

### Graceful Shutdown

SIGINT triggers context cancellation. The HTTP server shuts down, and all background goroutines exit via context channels. Rolodex is supervised by systemd and is not stopped by the systemcontroller.

### CLI Flags

- `-db <path>` -- path to SQLite database (defaults to ephemeral temp file).
- `-btrfs <path>` -- base path for btrfs subvolume operations.
- `-repo-dir <path>` -- base directory for git repositories (defaults to ephemeral temp dir).
- `-network-state <path>` -- directory for per-package network state files (defaults to `/var/run/town-os`).
- `-listen <addr>` -- HTTP listen address (defaults to `:5309`).

The NC binary path is not a flag; it is hardcoded to `/town-os-networkcontroller` (baked into the systemcontroller container at build time so the NC binary always matches the running systemcontroller).

### Environment Variables

- `CONTAINER_HOST` -- unix socket URL for the host podman daemon. Set automatically at startup to `unix:///run/podman/podman.sock` (see `HostPodmanSocket`). Every `podman` invocation — including child processes forked by the systemcontroller — inherits this from the process environment and routes through the host socket instead of the systemcontroller container's isolated podman storage. The install-repo systemd unit should also set `Environment=CONTAINER_HOST=...` for visibility in `systemctl` output, but the `setupPodmanEnv()` call is the runtime source of truth.
- `TOWN_OS_LISTEN` -- overrides `-listen` flag.
- `TOWN_OS_SIGNING_KEY` -- override the ephemeral JWT signing key (see Session Management).
- `TOWN_OS_TEST` -- if set, use test repositories instead of production defaults.
- `DEBUG` -- if set, allow all CORS origins and prepend test repositories to defaults.
- `LOG_LEVEL` -- logging level: `debug`, `info`, `warn`, `error` (defaults to `error`).
- `TOWN_OS_REPO_USERNAME` / `TOWN_OS_REPO_PASSWORD` -- repository credentials applied to all repositories on first initialization.
- `ROLODEX_IMAGE` -- override Rolodex container image (defaults to `quay.io/town/rolodex:<tag>`).
- `UI_IMAGE` -- override UI container image (defaults to `quay.io/town/ui:<tag>`).

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

### Settings

| Key                      | Default                          | Description                                     |
| ------------------------ | -------------------------------- | ----------------------------------------------- |
| `default_quota`          | `53687091200`                    | Default volume quota in bytes (50 GB)           |
| `max_archive_size`       | `1073741824`                     | Maximum upload size in bytes (1 GB)             |
| `archive_unpack_timeout` | `600`                            | Unpack timeout in seconds (10 min)              |
| `locale`                 | `en-US`                          | BCP 47 locale code (system-wide)                |
| `proton_image`           | `quay.io/town/proton:latest`     | Proton runner container image (GE-Proton)       |
| `dns_tld`                | `home`                           | Default top-level domain for package DNS records|
| `monitoring_backend`     | `uplot`                          | Monitoring dashboard: `uplot` or `grafana`      |

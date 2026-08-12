# Town OS Design

How Town OS works: the architecture, the behavior of every subsystem, the API
surface, and the invariants that hold them together. Build instructions, test
rules, and code style live in [CLAUDE.md](CLAUDE.md).

A behavioral change belongs in this file as part of the same commit that makes
it. A change to how the repo is built or tested belongs in CLAUDE.md.

Translations of this file: Chinese [zh-CN](DESIGN.zh-CN.md) (Simplified) and
[zh-TW](DESIGN.zh-TW.md) (Traditional); Spanish [es-ES](DESIGN.es-ES.md)
(Spain) and [es-MX](DESIGN.es-MX.md) (Mexico); Japanese
[ja-JP](DESIGN.ja-JP.md). See also
CLAUDE.md ([zh-CN](CLAUDE.zh-CN.md), [zh-TW](CLAUDE.zh-TW.md),
[es-ES](CLAUDE.es-ES.md), [es-MX](CLAUDE.es-MX.md), [ja-JP](CLAUDE.ja-JP.md)) and
README.md ([zh-CN](README.zh-CN.md), [zh-TW](README.zh-TW.md),
[es-ES](README.es-ES.md), [es-MX](README.es-MX.md), [ja-JP](README.ja-JP.md)).
**This file is authoritative** — describe the change here, and the translations
follow.

## Architectural Invariants

Rules that constrain the design rather than the code. Breaking one does not fail
a build or a linter — it produces a box that boots and then misbehaves, usually
somewhere far from the change.

- **The storage layer manages volumes; gfeh provides object storage.** `src/storage` deals in btrfs subvolumes and quotas and nothing else -- it does not handle object storage at all. Objects, per-file metadata and permissions, the hierarchical user/ACL database, sharing, per-file HTTP exposure, federation, and every protocol view (S3, IPFS, Google Drive, plain HTTP — and SMB/CIFS, which gfehd implements but Town OS does not serve) belong to gfeh, which is the responsible party. Never add object/blob/per-file endpoints to `src/storage` or `/storage/*`, and never teach `storage.Storage` or `storage.Controller` about users, permissions, or protocols. See [Storage](#storage).

- **Pages feature is always enabled** — the pages subsystem (static site hosting via Caddy) is initialized unconditionally at startup; there is no `TOWN_OS_PAGES` env gate. The pages manager is non-nil in a normal boot, so the pages API is always available. The handlers still keep a defensive nil-manager guard that returns "pages not configured" (exercised by tests that build a server without `ServerConfig.PagesMgr`), but real boots never hit it.

- **Version change detection and unit restart** — the systemcontroller detects image upgrades by comparing the running container's image SHA (from `/proc/1/cgroup` → `podman inspect`) against a persisted version file at `<btrfsPath>/town-os-version`. On version change: (1) all container images are pulled, (2) the NC image is rebuilt, (3) reconcile regenerates all systemd units, (4) units whose content changed are restarted in order: NC units first (they own networks), then dependency services, then parent/standalone services, (5) post-update commands (`post_update` field) are executed via `podman exec` for container packages whose units changed. The version file is written after successful reconcile. Unit content is compared before/after via `ReadUnit()` to avoid unnecessary restarts when content hasn't changed.

- **The network controller image is pulled, not built at boot** — the NC image is a published sibling image (`quay.io/town/networkcontroller:<tag>`, tag from `resolveImageTag()`) pulled alongside the other core images, exactly like the UI, rolodex, and ingress images. It is **not** built with `podman build` during startup; the earlier boot-time build (`localhost/town-os-networkcontroller:local`, alpine base, `--dns=8.8.8.8`) is gone. `NC_IMAGE` overrides the derived default and is what the integration harness sets to inject a locally built image. The pull is non-fatal: every package NC unit carries an `ExecStartPre` `--pull=never` network-create fallback, so a failed pull is recoverable on the next boot.

- **All monitoring services are system services** — Prometheus, Node Exporter, and the Monitoring UI all run under the system service namespace (`town-os-system--` prefix), started directly from `main.go` before reconcile. They are never installed through the package repository system; there is no installable "monitoring" package. The three services are: `town-os-system--node-exporter.service` (host networking, port 9100), `town-os-system--prometheus.service` (port 9090, bind-mount config/data from `{btrfsBase}/monitoring/`), and `town-os-system--monitoring-ui.service` (port 5308). The monitoring UI service runs either a socat forwarder (uPlot mode, default) or Grafana (grafana mode), controlled by the `monitoring_backend` setting. Prometheus config is written directly to disk. Prometheus, Grafana, and the uPlot socat forwarder are generated via `systemd.GeneratePackageUnits` with `PackageUnitConfig.SystemServiceKey` set, so they get a full network controller, socket activation, and a private podman network — the same plumbing as regular packages, but with the system service naming.

- **Host-volume ownership is declarative on `HostVolumeMount`, and non-recursive** — container images with a hardcoded internal uid (Grafana's `472`, Prometheus's `65534`, etc.) need to write into their bind-mounted host path, and bind mounts pass host ownership straight through, so the host path must be owned by that uid:gid before the container starts. We bind-mount (rather than using a named podman volume, which podman would chown on first create) because we want the data on a btrfs subvolume with a quota. The `systemd.HostVolumeMount` struct in `src/systemd/unit.go` carries optional `UID *uint32` and `GID *uint32` fields; when both are set, the unit generator emits **`ExecStartPre=/bin/chown <uid>:<gid> <hostpath>`** (no `-R`) for that mount right after the `ExecStartPre=/bin/mkdir -p` lines and before `podman run`. This is the single declarative source of ownership for host-bind-mounted volumes on system services and replaces the previous hand-rolled `ExecStartPreExtra` chown entries in `GrafanaPackageConfig` and `PrometheusPackageConfig`.

  The chown is deliberately non-recursive, which is sufficient because:
  1. **Writable mounts** (`grafana-data` → `/var/lib/grafana`, `prometheus-data` → `/prometheus`) only need top-level ownership so the container can create its own subdirectories inside. The container process creates those children as its own uid (472 or 65534), so they are already correctly owned and never drift. No recursion needed.
  2. **Read-only mounts** (`grafana-provisioning` → `/etc/grafana/provisioning`) do not declare UID/GID at all and emit no chown line. As long as host permissions are 0755/0644 (which `WriteGrafanaProvisioningFiles` sets), any uid can read the contents regardless of who owns them.

  `EnsureGrafanaStorage` (`src/monitoring/monitoring_ui.go`) now only creates the directories and returns; it does no chowning at all. `WriteGrafanaProvisioningFiles` writes the datasource and dashboard YAML/JSON files with world-readable perms and does not need to fix ownership afterwards. The in-process `filepath.WalkDir`-based chown that used to walk `grafana-data` on every boot is gone; the single `chown` syscall emitted by systemd is the authoritative fix-up. The uid/gid constants still live in their respective files (`grafanaUID = 472` / `grafanaGID = 472` in `monitoring_ui.go`, `prometheusUID = 65534` / `prometheusGID = 65534` in `prometheus.go`); do not change these without matching the upstream container image.

- **Network state directory must be host-shared** — the `-network-state` default is `/run/town-os` (`DefaultNetworkStatePath` in `src/svc/systemcontroller/cmd/systemcontroller/main.go`). The systemcontroller runs inside a container but creates NC containers on the host via `CONTAINER_HOST`, so the bind-mount source path (`-v /run/town-os:/run/town-os:ro` in every NC unit) must exist on the host filesystem. The install-repo systemcontroller systemd unit must bind-mount `/run/town-os:/run/town-os` and ensure the host directory exists before the systemcontroller starts (`ExecStartPre=/usr/bin/mkdir -p /run/town-os` or `RuntimeDirectory=town-os`). Without that mount, the systemcontroller's `os.MkdirAll` and state-file writes land inside the container's tmpfs, the host directory does not exist, and NC containers fail to start with `Error: statfs /run/town-os: no such file or directory` — taking down Prometheus, the monitoring UI, and every package with networking. Never default to `/var/run/town-os` or any path under `/var/run` or `/tmp`; the path must live under `/run` (or another host-shared bind mount) and must be the same path on both sides of the mount.

## System Controller Boot Sequence

The system controller startup in `src/svc/systemcontroller/cmd/systemcontroller/main.go` follows this exact order. Each step that says **(non-fatal)** logs to stderr and continues; everything else is fatal and aborts startup.

The boot is **observable**: `:5309` is bound before any work happens, backed by a minimal boot-status stub that streams progress; the full Echo router is swapped in at the end without ever closing the listener. Progress is reported as five coarse stages (`boot_controller`, `boot_dns`, `boot_services`, `restart_packages`, `ready`) — see [Boot Status and Refresh](#boot-status-and-refresh).

1. **Set `CONTAINER_HOST`** — `setupPodmanEnv()` sets `CONTAINER_HOST=unix:///run/podman/podman.sock` so every subsequent `podman` invocation (and child process) routes through the host's podman socket instead of the systemcontroller container's isolated storage.
2. **Parse CLI flags and env vars** — `-db`, `-btrfs`, `-repo-dir`, `-network-state`, `-listen`. Env overrides: `TOWN_OS_LISTEN`.
3. **Bind `:5309` with the boot handler** — `NewBootStatus()` + `NewRootHandler(NewBootHandler(bs))` bind the listener immediately, before any startup work. Until the swap in step 24 the socket answers only `GET /status/ping` (503 with `{booting, step, done, boot_id}`) and `GET /boot-status` (SSE); everything else is 403.
4. **Stage `boot_controller`** — temp working dir; create btrfs base and network state dir; remove any stale `town-os.db` left at the btrfs root by older deployments (`cleanupStaleRootDB`) and reject a `-db` path that would recreate it (`validateDBPath`) — the runtime DB lives at `<btrfsBase>/data/db/system.db`, never at the root.
5. **Open SQLite database** — persistent if `-db` is set, otherwise ephemeral temp file.
6. **Init account manager** — creates the accounts table and migrates a legacy one (capability columns become grants; `smb_nt_hash` is dropped). Then `PurgeLegacyServiceAccounts` **(non-fatal)** removes the object-storage daemon's old administrator account and its stored password, once, on the first boot after an upgrade — see [No service accounts](#no-service-accounts).
7. **Generate ephemeral JWT signing key** — 32 random bytes via `crypto/rand`, overridable with `TOWN_OS_SIGNING_KEY`. Init session manager, which clears all prior sessions (old tokens are invalid with the new key).
8. **Init audit, settings, pages, and network managers** — settings are seeded with defaults (`default_quota`, `max_archive_size`, `locale`, `dns_tld`, `dns_resolution_mode`, `peer_ttl`, …); pages is always initialized; the network manager owns the WireGuard network and peer tables **and seeds the home network**, so from this point on it always exists (see [The home network always exists](#the-home-network-always-exists)).
9. **Seed repositories** — if `repositories.json` does not exist, write default repos (or test repos if `TOWN_OS_TEST`/`DEBUG`). Apply `TOWN_OS_REPO_USERNAME`/`TOWN_OS_REPO_PASSWORD` credentials.
10. **Init repository root and force refresh** — clones/fetches all configured repos via go-git.
11. **Init install manager, btrfs storage, systemd manager**.
12. **Resolve image tag** — `resolveImageTag()`: the `TOWN_OS_TAG` env var (set by the install build system), else `rc.latest-<arch>` (`defaultVersionTag()`, arch from `runtime.GOARCH` mapped to `x86_64`/`aarch64` via `archTag()`). No `/town-os.tag` file and no compile-time `Version` pin. Every sibling image tag (UI, rolodex, network controller, ingress) derives from this one value; push tags are per-arch, so derived sibling tags are too.
13. **Derive the NC image** — `quay.io/town/networkcontroller:<tag>`, overridable via `NC_IMAGE`. Pulled (step 17), never built.
14. **Start background repo refresh** — goroutine polls every 5 minutes.
15. **Stage `boot_dns`: write Rolodex config and restart if changed** **(non-fatal)** — Rolodex is a boot service managed by systemd. The systemcontroller writes `rolodex.yml` (idempotent: skips if the file is newer than the binary and the content is unchanged) and restarts the service only when the file was written. `resolution.mode` comes from the `dns_resolution_mode` setting, and an unparseable stored value falls back to the default rather than rendering a config rolodex would refuse. `forwarders:` comes from the `dns_local_forwarders` setting: when it is on, the list is discovered from the host's resolvers on every boot, so a box that changed networks picks the new one up without the operator touching anything (see [Local forwarders](#local-forwarders)). The rolodex container runs with `--net host` and binds DNS to `127.0.0.2:{port}` directly. Then wait for DNS readiness (TCP connect poll), and configure systemd-resolved to route the TLD to rolodex — **skipped when `TOWN_OS_DNS_PORT` has moved rolodex off `:53`**, since a resolved per-domain server address carries no port and would blackhole every query for that TLD.
16. **Read the monitoring backend and discover btrfs disk devices** — `monitoring_backend` (default `uplot`); `monitoring.BtrfsDevices(btrfsPath)` **(non-fatal)** surfaces the backing block devices through `/monitoring/status`.
17. **Stage `boot_services`: pull core container images** **(non-fatal)** — the NC image, Prometheus, Node Exporter, the UI image, and Grafana when that backend is selected, in parallel via `parallelEnsureImages` (skips a pull when the image is already loaded).
18. **Start monitoring system services** **(all non-fatal)** — legacy NC/socket monitoring units from the previous design are torn down first (they still hold `-p 9090`/`-p 5308` and would crash-loop the new services). Node Exporter, Prometheus, and the Monitoring UI all run `--net host`; node-exporter and Prometheus bind loopback, and only the monitoring UI's `:5308` is LAN-facing. All three ports come from `monitoringPortsFromEnv()`, whose zero value is the production defaults ([System-service host ports](#system-service-host-ports)). Then install the nightly podman prune timer **(non-fatal)**.
19. **Ensure the local TLS CA** **(non-fatal)** — `tls.EnsureCA(<btrfsPath>/tls)` before reconcile, so reconcile can issue leaf certs as it walks installed packages.
20. **Start the ingress and the pages service** **(non-fatal)** — `ingressctl.Manager` installs and starts `town-os-system--ingress` (shared `:443` SNI + `:80` Host router), dual-stack only when the host has a global IPv6. The pages Caddy service starts alongside it. Both are skipped when `INGRESS_IMAGE` is explicitly set to empty (dev mode).
21. **Reconcile object storage** **(non-fatal)** — `ReconcileGfeh` ensures one gfeh partition per network: the `gfeh/<network>` subvolume (chowned to uid 2000), the rendered `gfehd.yaml`, and the `town-os-system--gfeh-<network>` unit, restarted only when the rendered content changed. Skipped entirely when `GFEH_IMAGE` is explicitly empty, and skipped when the ingress is disabled (the four HTTP views are only reachable through it). The partitions' *names* are published later and asynchronously — see step 30. See [Object Storage (gfeh)](#object-storage-gfeh).
22. **Detect version change** — compare the running container's image SHA (`/proc/1/cgroup` → `podman inspect`) against `<btrfsPath>/town-os-version`. Sets `versionChanged` for reconcile.
23. **Reconcile** — iterates all installed packages and restores runtime state:
    - Creates root btrfs subvolumes (`installed`, `uninstalled`, `archives`, `pages`, `vm-images`, `user`, `tls`, `gfeh`).
    - For each installed package (latest version per repo/name): loads YAML, compiles with saved responses, creates btrfs volumes with quotas, seeds empty volumes from archives/git/proton, applies file templates, issues the package's TLS leaf, writes network state files (including the resolved `fqdn`), generates and installs systemd units (service + NC + sockets), starts services.
    - If `versionChanged`: restarts units whose content changed (NC first, then deps, then services), then runs `post_update` commands.
    - Reconciles pages: ensures subvolumes, symlinks, and page content.
    Then persist the current image SHA to `<btrfsPath>/town-os-version`.
24. **Reconcile DNS and networks** — dial the rolodex gRPC socket (retry up to 30s). `RebuildDNS` wipes and rebuilds rolodex from scratch so drift from a crashed prior run is discarded; `RebuildNetworkDNS` re-registers the LAN-facing global records (and DANE pins) for non-default-network packages. `ReconcileNetworks` then reconciles the home network's TLD against `dns_tld` and brings up every enabled network's WireGuard interface, passing the rolodex client so each network's TLD scope is owned — including the DNS-only home scope. All non-fatal. Object storage is then reconciled **a second time** (idempotent), so a network this step brought up gets its partition without waiting for a restart.
25. **Program the ingress** **(non-fatal)** — wait for readiness, dial its gRPC socket, and `RebuildIngress` pushes the full route set (HTTP packages + pages + object-storage views and indexes) declaratively, the same model as `RebuildDNS`. It also renders each partition's index page from exactly the site set those routes are built from, on the same pass — a route cannot be programmed before the bytes it serves exist ([The partition index](#the-partition-index)).
26. **Start the UI container** **(non-fatal)** — `town-os-system--ui.service`; skipped when `UI_IMAGE` is explicitly empty (dev mode, where bun serves the UI).
27. **Stage `restart_packages`: freshness stage** — if the previous process left a refresh marker, restart every installed package unit serially, emitting a per-package progress event so the UI renders a row each. A stale marker from a crash is harmless.
28. **Create the HTTP handler** — wires all managers into `ServerConfig`, starts the background pollers (external IP hourly, DNS drift repair, expired-peer reaper), configures the Echo router with CORS, the fail-closed grant allowlist, auth, and audit middleware.
29. **Stage `ready`: swap the root handler** — the boot stub is atomically replaced by the full Echo router on the already-bound listener, so no port flap occurs and in-flight `/boot-status` SSE subscribers survive the handoff. `BootStatus.Done()` then closes the stream. **System is now ready.**
30. **Publish the object-storage names** **(non-fatal, background)** — `publishGfehNames` waits for at least one partition to answer its admin socket, then re-runs the DNS and ingress rebuilds so each partition's `/v1/names` output becomes A records, TLSA pins, leaf SANs, and ingress vhosts. It runs **after** the swap, and asynchronously, because gfehd polls `/status/ping` — which answers 503 until step 29 — before authenticating, so waiting for it inline would deadlock the boot it is waiting on. If nothing becomes ready in time the names are published by the next reconcile.
31. **Graceful shutdown** — on SIGINT: cancel context, shutdown HTTP server with a 30s timeout. All background goroutines exit via context cancellation.


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

- `POST /repository/add` (admin required) -- add a new repository. Accepts name, URL, and optional username/password credentials. If no credentials are provided, system-default credentials are used. The repository is cloned via go-git and a refresh is triggered.
- `POST /repository/remove` (admin required) -- remove a repository by name and trigger a refresh.
- `POST /repository/move` (admin required) -- change the priority position of a repository. Accepts name and target position index.
- `POST /repository/refresh` (admin required) -- force-refresh all repositories. Returns any refresh errors.
- `GET /repository` (auth required) -- list all repositories with search, sorting, and pagination. Each entry includes name, URL, username, and any refresh error.

### Repository Refresh

Repositories are refreshed periodically (default 5-minute interval) by fetching from origin via go-git. Stash/apply is used around dirty trees during refresh. Refresh errors are tracked per repository and exposed via the list and status ping endpoints.

## Package System

### Package Definition

Packages are defined in YAML with the following structure:

- `image` -- container image reference (mutually exclusive with `vm`).
- `vm` -- virtual machine configuration (mutually exclusive with `image`). See **VM Configuration** below.
- `proton` -- Proton/Wine runner configuration for Windows executables (mutually exclusive with `vm` and `command`). See **Proton Configuration** below.
- `entrypoint` -- list of strings that replaces the image's built-in `ENTRYPOINT` at podman-run time. Emitted as `podman run --entrypoint='["..."]'` (JSON array, single-quoted so systemd forwards it verbatim). Required for images whose upstream ENTRYPOINT is a wrapper script that rejects arbitrary command args (e.g. `matrixdotorg/synapse`'s `/start.py` interprets the first arg as a "mode" and errors on any unknown value — a package that wants `command: [sh, -c, "…"]` must also set `entrypoint: [sh, -c]` so podman replaces `/start.py` outright). Container runtime only; rejected for VM packages (`ErrEntrypointVMNotSupported`) and for Proton packages (Proton auto-generates its own command).
- `command` -- list of strings that becomes the container CMD (argv passed AFTER the entrypoint). Container runtime only; mutually exclusive with `proton`. Multi-word args containing whitespace or shell metacharacters are single-quoted in the generated unit file so systemd's ExecStart tokenizer forwards them as a single argv element — a chained `"a && exec b"` string stays one argument, and its `&&` is forwarded to `sh -c` (when entrypoint is `[sh, -c]`) rather than split by systemd.
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

Questions prompt the user during package installation. Each question has a `query` (display text), an optional `type` (output type for validation), and an optional `default` value. Question names must start with an alphanumeric character and may contain only alphanumeric characters and underscores (e.g. `port`, `dbpass`, `registration_secret`). Dashes, dots, and other punctuation are rejected; underscores are allowed because question names are used as `@template@` markers and multi-word identifiers like `registration_secret` are common in real packages.

#### Output Types

- **port** -- validated port number (1--65535). Auto-generates a random available port in the range 10000--60000 when the response is empty or `"auto"`.
- **hostname** -- lowercase alphanumeric with dashes. Auto-generates `<package-name>-<4-char-hex>` when empty.
- **volume** -- alphanumeric with dashes and underscores.
- **bytes** -- human-readable byte sizes (`mb`, `gb`, `tb` suffixes).
- **archive** -- archive file name.
- **duration** -- time durations (`s`, `m`, `h`, `d` suffixes).
- **secret** -- auto-generates a cryptographically secure value when the response is empty or `"auto"`. Generates 32 bytes via `crypto/rand`, returned as a 64-character hex string (256 bits of entropy). Suitable for passwords, encryption key salts, and other secret values. Users can override by providing an explicit response.
- **boolean** -- a yes/no option, rendered as a **checkbox** in the install questions dialog rather than a text input. Validation is `strconv.ParseBool`, which accepts exactly the spellings yaml.v3 (YAML 1.2) treats as booleans plus `1`/`0`/`t`/`f`, case-insensitively; `yes`/`no` are **not** accepted. The answer is normalized to the string `"true"` or `"false"`, so `@variable@` substitution and file templates (`{{.Responses.key}}`) always see one canonical form and can be tested with `{{if eq .Responses.key "true"}}`.

  An unchecked checkbox submits nothing, and a dependency's boolean question often goes unanswered by its parent — both would otherwise trip `Compile`'s empty-response validation. `autoGenerateResponses` (`controller_install_preview.go`) therefore resolves a missing or empty boolean to the question's `default` (normalized), or to `"false"` when no default is declared. An explicit `"false"` from the form always wins over a `default: "true"`, so a default-on option can actually be turned off; a `default` that `strconv.ParseBool` cannot parse is a package bug and fails the install rather than silently installing with the option off.

  The package info dialog renders saved boolean answers as Yes/No instead of the raw `"true"`/`"false"` string, and boolean questions bypass the cached-value/clear-button path in the install dialog — a saved answer simply pre-checks the box and stays directly editable.

- **oauth** -- a token obtained by running a device flow from the install dialog, rather than typed. Validated like a secret (any non-empty string), never auto-generated, and masked in the package info dialog. The install dialog renders a **Connect** button in place of a text field; a cached answer from a previous install renders as already connected, so a reinstall does not send the operator back to the provider.

#### OAuth questions

Some applications are configured with a credential only their vendor can mint -- a Plex account token, a GitHub personal token -- and the only way to get one has been to run a shell script by hand and paste what it printed. An `oauth` question runs that flow from the dialog instead.

There is **no provider registry**. The question carries an `oauth:` block naming the provider's own URLs, so a package can use any vendor with a device-style flow without a change to Town OS:

```yaml
questions:
  plextoken:
    query: "Plex account"
    type: oauth
    oauth:
      start: { method: POST, url: "https://plex.tv/api/v2/pins?strong=true", headers: { X-Plex-Client-Identifier: "{{client_id}}" } }
      extract: { id: id, code: code }
      approve: "https://app.plex.tv/auth#?clientID={{client_id}}&code={{code}}"
      poll: { url: "https://plex.tv/api/v2/pins/{{id}}", headers: { X-Plex-Client-Identifier: "{{client_id}}" } }
      token: authToken
      interval: 2s
      timeout: 10m
```

`start` opens the flow; `extract` names JSON fields to pull from its response; `approve` is the URL the browser opens; `poll` is repeated until the JSON field named by `token` stops being absent or null, which is exactly what "the user has not approved yet" looks like on the wire. `{{...}}` placeholders resolve against the extracted values plus `{{client_id}}`, a random per-flow identifier the controller sends on every step (Plex ties the pin to it). An extracted JSON number is rendered as digits, not as `1.234567e+06` -- a float-formatted pin id would 404 the poll URL and hang forever in "pending".

The flow lives in `src/packages/oauth.go` (schema plus validation) and `src/svc/systemcontroller/controller_oauth.go` (execution). `POST /packages/oauth/start` runs the start step and returns `{flow_id, approve_url, user_code, interval_ms}`; `POST /packages/oauth/poll` runs one poll step and returns `pending`, `approved` with the token, or `expired`. Both require admin. The server keeps the flow only until it is redeemed -- the token is handed to the browser, which submits it as the question's answer like any other response, so holding a copy server-side would only add a second place for it to leak from.

Validation comes in two halves, and conflating them is a bug. `ValidateOAuthSpec` checks the *shape* of the flow (required fields, parseable durations, no template in a URL's host) and is what `Compile` runs when a package is installed. `ValidateOAuthFlow` is that plus the address policy below, and runs only when a flow is about to be *executed*. An install happens long after its flow ran, on a host whose `OAuthAllowPrivate` setting `Compile` cannot see — so applying the address policy at compile time rejects an install whose own flow had just succeeded.

**The address guard is load-bearing.** A package names arbitrary URLs and the *controller* dials them, so without a guard a package could aim it at the host's own network. `packages.CheckOAuthAddr` runs in the HTTP client's `DialContext` (and on every redirect) and refuses loopback, private, link-local, multicast, unspecified, and CGNAT addresses; URLs must be `https`. Checking at dial time rather than parse time is what makes it DNS-rebinding-proof. `ServerConfig.OAuthAllowPrivate` relaxes it and exists only so tests can point a flow at an `httptest` server on 127.0.0.1.

#### Optional questions

Any question may set `optional: true`. Every other question must be answered with a non-empty value, which leaves a package author no way to express a setting the application can genuinely do without — an SMTP relay, an API key — except by inventing a placeholder default and hoping the operator overwrites it.

An optional question may be absent from the responses map or answered with an empty string; `Compile` exempts it from both `ErrMissingResponse` and `ErrEmptyResponse`, and substitutes the **empty string** at its `@variable@` sites. A blank answer also skips `OutputType.Output`, whose job is to reject exactly that for a typed question — an empty string is not a valid port — so `optional` composes with `type`: an answered optional port is still validated as a port, while a blank one compiles away to nothing.

Two details matter for correctness. `Compile` substitutes by walking the responses it was given, so a question omitted from the map entirely gets a second pass that fills its markers with the empty string; without it the literal `@smtp_host@` would survive into the container's environment. And `autoGenerateResponses` skips optional questions before the type switch: generating a value would defeat the question, since a blank optional secret would otherwise arrive as a random 256-bit string that the application would dutifully try to authenticate with. A blank optional question falls back to its `default` if it declares one, and to the empty string otherwise.

`optional` is meaningless on a boolean, which is a checkbox and always resolves to one of its two values.

#### Conditional questions (`show_if`)

A question may carry `show_if: <boolean_question>`, naming a boolean question in the same package. The install dialog keeps the question hidden until that checkbox is checked, so a package can tuck an advanced group — an SMTP relay, an API key — behind one switch instead of confronting the operator with every field at once.

It is more than a UI hint: the compiler honors it. While the controlling boolean resolves to false, the conditional question compiles to the **empty string** and is exempt from the answered-and-non-empty requirement — exactly as if it were `optional` and left blank — *no matter what the still-mounted field submitted*. `questionHidden` (`src/packages/questions.go`) reads the control value from the submitted response, falling back to the boolean's declared `default` when the operator never touched it, and parses it leniently because an unchecked box may arrive as `"false"`, `"0"`, or not at all. `Compile` forces the empty string and skips `Output()` for a hidden question, so a stale value cannot fail type validation for a field the operator cannot even see; a question omitted from the responses map entirely still gets its `@marker@` sites filled with the empty string. When the boolean is true, a non-optional conditional question is required as usual.

`ValidateShowIf` rejects a `show_if` that references a question that does not exist (`ErrShowIfUnknown`), one that is not of type `boolean` (`ErrShowIfNotBool`), the question itself (`ErrShowIfSelf`), or another question that is itself conditional (`ErrShowIfChain` — no chains). A conditional question is only coherent if the thing controlling its visibility is a plain checkbox.

### Compilation

Compilation validates all responses, applies type-specific validation, substitutes all template variables, normalizes container image URLs, and produces a resolved `Package` struct. For VM packages, memory strings are parsed to byte counts and CPU defaults are applied. Post-update commands are trimmed of leading/trailing whitespace. Validation errors are collected and returned together.

**No value that reaches a systemd unit may carry a control character.** A unit file is line-oriented and its quoting does not span lines: a directive ends at the first raw newline no matter what quotes enclose it. So a value carrying one does not corrupt its own line — everything after the newline is parsed as a fresh directive in the same `[Service]` section, and an environment value of `somevalue\nExecStartPre=/bin/sh -c '…'` adds an `ExecStartPre` to the generated unit. That crosses a privilege boundary rather than merely producing bad output: a package author already controls the image and the command, which is authority over what runs *inside a container*, while a systemd directive runs on the **host, as root**, before podman is invoked at all.

`packages.ValidateNoControlChars` rejects every C0 control and DEL. **Tab is the sole exception** — it is legitimate whitespace, and systemd's tokenizer treats it as a separator that quoting genuinely does contain.

The check runs **twice, and both passes are load-bearing**:

- `InputPackage.Validate()` covers the author's literals in `environment`, `command`, and `entrypoint`. It runs at the *top* of `Compile`, so it only ever sees pre-substitution text.
- A sweep over the **compiled** package at the end of `Compile` covers everything after substitution: environment values, command, entrypoint, volume mountpoints, and `post_update`. This is the pass that matters. A value that is a bare `@marker@` in the YAML carries no control character of its own and passes `Validate()`; the newline arrives with the *response*. A question declaring no `type:` is validated by nothing else at all, which makes the response path the one that actually reaches a unit file with caller-chosen bytes.

`systemd.quoteCommandArg` strips the same characters as a backstop, because unit generation has no error return and is the last point before the bytes are written to `/etc/systemd/system`. It **drops** rather than escapes: systemd does resolve C-style escapes inside quotes, but resting a security boundary on a parser detail buys nothing when there is no legitimate reason to deliver the byte at all.

Nothing that previously worked is refused. A multi-line value already produced a broken unit; the change is that it now fails loudly at compile time instead of silently generating a unit nobody inspected.

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

The template data context provides four namespaces:

- `.Responses.key` -- question response values (keyed by question name).
- `.Package.Name`, `.Package.Version`, `.Package.Repo`, `.Package.Image`, `.Package.Description` -- package metadata.
- `.System.Hostname`, `.System.ExternalIP`, `.System.InternalIP` -- system-level information.
- `.Dep.KEY.Host` and `.Dep.KEY.Ports` -- installed-dependency runtime coordinates, keyed by the same dep key the parent YAML declares under `dependencies:`. `Host` is the podman container name (resolvable via podman DNS on the shared network); `Ports` is `map[string]string` keyed by both the numeric container port (e.g. `"5432"`) and any semantic name declared on the dep's network entry (lowercased, e.g. `"sql"`). Access a named port with `{{index .Dep.db.Ports "sql"}}`. The map is nil for packages with no deps; `{{.Dep.db.Host}}` on an absent dep renders `<no value>` (like any other missing map key) and `index` on nil `Ports` deliberately errors so misconfigured templates fail loudly.

The `volume` and `path` fields support `@variable@` substitution (the same mechanism used by environment, network, and volume fields). The `content` field uses Go `text/template` syntax with `{{.Responses.key}}`, `{{.Package.Name}}`, `{{.Dep.KEY.Host}}`, etc. The `@dep_*@` marker form is NOT honoured inside `content` — use the Go-template `.Dep` namespace instead; `@dep_*@` remains the right form in `environment:` values and in dep `responses:` blocks.

Templates are applied after volume seeding (archives, git clones) **and after any dependencies install**, so `.Dep` is populated by the time the parent's content renders. During reconcile, templates are re-rendered but existing files are never overwritten, preserving data from archive uploads or previous runs; the dep map is rebuilt from persisted dependency records so `.Dep` still resolves when reconcile actually writes a missing template.

Validation enforces: template names follow the volume naming convention (alphanumeric with dots, dashes, and underscores), paths must be relative with no directory traversal, the volume must reference a defined package volume (unless the volume field contains template variables), and content must parse as valid Go `text/template`.

### Image Normalization

Container image references are normalized during compilation:
- Single name (`nginx`) becomes `docker.io/library/nginx:latest`.
- Two components (`user/app`) becomes `docker.io/user/app:latest`.
- Full references are preserved; `:latest` is appended if no tag is present.

### Response Persistence

Responses are saved per version at `responses/<repo>/<pkg>/<version>.json`. A `last` copy is saved at `responses/last/<repo>/<pkg>.json` for reuse during upgrades and reinstallation from uninstalled volumes. Last responses are cleared after a successful install.

Two API endpoints manage last responses:

- `POST /packages/last-responses` (admin required) -- retrieve cached last responses for a package (by repo and name).
- `POST /packages/clear-last-responses` (admin required) -- delete the cached last responses file.

### Installation Questions UI

When a user installs a package, the questions dialog loads existing responses (from a current install) and, if none exist, cached last responses (from a previous uninstall). Current responses take precedence over last responses.

**Cached responses** are displayed as read-only styled containers with a muted background, showing the saved value (passwords display as `********`). A hidden form input preserves the value for submission. Each cached field has a clear button (X icon) with a tooltip ("Clear to enter a new value") that, when clicked, replaces the read-only display with an editable input. The clear button uses a ghost style that turns red on hover.

**Defaults** are shown in two ways when no cached value is present: as placeholder text in the input (e.g., "Default: 8080") and as helper text below the input in muted text with the value in monospace. Type-specific placeholders are shown when no default is defined: "Auto-assigned if empty" for ports, "Auto-generated if empty" for hostnames, and "e.g. 30s, 5m, 2h, 1d" for durations.

**Validation errors** from the server are displayed per-field as red text below the input, and the input receives a red border.

**Sizing and pagination.** The dialog is capped at the viewport height (minus margins) and laid out as a flex column, so the header and footer stay put while the questions area scrolls — the base `DialogContent`'s `overflow-hidden` otherwise made the spillover of a many-question package unreachable. Questions are paged **5 per page** with Previous/Next controls that give way to the Install button on the last page. Every page stays mounted (inactive ones are `display:none`) so the uncontrolled form inputs keep their typed values and still submit; unmounting a page would silently drop the answers on it. A field error jumps to the page that carries it, so a validation error is never hidden behind the pager. The pager reuses the existing `datatable.next`/`previous` strings and a numeric page counter, so it adds no translation keys.

**Conditional questions** declared with `show_if` are hidden until their controlling checkbox is checked (see [Conditional questions](#conditional-questions-show_if)).

**OAuth questions** render from a single per-question status — `idle`, `starting`, `waiting`, `connected`, `error` — seeded from the cached response, not from "does a token exist anywhere". A token cached from a previous install used to make the field read as connected before anything had happened and keep it reading that way through a failed reconnect, putting a green Connected badge above a red error. The token is now read for exactly one decision (Connect versus Reconnect) and is otherwise only what the hidden input submits: a failed reconnect leaves the operator the token they already had, but nothing claims the failed attempt worked, a reconnect still in flight does not read as connected, and an approval that carries no token is an error rather than a silent success that would install an empty credential.

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

**A non-admin gets the notes and nothing else.** The route stays `requireAuth` because the dashboard renders every installed service's notes for every account — that is what notes are for — but a `type: secret` question is answered with a generated credential and a `type: oauth` one with a vendor token, so returning the full response map to anyone with a login would hand them every package's credentials. The questions are withheld too: a question's `query` is harmless, but pairing it with a redacted response map only advertises what is being kept back, and the one screen that renders questions is the admin-only install dialog. Dropping the map is not sufficient on its own — a note is compiled from those same answers, so `redactSecretsInNotes` masks any secret or oauth answer a note quoted, matching by value so a note that never quotes one is left completely intact. Answers shorter than six characters are left alone: a two-character secret is not a credential anybody chose, and masking every occurrence of it would shred unrelated note text.

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
- `POST /packages/responses` (admin required) -- get saved responses for an installed package by repo, name, and version.
- `POST /packages/purge-volumes` (admin required) -- permanently delete volumes for an installed package.

### Package Enable/Disable

- `POST /packages/disable` (admin required) -- disable a package. Sets the disabled flag and stops all associated systemd services.
- `POST /packages/enable` (admin required) -- re-enable a disabled package. Clears the disabled flag and starts all associated systemd services.

The `Installer` interface supports `SetDisabled`, `IsDisabled`, and `IsPackageChanged` in addition to the core `Install`, `Uninstall`, `ListInstalled`, and `GetResponses` methods.

### Uninstalled Volume Management

- `POST /packages/purge-uninstalled-volumes` (admin required) -- permanently delete all uninstalled volumes for a package.

## Storage

Storage uses btrfs subvolumes with quota enforcement.

### Separation of Concerns: Volumes vs. Object Storage

**The storage layer manages volumes. gfeh provides object storage. The storage layer does not handle object storage at all -- gfeh is the responsible party.**

`src/storage` creates, resizes, renames, snapshots, and deletes btrfs subvolumes, and reports disk usage. That is its entire remit. It must never learn what an object, a bucket, a key, a file handle, a content identifier (CID), an ACL, a share, or a protocol view is. To the storage layer a subvolume is an opaque byte arena with a quota.

gfeh (`gitea.com/town-os/gfeh`, a Rust system service shipped as `town-os-system--gfeh`) owns everything above that line: the object namespace, per-file metadata and permissions, the hierarchical user/ACL database, sharing, per-file HTTP exposure, federation to external services, and every protocol view (S3, IPFS, Google Drive, plain HTTP; SMB/CIFS exists in gfehd but [Town OS does not serve it](#no-smb-view)). It consumes the storage layer purely to provision and resize the subvolumes its partitions live in, then does its own direct I/O on the bind-mounted subtree.

Consequences to respect when changing either side:

- Do **not** add object, blob, key/value, or per-file endpoints to `src/storage` or the `/storage/*` API. If a feature needs to address individual files, it belongs in gfeh. The existing `upload-archive`/`download-archive` endpoints are a tar transport for volume seeding, not an object API, and must not grow in that direction.
- Do **not** teach `storage.Storage` or `storage.Controller` about users, permissions, or protocols. Quota is the only policy the storage layer enforces.
- gfeh partitions live under the reserved `gfeh/` subvolume prefix. They are provisioned through `storage.Storage`'s `CreateFilesystem`/`ModifyFilesystem` **in-process**, not through the `/storage/*` HTTP API: `createFilesystem` rewrites every submitted name to `user/<name>` unconditionally (`controller_storage.go`), so that route cannot produce a volume under any other prefix. Partition provisioning therefore needs its own `/gfeh/partitions/*` handlers, which also keeps reserved-prefix enforcement, quota policy, and the audit log in one place instead of duplicating them in gfeh.

- **gfeh depends on a written contract, and changes here can break it.** `TOWNOS_CONTRACT.md` in the gfeh repository lists every route, behavior, and invariant gfeh relies on from Town OS -- the `user/` rewrite, the reserved-prefix rules, the `/gfeh/partitions/*` status codes, indistinguishable auth failures, and the fail-closed meaning of an empty `Account.Networks` -- and pins the Town OS revision it was verified against. gfeh emulates that contract so its tests can run without root, systemd, podman, or btrfs.

  **When changing `src/storage`, `src/account`, or the system controller's routes, re-run `make check-townos-sync` in the gfeh checkout.** A drifted emulator gives gfeh a green test suite and a broken deployment. Reconcile the emulator and the contract document together; never one without the other.

The Town OS side of that integration — the partition routes, the per-network daemons, the admin socket, and how names reach DNS and the ingress — is [Object Storage (gfeh)](#object-storage-gfeh).

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
- `POST /storage/remove-package-volume-group` (admin required) -- cascading delete behind the storage tree's non-leaf delete buttons. `repo` and `name` are required; an empty `version` targets every installed version of the package. **Every systemd unit in the target package's dependency tree is stopped before any subvolume is removed**, so a podman container still holding a volume open cannot race the btrfs delete. `include_uninstalled` additionally sweeps the matching `uninstalled/` subtree (wired to the same "Show uninstalled" toggle that drives the volume listing).
- `POST /storage/upload-archive` (admin required) -- upload and unpack an archive into a volume.
- `POST /storage/download-archive` (admin required) -- download a volume as a compressed archive.

### Volume Namespacing

- **User volumes** -- `user/<name>` on disk. The `user/` prefix is prepended transparently by the create, remove, modify, and list handlers, and stripped in API responses so the API consumer sees only the bare name. The `user` root subvolume is created on boot by reconcile.
- **Installed package volumes** -- `installed/<repo>/<name>/<version>/<volname>`.
- **Uninstalled package volumes** -- `uninstalled/<repo>/<name>/<version>/<volname>`.
- **Archive storage** -- `archives/` prefix (system-managed).
- **VM images** -- `vm-images/` subvolume (system-managed). Stores cached raw VM disk images.
- **Object-storage partitions** -- `gfeh/<network>`, one per Town OS network, owned by uid/gid 2000. Reserved: `/storage/create` cannot produce one (it rewrites every name to `user/<name>`), so they are provisioned through [`/gfeh/partitions/*`](#protocol-1-partition-provisioning-gfehpartitions).

All prefix root names (`installed`, `uninstalled`, `archives`, `pages`, `vm-images`, `user`, `gfeh`) are reserved and cannot be directly created, modified, or deleted by users. Archive upload and download resolve subvolume names that lack an internal prefix by prepending `user/`.

**A prefix is not a boundary unless the name after it cannot climb back out.** `filepath.Join` collapses `..`, so `../gfeh/home` submitted to a handler that prepends `user/` becomes `user/../gfeh/home` and addresses another network's object-storage partition — and it slips past the reserved-name check too, which matches on a leading prefix the traversal does not carry yet. `storage.ValidateFilesystemName` (no leading slash, no null bytes, no empty or `.`/`..` components, and a restricted character set) is therefore applied to **both** names in `ModifyFilesystem` — validating only the rename target let a caller move somebody else's subvolume into their own namespace — and to `RemoveFilesystem`, which validated nothing at all and is the destructive one. The `/storage/*` handlers validate a submitted name **before** prepending `user/`, which is what makes the reserved-name check mean what it reads as. These routes are `requireAuth`, not `requireAdmin`, so this was reachable by any ordinary account on the box.

The **list** prefix is deliberately exempt: `nest/` is how a caller asks for everything under `nest`, nothing joins it into a filesystem path (the storage layer lists from its own base and uses it as a string filter), and `user/` is prepended unconditionally, so a traversing prefix matches nothing rather than reaching anything.

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

Each page site has: a unique name (primary key), source type (`archive`, `container_image`, or `git`; default: `archive`), repository URL (required for git), branch (defaults to `main`), container image reference (required for container_image), image directory (required for container_image), domain (defaults to the page name), status (`pending`, `active`, or `error`), a **network**, and creation/update timestamps. Pages are stored in a SQLite table.

`Network` is the page's publishing network, exactly like a package's install network: it selects the TLD the page's hostname, leaf SAN, DANE TLSA owner, and ingress vhost are all named under, and it decides who can resolve the page. Empty — the zero value and the DB default — means the default/home network, the same convention as `Installer.LoadNetwork` for packages. See [Pages are network-scoped too](#pages-are-network-scoped-too). It is accepted on create and is one of the partial-update fields.

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

## Object Storage (gfeh)

gfeh is the object-storage half of the split described in [Separation of Concerns](#separation-of-concerns-volumes-vs-object-storage): `src/storage` owns btrfs subvolumes and quotas, gfeh owns objects, per-file permissions, the user/ACL forest, sharing, and every protocol view. This section is the Town OS side of that boundary — how the daemons are deployed, and every protocol crossing it.

`gfehd` is a Rust binary published to crates.io and packaged here as `quay.io/town/gfeh` (`Containerfile.gfeh`), because gfeh's own repository ships no image. It is **one process per partition**, not a single multi-tenant daemon.

### Deployment shape: one partition per network

A **partition** is one btrfs subvolume, one `gfehd` process, one admin socket, and **its own set of users**. There is exactly one per Town OS network, so the object-storage namespace is partitioned by the same boundary that partitions DNS and WireGuard: a principal, a grant, and an exposure in the `office` partition mean nothing in `home`.

| Thing | Location |
|---|---|
| Partition data | `<btrfsBase>/gfeh/<network>` → container `/data/<network>` |
| Config | `<btrfsBase>/gfeh-control/<network>/gfehd.yaml` → `/etc/gfeh/gfehd.yaml` |
| Admin socket | `<btrfsBase>/gfeh-control/<network>/run/admin.sock` → `/run/gfeh/admin.sock` |
| Unit | `town-os-system--gfeh-<network>.service` |

Path helpers live in `src/gfeh/layout.go` — `PartitionVolume`, `ConfigPath`, `SocketPath`, `ServiceKey`, `NetworkFromKey` — and are the only place these strings are composed.

The socket sits on the btrfs because that is the one filesystem both the gfehd container and the systemcontroller container can see; this is the same trick `ingressctl` uses for its gRPC socket. gfehd runs as **uid/gid 2000** (`gfeh.UID`/`gfeh.GID`), and a bind mount passes host ownership straight through, so the partition subvolume is chowned to that uid at creation — which is why `storage.Filesystem` carries optional `UID`/`GID` and `storage.Controller` has `Chown`. Non-recursive, for the same reason `HostVolumeMount`'s chown is: the daemon creates its own children as its own uid, so they are correctly owned already and never drift.

**Ports.** The four HTTP views bind **fixed, identical container ports on every partition** — s3 9000, http 9001, drive 9002, ipfs 9003 — and publish **no host port**. That is safe precisely because they publish none: each container has its own netns and the ingress reaches it by container name, exactly as it reaches a package. Two partitions both serving S3 on 9000 cannot collide, including under a concurrent `make test-full`.

**No partition publishes a host port at all**, because SMB — the one view that would need one, being neither HTTP nor able to sit behind the ingress — is [not served](#no-smb-view). `DefaultSMBPortBase` (`4450`) and `GFEH_SMB_PORT_BASE` survive unused, so the harness setting stays harmless if the view ever returns.

### Protocol 1: partition provisioning (`/gfeh/partitions/*`)

These four routes exist because `createFilesystem` rewrites every submitted name to `user/<name>` unconditionally, so `/storage/create` **cannot** produce a volume under the `gfeh/` prefix. They are declared in `TOWNOS_CONTRACT.md` and gfeh's Rust client parses these exact shapes, so **a change here is a contract change, not a refactor**. `make check-townos-sync` in the gfeh checkout is what catches drift; `controller_gfeh_partitions_test.go` pins the wire shapes on this side.

| Route | Auth | Request | Response |
|---|---|---|---|
| `POST /gfeh/partitions/create` | admin | `{name, quota}`, name **without** prefix | `Filesystem` `{name:"gfeh/<n>", quota}` |
| `POST /gfeh/partitions/modify` | admin | `{name, quota}` | `Filesystem` |
| `POST /gfeh/partitions/remove` | admin | `{name}` | 200, empty |
| `POST /gfeh/partitions` | auth | no body | **plain JSON array** of `Filesystem` |

Two details are load-bearing:

- **The list returns a bare array, not a `PageResult`.** Every other Town OS list endpoint paginates; this one cannot, because gfeh's `list_partitions()` deserializes `Vec<Filesystem>` directly and a paginated envelope fails to decode on the Rust side.
- **The prefix is asymmetric.** Requests carry a bare name, responses carry `gfeh/<name>`. The prefix is Town OS's namespace artifact, not part of the partition's identity; gfeh's `Partition::from_volume` strips it coming back.

Status codes gfeh's client branches on: **409** already exists (its provisioning is a create-or-resize and distinguishes the two by this status — a daemon whose partition exists on every boot but the first would otherwise only ever start once), **404** missing, **400** bad name, **403** non-admin. A name containing a path separator is refused at this boundary because gfehd refuses it at its own; disagreeing about what a legal partition name is would let `../user/something` address a volume outside the object-storage root.

Handlers call `storage.Storage` in-process, never `/storage/*`, so reserved-prefix enforcement, quota policy, and the audit log stay in one place. These routes are **not** in `grantRoutes` — provisioning the root of a permission tree is not something a grant buys, so a grant-holding account is refused them by the global allowlist before any handler runs.

### Protocol 2: the admin socket (`/v1/*`)

Each daemon's administrative surface is JSON-over-HTTP on its **Unix socket only** — never a port. There is no token and no authentication: the filesystem permissions on the socket are the access control, so reaching it already means being root on the box. `src/gfeh/client.go` (`UnixClient`) is the Go side; it pins `DialContext` to the socket and uses a fake `http://gfeh` authority.

| Call | Method + path | Purpose |
|---|---|---|
| `Health` | `GET /v1/health` | liveness; also the readiness probe |
| `Names` | `GET /v1/names` | the names this partition wants published |
| `ListPrincipals` / `CreatePrincipal` / `DeletePrincipal` | `GET`/`POST` `/v1/principals`, `DELETE /v1/principals/<name>` | the partition's user forest |
| `ListGrants` / `CreateGrant` / `RevokeGrant` | `GET /v1/grants?principal=`, `POST /v1/grants`, `DELETE /v1/grants/<id>` | ACLs |
| `ListExposures` / `WithdrawExposure` | `GET /v1/exposures`, `DELETE /v1/exposures/<token>` | published `/f/<token>` links |

gfehd maps its internal errors onto HTTP status (404/409/400) and `StatusError.Unwrap` maps those back onto Go sentinels so `errors.Is` works.

Adding a user is `POST /v1/principals {name, parent, ceiling}` — **no password**, which is why the UI never asks for one. The ceiling follows gfeh's projection rule: `all` for a Town OS admin, read/write otherwise. A grant is clamped to the principal's ceiling by gfehd, so the UI shows the perms that came *back*, not the ones sent: an administrator has to be able to see that a grant was narrowed.

### Protocol 3: names — gfeh answers, Town OS composes

**gfeh never registers a DNS record or an ingress route.** `RebuildDNS` calls `TeardownTLD` and `RebuildIngress` calls `SetRoutes` with the full derived set — both destroy foreign state — so anything gfeh registered directly would survive exactly until the next reconcile. Instead `GET /v1/names` returns **labels** (`s3.<partition>`) with a view and a port, and Town OS composes the zone. The names are therefore *asked for* on every rebuild rather than pushed once.

`gfehFQDN(label, tld)` (`gfeh_tls.go`) qualifies a label under the network's TLD and is the single string that the A record, the leaf SAN, the DANE TLSA owner, and the ingress vhost must all agree on — the same invariant `packageFQDN` and `pageFQDN` exist to hold. It **always** qualifies: it does not consult `isPublicFQDN`, because every gfeh label already contains a dot (`s3.gfeh`) and that predicate reads any such name as a public FQDN, which would leave every name unqualified and request an ACME certificate for a domain nobody owns.

**It is also the chokepoint where a label stops being a string on a wire and becomes a vhost, a DNS record, and a filesystem path**, so `gfeh.ValidateLabel` is applied there and nowhere else. An ingress vhost is written as `https://<hostname> {` with no quoting, so a label carrying a newline and a brace closes that block and opens another — and Caddy does not reject one bad vhost, it refuses the whole config and takes every name on the box down with it. A label that does not validate yields the empty string, and every caller already drops an empty FQDN, so a malformed name contributes no record, no route, no certificate, and no directory rather than a broken one. Length (`gfeh.NameMaxLen`) is checked on the **composed** name rather than the label alone: a label inside the limit can still qualify past it under a long TLD, and a name DNS will not carry is one the certificate and the vhost should not claim either.

Publication matches packages and pages exactly:

- **Dual-homed DNS** — a non-default network's partition gets a scoped A record at the box's overlay IP (served to that network's WireGuard peers) *and* a global A record at the LAN IP, via the fold-ins in `RebuildDNS` and `RebuildNetworkDNS`. DANE TLSA is pinned on both halves.
- **TLS** — a local-CA leaf per name, carrying the box's overlay IP on that network as a SAN so a peer can dial by raw WireGuard address.
- **Ingress** — one vhost per HTTP view, backend `<container>:<port>` on the shared `town-os-ingress` podman network. `dedupeIngressRoutes` guards the route set first-wins, because Caddy rejects an entire config over one duplicate vhost.

`IsHTTPView` gates that last step, and an **unknown** view is treated as not-HTTP: a vhost for something that does not speak HTTP accepts a TLS handshake and then fails, which is worse than no route at all. (A non-HTTP view would contribute a DNS record and no ingress route; today all four served views are HTTP.)

### The partition index

Every view gfeh serves answers a **protocol**, and none of them answers a
browser: the HTTP view has exactly one route, `/f/{token}`, so its root is a
404; S3 returns an XML error to anything it cannot parse as an operation; Drive
and IPFS answer their own APIs. So the one thing anybody does with a new name —
open it — reported that object storage was broken, when in fact there had never
been anywhere to look.

Each partition publishes a static index at **`gfeh.<tld>`** — `gfeh.IndexLabel`,
which is `VolumePrefix` rather than the string `"gfeh"` written a second time,
because the index must land on the parent of the view labels it indexes. There
is no new name to learn: the views are already `s3.gfeh`, `http.gfeh`,
`drive.gfeh`, `ipfs.gfeh`.

- **It is contributed by `collectGfehSites` as an ordinary `GfehSite`**, and that is the point: it inherits the A and AAAA records, the scoped overlay record, the DANE pin, the leaf SAN, and the ingress route from the same code that derives all six for the views, so the vhost and the certificate cannot be composed from different strings. It is added only when the partition has at least one view the ingress fronts — an index for a partition serving nothing browsable would be a name, a certificate, and a route, all to render a page saying there is nothing to see.
- **It is served by the pages container, not by gfehd.** Static HTML needs no server of its own, and emitting it inline as a Caddy `respond` body would put generated markup inside the config file, where one escaping mistake makes Caddy reject everything.
- **Content lives under its own `gfeh-index/` root**, a sibling of `gfeh/` for the same reason `gfeh-control/` is: everything under `pages/` is a page, owned by a row and swept by the pages reconcile. The webroot is the one thing the two share, because it is what the container serves from. `ViewIndex` is deliberately **not** in `HTTPViews`, so `IsHTTPView` does not accept it — that predicate answers "is this a view gfehd reported that the ingress can front", and the index is neither reported by gfehd nor served by it.
- **`pruneStalePageSymlinks` folds in `gfehIndexHostnames`.** An index is not a page, so without this the first `reconcilePages` deletes every index link — and a box with object storage and no pages hits the most aggressive case of that on every pass. The valid set is derived from the **network set alone**, never by asking the daemons, so a partition that is merely slow to start cannot have its own index pruned: what may be deleted has to be decidable from state Town OS owns.
- **Indexes are rendered by `reconcileGfehIndexes`, from `RebuildIngress`**, not from `ReconcileGfeh`. That placement is load-bearing: the ingress rebuild runs on boot, the hourly reconcile, package and page CRUD, and critically `publishGfehNames` — the first pass on a cold boot at which any daemon is answering at all, since gfehd polls `/status/ping`, which is 503 until the handler swap. An index written from the gfeh reconcile would be written before the daemons could say what they serve, and would sit stale until the next hour.

The index carries **only the views**, which are already in DNS. Not exposures,
principals, grants, or quota: it is served with no authentication in front of
it, and every published `/f/<token>` link is a bearer credential — precisely
the thing an unauthenticated page must never enumerate.

### Protocol 4: the UI proxy (`/gfeh/*`)

The admin socket is unauthenticated and not network-reachable, so Town OS proxies it. These are deliberately **separate from the four contract routes** so `check-townos-sync` keeps matching exactly what the contract declares.

| Route | Auth |
|---|---|
| `GET /gfeh` | auth — partitions with network, TLD, quota, unit status, and `/v1/names` output |
| `GET /gfeh/principals?network=` | auth |
| `POST /gfeh/principals/add` / `remove` | `requireObjectStorage` (admin or the `gfeh` grant) |
| `GET /gfeh/grants?network=&principal=` | auth |
| `POST /gfeh/grants/add` / `revoke` | `requireObjectStorage` |
| `GET /gfeh/exposures?network=` | auth |
| `POST /gfeh/exposures/withdraw` | `requireObjectStorage` |

The four `GET`s are audit-excluded; the five mutators carry audit keys. With no partitions configured, `GET /gfeh` reports that object storage is not configured rather than erroring.

**Every one of these — reads included — is confined by `requireNetworkScope` to the caller's networks**, because "which network" lives in the body or query that only the handler has parsed. A scoped account listing another network's principals or published links would be exactly the leak the scope exists to prevent, and reads are `requireAuth`, so nothing upstream would have stopped it. `GET /gfeh` names no network (it enumerates them), so it filters rows instead — on the same `Restricted()` predicate, since filtering a plain account against its empty scope would render every partition invisible to every ordinary account rather than confining anybody.

**The order inside `gfehClientFor` is load-bearing: shape, then authority, then existence.** An empty network is a 400 for everybody (a typo is not a permissions problem); an out-of-scope network is 403 *before* any partition lookup; only then does a missing registry earn its 503 and an unknown network its 404. With the lookup first, a caller who had no business asking learned whether that partition existed and whether its daemon was up, and got it as a *successful* refusal of a different kind — so nothing recorded that a scoped account had reached outside its scope.

### No service accounts

An earlier release created a dedicated administrator account, `gfeh`, whose password was stored at the `gfeh_service_password` setting, so the daemon could authenticate to the control plane. **That is gone.** Town OS provisions each partition's subvolume and quota itself before the daemon starts and creates principals over the admin socket, so the credential paid for nothing — while costing an *enabled administrator account nobody created*, sitting in every box's users list with enough privilege to uninstall everything, and forcing every "does this box have an admin?" question to mean "a *human* admin".

`hasEnabledAdmin` (`src/svc/systemcontroller/admin_presence.go`) is now the plain question, shared by the setup flag in `/status/ping` and the bootstrap branch of `POST /account/create` so the two can never disagree — a box where one says "set up" and the other does not is a box nobody can get into.

`account.PurgeLegacyServiceAccounts` deletes the row and the stored password on the first boot after an upgrade, reporting whether it removed anything so the box says so once rather than logging every boot. It is deliberately raw SQL: `Manager` has no `Delete`, and an account-deletion capability is not something to introduce as a side effect of a cleanup.

What remains in `gfehd.yaml` is `credentials:` and `drive.tokens:` — **end users authenticating to gfeh's views**, never Town OS logins. The `town_os:` block still exists in the config schema (gfehd's YAML is mirrored exactly) but Town OS renders no account into it.

### No SMB view

SMB is **not served**. It is the one view that cannot sit behind the ingress and the one needing a credential of its own: an NT hash (`MD4(UTF16LE(password))`), which cannot be derived from the stored password hash, so every user who wanted a share had to carry a second password. Town OS accounts do not have one, so there is nobody gfehd could authenticate — and an unauthenticated share on the LAN is not the fallback to take.

Consequences: no partition declares an `smb:` block, no host port is allocated for it (`SMBPortBase` is kept only so the harness's `GFEH_SMB_PORT_BASE` stays wired), `Account.SMBNTHash` and `src/account/smb_credential.go` are gone, and the `smb_nt_hash` column is dropped by `migrateLegacyAccountColumns` — an NT hash is unsalted, work-factor-free and password-equivalent to anything still speaking NTLM, so leaving it at rest for a view nobody serves is the worst of both. The other four views are unaffected.

### Config file

`src/gfeh/config.go` mirrors gfehd's YAML **exactly**. Every gfehd config struct is `#[serde(deny_unknown_fields)]`, so a stray key is not ignored — it is a hard startup failure. Top level: `data_dir`, `partition`, `network` (a **pointer**: absent means the default partition, and an empty string is a different, invalid request), `admin_socket`, the five optional view blocks, `credentials`, and `town_os`. Town OS renders four of the five views and neither an `smb:` block nor a `town_os:` account. Written `0640` and group-readable by gfeh's gid under `<btrfsBase>/gfeh-control/<network>/`, since the daemon runs as uid 2000 and must read it.

### Boot and reconcile

`ReconcileGfeh` runs at boot **after the ingress and pages** and **before `Reconcile`** — the TLS CA and storage exist by then, and the names must be available to the `RebuildDNS`/`RebuildIngress` calls further down. It runs **a second time after `ReconcileNetworks`**, which is idempotent (an unchanged partition is left alone rather than bounced) and covers any network the reconcile brought into being. It is also called from `/networks/create`, `/networks/remove`, `/networks/enable`, and `/networks/disable`, so a network added at runtime gets a partition. Non-fatal throughout.

Per network it ensures the subvolume (with UID/GID), renders the config, and installs and restarts the unit **only when the rendered content changed** (the `ReadUnit`-diff idiom reconcile already uses). `pruneGfehPartitions` removes units for networks that no longer exist.

**The per-partition wait is gone, and its absence is load-bearing.** `reconcileGfehPartition` starts the unit and stops there; whether a daemon is answering is asked separately, by `GfehReadyNetworks` and by the name collectors, both of which already treat a silent partition as contributing nothing rather than as a failure. The wait used to sit inside the loop, once per partition — including every partition it did nothing for, since `ensureFirstUserPrincipal` returns on its first line for any network but home. On a context with a deadline that was not merely slow: the first daemon that never answered spent the whole remaining budget in `WaitForReady`, so every partition after it tried to `Start` on an expired context and `pruneGfehPartitions` never ran at all. One dead daemon took the rest of object storage down with it, in whatever order the network names happened to sort.

The one remaining wait is `seatGfehFounder`, at the very end of the reconcile: it waits for the **home** partition only, capped at `gfehFounderWaitBudget` (10s, overridable per-config for tests), and then seats the box's first account. Being last, overrunning it can only delay work that is already done; a daemon still cold-starting is seated by the next pass, which boot runs immediately after `ReconcileNetworks`. For the same reason `GfehReadyNetworks` gives each health probe its own budget via `context.WithoutCancel` rather than drawing on whatever the caller has left — a spent deadline would otherwise make every partition look dead at once. Cancellation is still honoured; that is a shutdown.

**Object storage has no on/off setting.** Storing files is what the box is for, so it runs the way DNS and the ingress run — as part of what Town OS is, not as a feature to be enabled. A switch bought only the chance to be found in the off position while somebody debugged where their files went; an administrator who wants the daemons down stops them from the services panel like any other system service. A stale `object_storage_enabled` row left in an upgraded box's settings table is read by nothing.

The remaining escape hatches are about a *build*, not policy: it is gated on the ingress (with `INGRESS_IMAGE` empty the four HTTP views are reachable by nothing, so starting partitions would publish names nothing serves), and `GFEH_IMAGE` explicitly empty skips object storage entirely (dev mode) — the same `LookupEnv` convention `UI_IMAGE` and `INGRESS_IMAGE` use, because `Getenv` would make an empty value mean "use the default" and leave no off switch.

**The first account is seated in the home partition.** `ensureFirstUserPrincipal` creates a principal named for the box's earliest-created account (by `CreatedAt`, username as tie-break, so the founder cannot change between reconciles on map iteration order), with `gfeh.CeilingForAccount(admin)`. A partition whose forest is empty serves nobody: the operator opens the Users tab, finds nothing, and has to work out that their own account is not in it. **Home only** — every box has that partition, whereas a network added later belongs to whoever is given a grant on it, and seating the founder there would hand them a namespace somebody else created. Idempotent by way of gfehd, which answers 409 for a principal that already exists.

**Names are published after the handler swap.** `publishGfehNames` runs in the background: gfehd polls `/status/ping`, which answers **503 until the full router is up** ([Boot Status](#boot-status-and-refresh)), so a partition cannot finish starting until boot is essentially done. Waiting for it inline would deadlock the boot it is waiting on. If no partition becomes ready in time the names are simply published by the next reconcile.

Partitions register in `collectSystemServices()`, so `POST /system-services/refresh` re-pulls and restarts them — the omission that made the ingress silently stale.

### Version coupling

**Town OS pins a gfehd floor, and it is a floor rather than a preference.** `Containerfile.gfeh` builds from crates.io at `GFEH_VERSION` (override, or `GFEH_LATEST` non-empty to take whatever crates.io holds today — the same shape as `TTYFORCE_LATEST` in the install repo). The current floor is **0.1.2**.

Neither failure is visible to `make test` — the unit and integration suites both stand in a **fake gfehd**, so pinning below the floor buys a green suite and a box where object storage is silently dead. Raise the pin when Town OS starts depending on new daemon behavior, and let the image build fail loudly if that version is not published yet.

### UI

`/dashboard/objects` (nav `nav.objects`, "Object Storage"). A network selector across the top, then `?tab=` sub-tabs, one file each under `ui/src/routes/objects/`: **Overview** (per-partition status, quota, and the published names with whether each is reached through the ingress or dialed directly), **Users** (principals and ceilings; add projects a Town OS account), **Grants**, and **Links** (exposures, with withdraw). Reads are `requireAuth`, so the tab is not admin-only; the mutating controls need admin or the `gfeh` grant, and either way only on the caller's networks.

Two details on that screen exist to stop a reader acting on a number or a token
that cannot be used:

- **The Overview's Port column is blank for an HTTP view.** The port gfehd reports for one is a *container-side backend port* the ingress proxies to, reachable from nowhere a reader sits — printing `9000` beside "Ingress (HTTPS)" invites dialling `s3.gfeh.home:9000` and concluding the feature is broken. SMB keeps its number, which would be a real host port.
- **The Links tab renders the complete URL, composed server-side.** `GfehExposureView.URL` is built from `gfehPublishedLinkBase` — `https://<http-view-fqdn>/f/` — which comes from the same collector that names the ingress vhost and the leaf's SAN, so a published link is by construction a name the ingress routes and the certificate covers. It is not composed in the browser because the UI would have to know four things the server already holds: that the serving name is the *http view's* rather than the partition's or the box's, that it is qualified under the partition's own network TLD rather than the global one, that the route is `/f/<token>`, and that the reported port must never appear. The field is empty when the partition serves no HTTP view — the honest answer, since nothing is then serving that token — and a disabled exposure renders as plain text rather than a clickable 404.

**This screen is the only place object storage is managed.** The services screen carries no object-storage section: a partition IS a system service — one `town-os-system--gfeh-<network>` unit each — so it is already a row in that screen's System Services table, `Object Storage (<network>)`, with the same status badge and the same start/stop/restart/logs actions as every other system service. A panel beside it repeated that row and polled independently of it, so one unit had two controls at two levels that could disagree; it also rendered unconditionally while the table was gated on its poll having returned, which put object storage alone at the top of the screen on first paint and dropped system services in above it a moment later. `?expand=objects` on the services screen opens System Services, where the row lives.

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

- `GET /systemd/units` (localhost or auth) -- list all package service units, flat. Returns unit status enriched with package identifier, package description, and network controller failure flag.
- `GET /systemd/units-tree` (localhost or auth) -- the same data grouped into a dependency tree: root packages at the top, deps nested under their parent, recursively (the shape mirrors `/storage/package-volumes`). Each node carries `repo`/`name`/`version` (raw effective name, which may contain `--dep--`) alongside the human-facing `package_identifier`, plus the same status fields as the flat endpoint, so a client needs no second fetch to enrich rows. **Search and pagination apply to root nodes only** — dependency descendants do not count against the page, so a tree always ships with its full subtree even at a page boundary.
- `POST /systemd/status` (admin required) -- change a service unit's status. Accepts unit name and action (start, stop, restart, enable, disable).
- `POST /systemd/status/tree` (admin required) -- apply an action across a root package's whole dependency tree. Accepts `repo`, `name` (raw effective name, so values from the install APIs feed back unchanged), `version`, and `action`. Only `start`, `stop`, and `restart` are allowed — `enable`/`disable` are rejected — and stopping the system controller's own unit is refused. **Traversal order depends on the action**: units are collected leaves-first (the natural order for start and restart) and the order is reversed for stop, so the root goes down before its descendants.

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

- `GET /systemd/logs` (localhost or admin) -- streams historical journal entries via Server-Sent Events. The `unit` query parameter selects the service; empty or `__system__` returns system-wide logs.
- `GET /systemd/logs/tail` (localhost or admin) -- returns a JSON page of journal entries. Supports parameters: `unit`, `lines` (default 100), `before`/`after` (cursor pagination), `grep` (case-insensitive search), `since`/`until` (Unix timestamps), and `priority` (syslog severity filter, 0 = no filter).
- `GET /systemd/logs/tree` and `GET /systemd/logs/tree/tail` (localhost or admin) -- the tree-scoped counterparts. Instead of a `unit`, they take `repo`, `name`, and `version` (all required) and cover **every** systemd unit in that package's dependency tree, so a parent's logs and its deps' logs interleave in one view. Replay and paging semantics otherwise match `/systemd/logs` and `/systemd/logs/tail`.

## Account Management

### Account Model

Each account has: username (primary key), password hash (never exposed in JSON), email, phone, real name, admin flag, disabled flag, a **grant set**, a network scope, and creation/update timestamps. Accounts are stored in a SQLite table.

There is **no account "kind"**. An account is an administrator (holds every grant, on every network) or it is not, and a non-admin carries whatever grants are toggled on. `Account.Restricted()` — a non-admin holding at least one grant — is derived, never stored.

**There are no service accounts.** An earlier release gave the object-storage daemon its own administrator account; it is gone, and `account.PurgeLegacyServiceAccounts` deletes it (and its stored password) on the first boot after the upgrade. See [No service accounts](#no-service-accounts).

### Validation Rules

- **Password** -- minimum 8 characters, and only printable ASCII (`0x21`--`0x7E`, no spaces). High-bit and control bytes are rejected at creation time (`ErrPasswordInvalidChars`) rather than trusting every layer on the path to bcrypt — HTTP Basic auth, JSON, URL encoding, the DB's latin1 columns — to round-trip them identically.
- **Email** -- standard email format (`user@domain.tld`).
- **Phone** -- digits with optional formatting (`+`, spaces, dashes, parentheses).
- **Contact info** -- email, phone, and real name are all required (non-empty).
- **Grants** -- every name must be in `account.AllGrants` (`ErrInvalidGrant`), an administrator may hold none explicitly (`ErrGrantsAdmin` — it holds them all already, so a stored subset could only disagree), and an account holding any must be scoped to at least one network (`ErrGrantsNoNetworks`).
- **Network scope** -- every entry must be a valid network name (`ErrInvalidNetworkName`). An empty list is never read as "any network".

### Grants

A **grant** is a named capability a non-admin account can hold. Two exist:

| Grant | Constant | Buys |
|---|---|---|
| `wireguard` | `account.GrantWireGuard` | enrolling and refreshing WireGuard peers on the account's networks |
| `gfeh` | `account.GrantGfeh` | administering the object storage those same networks own — principals, their grants, published links |

`account.AllGrants` is the registry: a grant absent from it cannot be stored, which is what stops a typo in an API request from becoming a permission that silently never matches anything. Adding a capability is one entry there plus its routes in `grantRoutes` — no new column, no new migration, no new `UpdateFields` pointer. The UI renders its checkboxes from the mirrored `ui/src/lib/grants.js`, so a new grant needs no new markup either.

The two are **independent**. Holding `wireguard` buys nothing in object storage and holding `gfeh` buys no peer enrollment; an account may hold both. `Account.HasGrant` answers "may this caller do this at all" and `Account.MayAdministerNetwork` answers "on which network" — never each other.

#### Enforcement is three layers, and the composition is the point

1. **`grantAllowlist`** is a *global*, fail-closed middleware. A route added tomorrow is denied to a restricted account by default until somebody lists it in `grantRoutes` (`src/svc/systemcontroller/controller_auth.go`), keyed by `"METHOD PATH"`. Requests with no valid token, from an administrator, or from an ordinary account holding no grants pass straight through to the route's own auth — a grant is *additive* authority for an account that exists to exercise it, and this confines only those.
2. **The route's own middleware** — `requirePeerEnroll` (the `wireguard` grant) and `requireObjectStorage` (the `gfeh` grant), both built from `requireGrant`, which admits administrators because they hold every grant. Reads stay `requireAuth`.
3. **`requireNetworkScope`**, inside the handler, because the network lives in the request body or query and only the handler has parsed it. It **confines**; it does not grant, and it confines `Restricted()` accounts only — an ordinary account holds no grants and therefore no scope, and an empty scope denies every network, so applying it to a plain account would 403 every read on routes that are `requireAuth` on purpose.

`grantRoutes` is the whole of what a grant buys:

```
wireguard: GET  /networks/peers   POST /networks/peers/add   POST /networks/peers/refresh
gfeh:      GET  /gfeh             GET  /gfeh/principals      POST /gfeh/principals/add
           POST /gfeh/principals/remove                      GET  /gfeh/grants
           POST /gfeh/grants/add  POST /gfeh/grants/revoke   GET  /gfeh/exposures
           POST /gfeh/exposures/withdraw
```

plus `grantCommonRoutes`, reachable by any grant-holder regardless of which grant: `POST /account/authenticate`, `GET /account/me`, `GET /networks`, `GET /dns/services`, `GET /tls/ca.crt`, and `GET /status/ping`. Without those a grant is unusable — you cannot exercise one without first signing in — so they are common rather than duplicated into every grant.

`GET /status/ping` is on that list for a second reason: it is **public**, registered with no auth middleware at all, so an anonymous stranger gets a 200. Because the allowlist is global and fail-closed, omitting it meant a valid token turned that 200 into a 403 — authenticating made a caller strictly worse off than presenting nothing. It is also the dashboard's 60-second session heartbeat and the source of its whole status surface, so an account holding `gfeh` could reach every `/gfeh` route and still not get a usable page. Granting `wireguard` as well never helped: ping is keyed to neither grant.

Note what is deliberately **absent**: `/gfeh/partitions/*` stays `requireAdmin` (provisioning a partition creates the root of a permission tree and allocates a btrfs subvolume; `TOWNOS_CONTRACT.md` reserves it for administrators and gfeh's client branches on the 403), and `GET /networks/peers/connected` aggregates every account's peers and observed source addresses across every network.

Unlike `Admin` — immutable after creation — grants are mutable, and `account.Manager.CreateGranted` is a separate method from `Create` so the invariants (a grant-holder is never an admin and always has a non-empty scope) are enforced in one place at creation time rather than assembled from a widened positional signature.

#### Migration from the old columns

Earlier releases carried a boolean column per capability. `legacyGrantColumns` (`src/account/sqlite.go`) maps each to the grant it becomes and `migrateLegacyAccountColumns` carries it over and drops the column:

| Legacy column | Becomes |
|---|---|
| `wireguard` | `wireguard` |
| `object_storage` | `gfeh` |
| `network_only` (an in-flight schema that folded both into one flag) | both |

**One column, one grant.** An account that could enroll peers still can, and one that could not does not silently gain it — widening authority during an upgrade is the direction you cannot take back, since the account keeps its password and nothing on screen says it grew. `smb_nt_hash` is dropped outright (see [No SMB view](#no-smb-view)).

### Every account belongs to the home network

`Manager.Create` — the path the **first** account and every ordinary account take — writes `networks: ["home"]`. `CreateGranted` does not merge it in: there, the scope an administrator chose is exactly the networks the account may reach, and folding `home` into it would widen a portal scoped to `office`.

This is safe because for a grantless account the scope is **membership, not confinement**: `Restricted()` is false, so no layer above consults it. And it can never name a network that is not there — see [The home network always exists](#the-home-network-always-exists).

### Account API

- `POST /account/create` -- create a new account. In bootstrap mode (no enabled admin account exists), unauthenticated access is allowed; otherwise admin authentication is required. A non-empty `grants` array routes to `CreateGranted` with the supplied `networks`; otherwise the account is created through `Create` and joins the home network. Duplicate username errors return a generic failure message to prevent user enumeration.
- `POST /account` -- get account by username (auth required).
- `GET /account` -- list all accounts with pagination and search (auth required).
- `POST /account/update` -- update account fields (auth required). The username being updated comes from the **body**, so editing anybody else's account is admin-only: without that check any authenticated account could POST `{"username":"admin","fields":{"password":"..."}}` and take the box over — the controller drives the host podman socket, so that is root. An ordinary account may still edit its own contact details and password, which is why the route is not `requireAdmin` outright. Admin status cannot be changed after account creation; grants and the network scope can, **by an administrator only, even on your own account** — otherwise a normal user could grant itself `gfeh` and walk into a partition, or `wireguard` and enroll a peer on the overlay. A nil `networks` leaves the stored scope untouched; a non-nil one replaces it wholesale. `validateGrantResult` checks the row's state *after* the update, so granting an administrator, promoting a grant-holder, and clearing the scope out from under a grant are all caught.
- `POST /account/disable` -- disable an account, preventing authentication (admin required). Also revokes the account's live sessions. That is not what makes disable take effect — `SessionManager.Validate` refuses a disabled account's token on its own, so the guarantee does not depend on the revocation having succeeded — it is what stops a token issued before the disable from working again if the account is later re-enabled, which is not what an administrator means by "enable" after having revoked someone's access.
- `POST /account/enable` -- re-enable a disabled account (admin required).

### Account Management UI

The users management screen (`/dashboard/users`) displays a paginated, sortable, searchable data table of accounts. Each row shows username, email, phone, real name, admin/user role badge, and enabled/disabled status. Actions per row include an Edit button (opens a dialog for updating password, email, phone, real name, the **grant checkboxes**, and the network-scope selector) and an Enable/Disable toggle with confirmation. A link navigates to a dedicated create user page (`/dashboard/users/create`) with a registration form carrying the same controls. Both forms render their checkboxes from `ui/src/lib/grants.js` and reject granting anything with no networks chosen.

### Session Management

Sessions use JWT tokens (HS256) with claims for session ID (UUID), username, and issued timestamp. The signing key is ephemeral: 32 random bytes generated via `crypto/rand` on every service start, never persisted to disk. When `InitSessionManager` runs at startup, all existing sessions are cleared (`DELETE FROM sessions`) since prior tokens are invalid with the new key. The `TOWN_OS_SIGNING_KEY` environment variable can override the generated key. Sessions expire after 7 days from last use. A background cleanup task periodically removes expired sessions.

**A disabled account's token is dead on arrival.** `Validate` checks `Disabled` and refuses, because every request after sign-in is authorized from that function alone: without the check, disabling an account only stopped it logging in *again*, while a token it already held stayed good for the full session lifetime and refreshed itself by its own use.

**No session manager means no service, not open service.** Every authorization decision on the box used to be derived from one nil: `requireAuth`, `requireAdmin`, `requireGrant`, `revokeSession`, `requireNetworkScope`, and `callerIsAdmin` each read `GetSessionManager() == nil` as "authentication is not configured, so let it through". That made *there is nobody to authenticate* and *everybody is authorized* the same state — the whole authorization surface one unset field away from serving `POST /account/create` and `POST /packages/install` to an anonymous caller, on a controller that drives the host podman socket as root, with nothing in the type system saying so and no error if it happened.

The condition is now **`ServerConfig.AuthDisabled`: stated, not inferred**. A missing session manager with auth enabled is a misconfiguration, and `NewHandler` returns `ErrAuthNotConfigured` rather than a handler — refusing at construction rather than per-request, because a box that boots and then answers 500 on every authenticated route is a confusing outage, while one that will not start says what is wrong once, in the journal, while it can still be fixed. The middleware refuses the same state too, so a handler set assembled by any other path is closed as well.

`InitTestServer` sets `AuthDisabled` when — and only when — the config installs no session manager. That is what keeps the ~230 test call sites that never build one working unchanged, while a test that *does* build one keeps its auth enforced; disabling it there would turn every authorization assertion in the suite into a tautology.

`callerIsAdmin` is the one place the answer changed rather than moved: it returns **false** for an unidentifiable caller, where it used to return true. Every route reaching it sits behind `requireAuth` or `requireAdmin`, both of which now refuse that state outright, so it is unreachable in practice — but a redaction helper is the wrong place to be generous on the strength of that.

The `SessionManager` interface provides: `Create`, `Validate`, `Revoke`, `RevokeAllForUser`, `Cleanup`, `List`, `GetUsername`, `HasActiveAdminSessions`, and `StartCleanup`.

Session API endpoints:

- `POST /account/authenticate` -- username/password login (public). Returns a JWT token and account object. Authentication failures (wrong password, nonexistent user, disabled account) all return the same generic "invalid credentials" error to prevent user enumeration.
- `GET /account/sessions` -- list the authenticated user's sessions (auth required).
- `GET /account/me` -- get the authenticated user's username (auth required).
- `POST /account/session/revoke` -- revoke a specific session by ID (auth required).

### Audit Logging

All administrative actions are recorded in an audit log. Each entry has: auto-increment ID, account (username), action description, request path, sanitized detail (credentials masked), success flag, error message, and creation timestamp.

**The sanitizer masks rather than deletes**, replacing a credential value with `[REDACTED]` and leaving the key. An audit reader should be able to see that a field was present and withheld, not be unable to tell it from a request that never carried one. It matches `auditRedactedKeys` case-insensitively against the whole key and against the key's suffix after the last underscore, so `smtp_password` is caught without a substring rule that would also swallow innocuous names, and it recurses into arrays as well as maps. A package install's `responses` map is treated as **opaque** and masked whole: its keys belong to the package author, so there is no vocabulary to match on, and its values are exactly the generated `type: secret` and `type: oauth` answers the log must not become a copy of. A bare `key` is deliberately NOT on the list — the suffix rule would then catch `public_key`, which `POST /networks/peers/add` carries, and a WireGuard public key is public by construction while being the one field that says which device was enrolled.

Tracked actions include: create/modify/remove filesystem, add/remove/move/refresh repository, install/uninstall package, purge volumes, disable/enable package, set unit status, create/update/disable account, authenticate, revoke session, update setting, dismiss upgrades, upload/download archive, create/update/remove/rebuild page, upload/delete VM image.

Read-only endpoints are explicitly excluded from audit logging. Excluded paths include the root path (`/`), all GET list/query endpoints, info endpoints (`/packages/installed/info`), response retrieval (`/packages/last-responses`, `/packages/responses`), install preview (`/packages/install-preview`), version/question lookups, timezone listing, the pages list endpoint, status ping, system services listing (`/system-services`), audit log queries, settings reads, and log streaming endpoints.

- `POST /audit/log` (localhost or admin) -- query the audit log with cursor-based or offset pagination, account filtering, sorting, and search.

### Settings Management

Key-value settings are stored in SQLite. Default settings include `default_quota` (50 GB), `max_archive_size` (1 GB), `archive_unpack_timeout` (600 seconds), `locale` (en-US), `dns_tld` (home), `dns_resolution_mode` (auto), `dns_local_forwarders` (false), `peer_ttl` (7200 seconds), and `gfeh_partition_quota` (0). `proton_image` is registered only in `proton`-tagged builds. See [Settings](#settings) for the full table.

- `GET /settings` -- get all settings (admin required).
- `POST /settings/get` -- get a specific setting by key (admin required).
- `POST /settings/set` -- set a setting value (admin required, audit logged). Byte-value settings (`default_quota`, `max_archive_size`) accept human-readable strings (e.g., "500GB", "10MB") which are parsed and stored as numeric byte counts.

**Every account manager takes a context on each method, and `dbTimeout` is a ceiling rather than the whole story.** They used to open their own root context per query (`account.dbCtx`, now gone), which meant a caller's cancellation stopped at the manager boundary: an abandoned HTTP request kept working, and graceful shutdown could not interrupt a query. That matters more here than it would elsewhere because `OpenDB` sets `SetMaxOpenConns(1)` — SQLite permits one writer, so every query is serialized behind a single connection and one slow query holds every other caller behind an uninterruptible 30-second wait.

`account.queryCtx` derives from the caller instead: a caller with a shorter deadline keeps it, a caller with none still cannot hang forever, and a cancelled caller stops the query rather than leaving it to run out its own clock. A nil context is read as `context.Background()` rather than panicking — a manager is the wrong layer to take a box down over an argument its caller forgot, and tests that construct handlers directly leave the server context nil.

Handlers pass `c.Request().Context()`; background goroutines pass the server-scoped context, never a request's, since the operation must outlive the request that triggered it.

**`getLocale()` is the one deliberate exception**, using the server context rather than taking one. It is called from ~55 sites, almost all building an error message, and the request context would be the wrong bound anyway: the one case where it is already cancelled is a client that hung up, when the message is not going to be delivered either way.

All six are converted — `SettingsManager`, `AuditManager`, `PagesManager`, `NetworkManager`, `SessionManager`, and `Manager` — along with `OpenDB`, and `dbCtx` is gone.

Two methods take the **server** context rather than the caller's, and both are deliberate. `AuditManager.LogEntry` is called by `auditMiddleware` *after* the handler returns, to record what it did: passing the request context would let a client that hangs up mid-request cancel the write recording the request, so the least-recorded actions would be exactly the ones somebody disconnected during. `NetworkManager.ReapExpiredPeers` is the background peer sweep, whose partial completion leaves peers the live WireGuard device still carries.

`Manager.Authenticate` takes a context that bounds its two queries but **not** the argon2id hash between them — argon2 has no cancellation, and `loginGate` is what caps concurrent hashes at 64 MiB each.

### Settings UI

The system settings screen provides admin-configurable controls for all system-wide settings. Each setting is displayed in a bordered section with a heading, a description showing the current value in human-readable format, and a form with a numeric input, a unit selector, and a save button.

- **Default Volume Quota** -- configurable in GB, MB, or bytes. Displays "0 (no quota)" when set to zero.
- **Max Archive Size** -- configurable in GB, MB, or bytes. Controls the maximum file size allowed for archive uploads.
- **Archive Unpack Timeout** -- configurable in seconds, minutes, or hours. Controls the maximum time allowed for unpacking an uploaded archive.
- **Language** -- a dropdown showing common languages with native-script names. An expandable section reveals extended locales. Unpopulated locales are shown with an asterisk and disabled.
- **Proton Image** -- an editable text input for the Proton runner container image reference (e.g., `quay.io/town/proton:latest`).
- **Local DNS Forwarders** -- a switch backed by `dns_local_forwarders`. Below it, the addresses rolodex is *actually* forwarding to, read from `GET /dns/status` rather than inferred from the setting; when discovery found nothing usable the panel says the public forwarders are still in use, which is the one case where the switch reads as on and nothing changed. See [Local forwarders](#local-forwarders).

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
- `TOWNOS_DEP_{KEY}_PORT_{NAME}` -- emitted in addition to the numeric form when the dep declared a semantic port name in `network.external` / `network.internal` (see **Named Ports** below). The name is uppercased so `sql` in the dep becomes `TOWNOS_DEP_DB_PORT_SQL` on the parent. Both the numeric and named forms coexist and always carry the same value.

### Dependency Template Variables

In addition to the runtime environment variables above, dependency host and port values are also available as `@variable@` template markers during package compilation. This allows parent packages to reference dependencies in their `environment` field values at compile time, and also allows **sibling dependencies** to reference each other in the `dependencies.<key>.responses` block.

- `@dep_KEY_host@` -- resolves to the dependency's podman container name (resolvable via podman DNS on the shared network).
- `@dep_KEY_port_N@` -- resolves to numeric container port N for the dependency.
- `@dep_KEY_port_NAME@` -- resolves to the container port the dep tagged with the semantic name `NAME` (see **Named Ports** below). Lower-case in the template; matches the env-var suffix case-insensitively. Coexists with `@dep_KEY_port_N@` for the same port.

Template keys are derived from the `TOWNOS_DEP_*` runtime environment variable names by stripping the `TOWNOS_` prefix and lowercasing the remainder. For example, `TOWNOS_DEP_DB_HOST` becomes template key `dep_db_host`, and `TOWNOS_DEP_DB_PORT_5432` becomes `dep_db_port_5432`.

The `@dep_*@` form is honoured only where `@variable@` substitution already runs — `environment` values and dep `responses`. Inside file-template `content`, use the Go-template `.Dep` namespace instead (see **File Templates** above): `{{.Dep.KEY.Host}}` and `{{index .Dep.KEY.Ports "sql"}}` carry the same values. `.Dep` is populated from the same `TOWNOS_DEP_*` computation and surfaces every port under both its numeric key (`"5432"`) and its lowercased semantic name (`"sql"`) when one was declared.

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

### Dependency Shared Volumes

Packages in the same dependency tree can share btrfs subvolumes via two-sided opt-in. The dep author marks a volume `shareable: true`; the parent author then declares either an `expose:` block (mount the dep's volume into the parent's container) or a `consume:` block on a different dep (mount one sibling's volume into another sibling's container). Volumes without `shareable: true` cannot be cross-mounted — the install/reconcile pass rejects any reference to a non-shareable volume.

The wiring is a thin layer over the existing `HostVolumeMount` infrastructure: the install path resolves each `expose`/`consume` entry into a podman `-v <hostpath>:<containerpath>:<options>` flag pointing at the producer dep's btrfs subvolume on disk. Reconcile rebuilds the same flags on every boot from the parent's persisted YAML, and `installUnitIfChanged` content-diffing picks up changes automatically — no special restart hook.

**Dep-side opt-in.** A dep declares `shareable: true` per volume:

```yaml
# radarr/1.0.yaml
volumes:
  movies:
    mountpoint: /movies
    quota: "@moviesize@"
    shareable: true     # opt-in: parent or sibling may mount this
  config:
    mountpoint: /config  # not shareable; rejected if any parent tries to expose it
```

**Parent → dep (`expose:`).** A parent's `dependencies.<key>.expose:` map names dep volumes to bind-mount into the parent's container. Each entry takes a container path and an optional `readonly` flag (default `true`, since parents typically just consume dep output):

```yaml
# plex/1.0.yaml
dependencies:
  radarr:
    package: radarr
    expose:
      movies:                  # volume name in radarr's YAML
        path: /data/movies     # in-container path on Plex
        readonly: true
  sonarr:
    package: sonarr
    expose:
      tv:
        path: /data/tv
        readonly: true
```

**Sibling → sibling (`consume:`).** A `dependencies.<key>.consume:` list mounts one sibling dep's volume into THIS dep's container. Each entry takes a `from:` (sibling dep key in the same parent's `dependencies:` map), `volume:` (volume name in the sibling's YAML), `path:` (container path on the consuming dep), and optional `readonly` (default `false`, since sibling-to-sibling sharing typically needs writability — e.g. an *arr importing into a download client's `/downloads`):

```yaml
# media/1.0.yaml — parent that wires download client + arrs
dependencies:
  qbittorrent:
    package: qbittorrent
  radarr:
    package: radarr
    consume:
      - from: qbittorrent
        volume: downloads
        path: /downloads
  sonarr:
    package: sonarr
    consume:
      - from: qbittorrent
        volume: downloads
        path: /downloads
```

**Topological install order.** `consume.from` references add edges to the install-time DAG built by `orderDependencies` alongside the existing `@dep_KEY_*@` response references. A dep B that consumes from sibling A is installed strictly after A so A's btrfs subvolume already exists when B's container starts. Cycles among consume edges (A consumes from B; B consumes from A) are a hard error and abort the install before any dep is provisioned. Self-consume (`from:` equals this dep's own key) is rejected at validation time.

**Validation.** Compile-time validation rejects: relative or traversal-bearing mount paths, `consume.from` references to keys not declared in the same `dependencies:` map, self-consume, and duplicate consume paths within one dep. Cross-package validation (`shareable: true` on the producer's matching volume) happens at install/reconcile time when the producer's YAML is loaded — a parent that exposes or consumes a non-shareable volume fails install with `volume %q is not marked shareable on %s`.

**Template path substitution.** `expose.<volname>.path` and `consume[].path` participate in `@question@` substitution exactly like regular volume mountpoints. `consume.from` and `consume.volume` (and `expose` map keys) are identifiers, not data, and are not substituted.

**Permissions caveat — bind mounts pass UID/GID through.** A dep's btrfs subvolume on the host is owned by whatever uid:gid the dep's container created it as. If the dep runs as 1000:1000 (linuxserver/* default) and the consuming parent or sibling runs as a different uid, the consumer gets EACCES on read or write. The fix is in package YAML, not the platform: align `PUID`/`PGID` question defaults across packages that share volumes. The `HostVolumeMount.UID`/`GID` chown line is intentionally non-recursive and only applies when the dep author explicitly sets them on a writable mount; the shared-volume resolver never auto-chowns.

**Template namespace.** A dep's shareable volumes also surface in the file-template `.Dep` namespace as `.Dep.<key>.Volumes.<volname>` (the value is the volume's mountpoint inside the dep's container). This is parallel to `.Dep.<key>.Ports`. Non-shareable volumes are deliberately omitted from the map so file templates cannot reach data the dep author did not opt in to expose.

**Uninstall ordering.** Existing `Before=`/`PartOf=` directives already guarantee parent stops before deps and that deps stop before their producers, so when a parent uninstalls (cascade-uninstalling its deps) the consumer's container is gone before the producer's volume is touched. No new uninstall logic is needed.

**Out of scope.** A dep belongs to exactly one parent (existing invariant); shared volumes do not make deps multi-tenant. Reverse-direction sharing (parent's volume → dep) is not supported in v1; the schema stays extensible if it becomes needed. System services (`town-os-system--*`) do not get this feature — `GenerateSystemServiceUnit` does not consult `expose`/`consume`.

### Named Ports

Dependency port references can use a semantic name instead of a container-port number. A dep declares the name as a YAML key in `network.external` / `network.internal`; parents reference the same port via `@dep_KEY_port_NAME@`. This keeps the raw port number in exactly one place (the dep that owns it) and lets the parent talk about roles (`sql`, `http`, `admin`) rather than protocol trivia.

**Canonical shape.** The dep owns the port number — ideally as a `type: port` question default so auto-generation and override both work normally:

```yaml
# dep: named-db/1.0.yaml
environment:
  PGPORT: "@port@"
network:
  internal:
    sql: "@port@"
questions:
  port:
    query: "What port should PostgreSQL listen on?"
    type: port
    default: "5432"
```

```yaml
# parent: named-parent/1.0.yaml
environment:
  DB_HOST: "@dep_db_host@"
  DB_PORT: "@dep_db_port_sql@"   # no "5432" anywhere in the parent
dependencies:
  db:
    package: named-db
```

**Map schema.** A port entry in `network.external` or `network.internal` has a YAML key that is either:

- A numeric port string (legacy form): `"5432": "5432"` → host port 5432 → container port 5432. No name is recorded.
- A semantic name matching `PortNameRegexp` (`^[a-zA-Z][a-zA-Z0-9_]*$`): `sql: "5432"` → the container port (value) doubles as the host port, and the name `sql` is stored in `PackageNetwork.{External,Internal}Names[containerPort]`. Names must start with a letter (to avoid ambiguity with numeric parsing) and may contain alphanumerics and underscores.

Both forms coexist in the same map; the parser branches on the key. A name mapping two different container ports, or two names mapping to the same container port, is a compile-time error. The compiled `Package` type gains two optional `PortNameMap` fields alongside the existing `PortMap`s; consumers that only care about numeric ports (unit generation, network-state serialization) see no change.

**Env var and template emission.** For every port in the compiled dep, the installer emits `TOWNOS_DEP_<KEY>_PORT_<N>=<N>` (always). If the port has a name, it additionally emits `TOWNOS_DEP_<KEY>_PORT_<UPPER_NAME>=<N>` with the same value. The template resolver strips the `TOWNOS_` prefix and lowercases the rest, so both `@dep_db_port_5432@` and `@dep_db_port_sql@` resolve to the same value. The `depKeyRefRegex` in `controller_install_dependencies.go` accepts both forms; sibling-dep topo sort recognizes named references when building the DAG.

**Back-compat.** Existing packages using the numeric form keep working unchanged — no migration is forced. Parents can mix numeric and named references to the same dep in the same file. Reconcile rebuilds both forms during startup so surviving existing installs never regress.

**When to use a name.** Whenever a parent references a dep's port. A name is the single fact the parent can cite; the dep owns the number. Use names for internal ports first (that's where parent-dep traffic lives on the shared podman network); external named ports are allowed but uncommon since parents don't usually dial deps through host bindings.

## Networks (WireGuard Overlays)

A **network** is a named WireGuard overlay paired with a DNS TLD. Packages install into a network; peers join it; the TLD is what partitions who can resolve what (see [Network TLDs, Dual-Home, and Split-Horizon Resolution](#network-tlds-dual-home-and-split-horizon-resolution)).

### Network Model

`account.Network` (`src/account/network.go`) carries: `Name`, `TLD`, `Subnet`, `Address` (the box's own overlay address, always the `.1` host), `PublicKey`, `PrivateKey` (never serialized), `ListenPort`, `Enabled`, and timestamps. Names are DNS-label-safe (`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`, max 32 chars) because they are reused as WireGuard interface suffixes and systemd unit names.

`Enabled` controls only the *transport*: when false the WireGuard interface is not brought up, cutting remote access while local DNS resolution and the containers themselves keep running.

### The home network always exists

`DefaultNetworkName` is `home`, and it is **seeded by `account.InitNetworkManager`**, alongside the tables — not by boot reconcile. So it is there from the moment there is a database: before the controller boots, in every test server, and for the first request the box ever serves. `account.DefaultNetwork()` is the canonical row.

That matters because everything downstream is written assuming it: the first account is scoped to it ([Every account belongs to the home network](#every-account-belongs-to-the-home-network)), the default TLD is its TLD, and gfeh gives it a partition and seats the founder there. A box where it had to be created first has a window in which all of that is false — which is what used to make object storage sit dead on a first boot until some later restart happened to find the network already there.

It **cannot be removed** (`ErrNetworkProtected`, and `POST /networks/remove` refuses it), and it cannot be created a second time — `POST /networks/create` for `home` gets a 409 from the TLD-collision check.

It is **DNS-only**: `applyNetworkTransport` gives it no WireGuard interface, no overlay subnet, and no peers, so it can never have a tunnelled device. The seeded row therefore carries **no transport fields at all** — empty subnet, no keypair, port 0. That is the truth rather than a placeholder; a derived subnet and keys would be fields nothing ever reads.

**Its TLD comes from `dns_tld`, and the controller keeps them in step.** The seed cannot know it (the account package has no settings manager), so the row arrives with the bare default and `ensureDefaultNetwork` reconciles it at boot, writing only when the two disagree. `POST /dns/tld` repoints it at the same time it writes the setting. Both go through `NetworkManager.SetTLD`, which exists for exactly this. Getting it wrong is not cosmetic: `applyNetworkTransport` hands `n.TLD` to `rolodex.EnsureNetworkScope`, which decides which zone the home scope owns.

### Addressing and Interfaces

- **Subnet** — `wireguard.SubnetForNetwork(seed, name)` derives a deterministic `/24` from a box-identity seed and the network name. Keying on box identity means two Town OS boxes that both serve peers pick distinct subnets, so a device joining both never sees a collision. Subnets are drawn from `10.64.0.0/10` to bias away from the `10.0`/`10.1` ranges consumer routers hand out. The seed is `networkIPAMSeed()`: the systemd machine-id, else the hostname, else a constant, so derivation never fails — with the instance salt folded in.
- **Interface name** — `wireguard.InterfaceName(salt, name)` is `"town" + 4 hex` of a SHA-256 of the salted network name: stable across create order, independent of how many networks exist, and within the kernel's 15-character limit. wg-quick derives the interface from the config filename, so the config is written as `<InterfaceName>.conf`. `systemcontroller.NetworkInterfaceName(name)` is the salt-applied form the integration tests use, so a test never asserts against a device nothing created.
- **Listen port** — `wireguard.ListenPortForName(salt, name)` offsets from `DefaultListenPortBase` (51820) by a hash of the salted name, probing forward past a port another network already holds.

#### The instance salt

A WireGuard interface name, its UDP listen port, and its overlay subnet are all **namespace-global**, and the test and dev containers both run `--net host` (deliberately — bridge-network DNS breaks on captive networks). Without a salt, a `make test-full` box and a `make dev` box derive the *same* interface name and listen port for the same network name: the second one up cannot create its device, and its overlay is simply dead. Two concurrent test worktrees collide the same way — IRON RULE.

`TOWN_OS_WG_SALT` (`EnvWireGuardSalt`) is read once into `wireGuardSalt`. The harness sets it to `<role>-<INSTANCE_ID>` via `wireguard_salt` in `make/lib.sh` — the role separates a test box from a dev box in one checkout, `INSTANCE_ID` separates checkouts, and both halves are needed. It is stable for a given role and checkout, which matters for dev, whose database survives across runs and whose stored subnets would otherwise point at devices named for the previous salt. **A real box sets nothing and keeps the historical unsalted names**; an empty salt returns every derivation untouched.

**Podman's default subnet pools must stay out of `10.64.0.0/10`.** The runtime image writes `/etc/containers/containers.conf` with `default_subnet_pools = [{"base" = "172.16.0.0/12", "size" = 24}]` precisely because podman's defaults (10.89/16, 10.90/15, 10.96/11, …) all sit inside the overlay range: in-range `/24`s get skipped as conflicting with overlay routes, the pool exhausts under load with "could not find free subnet from subnet pools", and package container networks stop working. Do not remove that file or widen the pools back into `10.64.0.0/10`.

The `wireguard` package performs **no interface control itself**. It generates keypairs and renders wg-quick-style config; the systemcontroller writes the rendered config into the host-shared network-state directory and a generated systemd unit brings the kernel interface up and down. That is what keeps the systemcontroller container free of host network-namespace requirements.

**Ordering matters in `applyNetworkTransport`.** Rolodex must be programmed *after* the interface is started and the overlay address is assigned, on an UP link, and covered by a route — assigned is not the same as usable. Programming it first asks rolodex to bind an address the host does not have yet; the bind fails `EADDRNOTAVAIL` and the listener dies permanently, because rolodex records a listener at spawn time and the corpse then blocks every re-assert.

### Peers

`account.NetworkPeer` carries `Network`, `PublicKey`, `Name`, `AllowedIP`, `Endpoint`, `Rolodex`, `CreatedBy`, `ExpiresAt`, and `CreatedAt`.

- **`Rolodex`** marks a peer that runs a rolodex DNS server on its overlay address. The box then registers that address as a per-TLD forwarder, so names under the shared TLD that are authoritative on the peer resolve across the overlay. Phones and laptops leave it false.
- **`CreatedBy`** is the ownership key: an account holding the `wireguard` grant may refresh only the peers it created, so a scoped account cannot keep another account's peer alive.
- **`Endpoint`** is derived from **the address the enrolling client dialed** (the `Host` header of its `peers/add` request), not from the box's own view of itself. The box's public IP (from ipinfo.io) or LAN address is unreachable behind a NAT, a port forward, or a relay — a phone on the same Wi-Fi cannot hairpin to the public IP and cannot route to the private LAN address at all, and the peer then handshakes into a void that looks to the user like broken DNS. The dialed address is reachable by construction: the request arrived over it. With no dialable address (a loopback enrollment) the endpoint is **omitted** rather than set to something that cannot work.

### Peer Enrollment TTL and the Reaper

An enrollment does not live forever. The `peer_ttl` setting (seconds, default `7200`) is how long one stays valid. A long-lived client refreshes its peer via `POST /networks/peers/refresh` before that elapses; an abandoned device's peer expires on its own, so the additive `peers/add` endpoint cannot silently accumulate dead peers and burn overlay addresses. A nil `ExpiresAt` means the peer never expires — permanent peers such as rolodex servers and operator-added devices.

The expiry is always **server-computed** as `now + peer_ttl`; the caller never chooses it. A background reaper goroutine calls `ReapExpiredPeers` and then re-renders the transport of each affected network once, so the live WireGuard device and the rolodex forwarders drop the reaped peers. It is best-effort and idempotent: the persisted peer set is the source of truth, and a failed re-render is repaired by the next tick or by boot reconcile. `peerReapInterval` is a quarter of the TTL, clamped to `[1m, 15m]`, so a lapsed peer lingers at most ~TTL/4 past expiry and neither a tiny nor an enormous TTL yields a pathological sweep rate.

### Connected Peers

`GET /networks/peers/connected` joins the persisted rows with the live kernel state of each tunnel. The persisted half (name, account, overlay address, expiry) answers "who is allowed on"; the `wg show <iface> dump` half (handshake, observed endpoint, transfer) answers "who is actually here right now" — neither alone is the question, which is why `ConnectedPeerView` exists rather than reusing `account.NetworkPeer`.

Parsing lives in the pure `wireguard.ParseDump`. The **first** line of a dump describes the interface itself and is deliberately skipped; treating it as a peer would manufacture a phantom holding the interface's own key. `wg`'s `(none)` and `off` placeholders are decoded rather than passed through as literal strings.

**Connectedness is a handshake inside WireGuard's 180s `REJECT_AFTER_TIME` window** (`HandshakeStaleAfter`) — the only liveness the protocol offers. There is no session teardown, so a peer that walks away is indistinguishable from one that is idle until its handshake goes stale. A peer that has *never* handshaken keeps a nil timestamp rather than the epoch, because "never set up" and "offline for a day" are different facts about a device.

The systemcontroller runs `--net host`, so it already shares the namespace where wg-quick created the device; the runtime image ships `wireguard-tools` for the `wg` binary alone (wg-quick still runs on the host, from the generated units). A missing interface is not an error — a disabled network, or one whose transport has not come up, simply has no live peers and its persisted rows must still render — and a dump failure degrades to the persisted rows instead of blanking the panel. The `home` network is excluded entirely: it has no transport, so including it would put a permanently disconnected row in a panel about who is tunnelled in.

**Disconnect reuses `POST /networks/peers/remove`** rather than adding an endpoint. WireGuard has no session to kill, so removing the peer is the only forcible termination there is.

### Networks API

- `GET /networks` (auth required) -- list all networks with peer count, derived interface name, and running state. The private key is never exposed.
- `POST /networks/create` (admin required) -- create a network. Accepts name and optional TLD (defaults to the name). Derives the subnet, generates a keypair, assigns a listen port, and returns the created network. A name or TLD already taken is a 409 — including `home`, which always exists.
- `POST /networks/remove` (admin required) -- delete a network by name. The home network cannot be removed.
- `POST /networks/enable` / `POST /networks/disable` (admin required) -- bring the overlay interface up or down.
- `GET /networks/peers?network=<name>` (auth required, and confined by `requireNetworkScope`) -- list the peers registered on a network. The route is on the `wireguard` grant's allowlist, so a scoped account reaches it, and a peer list names devices, the accounts that enrolled them, and their overlay addresses — a grant is authority over the caller's own networks, and a read is where that is easiest to forget.
- `GET /networks/peers/connected` (**admin required**) -- every peer across every WireGuard network joined with live tunnel state. Deliberately tighter than its `requireAuth` siblings and absent from `grantRoutes`.
- `POST /networks/peers/add` (`requirePeerEnroll`: admin or the `wireguard` grant, confined to the caller's networks) -- register a peer. When `public_key` is empty the server generates a keypair and returns the private key plus a ready-to-import device config. Accepts an optional `endpoint` and a `rolodex` flag.
- `POST /networks/peers/refresh` (`requirePeerEnroll`, and only for a peer the caller enrolled) -- extend a peer's TTL by `peer_ttl` and return the new expiry, so a client can pace its next heartbeat well before the TTL elapses.
- `POST /networks/peers/remove` (admin required) -- remove a peer by public key.

### Networks UI

`/dashboard/networks` lists the networks with create/remove/enable/disable actions and per-network peer enrollment. A second **Connected Peers** panel itemizes every peer across every WireGuard network — the device, the account that enrolled it, its overlay address, the endpoint it is dialing from, live handshake and transfer state, and its enrollment expiry — with a per-row Disconnect action.

## TLS and the Local CA

Town OS runs its own X.509 certificate authority so package and page traffic is served over HTTPS by name, with no public CA and no ACME dependency on the LAN.

- **The CA** (`src/tls/ca.go`) is an ECDSA P-256 key pair under the btrfs `tls` subvolume (`ca.crt`, `ca.key`), 10-year validity, so it survives reboots. `EnsureCA` loads an existing CA or generates one on demand; the cert is world-readable and the key is owner-only and must never be served. CA failure is non-fatal — the system boots without HTTPS rather than not at all.
- **Leaves** (`src/tls/leaf.go`) are per-package and per-page, written as `cert.pem`/`key.pem` in one directory so a consumer needs only a single mount path. `IssueLeaf` is **idempotent**: when an existing certificate already covers exactly the requested SAN set and is still valid it returns without touching disk, which is what lets reconcile call it on every boot without churning cert files. Hostnames may be DNS names or IP literals; anything that parses as an IP goes into `IPAddresses`, everything else into `DNSNames`.
- **`GET /tls/ca.crt`** is **public** (and in `grantCommonRoutes`) so any client — a browser, a phone joining over the overlay — can fetch the root and trust the box.

A package's leaf SAN set is derived from the same single FQDN as its A record, its DANE TLSA owner, and its ingress vhost; see [The package FQDN is one string](#the-package-fqdn-is-one-string--a-record-leaf-san-tlsa-owner-ingress-vhost). Leaves also carry the box's overlay IP on the install network so a peer can reach the package by raw WireGuard address, not only by name.

## Ingress

The ingress is the shared Host router: a sidecar that supervises a Caddy child and exposes a gRPC management API the systemcontroller programs, the same way it programs rolodex. It holds the desired route set in memory, renders a Caddyfile on every change, and reloads Caddy zero-downtime.

- **`src/ingress`** is the in-container service (`Server`, `renderCaddyfile`, the gRPC client, the `town-os-ingress` binary). It is built `CGO_ENABLED=0`.
- **`src/ingress/ingressctl`** is the systemcontroller-side lifecycle controller: it generates, installs, and restarts the `town-os-system--ingress` unit and exposes the gRPC socket path the systemcontroller dials. It is a separate package precisely so the CGO-free ingress binary never imports `src/systemd` (which pulls in cgo via sdjournal).

### Routing

- **`:443`** — one `https://<hostname>` vhost per route, terminating TLS with the route's file-pinned local-CA leaf, or an explicit ACME issuer for a public FQDN, and reverse-proxying to the backend container on the shared `town-os-ingress` podman network.
- **`:80`** — Host-routed: pages (`ServeHttp`) are served directly over plain HTTP (static content, nothing sensitive), packages get an HTTP→HTTPS redirect so they stay HTTPS-only, and any host not matched by a route falls through to the default backend — the Town OS UI, so bare-IP login (`http://<box-ip>/`) keeps working now that the UI no longer squats the host's `:80`.
- A route with **no issued leaf yet** (non-ACME, empty cert dir) is skipped for HTTPS, so a half-provisioned entry never makes Caddy reject the whole config; a page still gets its `:80` vhost, which needs no cert. Packages are only redirected once the HTTPS target actually exists, so nothing redirects into a not-yet-provisioned cert.

### Rendering

Output is **sorted by hostname** so the rendered bytes are deterministic across reconciles — that is what lets the supervisor no-op a reload whose content has not changed. Globals are `auto_https off` (Town OS manages certs) and `protocols h1 h2` (the ingress publishes TCP only, so H3/QUIC over UDP is unreachable). The Caddy admin API is deliberately **left enabled** on its default container-local `localhost:2019`: the supervisor programs new routes with `caddy reload`, which talks to that endpoint, so `admin off` would break every route update after the first boot.

The ingress is **interface-agnostic**: it publishes `-p 443:443` / `-p 80:80` with no host IP and its Caddyfile carries **no `bind` directive**, so Caddy listens on all interfaces and selects the vhost purely by SNI/Host. A LAN client and an overlay peer hit the same listener, SNI-select the same vhost, get the same local-CA leaf, and are proxied to the same container. Do not add `bind` directives or per-network listeners.

Production binds 443/80; integration tests pass ephemeral ports (rendered as `host:PORT`) so `make test-full` never collides on a privileged port. Boot programs the full route set declaratively via `RebuildIngress`, the same push model as `RebuildDNS`; package and page CRUD program incremental changes over the same gRPC API.

## Boot Status and Refresh

`:5309` is bound before any startup work happens, so the UI can watch a boot — including a self-update — proceed rather than polling a dead port.

### The Boot Stub

`NewBootHandler` is a bare `http.ServeMux` (intentionally, so it can never accidentally mount a real API route) serving exactly three things:

- `GET /status/ping` → `{booting, step, done, error, boot_id}`. It answers **503 while booting** and 200 once done, so external readiness probes — the test container's `wait_for_url`, orchestrator health checks — do not treat the stub as "service ready" and start hammering a half-booted controller. The JSON body still carries the progress fields, so the UI can distinguish "coming up" from "fully down".
- `GET /boot-status` → an SSE stream of progress events.
- everything else → **403**, not 404: the route exists in the full handler, it is just unavailable until the swap.

`RootHandler.Swap` atomically replaces the stub with the full Echo router at the end of boot. The listener socket is never closed, so there is no port flap, and already-dispatched SSE handlers hold their own writer and keep streaming across the swap.

### Progress Stages

Five coarse stages, deliberately few and user-facing — a person watching a self-update wants to know whether "the controller", "DNS", "the system services", or "my packages" is holding things up, not which internal constructor is running:

`boot_controller` → `boot_dns` → `boot_services` → `restart_packages` → `ready`

The freshness stage emits an additional event per installed package, prefixed `restarting_` (`PackageStepPrefix`); the UI strips the prefix and renders each as its own row, equal in weight to the coarse stages, so a box with many packages shows real progress rather than one stalled bar. These per-package names deliberately do not match the `[a-z0-9_]+` shape enforced for the fixed stages — they are dynamic values.

The stage literals are duplicated as `bs.Step("...")` calls in `main.go` rather than referenced as constants, because `TestBootStepsFrontendInSyncWithBackend` parses them out of `main.go` to prove the frontend list agrees. **Keep the two in sync**; that test fails loudly if they drift.

### Broadcast Semantics

`BootStatus` is safe for concurrent use and **never blocks the boot**. `Subscribe` replays history into a new subscriber first (so a late subscriber misses nothing), sizing the buffer to hold the full replay plus headroom; if boot already finished it closes the channel right after the replay so `for range` consumers exit. `publish` sends non-blocking — a subscriber whose buffer fills is dropped and closed, and its client reconnects and gets the history replay. No event can follow `Done`.

### Process Identity and Refresh

`boot_id` is a random UUID regenerated on every systemcontroller start, reported by **both** the stub's and the full router's `/status/ping` (and carried even in the unauthenticated minimal ping response, since a browser is briefly tokenless across a restart). A client that captured the id before asking for a refresh can tell "the old process is still answering" (same id) from "the new process is up" (different id) — the two are otherwise indistinguishable, because both serve a 200 ping and both 404 `/boot-status` once booted. This is what makes the UI's Refresh Core Services flow able to watch its own successor.

`/boot-status` is excluded from audit logging for the same reason: a UI holding the stream open across the handler swap lands its next request on the full router, which 404s. That is the expected end of the stream, not an operator action — auditing it would file a failed-action row on every successful refresh and inflate the red failure pill on the dashboard.

`POST /system-services/refresh` (admin) pulls every system-service image in dependency order — the systemcontroller image first (the version anchor, so the freshly pulled image is already local when it self-restarts at the end), then rolodex (the box's DNS, which the other pulls may need to resolve their registry), then everything else in parallel (max 3 concurrent) — and leaves a marker that the next process's freshness stage consumes to restart installed packages.

## DNS Management (Rolodex)

Town OS includes an integrated local DNS resolver powered by a `rolodex-dns` container. The rolodex server manages zone files and records for installed packages, providing local name resolution via a gRPC Unix socket interface.

### Rolodex Manager

Rolodex itself is a boot service installed and supervised by systemd — the systemcontroller does not install, start, stop, or restart it at the container level. The `rolodex.Manager` instead:

- **`WriteConfig`** -- writes `rolodex.yml` into `DataDir`. Idempotent: skips writing when the file exists, is newer than the systemcontroller binary, and already matches the expected content. Returns a boolean indicating whether the file was written (so the caller can decide whether to restart the systemd unit).
- **`WaitForDNSReady`** -- polls `DNSLoopback:{port}` over TCP until it accepts a connection or the 30-second deadline passes. Called at startup before any operation that depends on DNS (e.g., image pulls).
- **`SystemServices`** -- returns metadata for the rolodex system service (key, display name, image, port, unit name) so it surfaces alongside other system services in status responses and the UI.
- **`Status`** -- queries the systemd unit state to report whether rolodex is running.

The rolodex container runs with `--net host` and binds DNS to `DNSLoopback` (`127.0.0.2`) on the configured port (default `53`, overridable via `DNSPort` for tests). The image tag is derived from the system controller's release tag (`quay.io/town/rolodex:<tag>`), overridable via the `ROLODEX_IMAGE` environment variable.

**Resolution mode.** `rolodex.yml` pins `resolution.mode` explicitly via `Config.ResolutionMode`, defaulting to **`auto`** (`DefaultResolutionMode`) — rolodex's own tiered fallback chain: iterate from the root servers, then DoH/DoT, then the `forwarders:` list, then a public resolver on :53, sticking to whichever tier last worked. The mode is written explicitly rather than left to rolodex's default so Town OS behavior does not move when upstream changes its default. The forwarding integration test opts into `ResolutionModeForward` and points the forwarders at a local stub.

**Do not default to bare `recursive`.** It has *no* fallback, and rolodex's iterative resolver (`src/resolver.rs`) sends a **single un-retransmitted UDP datagram per nameserver with a 1500 ms deadline**; when every server in the current delegation set fails, `resolve()` errors and `iterative_query` converts *any* error into SERVFAIL. So one dropped packet SERVFAILs a query, and on a network that filters or hijacks outbound :53 (hotel, captive portal, some ISPs) *every* external name SERVFAILs. `auto` keeps recursion's privacy wherever the network permits it and degrades instead of failing where it does not. Related: rolodex's delegation cache and negative cache landed in `ce44bb5`, which is **not in any released tag** — until a release ships it, recursive mode re-walks from the roots for every uncached name and every NXDOMAIN (measured: 0.6–1.9 s per cold public name, 2.7 s for an RFC1918 PTR).

The mode is operator-configurable at runtime via the `dns_resolution_mode` setting (`auto` | `recursive` | `forward`; validated by `ValidateDNSResolutionMode`, so an unparseable value can never reach `rolodex.yml` and brick DNS). `main.go` reads it into `rolodex.Config` at boot; a change through `POST /settings/set` runs `Controller.RefreshDNSResolutionMode`, which calls **`Manager.RewriteConfig()`** and restarts the rolodex unit. `RewriteConfig` exists precisely because `WriteConfig` refuses to overwrite a `rolodex.yml` newer than the systemcontroller binary (it treats that as hand-edited) — and the file written at the previous boot *always* satisfies that condition, so `WriteConfig` would silently no-op on an operator-initiated change. Use `WriteConfig` at boot, `RewriteConfig` for runtime changes.

### Local forwarders

The `forwarders:` list Town OS writes by default is `DefaultForwarders` — public resolvers. On a network that blocks external DNS (a hotel, a captive portal, an ISP that drops outbound `:53` to anything but its own servers) those are precisely the addresses being dropped, so `auto`'s forwarder tier — the tier reached *after* the roots and the encrypted upstreams have already failed, which is exactly this case — has nothing to fall back to. The resolver such a network handed out over DHCP does still answer.

The `dns_local_forwarders` setting (`false` by default, validated by `ValidateBool`) replaces the forwarder list with the resolvers this box's own network configuration points at. It is **not a resolution mode**: it changes *which* addresses the local tier holds, and the mode still decides whether that tier is consulted at all — in `auto` it is the last resort, in `forward` it is the only upstream, in `recursive` it is unused. Turning it on must therefore never move the mode.

**Off is the default and the direction that matters.** The local resolver sees every name the household looks up, which is the thing resolving from the roots exists to avoid. That is a trade an operator makes knowingly, not one a box makes for them the first time a network misbehaves.

Discovery lives in `src/rolodex/hostdns.go`. `HostResolversFrom` reads `hostResolvConfPaths` in order — `/run/systemd/resolve/resolv.conf` **first**, then `/etc/resolv.conf` — and the first file yielding a usable address wins, not merely the first file that exists. The order is load-bearing: on a resolved box `/etc/resolv.conf` is the stub (`127.0.0.53`), which is discarded as loopback, so a discovery that stopped at the first *readable* file would find nothing on exactly the boxes this feature is for. The uplink file is reachable from inside the container because the systemcontroller unit bind-mounts `-v /run/systemd:/run/systemd`; losing that mount silently degrades discovery. Loopback, unspecified, multicast, and link-local addresses are all dropped — forwarding to the resolved stub or to rolodex's own `DNSLoopback` listener is a query loop, not an upstream, and a link-local address is meaningless without the zone a `resolv.conf` line does not carry.

**Discovery that finds nothing keeps the forwarders already configured.** `Manager.forwarders()` falls back to `Config.Forwarders`, then to `DefaultForwarders`, so turning the switch on can never leave the local tier pointing at nothing — which would be strictly worse than the public defaults it was turned on to replace.

`main.go` reads the setting into `rolodex.Config` at boot (an unparseable stored value is read as off — the safe direction), so a box that changed networks picks the new resolver up on the next boot with no operator action. A change through `POST /settings/set` runs `Controller.RefreshDNSLocalForwarders`, which — unlike the resolution mode — does **not** short-circuit when the flag is unchanged: with it already on, the discovered addresses themselves can have moved, and re-rendering is how that reaches rolodex. `RewriteConfig` still reports whether the bytes actually changed, so an identical render costs no restart.

`GET /dns/status` reports **both** `local_forwarders` (what the operator asked for) and `forwarders` (what `rolodex.yml` actually holds). They disagree in exactly one case — discovery found nothing usable and the public defaults were kept — which is the one case where the switch reads as on and changes nothing, so a UI showing only the flag would be showing a setting that is not in effect. The Settings screen renders the effective list for that reason, and says so explicitly when it is empty.

**Rolodex image is pulled per-arch in tests and dev** — the make harness pulls the host's per-arch rc tag `quay.io/town/rolodex:rc.latest-<arch>` (where `<arch>` is the raw `uname -m` form `x86_64`/`aarch64`), NOT the plain no-arch `rc.latest`. Internal Town OS image pulls default to the rc channel so the harness, dev, and the runtime all track `rc.latest-<arch>`. Rolodex publishes per-arch tags pushed natively from each host (`make push-rc` / `make push-release` in the rolodex-dns repo), so no multi-arch manifest assembly is required for test hosts of any architecture; the *plain* `rc.latest` (no arch suffix) is a single-arch manifest and crash-loops with `exec format error` on the other architecture — only the suffixed `rc.latest-<arch>` is safe to pull directly. The Makefile computes `HOST_ARCH` (normalized to `x86_64`/`aarch64`) and defaults `ROLODEX_IMAGE_TAG ?= rc.latest-$(HOST_ARCH)`; `ROLODEX_IMAGE` derives from it and is injected into test/dev containers via env. Override with `make ROLODEX_IMAGE_TAG=<tag> ...` (e.g. `latest-$(HOST_ARCH)` for a released rolodex) or the `ROLODEX_IMAGE` environment variable. Production/runtime behavior matches — the systemcontroller derives the tag from its release tag (falling back to `rc.latest-<arch>` via `defaultVersionTag()`) unless `ROLODEX_IMAGE` is set; the test and dev harnesses always set it. The dev container's baked rolodex unit (`integration/testdata/town-os-system--rolodex.service`) uses an `@ROLODEX_IMAGE@` placeholder substituted at image build time via the `ROLODEX_IMAGE` build arg in `integration/testdata/Containerfile.dev` (the build fails if the arg is empty), so the baked unit always matches the image the harness loads.

### Network TLDs, Dual-Home, and Split-Horizon Resolution

Every network owns a TLD, registered in rolodex as a network scope whose
`home_domain` is the TLD (`rolodex.EnsureNetworkScope`, called from
`applyNetworkTransport` in `controller_networks_reconcile.go`). Owning the TLD is
what **partitions** it: rolodex hides a scope's TLD from any WireGuard peer joined
to a *different* scope. The default/home network (`account.DefaultNetworkName`,
TLD from the `dns_tld` setting, default `home`) owns `home.` as a **DNS-only**
scope — it gets no WireGuard interface, overlay subnet, or peer association, so no
source IP is ever bound to the home scope. `.home` is therefore LAN-only and
hidden from every WireGuard peer, yet fully resolvable on the LAN.

**Dual-home.** A package installed into a non-default network is published twice
(`registerScopedPackageDNS`):

- a **scoped** A record under the network TLD at the box's **overlay IP** — served
  to WireGuard overlay peers by source IP (`AddScopedRecord`); and
- a **global** A record for the same FQDN at the box's **LAN IP**
  (`RegisterPackageDNS`) — served to loopback/LAN clients.

Each side receives an address it can actually route to. No global authoritative
zone is published for the network TLD: a bare global A record resolves on the LAN
with no zone, and rolodex's **LAN→owning-scope fallback** (rolodex-dns, resolution
step 5) treats the scope-owned TLD as authoritative for LAN sources — so an
unmatched name under a network TLD yields an authoritative NXDOMAIN from the LAN
instead of leaking the private TLD upstream. Default-network packages stay in the
global home zone only (`registerPackageDNS`); a non-default package must never
appear there (the original "resolves as `.home`" bug).

**Split-horizon summary.** A LAN client (no WireGuard) resolves **every** network
TLD (`.home`, and every WireGuard network's TLD) plus the public internet. A
WireGuard peer joined to one network resolves **only** that network's TLD plus the
public internet — a sibling network's TLD and `.home` both return NXDOMAIN. The
LAN view is never partitioned; only overlay peers are. `RebuildNetworkDNS`
(`reconcile.go`, called at boot) re-registers each non-default network package's
LAN-facing global record so an already-installed package keeps resolving on the
LAN after a restart; the scoped records persist in rolodex independently. Boot
network reconcile is passed the rolodex client so the home scope (and every
network scope) is established even on a cold boot.

### The package FQDN is one string — A record, leaf SAN, TLSA owner, ingress vhost

**A package's DNS name is always derived from its *install network's* TLD, never
from the global `dns_tld` setting.** `packageFQDN(repo, name, tld)`
(`src/svc/systemcontroller/controller_tls.go`) is the single source of truth, and
the TLD comes from `networkTLDValue(nm, settingsMgr, network)` (which falls back
to `dns_tld` only for the default network). Four things must name a package
identically, and a mismatch in any one of them silently breaks serving:

1. its **A record**, 2. its **leaf certificate SAN**, 3. its **DANE TLSA owner**,
and 4. its **shared :443 ingress vhost**.

**All three publishers compose that name through one validator.** A package, a
page, and an object-storage partition each get a name under a network's TLD, and
each used to compose it itself — disagreeing about what a legal name was.
`gfehFQDN` normalized the label, validated every dot-separated component against
the strict LDH rule, and refused a name that qualified past the 253-character
limit; `packageFQDN` was bare concatenation with neither check; `pageFQDN`
checked nothing beyond trimming. `qualifyPublishedName`
(`src/svc/systemcontroller/published_name.go`) is now the one composer, applying
gfeh's rules to all three, and `validatePublishedName` is the non-qualifying half
for a name that must be checked but not composed. A name that fails is **dropped**
— every collector already skips an empty FQDN, so it contributes no record, no
route, no certificate and no directory rather than a broken one to all four — and
the refusal logs at **Error**, because `LOG_LEVEL` defaults to `error` and a
service that silently stops resolving must not be discoverable only by turning
logging up.

**A page's domain is validated at the API, not just at composition.** For a page
the name is a *fifth* thing: its on-disk subvolume and webroot symlink, since the
pages Caddy roots on `/srv/<host>`. `ValidatePageDomain` runs in both
`POST /pages/create` and `POST /pages/update`, returning 400. Update is the route
that mattered: create was incidentally covered because `CreateFilesystem` runs
`storage.ValidateFilesystemName` and the handler rolls back before reaching the
symlink code, whereas `migratePageDir` logs a `RenameFilesystem` failure and
carries on to `RemovePageSymlink` / `EnsurePageSymlink` regardless.

The subtle part is that a **public FQDN is exempt from qualification but not from
validation**. `isPublicFQDN` reads any dotted name not ending in the TLD as the
operator's own domain, to be served verbatim via ACME — which is correct for
`blog.example.com` and is also how `../escape.example.com`,
`site.example.com/../../etc`, and `site.example.com other.example.com` reached
`filepath.Join` and the Caddyfile unexamined. "It is the operator's domain" is a
reason not to compose it under the box's TLD; it is never a reason not to check
it.

To keep them from drifting, the FQDN is computed **once** — in `applyPackageTLS`,
on the same line that issues the leaf — and persisted as
`PackageNetworkState.FQDN` (`fqdn` in the per-package network state JSON). The
ingress route builder (`collectPackageIngressSites`) reads that field rather than
recomposing the name, so the vhost is by construction the name the cert is valid
for. `reconcileWriteNetworkState` takes the TLD **from its caller**
(`reconcilePackage`, which resolved it from the install network); it must never
call `reconcileDNSTLD` itself. Doing so was a real bug: every boot re-issued a
`fart`-network package's leaf with SAN `<pkg>.<repo>.home`, clobbering the
correct `.fart` SAN, while the ingress rendered a `<pkg>.<repo>.home` vhost that
nothing dialed — so the package resolved on the LAN but was never served. An
empty `fqdn` (pre-upgrade state file, or a non-HTTP package) falls back to the
global TLD and self-heals on the next reconcile.

**The ingress is interface-agnostic and needs no per-network binding.** It
publishes `-p 443:443` / `-p 80:80` with no host IP (`0.0.0.0`, so LAN +
WireGuard + loopback all reach it) and its Caddyfile has **no `bind` directive**,
so Caddy listens on all interfaces and selects the vhost purely by **SNI/Host**.
Backends are reached by container name on the shared `town-os-ingress` podman
network, which every HTTP-fronted package joins regardless of its WireGuard
network. A LAN client and an overlay peer therefore hit the same listener,
SNI-select the same vhost, get the same local-CA leaf, and are proxied to the
same container. Nothing binds a listening socket to an overlay IP —
`BindOverlayAddress` is a rolodex *DNS scope association*, not a socket bind. Do
not add `bind` directives or per-network listeners to the ingress.

The package leaf also carries the box's **overlay IP** on that network as a SAN
(`networkOverlayIPValue`), so a peer can reach the package by raw WireGuard
address (`https://10.65.0.1`) and not only by name. It is empty for the default
network (which has no WireGuard transport), which keeps default-network leaves
from churning on every reconcile.

DANE TLSA for a network package is **dual-homed like its A record**:
`RebuildNetworkDNS` registers a global pin (served to LAN sources via the
LAN→owning-scope fallback) *and* a scoped pin (served to overlay peers, whose
queries never see global records). Install alone only ever wrote the scoped half,
and nothing republished either half across a restart.

### Pages are network-scoped too

A page carries a `network` (the `PageSite.Network` column; `""` means the
default/home network, the same convention as `Installer.LoadNetwork` for
packages) and gets **exactly the treatment a package does**: its name comes from
that network's TLD, it is dual-homed (scoped overlay record + global LAN record),
its leaf carries the network FQDN plus the box's overlay IP, its DANE TLSA is
pinned under the network TLD (global + scoped), and it is hidden from peers of
every *other* network. `pageFQDN` (`pages_tls.go`) is the page-side twin of
`packageFQDN`, and `pageNetworkTLD` of `networkTLDValue`.

The page-specific wrinkle: a page's FQDN **also names its on-disk btrfs
subvolume and its webroot symlink** (the pages Caddy roots on `/srv/<host>`). So
the FQDN is not merely a label — get it wrong and the content moves out from
under the name the ingress serves. Three consequences:

- `reconcilePages` builds its `valid` set with `pageFQDN`, because that set drives
  `pruneStalePageSymlinks` — naming a `fart` page `blog.home` there would both
  miss its real `blog.fart` directory *and* prune the live symlink.
- Changing a page's **network** renames its subvolume/symlink (`migratePageDir`),
  exactly as a `dns_tld` change does for default-network pages.
- `migratePageDirsForTLD` (the `dns_tld`-change handler) **skips non-default-network
  pages** — they are not named under the global TLD, so renaming them would break
  a page that was working.

Pages remain served by the single shared `town-os-system--pages` container behind
the ingress; the network is a naming/DNS/cert concern only, with no per-network
container or podman plumbing.

### DNS API

- `GET /dns/status` (auth required) -- returns DNS status including enabled flag, running state, TLD, record count, `local_forwarders` (whether the forwarder list is taken from the host's own resolvers), and `forwarders` (the addresses `rolodex.yml` actually holds — see [Local forwarders](#local-forwarders)).
- `GET /dns/records` (auth required) -- list all DNS records.
- `POST /dns/records/add` (admin required) -- add a DNS record. Accepts name, record type, value, and TTL.
- `POST /dns/records/remove` (admin required) -- remove a DNS record by name and type.
- `GET /dns/tld` (auth required) -- get the current top-level domain.
- `POST /dns/tld` (admin required) -- set the TLD. Changes the existing TLD and re-registers all installed packages.
- `POST /dns/setup` (admin required) -- initialize DNS and register all installed packages.
- `GET /dns/rbl` (auth required) -- get the RBL (Realtime Blackhole List, reverse-IP) configuration: global enabled flag, provider zones with their refusal codes **resolved to what is in effect**, the list-wide `refusal_cooldown_secs`, and `rotated_out` (providers currently backed off after refusing a query, with the code and seconds remaining). See [Refusal codes](#refusal-codes-a-provider-saying-stop-asking-is-not-saying-this-is-listed).
- `POST /dns/rbl` (admin required) -- replace the RBL configuration. Accepts an enabled flag, a list-wide `refusal_cooldown_secs`, and a list of `{zone, enabled, refusal_codes, refusal_cooldown_secs}` providers. Zones are validated as fully-qualified hostnames, lowercased, trimmed, and de-duplicated; refusal codes are validated by `ValidateRefusalCodes` (IPv4 address or `address/prefix`, masked to the prefix, `"none"` only as a lone entry, no duplicates).
- `GET /dns/dnsbl` (auth required) -- get the DNSBL (domain blocklist, forward-name) configuration, same shape as `/dns/rbl`.
- `POST /dns/dnsbl` (admin required) -- replace the DNSBL configuration (same shape and validation as `/dns/rbl`; its refusal cooldown is independent of the RBL one).
- `GET /dns/rbl/local` (auth required) -- list the local RBL blocklist entries (`{name, reason}`).
- `POST /dns/rbl/local/add` (admin required) -- add a local RBL entry. Accepts a name (domain or IP) and an optional reason. The name is validated (domain or IP), lowercased, and trimmed.
- `POST /dns/rbl/local/remove` (admin required) -- remove a local RBL entry by name.
- `GET /dns/dnsbl/allowlist` (auth required) -- list the DNSBL allowlist entries (`{name, reason}`).
- `POST /dns/dnsbl/allowlist/add` (admin required) -- exempt a name from the name-based blocklist check. Accepts a name and an optional reason. The name is lowercased, trimmed, and validated as a **domain name only** -- an IP literal is rejected (`ValidateDnsblAllowlistName`), because the allowlist matches names and their subdomains and could never match an address.
- `POST /dns/dnsbl/allowlist/remove` (admin required) -- remove an allowlist entry by name. The name is normalized but not re-validated, so an entry that predates a validation change is still removable.
- `GET /dns/services` (auth required) -- list installed package services with their published (in-DNS-zone) state (`{repo, name, version, fqdn, domains, published}`), deduplicated by repo/name.
- `POST /dns/services/set` (admin required) -- publish or unpublish a package service in the DNS zone. Accepts `{repo, name, published}`. Persists the choice and immediately registers/unregisters the records.

DNS read-only endpoints (`/dns/status`, `/dns/records`, `/dns/rbl/local`, `/dns/dnsbl/allowlist`, `/dns/services`, `GET /dns/tld`, `GET /dns/rbl`, `GET /dns/dnsbl`) are excluded from audit logging. The allowlist *writes* are audited (exempting a name from every blocklist is an accountable change); like the blocklist writes they mirror, they carry no named action in `account.RouteActions` — the path identifies them.

### RBL / DNSBL Blocklists

Rolodex (0.2.4+) provides three complementary spam/malware/ad blocking mechanisms, plus (0.4.3+) one mechanism for undoing them and one for not believing a provider that refused the query, all exposed through the DNS API and the `rolodex.Client` wrapper (`SetRblConfig`/`GetRblConfig`, `SetDnsblConfig`/`GetDnsblConfig`, `AddLocalRblEntry`/`RemoveLocalRblEntry`/`ListLocalRblEntries`, `AddDnsblAllowlistEntry`/`RemoveDnsblAllowlistEntry`/`ListDnsblAllowlistEntries`). All are **queried on demand by rolodex** — Town OS never downloads, parses, or pre-caches blocklist feeds.

Note the wrapper's two `Set*` methods take the list-wide refusal cooldown as a trailing argument (`SetRblConfig(ctx, enabled, providers, refusalCooldownSecs)`); they map onto upstream's `Set*ConfigWithRefusalCooldown`, since the arity-preserving upstream spellings exist for external API compatibility that an internal wrapper does not need.

- **RBL** (Realtime Blackhole List) -- reverse-IP blocklist zones queried on demand with a reversed IP against a zone (e.g. `zen.spamhaus.org`). Checked against IPs found in reverse DNS queries. Configured via `/dns/rbl` as a list of `{zone, enabled, refusal_codes, refusal_cooldown_secs}` providers plus a global enabled flag and a list-wide `refusal_cooldown_secs`.
- **DNSBL** (domain blocklist) -- domain blocklist zones queried on demand by prepending the looked-up domain to the zone (e.g. `googleadservices.com` + `dbl.spamhaus.org`). DNSBL listings take precedence over forwarded/iterative answers. Configured via `/dns/dnsbl` with the same shape as RBL, with its own independent cooldown.
- **Local RBL entries** -- a DB-backed list of names/IPs managed manually via `/dns/rbl/local*`, checked before external providers. A **domain-name** local entry blocks forward A/AAAA lookups for that domain with `NXDOMAIN`, and takes effect immediately (rolodex updates an in-memory cache on add).
- **DNSBL allowlist** (rolodex 0.4.3+) -- the operator's escape hatch from a third-party feed's false positive, managed via `/dns/dnsbl/allowlist*`. An entry covers the name **and every name beneath it**, so allowlisting `vendor.example` also exempts `cdn.vendor.example`. It **short-circuits the whole name-based check**, beating both the configured DNSBL providers and any matching local RBL entry, and it runs *before* the provider lookup so an exempted name never issues one. Also DB-backed with an in-memory cache, so it takes effect immediately.

  Without it the only remedy for a feed that lists a name the household needs is to disable the whole provider. Note the asymmetry with the local blocklist: an allowlist entry is a **name only**, never an IP, because the check it short-circuits is the name-based one. The IP-based RBL path is untouched by it.

  **Version floor:** an older rolodex answers the three allowlist RPCs with gRPC `Unimplemented`, surfacing as a 500. Neither `make test` nor the mocked integration tests catch that — `TestRolodexDnsblAllowlistRoundtripReal` is what proves the pinned image is new enough.

#### Refusal codes: a provider saying "stop asking" is not saying "this is listed"

A DNSxL answers a listing and a complaint about the querier with the **same kind of record** — an `A` under `127.0.0.0/8` — so the only thing separating them is the address. `127.0.0.2` means the name is listed; `127.255.255.254` means the query arrived via a public resolver and `127.255.255.255` means the querier is over its limit. Read the second kind as a listing and **every** name checked against that provider becomes `NXDOMAIN`: the blocklist stops being a blocklist and becomes an outage. Spamhaus publishes free-use limits a household box can cross without noticing, and the symptom when it does is the whole web going dark — which reads as broken DNS, not as a rate limit.

Rolodex recognizes these codes and, on a refusal, **rotates that provider out of the lookup rotation for a cooldown** rather than believing it. Town OS exposes both halves:

- **`refusal_codes`**, per provider, on both lists. Each entry is an IPv4 address or `address/prefix` — a prefix because providers document whole ranges, and Spamhaus reserves all of `127.255.255.0/24` for errors and adds codes to it over time, so enumerating today's three would silently start reading tomorrow's fourth as a listing.
- **`refusal_cooldown_secs`**, per provider and list-wide. A provider's `0` defers to the list value; the list's `0` uses rolodex's built-in default (3600).
- **`rotated_out`** on the `GET`, reporting which providers are currently not being asked, the code each refused with, and the seconds remaining. This is the operator-visible half: without it the only signal that a blocklist stopped being consulted is that it stopped blocking things.

**`ValidateRefusalCodes` (`controller_dns_validate.go`) mirrors rolodex's `resolve_refusal_codes` exactly**, because the list is passed through verbatim and disagreeing about what an entry means would be worse than not validating at all. Three cases:

- **empty** ⇒ rolodex substitutes its built-in set, so a configuration written before any of this existed gets the safe reading without being edited;
- **exactly `"none"`** ⇒ detection off, for a private blocklist whose real listings collide with a built-in code;
- **anything else** ⇒ exactly those codes, with the built-ins deliberately **not** merged in.

`"none"` mixed with real codes is rejected — a list that both disables detection and names codes to detect has no reading to pick. Codes are masked to their prefix and **a `/32` renders bare**, matching rolodex's `Display`: a code that read back differently from the one just submitted would look like the box had rewritten the operator's input.

**The `GET` reports codes RESOLVED**, so a provider that named none reads back carrying the built-in set — which is the point, since an operator has to be able to see what the box is actually matching on. It also means **a client must never echo that back on the next save**: doing so freezes today's list into the stored config, whereupon a code rolodex adds later starts being read as a listing — the exact failure this exists to prevent, reintroduced one layer up. `toWire` in `BlocklistsTab.jsx` collapses a resolved built-in set back to an absent field, and the UI keeps a copy of the built-in list (`BUILTIN_REFUSAL_CODES`) for one purpose only: deciding which radio the settings dialog opens on. If that copy drifts, the dialog opens on "Custom" prefilled with the codes in effect — a cosmetic wrong default, not a wrong configuration, since nothing changes unless the operator saves.

**Version floor:** a rolodex predating refusal handling accepts these fields — proto3 ignores unknown fields — and stores nothing. The mocked tests cannot tell that from success, because a mock echoes back whatever it was handed. `TestRolodexRblRefusalCodesRoundtripReal` and its DNSBL twin assert that an **empty** configured list reads back *resolved*, which is the assertion an old image fails.

There is **no feed ingestion / pre-caching**: provider zones are the unit of configuration, and the UI offers a curated list of well-known DNSBL/RBL zones as one-click quick-adds, but the user may add any zone. Provider-zone writes replace the whole config (validated, lowercased, de-duplicated).

**The quick-add list is an endorsement, and is curated on that basis** (`DNSBL_SUGGESTIONS` / `RBL_SUGGESTIONS` in `ui/src/routes/dns/BlocklistsTab.jsx`). A zone belongs there only if a household box can use it as shipped: still operating, free, and answering a self-recursing resolver with no registration step. Currently DNSBL — Spamhaus DBL, SURBL, URIBL, NordSpam DBL, Spam Eating Monkey; RBL — Spamhaus ZEN, SpamCop, PSBL.

Three are deliberately **absent**, and `TestBlocklistsTab`'s "offers no decommissioned or registration-gated zones" case keeps them that way: `dnsbl.sorbs.net` was decommissioned on 2024-06-05 and its zones emptied, so it is a permanent no-op that reads as protection; `b.barracudacentral.org` requires registering the querying IP first, and an unregistered box may answer for a while and then be cut off; UCEPROTECT levels 2/3 list whole ASNs, so one bad neighbour blocks an entire ISP. All three fail *silently* — the operator sees a configured zone and assumes it is working.

Note also that the RBL (reverse-IP) zones are only consulted for IPs found in reverse DNS queries, which ordinary browsing barely generates. The DNSBL (domain) zones are the ones that affect browsing, and they are tuned for spam URLs in email rather than ads or trackers — ad/tracker blocking would be feed territory, which is [deliberately out of scope](#rbl--dnsbl-blocklists).

### Per-Service DNS Publishing

Publishing is opt-out: every installed package service is published in the DNS zone unless its `repo/name` key is listed in the `dns_excluded_services` setting (a JSON array). `/dns/services/set` toggles membership and immediately registers/unregisters the records; `RebuildDNS` and `ReconcileDNS` filter excluded services (via `filterExcludedDNSInfo` + `loadDNSExcludedServices`) so the choice survives restarts and reconciliation. Unpublished services keep running but are not resolvable by name.

### DNS Management UI

The DNS management screen shows DNS status (enabled, running, TLD, record count) above four deep-linkable sub-tabs (`?tab=`):

- **Records** -- the DNS records table with dialogs for adding records (types: A, AAAA, CNAME, MX, TXT, SRV, PTR), removing records, changing the TLD, and initial DNS setup.
- **Blocklists** -- DNSBL and RBL provider-zone sections (global enable switch, per-zone enable/remove, per-zone refusal-code settings, suggested-zone quick-adds, custom-zone add — all queried on demand) plus a manual local-entry table (add/remove). Each section leads with the providers currently backed off after refusing a query, when there are any. No feeds, no apply, nothing cached.
- **Allow Lists** (`?tab=allowlists`, `ui/src/routes/dns/AllowListsTab.jsx`) -- the DNSBL allowlist: a table of exempted domains with reasons, plus add and remove. Reads are `requireAuth` so the tab is not admin-only; the add/remove controls are admin-only. It is a sibling tab rather than a card on Blocklists because an exemption is what an operator goes looking for by name when something is unreachable, not something to find while scrolling past provider zones.
- **Services** -- installed package services with a publish switch (publish/unpublish in the DNS zone).

## Status Endpoint

`GET /status/ping` (public) returns system status including: filesystem counts (user, installed, uninstalled), repository and package counts, installed package count, account and admin counts, service unit counts (total, active, failed), system service unit counts (total, active, failed), recent audit errors (last 5 minutes), setup status (`needs_setup` is true only when no enabled admin account exists; the login page is shown when admins exist regardless of session state), external IP (fetched hourly from ipinfo.io), internal IP (first non-loopback IPv4 address), disk usage statistics, upgrade availability, the server's UTC timezone offset in minutes, the current locale, `proton_enabled` (whether this build carries the `proton` build tag), `boot_id`, and the authenticated username if a valid token is provided.

Service unit counts are split into two fields: `units` counts only package service units (those matching `town-os-package--*`), while `system_services` counts system service units (those matching `town-os-system--*`). Leftover systemd units from uninstalled packages are excluded from the package count. The installed package list is cross-referenced with discovered systemd units by constructing the expected unit name from each package identity.

The handler lists accounts once (used for `needs_setup`, the total, and the admin count) and uses `FilesystemNames` rather than `ListFilesystems` for the volume counts — the latter runs `btrfs qgroup show` plus a rootid lookup per subvolume, which at ~30 subvolumes cost about a second of the ping's latency budget for a quota the ping never reads.

Unauthenticated requests from non-localhost origins receive a minimal response containing only `status`, `needs_setup`, and `boot_id`. `boot_id` is carried even there because the refresh flow polls ping across a controller restart, during which the browser is briefly unauthenticated; it is a random per-process UUID and discloses nothing about the system. Authenticated requests and all localhost requests receive the full response with all fields listed above, plus `repository_errors` (a map of repository name to error string tracking per-repository refresh failures).

While the controller is still booting, this path is served by the boot stub instead and returns **503** with `{booting, step, done, error, boot_id}` — see [Boot Status and Refresh](#boot-status-and-refresh).

### External IP Polling

The system controller fetches the server's public (external) IP address from `https://ipinfo.io/json`. The poller is started automatically when the HTTP handler is created (`NewHandler`) and when the Unix socket server starts. It fetches the IP immediately on startup, then polls every 1 hour. Each fetch has a 10-second HTTP timeout. The result is cached in an atomic value and included in authenticated ping responses as `external_ip`. Fetch failures are logged at debug level and do not affect the rest of the system; the field is omitted from the response when no IP has been fetched.

## Monitoring

An integrated Prometheus + Node Exporter monitoring stack provides system metrics. The `monitoring.Manager` manages the stack as systemd-supervised podman containers (system services) with `Restart=always`, using the `town-os-system--` naming prefix. The dashboard frontend is configurable via the `monitoring_backend` setting.

### Monitoring Port

Port **5308** is the dedicated monitoring dashboard port (`TOWN_OS_MONITORING_PORT` relocates it; likewise `TOWN_OS_PROMETHEUS_PORT` and `TOWN_OS_NODE_EXPORTER_PORT` for the two loopback ports — see [System-service host ports](#system-service-host-ports)). Ports reach the three services as a single `monitoring.Ports` value whose empty fields are filled by `withDefaults()`, so the defaulting lives in one place. The active backend determines what listens on the dashboard port:

- **uPlot mode** (default): a socat forwarder (`socat TCP-LISTEN:5308,fork,reuseaddr TCP:localhost:9090`) exposes the Prometheus HTTP API on port 5308. The React UI queries Prometheus's `/api/v1/query_range` directly and renders charts via uPlot.
- **Grafana mode**: Grafana listens on port 5308 directly (via podman port mapping). The React UI embeds a Grafana iframe.

There are **no reverse proxies** through the systemcontroller (port 5309). The browser talks to port 5308 directly for all monitoring data.

### Monitoring Backend Setting

The `monitoring_backend` system setting controls which dashboard frontend is used:

- `"uplot"` (default) -- lightweight built-in charts rendered in the React UI using uPlot (~35 KB). Queries Prometheus on port 5308 via the socat forwarder. Grafana is not pulled or started, saving ~771 MB on first boot.
- `"grafana"` -- full Grafana dashboards. The Grafana container image is pulled and started on port 5308. Pre-provisioned with a Prometheus datasource and every dashboard in the registry.

Changing the setting takes effect immediately: switching to `"grafana"` pulls the Grafana image and starts the container (stopping the socat forwarder); switching to `"uplot"` stops Grafana and starts the socat forwarder.

### Monitoring Containers

- **Node Exporter** (`quay.io/prometheus/node-exporter:latest`, host port 9100) -- collects host system metrics. Runs with host PID namespace, `SYS_TIME` capability, and a read-only bind mount of the host root filesystem at `/host`. The systemd unit passes `--collector.diskstats.device-exclude=^(ram|fd)\d+$` (the `monitoring.DiskstatsDeviceExclude` constant) to override node_exporter's upstream default (`^(ram|loop|fd|(h|s|v|xv)d[a-z]|nvme\d+n\d+p)\d+$`), which filters out partitions (`sda3`, `nvme0n1p3`) and loop devices — exactly the device shapes `monitoring.BtrfsDevices` reports for the btrfs filesystem backing `/town-os`. Without this override the Disk I/O dashboard queries silently return zero series and the panel renders empty. Do not remove or loosen the flag unless you also move the Disk I/O queries off `node_disk_*`. Regression coverage: `TestNodeExporterUnitConfigDiskstatsExcludeAllowsRealDevices` pins the flag and regex, and `TestMonitoringNodeExporterEmitsDiskMetricsForFilteredDevices` starts a real node_exporter container and confirms it emits `node_disk_read_bytes_total` for at least one upstream-default-excluded device.
- **Prometheus** (`quay.io/prometheus/prometheus:latest`, host port 9090) -- scrapes Node Exporter, itself, rolodex (job `rolodex`), and the system controller (job `systemcontroller`, see [System Controller Metrics](#system-controller-metrics)) at 15-second intervals. The two optional jobs are omitted when their address is unset rather than aimed at a guessed default, since a target nobody configured sits permanently down and reads as a broken service instead of an absent one. Data is stored with 30-day retention in a persistent data directory. Configuration and data volumes are bind-mounted from a monitoring data directory. The systemd unit includes `ExecStartPre` mkdir directives to pre-create volume directories on boot.
- **Grafana** (`docker.io/grafana/grafana:latest`, host port 5308) -- optional dashboarding UI, only started when `monitoring_backend` is `"grafana"`. Uses a light theme (`GF_USERS_DEFAULT_THEME=light`). Anonymous viewing is enabled with the Viewer role, iframe embedding is allowed. The systemd unit includes `ExecStartPre` mkdir directives to pre-create volume directories on boot. Pre-provisioned with a Prometheus datasource and the dashboards described in [Dashboards](#dashboards); see [Dashboard Provisioning](#dashboard-provisioning) for how they get there.
- **Socat forwarder** -- the `monitoring-ui` unit (`town-os-system--monitoring-ui.service`) in its uPlot form, started only when `monitoring_backend` is `"uplot"`. Forwards port 5308 to Prometheus on port 9090. It is the *same unit key* Grafana uses, not a second one: the two are alternative bodies for one service, which is what lets a backend change be a unit rewrite and restart rather than a pair of start/stop calls that could leave both or neither running.

### Dashboards

There are two dashboards, and **both backends render the same two from the same
queries**. They are separate rather than one long page because they answer
different questions: System is what an operator watches when the box feels slow,
DNS is what they open when a name will not resolve. Folding the eight DNS panels
into the overview would bury the four host panels that are the reason anyone
opens it.

**System** (Grafana uid `town-os-overview`, "Town OS Overview") -- four panels:

1. **Disk I/O (/town-os)** -- read/write throughput summed across the block devices backing the btrfs filesystem, so the panel shows one Read and one Write line however many devices the filesystem spans. The device regex is substituted in from `monitoring.BtrfsDevices`; an empty list resolves to `NoBtrfsDevicesSentinel`, which matches nothing, so the panel renders empty rather than silently summing every disk on the host.
2. **Network (External)** -- receive/transmit in bits/sec per physical device (excluding `lo`, veth, podman, cni, tailscale, bridge and docker), joined against `node_network_up == 1` so interfaces that once existed but are now down do not run flat zero lines off the legend.
3. **CPU Usage** -- stacked by mode (user, system, iowait, irq, softirq, steal, nice) with a Total overlay line, 0--100%.
4. **Memory Usage** -- total, used, available.

**DNS** (Grafana uid `town-os-dns`, "Town OS DNS") -- eight panels over the
`rolodex` scrape job:

1. **DNS Queries by Response Code** -- `rate(rolodex_dns_queries_total)` summed by `rcode`, stacked. The split is the panel rather than a drill-down because a bare query count cannot tell a busy resolver from one SERVFAILing everything — those are the same line.
2. **Query Latency** -- p50/p95/p99 from `rolodex_dns_query_duration_seconds_bucket`. The buckets are summed by `le` *before* `histogram_quantile`, because the raw series carry a `proto` label and quantiling them unaggregated draws one line per transport instead of the box-wide latency.
3. **Answers by Source** -- which resolution stage answered (cache, local, scoped, an upstream tier), stacked. This is the panel that says whether the box is answering for itself or forwarding.
4. **Cache Hit Ratio** -- hits plus negative hits over all lookups, 0--100%. A cached NXDOMAIN counts as a hit: it saved an upstream round trip exactly as a positive one did. The denominator is deliberately unclamped, so an idle box breaks the line rather than drawing a confident 0% for a cache nothing has asked anything.
5. **Cache Entries** -- positive, negative, and blocklist cache sizes.
6. **Blocklist Activity** -- blocks by kind, allowlisted, and **refused**. Refusals share the panel with blocks on purpose: a provider answering "stop asking" rather than "listed" is what silently turns a blocklist into an outage ([Refusal codes](#refusal-codes-a-provider-saying-stop-asking-is-not-saying-this-is-listed)), and it only reads as anomalous beside the block rate it replaced.
7. **Upstream Tier Outcomes** -- wins and failures per tier, plus queries that exhausted every tier.
8. **DNS Traffic** -- wire bytes rx/tx.

Every DNS query carries a `{job="rolodex"}` selector built from
`monitoring.RolodexJobName`, so the label the scrape config emits and the one the
dashboards select on cannot drift apart — a mismatch is not an error anywhere,
it is eight panels reading empty on a box whose DNS is working.

The two frontends are separate code in separate languages rendering the same
dashboard, and the **only** difference is the rate window: Grafana expands
`$__rate_interval` per panel, and the uPlot frontend has no macro expansion, so
it pins `RATE_INTERVAL` (`5m`). A leaked macro on the uPlot side is a Prometheus
parse error that blanks the whole tab.

Three tests hold the two sides together, because nothing else connects them:

- `TestRolodexDashboardMirroredInFrontendQueries` reads `ui/src/components/monitoring/queries.js` from the Go test and fails if either side names a rolodex metric family the other does not — the same drift guard `TestBootStepsFrontendInSyncWithBackend` applies to the boot stages.
- The rolodex scrape integration test asserts the **pinned rolodex image actually exports** every family in `monitoring.RolodexDashboardMetrics()`, matched on the `# TYPE` line so a family whose name is a prefix of another cannot vouch for a missing one. A panel naming a family the daemon does not emit renders an empty chart, which is indistinguishable from an idle resolver.
- `TestDashboardQueriesParseInPrometheus` runs every expression from every dashboard past a real Prometheus. Malformed PromQL inside JSON is a syntax error nowhere: the file provisions, the dashboard loads, the panel draws its axes, and it says "No data" forever.

### Dashboard Provisioning

`monitoring.GrafanaDashboards(diskDevices)` (`src/monitoring/dashboard.go`) is
the registry — filename, uid, title, and rendered JSON per dashboard — and
`WriteGrafanaProvisioningFiles` iterates it. Adding a dashboard is an entry
there and nothing else: the provisioner (`GrafanaDashboardProviderYAML`) points
at the `dashboard-json` **directory**, so every file in it is picked up. Before
the registry existed the file writer was the de-facto list, which meant a second
dashboard could only be added by editing code that has nothing to do with
dashboards.

The uids are constants (`OverviewDashboardUID`, `DNSDashboardUID`) because the
web UI deep-links them. A drifted uid produces no error anywhere — Grafana
serves a "dashboard not found" page inside the iframe.

The DNS dashboard is **built from panel specs and marshalled**
(`src/monitoring/dashboard_dns.go`) rather than concatenated into a JSON
template the way the older overview dashboard still is. Malformed JSON in a
dashboard does not cost a panel; it fails provisioning, and the dashboard never
appears at all. Panel targets carry the object-form datasource ref
(`{"type":"prometheus","uid":GrafanaDatasourceUID}`) — Grafana 13+ cannot
resolve the legacy string form in a target and renders "No data" with no error.

### Lifecycle

Prometheus and Node Exporter are always started on boot. The monitoring backend setting determines whether Grafana or the socat forwarder is also started. Startup failures are non-fatal; the system continues without monitoring. Systemd handles restarts via its `Restart=always` policy. The `Stop()` method is a no-op because system services persist across controller restarts.

### Monitoring API

- `GET /monitoring/status` (auth required) -- returns `backend` (`"uplot"` or `"grafana"`), a running flag per service (`prometheus`, `node_exporter`, `monitoring_ui`, and `grafana` only in Grafana mode), and `disk_devices`: the kernel device basenames backing the btrfs filesystem, which the frontend substitutes into the Disk I/O query. Empty `disk_devices` means discovery failed and the panel falls back to a regex that matches nothing. Returns `{"status": "disabled"}` when monitoring is not configured. Per-service image and unit metadata is not here — that is `GET /system-services`.
- `GET /metrics` (localhost or admin) -- the system controller's own Prometheus endpoint. See [System Controller Metrics](#system-controller-metrics).

### System Controller Metrics

The controller exports its own state in the Prometheus text exposition format on **its existing listener** (`:5309`, `MetricsPath = "/metrics"`), not on a port of its own. That is deliberate: the endpoint then rides the listener the harness already relocates with `TOWN_OS_LISTEN`, so there is no additional host port to add to `SYSTEM_PORT_FILES` and no way for a `make test-full` and a `make dev` to collide on it — IRON RULE.

It is **localhost-or-admin**, not public. The scrape aggregates account counts, disk usage, and which services are down: a map of what to attack and when the box is least able to resist. Prometheus runs `--net host`, so it reaches loopback with no podman-network hop, exactly like the node-exporter target.

`src/metrics` renders the format in a few hundred lines rather than depending on `prometheus/client_golang`, for the same reason `errgroup` was kept out. The library's value is its registry, collector interface, and histogram machinery — none of which is used, since every value here is either a process-lifetime tally or read from a manager per scrape — while its transitive tree (`prometheus/common`, `procfs`, protobuf) is real and lands in an image that boots from RAM.

**Label-value escaping is load-bearing, not defensive.** Label values carry operator input (a repository name, a package name, a systemd unit). An unescaped quote does not corrupt one line — it makes Prometheus reject the *entire* scrape, so one oddly-named package would silently take all monitoring down.

What is exported:

| Metric | Type | Notes |
|---|---|---|
| `townos_up` | gauge | always 1 while serving; absent when not |
| `townos_start_time_seconds` | gauge | uptime is `time() - this`, in the scraper's clock |
| `townos_package_units{state}` | gauge | `active`/`failed`/`inactive`, filtered to installed packages |
| `townos_system_units{state}` | gauge | `town-os-system--*`, excluding NC and socket units |
| `townos_package_unit_active{unit}` | gauge | per-unit 1/0, so an operator sees *which* service is down |
| `townos_system_unit_active{unit}` | gauge | ditto for system services |
| `townos_packages_installed` / `townos_packages_available` | gauge | inventory |
| `townos_repositories` / `townos_repository_errors` | gauge | errors counted, not labelled by name |
| `townos_upgrades_available` | gauge | |
| `townos_accounts{kind}` | gauge | `admin`/`user`/`disabled` |
| `townos_accounts_granted` | gauge | non-admins holding at least one grant |
| `townos_filesystems{state}` | gauge | `user`/`installed`/`uninstalled` |
| `townos_disk_total_bytes` / `_used_bytes` / `_available_bytes` | gauge | |
| `townos_audit_recent_errors` | gauge | the same number the dashboard's red pill renders |
| `townos_audit_events_total{result}` | counter | `success`/`failure`, incremented by `auditMiddleware` |
| `townos_http_requests_total{method,status}` | counter | status is a **class** (`2xx`…), never the exact code |

Several choices here are the point rather than incidental:

- **A scrape never fails as a unit.** Each collector tolerates a nil manager and logs-and-skips on error. A 500 because one subsystem is sick makes every other metric vanish at exactly the moment they are wanted, so the box reads as entirely dead rather than partly degraded — and a scrape during boot should report what is up.
- **Zero buckets are still emitted.** A gauge that disappears at zero is indistinguishable from one the box stopped reporting, so "no failed units" would look exactly like "unit collection is broken".
- **Status is bucketed by class.** Every distinct code would become a permanent series, and a control plane answering 400/401/403/404/409/422 across dozens of routes multiplies out fast for a question nobody asks of a household box. The exact code is already in the audit log and the request log.
- **The counters are in-memory and per-process.** A counter that survived a restart would describe the box's history rather than this process's, and Prometheus already understands a reset. It also keeps a scrape — and the audit middleware feeding it — off the database entirely.
- **`/metrics` is excluded from audit logging** and from its own request counter. A 15s scrape would otherwise write ~5,700 audit rows a day describing nothing an operator did, and would dominate the counter it serves.
- **`metricsMiddleware` is registered outermost** of the three (before audit and the grant allowlist) so a request denied by either gate is still counted — an unexplained 403 is exactly what the counter exists to surface. It takes the status from the returned error, because a handler that returns one has not written its status yet.

**The scrape target is not recomposed anywhere.** `MetricsScrapeTarget(listenAddr)` derives it from the same string the server binds and `main.go` hands the result to `monitoring.Ports.ControllerMetrics` — the same single-source-of-truth reason `PackageNetworkState.FQDN` and `Manager.MetricsAddr()` exist. A wildcard bind (`:5309`, `0.0.0.0:5309`, `[::]:5309`) is rewritten to `localhost` because a wildcard is not an address anything can connect to; an explicitly pinned host is left alone, since rewriting it would aim the scrape at an address the controller is deliberately not on. An empty result omits the job rather than aiming it at a guess. When `TOWN_OS_TLS` is on, `ControllerMetricsScheme` is `https` and the job also carries `insecure_skip_verify` — the leaf is issued by the box's own CA, which Prometheus has no reason to trust and no clean way to be handed, and the scrape is loopback inside the host namespace so nothing else can answer as it.

### Monitoring UI

The monitoring tab in the sidebar navigation opens a dashboard page carrying
**System / DNS sub-tabs**, deep-linkable as `?tab=system|dns` like every other
sub-tabbed screen, so a dashboard somebody is watching during an outage survives
a reload and can be linked to. An unknown `?tab=` value falls back to System
rather than rendering nothing. The tab list is one array holding both the uPlot
component to mount and the Grafana uid that shows the same panels, so a tab
cannot exist in one backend and not the other.

Rendering depends on the `backend` field from the status response:

- **uPlot mode**: panels rendered directly in React using uPlot, querying Prometheus on port 5308. The System grid pins itself to the viewport (four panels, two per row); the DNS grid does **not** — eight panels squeezed into one screen leaves each about 100px of canvas, at which point a latency chart is decoration, so panels get a fixed height and the page scrolls.
- **Grafana mode**: an embedded Grafana iframe targeting port 5308 in kiosk mode with light theme. Switching tabs repoints the frame at the other dashboard's uid, and the iframe is keyed on that uid so the frame is *replaced* rather than navigated — Grafana keeps its own history, and a src swap on a live frame leaves the browser Back button stepping through dashboards instead of leaving the page.

Panel titles are identical across the two backends: an operator who switches
should not have to work out which panel became which. They are hardcoded English
— this screen carries no `t()` calls, and a Grafana panel title cannot be
translated in any case, since it lives in the provisioned JSON.

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

Clicking a service row — either the status icon or the package name — navigates to `/dashboard/system?search=<package_identifier>`, the service's own row on the services screen. The services screen seeds its filter box from `?search=` and passes the term to `GET /systemd/units-tree`, whose search matches a root's own fields, so the screen opens on that one package with its dependency subtree rather than on the full list. The term is a starting value, not a lock: clearing or editing the box widens the list back out. The link carries the raw `package_identifier`, never the pretty `display_identifier` — the latter is not a term the tree search can match, so a link built from it would land on an empty tree. The panel is hidden when no services are installed. Notes are fetched once per service and cached.

### Layout

The dashboard uses a two-panel layout: a sticky left sidebar and a right content area with a sticky top header bar.

**Sidebar** -- a 256px-wide (`w-56`) vertical panel with the Town OS logo and brand text in a gray banner at the top, followed by vertically stacked navigation buttons (each with an icon and label). Active routes use `variant="secondary"`, inactive use `variant="ghost"`.

**Top status bar** -- a right-aligned horizontal bar showing: connection status pill (loading/offline/online), system services failure count (red pill badge linking to `/dashboard/system?expand=system` when `system_services.failed > 0`), logged-in username with admin badge, and logout button.

## System Services

System services are systemd-managed infrastructure containers (distinct from user-installed package services). They use the `town-os-system--` unit name prefix.

The set is: rolodex, the ingress, pages, the UI, node-exporter, Prometheus, the monitoring UI (socat forwarder or Grafana), and **one gfeh partition per network** (`town-os-system--gfeh-<network>`). Everything in that list must register in `collectSystemServices()` so `POST /system-services/refresh` re-pulls and restarts it — an omission there is invisible until an upgrade silently leaves the service on its old image.

### System Service Unit Generation

`GenerateSystemServiceUnit` produces podman-based systemd units with `Restart=always`. The unit config supports a `VolumeDirs` field listing host directories to pre-create via `ExecStartPre=/bin/mkdir -p <dir>` lines, preventing mount failures when containers start on reboot before the system controller has run.

### System Service API

- `GET /system-services` (localhost or auth) -- list system services with live unit status. Each entry includes key, display name, image, port, and systemd unit status fields. Returns an empty list when monitoring is not configured. Excluded from audit logging.
- `POST /system-services/status` (admin required) -- change a system service's status. Accepts key and action (`start`, `stop`, `restart`). The `enable` and `disable` actions are rejected.
- `POST /system-services/refresh` (admin required) -- refresh system service status.

## Web UI Production Image

An independent UI container image (`quay.io/town/ui`) is built from `Containerfile.ui`. It uses a two-stage build: `oven/bun:latest` builds the UI static files, then `docker.io/library/caddy:latest` serves them on port 80 with SPA routing (`try_files {path} /index.html`). The UI is reached through the shared ingress rather than squatting the host's `:80` directly — it is the ingress's default `:80` backend for any host not matched by a route, so bare-IP login keeps working.

**Cache headers are load-bearing** (`Caddyfile.ui`). Everything under `/assets/*` is fingerprinted by Vite, so an asset URL names one exact build for all time and is served `public, max-age=31536000, immutable`. `index.html` is the one file Vite does **not** fingerprint, and it is what names the current bundle; served with no `Cache-Control` at all, a browser may apply heuristic freshness (RFC 9111 §4.2.2) and reuse its cached copy without revalidating, so an upgraded box goes on handing out the previous release's `index.html` pointing at the previous release's bundle. The symptom is an upgrade that appears not to have happened — new features render as if the UI had never heard of them. Every non-asset path is an SPA route that `try_files` resolves to `index.html`, so the `no-cache` rule is written to cover all of them (`@html not path /assets/*`).

`make release-ui` builds with `--no-cache` so a `push-rc` always ships freshly built UI assets rather than a layer-cached bundle.

**Tests never pull the quay UI image** — the `ui-image` make target builds `Containerfile.ui` locally as `localhost/town-os-ui:<INSTANCE_ID>` (always matching the host arch and the in-repo UI source), saves it to the image cache, and the test harness loads it into test containers and injects it via the `UI_IMAGE` env var. `test-integration-build` and `test-ui-integration` depend on `ui-image`. The quay.io/town/ui tags are for production/release pushes only. `uiTestImage` in `integration/systemcontroller_ui_test.go` skips its test when `UI_IMAGE` is unset rather than falling back to a quay tag.

## Proton Runner Image

The Proton runner image (`quay.io/town/proton`) is built from `Containerfile.proton`. It uses a two-stage build: a downloader stage fetches the GE-Proton release tarball (pinned via `GE_PROTON_VERSION` build arg), and the runtime stage installs Wine/Proton dependencies (64-bit + 32-bit), Xvfb for headless operation, and a wrapper script at `/usr/local/bin/proton` that starts a virtual framebuffer and configures the Proton environment before executing the application.

The make pipeline provides: `release-proton-image` (build), `push-proton-rc` (push per-arch release candidate tags `rc.<date>-<arch>` + `rc.latest-<arch>`), and `push-proton-release` (push per-arch release tags `release.<date>-<arch>` + `latest-<arch>`). The proton image is also included in the full `push-rc` / `push-release` flows and the `manifest-rc` / `manifest-release` assembly when `PROTON_ENABLED=1`.

## Web UI API Client

The browser determines the API base URL at runtime from `window.location`, using the current protocol and hostname with port 5309 (e.g., `https://myhost:5309`). No server-side proxy is involved; the browser talks directly to the system controller API.

The `VITE_API_URL` environment variable overrides the browser-derived URL when set. This is useful during development when the API server runs on a different host or port.

The monitoring dashboard derives its monitoring port URL (port 5308) from the current hostname. When `VITE_API_URL` is set, the hostname is extracted from it; otherwise `window.location.hostname` is used.

## Web UI Accessibility

All dialog components include a `DialogDescription` element providing a concise description of the dialog's purpose. This satisfies the Radix UI accessibility requirement for screen readers and eliminates `aria-describedby` warnings. Descriptions are placed inside the dialog header after the title and are visible to all users.

## Internationalization

All user-facing strings (UI labels, error messages, toast notifications, audit log action descriptions) are translatable via a message catalog pattern.

### Backend

The `i18n` package provides a `T(locale, key, args...)` function that resolves translation keys. The fallback chain is: requested locale, then `en-US`, then the raw key string. When `args` are provided, `fmt.Sprintf` formatting is applied. Message keys use dot-separated namespaces (e.g., `auth.login_failed`, `pages.toast_provisioned`).

### Populated Catalogs

Backend catalogs live one file per locale in `src/i18n` (`de_de.go`, `zh_cn.go`, …); the frontend mirror lives in `ui/src/i18n` (`de-DE.js`, `zh-CN.js`, …). The two sides are kept in lockstep — every populated backend catalog has a frontend twin.

`PopulatedLocales()` is the authoritative list (48 entries): `en-US`, `ar-AE`, `ar-EG`, `ar-SA`, `bn-BD`, `bn-IN`, `cs-CZ`, `da-DK`, `de-AT`, `de-CH`, `de-DE`, `en-AU`, `en-CA`, `en-GB`, `en-IN`, `en-NZ`, `en-ZA`, `es-AR`, `es-ES`, `es-MX`, `fi-FI`, `fr-BE`, `fr-CA`, `fr-CH`, `fr-FR`, `hi-IN`, `hr-HR`, `hu-HU`, `it-IT`, `ja-JP`, `ko-KR`, `nl-BE`, `nl-NL`, `pl-PL`, `pt-BR`, `pt-PT`, `ro-RO`, `ru-RU`, `sa-IN`, `sk-SK`, `sl-SI`, `sv-SE`, `th-TH`, `tr-TR`, `uk-UA`, `vi-VN`, `zh-CN`, `zh-TW`. Anything not on it falls back to English. `IsPopulated(code)` is what the UI uses to disable an unpopulated entry in the language picker.

The list is **derived from the catalog map rather than written out**: `buildPopulatedLocales()` reads the keys of `catalogs` at init, sorts them, and pins `en-US` to the front, and `IsPopulated` indexes `catalogs` directly. It used to be a hand-maintained slice literal, which had exactly one failure mode and it was silent — a catalog registered in `catalogs` but forgotten in the literal was translated, shipped, and never offered in the picker. `PopulatedLocales()` returns a clone, because the list is package state now rather than a fresh literal per call and a caller that sorts or truncates the result must not be able to disturb the next one.

### Country Variants

A catalog is one of two kinds, and the difference is in how the file is written, not in how it is selected — both kinds are populated and both appear in the picker.

A **language catalog** is a translation, written out in full: `de_de.go`, `cs_cz.go`, `ja_jp.go`.

A **country catalog** is built by `derive(base, overrides)` (`src/i18n/derive.go`, mirrored by `ui/src/i18n/derive.js`) from the catalog of the language it belongs to plus only the strings that country states differently. Austrian German is German; the question `de_at.go` answers is not "how do you say this in German" but "which of these sentences would an Austrian not have written". Copying `de-DE` into `de_at.go` and editing four lines would mean the next message key added to `de-DE` silently reaches Austria in English, and a fix to a German string has to be found and repeated in three files. Inheriting the base and listing only the departures keeps a variant correct by default: a new key lands everywhere the moment its base language has it.

Eighteen locales are derived this way:

| Base | Derived from it |
| --- | --- |
| `en-US` | `en-CA`, `en-GB` |
| `en-GB` | `en-AU`, `en-IN`, `en-NZ`, `en-ZA` |
| `de-DE` | `de-AT`, `de-CH` |
| `fr-FR` | `fr-BE`, `fr-CA`, `fr-CH` |
| `es-ES` → `es-latam` | `es-AR`, `es-MX` |
| `pt-BR` | `pt-PT` |
| `nl-NL` | `nl-BE` |
| `ar-SA` | `ar-AE`, `ar-EG` |
| `bn-BD` | `bn-IN` |

`es-latam` (`src/i18n/es_latam.go`, `ui/src/i18n/es-latam.js`) is the one intermediate: it holds the departures from peninsular Spanish that every American variety shares — `inválido` over `no válido`, `agregar` over `añadir`, straight quotes over `« »` — and both `es-AR` and `es-MX` build on it. **It is not registered in `catalogs` and is not selectable**, because it is a shared fragment rather than a place anyone lives; advertising it would offer a country code that is not one.

Some override maps are small and several (`en-CA`, `de-CH` on the backend, `es-MX`) are empty. That is the honest answer for a technical control panel — Canadian English keeps the American `-ize` spellings, and no message in `de_de.go` contains a `ß` for Switzerland's `ss` rule to reach (the frontend `de-CH.js` does carry real overrides, because `de-DE.js` uses `ß`). An empty override map still marks the locale as deliberately reviewed rather than forgotten.

The scheme is held by tests on both sides (`src/i18n/derive_test.go`, `ui/src/i18n/derive.test.js`): every override key must exist in its base, every override must actually differ from the base string it replaces, every derived catalog must carry its base's full key set, and every derived catalog must be listed in the test's `variants()` table — so a country catalog cannot be shipped without those rules applying to it.

**Every locale code carries a region subtag**, and `TestLocaleCodesAreRegionQualified` holds it. Sumerian (`sux`) was the one exception — a bare ISO 639-3 code — and it is gone. It was removed for its script rather than its shape: cuneiform lives in `U+12000`–`U+1254F`, which almost nothing ships a font for, so on any box without Noto Sans Cuneiform every string in the locale painted as replacement boxes. The romanization the catalog carried in parentheses survived, which made it worse than blank — Latin fragments and punctuation around holes. Rendering it honestly meant vendoring a webfont (the catalog used 45 distinct codepoints, but the full face is 462K and subsetting wants `fonttools` on the build host) and adding `@font-face` machinery the UI has none of, which is a lot of apparatus for a language with no speakers.

### Locale Lists

BCP 47 locale codes are used throughout. Two curated lists are provided:

- **CommonLanguages** (21 entries) -- Arabic (ar-SA), Bengali (bn-BD), German (de-DE), English (en-US), Spanish (es-ES), French (fr-FR), Hindi (hi-IN), Italian (it-IT), Japanese (ja-JP), Korean (ko-KR), Dutch (nl-NL), Polish (pl-PL), Portuguese (pt-BR), Russian (ru-RU), Sanskrit (sa-IN), Swedish (sv-SE), Thai (th-TH), Turkish (tr-TR), Ukrainian (uk-UA), Vietnamese (vi-VN), Chinese (zh-CN). Each entry includes the native-script name and English name.
- **ExtendedLocales** (89 entries) -- comprehensive list of country-specific locale variants (e.g., de-AT, en-GB, es-MX, fr-CA, pt-PT, zh-TW).

### Frontend

A React context provider (`I18nProvider`) wraps the application and exposes a `useI18n()` hook returning `{ locale, setLocale, syncServerLocale, t }`. The `t` function resolves keys against the frontend catalog with the same fallback chain as the backend. Parameter interpolation uses `{name}` placeholders (e.g., `t('greeting', { name: 'Alice' })`).

`translateIn(locale, key, params)` is exported alongside it and translates in a named locale rather than the active one, with the same fallback chain. It exists for the message that confirms a language change: `t` closes over the locale of the render it was called from, so a confirmation fired from the language form would be written in the language being *left* — the one message on the page whose subject is that that language is no longer in use.

### Locale Detection, Storage, and Sync

The UI picks its language **from the browser first**, not from the global setting. On load it reads `navigator.languages` and matches the ordered preferences against the shipped catalogs. Matching is case-insensitive and tries exact tags across all preferences before falling back, in this order:

1. **Exact match.** `de-CH` now ships a catalog, so `de-CH` resolves to `de-CH` rather than folding to `de-DE`.
2. **Chinese by script/region.** `zh-Hant` or a `TW`/`HK`/`MO` region → `zh-TW`, otherwise `zh-CN`. Script is a stronger signal than any default, so this runs before the two rules below.
3. **A named regional default.** Countries that ship no catalog but read a variant rather than their language's default: Spanish-speaking Latin America → `es-MX`, Lusophone Africa and Timor → `pt-PT`, and the Englishes of Ireland, Africa, and South/Southeast Asia → `en-GB`. Without this, `es-CO` would get peninsular Spanish and `en-IE` would get American.
4. **A named language default.** `ar` → `ar-SA`, `bn` → `bn-BD`, `de` → `de-DE`, `en` → `en-US`, `es` → `es-ES`, `fr` → `fr-FR`, `nl` → `nl-NL`, `pt` → `pt-BR`.
5. **Any catalog sharing the primary subtag.**

Steps 3 and 4 exist because the fallback used to be step 5 alone, and that was only correct while each language had exactly one catalog. Eight languages now ship more than one: a browser asking for plain `en`, or for `en-PH`, would otherwise land on whichever English is declared first in the `catalogs` object, making the answer a property of import order rather than a decision anyone made.

Precedence, highest first:

1. an explicit choice, persisted **per browser** in `localStorage` — *pinned*
2. a browser-detected language matched to a shipped catalog — *pinned*
3. the server's global `locale` setting, applied later via `syncServerLocale` — *not pinned*

Once the locale is pinned, `syncServerLocale` is a no-op. This is the point of the split: the 60-second status ping used to call `setLocale` and so forced the admin's global `locale` setting onto every browser on every poll. The `locale` setting (system-wide, default `en-US`, still reported in the ping response) is now only the fallback for a language Town OS does not ship a catalog for.

### Locale API

- `GET /locales` (auth required) -- returns the current locale, list of populated locales, common languages, and extended locales. Excluded from audit logging.

### Settings UI

The system settings page includes a language picker. Common languages are shown in a dropdown with native-script names. An expandable section reveals the extended locales list. Unpopulated locales (those without a translation catalog) are displayed with an asterisk suffix and are disabled in the selector, preventing selection.

The picker opens on **the locale the page is rendered in** — the one `useI18n()` holds — not on the `current` value from `GET /locales`. Those two disagree in the ordinary case, because the browser picks the locale and pins it while the global `locale` setting sits at its `en-US` default (see [Locale Detection, Storage, and Sync](#locale-detection-storage-and-sync)); preselecting `current` made the control read "English" on a page that was not in English. When the active locale is a country variant it lives in the collapsed extended list, so the list is expanded on load rather than leaving the select on a value none of its visible options carry; collapsing it again keeps that one entry rendered, for the same reason. `current` is used only as a fallback, for an active locale the server does not offer.

Saving compares the choice against **both** of those. Matching only one is still work: same as the server but different from the page means switch the page (`setLocale`, which pins the choice for this browser) without writing the setting; same as the page but different from the server means write the setting. Only when the choice matches both is there nothing to be done. The success toast is written with `translateIn` in the language just chosen, since the UI behind it has already switched; the nothing-to-be-done toast stays in the language on screen, because nothing changed. Comparing against `current` alone made the displayed language unselectable — pressing Save on it reported "nothing to be done", so returning to English required saving a third language first.

## System Controller Configuration

### Startup Sequence

The authoritative step-by-step boot ordering lives in [System Controller Boot Sequence](#system-controller-boot-sequence). In summary:

1. `setupPodmanEnv()` points `CONTAINER_HOST` at the host podman socket.
2. Flag parsing, then `:5309` is bound immediately with the boot-status stub.
3. Directory creation, stale-root-DB cleanup, database, and the account (plus legacy-service-account purge), session, audit, settings, pages, and network managers — the last of which seeds the home network.
4. Repository seeding, repository root force-refresh.
5. Install manager, btrfs storage, systemd manager; image tag resolution.
6. Rolodex config write + readiness wait (rolodex itself is supervised by systemd).
7. Core image pulls (NC, monitoring, UI) and monitoring system service starts.
8. Local TLS CA, ingress, and pages service.
9. Object-storage reconcile (one gfeh partition per network).
10. Version-change detection, reconcile, post-update commands.
11. DNS rebuild, network reconcile, a second (idempotent) object-storage reconcile, ingress programming, UI container start.
12. Freshness stage (per-package restarts after a refresh).
13. Handler construction and the atomic swap from the boot stub to the full router.
14. Background publication of the object-storage names, once a partition answers.

Startup failures for monitoring, Rolodex config, core image pulls, the TLS CA, the ingress, the pages service, object storage, network reconcile, and the UI container are non-fatal; the system continues without them. All container image pulls use the `ensureImage` helper which checks `podman image exists` before pulling, avoiding redundant pulls in test/dev environments where images are pre-loaded. Pull failures for non-essential services are logged to stderr and do not prevent startup, allowing the system to boot even when the network is temporarily unavailable.

### Version Tag Detection

The system controller derives matching image tags for every sibling service (UI, Rolodex, network controller, ingress) from a single tag resolved by `resolveImageTag()`: the `TOWN_OS_TAG` env var if set, else `rc.latest-<arch>` (`defaultVersionTag()`, arch from `runtime.GOARCH` mapped to `x86_64`/`aarch64` via `archTag()`). There is no compile-time `Version` pin and no `/town-os.tag` file — both were removed because a stale value in either one silently held every sibling image back on an old tag even after the controller advanced. The install build system pins a specific tag by setting `TOWN_OS_TAG` on the systemcontroller systemd unit (`../install/make/install.sh` derives it from `CONTROLLER_IMAGE`); with no override the fleet always tracks `rc.latest-<arch>`. This tag constructs image references like `quay.io/town/ui:<tag>` and `quay.io/town/rolodex:<tag>`; pushed tags are per-arch, so every derived sibling tag carries the arch suffix.

### Error Format

All API errors are returned as RFC 9457 Problem Detail objects (structured JSON with type, title, status, and detail fields). A custom `ProblemDetailHTTPErrorHandler` is set as the Echo error handler.

### Request Logging

Echo's `RequestLogger()` middleware is enabled globally, logging all HTTP requests to stderr. The verbosity is controlled by the `LOG_LEVEL` environment variable.

### Login Throttling

`POST /account/authenticate` is public and every attempt costs one argon2id hash at 64 MiB. That is the right cost for a password hash and the wrong thing to let an unauthenticated caller schedule without limit: a few hundred concurrent attempts is tens of gigabytes of allocation on a box whose whole design point is running from RAM, and the failure is not a slow login — it is the OOM killer taking the controller.

Two independent limits, because they answer different questions. `loginLimiter` caps **attempts per source** over a window (20 per 5 minutes), which is what makes online password guessing infeasible, and it is keyed per source address so one abusive client cannot lock out the household. `loginGate` caps **concurrent hashes** across all sources (4, bounding peak argon2 memory near a quarter gigabyte), which is what the per-source limiter alone cannot do. Both are in-memory and per-process: they protect this process's memory and CPU, and persisting them would make a failed login a database write.

Both are checked **before** hashing, not after — the cost being defended against is the hash itself, so a refusal that still hashed would have paid for the attack it was refusing. The gate slot is released through a `defer` inside a closure rather than after the call, because a slot leaked by a panic would be gone for the life of the process and four of them would wedge every login on the box until a restart. A proved-good password clears the source's window, so a household behind one NAT address cannot walk into a lockout through ordinary use.

### CORS

In `DEBUG` mode, all origins are allowed. Otherwise, cross-port requests from the same hostname are permitted (e.g., browser on port 80 talks to API on port 5309), **but only once the Host header has been checked against what this box may legitimately be called**. Allowed methods: GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS. Credentials are allowed with a 3600-second max age.

The check matters because the old rule — "the Origin's hostname equals the Host header's hostname" — compared two values that both come from the same attacker-chosen URL. Point `box.evil.example` at the box's LAN address and a browser sends `Origin: http://box.evil.example` and `Host: box.evil.example:5309`, which match. That is the DNS-rebinding shape, and with `AllowCredentials` it handed a drive-by page the bootstrap window (`POST /account/create` answers unauthenticated while no enabled admin exists).

`originAllowed` therefore requires the Host header to name this box: its own hostname, `<hostname>.local`, `<hostname>.<dns_tld>`, the loopback and LAN addresses it answers on, or whatever the operator configured in `AllowedHosts`. Those forms are **enumerated, not matched by suffix** — a rule like "any name whose first label is the hostname" would accept `townos.evil.example`, which an attacker can simply register. An IP literal is accepted on its own: an address cannot be aliased by DNS, so `http://192.168.1.10/` reaching `http://192.168.1.10:5309` is the same box by construction, which is the common way this is actually used.

**Private Network Access is answered only for an origin CORS would accept.** The `Access-Control-Allow-Private-Network` header was previously echoed unconditionally, which hands every origin on the internet the browser's permission to reach a private address — the one protection PNA exists to add on top of CORS. Its middleware is registered **before** the CORS middleware so it still runs on a preflight, which CORS answers itself without calling further down the chain.

### Graceful Shutdown

SIGINT triggers context cancellation. The HTTP server shuts down, and all background goroutines exit via context channels. Rolodex is supervised by systemd and is not stopped by the systemcontroller.

### CLI Flags

- `-db <path>` -- path to SQLite database (defaults to ephemeral temp file).
- `-btrfs <path>` -- base path for btrfs subvolume operations.
- `-repo-dir <path>` -- base directory for git repositories (defaults to ephemeral temp dir).
- `-network-state <path>` -- directory for per-package network state files (defaults to `/run/town-os`, `DefaultNetworkStatePath`; it must be a path the systemcontroller container and the host share — never `/var/run/...` or `/tmp`).
- `-listen <addr>` -- HTTP listen address (defaults to `:5309`).

The network controller image is not a flag either; it is derived from the resolved image tag and overridable with `NC_IMAGE`.

### Environment Variables

- `CONTAINER_HOST` -- unix socket URL for the host podman daemon. Set automatically at startup to `unix:///run/podman/podman.sock` (see `HostPodmanSocket`). Every `podman` invocation — including child processes forked by the systemcontroller — inherits this from the process environment and routes through the host socket instead of the systemcontroller container's isolated podman storage. The install-repo systemd unit should also set `Environment=CONTAINER_HOST=...` for visibility in `systemctl` output, but the `setupPodmanEnv()` call is the runtime source of truth.
- `TOWN_OS_LISTEN` -- overrides `-listen` flag.
- `TOWN_OS_SIGNING_KEY` -- override the ephemeral JWT signing key (see Session Management).
- `TOWN_OS_TLS` -- serve the control plane's own listener (`:5309`) over HTTPS, terminated by the box's local CA with a leaf issued exactly like a package's. **Off by default, and that is sequencing rather than a hedge**: a browser that has not been given the box's CA cannot complete an XHR to an untrusted certificate, and unlike a navigation there is no interstitial to click through — the UI would simply stop working with no way to reach the screen that explains why. The UI is also served over plain HTTP today (it is the ingress's default `:80` backend), so a box that turned this on without installing the CA first would go from "unencrypted" to "down". The operator installs the CA (`GET /tls/ca.crt`, public), then sets this. Accepts `1`/`true`/`yes`/`on`. Resolved **before** the listener binds, so a boot-status stream that starts as HTTP never becomes HTTPS underneath its client, and **fatal** on failure rather than falling back to cleartext: an operator who asked for TLS and silently got plaintext is worse off than one whose box refuses to start and says why.
- `TOWN_OS_TLS_CERT` / `TOWN_OS_TLS_KEY` -- an operator-supplied certificate and key, for a box fronted by a name that already has a publicly trusted cert. Setting **both** enables TLS on its own and the local CA is not consulted; setting one alone does nothing.
- `TOWN_OS_TLS_SANS` -- comma-separated extra names or IPs for the generated leaf, for a box reached by a name the controller cannot derive (a CNAME, a router-assigned DHCP name).
- `TOWN_OS_TEST` -- if set, use test repositories instead of production defaults.
- `DEBUG` -- if set, allow all CORS origins and prepend test repositories to defaults.
- `LOG_LEVEL` -- logging level: `debug`, `info`, `warn`, `error` (defaults to `error`).
- `TOWN_OS_REPO_USERNAME` / `TOWN_OS_REPO_PASSWORD` -- repository credentials applied to all repositories on first initialization.
- `TOWN_OS_TAG` -- pins the image tag every sibling image derives from (see [Version Tag Detection](#version-tag-detection)). Set by the install build system on the systemcontroller systemd unit.
- `ROLODEX_IMAGE` -- override Rolodex container image (defaults to `quay.io/town/rolodex:<tag>`).
- `UI_IMAGE` -- override UI container image (defaults to `quay.io/town/ui:<tag>`). Setting it to the **empty string** (explicitly present but empty) skips the UI container entirely — dev mode, where bun serves the UI.
- `NC_IMAGE` -- override the network controller image (defaults to `quay.io/town/networkcontroller:<tag>`). Used by the integration harness to inject a locally built NC.
- `INGRESS_IMAGE` -- override the ingress image (defaults to `quay.io/town/ingress:<tag>`). Setting it to the empty string skips the ingress and the pages service — dev mode.
- `GFEH_IMAGE` -- override the object-storage image (defaults to `quay.io/town/gfeh:<tag>`). Setting it to the **empty string** skips object storage entirely — dev mode. Object storage is also skipped when the ingress is disabled, since the four HTTP views are reachable only through it.
- `GFEH_SMB_PORT_BASE` -- override the host port SMB listeners would start from (default `4450`). Vestigial: [no partition serves SMB](#no-smb-view), so no host port is allocated. Kept wired so the harness setting stays harmless.
- `TOWN_OS_WG_SALT` -- the instance salt that separates this box's WireGuard interface names, listen ports, and overlay subnets from another Town OS sharing the network namespace. Unset on a real box; set by the test and dev harnesses. See [The instance salt](#the-instance-salt).

#### System-service host ports

Every system service runs `--net host`, so all of these bind in whatever network namespace the controller is in — the *host* namespace, including inside the integration harness (whose container also runs `--net host`, deliberately, so builds keep working on captive networks where bridge DNS is broken). A `make test-full` box and a `make dev` box therefore fight over every one of these ports and, under `Restart=always`, crash-loop each other forever.

Each of these relocates one of them and **defaults to the production port**, so an unset environment reproduces today's boot exactly. `make/lib.sh`'s `system_port_env` allocates them per run into `SYSTEM_PORT_FILES` and passes them to the test container — IRON RULE. `make dev` deliberately sets **none** of them: dev mirrors a real box, where `redirect_host_dns` needs rolodex on `:53` and a browser needs the ingress on `:443`. An unparseable value is reported on stderr and falls back to the default, because a typo would otherwise look exactly like not setting it.

- `TOWN_OS_DNS_PORT` -- the port rolodex serves DNS on (default `53`, on `DNSLoopback`). **systemd-resolved routing is skipped entirely when this is non-default**: a per-domain DNS server address carries no port, so pointing resolved at `DNSLoopback` would silently blackhole every `.tld` query instead of leaving them to the normal resolver path.
- `TOWN_OS_ROLODEX_METRICS_PORT` -- the port rolodex serves its Prometheus `/metrics` endpoint on, also on `DNSLoopback` (default `9153`). It is a separate listener from the DNS port and needs its own override; `rolodex.Manager.MetricsAddr()` is the single string both `rolodex.yml` and the Prometheus scrape target are built from, so relocating it moves both.
- `TOWN_OS_NODE_EXPORTER_PORT` -- node-exporter's loopback metrics port (default `9100`).
- `TOWN_OS_PROMETHEUS_PORT` -- Prometheus's loopback HTTP API port (default `9090`).
- `TOWN_OS_MONITORING_PORT` -- the single LAN-facing monitoring port (default `5308`).
- `INGRESS_HTTPS_PORT` / `INGRESS_HTTP_PORT` -- the ingress's published ports (defaults `443` / `80`).

## Settings

| Key                      | Default                          | Description                                     |
| ------------------------ | -------------------------------- | ----------------------------------------------- |
| `default_quota`          | `53687091200`                    | Default volume quota in bytes (50 GB)           |
| `max_archive_size`       | `1073741824`                     | Maximum upload size in bytes (1 GB)             |
| `archive_unpack_timeout` | `600`                            | Unpack timeout in seconds (10 min)              |
| `locale`                 | `en-US`                          | BCP 47 locale code (system-wide fallback)       |
| `dns_tld`                | `home`                           | Default top-level domain for package DNS records|
| `dns_resolution_mode`    | `auto`                           | Rolodex upstream resolution: `auto`, `recursive`, or `forward` |
| `dns_local_forwarders`   | `false`                          | Take the forwarder list from the resolvers this box's own network handed it, instead of the public defaults |
| `peer_ttl`               | `7200`                           | WireGuard peer enrollment lifetime in seconds (2 h) |
| `gfeh_partition_quota`   | `0`                              | Quota in bytes for each object-storage partition (0 = unlimited) |
| `proton_image`           | `quay.io/town/proton:latest`     | Proton runner image — **registered only under the `proton` build tag** |

`DefaultSettings` (`src/account/settings.go`) is seeded on first init and existing values are never overwritten.

Several keys are **read but never seeded** — they have no row until something
writes one, and their default lives at the read site as a fallback for the empty
string. Do not add them to `DefaultSettings` expecting no other change: a seeded
row is indistinguishable from an operator's choice, which for the blocklist
configs is the difference between "never configured, leave alone" and "explicitly
set to empty, push it" ([RBL / DNSBL Blocklists](#rbl--dnsbl-blocklists)).

| Key | Default when absent | Written by |
| --- | --- | --- |
| `monitoring_backend`     | `uplot` | `POST /settings/set` |
| `dns_rbl_config` / `dns_dnsbl_config` | unconfigured (not the same as empty) | `POST /dns/rbl`, `POST /dns/dnsbl` |
| `dns_excluded_services`  | empty list (publishing is opt-out) | `POST /dns/services/set` |
| `dismissed_upgrades_hash` | absent (nothing dismissed) | `POST /packages/upgrades/dismiss` |

**There is no `object_storage_enabled` and no service-account password.** Object storage is not a feature to switch on ([Boot and reconcile](#boot-and-reconcile)), and the daemon holds no Town OS credential ([No service accounts](#no-service-accounts)). A row for either, left behind on an upgraded box, is read by nothing.

`proton_image` is not in the base map: `src/account/settings_proton.go` is `//go:build proton` and registers the default in `init()`, so a build without the tag has no Proton setting, no Proton install path, and reports `proton_enabled: false` in the status ping. Build-tag-gated registration is used rather than an exported `Register` function so no caller acquires a call-order dependency on `DefaultSettings`.

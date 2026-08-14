# make/lib.sh - Shared helpers for make scripts.
# Source this file: . make/lib.sh
#
# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.

# ---------------------------------------------------------------------------
# Privileged execution
# ---------------------------------------------------------------------------

SUDO="sudo HOME=$HOME"

# ---------------------------------------------------------------------------
# Ephemeral state directory
# ---------------------------------------------------------------------------
mkdir -p "${STATE_DIR}" 2>/dev/null || true

# ---------------------------------------------------------------------------
# Disk-backed storage for loopback images (NEVER tmpfs — see CLAUDE.md)
# ---------------------------------------------------------------------------
# A loop device backed by a tmpfs file deadlocks the host kernel under memory
# pressure (tmpfs pages can only be reclaimed to swap, but loop writeback must
# allocate memory to drain them) and hard-reboots the machine. /tmp is tmpfs on
# Arch/Manjaro/Fedora, so btrfs backing images must NOT live in STATE_DIR.
BTRFS_IMAGE_DIR="${BTRFS_IMAGE_DIR:-${PWD}/.cache/btrfs}"

# ---------------------------------------------------------------------------
# Per-checkout caches
# ---------------------------------------------------------------------------
# Every cache this build keeps lives under the checkout's gitignored .cache/,
# so the checkout owns all of its own state (see the Makefile for why neither of
# these is host-wide any more).
#
# Both are defaulted here as well as in the Makefile so a script invoked
# directly still lands in the same place. That matters most for bun, which
# silently falls back to ~/.bun/install/cache when it is told nothing — which is
# how a build ends up re-downloading the world into a directory nothing else
# reads. Every bun in this tree — host-side via bun_install, and the container
# builds via the BUN_BUILD_ARGS mount — resolves to this one directory.
IMAGE_CACHE="${IMAGE_CACHE:-${PWD}/.cache/images}"
BUN_CACHE="${BUN_CACHE:-${PWD}/.cache/bun}"
BUN_INSTALL_CACHE_DIR="${BUN_INSTALL_CACHE_DIR:-${BUN_CACHE}}"
export IMAGE_CACHE BUN_CACHE BUN_INSTALL_CACHE_DIR

# BUN_BUILD_ARGS — the podman build arguments that hand a build the shared bun
# cache. Every `podman build` whose Containerfile runs `bun install` passes
# these and nothing else; bun_cache_reclaim is the other half of the contract.
#
# The cache is mounted at ITS OWN HOST PATH rather than at a fixed /bun-cache,
# and that path is forwarded as a build-arg so the Containerfile's
# BUN_INSTALL_CACHE_DIR agrees with where it actually landed. This is not
# cosmetic. Bun's cache is a flat set of extracted packages plus a lookup layer
# of ABSOLUTE symlinks:
#
#   .cache/bun/react/19.2.4@@@1 -> <cache>/react@19.2.4@@@1
#
# so the cache directory's own path is written into every entry in it. While
# the container mounted it at /bun-cache, every symlink it recorded named a
# path that does not exist on the host: host-side bun_install missed on all
# ~770 packages of ui/bun.lock, re-downloaded the lockfile from npmjs, and —
# because the rootful build also left the per-package directories root-owned —
# could not write its results back either. The cache was write-only as seen
# from the host, so it never warmed: a full network install on every `make
# dev`. Mounting at the same path on both sides makes an entry written by
# either half resolvable by the other.
BUN_BUILD_ARGS=(
  --build-arg "BUN_CACHE_DIR=${BUN_CACHE}"
  --volume "${BUN_CACHE}:${BUN_CACHE}:z"
)

# bun_cache_reclaim — hand the shared bun cache back to the invoking user after
# a rootful build has written to it, and drop entries that can no longer be
# resolved. Two repairs, both needed before host-side bun can use the cache:
#
#   - ownership. Every podman build in this pipeline is rootful, so bun runs as
#     root and the package directories it creates are root:root 0755. Host-side
#     bun can read them, but it cannot add a version symlink to an existing name
#     directory — so it can never record a package it just downloaded, and the
#     next run misses on it again.
#
#   - dangling lookup symlinks. They are absolute (see BUN_BUILD_ARGS), so an
#     entry written under some other cache path — the historical /bun-cache
#     mount, or a checkout that has since been moved or renamed — points at
#     nothing. Bun reads that as a miss and has no way to replace the symlink,
#     so it stays a miss forever. Deleting it costs one re-extract and makes the
#     next lookup succeed.
#
# Best-effort throughout: a cache that cannot be repaired is slow, not broken,
# and must never fail a build that otherwise succeeded. Called from an EXIT trap
# so a FAILED build still leaves the cache usable — by then it has usually
# already downloaded packages worth keeping.
bun_cache_reclaim() {
  [ -d "${BUN_CACHE}" ] || return 0
  # -type l plus an explicit existence test rather than `-xtype l`, which also
  # matches a symlink pointing at another symlink — resolvable, and not ours to
  # delete.
  ${SUDO} find "${BUN_CACHE}" -type l ! -exec test -e {} \; -delete 2>/dev/null || true
  ${SUDO} chown -R "$(id -u):$(id -g)" "${BUN_CACHE}" 2>/dev/null || true
  return 0
}

# require_disk_backed DIR — create DIR and abort if it resolves to a tmpfs/
# ramfs mount. Guards against re-introducing the loop-over-tmpfs host reboot
# when BTRFS_IMAGE_DIR is overridden or the checkout itself sits on tmpfs.
require_disk_backed() {
  local dir="$1" fstype
  mkdir -p "${dir}"
  fstype="$(findmnt -nro FSTYPE -T "${dir}" 2>/dev/null || true)"
  case "${fstype}" in
    tmpfs|ramfs)
      echo "ERROR: ${dir} is on ${fstype}; loopback images on RAM-backed filesystems deadlock and reboot the host. Set BTRFS_IMAGE_DIR to a disk-backed path." >&2
      exit 1
      ;;
  esac
}

# ---------------------------------------------------------------------------
# Go module boundary for dev-repos
# ---------------------------------------------------------------------------
# dev-repos/ contains root-owned directories that Go tooling (go mod tidy,
# go vet, etc.) cannot read. Placing a go.mod here marks it as a separate
# module so the parent module scan skips it entirely.
if [ -d "${STATE_DIR}/dev-repos" ] && [ ! -f "${STATE_DIR}/dev-repos/go.mod" ]; then
  printf 'module dev-repos\n' > "${STATE_DIR}/dev-repos/go.mod" 2>/dev/null || true
fi

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------

_cyan='\033[1;36m'
_green='\033[1;32m'
_yellow='\033[1;33m'
_reset='\033[0m'

step() {
  printf "${_cyan}==> %s${_reset}\n" "$*"
}

substep() {
  printf "${_green}  -> %s${_reset}\n" "$*"
}

warn() {
  printf "${_yellow}  ** %s${_reset}\n" "$*"
}

# ---------------------------------------------------------------------------
# Per-arch image tags
# ---------------------------------------------------------------------------

# All architectures a release covers, in registry-tag form (x86_64/aarch64);
# manifest assembly iterates this list.
: "${ARCHES:=x86_64 aarch64}"

# host_arch — print the OCI platform arch (amd64/arm64) for the current host.
#   This is podman's `.Architecture` value and the `--platform linux/<arch>`
#   form; it is NOT the image tag suffix (use host_arch_tag for that).
host_arch() {
  case "$(uname -m)" in
    x86_64 | amd64) echo amd64 ;;
    aarch64 | arm64) echo arm64 ;;
    *)
      echo "unsupported host architecture: $(uname -m)" >&2
      return 1
      ;;
  esac
}

# host_arch_tag — print the per-arch image tag suffix (x86_64/aarch64, the
#   uname -m form) for the current host. Image tags are partitioned with this
#   suffix (rc.latest-x86_64, rc.latest-aarch64, ...); it differs deliberately
#   from host_arch, which is the OCI platform name podman pulls/inspects with.
host_arch_tag() {
  case "$(uname -m)" in
    x86_64 | amd64) echo x86_64 ;;
    aarch64 | arm64) echo aarch64 ;;
    *)
      echo "unsupported host architecture: $(uname -m)" >&2
      return 1
      ;;
  esac
}

# build_arch_tag — print the tag suffix (x86_64/aarch64) this invocation is
#   BUILDING for, which is the host's unless TARGET asked for another. The
#   Makefile exports BUILD_ARCH after validating TARGET; the fallback is for
#   running these scripts directly, where there is no TARGET to honor.
build_arch_tag() {
  if [ -n "${BUILD_ARCH:-}" ]; then
    echo "${BUILD_ARCH}"
  else
    host_arch_tag
  fi
}

# build_oci_arch — the same value as build_arch_tag in podman's platform
#   spelling (amd64/arm64), for `--platform linux/<arch>`.
build_oci_arch() {
  case "$(build_arch_tag)" in
    x86_64) echo amd64 ;;
    aarch64) echo arm64 ;;
    *)
      echo "unsupported build architecture: $(build_arch_tag)" >&2
      return 1
      ;;
  esac
}

# cross_building — true when the build arch differs from the host's.
cross_building() {
  [ "$(build_oci_arch)" != "$(host_arch)" ]
}

# staged_ref IMAGE — the local name a release build writes, and the ONLY name
#   the push targets are allowed to tag from.
#
#   Every release image used to be built as the bare `quay.io/town/<name>`,
#   which podman stores as `:latest` — one slot per image, shared by every
#   architecture. The push targets then did `podman tag <image> <image>:rc.latest-$ARCH`,
#   which does not name an architecture anywhere: it tags whatever happens to
#   be in that slot at that instant. Build aarch64 and x86_64 in the same
#   checkout — concurrently, or just one after the other — and the second build
#   overwrites the first's slot, so the -x86_64 tag can be handed arm64 content
#   and pushed. That is exactly what shipped: ingress, networkcontroller and ui
#   went out as rc.latest-x86_64 holding arm64 binaries, and every one of those
#   services died on the box with "exec container process: Exec format error".
#   The systemcontroller escaped only because push-rc rebuilt it straight into
#   the arch-suffixed tag rather than re-tagging the shared slot.
#
#   Giving the staging tag the arch removes the shared slot, which is what makes
#   two builds in one checkout safe: an aarch64 build and an x86_64 build now
#   write `:local-aarch64` and `:local-x86_64` and cannot see each other.
staged_ref() {
  echo "${1}:local-$(build_arch_tag)"
}

# assert_image_arch REF — refuse to push an image whose architecture is not the
#   one this invocation is building for.
#
#   The naming above makes a mismatch hard; this makes it loud. A wrong-arch
#   push is invisible at push time and surfaces only as an unbootable service on
#   somebody's box, so the check belongs here, before the bytes leave — one
#   `podman image inspect` against a value we already know.
assert_image_arch() {
  local ref="$1" want got
  want="$(build_oci_arch)"
  got="$(${SUDO} podman image inspect "${ref}" --format '{{.Architecture}}' 2>/dev/null)" || {
    echo "assert_image_arch: ${ref} is not in local storage — build it before pushing" >&2
    return 1
  }
  if [ "${got}" != "${want}" ]; then
    echo "assert_image_arch: ${ref} is ${got}, but this build targets ${want}." >&2
    echo "  Refusing to push: a ${want} tag holding ${got} binaries fails at exec time on every box that pulls it." >&2
    return 1
  fi
}

# tag_from_staged IMAGE TAG — point IMAGE:TAG at what this build produced.
#
#   The two steps are one function on purpose. Tagging from the arch-agnostic
#   name is the bug; checking the arch is the guard against it; and a guard the
#   caller has to remember is a guard that gets skipped the next time somebody
#   adds an image. There is exactly one way to name a release tag now, and it
#   verifies itself.
tag_from_staged() {
  local image="$1" tag="$2" ref
  ref="$(staged_ref "${image}")"
  assert_image_arch "${ref}" || return 1
  ${SUDO} podman tag "${ref}" "${image}:${tag}"
}

# require_native_target LABEL — refuse a cross TARGET for work that runs the
#   images it builds on this host (the test harness, dev). A foreign-arch image
#   cannot execute here, so the useful failure is this one, up front and named,
#   rather than an `exec format error` from inside a container twenty minutes in.
require_native_target() {
  cross_building || return 0
  echo "${1}: TARGET=${TARGET:-} builds $(build_arch_tag) images, but this is an $(host_arch_tag) host." >&2
  echo "  These images are run here, not shipped, so a foreign arch cannot work." >&2
  echo "  Drop TARGET (or set it to $(host_arch_tag)); TARGET is for release/push targets." >&2
  return 1
}

# require_cross_binfmt — a cross build compiles natively but still executes a
#   handful of target-arch binaries in the runtime stages (apt-get, groupadd,
#   apk), so the kernel needs a binfmt_misc handler for that arch. This is the
#   one piece of the cross path that cannot be arranged from inside the build,
#   and it needs root, so check it up front and print the remediation rather
#   than letting podman fail with `exec format error` on a RUN line.
#
#   The F ("fix binary") flag matters as much as the registration: without it
#   the interpreter is resolved inside the container's mount namespace, where
#   it does not exist. That is also why the interpreter must be the STATIC
#   qemu-user build — a dynamically linked one still needs its shared libraries
#   present in the target rootfs.
#
#   Two distinct failures wear this same missing-handler symptom, and the fix
#   for one is useless for the other, so the message below distinguishes them:
#   the packages are absent, or they are installed and the REGISTRATION is
#   gone. binfmt_misc is global kernel state that anything on the box can wipe
#   (a `--reset` from a multiarch/qemu-user-static container unregisters every
#   handler, not just its own), and systemd-binfmt does not re-run on its own
#   afterwards — so a host that ran `make deps` weeks ago can sit here with
#   every package installed and no handler. Sending that host to pacman/apt
#   gets "already installed" and leaves it exactly where it started.
require_cross_binfmt() {
  cross_building || return 0

  local want handler
  case "$(build_oci_arch)" in
    arm64) want=aarch64 ;;
    amd64) want=x86_64 ;;
  esac
  handler="/proc/sys/fs/binfmt_misc/qemu-${want}"

  if [ -e "${handler}" ] && grep -qx enabled "${handler}" 2>/dev/null; then
    if ! grep -q '^flags:.*F' "${handler}" 2>/dev/null; then
      warn "binfmt handler qemu-${want} is registered without the F flag; runtime stages may fail inside the build container"
    fi
    substep "cross build: binfmt handler qemu-${want} present"
    return 0
  fi

  echo "cross build to $(build_arch_tag) needs a qemu-${want} binfmt handler, and this host has none." >&2
  echo "  Compilation is native (the builder stages are pinned to the build platform)," >&2
  echo "  but the runtime stages still exec a few ${want} binaries (apt-get, groupadd)." >&2
  echo >&2

  if [ -e "/usr/lib/binfmt.d/qemu-${want}-static.conf" ] ||
     [ -e "/usr/lib/binfmt.d/qemu-${want}.conf" ] ||
     [ -e "/var/lib/binfmts/qemu-${want}" ] ||
     command -v "qemu-${want}-static" >/dev/null 2>&1; then
    echo "  qemu-${want} IS installed here — it is the registration that is missing," >&2
    echo "  so installing the packages again will report them up to date and change" >&2
    echo "  nothing. Re-register it (either works):" >&2
    echo "    make deps                               # installs and re-registers" >&2
    echo "    sudo systemctl restart systemd-binfmt   # registration only" >&2
  else
    echo "  Install it and register the handler:" >&2
    echo "    make deps" >&2
    echo >&2
    echo "  deps.sh installs the STATIC qemu-user build for this distro and restarts" >&2
    echo "  systemd-binfmt. By hand that is qemu-user-static plus the registration" >&2
    echo "  package: qemu-user-static-binfmt on Arch, binfmt-support on Debian." >&2
  fi
  echo >&2
  echo "  Note it must be the STATIC qemu build; a dynamically linked interpreter" >&2
  echo "  cannot find its shared libraries inside the build container." >&2
  return 1
}

# build_manifest IMAGE TAG — assemble and push the multi-arch manifest list
#   IMAGE:TAG from the per-arch tags IMAGE:TAG-<arch> (one per entry in
#   ARCHES). Every per-arch tag must already be pushed from its native host.
build_manifest() {
  local image="$1" list="$2"
  local ref="${image}:${list}"
  substep "Creating manifest ${ref}"
  ${SUDO} podman manifest rm "${ref}" 2>/dev/null || true
  ${SUDO} podman manifest create "${ref}"
  local arch
  for arch in ${ARCHES}; do
    substep "Adding ${ref}-${arch}"
    ${SUDO} podman manifest add "${ref}" "docker://${ref}-${arch}"
  done
  substep "Pushing ${ref}"
  ${SUDO} podman manifest push --all "${ref}" "docker://${ref}"
}

# ---------------------------------------------------------------------------
# Repository credential checks
# ---------------------------------------------------------------------------

# warn_missing_repo_creds — print a loud warning when test runs have no
# GitHub credentials for fetching the test package repos. Without them,
# populate-repos falls back to anonymous GitHub access, which can rate-
# limit or hang the initial test run. The warning is informational: the
# test still proceeds, since local sibling clones or a primed
# .cache/git-repos/ cache may make creds unnecessary.
warn_missing_repo_creds() {
  if [ -n "${TOWN_OS_REPO_USERNAME:-}" ] && [ -n "${TOWN_OS_REPO_PASSWORD:-}" ]; then
    return 0
  fi
  local dotenv_note="no .env file found"
  if [ -f .env ]; then
    dotenv_note=".env is present but does not set both variables"
  fi
  warn "TOWN_OS_REPO_USERNAME / TOWN_OS_REPO_PASSWORD are not set (${dotenv_note})."
  warn "Test package repos will be fetched anonymously from GitHub."
  warn "GitHub rate-limits anonymous clones and the initial populate step"
  warn "may stall or hang for several minutes (or fail outright) on a fresh"
  warn "checkout with no .cache/git-repos/ or ../test-packages-* siblings."
  warn "Fix: create a .env with TOWN_OS_REPO_USERNAME and TOWN_OS_REPO_PASSWORD"
  warn "     (a GitHub personal access token works as the password)."
}

# ---------------------------------------------------------------------------
# Container helpers
# ---------------------------------------------------------------------------

# system_port_env — populate the SYSTEM_PORT_ENV array with the podman -e flags
#   that relocate the otherwise-fixed host ports the system services bind:
#   rolodex :53 and its Prometheus endpoint :9153, node-exporter :9100,
#   Prometheus :9090, the monitoring UI :5308, and the ingress :443/:80.
#
#   The test container runs --net host (deliberately: bridge-network DNS breaks
#   on captive networks), so every one of those services binds in the *host*
#   network namespace — the same namespace `make dev` binds in. Without these
#   overrides a `make test-full` and a `make dev` fight over every one of them and
#   crash-loop each other under Restart=always. The values are allocated per run
#   by make/port.sh into SYSTEM_PORT_FILES — IRON RULE.
#
#   `make dev` deliberately does NOT call this: dev keeps the production ports
#   because it is meant to mirror a real box (redirect_host_dns needs rolodex on
#   :53, and a browser needs the ingress on :443).
system_port_env() {
  SYSTEM_PORT_ENV=()
  local pair var file
  for pair in \
    "TOWN_OS_DNS_PORT:.dns-port" \
    "TOWN_OS_ROLODEX_METRICS_PORT:.rolodex-metrics-port" \
    "TOWN_OS_NODE_EXPORTER_PORT:.node-exporter-port" \
    "TOWN_OS_PROMETHEUS_PORT:.prometheus-port" \
    "TOWN_OS_MONITORING_PORT:.monitoring-port" \
    "INGRESS_HTTPS_PORT:.ingress-https-port" \
    "INGRESS_HTTP_PORT:.ingress-http-port"; do
    var="${pair%%:*}"
    file="${STATE_DIR}/${pair#*:}"
    if [ ! -f "${file}" ]; then
      echo "ERROR: missing port file ${file}. Run this through make so SYSTEM_PORT_FILES are allocated." >&2
      exit 1
    fi
    SYSTEM_PORT_ENV+=(-e "${var}=$(cat "${file}")")
  done
}

# wireguard_salt ROLE — echo the TOWN_OS_WG_SALT value for a container.
#
#   A WireGuard interface name, its UDP listen port, and its overlay subnet are
#   all namespace-global, and the test and dev containers both run --net host.
#   Without a salt, a test box and a dev box derive the *same* interface name and
#   listen port for the same network name, so the second one up cannot create its
#   device and its overlay is dead. Two concurrent test worktrees collide the
#   same way — IRON RULE.
#
#   ROLE ("test" / "dev") separates the two boxes in one checkout; INSTANCE_ID
#   separates checkouts. Both halves are needed: INSTANCE_ID alone is identical
#   for a test and a dev run in the same working directory.
#
#   The value is stable for a given role+checkout, which matters for dev: its
#   database survives across `make dev` runs, and a salt that moved would leave
#   stored subnets pointing at devices named for the previous salt.
#
#   A real box sets nothing and keeps the historical unsalted names.
wireguard_salt() {
  printf '%s-%s' "$1" "${INSTANCE_ID}"
}

# remove_container NAME — force-remove a container, ignoring errors.
# Part of the iron rule: every container must be cleanable for concurrent runs.
remove_container() {
  ${SUDO} podman rm -f "$1" 2>/dev/null || true
}

# wait_for_systemd CONTAINER [TIMEOUT] — block until the systemd D-Bus socket exists.
wait_for_systemd() {
  local container="$1" timeout="${2:-30}"
  for i in $(seq 1 "${timeout}"); do
    ${SUDO} podman exec "${container}" test -S /var/run/dbus/system_bus_socket 2>/dev/null && return 0
    sleep 1
  done
  return 1
}

# wait_for_url URL [TIMEOUT] — block until a URL responds successfully.
wait_for_url() {
  local url="$1" timeout="${2:-30}"
  for i in $(seq 1 "${timeout}"); do
    curl -sf "${url}" >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}

# ---------------------------------------------------------------------------
# Registry login
# ---------------------------------------------------------------------------

# registry_login REGISTRY USER_VAR PASS_VAR — log in or skip if creds are empty.
#   USER_VAR / PASS_VAR are the *names* of the env vars (e.g. DOCKER_USERNAME).
registry_login() {
  local registry="$1" user_var="$2" pass_var="$3"
  local user="${!user_var}" pass="${!pass_var}"
  if [ -z "${user}" ] || [ -z "${pass}" ]; then
    step "Skipping ${registry} login (credentials not set)"
  else
    step "Logging in to ${registry}"
    ${SUDO} podman login -u "${user}" -p "${pass}" "${registry}"
  fi
}

# require_registry_login REGISTRY — fail if the build user's podman is not logged in.
#   Call before any push operation to catch the common mistake of logging in
#   as a different user than the one that built the images.
require_registry_login() {
  local registry="$1"
  if ! ${SUDO} podman login --get-login "${registry}" >/dev/null 2>&1; then
    echo "ERROR: Not logged in to ${registry} (via '${SUDO}')." >&2
    echo "  Pushes must use the same user that built the images." >&2
    echo "  Fix: set QUAY_USERNAME and QUAY_PASSWORD in .env, or run:" >&2
    echo "    ${SUDO} podman login ${registry}" >&2
    exit 1
  fi
}

# ---------------------------------------------------------------------------
# Image cache helpers
# ---------------------------------------------------------------------------

# image_safe_name IMAGE — image ref to safe filename (e.g. "nginx:1.27-alpine" -> "nginx-1.27-alpine").
image_safe_name() {
  basename "$1" | tr ':' '-'
}

# ensure_image_cache_dir — create IMAGE_CACHE, owned by the user running make.
#
# Deliberately NOT ${SUDO} mkdir, even though everything that writes here is
# rootful podman. IMAGE_CACHE now lives under the checkout's .cache/, and a
# root-owned mkdir -p creates .cache itself as root on a fresh checkout — after
# which every unprivileged `mkdir -p .cache/go-mod` in build.sh fails and the
# build is dead before it compiles anything. Root can write its tars into a
# user-owned directory perfectly well; the reverse is not true.
ensure_image_cache_dir() {
  mkdir -p "${IMAGE_CACHE}"
}

# image_cache_tar IMAGE [ARCH] — path to the cached tar for an image, for ARCH
#   (OCI form: amd64/arm64). Defaults to the host's architecture.
#
# The arch is IN THE FILENAME, and that is the whole point. Podman's storage is
# keyed by name:tag with no room for two architectures at once, so pulling
# docker.io/library/debian:bookworm-slim for arm64 repoints that name at the
# arm64 image and the amd64 one becomes dangling. That part is unavoidable. What
# is avoidable is the CACHE having the same problem: with one tar per image
# name, a cross build's `podman save` overwrote the host-arch tar, so the two
# architectures evicted each other and every switch between them cost a full
# network re-pull of every base image.
#
# Keyed by arch, each architecture keeps its own tar, storage is restored from a
# local load instead of a pull, and a wrong-arch image can no longer be handed
# out under a name that claims otherwise.
image_cache_tar() {
  local arch="${2:-}"
  [ -n "${arch}" ] || arch="$(host_arch)" || return 1
  printf '%s/%s-%s.tar' "${IMAGE_CACHE}" "$(image_safe_name "$1")" "${arch}"
}

# save_image_cache IMAGE — save an image to the checkout's cache, replacing any existing tar.
# The write is atomic (temp file + mv) because IMAGE_CACHE is shared across concurrent
# `make test-full` runs in the same checkout (which refresh it through pull-images-daily,
# and through the repair pull ensure-cache triggers when a tar is missing): a reader loading the tar
# must never observe a partial `podman save` mid-write. It also means an interrupted
# pull leaves at most a stray .tmp file, never a truncated cache entry. The
# temp name is per-PID unique so concurrent writers don't collide, and `mv` on the same
# filesystem atomically replaces the target — IRON RULE.
#
# The arch is read back OFF THE IMAGE rather than taken on trust, so a tar can
# only ever land under the architecture it actually contains. Passing the wrong
# arch (or defaulting to the host's while holding a cross-built image) would
# poison the cache in the one way that is expensive to notice: a load that
# succeeds and produces an image nothing on this machine can execute.
save_image_cache() {
  local tar tmp arch
  arch="$(${SUDO} podman image inspect "$1" --format '{{.Architecture}}' 2>/dev/null)"
  if [ -z "${arch}" ]; then
    warn "$1: cannot read architecture; not caching"
    return 1
  fi
  tar="$(image_cache_tar "$1" "${arch}")" || return 1
  tmp="${tar}.tmp.$$"
  ${SUDO} podman save -o "${tmp}" "$1"
  ${SUDO} mv -f "${tmp}" "${tar}"
}

# image_arch_matches IMAGE WANT_ARCH — true if IMAGE is in storage AND its
#   architecture equals WANT_ARCH. A wrong-arch image is worse than a missing
#   one: `podman image exists` reports it present, but `podman build
#   --pull=never` is platform-aware and fails with "image not known" because no
#   image for the host platform is available. This catches that case.
image_arch_matches() {
  local img="$1" want="$2" have
  have="$(${SUDO} podman image inspect "${img}" --format '{{.Architecture}}' 2>/dev/null)" || return 1
  [ "${have}" = "${want}" ]
}

# ensure_image IMAGE [ARCH] — make sure an image of ARCH (OCI form, default the
#   host's) is the one in podman storage under that name.
#   Checks podman storage first (validating architecture), then the cache,
#   then pulls (pinned to that platform). A wrong-architecture image found
#   in storage or in the cache is purged and replaced so the caller always ends
#   up with an image podman can actually build FROM.
#   Returns 1 if the pull fails (caller can decide to continue or abort).
#
# ARCH exists because storage holds one image per name and a cross build needs
# the OTHER one. Calling this before a build makes the store's contents a
# decision rather than a side effect of whichever build pulled last: previously
# a cross build let podman fetch the target-arch base implicitly, silently
# repointing the name, and the next native build found the wrong architecture
# there. With per-arch cache tars the swap back is a local load, so alternating
# between architectures no longer re-pulls anything.
ensure_image() {
  local img="$1" tar arch
  arch="${2:-}"
  [ -n "${arch}" ] || arch="$(host_arch)" || return 1
  tar="$(image_cache_tar "${img}" "${arch}")" || return 1

  if ${SUDO} podman image exists "${img}" 2>/dev/null; then
    if image_arch_matches "${img}" "${arch}"; then
      substep "${img}: already in podman storage (${arch})"
      # Backfill the cache tar if the store has a correct image but the
      # tar is missing — otherwise `ensure-cache` would see an incomplete
      # cache and trigger a full pull-images on the next run.
      [ -f "${tar}" ] || save_image_cache "${img}"
      return 0
    fi
    # Drop the image, KEEP the tar. The tar is keyed by the architecture we
    # want, so it is not the thing that is wrong here — storage simply holds the
    # other arch, because the last build that touched this name was for the
    # other arch. Deleting it would re-pull over the network on every switch
    # between architectures, which is precisely what per-arch tars exist to
    # stop; the cache load below is the fast path this falls into.
    warn "${img}: wrong architecture in storage (want ${arch}) — replacing"
    ${SUDO} podman rmi -f "${img}" 2>/dev/null || true
  fi

  if [ -f "${tar}" ]; then
    substep "${img}: loading from cache"
    ${SUDO} podman load -i "${tar}"
    if image_arch_matches "${img}" "${arch}"; then
      return 0
    fi
    warn "${img}: cached tar is wrong architecture (want ${arch}) — re-pulling"
    ${SUDO} podman rmi -f "${img}" 2>/dev/null || true
    ${SUDO} rm -f "${tar}"
  fi

  substep "${img}: pulling (${arch})"
  if ! ${SUDO} podman pull --platform "linux/${arch}" "${img}"; then
    return 1
  fi
  substep "${img}: saving to cache"
  save_image_cache "${img}"
}

# load_images_into_container CONTAINER IMAGE... — copy cached image tars into
#   a container and load them with the container's inner podman.
#
# Always the HOST-arch tar, never a cross target's: these containers are run
# here, on this machine, so a foreign-arch image loaded into one is an
# `exec format error` waiting for whatever starts it. require_native_target
# already refuses a cross TARGET for every target that calls this, so asking
# for the host arch here is stating that constraint rather than adding one.
load_images_into_container() {
  local container="$1"; shift
  for img in "$@"; do
    local safe tar
    safe="$(image_safe_name "${img}")"
    tar="$(image_cache_tar "${img}")" || return 1
    if [ -f "${tar}" ]; then
      substep "Loading ${img}"
      ${SUDO} podman cp "${tar}" "${container}:/tmp/${safe}.tar"
      ${SUDO} podman exec "${container}" podman load -i "/tmp/${safe}.tar"
      ${SUDO} podman exec "${container}" rm -f "/tmp/${safe}.tar"
    else
      warn "Missing cached image $(basename "${tar}") for ${img}"
    fi
  done
}

# ---------------------------------------------------------------------------
# Upstream freshness stamps
# ---------------------------------------------------------------------------
#
# Two registries, one question: how old may our picture of upstream be before we
# go look again. `make test-full` used to re-pull every image and let every bun
# install re-resolve against npmjs on every single run — gigabytes of traffic and
# a hard dependency on the network, usually to learn that nothing had moved.
#
# A stamp file per cache records when we last checked. PULL_MAX_AGE (default a
# day) is the window; `make pull-images` forces an image check regardless, and
# deleting a stamp forces the other.

# stamp_fresh STAMP — true when STAMP exists and is younger than PULL_MAX_AGE.
#
# A missing or unreadable stamp is deliberately NOT fresh: the cost of an extra
# check is a little time, and the cost of wrongly skipping one is a build against
# stale dependencies, which is the failure nobody attributes to caching.
stamp_fresh() {
  local stamp="$1" max_age="${PULL_MAX_AGE:-86400}" stamped age

  [ -f "${stamp}" ] || return 1
  stamped="$(stat -c %Y "${stamp}" 2>/dev/null)" || return 1
  age=$(( $(date +%s) - stamped ))
  # A negative age is a clock that moved backwards (or a file stamped in the
  # future); treat it as stale rather than as fresh forever.
  [ "${age}" -ge 0 ] && [ "${age}" -lt "${max_age}" ]
}

# stamp_age_human STAMP — the stamp's age as "3h 20m", for log lines.
stamp_age_human() {
  local stamped age
  stamped="$(stat -c %Y "$1" 2>/dev/null)" || { printf 'unknown time'; return; }
  age=$(( $(date +%s) - stamped ))
  printf '%dh %dm' "$(( age / 3600 ))" "$(( age % 3600 / 60 ))"
}

# stamp_touch STAMP — record a successful upstream check.
stamp_touch() {
  mkdir -p "$(dirname "$1")"
  touch "$1"
}

# ---------------------------------------------------------------------------
# UI dependency install
# ---------------------------------------------------------------------------

# bun_install [DIR] — install the UI's JS dependencies with visible progress.
#
# Bare `bun install` prints nothing until it is finished, so a cold cache (or a
# captive network stalling the registry) looks exactly like a hung build: the
# last thing on screen is whatever ran before it, sometimes for minutes. Every
# other slow step in these scripts announces itself; this one did not, and it is
# the one people actually wait on.
#
# --verbose is deliberately NOT used: it prints a line per resolved package and
# buries the failure that matters. What is missing is a start line, the elapsed
# time, and enough context (which directory, which bun) to tell a slow install
# from a wedged one.
#
# The cache directory is passed explicitly rather than left to the environment.
# Bun falls back to ~/.bun/install/cache when BUN_INSTALL_CACHE_DIR is unset, so
# any caller reached without the Makefile's environment — a script run by hand,
# a sudo that dropped it — would quietly fill a second cache while the shared one
# went unused. --cache-dir makes "always the local cache" true by construction.
#
# Within PULL_MAX_AGE of the last check we install --frozen-lockfile: bun then
# installs exactly what the lockfile pins and asks npmjs for nothing. Once a day
# a plain install lets it refresh its manifests, which is the only thing here
# that legitimately needs the network.
bun_install() {
  local dir="${1:-ui}"
  local start=${SECONDS}
  local cache="${BUN_INSTALL_CACHE_DIR:-${BUN_CACHE:-${PWD}/.cache/bun}}"
  local stamp="${BUN_STAMP:-.cache/.bun-refreshed-daily}"
  local -a flags=(--cache-dir "${cache}")
  local refreshing=1

  mkdir -p "${cache}"

  # An entry the host cannot resolve is worse than no entry at all: bun re-fetches
  # the package from npmjs and, on a cache still owned by a rootful build, cannot
  # record what it fetched — so the next run pays the same download again. Only a
  # build can repair that (bun_cache_reclaim needs root), and the symptom on its
  # own is indistinguishable from a slow network, so name it.
  local unresolvable
  unresolvable="$(find "${cache}" -maxdepth 2 -type l ! -exec test -e {} \; -print 2>/dev/null | wc -l)"
  if [ "${unresolvable}" -gt 0 ]; then
    warn "${unresolvable} unresolvable entries in ${cache}: this install re-downloads them from npmjs"
    warn "repair with any image build (make dev-production-image / ui-image) or 'make clean-bun-cache'"
  fi

  if stamp_fresh "${stamp}"; then
    refreshing=0
    flags+=(--frozen-lockfile)
    substep "bun install (${dir}, $(bun --version 2>/dev/null || echo 'bun version unknown'), cached — last refresh $(stamp_age_human "${stamp}") ago)"
  else
    substep "bun install (${dir}, $(bun --version 2>/dev/null || echo 'bun version unknown'), refreshing from npmjs)"
  fi

  if ! (cd "${dir}" && bun install "${flags[@]}"); then
    # --frozen-lockfile fails outright when package.json has moved ahead of the
    # lockfile, which is an ordinary thing to happen mid-branch and not a reason
    # to stop the build. Retry against the registry and count it as the day's
    # refresh, so the next run is cheap again.
    if [ "${refreshing}" -eq 0 ]; then
      warn "bun install from cache failed in ${dir} (lockfile likely out of date); refreshing from npmjs"
      if ! (cd "${dir}" && bun install --cache-dir "${cache}"); then
        warn "bun install failed in ${dir} after $((SECONDS - start))s"
        return 1
      fi
      stamp_touch "${stamp}"
      substep "bun install finished in $((SECONDS - start))s"
      return 0
    fi
    warn "bun install failed in ${dir} after $((SECONDS - start))s"
    return 1
  fi

  if [ "${refreshing}" -eq 1 ]; then
    stamp_touch "${stamp}"
  fi
  substep "bun install finished in $((SECONDS - start))s"
}

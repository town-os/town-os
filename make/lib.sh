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
#   rolodex :53, node-exporter :9100, Prometheus :9090, the monitoring UI :5308,
#   and the ingress :443/:80.
#
#   The test container runs --net host (deliberately: bridge-network DNS breaks
#   on captive networks), so every one of those services binds in the *host*
#   network namespace — the same namespace `make dev` binds in. Without these
#   overrides a `make test-full` and a `make dev` fight over all six ports and
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

# image_cache_tar IMAGE — path to the cached tar for an image.
image_cache_tar() {
  printf '%s/%s.tar' "${IMAGE_CACHE}" "$(image_safe_name "$1")"
}

# save_image_cache IMAGE — save an image to the global cache, replacing any existing tar.
# The write is atomic (temp file + mv) because IMAGE_CACHE is shared across concurrent
# `make test-full` runs (which now always refresh it via the pull-images prerequisite):
# a reader loading the tar must never observe a partial `podman save` mid-write. The
# temp name is per-PID unique so concurrent writers don't collide, and `mv` on the same
# filesystem atomically replaces the target — IRON RULE.
save_image_cache() {
  local tar tmp
  tar="$(image_cache_tar "$1")"
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

# ensure_image IMAGE — make sure a HOST-ARCH image is in podman storage.
#   Checks podman storage first (validating architecture), then the cache,
#   then pulls (pinned to the host platform). A wrong-architecture image found
#   in storage or in the cache is purged and re-pulled so the host always ends
#   up with an image podman can actually build FROM.
#   Returns 1 if the pull fails (caller can decide to continue or abort).
ensure_image() {
  local img="$1" tar arch
  tar="$(image_cache_tar "${img}")"
  arch="$(host_arch)" || return 1

  if ${SUDO} podman image exists "${img}" 2>/dev/null; then
    if image_arch_matches "${img}" "${arch}"; then
      substep "${img}: already in podman storage (${arch})"
      # Backfill the cache tar if the store has a correct image but the
      # tar is missing — otherwise `ensure-cache` would see an incomplete
      # cache and trigger a full pull-images on the next run.
      [ -f "${tar}" ] || save_image_cache "${img}"
      return 0
    fi
    warn "${img}: wrong architecture in storage (want ${arch}) — re-pulling"
    ${SUDO} podman rmi -f "${img}" 2>/dev/null || true
    ${SUDO} rm -f "${tar}"
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
load_images_into_container() {
  local container="$1"; shift
  for img in "$@"; do
    local safe tar
    safe="$(image_safe_name "${img}")"
    tar="${IMAGE_CACHE}/${safe}.tar"
    if [ -f "${tar}" ]; then
      substep "Loading ${img}"
      ${SUDO} podman cp "${tar}" "${container}:/tmp/${safe}.tar"
      ${SUDO} podman exec "${container}" podman load -i "/tmp/${safe}.tar"
      ${SUDO} podman exec "${container}" rm -f "/tmp/${safe}.tar"
    else
      warn "Missing cached image ${safe}.tar for ${img}"
    fi
  done
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
bun_install() {
  local dir="${1:-ui}"
  local start=${SECONDS}

  substep "bun install (${dir}, $(bun --version 2>/dev/null || echo 'bun version unknown'))"
  if ! (cd "${dir}" && bun install); then
    warn "bun install failed in ${dir} after $((SECONDS - start))s"
    return 1
  fi
  substep "bun install finished in $((SECONDS - start))s"
}

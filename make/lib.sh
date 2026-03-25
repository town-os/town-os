# make/lib.sh - Shared helpers for make scripts.
# Source this file: . make/lib.sh
#
# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.

# ---------------------------------------------------------------------------
# Privileged execution
# ---------------------------------------------------------------------------

SUDO="sudo -E"

# ---------------------------------------------------------------------------
# Ephemeral state directory
# ---------------------------------------------------------------------------
mkdir -p "${STATE_DIR}" 2>/dev/null || true

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
# Container helpers
# ---------------------------------------------------------------------------

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
save_image_cache() {
  local tar
  tar="$(image_cache_tar "$1")"
  ${SUDO} rm -f "${tar}"
  ${SUDO} podman save -o "${tar}" "$1"
}

# ensure_image IMAGE — make sure an image is in podman storage.
#   Checks podman storage first, then the cache, then pulls.
#   Returns 1 if the pull fails (caller can decide to continue or abort).
ensure_image() {
  local img="$1" tar
  tar="$(image_cache_tar "${img}")"
  if ${SUDO} podman image exists "${img}" 2>/dev/null; then
    substep "${img}: already in podman storage"
  elif [ -f "${tar}" ]; then
    substep "${img}: loading from cache"
    ${SUDO} podman load -i "${tar}"
  else
    substep "${img}: pulling"
    if ! ${SUDO} podman pull "${img}"; then
      return 1
    fi
    substep "${img}: saving to cache"
    save_image_cache "${img}"
  fi
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

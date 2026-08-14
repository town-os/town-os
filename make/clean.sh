#!/usr/bin/env bash

# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
# Cleanup is best-effort; do not set -e.
. make/lib.sh

# ---------------------------------------------------------------------------
# Individual cleanups
#
# Each is a function so `all` can run them in one process in a known order,
# rather than fanning out to make prerequisites — which `make -j` is free to
# run concurrently, and the order below is load-bearing (see clean_all).
# ---------------------------------------------------------------------------

clean_integration() {
  step "Cleaning integration containers"
  remove_container "${PODMAN_CONTAINER}"
  remove_container "${PODMAN_UI_BACKEND}"
  remove_container "${PODMAN_UI_CONTAINER}"
  rm -f "${STATE_DIR}/.integration-port"
  rm -f "${STATE_DIR}/.dns-port" "${STATE_DIR}/.rolodex-metrics-port" \
    "${STATE_DIR}/.node-exporter-port" \
    "${STATE_DIR}/.prometheus-port" "${STATE_DIR}/.monitoring-port" \
    "${STATE_DIR}/.ingress-https-port" "${STATE_DIR}/.ingress-http-port" \
    "${STATE_DIR}/.ingress-metrics-port"
  ${MAKE} clean-btrfs
}

clean_dev_data() {
  step "Cleaning dev data cache"
  ${SUDO} rm -rf "${STATE_DIR}/dev-data" "${STATE_DIR}/dev-repos" "${STATE_DIR}/dev-rolodex"
}

# The per-checkout build cache — which is every cache this build keeps: the Go
# module and build caches, the bun cache, the saved image tars, the UI bundle
# staging, and — the reason ordering matters — the btrfs loopback backing image
# at .cache/btrfs (BTRFS_IMAGE_DIR in lib.sh).
#
# ${SUDO} is not belt-and-braces here. Rootful podman writes into .cache/images,
# .cache/bun and .cache/go-* through its bind mounts and its saves, so parts of
# this tree are owned by root
# and a plain rm leaves them behind — which is how a merged worktree ends up
# impossible to `git worktree remove` as the user who created it.
clean_build_cache() {
  step "Cleaning build cache"
  ${SUDO} rm -rf .cache
  verify_build_cache_gone
}

# The whole point of the target: say so loudly if anything survived, rather than
# reporting success and leaving the caller to discover it when a later rm -rf or
# `git worktree remove` fails on a permission error.
verify_build_cache_gone() {
  if [ ! -e .cache ]; then
    return 0
  fi
  warn ".cache still exists after removal: ${PWD}/.cache"
  ${SUDO} find .cache -maxdepth 2 -printf '  %u %M %p\n' 2>/dev/null | head -20
  echo "ERROR: could not remove ${PWD}/.cache" >&2
  exit 1
}

clean_image_cache() {
  step "Cleaning image cache"
  ${SUDO} rm -rf "${IMAGE_CACHE}"
}

# ${SUDO} for the same reason clean_build_cache needs it: the container builds
# bind-mount this directory as rootful podman, so some of what is in it is
# owned by root and a plain rm would leave it behind.
clean_bun_cache() {
  step "Cleaning bun package cache"
  ${SUDO} rm -rf "${BUN_CACHE}"
}

clean_containers() {
  step "Cleaning all town-os containers"
  # Remove all town-os containers from any working directory / instance.
  ${SUDO} podman ps -a --format '{{.Names}}' 2>/dev/null \
    | grep -E '^(town-os-(test|dev|registry|gitea|ui-(integration|backend))|preflight-test)-' \
    | xargs -r -I{} ${SUDO} podman rm -f {} 2>/dev/null || true
  ${SUDO} podman ps -a --format '{{.Names}}' 2>/dev/null \
    | grep -E '^town-os-dev$' \
    | xargs -r -I{} ${SUDO} podman rm -f {} 2>/dev/null || true
  # Remove orphaned monitoring/system containers that escape the dev
  # container (host PID/network namespace) and hold ports after removal.
  ${SUDO} podman ps -a --format '{{.Names}}' 2>/dev/null \
    | grep -E '^town-os-system--' \
    | xargs -r -I{} ${SUDO} podman rm -f {} 2>/dev/null || true
  # Prune orphaned volumes to free podman locks.
  ${SUDO} podman volume prune -f 2>/dev/null || true
}

# ---------------------------------------------------------------------------
# The aggregate
# ---------------------------------------------------------------------------

# `make clean` is the one target that leaves nothing behind.
#
# Each step goes through ${MAKE} rather than calling the function above it,
# because the targets carry prerequisites that are half the cleanup:
# clean-integration stops the registry and Gitea, clean-dev stops the dev
# containers and detaches the dev btrfs. Calling the recipe bodies directly
# would silently skip all of that.
#
# Serial by construction, for the same reason: these are prerequisites of
# nothing, so `make -j` would be free to interleave them, and the order is not
# cosmetic:
#
#  1. Containers first. A running test or dev container holds the btrfs mount
#     and podman volumes open, so tearing them down first is what lets the
#     unmounts below actually succeed.
#  2. The btrfs loopback devices next — clean-integration detaches the test
#     filesystem, clean-dev the dev one.
#  3. The build cache LAST, because .cache/btrfs is where the loopback backing
#     images live. Deleting it before step 2 leaves a loop device pointing at an
#     unlinked file: the mount survives, `losetup -j` can no longer find it by
#     path, and the next run collides with a device nothing can clean up.
#
# The image and bun caches (IMAGE_CACHE, BUN_CACHE) now live under .cache too,
# so clean-build-cache would take them regardless; they are still swept by name
# first so the output says what is going. Re-populating them costs a
# multi-gigabyte pull and a full npmjs fetch, which is why they stay reachable
# on their own — `make clean` is the deliberate "leave nothing" hammer, not
# something to reach for between test runs.
clean_all() {
  ${MAKE} clean-containers
  ${MAKE} clean-integration
  ${MAKE} clean-dev
  ${MAKE} clean-image-cache
  ${MAKE} clean-bun-cache
  ${MAKE} clean-build-cache
}

case "$1" in
  all) clean_all ;;
  integration) clean_integration ;;
  cache) clean_dev_data ;;
  build-cache) clean_build_cache ;;
  image-cache) clean_image_cache ;;
  bun-cache) clean_bun_cache ;;
  containers) clean_containers ;;
  *)
    echo "Usage: $0 {all|integration|cache|build-cache|image-cache|bun-cache|containers}"
    exit 1
    ;;
esac

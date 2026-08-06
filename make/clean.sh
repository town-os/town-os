#!/usr/bin/env bash

# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
# Cleanup is best-effort; do not set -e.
. make/lib.sh

case "$1" in
  integration)
    step "Cleaning integration containers"
    remove_container "${PODMAN_CONTAINER}"
    remove_container "${PODMAN_UI_BACKEND}"
    remove_container "${PODMAN_UI_CONTAINER}"
    rm -f "${STATE_DIR}/.integration-port"
    rm -f "${STATE_DIR}/.dns-port" "${STATE_DIR}/.node-exporter-port" \
      "${STATE_DIR}/.prometheus-port" "${STATE_DIR}/.monitoring-port" \
      "${STATE_DIR}/.ingress-https-port" "${STATE_DIR}/.ingress-http-port"
    ${MAKE} clean-btrfs
    ;;
  cache)
    step "Cleaning dev data cache"
    ${SUDO} rm -rf "${STATE_DIR}/dev-data" "${STATE_DIR}/dev-repos" "${STATE_DIR}/dev-rolodex"
    ;;
  main)
    step "Cleaning build cache"
    ${SUDO} rm -rf .cache
    ;;
  image-cache)
    step "Cleaning image cache"
    ${SUDO} rm -rf "${IMAGE_CACHE}"
    ;;
  bun-cache)
    # Not swept by `clean`: the point of a host-wide package cache is to outlive
    # any one checkout, and `make clean` runs often enough that folding it in
    # would mean re-downloading npmjs most days. Deliberate, and separate.
    step "Cleaning bun package cache"
    rm -rf "${BUN_CACHE}"
    ;;
  containers)
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
    ;;
  *)
    echo "Usage: $0 {integration|cache|main|image-cache|bun-cache|containers}"
    exit 1
    ;;
esac

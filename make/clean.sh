#!/usr/bin/env bash
# Cleanup is best-effort; do not set -e.

case "$1" in
  integration)
    sudo -E podman rm -f "${PODMAN_CONTAINER}" 2>/dev/null || true
    sudo -E podman rm -f "${PODMAN_UI_BACKEND}" 2>/dev/null || true
    sudo -E podman rm -f "${PODMAN_UI_CONTAINER}" 2>/dev/null || true
    rm -f .integration-port
    ${MAKE} clean-btrfs
    ;;
  cache)
    sudo rm -rf dev-data dev-repos
    ;;
  main)
    sudo rm -rf .cache
    ;;
  image-cache)
    sudo rm -rf "${IMAGE_CACHE}"
    ;;
  containers)
    # Remove all town-os containers from any working directory / instance.
    sudo -E podman ps -a --format '{{.Names}}' 2>/dev/null \
      | grep -E '^(town-os-(test|dev|registry|gitea|ui-(integration|backend))|preflight-test)-' \
      | xargs -r -I{} sudo -E podman rm -f {} 2>/dev/null || true
    sudo -E podman ps -a --format '{{.Names}}' 2>/dev/null \
      | grep -E '^town-os-dev$' \
      | xargs -r -I{} sudo -E podman rm -f {} 2>/dev/null || true
    ;;
  *)
    echo "Usage: $0 {integration|cache|main|image-cache|containers}"
    exit 1
    ;;
esac

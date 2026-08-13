#!/usr/bin/env bash
# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
set -e
. make/lib.sh

step "Running preflight checks"

substep "Checking podman"
command -v podman >/dev/null 2>&1 || { echo "ERROR: podman not found"; exit 1; }

substep "Checking btrfs-progs"
command -v mkfs.btrfs >/dev/null 2>&1 || { echo "ERROR: mkfs.btrfs not found"; exit 1; }

substep "Checking credentials"
test -n "${TOWN_OS_REPO_USERNAME}" || { echo "ERROR: TOWN_OS_REPO_USERNAME not set"; exit 1; }
test -n "${TOWN_OS_REPO_PASSWORD}" || { echo "ERROR: TOWN_OS_REPO_PASSWORD not set"; exit 1; }

substep "Checking bridge networking"
${SUDO} podman load -i "$(image_cache_tar docker.io/library/nginx:1.27-alpine)"
port=$(cat "${STATE_DIR}/.integration-port")
if ${SUDO} podman run --replace --pull=never --rm -d --name "${PREFLIGHT_CONTAINER}" -p "${port}:80" docker.io/library/nginx:1.27-alpine >/dev/null 2>&1 && \
   sleep 2 && \
   curl -sf "http://127.0.0.1:${port}/" >/dev/null 2>&1; then
  substep "Bridge networking: OK"
  remove_container "${PREFLIGHT_CONTAINER}"
else
  remove_container "${PREFLIGHT_CONTAINER}"
  echo "ERROR: bridge networking (-p) not working"
  exit 1
fi

step "All preflight checks passed"

#!/usr/bin/env bash
set -e

echo "Checking podman..."
command -v podman >/dev/null 2>&1 || { echo "ERROR: podman not found"; exit 1; }

echo "Checking btrfs-progs..."
command -v mkfs.btrfs >/dev/null 2>&1 || { echo "ERROR: mkfs.btrfs not found"; exit 1; }

echo "Checking credentials..."
test -n "${TOWN_OS_REPO_USERNAME}" || { echo "ERROR: TOWN_OS_REPO_USERNAME not set"; exit 1; }
test -n "${TOWN_OS_REPO_PASSWORD}" || { echo "ERROR: TOWN_OS_REPO_PASSWORD not set"; exit 1; }

echo "Checking bridge networking..."
sudo -E podman load -i "${IMAGE_CACHE}/nginx-1.27-alpine.tar"
port=$(cat .integration-port)
if sudo podman run --pull=never --rm -d --name "${PREFLIGHT_CONTAINER}" -p "${port}:80" docker.io/library/nginx:1.27-alpine >/dev/null 2>&1 && \
   sleep 2 && \
   curl -sf "http://127.0.0.1:${port}/" >/dev/null 2>&1; then
  echo "Bridge networking: OK"
  sudo podman rm -f "${PREFLIGHT_CONTAINER}" >/dev/null 2>&1
else
  sudo podman rm -f "${PREFLIGHT_CONTAINER}" >/dev/null 2>&1
  echo "ERROR: bridge networking (-p) not working"
  exit 1
fi

echo "All preflight checks passed."

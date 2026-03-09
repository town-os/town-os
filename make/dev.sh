#!/usr/bin/env bash
set -e
. make/lib.sh

case "$1" in
  start)
    step "Starting dev environment"
    ${SUDO} podman rm -f "${PODMAN_DEV_CONTAINER}"
    mkdir -p dev-data dev-repos
    substep "Launching dev container"
    ${SUDO} podman run -d --net host -e LOG_LEVEL=debug -e DEBUG=1 \
      -e "TOWN_OS_REPO_USERNAME=${TOWN_OS_REPO_USERNAME}" \
      -e "TOWN_OS_REPO_PASSWORD=${TOWN_OS_REPO_PASSWORD}" \
      -e TOWN_OS_NETWORK_MODE=host \
      --systemd=true --privileged \
      --device /dev/btrfs-control:/dev/btrfs-control:rwm \
      -v "$(cat town-os-dev.mount):/town-os:z" \
      -v "$(pwd)/dev-data:/data/db:z" \
      -v "$(pwd)/dev-repos:/data/repos:z" \
      --name "${PODMAN_DEV_CONTAINER}" "${PODMAN_DEV_IMAGE}"
    substep "Waiting for dev container to be ready"
    for i in $(seq 1 30); do
      if ${SUDO} podman exec "${PODMAN_DEV_CONTAINER}" true 2>/dev/null; then
        break
      fi
      sleep 1
    done
    step "Loading monitoring images into dev container"
    load_images_into_container "${PODMAN_DEV_CONTAINER}" ${MONITORING_IMAGES}
    step "Loading rolodex image into dev container"
    load_images_into_container "${PODMAN_DEV_CONTAINER}" ${ROLODEX_IMAGE}
    step "Starting UI dev server"
    substep "API server: http://$(hostname):5309"
    cd ui && bun install && bun run dev -- --host
    ${SUDO} podman rm -f "${PODMAN_DEV_CONTAINER}"
    ;;
  logs)
    step "Streaming dev container logs"
    ${SUDO} podman exec -it "${PODMAN_DEV_CONTAINER}" journalctl -f
    ;;
  stop)
    step "Stopping dev container"
    # Stop and remove the dev container for this working directory.
    remove_container "${PODMAN_DEV_CONTAINER}"
    ;;
  stop-all)
    step "Stopping all dev containers"
    # Stop and remove all town-os dev containers (from any working directory).
    ${SUDO} podman ps -a --format '{{.Names}}' 2>/dev/null \
      | grep -E '^town-os-dev$' \
      | xargs -r -I{} ${SUDO} podman rm -f {} 2>/dev/null || true
    ;;
  *)
    echo "Usage: $0 {start|logs|stop|stop-all}"
    exit 1
    ;;
esac

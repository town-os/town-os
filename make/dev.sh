#!/usr/bin/env bash
set -e

case "$1" in
  start)
    sudo -E podman rm -f "${PODMAN_DEV_CONTAINER}"
    mkdir -p dev-data dev-repos
    sudo -E podman run -d --net host -e LOG_LEVEL=debug -e DEBUG=1 \
      -e "TOWN_OS_REPO_USERNAME=${TOWN_OS_REPO_USERNAME}" \
      -e "TOWN_OS_REPO_PASSWORD=${TOWN_OS_REPO_PASSWORD}" \
      -e TOWN_OS_NETWORK_MODE=host \
      --systemd=true --privileged \
      --device /dev/btrfs-control:/dev/btrfs-control:rwm \
      -v "$(cat town-os-dev.mount):/data/btrfs:z" \
      -v "$(pwd)/dev-data:/data/db:z" \
      -v "$(pwd)/dev-repos:/data/repos:z" \
      --name "${PODMAN_DEV_CONTAINER}" "${PODMAN_DEV_IMAGE}"
    echo "Loading monitoring images into dev container..."
    for img in ${MONITORING_IMAGES}; do
      safe=$(basename "${img}" | tr ':' '-')
      if [ -f "${IMAGE_CACHE}/${safe}.tar" ]; then
        sudo -E podman cp "${IMAGE_CACHE}/${safe}.tar" "${PODMAN_DEV_CONTAINER}:/tmp/${safe}.tar"
        sudo -E podman exec "${PODMAN_DEV_CONTAINER}" podman load -i "/tmp/${safe}.tar"
        sudo -E podman exec "${PODMAN_DEV_CONTAINER}" rm -f "/tmp/${safe}.tar"
      else
        echo "WARNING: missing cached image ${safe}.tar for ${img}"
      fi
    done
    echo "API server: http://$(hostname):5309"
    cd ui && bun install && bun run dev -- --host
    sudo -E podman rm -f "${PODMAN_DEV_CONTAINER}"
    ;;
  logs)
    sudo podman exec -it "${PODMAN_DEV_CONTAINER}" journalctl -f
    ;;
  stop)
    # Stop and remove the dev container for this working directory.
    sudo -E podman rm -f "${PODMAN_DEV_CONTAINER}" 2>/dev/null || true
    ;;
  stop-all)
    # Stop and remove all town-os dev containers (from any working directory).
    sudo -E podman ps -a --format '{{.Names}}' 2>/dev/null \
      | grep -E '^town-os-dev$' \
      | xargs -r -I{} sudo -E podman rm -f {} 2>/dev/null || true
    ;;
  *)
    echo "Usage: $0 {start|logs|stop|stop-all}"
    exit 1
    ;;
esac

#!/usr/bin/env bash
set -e

case "$1" in
  start)
    # Start a local registry:2 container.
    sudo -E podman rm -f "${REGISTRY_CONTAINER}" 2>/dev/null || true
    sudo -E podman load -i "${IMAGE_CACHE}/registry-2.tar"
    sudo -E podman run -d --pull=never --name "${REGISTRY_CONTAINER}" \
      -p "$(cat .registry-port):5000" \
      docker.io/library/registry:2
    echo "Registry running on port $(cat .registry-port)"
    ;;
  populate)
    # Pull each discovered image, re-tag for the local registry, and push.
    sudo -E mkdir -p "${IMAGE_CACHE}"
    port=$(cat .registry-port)
    while IFS= read -r image; do
      local_tag="localhost:${port}/${image#docker.io/}"
      safe=$(basename "${image}" | tr ':' '-')
      if sudo -E podman image exists "${image}" 2>/dev/null; then
        echo "${image}: already in podman storage"
      elif [ -f "${IMAGE_CACHE}/${safe}.tar" ]; then
        echo "${image}: loading from cache..."
        sudo -E podman load -i "${IMAGE_CACHE}/${safe}.tar"
      else
        echo "${image}: pulling..."
        sudo -E podman pull "${image}" || { echo "WARNING: failed to pull ${image}"; continue; }
        echo "${image}: saving to cache..."
        sudo -E podman save -o "${IMAGE_CACHE}/${safe}.tar" "${image}"
      fi
      echo "Mirroring ${image} -> ${local_tag}"
      sudo -E podman tag "${image}" "${local_tag}" && \
      sudo -E podman push --tls-verify=false "${local_tag}" || \
      { echo "WARNING: failed to mirror ${image}"; }
    done < .cache/.registry-images
    ;;
  stop)
    # Stop and remove the local registry container.
    sudo -E podman rm -f "${REGISTRY_CONTAINER}" 2>/dev/null || true
    rm -f .registry-port
    ;;
  gen-config)
    # Generate registries.conf that redirects docker.io to the local registry.
    mkdir -p .cache
    printf '[[registry]]\nprefix = "docker.io"\nlocation = "docker.io"\n\n[[registry.mirror]]\nlocation = "localhost:%s"\ninsecure = true\n' "$(cat .registry-port)" > .cache/registries.conf
    echo "Generated registries.conf (mirror on port $(cat .registry-port))"
    ;;
  discover-images)
    # Discover docker.io images from test package repositories.
    mkdir -p .cache
    TOWN_OS_REPO_USERNAME= TOWN_OS_REPO_PASSWORD= \
    TOWN_OS_TEST_REPO_CORE_URL="http://127.0.0.1:$(cat .gitea-port)/town-os/test-packages-core.git" \
    TOWN_OS_TEST_REPO_EXTRAS_URL="http://127.0.0.1:$(cat .gitea-port)/town-os/test-packages-extras.git" \
      go run ./src/registry/cmd/discover-images/ > .cache/.registry-images
    echo "Discovered $(wc -l < .cache/.registry-images) images"
    ;;
  *)
    echo "Usage: $0 {start|populate|stop|gen-config|discover-images}"
    exit 1
    ;;
esac

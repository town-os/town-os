#!/usr/bin/env bash
set -e
. make/lib.sh

case "$1" in
  start)
    step "Starting local registry"
    # Start a local registry:2 container.
    remove_container "${REGISTRY_CONTAINER}"
    ${SUDO} podman load -i "${IMAGE_CACHE}/registry-2.tar"
    ${SUDO} podman run -d --pull=never --name "${REGISTRY_CONTAINER}" \
      -p "$(cat .registry-port):5000" \
      docker.io/library/registry:2
    substep "Registry running on port $(cat .registry-port)"
    ;;
  populate)
    step "Populating local registry with images"
    # Pull each discovered image, re-tag for the local registry, and push.
    ${SUDO} mkdir -p "${IMAGE_CACHE}"
    port=$(cat .registry-port)
    while IFS= read -r image; do
      local_tag="localhost:${port}/${image#docker.io/}"
      if ! ensure_image "${image}"; then
        warn "Failed to pull ${image}"
        continue
      fi
      substep "Mirroring ${image} -> ${local_tag}"
      ${SUDO} podman tag "${image}" "${local_tag}" && \
      ${SUDO} podman push --tls-verify=false "${local_tag}" || \
      { warn "Failed to mirror ${image}"; }
    done < .cache/.registry-images
    ;;
  stop)
    step "Stopping local registry"
    # Stop and remove the local registry container.
    remove_container "${REGISTRY_CONTAINER}"
    rm -f .registry-port
    ;;
  gen-config)
    step "Generating registries.conf"
    # Generate registries.conf that redirects docker.io to the local registry.
    mkdir -p .cache
    printf '[[registry]]\nprefix = "docker.io"\nlocation = "docker.io"\n\n[[registry.mirror]]\nlocation = "localhost:%s"\ninsecure = true\n' "$(cat .registry-port)" > .cache/registries.conf
    substep "Mirror configured on port $(cat .registry-port)"
    ;;
  discover-images)
    step "Discovering images from test repos"
    # Discover docker.io images from test package repositories.
    mkdir -p .cache
    TOWN_OS_REPO_USERNAME= TOWN_OS_REPO_PASSWORD= \
    TOWN_OS_TEST_REPO_CORE_URL="http://127.0.0.1:$(cat .gitea-port)/town-os/test-packages-core.git" \
    TOWN_OS_TEST_REPO_EXTRAS_URL="http://127.0.0.1:$(cat .gitea-port)/town-os/test-packages-extras.git" \
      go run ./src/registry/cmd/discover-images/ > .cache/.registry-images
    substep "Discovered $(wc -l < .cache/.registry-images) images"
    ;;
  *)
    echo "Usage: $0 {start|populate|stop|gen-config|discover-images}"
    exit 1
    ;;
esac

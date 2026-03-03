#!/usr/bin/env bash
set -e

case "$1" in
  unit)
    go test -v -timeout 60m ./src/...
    cd ui && bun install && bun run test
    ;;
  integration)
    trap '${MAKE} clean-btrfs' EXIT
    ${MAKE} btrfs
    sudo -E podman rm -f "${PODMAN_CONTAINER}" 2>/dev/null || true
    sudo -E podman run -e "LOG_LEVEL=${LOG_LEVEL}" -e TOWN_OS_TEST=1 \
      -e TOWN_OS_REPO_USERNAME=town-os \
      -e TOWN_OS_REPO_PASSWORD=town-os-test \
      -e "TOWN_OS_LISTEN=:$(cat .integration-port)" \
      -e "TOWN_OS_TEST_REPO_CORE_URL=http://127.0.0.1:$(cat .gitea-port)/town-os/test-packages-core.git" \
      -e "TOWN_OS_TEST_REPO_EXTRAS_URL=http://127.0.0.1:$(cat .gitea-port)/town-os/test-packages-extras.git" \
      -d --net host --systemd=true --privileged \
      --device /dev/btrfs-control:/dev/btrfs-control:rwm \
      -v "$(cat town-os.mount):/data/btrfs:z" \
      -v "$(pwd)/.cache/registries.conf:/etc/containers/registries.conf.d/local-registry.conf:ro,z" \
      --name="${PODMAN_CONTAINER}" "${PODMAN_TEST_IMAGE}"
    echo "Waiting for systemd to be ready..."
    for i in $(seq 1 30); do
      sudo -E podman exec "${PODMAN_CONTAINER}" test -S /var/run/dbus/system_bus_socket 2>/dev/null && break
      sleep 1
    done
    echo "Loading monitoring images into test container..."
    for img in ${MONITORING_IMAGES}; do
      safe=$(basename "${img}" | tr ':' '-')
      if [ -f "${IMAGE_CACHE}/${safe}.tar" ]; then
        sudo -E podman cp "${IMAGE_CACHE}/${safe}.tar" "${PODMAN_CONTAINER}:/tmp/${safe}.tar"
        sudo -E podman exec "${PODMAN_CONTAINER}" podman load -i "/tmp/${safe}.tar"
        sudo -E podman exec "${PODMAN_CONTAINER}" rm -f "/tmp/${safe}.tar"
      else
        echo "WARNING: missing cached image ${safe}.tar for ${img}"
      fi
    done
    rc=0
    sudo -E podman exec -w /test "${PODMAN_CONTAINER}" /integration-test -test.v -test.timeout 60m || rc=$?
    exit ${rc}
    ;;
  ui-integration)
    trap '${MAKE} clean-btrfs' EXIT
    ${MAKE} btrfs
    sudo -E podman rm -f "${PODMAN_UI_CONTAINER}" 2>/dev/null || true
    sudo -E podman rm -f "${PODMAN_UI_BACKEND}" 2>/dev/null || true
    sudo -E podman run -e LOG_LEVEL=debug -e DEBUG=1 -e TOWN_OS_TEST=1 \
      -e TOWN_OS_REPO_USERNAME=town-os \
      -e TOWN_OS_REPO_PASSWORD=town-os-test \
      -e "TOWN_OS_LISTEN=:$(cat .integration-port)" \
      -e "TOWN_OS_TEST_REPO_CORE_URL=http://127.0.0.1:$(cat .gitea-port)/town-os/test-packages-core.git" \
      -e "TOWN_OS_TEST_REPO_EXTRAS_URL=http://127.0.0.1:$(cat .gitea-port)/town-os/test-packages-extras.git" \
      -d --net host --systemd=true --privileged \
      --device /dev/btrfs-control:/dev/btrfs-control:rwm \
      -v "$(cat town-os.mount):/data/btrfs:z" \
      -v "$(pwd)/.cache/registries.conf:/etc/containers/registries.conf.d/local-registry.conf:ro,z" \
      --name="${PODMAN_UI_BACKEND}" "${PODMAN_TEST_IMAGE}"
    echo "Waiting for systemd to be ready..."
    for i in $(seq 1 30); do
      sudo -E podman exec "${PODMAN_UI_BACKEND}" test -S /var/run/dbus/system_bus_socket 2>/dev/null && break
      sleep 1
    done
    echo "Waiting for systemcontroller API to be ready..."
    for i in $(seq 1 30); do
      curl -sf "http://localhost:$(cat .integration-port)/" >/dev/null 2>&1 && break
      sleep 1
    done
    rc=0
    sudo -E podman run \
      --net host \
      -e "INTEGRATION_URL=http://localhost:$(cat .integration-port)" \
      -e "VITE_API_URL=http://localhost:$(cat .integration-port)" \
      -e TOWN_OS_REPO_USERNAME=town-os \
      -e TOWN_OS_REPO_PASSWORD=town-os-test \
      -e "TOWN_OS_TEST_REPO_CORE_URL=http://127.0.0.1:$(cat .gitea-port)/town-os/test-packages-core.git" \
      --name "${PODMAN_UI_CONTAINER}" "${PODMAN_UI_IMAGE}" \
      bun run test:integration -- --reporter=verbose || rc=$?
    exit ${rc}
    ;;
  full)
    # Run the full test suite and always clean up containers and btrfs afterward.
    trap '${MAKE} clean-integration clean-btrfs' EXIT
    rc=0
    ${MAKE} test-integration || rc=$?
    ${MAKE} test-ui-integration || rc=$?
    exit ${rc}
    ;;
  auto)
    go get github.com/cespare/reflex@latest
    reflex -r '\.(js|go)$' make test
    ;;
  auto-full)
    go get github.com/cespare/reflex@latest
    sudo -E -E "$(go env GOPATH)/bin/reflex" -r '\.(go|js)$' make test-full
    ;;
  *)
    echo "Usage: $0 {unit|integration|ui-integration|full|auto|auto-full}"
    exit 1
    ;;
esac

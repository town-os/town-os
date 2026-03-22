#!/usr/bin/env bash
# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
set -e
. make/lib.sh

case "$1" in
  unit)
    step "Running Go unit tests"
    go test -v -timeout 60m ./src/...
    step "Running UI unit tests"
    cd ui && bun install && bun run test
    ;;
  # Build integration test image and start the container with all images loaded.
  # Does not run any tests. Called by test-integration; can also be used standalone
  # to prepare the container for test-integration-rerun.
  integration-build)
    step "Creating btrfs volume for integration tests"
    ${MAKE} btrfs
    step "Starting integration test container"
    remove_container "${PODMAN_CONTAINER}"
    # --replace: ensure concurrent make test-full runs never conflict on container names
    ${SUDO} podman run -e "LOG_LEVEL=${LOG_LEVEL}" -e TOWN_OS_TEST=1 \
      -e TOWN_OS_REPO_USERNAME=town-os \
      -e TOWN_OS_REPO_PASSWORD=town-os-test \
      -e "TOWN_OS_LISTEN=:$(cat "${STATE_DIR}/.integration-port")" \
      -e "TOWN_OS_TEST_REPO_CORE_URL=http://127.0.0.1:$(cat "${STATE_DIR}/.gitea-port")/town-os/test-packages-core.git" \
      -e "TOWN_OS_TEST_REPO_EXTRAS_URL=http://127.0.0.1:$(cat "${STATE_DIR}/.gitea-port")/town-os/test-packages-extras.git" \
      -e "ROLODEX_IMAGE=${ROLODEX_IMAGE}" \
      -e "UI_IMAGE=${UI_IMAGE}" \
      -d --net host --systemd=true --privileged \
      --device /dev/btrfs-control:/dev/btrfs-control:rwm \
      -v "$(cat "${STATE_DIR}/town-os.mount"):/town-os:z" \
      -v "${STATE_DIR}/registries.conf:/etc/containers/registries.conf.d/local-registry.conf:ro,z" \
      --replace --name="${PODMAN_CONTAINER}" "${PODMAN_TEST_IMAGE}"
    substep "Waiting for systemd to be ready"
    wait_for_systemd "${PODMAN_CONTAINER}"
    step "Loading monitoring images into test container"
    load_images_into_container "${PODMAN_CONTAINER}" ${MONITORING_IMAGES}
    step "Loading rolodex image into test container"
    load_images_into_container "${PODMAN_CONTAINER}" ${ROLODEX_IMAGE}
    step "Loading UI image into test container"
    load_images_into_container "${PODMAN_CONTAINER}" ${UI_IMAGE}
    step "Loading alpine image into test container"
    load_images_into_container "${PODMAN_CONTAINER}" docker.io/library/alpine:latest
    step "Building network controller image inside test container"
    ${SUDO} podman exec "${PODMAN_CONTAINER}" /bin/sh -c \
      'cd /tmp && mkdir -p nc-build && cd nc-build && \
       cp /town-os-networkcontroller . && \
       printf "FROM docker.io/library/alpine:latest\nRUN apk add --no-cache socat\nCOPY town-os-networkcontroller /town-os-networkcontroller\nENTRYPOINT [\"/town-os-networkcontroller\"]\n" > Containerfile && \
       podman build --dns 1.1.1.1 --pull=never -t town-os-networkcontroller:local -f Containerfile . && \
       cd /tmp && rm -rf nc-build'
    step "Restarting systemcontroller after image loading"
    ${SUDO} podman exec "${PODMAN_CONTAINER}" systemctl reset-failed town-os-systemcontroller.service || true
    ${SUDO} podman exec "${PODMAN_CONTAINER}" systemctl restart town-os-systemcontroller.service
    step "Waiting for systemcontroller API to be ready"
    wait_for_url "http://localhost:$(cat "${STATE_DIR}/.integration-port")/status/ping" 120
    ;;
  # Internal: called by make test-full, do not run standalone (cleanup is handled by make test-full's trap).
  integration)
    step "Running integration tests"
    run_args=(-test.v -test.parallel 1 -test.timeout "${TEST_TIMEOUT:-60m}")
    if [[ -n "${TEST_RUN:-}" ]]; then
      run_args+=(-test.run "${TEST_RUN}")
    fi
    ${SUDO} podman exec -w /test "${PODMAN_CONTAINER}" /integration-test "${run_args[@]}"
    ;;
  # Run integration tests in an already-running container. Use after
  # make test-integration to re-run tests without rebuilding.
  integration-rerun)
    if ! ${SUDO} podman inspect "${PODMAN_CONTAINER}" >/dev/null 2>&1; then
      echo "Error: container ${PODMAN_CONTAINER} not found. Run 'make test-integration' first." >&2
      exit 1
    fi
    if ! ${SUDO} podman inspect --format '{{.State.Running}}' "${PODMAN_CONTAINER}" 2>/dev/null | grep -q true; then
      ${SUDO} podman start "${PODMAN_CONTAINER}"
      substep "Waiting for systemd to be ready"
      wait_for_systemd "${PODMAN_CONTAINER}"
    fi
    step "Running integration tests"
    run_args=(-test.v -test.parallel 1 -test.timeout "${TEST_TIMEOUT:-60m}")
    if [[ -n "${TEST_RUN:-}" ]]; then
      run_args+=(-test.run "${TEST_RUN}")
    fi
    ${SUDO} podman exec -w /test "${PODMAN_CONTAINER}" /integration-test "${run_args[@]}"
    ;;
  ui-unit)
    step "Running UI unit tests"
    cd ui && bun install && bun run test
    ;;
  ui-integration-local)
    step "Running UI integration tests (local backend)"
    cd ui && bun install && bun run test:integration
    ;;
  # Internal: called by make test-full, do not run standalone (cleanup is handled by make test-full's trap).
  ui-integration)
    step "Creating btrfs volume for UI integration tests"
    ${MAKE} btrfs
    step "Starting UI integration backend container"
    remove_container "${PODMAN_UI_CONTAINER}"
    remove_container "${PODMAN_UI_BACKEND}"
    # --replace: ensure concurrent make test-full runs never conflict on container names
    ${SUDO} podman run -e LOG_LEVEL=debug -e DEBUG=1 -e TOWN_OS_TEST=1 \
      -e TOWN_OS_REPO_USERNAME=town-os \
      -e TOWN_OS_REPO_PASSWORD=town-os-test \
      -e "TOWN_OS_LISTEN=:$(cat "${STATE_DIR}/.integration-port")" \
      -e "TOWN_OS_TEST_REPO_CORE_URL=http://127.0.0.1:$(cat "${STATE_DIR}/.gitea-port")/town-os/test-packages-core.git" \
      -e "TOWN_OS_TEST_REPO_EXTRAS_URL=http://127.0.0.1:$(cat "${STATE_DIR}/.gitea-port")/town-os/test-packages-extras.git" \
      -e "ROLODEX_IMAGE=${ROLODEX_IMAGE}" \
      -e "UI_IMAGE=${UI_IMAGE}" \
      -d --net host --systemd=true --privileged \
      --device /dev/btrfs-control:/dev/btrfs-control:rwm \
      -v "$(cat "${STATE_DIR}/town-os.mount"):/town-os:z" \
      -v "${STATE_DIR}/registries.conf:/etc/containers/registries.conf.d/local-registry.conf:ro,z" \
      --replace --name="${PODMAN_UI_BACKEND}" "${PODMAN_TEST_IMAGE}"
    substep "Waiting for systemd to be ready"
    wait_for_systemd "${PODMAN_UI_BACKEND}"
    step "Loading monitoring images into UI integration container"
    load_images_into_container "${PODMAN_UI_BACKEND}" ${MONITORING_IMAGES}
    step "Loading rolodex image into UI integration container"
    load_images_into_container "${PODMAN_UI_BACKEND}" ${ROLODEX_IMAGE}
    step "Loading UI image into UI integration container"
    load_images_into_container "${PODMAN_UI_BACKEND}" ${UI_IMAGE}
    step "Loading alpine image into UI integration container"
    load_images_into_container "${PODMAN_UI_BACKEND}" docker.io/library/alpine:latest
    step "Building network controller image inside UI integration container"
    ${SUDO} podman exec "${PODMAN_UI_BACKEND}" /bin/sh -c \
      'cd /tmp && mkdir -p nc-build && cd nc-build && \
       cp /town-os-networkcontroller . && \
       printf "FROM docker.io/library/alpine:latest\nRUN apk add --no-cache socat\nCOPY town-os-networkcontroller /town-os-networkcontroller\nENTRYPOINT [\"/town-os-networkcontroller\"]\n" > Containerfile && \
       podman build --dns 1.1.1.1 --pull=never -t town-os-networkcontroller:local -f Containerfile . && \
       cd /tmp && rm -rf nc-build'
    step "Restarting systemcontroller after image loading"
    ${SUDO} podman exec "${PODMAN_UI_BACKEND}" systemctl reset-failed town-os-systemcontroller.service || true
    ${SUDO} podman exec "${PODMAN_UI_BACKEND}" systemctl restart town-os-systemcontroller.service
    step "Waiting for systemcontroller API to be ready"
    wait_for_url "http://localhost:$(cat "${STATE_DIR}/.integration-port")/status/ping" 120
    step "Running UI integration tests"
    # --replace: ensure concurrent make test-full runs never conflict on container names
    ${SUDO} podman run \
      --net host \
      -e "INTEGRATION_URL=http://localhost:$(cat "${STATE_DIR}/.integration-port")" \
      -e "VITE_API_URL=http://localhost:$(cat "${STATE_DIR}/.integration-port")" \
      -e TOWN_OS_REPO_USERNAME=town-os \
      -e TOWN_OS_REPO_PASSWORD=town-os-test \
      -e "TOWN_OS_TEST_REPO_CORE_URL=http://127.0.0.1:$(cat "${STATE_DIR}/.gitea-port")/town-os/test-packages-core.git" \
      --replace --name "${PODMAN_UI_CONTAINER}" "${PODMAN_UI_IMAGE}" \
      bun run test:integration -- --reporter=verbose
    ;;
  full)
    step "Running full test suite"
    # Run the full test suite and always clean up containers afterward.
    # IRON RULE: fail fast — if any test phase fails, stop immediately.
    # The EXIT trap guarantees cleanup runs regardless.
    # Inline cleanup — avoid nested make invocations so Ctrl+C exits fast.
    cleanup() {
      remove_container "${PODMAN_CONTAINER}"
      remove_container "${PODMAN_UI_BACKEND}"
      remove_container "${PODMAN_UI_CONTAINER}"
      remove_container "${REGISTRY_CONTAINER}"
      remove_container "${GITEA_CONTAINER}"
      rm -f "${STATE_DIR}/.integration-port" "${STATE_DIR}/.registry-port" "${STATE_DIR}/.gitea-port"
      make/btrfs.sh clean 2>/dev/null || true
    }
    trap cleanup EXIT
    ${MAKE} test-integration
    # Stop the integration container and clean btrfs before UI tests so the
    # integration port is released and the btrfs volume can be recreated.
    remove_container "${PODMAN_CONTAINER}"
    ${MAKE} clean-btrfs
    ${MAKE} test-ui-integration
    ;;
  auto)
    step "Starting auto-test watcher"
    go get github.com/cespare/reflex@latest
    reflex -r '\.(js|go)$' make test
    ;;
  auto-full)
    step "Starting auto-test-full watcher"
    go get github.com/cespare/reflex@latest
    ${SUDO} "$(go env GOPATH)/bin/reflex" -r '\.(go|js)$' make test-full
    ;;
  *)
    echo "Usage: $0 {unit|ui-unit|ui-integration-local|integration-build|integration|integration-rerun|ui-integration|full|auto|auto-full}"
    exit 1
    ;;
esac

#!/usr/bin/env bash
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
    ${SUDO} podman run -e "LOG_LEVEL=${LOG_LEVEL}" -e TOWN_OS_TEST=1 \
      -e TOWN_OS_REPO_USERNAME=town-os \
      -e TOWN_OS_REPO_PASSWORD=town-os-test \
      -e "TOWN_OS_LISTEN=:$(cat .integration-port)" \
      -e "TOWN_OS_TEST_REPO_CORE_URL=http://127.0.0.1:$(cat .gitea-port)/town-os/test-packages-core.git" \
      -e "TOWN_OS_TEST_REPO_EXTRAS_URL=http://127.0.0.1:$(cat .gitea-port)/town-os/test-packages-extras.git" \
      -e "ROLODEX_IMAGE=${ROLODEX_IMAGE}" \
      -d --net host --systemd=true --privileged \
      --device /dev/btrfs-control:/dev/btrfs-control:rwm \
      -v "$(cat town-os.mount):/town-os:z" \
      -v "$(pwd)/.cache/registries.conf:/etc/containers/registries.conf.d/local-registry.conf:ro,z" \
      --name="${PODMAN_CONTAINER}" "${PODMAN_TEST_IMAGE}"
    substep "Waiting for systemd to be ready"
    wait_for_systemd "${PODMAN_CONTAINER}"
    step "Loading monitoring images into test container"
    load_images_into_container "${PODMAN_CONTAINER}" ${MONITORING_IMAGES}
    step "Loading rolodex image into test container"
    load_images_into_container "${PODMAN_CONTAINER}" ${ROLODEX_IMAGE}
    ;;
  # Internal: called by make test-full, do not run standalone (cleanup is handled by make test-full's trap).
  integration)
    step "Running integration tests"
    rc=0
    run_args=(-test.v -test.timeout "${TEST_TIMEOUT:-60m}")
    if [[ -n "${TEST_RUN:-}" ]]; then
      run_args+=(-test.run "${TEST_RUN}")
    fi
    ${SUDO} podman exec -w /test "${PODMAN_CONTAINER}" /integration-test "${run_args[@]}" || rc=$?
    exit ${rc}
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
    rc=0
    run_args=(-test.v -test.timeout "${TEST_TIMEOUT:-60m}")
    if [[ -n "${TEST_RUN:-}" ]]; then
      run_args+=(-test.run "${TEST_RUN}")
    fi
    ${SUDO} podman exec -w /test "${PODMAN_CONTAINER}" /integration-test "${run_args[@]}" || rc=$?
    exit ${rc}
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
    ${SUDO} podman run -e LOG_LEVEL=debug -e DEBUG=1 -e TOWN_OS_TEST=1 \
      -e TOWN_OS_REPO_USERNAME=town-os \
      -e TOWN_OS_REPO_PASSWORD=town-os-test \
      -e "TOWN_OS_LISTEN=:$(cat .integration-port)" \
      -e "TOWN_OS_TEST_REPO_CORE_URL=http://127.0.0.1:$(cat .gitea-port)/town-os/test-packages-core.git" \
      -e "TOWN_OS_TEST_REPO_EXTRAS_URL=http://127.0.0.1:$(cat .gitea-port)/town-os/test-packages-extras.git" \
      -e "ROLODEX_IMAGE=${ROLODEX_IMAGE}" \
      -d --net host --systemd=true --privileged \
      --device /dev/btrfs-control:/dev/btrfs-control:rwm \
      -v "$(cat town-os.mount):/town-os:z" \
      -v "$(pwd)/.cache/registries.conf:/etc/containers/registries.conf.d/local-registry.conf:ro,z" \
      --name="${PODMAN_UI_BACKEND}" "${PODMAN_TEST_IMAGE}"
    substep "Waiting for systemd to be ready"
    wait_for_systemd "${PODMAN_UI_BACKEND}"
    step "Loading monitoring images into UI integration container"
    load_images_into_container "${PODMAN_UI_BACKEND}" ${MONITORING_IMAGES}
    step "Loading rolodex image into UI integration container"
    load_images_into_container "${PODMAN_UI_BACKEND}" ${ROLODEX_IMAGE}
    substep "Waiting for systemcontroller API to be ready"
    wait_for_url "http://localhost:$(cat .integration-port)/status/ping" 60
    step "Running UI integration tests"
    rc=0
    ${SUDO} podman run \
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
    step "Running full test suite"
    # Run the full test suite and always clean up containers afterward.
    trap '${MAKE} clean-integration' EXIT
    rc=0
    ${MAKE} test-integration || rc=$?
    # Stop the integration container and clean btrfs before UI tests so the
    # integration port is released and the btrfs volume can be recreated.
    remove_container "${PODMAN_CONTAINER}"
    ${MAKE} clean-btrfs
    ${MAKE} test-ui-integration || rc=$?
    exit ${rc}
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

#!/usr/bin/env bash
# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
set -e
. make/lib.sh

: "${GO_BUILD_TAGS:=}"
GO_TAGS_ARG=()
if [[ -n "${GO_BUILD_TAGS}" ]]; then
  GO_TAGS_ARG=(-tags "${GO_BUILD_TAGS}")
fi

case "$1" in
  unit)
    step "Running Go unit tests"
    # -count=1 defeats Go's test cache, and it is not belt-and-braces here.
    #
    # ./src/... is not a pure unit suite: the TestIntegration* tests live in
    # these same packages, and they touch state Go's cache cannot see —
    # containers, btrfs subvolumes, systemd units, host ports. The cache keys on
    # the test binary and the files a test read, so a run that touched none of
    # those is replayed as a PASS even though the box it asserts about has
    # changed underneath it. A cached PASS is then not evidence the test passes
    # now, which is the one thing a test run is for.
    #
    # The build cache is untouched, so this costs re-execution, not
    # re-compilation. ./integration/... needs nothing equivalent: it is compiled
    # to a binary with `go test -c` and executed directly, and the test cache
    # only ever applies to `go test` runs.
    go test "${GO_TAGS_ARG[@]}" -count=1 -v -timeout 60m ./src/...
    step "Running UI unit tests"
    bun_install ui
    cd ui && bun run test
    ;;
  # Build integration test image and start the container with all images loaded.
  # Does not run any tests. Called by test-integration; can also be used standalone
  # to prepare the container for test-integration-rerun.
  integration-build)
    step "Creating btrfs volume for integration tests"
    ${MAKE} btrfs
    step "Starting integration test container"
    system_port_env
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
      -e "NC_IMAGE=${NC_IMAGE}" \
      -e "INGRESS_IMAGE=${INGRESS_IMAGE}" \
      -e "GFEH_IMAGE=${GFEH_IMAGE}" \
      "${SYSTEM_PORT_ENV[@]}" \
      -e "TOWN_OS_WG_SALT=$(wireguard_salt test)" \
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
    # Built on the host by the nc-image make target; building inside the
    # container needed hardcoded public DNS, which captive networks block.
    step "Loading network controller image into test container"
    load_images_into_container "${PODMAN_CONTAINER}" ${NC_IMAGE}
    step "Loading ingress image into test container"
    load_images_into_container "${PODMAN_CONTAINER}" ${INGRESS_IMAGE}
    # The real gfehd. Object storage is skipped entirely when GFEH_IMAGE is
    # empty, so an integration test of it needs the actual daemon here -- there
    # is no stand-in that would prove a partition starts, answers its admin
    # socket, and enforces its own ceilings.
    step "Loading gfeh image into test container"
    load_images_into_container "${PODMAN_CONTAINER}" ${GFEH_IMAGE}
    step "Restarting systemcontroller after image loading"
    ${SUDO} podman exec "${PODMAN_CONTAINER}" systemctl reset-failed town-os-systemcontroller.service || true
    ${SUDO} podman exec "${PODMAN_CONTAINER}" systemctl restart town-os-systemcontroller.service
    step "Waiting for systemcontroller API to be ready"
    wait_for_url "http://localhost:$(cat "${STATE_DIR}/.integration-port")/status/ping" 120
    ;;
  # Internal: called by make test-full, do not run standalone (cleanup is handled by make test-full's trap).
  integration)
    step "Running integration tests"
    run_args=(-test.v -test.parallel 4 -test.timeout "${TEST_TIMEOUT:-60m}")
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
    run_args=(-test.v -test.parallel 4 -test.timeout "${TEST_TIMEOUT:-60m}")
    if [[ -n "${TEST_RUN:-}" ]]; then
      run_args+=(-test.run "${TEST_RUN}")
    fi
    ${SUDO} podman exec -w /test "${PODMAN_CONTAINER}" /integration-test "${run_args[@]}"
    ;;
  ui-unit)
    step "Running UI unit tests"
    bun_install ui
    cd ui && bun run test
    ;;
  ui-integration-local)
    step "Running UI integration tests (local backend)"
    bun_install ui
    cd ui && bun run test:integration
    ;;
  # Internal: called by make test-full, do not run standalone (cleanup is handled by make test-full's trap).
  ui-integration)
    step "Creating btrfs volume for UI integration tests"
    ${MAKE} btrfs
    step "Starting UI integration backend container"
    system_port_env
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
      -e "NC_IMAGE=${NC_IMAGE}" \
      -e "INGRESS_IMAGE=${INGRESS_IMAGE}" \
      -e "GFEH_IMAGE=${GFEH_IMAGE}" \
      "${SYSTEM_PORT_ENV[@]}" \
      -e "TOWN_OS_WG_SALT=$(wireguard_salt test)" \
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
    # Built on the host by the nc-image make target; building inside the
    # container needed hardcoded public DNS, which captive networks block.
    step "Loading network controller image into UI integration container"
    load_images_into_container "${PODMAN_UI_BACKEND}" ${NC_IMAGE}
    step "Loading ingress image into UI integration container"
    load_images_into_container "${PODMAN_UI_BACKEND}" ${INGRESS_IMAGE}
    # The UI's object storage panel is empty without a running partition, and
    # GFEH_IMAGE is already injected above -- an image that is named but absent
    # leaves the daemon failing to pull on every reconcile.
    step "Loading gfeh image into UI integration container"
    load_images_into_container "${PODMAN_UI_BACKEND}" ${GFEH_IMAGE}
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
      remove_container "${PODMAN_UI_CONTAINER}"
      remove_container "${REGISTRY_CONTAINER}"
      remove_container "${GITEA_CONTAINER}"
      rm -f "${STATE_DIR}/.integration-port" "${STATE_DIR}/.registry-port" "${STATE_DIR}/.gitea-port"
      # System-service ports (SYSTEM_PORT_FILES) are per run too — leaving them
      # behind would pin the next run to the same host ports.
      rm -f "${STATE_DIR}/.dns-port" "${STATE_DIR}/.rolodex-metrics-port" \
        "${STATE_DIR}/.node-exporter-port" \
        "${STATE_DIR}/.prometheus-port" "${STATE_DIR}/.monitoring-port" \
        "${STATE_DIR}/.ingress-https-port" "${STATE_DIR}/.ingress-http-port"
      make/btrfs.sh clean 2>/dev/null || true
      # Prune orphaned volumes to free podman locks. Without this, repeated
      # test runs exhaust the lock table (default 2048).
      ${SUDO} podman volume prune -f 2>/dev/null || true
    }
    trap cleanup EXIT
    ${MAKE} test-integration
    # Reuse the integration container for UI tests — skip re-importing all
    # container images. Just wipe btrfs subvolumes, reset the DB, and
    # restart the systemcontroller.
    step "Resetting state for UI integration tests"
    # Stop the CONTAINERS before wiping what they are mounted on, not just the
    # systemcontroller. Every package unit and system service from the
    # integration run is still up, bind-mounting a subvolume under /town-os and
    # carrying Restart=always -- so deleting those subvolumes out from under
    # them starts a restart storm whose ExecStart is `podman run` against the
    # host podman socket. The next `systemctl restart` then waits on a systemd
    # that is busy servicing it, and the step hangs with no timeout to end it.
    # Reset-failed afterwards so the storm's failures do not survive into the
    # UI run's unit status.
    #
    # Every command here runs under `timeout`. Not belt-and-braces: this step
    # is a straight line of `podman exec`s with no deadline anywhere in it, so
    # a wedged systemd or a contended host podman socket (several concurrent
    # `make test-full` runs share one) turns the whole suite into a silent hang
    # holding the terminal. A non-zero exit is recoverable; a hang is not. The
    # exit code is not swallowed -- timeout's 124 propagates under set -e.
    substep "Stopping package and system units before the wipe"
    timeout 300 ${SUDO} podman exec "${PODMAN_CONTAINER}" /bin/sh -c \
      'systemctl stop "town-os-package--*" "town-os-system--*" town-os-systemcontroller.service 2>/dev/null; systemctl reset-failed "town-os-*" 2>/dev/null; true' || true
    # Delete all btrfs subvolumes under /town-os to get a clean slate.
    timeout 300 ${SUDO} podman exec "${PODMAN_CONTAINER}" /bin/sh -c \
      'btrfs subvolume list -o /town-os 2>/dev/null | awk "{print \$NF}" | sort -r | while read sv; do btrfs subvolume delete "/town-os/${sv}" 2>/dev/null; done; true'
    # Remove the DB so the systemcontroller re-initializes accounts/sessions.
    timeout 60 ${SUDO} podman exec "${PODMAN_CONTAINER}" /bin/sh -c 'rm -f /data/db/*.db'
    # Set DEBUG+LOG_LEVEL for UI tests.
    timeout 60 ${SUDO} podman exec "${PODMAN_CONTAINER}" systemctl set-environment DEBUG=1 LOG_LEVEL=debug
    timeout 60 ${SUDO} podman exec "${PODMAN_CONTAINER}" systemctl reset-failed town-os-systemcontroller.service || true
    # --no-block: with Type=simple there is nothing to wait for that
    # wait_for_url does not already wait for, and a blocking restart is what
    # turns a busy systemd into an unbounded hang. Readiness is proven by the
    # ping poll below, which has a deadline.
    timeout 60 ${SUDO} podman exec "${PODMAN_CONTAINER}" systemctl restart --no-block town-os-systemcontroller.service
    wait_for_url "http://localhost:$(cat "${STATE_DIR}/.integration-port")/status/ping" 120
    # Put the baked fixture unit back. It was collateral from the
    # `town-os-package--*` stop above, which exists to keep real package
    # containers from restart-storming the wipe -- but this one is a
    # Type=oneshot echo with no volumes and no podman, so it has nothing to do
    # with that hazard and nothing restarts it afterwards. The UI suite asserts
    # it is listed active, which is the whole reason the image bakes and enables
    # it; leaving it stopped means the UI phase sees a different box than the
    # integration phase did.
    substep "Restarting the baked fixture unit for the UI phase"
    timeout 60 ${SUDO} podman exec "${PODMAN_CONTAINER}" \
      systemctl start town-os-package--repo-test-1.0.service
    step "Running UI integration tests"
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

#!/usr/bin/env bash

# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
set -e
. make/lib.sh

# Host DNS redirection: redirect_host_dns / restore_host_dns,
# adopt_orphan_dns_backup, and the helpers they are built from.
. make/dns.sh

# Sourcing this file pulls in make/dns.sh and stops here, without dispatching
# and without requiring an argument. That is what lets the DNS restore logic be
# tested through dev.sh (see src/rolodex/dev_restore_dns_test.go) rather than
# only through a `make dev` that rewrites the host's resolver. The redirect side
# is driven by sourcing make/dns.sh directly, which needs no guard at all
# (src/svc/systemcontroller/dev_dns_script_test.go).
if [ "${BASH_SOURCE[0]}" != "${0}" ]; then
  return 0
fi

case "$1" in
  start)
    step "Starting dev environment"
    # Kill orphaned monitoring containers from a previous dev run that may
    # still be holding host ports (node-exporter on 9100, prometheus NC on
    # 9090, monitoring-ui NC on 5308).
    for c in town-os-system--node-exporter \
             town-os-system--prometheus town-os-system--prometheus-network \
             town-os-system--monitoring-ui town-os-system--monitoring-ui-network; do
      ${SUDO} podman rm -f "$c" 2>/dev/null || true
    done
    ${SUDO} podman rm -f "${PODMAN_DEV_CONTAINER}"
    mkdir -p "${STATE_DIR}/dev-data" "${STATE_DIR}/dev-repos" "${STATE_DIR}/dev-rolodex"
    # Object storage runs for real in dev only if `make gfeh-image` has been run:
    # quay.io/town/gfeh is not public, so a GFEH_IMAGE the dev container cannot
    # load is worse than none — the partition unit crash-loops on the pull and
    # every /gfeh/* route answers 503 behind a UI that still offers the tabs.
    # An explicitly empty value is the documented off switch, so the page says
    # "not configured" instead, which is at least true.
    DEV_GFEH_IMAGE="${GFEH_IMAGE}"
    if [ ! -f "$(image_cache_tar "${GFEH_IMAGE}")" ]; then
      warn "No cached ${GFEH_IMAGE}; disabling object storage in dev (run 'make gfeh-image' to enable it)"
      DEV_GFEH_IMAGE=""
    fi
    substep "Launching dev container"
    ${SUDO} podman run -d --replace --net host -e LOG_LEVEL=debug -e DEBUG=1 \
      -e "TOWN_OS_REPO_USERNAME=${TOWN_OS_REPO_USERNAME}" \
      -e "TOWN_OS_REPO_PASSWORD=${TOWN_OS_REPO_PASSWORD}" \
      -e "TOWN_OS_TAG=${TOWN_OS_TAG}" \
      -e "ROLODEX_IMAGE=${ROLODEX_IMAGE}" \
      -e "UI_IMAGE=" \
      -e "NC_IMAGE=${NC_IMAGE}" \
      -e "GFEH_IMAGE=${DEV_GFEH_IMAGE}" \
      -e "TOWN_OS_WG_SALT=$(wireguard_salt dev)" \
      --systemd=true --privileged \
      --device /dev/btrfs-control:/dev/btrfs-control:rwm \
      -v "$(cat "${STATE_DIR}/town-os-dev.mount"):/town-os:z" \
      -v "${STATE_DIR}/dev-data:/data/db:z" \
      -v "${STATE_DIR}/dev-repos:/data/repos:z" \
      -v "${STATE_DIR}/dev-rolodex:/town-os/rolodex:z" \
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
    step "Loading alpine image into dev container"
    load_images_into_container "${PODMAN_DEV_CONTAINER}" docker.io/library/alpine:latest
    # Built on the host by the nc-image-dev make target; building inside the
    # container needed hardcoded public DNS, which captive networks block.
    step "Loading network controller image into dev container"
    load_images_into_container "${PODMAN_DEV_CONTAINER}" ${NC_IMAGE}
    # Built on the host by `make gfeh-image`, for the same reason as the NC.
    if [ -n "${DEV_GFEH_IMAGE}" ]; then
      step "Loading object storage (gfeh) image into dev container"
      load_images_into_container "${PODMAN_DEV_CONTAINER}" ${DEV_GFEH_IMAGE}
    fi
    step "Restarting systemcontroller after image loading"
    ${SUDO} podman exec "${PODMAN_DEV_CONTAINER}" systemctl reset-failed town-os-systemcontroller.service || true
    ${SUDO} podman exec "${PODMAN_DEV_CONTAINER}" systemctl restart town-os-systemcontroller.service
    substep "Waiting for systemcontroller API to be ready"
    wait_for_url "http://localhost:5309/status/ping" 120
    # Create a default dev account and authenticate unless NO_ACCOUNT=1.
    DEV_TOKEN=""
    if [ "${NO_ACCOUNT:-}" != "1" ]; then
      step "Creating dev account (townos / townos!!)"
      curl -sf -X POST http://localhost:5309/account/create \
        -H 'Content-Type: application/json' \
        -d '{"username":"townos","password":"townos!!","email":"dev@town-os.local","phone":"555-0100","real_name":"Town OS Developer","admin":true}' \
        >/dev/null 2>&1 || true
      DEV_TOKEN=$(curl -sf -X POST http://localhost:5309/account/authenticate \
        -H 'Content-Type: application/json' \
        -d '{"username":"townos","password":"townos!!"}' \
        2>/dev/null | grep -o '"token":"[^"]*"' | cut -d'"' -f4) || true
    fi
    step "Redirecting host DNS to rolodex"
    # Registered before the redirect, so a failure part-way through it still
    # restores. The signal traps matter as much as EXIT: bash does not reliably
    # run an EXIT trap when it dies from a signal it has no handler for, and
    # Ctrl-C on the UI dev server below is the normal way to stop dev — a missed
    # restore leaves the host resolving through a rolodex that is about to be
    # torn down.
    trap restore_host_dns EXIT
    trap 'restore_host_dns; exit 130' INT HUP TERM
    redirect_host_dns
    step "Starting UI dev server"
    substep "API server: http://$(hostname):5309"
    if [ -n "${DEV_TOKEN}" ]; then
      substep "Dashboard:  http://$(hostname):5173/?token=${DEV_TOKEN}"
    else
      substep "Dashboard:  http://$(hostname):5173"
    fi
    bun_install ui
    cd ui && bun run dev -- --host
    # The dev server has stopped: give the host its resolver back before the
    # container teardown below, which is slow and needs no DNS of ours.
    restore_host_dns
    # Stop services inside the dev container before removing it so
    # monitoring containers (which share the host network/PID namespace)
    # do not orphan conmon processes that hold ports.
    ${SUDO} podman exec "${PODMAN_DEV_CONTAINER}" systemctl stop \
      town-os-system--node-exporter.service \
      town-os-system--prometheus.service \
      town-os-system--prometheus-network.service \
      town-os-system--monitoring-ui.service \
      town-os-system--monitoring-ui-network.service \
      2>/dev/null || true
    ${SUDO} podman rm -f "${PODMAN_DEV_CONTAINER}"
    ;;
  logs)
    step "Streaming dev container logs"
    ${SUDO} podman exec -it "${PODMAN_DEV_CONTAINER}" journalctl -f
    ;;
  stop)
    step "Stopping dev container"
    restore_host_dns
    # Stop services inside the container before removal.
    ${SUDO} podman exec "${PODMAN_DEV_CONTAINER}" systemctl stop \
      town-os-system--node-exporter.service \
      town-os-system--prometheus.service \
      town-os-system--prometheus-network.service \
      town-os-system--monitoring-ui.service \
      town-os-system--monitoring-ui-network.service \
      2>/dev/null || true
    # Stop and remove the dev container for this working directory.
    remove_container "${PODMAN_DEV_CONTAINER}"
    # Clean up orphaned monitoring containers on the host.
    for c in town-os-system--node-exporter \
             town-os-system--prometheus town-os-system--prometheus-network \
             town-os-system--monitoring-ui town-os-system--monitoring-ui-network; do
      ${SUDO} podman rm -f "$c" 2>/dev/null || true
    done
    ;;
  stop-all)
    step "Stopping all dev containers"
    restore_host_dns
    # Stop and remove all town-os dev containers (from any working directory).
    ${SUDO} podman ps -a --format '{{.Names}}' 2>/dev/null \
      | grep -E '^town-os-dev$' \
      | xargs -r -I{} ${SUDO} podman rm -f {} 2>/dev/null || true
    # Clean up orphaned monitoring containers on the host.
    for c in town-os-system--node-exporter \
             town-os-system--prometheus town-os-system--prometheus-network \
             town-os-system--monitoring-ui town-os-system--monitoring-ui-network; do
      ${SUDO} podman rm -f "$c" 2>/dev/null || true
    done
    ;;
  restore-dns)
    # Escape hatch for a dev run that died hard enough to skip its own restore
    # (SIGKILL, a lost terminal), including one whose checkout has since been
    # deleted — see adopt_orphan_dns_backup. No-op when there is nothing to put
    # back, or when the host is not pointed at rolodex in the first place.
    step "Restoring host DNS"
    adopt_orphan_dns_backup
    restore_host_dns
    ;;
  *)
    echo "Usage: $0 {start|logs|stop|stop-all|restore-dns}"
    exit 1
    ;;
esac

#!/usr/bin/env bash

# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
set -e
. make/lib.sh

DNS_BACKUP="${STATE_DIR}/resolv.conf.bak"

# restore_host_dns — restore /etc/resolv.conf from backup if it exists.
restore_host_dns() {
  if [ -f "${DNS_BACKUP}" ]; then
    local first_line
    first_line="$(head -1 "${DNS_BACKUP}")"
    if [ "${first_line}" = "__SYMLINK__" ]; then
      local target
      target="$(sed -n '2p' "${DNS_BACKUP}")"
      ${SUDO} rm -f /etc/resolv.conf
      ${SUDO} ln -s "${target}" /etc/resolv.conf
    else
      ${SUDO} cp "${DNS_BACKUP}" /etc/resolv.conf
    fi
    rm -f "${DNS_BACKUP}"
    step "Restored /etc/resolv.conf"
  fi
}

# redirect_host_dns — back up /etc/resolv.conf and point it at rolodex (127.0.0.2).
redirect_host_dns() {
  # Wait for rolodex to be listening on 127.0.0.2:53
  substep "Waiting for rolodex DNS on 127.0.0.2:53"
  local waited=0
  while [ "${waited}" -lt 30 ]; do
    if (echo >/dev/tcp/127.0.0.2/53) 2>/dev/null; then
      break
    fi
    sleep 1
    waited=$((waited + 1))
  done
  if [ "${waited}" -ge 30 ]; then
    warn "Rolodex did not start within 30s — skipping DNS redirect"
    return 0
  fi
  substep "Rolodex is listening"

  # Back up current /etc/resolv.conf (handle symlinks)
  if [ -L /etc/resolv.conf ]; then
    local target
    target="$(readlink -f /etc/resolv.conf)"
    printf '__SYMLINK__\n%s\n' "${target}" > "${DNS_BACKUP}"
  else
    cp /etc/resolv.conf "${DNS_BACKUP}"
  fi

  # Rewrite resolv.conf
  printf 'nameserver 127.0.0.2\n' | ${SUDO} tee /etc/resolv.conf >/dev/null

  printf "${_yellow}%s${_reset}\n" \
    "╔══════════════════════════════════════════════════════════════╗" \
    "║  WARNING: /etc/resolv.conf has been rewritten to use        ║" \
    "║  Town OS DNS (127.0.0.2). Your system DNS is now routed     ║" \
    "║  through the dev rolodex container.                         ║" \
    "║                                                             ║" \
    "║  Run 'make clean-dev' to restore your original DNS config.  ║" \
    "╚══════════════════════════════════════════════════════════════╝"
}

case "$1" in
  start)
    step "Starting dev environment"
    ${SUDO} podman rm -f "${PODMAN_DEV_CONTAINER}"
    mkdir -p "${STATE_DIR}/dev-data" "${STATE_DIR}/dev-repos" "${STATE_DIR}/dev-rolodex"
    substep "Launching dev container"
    ${SUDO} podman run -d --replace --net host -e LOG_LEVEL=debug -e DEBUG=1 \
      -e "TOWN_OS_REPO_USERNAME=${TOWN_OS_REPO_USERNAME}" \
      -e "TOWN_OS_REPO_PASSWORD=${TOWN_OS_REPO_PASSWORD}" \
      -e TOWN_OS_NETWORK_MODE=host \
      -e TOWN_OS_PAGES=1 \
      -e ROLODEX_LOCAL=1 \
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
    step "Building network controller image inside dev container"
    ${SUDO} podman exec "${PODMAN_DEV_CONTAINER}" /bin/sh -c \
      'cd /tmp && mkdir -p nc-build && cd nc-build && \
       cp /town-os-networkcontroller . && \
       printf "FROM docker.io/library/alpine:latest\nRUN apk add --no-cache socat\nCOPY town-os-networkcontroller /town-os-networkcontroller\nCMD [\"/town-os-networkcontroller\"]\n" > Containerfile && \
       podman build --dns 1.1.1.1 --pull=never -t localhost/town-os-networkcontroller:local -f Containerfile . && \
       cd /tmp && rm -rf nc-build'
    step "Redirecting host DNS to rolodex"
    redirect_host_dns
    # Ensure DNS is restored when the UI dev server exits (Ctrl-C, crash, etc.)
    trap restore_host_dns EXIT
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
    restore_host_dns
    # Stop and remove the dev container for this working directory.
    remove_container "${PODMAN_DEV_CONTAINER}"
    ;;
  stop-all)
    step "Stopping all dev containers"
    restore_host_dns
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

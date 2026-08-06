#!/usr/bin/env bash

# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
set -e
. make/lib.sh

DNS_BACKUP="${STATE_DIR}/resolv.conf.bak"
RESOLVED_BACKUP="${STATE_DIR}/resolved-dns.bak"
ROLODEX_DNS="127.0.0.2"

# The two host paths the DNS redirect touches, overridable ONLY so the test
# suite can drive the whole restore path against a temp directory. A test able
# to reach the real /etc/resolv.conf is a test that can take the machine
# running it off the network, which is the failure this code exists to repair.
# Unset in every real invocation, so production behavior is the literals.
RESOLV_CONF="${TOWN_OS_RESOLV_CONF:-/etc/resolv.conf}"
STATE_GLOB="${TOWN_OS_STATE_GLOB:-/tmp/town-os-*}"

# resolved_running — true when systemd-resolved is the host's resolver.
#
# Rewriting /etc/resolv.conf is not enough on a resolved box: anything that
# resolves through nss-resolve or the D-Bus API (systemd units, NetworkManager,
# Chrome's built-in resolver on some builds) never reads resolv.conf, so .home
# names fail there while `dig` works — the exact split that makes dev DNS look
# haunted.
resolved_running() {
  command -v resolvectl >/dev/null 2>&1 || return 1
  systemctl is-active --quiet systemd-resolved 2>/dev/null
}

# dns_links — the interfaces actually carrying traffic: whatever holds a default
# route, v4 or v6. Those are the links resolved consults, so those are the ones
# that have to point at rolodex.
dns_links() {
  {
    ip -4 route show default 2>/dev/null
    ip -6 route show default 2>/dev/null
  } | awk '{ for (i = 1; i < NF; i++) if ($i == "dev") print $(i + 1) }' | sort -u
}

# link_setting <dns|domain> <link> — the current value, with resolvectl's
# "Link 2 (eth0): " prefix stripped. Empty when the link has none set.
link_setting() {
  resolvectl "$1" "$2" 2>/dev/null | sed -n '1s/^[^:]*: *//p'
}

# redirect_resolved_dns — point each in-use link's resolved DNS at rolodex.
#
# `~.` is what makes it stick: without a routing domain resolved is free to
# prefer another link's servers for names it thinks they own, and a .home query
# leaving for the LAN resolver is an NXDOMAIN.
redirect_resolved_dns() {
  if ! resolved_running; then
    substep "systemd-resolved not running — resolv.conf only"
    return 0
  fi
  local links link dns domains
  links="$(dns_links)"
  if [ -z "${links}" ]; then
    warn "No default-route interface found — skipping systemd-resolved"
    return 0
  fi
  : > "${RESOLVED_BACKUP}"
  for link in ${links}; do
    dns="$(link_setting dns "${link}")"
    domains="$(link_setting domain "${link}")"
    # Recorded before the change, so the restore below puts back exactly what
    # NetworkManager/networkd had pushed rather than guessing.
    printf '%s|%s|%s\n' "${link}" "${dns}" "${domains}" >> "${RESOLVED_BACKUP}"
    if ${SUDO} resolvectl dns "${link}" "${ROLODEX_DNS}" \
      && ${SUDO} resolvectl domain "${link}" '~.'; then
      substep "systemd-resolved: ${link} -> ${ROLODEX_DNS} (~.)"
    else
      warn "Could not set systemd-resolved DNS on ${link}"
    fi
  done
  ${SUDO} resolvectl flush-caches 2>/dev/null || true
}

# restore_resolved_dns — put every link recorded above back the way it was.
#
# Explicit restore rather than `resolvectl revert`: revert drops the settings
# NetworkManager pushed at connection time and NM does not re-push them until
# the connection is reactivated, so a reverted link is left with no DNS at all.
# Revert is only the fallback for a link that genuinely had nothing set.
restore_resolved_dns() {
  [ -f "${RESOLVED_BACKUP}" ] || return 0
  if ! resolved_running; then
    rm -f "${RESOLVED_BACKUP}"
    return 0
  fi
  local link dns domains
  while IFS='|' read -r link dns domains; do
    [ -n "${link}" ] || continue
    if [ -n "${dns}" ] || [ -n "${domains}" ]; then
      # Unquoted on purpose: both fields are space-separated lists.
      # shellcheck disable=SC2086
      ${SUDO} resolvectl dns "${link}" ${dns} 2>/dev/null \
        || ${SUDO} resolvectl revert "${link}" 2>/dev/null || true
      # shellcheck disable=SC2086
      ${SUDO} resolvectl domain "${link}" ${domains} 2>/dev/null || true
    else
      ${SUDO} resolvectl revert "${link}" 2>/dev/null || true
    fi
    substep "Restored systemd-resolved DNS on ${link}"
  done < "${RESOLVED_BACKUP}"
  ${SUDO} resolvectl flush-caches 2>/dev/null || true
  rm -f "${RESOLVED_BACKUP}"
}

# restore_resolv_conf — restore resolv.conf from backup if it exists.
restore_resolv_conf() {
  if [ -f "${DNS_BACKUP}" ]; then
    local first_line
    first_line="$(head -1 "${DNS_BACKUP}")"
    if [ "${first_line}" = "__SYMLINK__" ]; then
      local target
      target="$(sed -n '2p' "${DNS_BACKUP}")"
      ${SUDO} rm -f "${RESOLV_CONF}"
      ${SUDO} ln -s "${target}" "${RESOLV_CONF}"
    else
      ${SUDO} cp "${DNS_BACKUP}" "${RESOLV_CONF}"
    fi
    rm -f "${DNS_BACKUP}"
    step "Restored ${RESOLV_CONF}"
  fi
}

# restore_host_dns — undo everything redirect_host_dns did. Safe to call when
# nothing was redirected, and safe to call twice.
restore_host_dns() {
  restore_resolved_dns
  restore_resolv_conf
}

# host_dns_redirected — true when the host still resolves through rolodex, by
# either half of the redirect.
host_dns_redirected() {
  if grep -qs "^[[:space:]]*nameserver[[:space:]]\+${ROLODEX_DNS}[[:space:]]*\$" "${RESOLV_CONF}"; then
    return 0
  fi
  if ! resolved_running; then
    return 1
  fi
  resolvectl status 2>/dev/null | grep -q "Current DNS Server: ${ROLODEX_DNS}"
}

# adopt_orphan_dns_backup — repoint DNS_BACKUP/RESOLVED_BACKUP at a leftover
# backup that belongs to a checkout which no longer exists.
#
# STATE_DIR is keyed to the md5 of the working directory, so the backup a
# `make dev` took is only reachable from the directory that took it. Delete that
# worktree — the normal end of a worktree's life — and the escape hatch can no
# longer find the file it exists to restore: restore_resolv_conf opens with
# `[ -f "${DNS_BACKUP}" ] || return 0`, so `make dev-restore-dns` from any
# surviving checkout exits 0 having done nothing, while the host stays pointed
# at a rolodex that is gone and every name on the box fails to resolve.
#
# Only the explicit restore-dns path calls this. The trap-driven restore at the
# end of a normal dev run must consume its own backup and nothing else, or one
# dev box would put the host back the way a different one found it.
#
# Three guards, because this writes /etc/resolv.conf from a file nobody asked
# for by name:
#
#   1. Nothing is adopted unless this instance has no backup of its own — a real
#      one always wins.
#   2. Nothing is adopted unless the host is STILL redirected at rolodex. A
#      backup left behind by a run that did restore is stale, and copying it
#      over a working resolv.conf would break the thing this repairs.
#   3. Only files we own, since /tmp is world-writable and another user's
#      leftovers are neither ours to consume nor readable anyway.
adopt_orphan_dns_backup() {
  if [ -f "${DNS_BACKUP}" ] || [ -f "${RESOLVED_BACKUP}" ]; then
    return 0
  fi
  if ! host_dns_redirected; then
    return 0
  fi

  local newest="" candidate
  # Unquoted on purpose: STATE_GLOB is a glob, not a path.
  # shellcheck disable=SC2231
  for candidate in ${STATE_GLOB}/resolv.conf.bak; do
    [ -f "${candidate}" ] || continue
    [ -O "${candidate}" ] || continue
    if [ -z "${newest}" ] || [ "${candidate}" -nt "${newest}" ]; then
      newest="${candidate}"
    fi
  done
  if [ -z "${newest}" ]; then
    return 0
  fi

  local dir
  dir="$(dirname "${newest}")"
  warn "No DNS backup for this checkout; adopting the orphan left in ${dir}"
  DNS_BACKUP="${newest}"
  RESOLVED_BACKUP="${dir}/resolved-dns.bak"
}

# redirect_host_dns — route the host's name resolution through the dev rolodex,
# both halves of it: systemd-resolved's per-link servers and /etc/resolv.conf.
redirect_host_dns() {
  # Wait for rolodex to be listening on 127.0.0.2:53
  substep "Waiting for rolodex DNS on ${ROLODEX_DNS}:53"
  local waited=0
  while [ "${waited}" -lt 30 ]; do
    if (echo >"/dev/tcp/${ROLODEX_DNS}/53") 2>/dev/null; then
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

  redirect_resolved_dns

  # Back up the current resolv.conf (handle symlinks)
  if [ -L "${RESOLV_CONF}" ]; then
    local target
    target="$(readlink -f "${RESOLV_CONF}")"
    printf '__SYMLINK__\n%s\n' "${target}" > "${DNS_BACKUP}"
    # Drop the symlink instead of writing through it. It usually points at
    # resolved's generated stub-resolv.conf, and clobbering that file leaves
    # the host pointed at a dead rolodex long after dev has exited — the
    # restore below puts the symlink back, but not the file's contents.
    ${SUDO} rm -f "${RESOLV_CONF}"
  else
    cp "${RESOLV_CONF}" "${DNS_BACKUP}"
  fi

  # Rewrite resolv.conf
  printf 'nameserver %s\n' "${ROLODEX_DNS}" | ${SUDO} tee "${RESOLV_CONF}" >/dev/null

  printf "${_yellow}%s${_reset}\n" \
    "╔══════════════════════════════════════════════════════════════╗" \
    "║  WARNING: host DNS now goes through Town OS rolodex          ║" \
    "║  (127.0.0.2). Both /etc/resolv.conf and the systemd-resolved ║" \
    "║  servers on your default-route interfaces were rewritten.    ║" \
    "║                                                              ║" \
    "║  Both are restored when this dev server stops, and by        ║" \
    "║  'make dev-stop' / 'make clean-dev'.                         ║" \
    "╚══════════════════════════════════════════════════════════════╝"
}

# Sourcing this file defines the functions above and stops here, without
# dispatching and without requiring an argument. That is what lets the DNS
# restore logic be tested directly (see src/rolodex/dev_restore_dns_test.go)
# rather than only through a `make dev` that rewrites the host's resolver.
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

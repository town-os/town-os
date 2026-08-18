#!/usr/bin/env bash

# make/dns.sh - Host DNS redirection for the dev server.
# Source this file AFTER make/lib.sh: . make/dns.sh
#
# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
#
# Split out of make/dev.sh because this is the one part of the dev harness that
# mutates the host's resolver -- the single thing the test rules say a test must
# never touch (no rewriting /etc/resolv.conf, no signalling systemd-resolved, no
# binding the host's :53). Its own file means the unit suite can source exactly
# this and nothing else, with stubbed ip/resolvectl/systemctl on PATH and SUDO
# pinned to a recorder, and assert on what WOULD have been run without a single
# real call reaching the host. dev.sh sources it and is otherwise unaware of how
# any of it works.
#
# Requires from lib.sh: SUDO, step, substep, warn, _yellow, _reset, STATE_DIR.
DNS_BACKUP="${STATE_DIR}/resolv.conf.bak"
RESOLVED_BACKUP="${STATE_DIR}/resolved-dns.bak"
ROLODEX_DNS="127.0.0.2"

# The systemd unit the dev container runs rolodex under, which is what
# systemd.SystemServiceUnitName("rolodex") produces. Named here because the
# readiness wait below asks the container about it directly rather than
# inferring rolodex's health from a socket.
ROLODEX_UNIT="town-os-system--rolodex.service"

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

# rolodex_dns_is_bound — true when something holds ROLODEX_DNS:53 in the HOST
# network namespace, which is the only namespace there is here: every Town OS
# container runs --net host, the dev container included, so a rolodex started
# inside it binds the host's 127.0.0.2:53.
#
# Both transports, because rolodex binds udp and tcp separately and either one
# alone is a rolodex that is only half up.
rolodex_dns_is_bound() {
  [ -n "$(ss -H -lntu "src ${ROLODEX_DNS}:53" 2>/dev/null)" ]
}

# rolodex_dns_is_blocked — true when ANYTHING would collide with ROLODEX_DNS:53,
# including a listener that does not name the address at all.
#
# Deliberately wider than rolodex_dns_is_bound, and the two are not
# interchangeable. Readiness asks "is OUR rolodex up on its address", which is a
# question about one literal socket. Occupancy asks "can this address still be
# taken", and a wildcard `0.0.0.0:53` collides with `127.0.0.2:53` just as
# completely while matching none of an `ss "src 127.0.0.2:53"` filter — that
# filter compares the socket's own local address, so a wildcard socket is
# invisible to it.
#
# Not a corner case: a rolodex started with no config file binds the wildcard
# (Config::default is `0.0.0.0:53`), which is what a dev box from before the
# config seed does, and what one left behind by a hard kill goes on doing. Such a
# box answers for every loopback address on the machine, so the next dev run
# would bind nothing, resolve through it, and look like its own DNS was broken.
rolodex_dns_is_blocked() {
  local addr
  for addr in "${ROLODEX_DNS}:53" "0.0.0.0:53" "[::]:53"; do
    [ -n "$(ss -H -lntu "src ${addr}" 2>/dev/null)" ] && return 0
  done
  return 1
}

# require_free_rolodex_dns — refuse to start a dev box whose resolver address is
# already taken, BEFORE anything is launched.
#
# There is exactly one 127.0.0.2:53 on this machine and more than one thing that
# wants it: the install repo's host rolodex (`town-os-host-rolodex`), a dev
# container from another checkout, an orphaned system-service container from a
# dev run that was killed hard. Whichever got there first keeps it, so the
# rolodex this run starts fails to bind — and the old redirect could not tell the
# difference, because a TCP connect to 127.0.0.2:53 succeeds just as well when
# the answer is coming from somebody else's resolver. The host then resolved
# through a rolodex that knows nothing about this dev box, which reads exactly
# like the dev box's own DNS being broken.
#
# Called after this checkout's dev container is removed, so anything still
# holding the address is genuinely foreign rather than our own leftovers.
require_free_rolodex_dns() {
  rolodex_dns_is_blocked || return 0

  warn "${ROLODEX_DNS}:53 is already taken — something else is serving DNS there:"
  # Every shape that collides, not just the literal address: a wildcard holder is
  # the likeliest one here and naming only 127.0.0.2 would print nothing at all.
  for _addr in "${ROLODEX_DNS}:53" "0.0.0.0:53" "[::]:53"; do
    ss -H -lntu "src ${_addr}" 2>/dev/null | sed "s/^/  ** /" || true
  done
  warn "That is the address this dev box's rolodex needs, and pointing the host"
  warn "at it would resolve through a resolver that is not this checkout's."
  warn "Usual suspects, in order:"
  warn "  - the install repo's host rolodex: ${SUDO} podman rm -f town-os-host-rolodex"
  warn "  - another checkout's dev box:      make dev-stop (from that checkout)"
  warn "  - an orphan from a hard kill:      ${SUDO} podman rm -f town-os-system--rolodex"
  warn "A holder on 0.0.0.0:53 rather than ${ROLODEX_DNS}:53 is a rolodex that came"
  warn "up with no config file; it answers for every loopback address on this machine."
  exit 1
}

# seed_dev_rolodex_config DIR — render the bootstrap rolodex.yml a dev box needs,
# into the host directory `make dev` mounts at /town-os/rolodex.
#
# rolodex reads this file ONCE at startup and it is the only way two of its
# settings can be set at all: the DNS bind list and the metrics listener. Neither
# can be programmed over gRPC, which is why ProgramRolodex does not write here
# and why a rolodex with no config file opens no listener on ROLODEX_DNS at all.
#
# On a real box the install image renders it (../install/scripts/rolodex-config.sh
# is its only writer). `make dev` runs no installer, so before this nothing did:
# integration/testdata/town-os-system--rolodex.service creates the DIRECTORY with
# an ExecStartPre mkdir and then starts rolodex `--config /data/rolodex.yml` on a
# file that never existed. The dev box came up with the unit active and nothing
# bound, which cost two thirty-second waits inside the controller's own boot
# (rolodex.Manager.WaitForDNSReady, then the gRPC socket dial loop) and left the
# host resolving through the LAN for every .home name.
#
# Written HERE, on the host, and never baked into the image: the same unit file
# ships in the integration test image, and a rolodex.yml binding 127.0.0.2:53
# inside a --net host test container would bind the HOST's :53 — the one thing
# the test rules forbid outright.
#
# Deliberately minimal, and deliberately not a copy of the install script's
# output. That script enumerates the box's routable addresses and binds :853 on
# 0.0.0.0; a dev box has no business claiming LAN-facing DNS ports on the
# developer's machine, so everything here is loopback. Only what cannot be
# programmed later is set: the binds, the metrics listener, the gRPC socket the
# controller dials, and a starting forwarder pair. Forwarders, resolution mode
# and both blocklists are all overwritten by ProgramRolodex moments later.
#
# Rewritten on every run rather than created-if-absent. dev-rolodex persists
# across runs (it holds rolodex.db), so a stale config from an older schema would
# otherwise outlive the checkout that wrote it and fail in a way that looks like
# rolodex being broken. Only rolodex.yml is touched; everything beside it stays.
seed_dev_rolodex_config() {
  local dir="$1"

  substep "Seeding dev rolodex config"
  mkdir -p "${dir}"
  # Paths here are the ROLODEX CONTAINER's view: the unit mounts
  # /town-os/rolodex (this directory) as /data.
  cat > "${dir}/rolodex.yml" <<EOF
database_path: /data/rolodex.db
dns:
  bind:
    - udp: "${ROLODEX_DNS}:53"
    - tcp: "${ROLODEX_DNS}:53"
grpc:
  tcp_bind: ""
  unix_socket: /data/rolodex.sock
  shared_secret: ""
forwarders:
  - "8.8.8.8:53"
  - "8.8.4.4:53"
resolution:
  mode: auto
metrics:
  bind: "${ROLODEX_DNS}:9153"
doh:
  bind: "${ROLODEX_DNS}:4443"
  tls:
    auto_self_signed: true
EOF
}

# wait_for_rolodex_dns CONTAINER [TIMEOUT] — block until THIS checkout's rolodex
# is serving DNS, and say why in the container's own words when it never does.
#
# Two conditions, because neither alone is the thing being waited for. The unit
# being active is what makes it ours: it is the rolodex the systemcontroller in
# this dev container started, asked about by name rather than inferred. The
# address being bound is what makes it usable: the unit goes active the moment
# `podman run` is up, which is before rolodex has parsed its config and opened a
# socket, and redirecting into that window points the host at a closed port.
#
# Fatal rather than a warning. This used to warn and carry on, which produced a
# dev box that looked like it had started — UI up, API answering — while every
# .home name on the host went to the LAN resolver and came back NXDOMAIN. A dev
# box whose whole purpose is to mirror a real one is not usable without its
# resolver, so it fails here with the journal that explains it.
wait_for_rolodex_dns() {
  local container="$1" timeout="${2:-60}" i
  substep "Waiting for rolodex DNS on ${ROLODEX_DNS}:53"
  for i in $(seq 1 "${timeout}"); do
    if ${SUDO} podman exec "${container}" systemctl is-active --quiet "${ROLODEX_UNIT}" \
      && rolodex_dns_is_bound; then
      substep "Rolodex is serving DNS"
      return 0
    fi
    sleep 1
  done

  warn "rolodex is not serving DNS on ${ROLODEX_DNS}:53 after ${timeout}s"
  warn "systemctl status ${ROLODEX_UNIT}:"
  timeout 60 ${SUDO} podman exec "${container}" \
    systemctl status --no-pager --full "${ROLODEX_UNIT}" 2>&1 | tail -40 || true
  warn "journalctl -u ${ROLODEX_UNIT} (last 100 lines):"
  timeout 60 ${SUDO} podman exec "${container}" \
    journalctl --no-pager -n 100 -u "${ROLODEX_UNIT}" 2>&1 | tail -100 || true
  exit 1
}

# redirect_host_dns CONTAINER — route the host's name resolution through the dev
# rolodex, both halves of it: systemd-resolved's per-link servers and
# /etc/resolv.conf.
redirect_host_dns() {
  wait_for_rolodex_dns "$1"

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

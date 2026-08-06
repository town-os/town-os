# make/dns.sh - Host DNS redirection for the dev server.
# Source this file AFTER make/lib.sh: . make/dns.sh
#
# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
#
# Split out of make/dev.sh so it can be sourced on its own. dev.sh ends in a
# `case "$1"` dispatch that exits on an unrecognised argument, so sourcing it to
# reach these functions is impossible -- and every one of them mutates the
# host's resolver, which is the one thing the test rules say a test must never
# touch. Isolating them here lets the unit suite source this file with stubbed
# `ip`/`resolvectl`/`systemctl`/`sudo` on PATH and assert on what WOULD have
# been run, without a single real call reaching the host.
#
# Requires from lib.sh: SUDO, step, substep, warn. Requires STATE_DIR.
DNS_BACKUP="${STATE_DIR}/resolv.conf.bak"
RESOLVED_BACKUP="${STATE_DIR}/resolved-dns.bak"
ROLODEX_DNS="127.0.0.2"

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

# restore_resolv_conf — restore /etc/resolv.conf from backup if it exists.
restore_resolv_conf() {
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

# restore_host_dns — undo everything redirect_host_dns did. Safe to call when
# nothing was redirected, and safe to call twice.
restore_host_dns() {
  restore_resolved_dns
  restore_resolv_conf
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

  # Back up current /etc/resolv.conf (handle symlinks)
  if [ -L /etc/resolv.conf ]; then
    local target
    target="$(readlink -f /etc/resolv.conf)"
    printf '__SYMLINK__\n%s\n' "${target}" > "${DNS_BACKUP}"
    # Drop the symlink instead of writing through it. It usually points at
    # resolved's generated stub-resolv.conf, and clobbering that file leaves
    # the host pointed at a dead rolodex long after dev has exited — the
    # restore below puts the symlink back, but not the file's contents.
    ${SUDO} rm -f /etc/resolv.conf
  else
    cp /etc/resolv.conf "${DNS_BACKUP}"
  fi

  # Rewrite resolv.conf
  printf 'nameserver %s\n' "${ROLODEX_DNS}" | ${SUDO} tee /etc/resolv.conf >/dev/null

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

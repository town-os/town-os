#!/usr/bin/env bash

# Install all host dependencies needed to build, test, and run Town OS.
# Targets a fresh Arch, Fedora/RHEL (incl. Fedora Asahi Remix), or
# Ubuntu/Debian machine. Re-running on an already provisioned machine is safe
# (apt, pacman, and dnf skip already-installed packages, the Go and tool
# installers overwrite cleanly).

set -euo pipefail

GO_VERSION="${GO_VERSION:-1.25.6}"

if [ ! -r /etc/os-release ]; then
  echo "ERROR: /etc/os-release not found; cannot detect distro." >&2
  exit 1
fi
. /etc/os-release

DISTRO_ID="${ID:-}"
DISTRO_LIKE="${ID_LIKE:-}"

is_arch=false
is_debian=false
is_fedora=false
case "$DISTRO_ID" in
  arch|manjaro|endeavouros|artix|cachyos) is_arch=true ;;
  ubuntu|debian|linuxmint|pop|elementary) is_debian=true ;;
  fedora|fedora-asahi-remix|rhel|centos|rocky|almalinux) is_fedora=true ;;
  *)
    case " $DISTRO_LIKE " in
      *" arch "*) is_arch=true ;;
      *" debian "*|*" ubuntu "*) is_debian=true ;;
      *" fedora "*|*" rhel "*) is_fedora=true ;;
      *)
        echo "ERROR: unsupported distro '$DISTRO_ID' (ID_LIKE='$DISTRO_LIKE'). Only Arch, Fedora/RHEL, and Ubuntu/Debian are supported." >&2
        exit 1
        ;;
    esac
    ;;
esac

if [ "$(id -u)" -eq 0 ]; then
  SUDO=""
else
  if ! command -v sudo >/dev/null 2>&1; then
    echo "ERROR: sudo is required when not running as root." >&2
    exit 1
  fi
  SUDO="sudo"
fi

install_arch_packages() {
  echo ">>> Installing Arch packages via pacman..."
  $SUDO pacman -Sy --needed --noconfirm \
    base-devel \
    pkgconf \
    systemd \
    btrfs-progs \
    podman \
    runc \
    wireguard-tools \
    python \
    curl \
    git \
    unzip \
    qemu-base \
    qemu-img
  # golangci-lint is intentionally NOT taken from pacman: check-golangci-lint
  # and lint.sh look for it under $(go env GOPATH)/bin, but the pacman package
  # lands in /usr/bin. install_golangci_lint installs it to GOPATH/bin instead
  # (same reason the Fedora path skips the dnf package).
}

install_debian_packages() {
  echo ">>> Installing Debian/Ubuntu packages via apt-get..."
  export DEBIAN_FRONTEND=noninteractive
  $SUDO apt-get update
  $SUDO apt-get install -y --no-install-recommends \
    build-essential \
    pkg-config \
    ca-certificates \
    libsystemd-dev \
    btrfs-progs \
    podman \
    runc \
    wireguard-tools \
    python3 \
    curl \
    git \
    unzip \
    qemu-system-x86 \
    qemu-utils
}

install_fedora_packages() {
  echo ">>> Installing Fedora/RHEL packages via dnf..."
  # golangci-lint is intentionally NOT taken from dnf: check-golangci-lint and
  # lint.sh look for it under $(go env GOPATH)/bin, so it is installed there by
  # install_golangci_lint instead (the dnf package lands in /usr/bin).
  # qemu-system-x86-core provides /usr/bin/qemu-system-x86_64 (the binary the
  # VM runtime shells out to) on every arch, including aarch64 (Asahi) via TCG.
  local pkgs=(
    gcc
    make
    pkgconf-pkg-config
    ca-certificates
    systemd-devel
    btrfs-progs
    podman
    runc
    wireguard-tools
    python3
    git
    unzip
    qemu-system-x86-core
    qemu-img
  )
  # Fedora's base image ships curl-minimal; only pull full curl when there is
  # no curl at all, so we never trigger a curl-minimal -> curl swap prompt.
  if ! command -v curl >/dev/null 2>&1; then
    pkgs+=(curl)
  fi
  $SUDO dnf install -y "${pkgs[@]}"
}

install_go() {
  if command -v go >/dev/null 2>&1; then
    current="$(go env GOVERSION 2>/dev/null | sed 's/^go//')"
    if [ -n "$current" ] && printf '%s\n%s\n' "$GO_VERSION" "$current" | sort -V -C; then
      echo ">>> Go $current already installed (>= $GO_VERSION); skipping."
      return
    fi
  fi
  echo ">>> Installing Go $GO_VERSION from go.dev..."
  arch="$(uname -m)"
  case "$arch" in
    x86_64) goarch=amd64 ;;
    aarch64|arm64) goarch=arm64 ;;
    armv6l|armv7l) goarch=armv6l ;;
    *) echo "ERROR: unsupported architecture for Go install: $arch" >&2; exit 1 ;;
  esac
  tarball="go${GO_VERSION}.linux-${goarch}.tar.gz"
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT
  curl -fsSL "https://go.dev/dl/${tarball}" -o "$tmpdir/$tarball"
  $SUDO rm -rf /usr/local/go
  $SUDO tar -C /usr/local -xzf "$tmpdir/$tarball"
  rm -rf "$tmpdir"
  trap - EXIT
  if ! echo "$PATH" | tr ':' '\n' | grep -qx /usr/local/go/bin; then
    echo ">>> NOTE: add /usr/local/go/bin to your PATH (e.g. in ~/.profile or ~/.zshrc):"
    echo "         export PATH=\"/usr/local/go/bin:\$(go env GOPATH 2>/dev/null || echo \$HOME/go)/bin:\$PATH\""
  fi
}

install_golangci_lint() {
  GOPATH_BIN="$(PATH="/usr/local/go/bin:$PATH" go env GOPATH)/bin"
  if [ -x "$GOPATH_BIN/golangci-lint" ]; then
    echo ">>> golangci-lint already installed at $GOPATH_BIN/golangci-lint; skipping."
    return
  fi
  echo ">>> Installing golangci-lint to $GOPATH_BIN..."
  mkdir -p "$GOPATH_BIN"
  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh \
    | sh -s -- -b "$GOPATH_BIN"
}

# bun_version PATH — print the version if PATH is a working bun binary, else
# print nothing and return 1. A zero-filled or wrong-arch binary is still -x
# but fails to exec ("exec format error"), so testing executability is NOT
# enough — we must actually run it and require a non-empty version string.
bun_version() {
  local v
  v="$("$1" --version 2>/dev/null)" || return 1
  [ -n "$v" ] || return 1
  printf '%s' "$v"
}

install_bun() {
  local src ver dst=/usr/local/bin/bun tmp

  # The canonical source is the official installer's output at ~/.bun/bin/bun.
  # NEVER source from `command -v bun`: when $dst is already broken (e.g. a
  # zero-filled file left by a crash mid-copy) it is first on PATH, so
  # `command -v bun` returns the broken binary and we would copy it onto
  # itself and cement the breakage forever.
  src="$HOME/.bun/bin/bun"
  if ! ver="$(bun_version "$src")"; then
    echo ">>> Installing bun via official installer..."
    curl -fsSL https://bun.sh/install | bash
    if ! ver="$(bun_version "$src")"; then
      echo "ERROR: bun installer did not produce a working binary at $src" >&2
      exit 1
    fi
  else
    echo ">>> bun already installed ($ver) at $src."
  fi

  # Mirror onto the system PATH so non-login shells and make targets find it
  # without a per-user shell rc edit. Skip only when $dst already *runs* and
  # reports the same version — a $dst that exists but fails to exec
  # (zeroed/corrupt/wrong arch) must be reinstalled, not skipped. (The old
  # check compared two `--version` outputs that were both empty when the
  # binaries were broken, so empty==empty made it skip a broken install.)
  if [ "$(bun_version "$dst" 2>/dev/null || true)" = "$ver" ]; then
    echo ">>> bun already on system PATH at $dst; skipping."
    return
  fi

  echo ">>> Installing bun into $dst (system PATH)..."
  # Atomic install: write to a temp file in the same directory, flush it to
  # disk, then rename into place. `install` itself writes the destination in
  # place (open/truncate/write), so a crash mid-write leaves a half-written or
  # zero-filled binary directly on PATH. temp + sync + mv guarantees PATH only
  # ever sees a complete, fsync'd binary.
  tmp="$($SUDO mktemp /usr/local/bin/.bun.XXXXXX)"
  $SUDO install -m 0755 "$src" "$tmp"
  $SUDO sync "$tmp"
  $SUDO mv -f "$tmp" "$dst"
  $SUDO ln -sf bun /usr/local/bin/bunx

  # Verify the binary we just installed actually executes.
  if ! bun_version "$dst" >/dev/null; then
    echo "ERROR: installed bun at $dst does not execute (wrong arch or corrupt)." >&2
    exit 1
  fi
}

install_ui_deps() {
  if [ ! -f ui/package.json ]; then
    return
  fi
  # bun is a hard dependency installed by install_bun just above, so it must be
  # present here. eslint is NOT a host-global tool: it is a ui/package.json
  # devDependency that `make lint` invokes via `bun run lint` -> `eslint .`,
  # resolved from ui/node_modules/.bin. If this step is skipped, lint fails with
  # a confusing "eslint: command not found", so treat a missing bun as fatal
  # rather than warning and continuing.
  BUN_BIN="$(command -v bun || true)"
  if [ -z "$BUN_BIN" ] && [ -x "$HOME/.bun/bin/bun" ]; then
    BUN_BIN="$HOME/.bun/bin/bun"
  fi
  if [ -z "$BUN_BIN" ]; then
    echo "ERROR: bun not found; cannot install UI dependencies (eslint). install_bun must run first." >&2
    exit 1
  fi
  echo ">>> Installing UI dependencies via bun (eslint, vite, vitest, ...)..."
  (cd ui && "$BUN_BIN" install)
  # Verify the install actually produced the eslint binary that `make lint`
  # expects, so a partial/broken install fails here instead of at lint time.
  if [ ! -x ui/node_modules/.bin/eslint ]; then
    echo "ERROR: bun install did not produce ui/node_modules/.bin/eslint." >&2
    exit 1
  fi
  echo ">>> eslint installed at ui/node_modules/.bin/eslint."
}

enable_podman_socket() {
  if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    echo ">>> Enabling rootful podman socket (required by the systemcontroller)..."
    $SUDO systemctl enable --now podman.socket || true
  fi
}

if "$is_arch"; then
  install_arch_packages
elif "$is_fedora"; then
  install_fedora_packages
else
  install_debian_packages
fi

install_go
install_golangci_lint
install_bun
install_ui_deps
enable_podman_socket

echo
echo "All host dependencies installed."
echo "If this is a fresh shell, log out and back in (or source your shell rc) to pick up new PATH entries."

#!/usr/bin/env bash

# Install all host dependencies needed to build, test, and run Town OS.
# Targets a fresh Arch or Ubuntu/Debian machine. Re-running on an already
# provisioned machine is safe (apt and pacman skip already-installed packages,
# the Go and tool installers overwrite cleanly).

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
case "$DISTRO_ID" in
  arch|manjaro|endeavouros|artix|cachyos) is_arch=true ;;
  ubuntu|debian|linuxmint|pop|elementary) is_debian=true ;;
  *)
    case " $DISTRO_LIKE " in
      *" arch "*) is_arch=true ;;
      *" debian "*|*" ubuntu "*) is_debian=true ;;
      *)
        echo "ERROR: unsupported distro '$DISTRO_ID' (ID_LIKE='$DISTRO_LIKE'). Only Arch and Ubuntu/Debian are supported." >&2
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
    python \
    curl \
    git \
    unzip \
    qemu-base \
    qemu-img \
    golangci-lint
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
    python3 \
    curl \
    git \
    unzip \
    qemu-system-x86 \
    qemu-utils
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
  if "$is_arch"; then
    return # provided by pacman package above
  fi
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

install_bun() {
  if command -v bun >/dev/null 2>&1; then
    echo ">>> bun already installed ($(bun --version)); skipping."
    return
  fi
  echo ">>> Installing bun via official installer..."
  curl -fsSL https://bun.sh/install | bash
  echo ">>> NOTE: add ~/.bun/bin to your PATH (the bun installer writes this to your shell rc)."
}

install_ui_deps() {
  if [ ! -f ui/package.json ]; then
    return
  fi
  if ! command -v bun >/dev/null 2>&1 && [ ! -x "$HOME/.bun/bin/bun" ]; then
    echo ">>> WARNING: bun not found on PATH; skipping UI dependency install (eslint)." >&2
    return
  fi
  BUN_BIN="$(command -v bun || echo "$HOME/.bun/bin/bun")"
  echo ">>> Installing UI dependencies via bun (eslint, vite, vitest, ...)..."
  (cd ui && "$BUN_BIN" install)
}

enable_podman_socket() {
  if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    echo ">>> Enabling rootful podman socket (required by the systemcontroller)..."
    $SUDO systemctl enable --now podman.socket || true
  fi
}

if "$is_arch"; then
  install_arch_packages
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

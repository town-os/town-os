#!/usr/bin/env bash

# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
set -e

case "$1" in
  go)
    command -v go >/dev/null 2>&1 || { echo "ERROR: go not found in PATH. Install Go 1.25+: see README.md Prerequisites section"; exit 1; }
    ;;
  bun)
    command -v bun >/dev/null 2>&1 || { echo "ERROR: bun not found in PATH. Install Bun: https://bun.sh"; exit 1; }
    ;;
  podman)
    command -v podman >/dev/null 2>&1 || { echo "ERROR: podman not found in PATH. Install podman: see README.md Prerequisites section"; exit 1; }
    ;;
  runc)
    command -v runc >/dev/null 2>&1 || { echo "ERROR: runc not found in PATH. Podman requires the runc container runtime: see README.md Prerequisites section"; exit 1; }
    ;;
  btrfs)
    command -v mkfs.btrfs >/dev/null 2>&1 || { echo "ERROR: mkfs.btrfs not found in PATH. Install btrfs-progs: see README.md Prerequisites section"; exit 1; }
    ;;
  golangci-lint)
    test -x "$(go env GOPATH)/bin/golangci-lint" || { echo "ERROR: golangci-lint not found at $(go env GOPATH)/bin/golangci-lint. Install: see README.md Prerequisites section"; exit 1; }
    ;;
  python3)
    command -v python3 >/dev/null 2>&1 || { echo "ERROR: python3 not found in PATH. Install python3: see README.md Prerequisites section"; exit 1; }
    ;;
  libsystemd)
    command -v pkg-config >/dev/null 2>&1 || { echo "ERROR: pkg-config not found in PATH. Install build-essential (Ubuntu/Debian) or pkgconf (Arch): see README.md Prerequisites section"; exit 1; }
    pkg-config --exists libsystemd 2>/dev/null || { echo "ERROR: libsystemd development headers not found. Install libsystemd-dev (Ubuntu/Debian) or systemd-libs (Arch): see README.md Prerequisites section"; exit 1; }
    ;;
  binfmt)
    # Cross-build precondition, checked here so `make check-binfmt` answers the
    # question without starting a release build. Native builds never need it.
    case "$(uname -m)" in
      x86_64 | amd64) want=aarch64 ;;
      aarch64 | arm64) want=x86_64 ;;
      *) echo "ERROR: unsupported host architecture $(uname -m)"; exit 1 ;;
    esac
    test -e "/proc/sys/fs/binfmt_misc/qemu-${want}" || { echo "ERROR: no qemu-${want} binfmt handler registered; cross builds (TARGET=${want}) cannot run their runtime stages. Run 'make deps', or register one by hand: see README.md"; exit 1; }
    grep -qx enabled "/proc/sys/fs/binfmt_misc/qemu-${want}" || { echo "ERROR: binfmt handler qemu-${want} is registered but disabled"; exit 1; }
    grep -q '^flags:.*F' "/proc/sys/fs/binfmt_misc/qemu-${want}" || { echo "ERROR: binfmt handler qemu-${want} lacks the F (fix binary) flag, so the interpreter cannot be found inside a build container. Install the STATIC qemu-user build: run 'make deps'"; exit 1; }
    ;;
  *)
    echo "Usage: $0 {go|bun|podman|runc|btrfs|golangci-lint|python3|libsystemd|binfmt}"
    exit 1
    ;;
esac

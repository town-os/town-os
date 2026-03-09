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
  *)
    echo "Usage: $0 {go|bun|podman|runc|btrfs|golangci-lint|python3|libsystemd}"
    exit 1
    ;;
esac

#!/usr/bin/env bash
. make/lib.sh

case "$1" in
  create)
    set -e
    step "Creating btrfs volume"
    BTRFS_IMAGE="${BTRFS_IMAGE:-$(mktemp btrfs.XXXXXX)}"
    echo "${BTRFS_IMAGE}" > town-os.disk
    truncate -s 50G "$(cat town-os.disk)"
    substep "Formatting btrfs filesystem"
    mkfs.btrfs -f "$(cat town-os.disk)"
    ${SUDO} losetup -f "$(cat town-os.disk)"
    ${SUDO} losetup -j "$(cat town-os.disk)" | awk -F: '{ print $1 }' > town-os.loop
    mktemp -d > town-os.mount
    substep "Mounting at $(cat town-os.mount)"
    ${SUDO} mount -t btrfs "$(cat town-os.loop)" "$(cat town-os.mount)"
    ;;
  clean)
    step "Cleaning btrfs volume"
    # Cleanup is best-effort; do not set -e.
    if [ -f town-os.mount ]; then
      ${SUDO} umount "$(cat town-os.mount)" 2>/dev/null || true
      rmdir "$(cat town-os.mount)" 2>/dev/null || true
    fi
    if [ -f town-os.disk ]; then
      ${SUDO} losetup -j "$(cat town-os.disk)" | awk -F: '{ print $1 }' | xargs -I{} ${SUDO} losetup -d {} 2>/dev/null || true
    fi
    # Safety net: detach any loop devices still backed by btrfs images in this directory.
    ${SUDO} losetup -a 2>/dev/null | grep "$(pwd)/btrfs\." | awk -F: '{ print $1 }' | while read dev; do
      ${SUDO} umount "${dev}" 2>/dev/null || true
      ${SUDO} losetup -d "${dev}" 2>/dev/null || true
    done || true
    rm -f btrfs.* town-os.disk town-os.loop town-os.mount
    ;;
  create-dev)
    set -e
    step "Creating dev btrfs volume"
    DEV_BTRFS_IMAGE="${DEV_BTRFS_IMAGE:-$(mktemp btrfs-dev.XXXXXX)}"
    echo "${DEV_BTRFS_IMAGE}" > town-os-dev.disk
    truncate -s 50G "$(cat town-os-dev.disk)"
    substep "Formatting btrfs filesystem"
    mkfs.btrfs -f "$(cat town-os-dev.disk)"
    ${SUDO} losetup -f "$(cat town-os-dev.disk)"
    ${SUDO} losetup -j "$(cat town-os-dev.disk)" | awk -F: '{ print $1 }' > town-os-dev.loop
    mktemp -d > town-os-dev.mount
    substep "Mounting at $(cat town-os-dev.mount)"
    ${SUDO} mount -t btrfs "$(cat town-os-dev.loop)" "$(cat town-os-dev.mount)"
    ;;
  clean-dev)
    step "Cleaning dev btrfs volume"
    # Cleanup is best-effort; do not set -e.
    if [ -f town-os-dev.mount ]; then
      ${SUDO} umount "$(cat town-os-dev.mount)" 2>/dev/null || true
      rmdir "$(cat town-os-dev.mount)" 2>/dev/null || true
    fi
    if [ -f town-os-dev.disk ]; then
      ${SUDO} losetup -j "$(cat town-os-dev.disk)" | awk -F: '{ print $1 }' | xargs -I{} ${SUDO} losetup -d {} 2>/dev/null || true
    fi
    # Safety net: detach any loop devices still backed by btrfs-dev images in this directory.
    ${SUDO} losetup -a 2>/dev/null | grep "$(pwd)/btrfs-dev\." | awk -F: '{ print $1 }' | while read dev; do
      ${SUDO} umount "${dev}" 2>/dev/null || true
      ${SUDO} losetup -d "${dev}" 2>/dev/null || true
    done || true
    rm -f btrfs-dev.* town-os-dev.disk town-os-dev.loop town-os-dev.mount
    ;;
  ensure-dev)
    # Create the dev btrfs volume only if one isn't already mounted.
    if [ ! -f town-os-dev.mount ] || ! mountpoint -q "$(cat town-os-dev.mount)" 2>/dev/null; then
      ${MAKE} btrfs-dev
    fi
    ;;
  *)
    echo "Usage: $0 {create|clean|create-dev|clean-dev|ensure-dev}"
    exit 1
    ;;
esac

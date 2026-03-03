#!/usr/bin/env bash

case "$1" in
  create)
    set -e
    BTRFS_IMAGE="${BTRFS_IMAGE:-$(mktemp btrfs.XXXXXX)}"
    echo "${BTRFS_IMAGE}" > town-os.disk
    truncate -s 50G "$(cat town-os.disk)"
    mkfs.btrfs -f "$(cat town-os.disk)"
    sudo -E losetup -f "$(cat town-os.disk)"
    sudo -E losetup -j "$(cat town-os.disk)" | awk -F: '{ print $1 }' > town-os.loop
    mktemp -d > town-os.mount
    sudo -E mount -t btrfs "$(cat town-os.loop)" "$(cat town-os.mount)"
    ;;
  clean)
    # Cleanup is best-effort; do not set -e.
    if [ -f town-os.mount ]; then
      sudo -E umount "$(cat town-os.mount)" 2>/dev/null || true
      rmdir "$(cat town-os.mount)" 2>/dev/null || true
    fi
    if [ -f town-os.disk ]; then
      sudo -E losetup -j "$(cat town-os.disk)" | awk -F: '{ print $1 }' | xargs -I{} sudo -E losetup -d {} 2>/dev/null || true
    fi
    # Safety net: detach any loop devices still backed by btrfs images in this directory.
    sudo -E losetup -a 2>/dev/null | grep "$(pwd)/btrfs\." | awk -F: '{ print $1 }' | while read dev; do
      sudo -E umount "${dev}" 2>/dev/null || true
      sudo -E losetup -d "${dev}" 2>/dev/null || true
    done || true
    rm -f btrfs.* town-os.disk town-os.loop town-os.mount
    ;;
  create-dev)
    set -e
    DEV_BTRFS_IMAGE="${DEV_BTRFS_IMAGE:-$(mktemp btrfs-dev.XXXXXX)}"
    echo "${DEV_BTRFS_IMAGE}" > town-os-dev.disk
    truncate -s 50G "$(cat town-os-dev.disk)"
    mkfs.btrfs -f "$(cat town-os-dev.disk)"
    sudo -E losetup -f "$(cat town-os-dev.disk)"
    sudo -E losetup -j "$(cat town-os-dev.disk)" | awk -F: '{ print $1 }' > town-os-dev.loop
    mktemp -d > town-os-dev.mount
    sudo -E mount -t btrfs "$(cat town-os-dev.loop)" "$(cat town-os-dev.mount)"
    ;;
  clean-dev)
    # Cleanup is best-effort; do not set -e.
    if [ -f town-os-dev.mount ]; then
      sudo -E umount "$(cat town-os-dev.mount)" 2>/dev/null || true
      rmdir "$(cat town-os-dev.mount)" 2>/dev/null || true
    fi
    if [ -f town-os-dev.disk ]; then
      sudo -E losetup -j "$(cat town-os-dev.disk)" | awk -F: '{ print $1 }' | xargs -I{} sudo -E losetup -d {} 2>/dev/null || true
    fi
    # Safety net: detach any loop devices still backed by btrfs-dev images in this directory.
    sudo -E losetup -a 2>/dev/null | grep "$(pwd)/btrfs-dev\." | awk -F: '{ print $1 }' | while read dev; do
      sudo -E umount "${dev}" 2>/dev/null || true
      sudo -E losetup -d "${dev}" 2>/dev/null || true
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

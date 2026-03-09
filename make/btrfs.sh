#!/usr/bin/env bash

# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
. make/lib.sh

case "$1" in
  create)
    set -e
    step "Creating btrfs volume"
    BTRFS_IMAGE="${BTRFS_IMAGE:-$(mktemp "${STATE_DIR}/btrfs.XXXXXX")}"
    echo "${BTRFS_IMAGE}" > "${STATE_DIR}/town-os.disk"
    truncate -s 50G "$(cat "${STATE_DIR}/town-os.disk")"
    substep "Formatting btrfs filesystem"
    mkfs.btrfs -f "$(cat "${STATE_DIR}/town-os.disk")"
    ${SUDO} losetup -f "$(cat "${STATE_DIR}/town-os.disk")"
    ${SUDO} losetup -j "$(cat "${STATE_DIR}/town-os.disk")" | awk -F: '{ print $1 }' > "${STATE_DIR}/town-os.loop"
    mktemp -d > "${STATE_DIR}/town-os.mount"
    substep "Mounting at $(cat "${STATE_DIR}/town-os.mount")"
    ${SUDO} mount -t btrfs "$(cat "${STATE_DIR}/town-os.loop")" "$(cat "${STATE_DIR}/town-os.mount")"
    ;;
  clean)
    step "Cleaning btrfs volume"
    # Cleanup is best-effort; do not set -e.
    if [ -f "${STATE_DIR}/town-os.mount" ]; then
      ${SUDO} umount "$(cat "${STATE_DIR}/town-os.mount")" 2>/dev/null || true
      rmdir "$(cat "${STATE_DIR}/town-os.mount")" 2>/dev/null || true
    fi
    if [ -f "${STATE_DIR}/town-os.disk" ]; then
      ${SUDO} losetup -j "$(cat "${STATE_DIR}/town-os.disk")" | awk -F: '{ print $1 }' | xargs -I{} ${SUDO} losetup -d {} 2>/dev/null || true
    fi
    # Safety net: detach any loop devices still backed by btrfs images in this directory.
    ${SUDO} losetup -a 2>/dev/null | grep "${STATE_DIR}/btrfs\." | awk -F: '{ print $1 }' | while read dev; do
      ${SUDO} umount "${dev}" 2>/dev/null || true
      ${SUDO} losetup -d "${dev}" 2>/dev/null || true
    done || true
    rm -f "${STATE_DIR}"/btrfs.* "${STATE_DIR}/town-os.disk" "${STATE_DIR}/town-os.loop" "${STATE_DIR}/town-os.mount"
    ;;
  create-dev)
    set -e
    step "Creating dev btrfs volume"
    DEV_BTRFS_IMAGE="${DEV_BTRFS_IMAGE:-$(mktemp "${STATE_DIR}/btrfs-dev.XXXXXX")}"
    echo "${DEV_BTRFS_IMAGE}" > "${STATE_DIR}/town-os-dev.disk"
    truncate -s 50G "$(cat "${STATE_DIR}/town-os-dev.disk")"
    substep "Formatting btrfs filesystem"
    mkfs.btrfs -f "$(cat "${STATE_DIR}/town-os-dev.disk")"
    ${SUDO} losetup -f "$(cat "${STATE_DIR}/town-os-dev.disk")"
    ${SUDO} losetup -j "$(cat "${STATE_DIR}/town-os-dev.disk")" | awk -F: '{ print $1 }' > "${STATE_DIR}/town-os-dev.loop"
    mktemp -d > "${STATE_DIR}/town-os-dev.mount"
    substep "Mounting at $(cat "${STATE_DIR}/town-os-dev.mount")"
    ${SUDO} mount -t btrfs "$(cat "${STATE_DIR}/town-os-dev.loop")" "$(cat "${STATE_DIR}/town-os-dev.mount")"
    ;;
  clean-dev)
    step "Cleaning dev btrfs volume"
    # Cleanup is best-effort; do not set -e.
    if [ -f "${STATE_DIR}/town-os-dev.mount" ]; then
      ${SUDO} umount "$(cat "${STATE_DIR}/town-os-dev.mount")" 2>/dev/null || true
      rmdir "$(cat "${STATE_DIR}/town-os-dev.mount")" 2>/dev/null || true
    fi
    if [ -f "${STATE_DIR}/town-os-dev.disk" ]; then
      ${SUDO} losetup -j "$(cat "${STATE_DIR}/town-os-dev.disk")" | awk -F: '{ print $1 }' | xargs -I{} ${SUDO} losetup -d {} 2>/dev/null || true
    fi
    # Safety net: detach any loop devices still backed by btrfs-dev images in this directory.
    ${SUDO} losetup -a 2>/dev/null | grep "${STATE_DIR}/btrfs-dev\." | awk -F: '{ print $1 }' | while read dev; do
      ${SUDO} umount "${dev}" 2>/dev/null || true
      ${SUDO} losetup -d "${dev}" 2>/dev/null || true
    done || true
    rm -f "${STATE_DIR}"/btrfs-dev.* "${STATE_DIR}/town-os-dev.disk" "${STATE_DIR}/town-os-dev.loop" "${STATE_DIR}/town-os-dev.mount"
    ;;
  ensure-dev)
    # Create the dev btrfs volume only if one isn't already mounted.
    if [ ! -f "${STATE_DIR}/town-os-dev.mount" ] || ! mountpoint -q "$(cat "${STATE_DIR}/town-os-dev.mount")" 2>/dev/null; then
      ${MAKE} btrfs-dev
    fi
    ;;
  *)
    echo "Usage: $0 {create|clean|create-dev|clean-dev|ensure-dev}"
    exit 1
    ;;
esac

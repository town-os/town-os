#!/usr/bin/env bash

# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
set -e
. make/lib.sh

case "$1" in
  docker-login)
    registry_login docker.io DOCKER_USERNAME DOCKER_PASSWORD
    ;;
  quay-login)
    registry_login quay.io QUAY_USERNAME QUAY_PASSWORD
    ;;
  ensure-cache)
    step "Checking image cache"
    # If any cached image tar is missing, pull everything.
    for img in ${ALL_IMAGES}; do
      if [ ! -f "$(image_cache_tar "${img}")" ]; then
        warn "Image cache incomplete (missing $(image_safe_name "${img}").tar) — running pull-images"
        ${MAKE} pull-images
        break
      fi
    done
    ;;
  load-base)
    step "Loading base images"
    ensure_image_cache_dir
    for img in ${BASE_IMAGES}; do
      ensure_image "${img}"
    done
    touch "${STATE_DIR}/.images-pulled"
    ;;
  pull)
    step "Pulling all container images"
    ensure_image_cache_dir
    for img in ${ALL_IMAGES}; do
      substep "Pulling ${img}"
      ${SUDO} podman pull "${img}"
      substep "${img}: saving to cache"
      save_image_cache "${img}"
    done
    touch "${STATE_DIR}/.images-pulled"
    # Stamped here rather than only in pull-daily so the repair pull that
    # ensure-cache runs also counts as today's check: two full pulls back to
    # back is exactly what this is meant to stop.
    stamp_touch "${IMAGE_PULL_STAMP}"
    ;;
  pull-daily)
    # The throttle itself: pull only when the last check has aged out.
    #
    # A missing or unreadable stamp pulls, which is the safe direction — the
    # cost of an extra pull is time, and the cost of skipping one that was
    # needed is a test run against stale images.
    if stamp_fresh "${IMAGE_PULL_STAMP}"; then
      step "Image pull check: last ran $(stamp_age_human "${IMAGE_PULL_STAMP}") ago, skipping"
      substep "run 'make pull-images' to check upstream now"
      exit 0
    fi
    ${MAKE} pull-images
    ;;
  *)
    echo "Usage: $0 {docker-login|quay-login|ensure-cache|load-base|pull|pull-daily}"
    exit 1
    ;;
esac

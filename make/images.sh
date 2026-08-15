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
        warn "Image cache incomplete (missing $(basename "$(image_cache_tar "${img}")")) — running pull-images"
        ${MAKE} pull-images
        break
      fi
    done
    ;;
  load-base)
    step "Loading base images"
    ensure_image_cache_dir
    # Each base is staged at the architecture the CURRENT build wants it at, not
    # blanket at the host's. BASE_IMAGES_RUNTIME (see the Makefile) is the subset
    # a cross-buildable Containerfile names with a bare FROM — the stages that
    # ship — and those follow TARGET. The rest are toolchain images pinned to
    # $BUILDPLATFORM by every Containerfile that cross-builds, so the host arch
    # is right for them under any TARGET.
    #
    # This target is a prerequisite of nearly every build, which is what made
    # ignoring TARGET here so expensive: a cross build's own prerequisites
    # forced the runtime bases back to the host arch moments before the build
    # arm staged them at the target's, so every cross invocation paid the round
    # trip and the store read as the host arch whenever anyone looked.
    for img in ${BASE_IMAGES}; do
      if printf '%s\n' ${BASE_IMAGES_RUNTIME} | grep -qxF "${img}"; then
        ensure_image "${img}" "$(build_oci_arch)"
      else
        ensure_image "${img}"
      fi
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

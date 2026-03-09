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
    ${SUDO} mkdir -p "${IMAGE_CACHE}"
    for img in ${BASE_IMAGES}; do
      ensure_image "${img}"
    done
    touch "${STATE_DIR}/.images-pulled"
    ;;
  pull)
    step "Pulling all container images"
    ${SUDO} mkdir -p "${IMAGE_CACHE}"
    for img in ${ALL_IMAGES}; do
      substep "Pulling ${img}"
      ${SUDO} podman pull "${img}"
      substep "${img}: saving to cache"
      save_image_cache "${img}"
    done
    touch "${STATE_DIR}/.images-pulled"
    ;;
  *)
    echo "Usage: $0 {docker-login|quay-login|ensure-cache|load-base|pull}"
    exit 1
    ;;
esac

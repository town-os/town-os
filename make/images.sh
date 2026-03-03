#!/usr/bin/env bash
set -e

case "$1" in
  docker-login)
    if [ -n "${DOCKER_USERNAME}" ] && [ -n "${DOCKER_PASSWORD}" ]; then
      echo "${DOCKER_PASSWORD}" | sudo -E podman login -u "${DOCKER_USERNAME}" --password-stdin docker.io
    fi
    ;;
  ensure-cache)
    # If any cached image tar is missing, pull everything.
    for img in ${ALL_IMAGES}; do
      safe=$(basename "${img}" | tr ':' '-')
      if [ ! -f "${IMAGE_CACHE}/${safe}.tar" ]; then
        echo "Image cache incomplete (missing ${safe}.tar) — running pull-images..."
        ${MAKE} pull-images
        break
      fi
    done
    ;;
  load-base)
    # Load base images from cache or pull from Docker Hub.
    sudo -E mkdir -p "${IMAGE_CACHE}"
    for img in ${BASE_IMAGES}; do
      safe=$(basename "${img}" | tr ':' '-')
      if sudo -E podman image exists "${img}" 2>/dev/null; then
        echo "${img}: already in podman storage"
      elif [ -f "${IMAGE_CACHE}/${safe}.tar" ]; then
        echo "${img}: loading from cache..."
        sudo -E podman load -i "${IMAGE_CACHE}/${safe}.tar"
      else
        echo "${img}: pulling from Docker Hub..."
        sudo -E podman pull "${img}"
        echo "${img}: saving to cache..."
        sudo -E podman save -o "${IMAGE_CACHE}/${safe}.tar" "${img}"
      fi
    done
    mkdir -p .cache
    touch .cache/.images-pulled
    ;;
  pull)
    # Pull all container images from Docker Hub and save to global cache.
    sudo -E mkdir -p "${IMAGE_CACHE}"
    for img in ${ALL_IMAGES}; do
      echo "Pulling ${img}..."
      sudo -E podman pull "${img}"
      safe=$(basename "${img}" | tr ':' '-')
      echo "${img}: saving to cache..."
      sudo -E podman save -o "${IMAGE_CACHE}/${safe}.tar" "${img}"
    done
    mkdir -p .cache
    touch .cache/.images-pulled
    ;;
  *)
    echo "Usage: $0 {docker-login|ensure-cache|load-base|pull}"
    exit 1
    ;;
esac

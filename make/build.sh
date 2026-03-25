#!/usr/bin/env bash

# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
set -e
. make/lib.sh

case "$1" in
  production)
    step "Building production image"
    mkdir -p .cache/go-mod .cache/go-build
    ${SUDO} podman build --pull=never \
      --build-arg "TOWN_OS_TAG=${TOWN_OS_TAG}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${PODMAN_IMAGE}" -f Containerfile .
    ;;
  test)
    step "Building test image"
    mkdir -p .cache/go-mod .cache/go-build
    ${SUDO} podman build --pull=never \
      --build-arg "TOWN_OS_IMAGE=${PODMAN_IMAGE}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${PODMAN_TEST_IMAGE}" -f integration/testdata/Containerfile.systemd .
    ;;
  dev-base)
    step "Building dev base image"
    mkdir -p .cache/dev-go-mod .cache/dev-go-build
    ${SUDO} podman build --pull=never \
      --volume "$(pwd)/.cache/dev-go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/dev-go-build:/root/.cache/go-build:z" \
      -t "${PODMAN_DEV_BASE}" -f Containerfile .
    ;;
  dev)
    step "Building dev image"
    ${SUDO} podman build --pull=never \
      --build-arg "TOWN_OS_IMAGE=${PODMAN_DEV_BASE}" \
      -t "${PODMAN_DEV_IMAGE}" -f integration/testdata/Containerfile.dev .
    ;;
  ui-integration)
    step "Building UI integration image"
    ${SUDO} podman build --pull=never \
      -t "${PODMAN_UI_IMAGE}" -f integration/testdata/Containerfile.ui-integration .
    ;;
  release)
    step "Building release image"
    mkdir -p .cache/go-mod .cache/go-build
    ${SUDO} podman build --pull=never \
      --build-arg "TOWN_OS_TAG=${TOWN_OS_TAG}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${RELEASE_IMAGE}" -f Containerfile .
    ;;
  release-ui)
    step "Building UI release image"
    ${SUDO} podman build --pull=never \
      -t "${RELEASE_UI_IMAGE}" -f Containerfile.ui .
    ;;
  release-proton)
    step "Building Proton runner image"
    ${SUDO} podman build \
      -t "${RELEASE_PROTON_IMAGE}" -f Containerfile.proton .
    ;;
  push-rc)
    step "Pushing release candidate"
    DATE_TAG="$(date +%Y%m%d)"

    # Rebuild systemcontroller with tag baked in.
    # All quay.io/town/* images MUST use the same tag within a release.
    substep "Building ${RELEASE_IMAGE} with tag rc.${DATE_TAG}"
    mkdir -p .cache/go-mod .cache/go-build
    ${SUDO} podman build --pull=never \
      --build-arg "TOWN_OS_TAG=rc.${DATE_TAG}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${RELEASE_IMAGE}:rc.${DATE_TAG}" -f Containerfile .
    substep "Tagging ${RELEASE_IMAGE}:rc.latest"
    ${SUDO} podman tag "${RELEASE_IMAGE}:rc.${DATE_TAG}" "${RELEASE_IMAGE}:rc.latest"
    substep "Pushing ${RELEASE_IMAGE}:rc.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_IMAGE}:rc.${DATE_TAG}"
    substep "Pushing ${RELEASE_IMAGE}:rc.latest"
    ${SUDO} podman push "${RELEASE_IMAGE}:rc.latest"

    # UI image — tagged to match (systemcontroller derives the tag at runtime).
    substep "Tagging ${RELEASE_UI_IMAGE}:rc.${DATE_TAG}"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:rc.${DATE_TAG}"
    substep "Tagging ${RELEASE_UI_IMAGE}:rc.latest"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:rc.latest"
    substep "Pushing ${RELEASE_UI_IMAGE}:rc.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:rc.${DATE_TAG}"
    substep "Pushing ${RELEASE_UI_IMAGE}:rc.latest"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:rc.latest"

    # Rolodex image — tagged to match from rc.latest.
    substep "Tagging ${ROLODEX_IMAGE%%:*}:rc.${DATE_TAG}"
    ${SUDO} podman tag "${ROLODEX_IMAGE}" "${ROLODEX_IMAGE%%:*}:rc.${DATE_TAG}"
    substep "Pushing ${ROLODEX_IMAGE%%:*}:rc.${DATE_TAG}"
    ${SUDO} podman push "${ROLODEX_IMAGE%%:*}:rc.${DATE_TAG}"

    # Proton runner image.
    substep "Tagging ${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}"
    ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}"
    substep "Tagging ${RELEASE_PROTON_IMAGE}:rc.latest"
    ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:rc.latest"
    substep "Pushing ${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}"
    substep "Pushing ${RELEASE_PROTON_IMAGE}:rc.latest"
    ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:rc.latest"

    ;;
  push-release)
    step "Pushing release"
    DATE_TAG="$(date +%Y%m%d)"

    # Rebuild systemcontroller with tag baked in.
    # All quay.io/town/* images MUST use the same tag within a release.
    substep "Building ${RELEASE_IMAGE} with tag release.${DATE_TAG}"
    mkdir -p .cache/go-mod .cache/go-build
    ${SUDO} podman build --pull=never \
      --build-arg "TOWN_OS_TAG=release.${DATE_TAG}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${RELEASE_IMAGE}:release.${DATE_TAG}" -f Containerfile .
    substep "Tagging ${RELEASE_IMAGE}:latest"
    ${SUDO} podman tag "${RELEASE_IMAGE}:release.${DATE_TAG}" "${RELEASE_IMAGE}:latest"
    substep "Pushing ${RELEASE_IMAGE}:release.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_IMAGE}:release.${DATE_TAG}"
    substep "Pushing ${RELEASE_IMAGE}:latest"
    ${SUDO} podman push "${RELEASE_IMAGE}:latest"

    # UI image — tagged to match (systemcontroller derives the tag at runtime).
    substep "Tagging ${RELEASE_UI_IMAGE}:release.${DATE_TAG}"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:release.${DATE_TAG}"
    substep "Tagging ${RELEASE_UI_IMAGE}:latest"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:latest"
    substep "Pushing ${RELEASE_UI_IMAGE}:release.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:release.${DATE_TAG}"
    substep "Pushing ${RELEASE_UI_IMAGE}:latest"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:latest"

    # Rolodex image — tagged to match from rc.latest.
    substep "Tagging ${ROLODEX_IMAGE%%:*}:release.${DATE_TAG}"
    ${SUDO} podman tag "${ROLODEX_IMAGE}" "${ROLODEX_IMAGE%%:*}:release.${DATE_TAG}"
    substep "Tagging ${ROLODEX_IMAGE%%:*}:latest"
    ${SUDO} podman tag "${ROLODEX_IMAGE}" "${ROLODEX_IMAGE%%:*}:latest"
    substep "Pushing ${ROLODEX_IMAGE%%:*}:release.${DATE_TAG}"
    ${SUDO} podman push "${ROLODEX_IMAGE%%:*}:release.${DATE_TAG}"
    substep "Pushing ${ROLODEX_IMAGE%%:*}:latest"
    ${SUDO} podman push "${ROLODEX_IMAGE%%:*}:latest"

    # Proton runner image.
    substep "Tagging ${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}"
    ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}"
    substep "Tagging ${RELEASE_PROTON_IMAGE}:latest"
    ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:latest"
    substep "Pushing ${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}"
    substep "Pushing ${RELEASE_PROTON_IMAGE}:latest"
    ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:latest"

    ;;
  push-ui-rc)
    step "Pushing UI release candidate"
    DATE_TAG="$(date +%Y%m%d)"
    substep "Tagging ${RELEASE_UI_IMAGE}:rc.${DATE_TAG}"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:rc.${DATE_TAG}"
    substep "Tagging ${RELEASE_UI_IMAGE}:rc.latest"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:rc.latest"
    substep "Pushing ${RELEASE_UI_IMAGE}:rc.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:rc.${DATE_TAG}"
    substep "Pushing ${RELEASE_UI_IMAGE}:rc.latest"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:rc.latest"
    ;;
  push-ui-release)
    step "Pushing UI release"
    DATE_TAG="$(date +%Y%m%d)"
    substep "Tagging ${RELEASE_UI_IMAGE}:release.${DATE_TAG}"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:release.${DATE_TAG}"
    substep "Tagging ${RELEASE_UI_IMAGE}:latest"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:latest"
    substep "Pushing ${RELEASE_UI_IMAGE}:release.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:release.${DATE_TAG}"
    substep "Pushing ${RELEASE_UI_IMAGE}:latest"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:latest"
    ;;
  push-proton-rc)
    step "Pushing Proton runner release candidate"
    DATE_TAG="$(date +%Y%m%d)"
    substep "Tagging ${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}"
    ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}"
    substep "Tagging ${RELEASE_PROTON_IMAGE}:rc.latest"
    ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:rc.latest"
    substep "Pushing ${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}"
    substep "Pushing ${RELEASE_PROTON_IMAGE}:rc.latest"
    ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:rc.latest"
    ;;
  push-proton-release)
    step "Pushing Proton runner release"
    DATE_TAG="$(date +%Y%m%d)"
    substep "Tagging ${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}"
    ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}"
    substep "Tagging ${RELEASE_PROTON_IMAGE}:latest"
    ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:latest"
    substep "Pushing ${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}"
    substep "Pushing ${RELEASE_PROTON_IMAGE}:latest"
    ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:latest"
    ;;
  push-tag)
    TAG="$2"
    if [ -z "${TAG}" ]; then
      echo "Usage: $0 push-tag <tag>"
      exit 1
    fi
    step "Pushing all images with tag ${TAG}"

    # Systemcontroller — rebuild with tag baked in.
    substep "Building ${RELEASE_IMAGE}:${TAG}"
    mkdir -p .cache/go-mod .cache/go-build
    ${SUDO} podman build --pull=never \
      --build-arg "TOWN_OS_TAG=${TAG}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${RELEASE_IMAGE}:${TAG}" -f Containerfile .
    substep "Pushing ${RELEASE_IMAGE}:${TAG}"
    ${SUDO} podman push "${RELEASE_IMAGE}:${TAG}"

    # UI image.
    substep "Tagging ${RELEASE_UI_IMAGE}:${TAG}"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:${TAG}"
    substep "Pushing ${RELEASE_UI_IMAGE}:${TAG}"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:${TAG}"

    # Rolodex image.
    substep "Tagging ${ROLODEX_IMAGE%%:*}:${TAG}"
    ${SUDO} podman tag "${ROLODEX_IMAGE}" "${ROLODEX_IMAGE%%:*}:${TAG}"
    substep "Pushing ${ROLODEX_IMAGE%%:*}:${TAG}"
    ${SUDO} podman push "${ROLODEX_IMAGE%%:*}:${TAG}"

    # Proton runner image.
    substep "Tagging ${RELEASE_PROTON_IMAGE}:${TAG}"
    ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:${TAG}"
    substep "Pushing ${RELEASE_PROTON_IMAGE}:${TAG}"
    ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:${TAG}"
    ;;
  networkcontroller)
    step "Building network controller binary"
    CGO_ENABLED=0 go build -o town-os-networkcontroller ./src/networkcontroller/cmd/town-os-networkcontroller
    ;;
  *)
    echo "Usage: $0 {production|test|dev-base|dev|ui-integration|networkcontroller|release|release-ui|release-proton|push-rc|push-release|push-ui-rc|push-ui-release|push-tag <tag>}"
    exit 1
    ;;
esac

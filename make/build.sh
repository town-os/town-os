#!/usr/bin/env bash
set -e
. make/lib.sh

case "$1" in
  production)
    step "Building production image"
    mkdir -p .cache/go-mod .cache/go-build
    ${SUDO} podman build --pull=never \
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
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${RELEASE_IMAGE}" -f Containerfile .
    ;;
  release-ui)
    step "Building UI release image"
    ${SUDO} podman build --pull=never \
      -t "${RELEASE_UI_IMAGE}" -f Containerfile.ui .
    ;;
  push-rc)
    step "Pushing release candidate"
    DATE_TAG="$(date +%Y%m%d)"

    # Bake the tag into the systemcontroller image so it can derive
    # matching tags for sibling images (UI, rolodex) at runtime.
    # All quay.io/town/* images MUST use the same tag within a release.
    substep "Baking tag rc.${DATE_TAG} into ${RELEASE_IMAGE}"
    printf 'FROM %s\nRUN echo "rc.%s" > /town-os.tag\n' "${RELEASE_IMAGE}" "${DATE_TAG}" | \
      ${SUDO} podman build --pull=never -t "${RELEASE_IMAGE}:rc.${DATE_TAG}" -
    substep "Tagging ${RELEASE_IMAGE}:rc.latest"
    ${SUDO} podman tag "${RELEASE_IMAGE}:rc.${DATE_TAG}" "${RELEASE_IMAGE}:rc.latest"
    substep "Pushing ${RELEASE_IMAGE}:rc.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_IMAGE}:rc.${DATE_TAG}"
    substep "Pushing ${RELEASE_IMAGE}:rc.latest"
    ${SUDO} podman push "${RELEASE_IMAGE}:rc.latest"

    # UI image — tagged to match but no tag file needed (systemcontroller derives it).
    substep "Tagging ${RELEASE_UI_IMAGE}:rc.${DATE_TAG}"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:rc.${DATE_TAG}"
    substep "Tagging ${RELEASE_UI_IMAGE}:rc.latest"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:rc.latest"
    substep "Pushing ${RELEASE_UI_IMAGE}:rc.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:rc.${DATE_TAG}"
    substep "Pushing ${RELEASE_UI_IMAGE}:rc.latest"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:rc.latest"
    ;;
  push-release)
    step "Pushing release"
    DATE_TAG="$(date +%Y%m%d)"

    # Bake the tag into the systemcontroller image so it can derive
    # matching tags for sibling images (UI, rolodex) at runtime.
    # All quay.io/town/* images MUST use the same tag within a release.
    substep "Baking tag release.${DATE_TAG} into ${RELEASE_IMAGE}"
    printf 'FROM %s\nRUN echo "release.%s" > /town-os.tag\n' "${RELEASE_IMAGE}" "${DATE_TAG}" | \
      ${SUDO} podman build --pull=never -t "${RELEASE_IMAGE}:release.${DATE_TAG}" -
    substep "Tagging ${RELEASE_IMAGE}:latest"
    ${SUDO} podman tag "${RELEASE_IMAGE}:release.${DATE_TAG}" "${RELEASE_IMAGE}:latest"
    substep "Pushing ${RELEASE_IMAGE}:release.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_IMAGE}:release.${DATE_TAG}"
    substep "Pushing ${RELEASE_IMAGE}:latest"
    ${SUDO} podman push "${RELEASE_IMAGE}:latest"

    # UI image — tagged to match but no tag file needed (systemcontroller derives it).
    substep "Tagging ${RELEASE_UI_IMAGE}:release.${DATE_TAG}"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:release.${DATE_TAG}"
    substep "Tagging ${RELEASE_UI_IMAGE}:latest"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:latest"
    substep "Pushing ${RELEASE_UI_IMAGE}:release.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:release.${DATE_TAG}"
    substep "Pushing ${RELEASE_UI_IMAGE}:latest"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:latest"
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
  networkcontroller)
    step "Building network controller binary"
    CGO_ENABLED=0 go build -o town-os-networkcontroller ./src/networkcontroller/cmd/town-os-networkcontroller
    ;;
  *)
    echo "Usage: $0 {production|test|dev-base|dev|ui-integration|networkcontroller|release|release-ui|push-rc|push-release|push-ui-rc|push-ui-release}"
    exit 1
    ;;
esac

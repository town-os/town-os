#!/usr/bin/env bash

# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
set -e
. make/lib.sh

# PROTON_ENABLED / GO_BUILD_TAGS — controls the `proton` Go build tag and the
# Containerfile.proton release pipeline. Default off; set PROTON_ENABLED=1 in
# the environment (or via `make PROTON_ENABLED=1 ...`) to opt in.
: "${PROTON_ENABLED:=0}"
: "${GO_BUILD_TAGS:=}"

case "$1" in
  production)
    step "Building production image"
    mkdir -p .cache/go-mod .cache/go-build
    ${SUDO} podman build --pull=never \
      --build-arg "TOWN_OS_TAG=${TOWN_OS_TAG}" \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${PODMAN_IMAGE}" -f Containerfile .
    ;;
  test)
    step "Building test image"
    mkdir -p .cache/go-mod .cache/go-build
    ${SUDO} podman build --pull=never \
      --build-arg "TOWN_OS_IMAGE=${PODMAN_IMAGE}" \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
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
    ${SUDO} podman build --pull=never --no-cache \
      -t "${PODMAN_UI_IMAGE}" -f integration/testdata/Containerfile.ui-integration .
    ;;
  release)
    step "Building release image"
    mkdir -p .cache/go-mod .cache/go-build
    ${SUDO} podman build --pull=never \
      --build-arg "TOWN_OS_TAG=${TOWN_OS_TAG}" \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
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
    if [[ "${PROTON_ENABLED}" != "1" ]]; then
      echo "release-proton requires PROTON_ENABLED=1 (build tag and runner image are off by default)" >&2
      exit 1
    fi
    step "Building Proton runner image"
    ${SUDO} podman build \
      -t "${RELEASE_PROTON_IMAGE}" -f Containerfile.proton .
    ;;
  release-nc)
    step "Building network controller image"
    mkdir -p .cache/go-mod .cache/go-build
    # No --pull=never: alpine:latest is the runtime base and is not in
    # BASE_IMAGES, so the host image store may not have it yet on a
    # fresh checkout. Let podman pull it on demand.
    ${SUDO} podman build \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${RELEASE_NC_IMAGE}" -f Containerfile.networkcontroller .
    ;;
  push-rc)
    require_registry_login quay.io
    step "Pushing release candidate"
    DATE_TAG="$(date +%Y%m%d)"

    # Rebuild systemcontroller with tag baked in.
    # All quay.io/town/* images MUST use the same tag within a release.
    substep "Building ${RELEASE_IMAGE} with tag rc.${DATE_TAG}"
    mkdir -p .cache/go-mod .cache/go-build
    ${SUDO} podman build --pull=never \
      --build-arg "TOWN_OS_TAG=rc.${DATE_TAG}" \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
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
    # Proton runner image — only when PROTON_ENABLED=1.
    if [[ "${PROTON_ENABLED}" = "1" ]]; then
      substep "Tagging ${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}"
      ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}"
      substep "Tagging ${RELEASE_PROTON_IMAGE}:rc.latest"
      ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:rc.latest"
      substep "Pushing ${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}"
      ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}"
      substep "Pushing ${RELEASE_PROTON_IMAGE}:rc.latest"
      ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:rc.latest"
    fi

    # Network controller image.
    substep "Tagging ${RELEASE_NC_IMAGE}:rc.${DATE_TAG}"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:rc.${DATE_TAG}"
    substep "Tagging ${RELEASE_NC_IMAGE}:rc.latest"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:rc.latest"
    substep "Pushing ${RELEASE_NC_IMAGE}:rc.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:rc.${DATE_TAG}"
    substep "Pushing ${RELEASE_NC_IMAGE}:rc.latest"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:rc.latest"

    ;;
  push-release)
    require_registry_login quay.io
    step "Pushing release"
    DATE_TAG="$(date +%Y%m%d)"

    # Rebuild systemcontroller with tag baked in.
    # All quay.io/town/* images MUST use the same tag within a release.
    substep "Building ${RELEASE_IMAGE} with tag release.${DATE_TAG}"
    mkdir -p .cache/go-mod .cache/go-build
    ${SUDO} podman build --pull=never \
      --build-arg "TOWN_OS_TAG=release.${DATE_TAG}" \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
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

    # Proton runner image — only when PROTON_ENABLED=1.
    if [[ "${PROTON_ENABLED}" = "1" ]]; then
      substep "Tagging ${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}"
      ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}"
      substep "Tagging ${RELEASE_PROTON_IMAGE}:latest"
      ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:latest"
      substep "Pushing ${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}"
      ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}"
      substep "Pushing ${RELEASE_PROTON_IMAGE}:latest"
      ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:latest"
    fi

    # Network controller image.
    substep "Tagging ${RELEASE_NC_IMAGE}:release.${DATE_TAG}"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:release.${DATE_TAG}"
    substep "Tagging ${RELEASE_NC_IMAGE}:latest"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:latest"
    substep "Pushing ${RELEASE_NC_IMAGE}:release.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:release.${DATE_TAG}"
    substep "Pushing ${RELEASE_NC_IMAGE}:latest"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:latest"

    ;;
  push-ui-rc)
    require_registry_login quay.io
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
    require_registry_login quay.io
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
    if [[ "${PROTON_ENABLED}" != "1" ]]; then
      echo "push-proton-rc requires PROTON_ENABLED=1" >&2
      exit 1
    fi
    require_registry_login quay.io
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
    if [[ "${PROTON_ENABLED}" != "1" ]]; then
      echo "push-proton-release requires PROTON_ENABLED=1" >&2
      exit 1
    fi
    require_registry_login quay.io
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
  push-nc-rc)
    require_registry_login quay.io
    step "Pushing network controller release candidate"
    DATE_TAG="$(date +%Y%m%d)"
    substep "Tagging ${RELEASE_NC_IMAGE}:rc.${DATE_TAG}"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:rc.${DATE_TAG}"
    substep "Tagging ${RELEASE_NC_IMAGE}:rc.latest"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:rc.latest"
    substep "Pushing ${RELEASE_NC_IMAGE}:rc.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:rc.${DATE_TAG}"
    substep "Pushing ${RELEASE_NC_IMAGE}:rc.latest"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:rc.latest"
    ;;
  push-nc-release)
    require_registry_login quay.io
    step "Pushing network controller release"
    DATE_TAG="$(date +%Y%m%d)"
    substep "Tagging ${RELEASE_NC_IMAGE}:release.${DATE_TAG}"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:release.${DATE_TAG}"
    substep "Tagging ${RELEASE_NC_IMAGE}:latest"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:latest"
    substep "Pushing ${RELEASE_NC_IMAGE}:release.${DATE_TAG}"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:release.${DATE_TAG}"
    substep "Pushing ${RELEASE_NC_IMAGE}:latest"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:latest"
    ;;
  push-tag)
    TAG="$2"
    if [ -z "${TAG}" ]; then
      echo "Usage: $0 push-tag <tag>"
      exit 1
    fi
    require_registry_login quay.io
    step "Pushing all images with tag ${TAG}"

    # Systemcontroller — rebuild with tag baked in.
    substep "Building ${RELEASE_IMAGE}:${TAG}"
    mkdir -p .cache/go-mod .cache/go-build
    ${SUDO} podman build --pull=never \
      --build-arg "TOWN_OS_TAG=${TAG}" \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
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

    # Proton runner image — only when PROTON_ENABLED=1.
    if [[ "${PROTON_ENABLED}" = "1" ]]; then
      substep "Tagging ${RELEASE_PROTON_IMAGE}:${TAG}"
      ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:${TAG}"
      substep "Pushing ${RELEASE_PROTON_IMAGE}:${TAG}"
      ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:${TAG}"
    fi

    # Network controller image.
    substep "Tagging ${RELEASE_NC_IMAGE}:${TAG}"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:${TAG}"
    substep "Pushing ${RELEASE_NC_IMAGE}:${TAG}"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:${TAG}"
    ;;
  networkcontroller)
    step "Building network controller binary"
    CGO_ENABLED=0 go build -o town-os-networkcontroller ./src/networkcontroller/cmd/town-os-networkcontroller
    ;;
  *)
    echo "Usage: $0 {production|test|dev-base|dev|ui-integration|networkcontroller|release|release-ui|release-proton|release-nc|push-rc|push-release|push-ui-rc|push-ui-release|push-proton-rc|push-proton-release|push-nc-rc|push-nc-release|push-tag <tag>}"
    exit 1
    ;;
esac

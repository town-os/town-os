#!/usr/bin/env bash
set -e

case "$1" in
  production)
    mkdir -p .cache/go-mod .cache/go-build
    sudo -E podman build --pull=never \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${PODMAN_IMAGE}" -f Containerfile .
    ;;
  test)
    mkdir -p .cache/go-mod .cache/go-build
    sudo -E podman build --pull=never \
      --build-arg "TOWN_OS_IMAGE=${PODMAN_IMAGE}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${PODMAN_TEST_IMAGE}" -f integration/testdata/Containerfile.systemd .
    ;;
  dev-base)
    mkdir -p .cache/dev-go-mod .cache/dev-go-build
    sudo -E podman build --pull=never \
      --volume "$(pwd)/.cache/dev-go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/dev-go-build:/root/.cache/go-build:z" \
      -t "${PODMAN_DEV_BASE}" -f Containerfile .
    ;;
  dev)
    sudo -E podman build --pull=never \
      --build-arg "TOWN_OS_IMAGE=${PODMAN_DEV_BASE}" \
      -t "${PODMAN_DEV_IMAGE}" -f integration/testdata/Containerfile.dev .
    ;;
  ui-integration)
    sudo -E podman build --pull=never \
      -t "${PODMAN_UI_IMAGE}" -f integration/testdata/Containerfile.ui-integration .
    ;;
  networkcontroller)
    CGO_ENABLED=0 go build -o town-os-networkcontroller ./src/networkcontroller/cmd/town-os-networkcontroller
    ;;
  *)
    echo "Usage: $0 {production|test|dev-base|dev|ui-integration|networkcontroller}"
    exit 1
    ;;
esac

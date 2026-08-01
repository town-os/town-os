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

# Registry tag arch suffix (x86_64/aarch64, the uname -m form) for per-arch rc
# tags. rc tags are pushed natively from each host as rc.<date>-<arch> /
# rc.latest-<arch>; manifest-rc assembles the plain multi-arch manifest lists
# after every arch has pushed. This is the tag suffix, not the OCI platform
# name (host_arch) podman pulls with.
ARCH="$(host_arch_tag)"

case "$1" in
  production)
    step "Building production image"
    mkdir -p .cache/go-mod .cache/go-build .cache/bun
    ${SUDO} podman build --network=host --pull=never \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      --volume "$(pwd)/.cache/bun:/bun-cache:z" \
      -t "${PODMAN_IMAGE}" -f Containerfile .
    ;;
  test)
    step "Building test image"
    mkdir -p .cache/go-mod .cache/go-build
    ${SUDO} podman build --network=host --pull=never \
      --build-arg "TOWN_OS_IMAGE=${PODMAN_IMAGE}" \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${PODMAN_TEST_IMAGE}" -f integration/testdata/Containerfile.systemd .
    ;;
  dev-base)
    step "Building dev base image"
    mkdir -p .cache/dev-go-mod .cache/dev-go-build .cache/bun
    ${SUDO} podman build --network=host --pull=never \
      --volume "$(pwd)/.cache/dev-go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/dev-go-build:/root/.cache/go-build:z" \
      --volume "$(pwd)/.cache/bun:/bun-cache:z" \
      -t "${PODMAN_DEV_BASE}" -f Containerfile .
    ;;
  dev)
    step "Building dev image"
    ${SUDO} podman build --network=host --pull=never \
      --build-arg "TOWN_OS_IMAGE=${PODMAN_DEV_BASE}" \
      --build-arg "ROLODEX_IMAGE=${ROLODEX_IMAGE}" \
      -t "${PODMAN_DEV_IMAGE}" -f integration/testdata/Containerfile.dev .
    ;;
  ui-integration)
    step "Building UI integration image"
    # --no-cache reruns bun install on every build; the mounted bun cache
    # keeps it off the network once warm.
    mkdir -p .cache/bun
    ${SUDO} podman build --network=host --pull=never --no-cache \
      --volume "$(pwd)/.cache/bun:/bun-cache:z" \
      -t "${PODMAN_UI_IMAGE}" -f integration/testdata/Containerfile.ui-integration .
    ;;
  # Local UI image for tests. Built from the in-repo UI source so it always
  # matches the host arch; quay.io/town/ui tags are for production/release
  # only and must never be used for testing. Saved to the image cache so
  # load_images_into_container can copy it into test containers.
  ui-local)
    step "Building local UI test image"
    mkdir -p .cache/bun
    ${SUDO} podman build --network=host --pull=never \
      --volume "$(pwd)/.cache/bun:/bun-cache:z" \
      -t "${UI_IMAGE}" -f Containerfile.ui .
    save_image_cache "${UI_IMAGE}"
    ;;
  # Local NC image for tests and dev. Built on the host (container-network
  # DNS hardcoded to public resolvers breaks on captive networks that block
  # them) and loaded into the test/dev containers from the image cache. The
  # NC binary is extracted from the given source image (default: the
  # production image) so it always matches the systemcontroller under test.
  nc-local)
    SRC_IMAGE="${2:-${PODMAN_IMAGE}}"
    step "Building local NC test image from ${SRC_IMAGE}"
    ensure_image docker.io/library/alpine:latest
    builddir="$(mktemp -d "${TMPDIR:-/tmp}/town-os-nc-build.XXXXXX")"
    cid="$(${SUDO} podman create "${SRC_IMAGE}")"
    ${SUDO} podman cp "${cid}:/town-os-networkcontroller" "${builddir}/town-os-networkcontroller"
    ${SUDO} podman rm "${cid}" >/dev/null
    printf 'FROM docker.io/library/alpine:latest\nRUN apk add --no-cache socat\nCOPY town-os-networkcontroller /town-os-networkcontroller\nCMD ["/town-os-networkcontroller"]\n' \
      > "${builddir}/Containerfile"
    ${SUDO} podman build --network=host --pull=never \
      -t "${NC_IMAGE}" -f "${builddir}/Containerfile" "${builddir}"
    ${SUDO} rm -rf "${builddir}"
    save_image_cache "${NC_IMAGE}"
    ;;
  # Local ingress image for tests and dev. Self-contained via Containerfile.ingress
  # (it bundles caddy + the ingress binary), built on the host and loaded into the
  # test/dev containers from the image cache — same rationale as nc-local.
  ingress-local)
    step "Building local ingress test image"
    mkdir -p .cache/go-mod .cache/go-build
    ${SUDO} podman build --network=host \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${INGRESS_IMAGE}" -f Containerfile.ingress .
    save_image_cache "${INGRESS_IMAGE}"
    ;;
  gfeh-local)
    step "Building local gfeh test image"
    # A cargo registry cache, for the same reason the Go builds keep a module
    # cache: gfehd pulls a large dependency tree and rebuilding it from scratch
    # on every invocation is minutes per run.
    mkdir -p .cache/cargo-registry
    # No --pull=never: rust:1-bookworm is the builder base and is deliberately
    # NOT in BASE_IMAGES (it is ~1.5G and only this target needs it), so the
    # host store may not have it on a fresh checkout.
    ${SUDO} podman build --network=host \
      --build-arg "GFEH_VERSION=${GFEH_VERSION:-}" \
      --build-arg "GFEH_LATEST=${GFEH_LATEST:-}" \
      --volume "$(pwd)/.cache/cargo-registry:/usr/local/cargo/registry:z" \
      -t "${GFEH_IMAGE}" -f Containerfile.gfeh .
    save_image_cache "${GFEH_IMAGE}"
    ;;
  release)
    step "Building release image"
    mkdir -p .cache/go-mod .cache/go-build .cache/bun
    ${SUDO} podman build --network=host --pull=never \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      --volume "$(pwd)/.cache/bun:/bun-cache:z" \
      -t "${RELEASE_IMAGE}" -f Containerfile .
    ;;
  release-ui)
    step "Building UI release image"
    # --no-cache forces `bun run build` to actually rerun every release, same as
    # the ui-integration build. Without it, podman reuses a cached COPY/build
    # layer and push-rc happily re-tags and re-pushes STALE /srv assets: the
    # quay tag moves (new push timestamp) but the image's own created date — and
    # the UI the box serves — stays frozen at whatever bun last built. Unlike the
    # controller (whose daily TOWN_OS_TAG build-arg busts its cache) the UI has
    # no changing input, so a cache hit silently ships an old UI. The mounted
    # .cache/bun keeps bun install fast despite --no-cache.
    mkdir -p .cache/bun
    ${SUDO} podman build --network=host --pull=never --no-cache \
      --volume "$(pwd)/.cache/bun:/bun-cache:z" \
      -t "${RELEASE_UI_IMAGE}" -f Containerfile.ui .
    ;;
  release-proton)
    if [[ "${PROTON_ENABLED}" != "1" ]]; then
      echo "release-proton requires PROTON_ENABLED=1 (build tag and runner image are off by default)" >&2
      exit 1
    fi
    step "Building Proton runner image"
    ${SUDO} podman build --network=host \
      -t "${RELEASE_PROTON_IMAGE}" -f Containerfile.proton .
    ;;
  release-nc)
    step "Building network controller image"
    mkdir -p .cache/go-mod .cache/go-build
    # No --pull=never: alpine:latest is the runtime base and is not in
    # BASE_IMAGES, so the host image store may not have it yet on a
    # fresh checkout. Let podman pull it on demand.
    ${SUDO} podman build --network=host \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${RELEASE_NC_IMAGE}" -f Containerfile.networkcontroller .
    ;;
  release-ingress)
    step "Building ingress image"
    mkdir -p .cache/go-mod .cache/go-build
    # No --pull=never: alpine:latest is the runtime base and is not in
    # BASE_IMAGES, so the host image store may not have it yet on a fresh
    # checkout. Let podman pull it on demand.
    ${SUDO} podman build --network=host \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${RELEASE_INGRESS_IMAGE}" -f Containerfile.ingress .
    ;;
  release-gfeh)
    step "Building gfeh image"
    mkdir -p .cache/cargo-registry
    ${SUDO} podman build --network=host \
      --build-arg "GFEH_VERSION=${GFEH_VERSION:-}" \
      --build-arg "GFEH_LATEST=${GFEH_LATEST:-}" \
      --volume "$(pwd)/.cache/cargo-registry:/usr/local/cargo/registry:z" \
      -t "${RELEASE_GFEH_IMAGE}" -f Containerfile.gfeh .
    ;;
  push-rc)
    require_registry_login quay.io
    step "Pushing release candidate (${ARCH})"
    DATE_TAG="$(date +%Y%m%d)"

    # rc tags are per-arch: rc.<date>-<arch> / rc.latest-<arch>, pushed
    # natively from each host. The plain rc.<date> / rc.latest names are
    # multi-arch manifest lists assembled by manifest-rc once every arch
    # has pushed; they are never pushed as single-arch tags.
    # Rebuild systemcontroller with the per-arch tag baked in so the runtime
    # derives matching per-arch sibling image tags (UI, rolodex, NC).
    # All quay.io/town/* images MUST use the same tag within a release.
    substep "Building ${RELEASE_IMAGE} with tag rc.${DATE_TAG}-${ARCH}"
    mkdir -p .cache/go-mod .cache/go-build .cache/bun
    ${SUDO} podman build --network=host --pull=never \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      --volume "$(pwd)/.cache/bun:/bun-cache:z" \
      -t "${RELEASE_IMAGE}:rc.${DATE_TAG}-${ARCH}" -f Containerfile .
    substep "Tagging ${RELEASE_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_IMAGE}:rc.${DATE_TAG}-${ARCH}" "${RELEASE_IMAGE}:rc.latest-${ARCH}"
    substep "Pushing ${RELEASE_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_IMAGE}:rc.latest-${ARCH}"

    # UI image — tagged to match (systemcontroller derives the tag at runtime).
    substep "Tagging ${RELEASE_UI_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_UI_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:rc.latest-${ARCH}"
    substep "Pushing ${RELEASE_UI_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_UI_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:rc.latest-${ARCH}"
    # Proton runner image — only when PROTON_ENABLED=1.
    if [[ "${PROTON_ENABLED}" = "1" ]]; then
      substep "Tagging ${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}-${ARCH}"
      ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}-${ARCH}"
      substep "Tagging ${RELEASE_PROTON_IMAGE}:rc.latest-${ARCH}"
      ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:rc.latest-${ARCH}"
      substep "Pushing ${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}-${ARCH}"
      ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}-${ARCH}"
      substep "Pushing ${RELEASE_PROTON_IMAGE}:rc.latest-${ARCH}"
      ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:rc.latest-${ARCH}"
    fi

    # Network controller image.
    substep "Tagging ${RELEASE_NC_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_NC_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:rc.latest-${ARCH}"
    substep "Pushing ${RELEASE_NC_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_NC_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:rc.latest-${ARCH}"

    # Ingress image.
    substep "Tagging ${RELEASE_INGRESS_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_INGRESS_IMAGE}" "${RELEASE_INGRESS_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_INGRESS_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_INGRESS_IMAGE}" "${RELEASE_INGRESS_IMAGE}:rc.latest-${ARCH}"
    substep "Pushing ${RELEASE_INGRESS_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_INGRESS_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_INGRESS_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_INGRESS_IMAGE}:rc.latest-${ARCH}"

    ;;
  manifest-rc)
    require_registry_login quay.io
    step "Assembling release candidate manifests"
    DATE_TAG="$(date +%Y%m%d)"
    for image in "${RELEASE_IMAGE}" "${RELEASE_UI_IMAGE}" "${RELEASE_NC_IMAGE}" "${RELEASE_INGRESS_IMAGE}" "${RELEASE_GFEH_IMAGE}"; do
      build_manifest "${image}" "rc.${DATE_TAG}"
      build_manifest "${image}" "rc.latest"
    done
    if [[ "${PROTON_ENABLED}" = "1" ]]; then
      build_manifest "${RELEASE_PROTON_IMAGE}" "rc.${DATE_TAG}"
      build_manifest "${RELEASE_PROTON_IMAGE}" "rc.latest"
    fi
    ;;
  push-release)
    require_registry_login quay.io
    step "Pushing release (${ARCH})"
    DATE_TAG="$(date +%Y%m%d)"

    # Release tags are per-arch like rc tags: release.<date>-<arch> /
    # latest-<arch>, pushed natively from each host. The plain
    # release.<date> / latest names are multi-arch manifest lists assembled
    # by manifest-release once every arch has pushed.
    # Rebuild systemcontroller with the per-arch tag baked in so the runtime
    # derives matching per-arch sibling image tags (UI, rolodex, NC).
    # All quay.io/town/* images MUST use the same tag within a release.
    substep "Building ${RELEASE_IMAGE} with tag release.${DATE_TAG}-${ARCH}"
    mkdir -p .cache/go-mod .cache/go-build .cache/bun
    ${SUDO} podman build --network=host --pull=never \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      --volume "$(pwd)/.cache/bun:/bun-cache:z" \
      -t "${RELEASE_IMAGE}:release.${DATE_TAG}-${ARCH}" -f Containerfile .
    substep "Tagging ${RELEASE_IMAGE}:latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_IMAGE}:release.${DATE_TAG}-${ARCH}" "${RELEASE_IMAGE}:latest-${ARCH}"
    substep "Pushing ${RELEASE_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_IMAGE}:latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_IMAGE}:latest-${ARCH}"

    # UI image — tagged to match (systemcontroller derives the tag at runtime).
    substep "Tagging ${RELEASE_UI_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_UI_IMAGE}:latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:latest-${ARCH}"
    substep "Pushing ${RELEASE_UI_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_UI_IMAGE}:latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:latest-${ARCH}"

    # Proton runner image — only when PROTON_ENABLED=1.
    if [[ "${PROTON_ENABLED}" = "1" ]]; then
      substep "Tagging ${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}-${ARCH}"
      ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}-${ARCH}"
      substep "Tagging ${RELEASE_PROTON_IMAGE}:latest-${ARCH}"
      ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:latest-${ARCH}"
      substep "Pushing ${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}-${ARCH}"
      ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}-${ARCH}"
      substep "Pushing ${RELEASE_PROTON_IMAGE}:latest-${ARCH}"
      ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:latest-${ARCH}"
    fi

    # Network controller image.
    substep "Tagging ${RELEASE_NC_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_NC_IMAGE}:latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:latest-${ARCH}"
    substep "Pushing ${RELEASE_NC_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_NC_IMAGE}:latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:latest-${ARCH}"

    ;;
  manifest-release)
    require_registry_login quay.io
    step "Assembling release manifests"
    DATE_TAG="$(date +%Y%m%d)"
    # Every image push-release pushes per-arch tags for needs its plain names
    # assembled here, or `latest` and `release.<date>` stay whatever single-arch
    # tag was pushed last -- which is an `exec format error` on the other
    # architecture. Ingress was missing from this list while being present in
    # manifest-rc above; that was a bug, not a deliberate exclusion.
    for image in "${RELEASE_IMAGE}" "${RELEASE_UI_IMAGE}" "${RELEASE_NC_IMAGE}" "${RELEASE_INGRESS_IMAGE}" "${RELEASE_GFEH_IMAGE}"; do
      build_manifest "${image}" "release.${DATE_TAG}"
      build_manifest "${image}" "latest"
    done
    if [[ "${PROTON_ENABLED}" = "1" ]]; then
      build_manifest "${RELEASE_PROTON_IMAGE}" "release.${DATE_TAG}"
      build_manifest "${RELEASE_PROTON_IMAGE}" "latest"
    fi
    ;;
  push-ui-rc)
    require_registry_login quay.io
    step "Pushing UI release candidate (${ARCH})"
    DATE_TAG="$(date +%Y%m%d)"
    substep "Tagging ${RELEASE_UI_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_UI_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:rc.latest-${ARCH}"
    substep "Pushing ${RELEASE_UI_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_UI_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:rc.latest-${ARCH}"
    ;;
  push-ui-release)
    require_registry_login quay.io
    step "Pushing UI release (${ARCH})"
    DATE_TAG="$(date +%Y%m%d)"
    substep "Tagging ${RELEASE_UI_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_UI_IMAGE}:latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_UI_IMAGE}" "${RELEASE_UI_IMAGE}:latest-${ARCH}"
    substep "Pushing ${RELEASE_UI_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_UI_IMAGE}:latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_UI_IMAGE}:latest-${ARCH}"
    ;;
  push-proton-rc)
    if [[ "${PROTON_ENABLED}" != "1" ]]; then
      echo "push-proton-rc requires PROTON_ENABLED=1" >&2
      exit 1
    fi
    require_registry_login quay.io
    step "Pushing Proton runner release candidate (${ARCH})"
    DATE_TAG="$(date +%Y%m%d)"
    substep "Tagging ${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_PROTON_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:rc.latest-${ARCH}"
    substep "Pushing ${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_PROTON_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:rc.latest-${ARCH}"
    ;;
  push-proton-release)
    if [[ "${PROTON_ENABLED}" != "1" ]]; then
      echo "push-proton-release requires PROTON_ENABLED=1" >&2
      exit 1
    fi
    require_registry_login quay.io
    step "Pushing Proton runner release (${ARCH})"
    DATE_TAG="$(date +%Y%m%d)"
    substep "Tagging ${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_PROTON_IMAGE}:latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_PROTON_IMAGE}" "${RELEASE_PROTON_IMAGE}:latest-${ARCH}"
    substep "Pushing ${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_PROTON_IMAGE}:latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_PROTON_IMAGE}:latest-${ARCH}"
    ;;
  push-nc-rc)
    require_registry_login quay.io
    step "Pushing network controller release candidate (${ARCH})"
    DATE_TAG="$(date +%Y%m%d)"
    substep "Tagging ${RELEASE_NC_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_NC_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:rc.latest-${ARCH}"
    substep "Pushing ${RELEASE_NC_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_NC_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:rc.latest-${ARCH}"
    ;;
  push-nc-release)
    require_registry_login quay.io
    step "Pushing network controller release (${ARCH})"
    DATE_TAG="$(date +%Y%m%d)"
    substep "Tagging ${RELEASE_NC_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_NC_IMAGE}:latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_NC_IMAGE}" "${RELEASE_NC_IMAGE}:latest-${ARCH}"
    substep "Pushing ${RELEASE_NC_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_NC_IMAGE}:latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_NC_IMAGE}:latest-${ARCH}"
    ;;
  push-gfeh-rc)
    require_registry_login quay.io
    step "Pushing gfeh release candidate (${ARCH})"
    DATE_TAG="$(date +%Y%m%d)"
    substep "Tagging ${RELEASE_GFEH_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_GFEH_IMAGE}" "${RELEASE_GFEH_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_GFEH_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_GFEH_IMAGE}" "${RELEASE_GFEH_IMAGE}:rc.latest-${ARCH}"
    substep "Pushing ${RELEASE_GFEH_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_GFEH_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_GFEH_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_GFEH_IMAGE}:rc.latest-${ARCH}"
    ;;
  push-ingress-rc)
    require_registry_login quay.io
    step "Pushing ingress release candidate (${ARCH})"
    DATE_TAG="$(date +%Y%m%d)"
    substep "Tagging ${RELEASE_INGRESS_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_INGRESS_IMAGE}" "${RELEASE_INGRESS_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_INGRESS_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_INGRESS_IMAGE}" "${RELEASE_INGRESS_IMAGE}:rc.latest-${ARCH}"
    substep "Pushing ${RELEASE_INGRESS_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_INGRESS_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_INGRESS_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_INGRESS_IMAGE}:rc.latest-${ARCH}"
    ;;
  push-ingress-release)
    require_registry_login quay.io
    step "Pushing ingress release (${ARCH})"
    DATE_TAG="$(date +%Y%m%d)"
    substep "Tagging ${RELEASE_INGRESS_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_INGRESS_IMAGE}" "${RELEASE_INGRESS_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_INGRESS_IMAGE}:latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_INGRESS_IMAGE}" "${RELEASE_INGRESS_IMAGE}:latest-${ARCH}"
    substep "Pushing ${RELEASE_INGRESS_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_INGRESS_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_INGRESS_IMAGE}:latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_INGRESS_IMAGE}:latest-${ARCH}"
    ;;
  push-gfeh-release)
    require_registry_login quay.io
    step "Pushing gfeh release (${ARCH})"
    DATE_TAG="$(date +%Y%m%d)"
    substep "Tagging ${RELEASE_GFEH_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_GFEH_IMAGE}" "${RELEASE_GFEH_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_GFEH_IMAGE}:latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_GFEH_IMAGE}" "${RELEASE_GFEH_IMAGE}:latest-${ARCH}"
    substep "Pushing ${RELEASE_GFEH_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_GFEH_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_GFEH_IMAGE}:latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_GFEH_IMAGE}:latest-${ARCH}"
    ;;
  push-tag)
    TAG="$2"
    if [ -z "${TAG}" ]; then
      echo "Usage: $0 push-tag <tag>"
      exit 1
    fi
    require_registry_login quay.io
    step "Pushing all images with tag ${TAG}"

    # Systemcontroller image. The tag is no longer baked into the binary; the
    # controller resolves it at runtime from TOWN_OS_TAG (set on its systemd
    # unit by the install build system), defaulting to rc.latest-<arch>.
    substep "Building ${RELEASE_IMAGE}:${TAG}"
    mkdir -p .cache/go-mod .cache/go-build .cache/bun
    ${SUDO} podman build --network=host --pull=never \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      --volume "$(pwd)/.cache/bun:/bun-cache:z" \
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
    echo "Usage: $0 {production|test|dev-base|dev|ui-integration|ui-local|nc-local [src-image]|networkcontroller|release|release-ui|release-proton|release-nc|push-rc|manifest-rc|push-release|manifest-release|push-ui-rc|push-ui-release|push-proton-rc|push-proton-release|push-nc-rc|push-nc-release|push-tag <tag>}"
    exit 1
    ;;
esac

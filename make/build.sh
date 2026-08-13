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
#
# BUILD_ARCH is exported by the Makefile, which derives it from TARGET.
# host_arch_tag is the fallback for running this script directly.
ARCH="$(build_arch_tag)"

# Cross-build plumbing, empty on a native build so the command lines below are
# byte-identical to what they have always been.
#
# BUILD_PLATFORM_ARGS sets the platform every *runtime* stage resolves to; the
# toolchain stages override it back with FROM --platform=$BUILDPLATFORM and
# cross-compile, so nothing large is ever emulated. PULL_ARGS drops the
# --pull=never that the native release path relies on: the image cache holds
# host-arch bases only (its tars are keyed by image name, not by arch), so on a
# cross build podman has to be allowed to fetch the target-arch base itself.
BUILD_PLATFORM_ARGS=()
PULL_ARGS=(--pull=never)
if cross_building; then
  BUILD_PLATFORM_ARGS=(--platform "linux/$(build_oci_arch)")
  PULL_ARGS=()
fi

# The local targets build images that are RUN on this host — the test harness,
# dev, and the locally built UI/NC/ingress/gfeh images the harness injects into
# its containers. A cross TARGET cannot produce anything usable for any of them,
# so refuse it here rather than at exec time inside a container.
case "$1" in
  production | test | dev-base | dev | ui-integration | ui-local | nc-local | ingress-local | gfeh-local)
    require_native_target "$1"
    ;;
esac

# The targets whose Containerfile runs `bun install`, and so mount the shared
# bun cache via BUN_BUILD_ARGS. podman build is rootful here, so bun writes the
# cache as root and the host-side bun_install that shares the directory cannot
# add to it afterwards; bun_cache_reclaim puts it back. On EXIT rather than
# after the build, because a build that failed part-way has usually already
# downloaded packages worth keeping, and leaving them root-owned would make the
# next `make dev` re-fetch every one of them.
case "$1" in
  production | dev-base | ui-integration | ui-local | release | release-ui | push-rc | push-release | push-tag)
    trap bun_cache_reclaim EXIT
    ;;
esac

case "$1" in
  production)
    step "Building production image"
    mkdir -p .cache/go-mod .cache/go-build "${BUN_CACHE}"
    ${SUDO} podman build --network=host --pull=never \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      "${BUN_BUILD_ARGS[@]}" \
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
    mkdir -p .cache/dev-go-mod .cache/dev-go-build "${BUN_CACHE}"
    ${SUDO} podman build --network=host --pull=never \
      --volume "$(pwd)/.cache/dev-go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/dev-go-build:/root/.cache/go-build:z" \
      "${BUN_BUILD_ARGS[@]}" \
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
    mkdir -p "${BUN_CACHE}"
    ${SUDO} podman build --network=host --pull=never --no-cache \
      "${BUN_BUILD_ARGS[@]}" \
      -t "${PODMAN_UI_IMAGE}" -f integration/testdata/Containerfile.ui-integration .
    ;;
  # Local UI image for tests. Built from the in-repo UI source so it always
  # matches the host arch; quay.io/town/ui tags are for production/release
  # only and must never be used for testing. Saved to the image cache so
  # load_images_into_container can copy it into test containers.
  ui-local)
    step "Building local UI test image"
    mkdir -p "${BUN_CACHE}"
    ${SUDO} podman build --network=host --pull=never \
      "${BUN_BUILD_ARGS[@]}" \
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
    # Busted daily rather than --no-cache'd like the release build below. This
    # fixture is a prerequisite of every test-integration and dev run, so
    # --no-cache would recompile the Rust dependency tree on each one. It cannot
    # stay purely cached either: the NC, ingress and UI fixtures take their
    # content from repo source, so a source change busts their layer, but this
    # one takes gfehd from crates.io behind a byte-identical RUN line. A pure
    # cache hit freezes it on whatever was current the first time it was built
    # here, and the integration and UI suites start real partitions against it.
    ${SUDO} podman build --network=host \
      --build-arg "GFEH_CACHE_DATE=$(date +%Y%m%d)" \
      --volume "$(pwd)/.cache/cargo-registry:/usr/local/cargo/registry:z" \
      -t "${GFEH_IMAGE}" -f Containerfile.gfeh .
    save_image_cache "${GFEH_IMAGE}"
    ;;
  release)
    step "Building release image"
    require_cross_binfmt
    mkdir -p .cache/go-mod .cache/go-build "${BUN_CACHE}"
    ${SUDO} podman build --network=host "${PULL_ARGS[@]}" "${BUILD_PLATFORM_ARGS[@]}" \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      "${BUN_BUILD_ARGS[@]}" \
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
    # The mounted host bun cache keeps bun install fast despite --no-cache.
    require_cross_binfmt
    mkdir -p "${BUN_CACHE}"
    ${SUDO} podman build --network=host "${PULL_ARGS[@]}" "${BUILD_PLATFORM_ARGS[@]}" --no-cache \
      "${BUN_BUILD_ARGS[@]}" \
      -t "${RELEASE_UI_IMAGE}" -f Containerfile.ui .
    ;;
  release-proton)
    if [[ "${PROTON_ENABLED}" != "1" ]]; then
      echo "release-proton requires PROTON_ENABLED=1 (build tag and runner image are off by default)" >&2
      exit 1
    fi
    # No cross build, and not an oversight: GE-Proton is x86_64 Wine and the
    # image adds the i386 multiarch to run 32-bit Windows executables. There is
    # nothing to cross-compile TO — an aarch64 Proton runner would be an image
    # with no runner in it.
    if [ "${ARCH}" != "x86_64" ]; then
      echo "release-proton builds x86_64 only (GE-Proton ships x86_64 Wine); TARGET=${TARGET:-} asks for ${ARCH}" >&2
      exit 1
    fi
    step "Building Proton runner image"
    ${SUDO} podman build --network=host "${BUILD_PLATFORM_ARGS[@]}" \
      -t "${RELEASE_PROTON_IMAGE}" -f Containerfile.proton .
    ;;
  release-nc)
    step "Building network controller image"
    require_cross_binfmt
    mkdir -p .cache/go-mod .cache/go-build
    # No --pull=never: release-nc-image has no dependency on the image-cache
    # load, so the bases (golang:1.25-bookworm, caddy:2-alpine, alpine:latest)
    # may not be in the host store on a fresh checkout even though all three
    # are in BASE_IMAGES/ALL_IMAGES. Let podman pull them on demand.
    ${SUDO} podman build --network=host "${BUILD_PLATFORM_ARGS[@]}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${RELEASE_NC_IMAGE}" -f Containerfile.networkcontroller .
    ;;
  release-ingress)
    step "Building ingress image"
    require_cross_binfmt
    mkdir -p .cache/go-mod .cache/go-build
    # No --pull=never: release-ingress-image has no dependency on the
    # image-cache load, so the bases (golang:1.25-bookworm, caddy:2-alpine,
    # alpine:latest) may not be in the host store on a fresh checkout even
    # though all three are in BASE_IMAGES/ALL_IMAGES. Let podman pull them on
    # demand.
    ${SUDO} podman build --network=host "${BUILD_PLATFORM_ARGS[@]}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      -t "${RELEASE_INGRESS_IMAGE}" -f Containerfile.ingress .
    ;;
  release-gfeh)
    step "Building gfeh image"
    require_cross_binfmt
    mkdir -p .cache/cargo-registry
    # --no-cache is load-bearing, not caution. The image takes whatever gfehd
    # crates.io holds today, and `cargo install gfehd` is a byte-identical RUN
    # line on every build — so podman's layer cache would serve the first
    # build's crate forever and a release would silently ship a version that
    # was current months ago. There is no cheaper cache key available: knowing
    # when the crate changed means asking crates.io, which is the thing this
    # build is for.
    #
    # The cargo registry volume survives --no-cache, so this re-compiles the
    # dependency tree but does not re-download it. Only the release path pays it
    # on every build; the local fixture above pays it once a day.
    ${SUDO} podman build --network=host --no-cache "${BUILD_PLATFORM_ARGS[@]}" \
      --volume "$(pwd)/.cache/cargo-registry:/usr/local/cargo/registry:z" \
      -t "${RELEASE_GFEH_IMAGE}" -f Containerfile.gfeh .
    ;;
  push-rc)
    require_cross_binfmt
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
    mkdir -p .cache/go-mod .cache/go-build "${BUN_CACHE}"
    ${SUDO} podman build --network=host "${PULL_ARGS[@]}" "${BUILD_PLATFORM_ARGS[@]}" \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      "${BUN_BUILD_ARGS[@]}" \
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

    # Object storage (gfeh) image.
    substep "Tagging ${RELEASE_GFEH_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_GFEH_IMAGE}" "${RELEASE_GFEH_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_GFEH_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_GFEH_IMAGE}" "${RELEASE_GFEH_IMAGE}:rc.latest-${ARCH}"
    substep "Pushing ${RELEASE_GFEH_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_GFEH_IMAGE}:rc.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_GFEH_IMAGE}:rc.latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_GFEH_IMAGE}:rc.latest-${ARCH}"

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
    require_cross_binfmt
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
    mkdir -p .cache/go-mod .cache/go-build "${BUN_CACHE}"
    ${SUDO} podman build --network=host "${PULL_ARGS[@]}" "${BUILD_PLATFORM_ARGS[@]}" \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      "${BUN_BUILD_ARGS[@]}" \
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

    # Ingress image.
    substep "Tagging ${RELEASE_INGRESS_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_INGRESS_IMAGE}" "${RELEASE_INGRESS_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_INGRESS_IMAGE}:latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_INGRESS_IMAGE}" "${RELEASE_INGRESS_IMAGE}:latest-${ARCH}"
    substep "Pushing ${RELEASE_INGRESS_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_INGRESS_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_INGRESS_IMAGE}:latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_INGRESS_IMAGE}:latest-${ARCH}"

    # Object storage (gfeh) image.
    substep "Tagging ${RELEASE_GFEH_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman tag "${RELEASE_GFEH_IMAGE}" "${RELEASE_GFEH_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Tagging ${RELEASE_GFEH_IMAGE}:latest-${ARCH}"
    ${SUDO} podman tag "${RELEASE_GFEH_IMAGE}" "${RELEASE_GFEH_IMAGE}:latest-${ARCH}"
    substep "Pushing ${RELEASE_GFEH_IMAGE}:release.${DATE_TAG}-${ARCH}"
    ${SUDO} podman push "${RELEASE_GFEH_IMAGE}:release.${DATE_TAG}-${ARCH}"
    substep "Pushing ${RELEASE_GFEH_IMAGE}:latest-${ARCH}"
    ${SUDO} podman push "${RELEASE_GFEH_IMAGE}:latest-${ARCH}"

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
    require_cross_binfmt
    require_registry_login quay.io
    step "Pushing all images with tag ${TAG}"

    # Systemcontroller image. The tag is no longer baked into the binary; the
    # controller resolves it at runtime from TOWN_OS_TAG (set on its systemd
    # unit by the install build system), defaulting to rc.latest-<arch>.
    substep "Building ${RELEASE_IMAGE}:${TAG}"
    mkdir -p .cache/go-mod .cache/go-build "${BUN_CACHE}"
    ${SUDO} podman build --network=host "${PULL_ARGS[@]}" "${BUILD_PLATFORM_ARGS[@]}" \
      --build-arg "TOWN_OS_GO_TAGS=${GO_BUILD_TAGS}" \
      --volume "$(pwd)/.cache/go-mod:/go/pkg/mod:z" \
      --volume "$(pwd)/.cache/go-build:/root/.cache/go-build:z" \
      "${BUN_BUILD_ARGS[@]}" \
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

    # Ingress image.
    substep "Tagging ${RELEASE_INGRESS_IMAGE}:${TAG}"
    ${SUDO} podman tag "${RELEASE_INGRESS_IMAGE}" "${RELEASE_INGRESS_IMAGE}:${TAG}"
    substep "Pushing ${RELEASE_INGRESS_IMAGE}:${TAG}"
    ${SUDO} podman push "${RELEASE_INGRESS_IMAGE}:${TAG}"

    # Object storage (gfeh) image.
    substep "Tagging ${RELEASE_GFEH_IMAGE}:${TAG}"
    ${SUDO} podman tag "${RELEASE_GFEH_IMAGE}" "${RELEASE_GFEH_IMAGE}:${TAG}"
    substep "Pushing ${RELEASE_GFEH_IMAGE}:${TAG}"
    ${SUDO} podman push "${RELEASE_GFEH_IMAGE}:${TAG}"
    ;;
  networkcontroller)
    step "Building network controller binary"
    CGO_ENABLED=0 go build -o town-os-networkcontroller ./src/networkcontroller/cmd/town-os-networkcontroller
    ;;
  *)
    echo "Usage: $0 {production|test|dev-base|dev|ui-integration|ui-local|nc-local [src-image]|ingress-local|gfeh-local|networkcontroller|release|release-ui|release-proton|release-nc|release-ingress|release-gfeh|push-rc|manifest-rc|push-release|manifest-release|push-ui-rc|push-ui-release|push-proton-rc|push-proton-release|push-nc-rc|push-nc-release|push-ingress-rc|push-ingress-release|push-gfeh-rc|push-gfeh-release|push-tag <tag>}"
    exit 1
    ;;
esac

#!/usr/bin/env bash

# Print a grouped list of the most useful Town OS make targets.
# This is the default `make` target.
#
# The listing is static: it never varies with the environment, so what you read
# here is the same on every machine. Targets that need a flag to be usable say
# so inline rather than appearing and disappearing.

set -e

cat <<'EOF'
Town OS — make targets

Setup
  deps                    Install Go, podman, btrfs-progs, golangci-lint, bun,
                          and all other host dependencies (Arch or Ubuntu).
  preflight-dev           Verify podman, btrfs, repo creds, and bridge networking.
  check-<tool>            Verify one host dependency is present. One of:
                          go, bun, podman, runc, btrfs, golangci-lint,
                          python3, libsystemd.
  pull-images             Pull every base/monitoring/rolodex image into the
                          shared image cache. Always pulls.
  pull-images-daily       The same pull, but at most once a day (what test-full
                          and dev run). Same window as the bun refresh; override
                          with PULL_MAX_AGE=<seconds>.
  ensure-image-cache      Create the shared image cache directory.
  docker-login            Log in to docker.io with DOCKER_USERNAME/PASSWORD.
  quay-login              Log in to quay.io with QUAY_USERNAME/PASSWORD.

Build & lint
  lint                    Run golangci-lint and the UI lint suite.
  production-image        Build the production systemcontroller container image.
  test-image              Build the integration-test container image.
  ui-image                Build the local UI image used by tests (host arch).
  nc-image                Build the local NC image used by tests (host arch).
  nc-image-dev            Build the local NC image for the dev container.
  ingress-image           Build the local ingress image used by tests (host arch).
  gfeh-image              Build the local object-storage (gfeh) image (host arch).
  ui-integration-image    Build the container that runs the UI integration suite.
  dev-production-image    Build the dev base (production) container image.
  dev-image               Build the dev container image.
  build-networkcontroller Build the network controller binary into the image.

Tests
  test                    Run Go and UI unit tests.
  test-ui-unit            Run UI unit tests only.
  test-ui-integration-local
                          Run the UI integration suite on the host against an
                          already-running backend.
  test-integration-build  Build everything test-integration needs, without
                          running the tests.
  test-integration        Run Go integration tests inside the test container.
  test-integration-rerun  Re-run the Go integration tests in the container left
                          behind by test-integration (no rebuild).
  test-ui-integration     Run UI integration tests against a backend container.
  test-full               Run lint + unit + integration + UI integration.
  test-full-log           Same as test-full, tee'd into a timestamped log file.
  auto-test               Watch .go/.js files and re-run `make test`.
  auto-test-full          Watch .go/.js files and re-run `make test-full`.

Dev loop
  dev                     Start the dev container with btrfs, podman, and the
                          systemcontroller running.
  dev-logs                Tail the dev container logs.
  dev-stop                Stop the dev container for this working tree.
  dev-stop-all            Stop every town-os-dev container on the host.
  ssh                     SSH into the running town-os.local image.

Storage
  btrfs                   Create the test btrfs loopback filesystem.
  btrfs-dev               Create the dev btrfs loopback filesystem (recreates it).
  dev-btrfs               Create the dev btrfs filesystem only if absent.

Local infrastructure
  registry                Start the local docker registry mirror.
  registry-populate       Mirror all referenced docker.io images into it.
  registry-stop           Stop the local registry.
  gitea                   Start the local Gitea server for test repos.
  gitea-populate          Migrate test package repos into local Gitea.
  gitea-stop              Stop the local Gitea server.

Release
  release-build           Run test-full and build all release images
                          (sc, ui, nc, ingress; proton when PROTON_ENABLED=1).
  release-image           Build the system controller release image.
  release-ui-image        Build the UI release image.
  release-nc-image        Build the network controller release image.
  release-ingress-image   Build the ingress release image.
  release-gfeh-image      Build the object-storage (gfeh) release image.
  push / push-rc          Push all release images with per-arch rc tags
                          (rc.latest-<arch>, rc.<date>-<arch>).
  manifest-rc             Assemble + push the plain rc.latest / rc.<date>
                          multi-arch manifest lists from the per-arch tags.
  push-release            Push all release images with per-arch release tags
                          (latest-<arch>, release.<date>-<arch>).
  manifest-release        Assemble + push the plain latest / release.<date>
                          multi-arch manifest lists from the per-arch tags.
  push-tag PUSH_TAG=...   Push all release images with a specific tag.

  Single-image pushes (same per-arch tag scheme as push-rc/push-release):
  push-ui-rc              push-ui-release
  push-nc-rc              push-nc-release
  push-ingress-rc         push-ingress-release
  push-gfeh-rc            push-gfeh-release

Proton (defined only when PROTON_ENABLED=1; see Variables below)
  release-proton-image    Build the Proton runner release image.
  push-proton-rc          Push the Proton runner image with per-arch rc tags.
  push-proton-release     Push the Proton runner image with per-arch release tags.

Cleanup
  clean                   Remove cached state for this working tree.
  clean-cache             Remove the per-working-tree cache directory.
  clean-image-cache       Remove the shared image cache.
  clean-bun-cache         Remove the host-wide bun package cache. Not swept by
                          clean: it is meant to outlive a checkout.
  clean-btrfs             Unmount and detach the btrfs loopback device.
  clean-btrfs-dev         Unmount and detach the dev btrfs loopback device.
  clean-containers        Remove all town-os-* containers on the host.
  clean-integration       Stop registry and Gitea containers.
  clean-dev               Stop dev containers and clean their cache.
  clean-all               Run every cleanup target.

Variables
  Pass on the command line (`make VAR=value <target>`) or set in .env, which
  the Makefile includes if present.

  PROTON_ENABLED=0|1      Opt in to the Proton/Wine runner (default 0). Turns on
                          the `proton` Go build tag, defines the Proton targets
                          above, adds the runner image to release-build /
                          push-rc / push-release, and exposes the Proton
                          settings card in the UI.
  GFEH_VERSION=<semver>   The gfeh crate the object-storage image is built from
                          (default pinned). Ignored when GFEH_LATEST is set.
  GFEH_LATEST=1           Build the object-storage image from whatever crates.io
                          holds today instead of the pin. For tracking an
                          unreleased fix; not for a release.
  TEST_RUN=<regex>        Restrict the integration run to matching tests.
  TEST_TIMEOUT=<dur>      Integration test timeout (default 60m).
  PUSH_TAG=<tag>          Tag used by push-tag.
  ARCHES="x86_64 aarch64" Architectures the manifest-* targets assemble.
  IMAGE_CACHE=<dir>       Shared image cache (default /var/cache/town-os/images).
  BUN_CACHE=<dir>         Host-wide npmjs package cache, shared by host-side bun
                          installs and the UI container builds
                          (default ~/.cache/town-os/bun).
  PULL_MAX_AGE=<seconds>  How stale our picture of upstream may get before
                          test-full and dev re-pull images and let bun
                          re-resolve against npmjs (default 86400, one day).
                          0 checks every run.
  BTRFS_IMAGE_DIR=<dir>   Where btrfs loopback backing images live. Must be a
                          real disk, never tmpfs (default .cache/btrfs).
  ROLODEX_IMAGE_TAG=<tag> Rolodex tag to pull, always arch-suffixed
                          (default rc.latest-<arch>).
  TOWN_OS_TAG=<tag>       Tag every sibling image derives from
                          (default rc.latest-<arch>).
  UI_IMAGE / NC_IMAGE / INGRESS_IMAGE
                          Locally built images injected into test and dev
                          containers; override to test a different build.
  VITE_API_URL=<url>      Override the API URL the UI talks to.

  Credentials (env or .env):
  TOWN_OS_REPO_USERNAME / TOWN_OS_REPO_PASSWORD   Package repository access.
  DOCKER_USERNAME / DOCKER_PASSWORD               docker-login.
  QUAY_USERNAME / QUAY_PASSWORD                   quay-login and every push-*.

Run `make <target>` to invoke one. See CLAUDE.md and README.md for details.
EOF

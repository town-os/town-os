#!/usr/bin/env bash

# Print a grouped list of the most useful Town OS make targets.
# This is the default `make` target.

set -e

: "${PROTON_ENABLED:=0}"

cat <<'EOF'
Town OS — make targets

Setup
  deps                    Install Go, podman, btrfs-progs, golangci-lint, bun,
                          and all other host dependencies (Arch or Ubuntu).
  preflight-dev           Verify podman, btrfs, repo creds, and bridge networking.

Build & lint
  lint                    Run golangci-lint and the UI lint suite.
  production-image        Build the production systemcontroller container image.
  test-image              Build the integration-test container image.
  ui-image                Build the local UI image used by tests (host arch).
  nc-image                Build the local NC image used by tests (host arch).
  nc-image-dev            Build the local NC image for the dev container.
  dev-image               Build the dev container image.
  build-networkcontroller Build the network controller binary into the image.

Tests
  test                    Run Go and UI unit tests.
  test-ui-unit            Run UI unit tests only.
  test-integration        Run Go integration tests inside the test container.
  test-ui-integration     Run UI integration tests against a backend container.
  test-full               Run lint + unit + integration + UI integration.
  test-full-log           Same as test-full, tee'd into a timestamped log file.

Dev loop
  dev                     Start the dev container with btrfs, podman, and the
                          systemcontroller running.
  dev-logs                Tail the dev container logs.
  dev-stop                Stop the dev container for this working tree.
  dev-stop-all            Stop every town-os-dev container on the host.
  ssh                     SSH into the running town-os.local image.

Local infrastructure
  registry                Start the local docker registry mirror.
  registry-populate       Mirror all referenced docker.io images into it.
  registry-stop           Stop the local registry.
  gitea                   Start the local Gitea server for test repos.
  gitea-populate          Migrate test package repos into local Gitea.
  gitea-stop              Stop the local Gitea server.

Release
  release-build           Build all release images (sc, ui, nc; proton when PROTON_ENABLED=1).
  release-image           Build the system controller release image.
  release-ui-image        Build the UI release image.
  release-nc-image        Build the network controller release image.
  push-rc                 Push all release images with per-arch rc tags
                          (rc.latest-<arch>, rc.<date>-<arch>).
  manifest-rc             Assemble + push the plain rc.latest / rc.<date>
                          multi-arch manifest lists from the per-arch tags.
  push-release            Push all release images with per-arch release tags
                          (latest-<arch>, release.<date>-<arch>).
  manifest-release        Assemble + push the plain latest / release.<date>
                          multi-arch manifest lists from the per-arch tags.
  push-tag PUSH_TAG=...   Push all release images with a specific tag.

Cleanup
  clean                   Remove cached state for this working tree.
  clean-cache             Remove the per-working-tree cache directory.
  clean-image-cache       Remove the shared image cache.
  clean-btrfs             Unmount and detach the btrfs loopback device.
  clean-containers        Remove all town-os-* containers on the host.
  clean-integration       Stop registry and Gitea containers.
  clean-dev               Stop dev containers and clean their cache.
  clean-all               Run every cleanup target.

Run `make <target>` to invoke one. See CLAUDE.md and README.md for details.
EOF

if [[ "${PROTON_ENABLED}" = "1" ]]; then
  cat <<'EOF'

Proton (opt-in; built because PROTON_ENABLED=1)
  release-proton-image    Build the Proton runner release image.
  push-proton-rc          Push the Proton runner image with per-arch rc tags.
  push-proton-release     Push the Proton runner image with per-arch release tags.
EOF
else
  cat <<'EOF'

Note: Proton/Wine runner support is OFF. Rebuild with `make PROTON_ENABLED=1 ...`
(or `go build -tags proton`) to opt in. See README.md for the full story.
EOF
fi

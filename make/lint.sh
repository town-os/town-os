#!/usr/bin/env bash

# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
set -e
. make/lib.sh

# Per-checkout lint cache. golangci keys cached issues by file path, and its
# paths are anchored per checkout, so a cache shared with another working copy
# (a worktree, a second clone) replays another tree's results against this one
# -- which resurfaces issues whose //nolint directives live at different lines
# here. Disk-backed under .cache/ like the btrfs images, never tmpfs, and
# per-working-directory so concurrent runs cannot collide.
export GOLANGCI_LINT_CACHE="${PWD}/.cache/golangci-lint"
mkdir -p "${GOLANGCI_LINT_CACHE}"

: "${GO_BUILD_TAGS:=}"
GO_TAGS_ARG=()
LINT_TAGS_ARG=()
if [[ -n "${GO_BUILD_TAGS}" ]]; then
  GO_TAGS_ARG=(-tags "${GO_BUILD_TAGS}")
  LINT_TAGS_ARG=(--build-tags "${GO_BUILD_TAGS}")
fi

step "Running go vet"
go vet "${GO_TAGS_ARG[@]}" ./src/... ./integration/...
step "Running golangci-lint"
"$(go env GOPATH)/bin/golangci-lint" run "${LINT_TAGS_ARG[@]}" ./src/... ./integration/...
step "Running eslint (ui)"
bun_install ui
(cd ui && bun run lint)

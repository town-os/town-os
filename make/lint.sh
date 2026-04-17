#!/usr/bin/env bash

# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
set -e
. make/lib.sh

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
(cd ui && bun run lint)

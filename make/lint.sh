#!/usr/bin/env bash

# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
set -e
. make/lib.sh

step "Running go vet"
go vet ./src/... ./integration/...
step "Running golangci-lint"
"$(go env GOPATH)/bin/golangci-lint" run ./src/... ./integration/...

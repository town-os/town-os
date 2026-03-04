#!/usr/bin/env bash
set -e
. make/lib.sh

step "Running go vet"
go vet ./src/... ./integration/...
step "Running golangci-lint"
"$(go env GOPATH)/bin/golangci-lint" run ./src/... ./integration/...

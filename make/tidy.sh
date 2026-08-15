#!/usr/bin/env bash

# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
set -e
. make/lib.sh

# `go mod tidy` prunes what `go get` only ever appends to.
#
# This exists as a make target rather than as a command a developer types
# because every other Go invocation in this repo goes through one -- the
# wrappers own the build tags, and a bare `go` call in a tree with tagged
# builds tidies against a different file set than the one that gets compiled.
#
# What it fixes, concretely: `go get` writes a new module version into go.mod
# and APPENDS its hashes to go.sum, leaving every superseded pseudo-version
# behind. Four rolodex-dns versions had accumulated that way. They are inert --
# nothing resolves them -- but they are indistinguishable at a glance from the
# one version that is real, so the file stops answering "which rolodex is this
# box built against" the moment there is more than one candidate in it.
: "${GO_BUILD_TAGS:=}"
GO_TAGS_ARG=()
if [[ -n "${GO_BUILD_TAGS}" ]]; then
  GO_TAGS_ARG=(-tags "${GO_BUILD_TAGS}")
fi

step "Tidying go.mod and go.sum"
go mod tidy "${GO_TAGS_ARG[@]}"

# Say what moved. A tidy that changed nothing and a tidy that dropped half of
# go.sum print the same thing otherwise, and this is a target whose whole
# output is the diff it leaves in the working tree.
if git diff --quiet -- go.mod go.sum; then
  substep "go.mod and go.sum were already tidy"
else
  substep "go.mod / go.sum changed:"
  git diff --stat -- go.mod go.sum
fi

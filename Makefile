# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.

-include .env
export MAKE
export TOWN_OS_REPO_USERNAME
export TOWN_OS_REPO_PASSWORD
export DOCKER_USERNAME
export DOCKER_PASSWORD
export QUAY_USERNAME
export QUAY_PASSWORD
export VITE_API_URL

# The REAL host arch in registry tag form (x86_64/aarch64, the uname -m form).
# BUILD_ARCH is derived from TARGET below and is what every per-arch image tag
# is suffixed with; HOST_ARCH never changes, so the build machinery can always
# tell which architecture it is actually standing on.
HOST_ARCH := $(shell uname -m | sed -e 's/amd64/x86_64/' -e 's/arm64/aarch64/')
# All architectures a release covers (used by manifest assembly).
ARCHES ?= x86_64 aarch64

# TARGET names the architecture this invocation builds and pushes for, spelled
# exactly as ../install spells it so one value drives both repos in a release
# run. Empty (the default) is the native host arch. Recognized values:
#   x86_64 (x86, amd64)
#   aarch64 (arm64)
#   rpi, rg35xxpro (rg35xx-pro, rg35xx, anbernic)   — ../install image flavors,
#       accepted here and folded to aarch64, since a flavor changes how that
#       repo boots the box and not which container images it runs.
#
# A TARGET naming a foreign arch cross-builds (release/push targets only — see
# CROSS below). ../install emulates a whole aarch64 MACHINE for its disk image;
# these are container images, so instead every builder stage is pinned to the
# BUILD platform and cross-compiles. bun, the Go toolchain and cargo all run
# natively at full speed, which matters because bun is JavaScriptCore and its
# JIT does not survive qemu-user. Only the runtime stages (apt-get, groupadd)
# execute target-arch binaries, and those need a binfmt handler — build.sh
# checks for one and says how to register it.
TARGET ?=
ifeq ($(TARGET),)
BUILD_ARCH := $(HOST_ARCH)
else ifneq ($(filter x86_64 x86 amd64,$(TARGET)),)
BUILD_ARCH := x86_64
else ifneq ($(filter aarch64 arm64 rpi rg35xxpro rg35xx-pro rg35xx anbernic,$(TARGET)),)
BUILD_ARCH := aarch64
else
$(error unknown TARGET '$(TARGET)' — expected one of: x86_64, aarch64, rpi, rg35xxpro)
endif

# CROSS is derived, not a user knob — set TARGET. It is what the release
# targets consult to decide whether to pass --platform and demand binfmt, and
# what the test and dev targets consult to refuse outright: those RUN the
# images they build, on this host, so a foreign arch there is never what
# somebody meant.
CROSS :=
ifneq ($(BUILD_ARCH),$(HOST_ARCH))
CROSS := 1
endif

export HOST_ARCH ARCHES TARGET BUILD_ARCH CROSS

# Runtime tags stay on HOST_ARCH even under a cross build, and deliberately:
# this is the tag the controller derives its sibling images from when it is
# RUNNING (dev, and the systemcontroller's own default), so it must name images
# that can execute on this box. Only release artifacts carry BUILD_ARCH — see
# ARCH in make/build.sh.
TOWN_OS_TAG ?= rc.latest-$(HOST_ARCH)
export TOWN_OS_TAG

# Opt-in build of the Proton/Wine runner. Default off — see CLAUDE.md and the
# README for the reasoning. PROTON_ENABLED=1 turns on the `proton` Go build
# tag, includes the proton runner image in the release pipeline, and exposes
# the proton settings card in the UI (via /status/ping).
PROTON_ENABLED ?= 0
ifeq ($(PROTON_ENABLED),1)
GO_BUILD_TAGS := proton
else
GO_BUILD_TAGS :=
endif
export PROTON_ENABLED GO_BUILD_TAGS

# Unique instance ID from working directory path.
INSTANCE_ID := $(shell echo -n "$(CURDIR)" | md5sum | cut -c1-8)
export INSTANCE_ID

# Ephemeral state directory — per-run bookkeeping (port files, .disk/.loop/
# .mount tracking files, dev metadata) lives here instead of the working
# directory. Data-bearing artifacts do NOT: see BTRFS_IMAGE_DIR below.
STATE_DIR := /tmp/town-os-$(INSTANCE_ID)
export STATE_DIR

# Disk-backed directory for btrfs loopback backing images. These MUST NOT live
# on tmpfs: a loop device backed by tmpfs deadlocks the host kernel under
# memory pressure and hard-reboots the machine. /tmp is tmpfs on Arch/Manjaro/
# Fedora, so the images live in the gitignored .cache/ under the checkout
# (a real disk) instead of STATE_DIR. Override with BTRFS_IMAGE_DIR=...
BTRFS_IMAGE_DIR ?= $(CURDIR)/.cache/btrfs
export BTRFS_IMAGE_DIR

# Where `make <target>-log` tees its transcript (see the %-log pattern rule in
# make/include.mk). `?=` rather than `:=` because .env is included above: a `:=`
# here would silently clobber a LOG_DIR set there or in the environment, leaving
# only the command line able to override it.
LOG_DIR ?= /tmp/town-os/log
export LOG_DIR

# Per-run ephemeral port files. Pure bookkeeping (no data), so STATE_DIR is the
# right home for them.
#
# SYSTEM_PORT_FILES relocate the otherwise-fixed ports the system services bind:
# rolodex :53 and its metrics :9153, node-exporter :9100, Prometheus :9090, the
# monitoring UI :5308, and the ingress :443/:80 and its metrics :9146. The test
# container runs
# --net host (deliberately —
# bridge-network DNS breaks on captive networks), so those services bind in the
# *host* namespace, and without these a `make test-full` and a `make dev` fight
# over every one of them and crash-loop each other under Restart=always.
# `make dev` allocates none of these and keeps the production ports, because dev
# is meant to mirror a real box — IRON RULE.
SYSTEM_PORT_FILES := $(STATE_DIR)/.dns-port $(STATE_DIR)/.rolodex-metrics-port \
                     $(STATE_DIR)/.node-exporter-port \
                     $(STATE_DIR)/.prometheus-port $(STATE_DIR)/.monitoring-port \
                     $(STATE_DIR)/.ingress-https-port $(STATE_DIR)/.ingress-http-port \
                     $(STATE_DIR)/.ingress-metrics-port
PORT_FILES := $(STATE_DIR)/.integration-port $(STATE_DIR)/.registry-port \
              $(STATE_DIR)/.gitea-port $(SYSTEM_PORT_FILES)
export SYSTEM_PORT_FILES PORT_FILES

# Image names (unique per working directory).
# Integration and dev use separate production base images so builds
# cannot interfere with each other.
PODMAN_IMAGE         := town-os-$(INSTANCE_ID)
PODMAN_DEV_BASE      := town-os-dev-base-$(INSTANCE_ID)
PODMAN_TEST_IMAGE    := town-os-test-$(INSTANCE_ID)
PODMAN_DEV_IMAGE     := town-os-dev-$(INSTANCE_ID)
PODMAN_UI_IMAGE      := town-os-ui-integration-$(INSTANCE_ID)
RELEASE_IMAGE        := quay.io/town/town
RELEASE_UI_IMAGE     := quay.io/town/ui
RELEASE_PROTON_IMAGE := quay.io/town/proton
RELEASE_NC_IMAGE     := quay.io/town/networkcontroller
RELEASE_INGRESS_IMAGE := quay.io/town/ingress
RELEASE_GFEH_IMAGE   := quay.io/town/gfeh
export PODMAN_IMAGE PODMAN_DEV_BASE PODMAN_TEST_IMAGE PODMAN_DEV_IMAGE PODMAN_UI_IMAGE RELEASE_IMAGE RELEASE_UI_IMAGE RELEASE_PROTON_IMAGE RELEASE_NC_IMAGE RELEASE_INGRESS_IMAGE RELEASE_GFEH_IMAGE

# Container names (unique per working directory).
PODMAN_CONTAINER     := town-os-test-$(INSTANCE_ID)
PODMAN_UI_CONTAINER  := town-os-ui-integration-$(INSTANCE_ID)
PODMAN_UI_BACKEND    := town-os-ui-backend-$(INSTANCE_ID)
PODMAN_DEV_CONTAINER := town-os-dev
PREFLIGHT_CONTAINER  := preflight-test-$(INSTANCE_ID)
REGISTRY_CONTAINER   := town-os-registry-$(INSTANCE_ID)
GITEA_CONTAINER      := town-os-gitea-$(INSTANCE_ID)
export PODMAN_CONTAINER PODMAN_UI_CONTAINER PODMAN_UI_BACKEND PODMAN_DEV_CONTAINER
export PREFLIGHT_CONTAINER REGISTRY_CONTAINER GITEA_CONTAINER

# Per-checkout image cache. Every cache this build keeps lives under the
# gitignored .cache/ of the checkout that produced it, so a checkout owns all of
# its own state and `rm -rf` on the tree really does leave nothing behind. This
# one used to be /var/cache/town-os/images, shared across working trees: a
# root-owned directory outside every checkout, which no `git worktree remove`
# could reach, which `make clean` in one tree deleted out from under every
# other, and which the ${SUDO} that had to write it made unreadable to anything
# running as the user. Sharing bought a saved pull; it cost a cache nobody
# owned. Override with:
#   make IMAGE_CACHE=/some/other/path test-full
IMAGE_CACHE ?= $(CURDIR)/.cache/images
export IMAGE_CACHE

# How often `make test-full` and `make dev` re-check upstream for new tags.
#
# Both used to hang an unconditional pull-images off the front, so every run
# re-pulled every entry in ALL_IMAGES — several gigabytes of registry traffic to
# usually learn nothing had moved, and a hard dependency on the network for a
# test run that had all its images locally. The stamp file records when the last
# check ran; pull-images-daily skips entirely (no pull, no registry login) while
# it is younger than PULL_MAX_AGE.
#
# The stamps sit in .cache/ beside the caches they describe — per-checkout
# bookkeeping about a per-checkout cache. Force a check with `make pull-images`,
# which is still unconditional and is what release-build uses — a release must
# never ship an image the box happened to have lying around.
IMAGE_PULL_STAMP := $(CURDIR)/.cache/.images-pulled-daily
BUN_STAMP := $(CURDIR)/.cache/.bun-refreshed-daily
# One window for both, because they are the same question asked of two
# registries: how old may our picture of upstream be before we go look again.
PULL_MAX_AGE ?= 86400
export IMAGE_PULL_STAMP BUN_STAMP PULL_MAX_AGE

# Per-checkout npmjs package cache, the JS half of IMAGE_CACHE and in the same
# place for the same reason.
#
# Every bun in this tree resolves to this one directory: host-side `bun install`
# (lint, test, dev) via bun_install's --cache-dir, and the container builds via
# the BUN_BUILD_ARGS mount. What must not happen is a build reaching bun with
# the variable unset, because bun then silently falls back to
# ~/.bun/install/cache and re-downloads the world into a directory nothing else
# reads.
#
# "The same directory" has to mean the same PATH, on the host and inside every
# build that mounts it. Bun resolves a package through an absolute symlink
# (<cache>/<name>/<version> -> <cache>/<name>@<version>@@@N), so the cache's own
# path is written into each of its entries and a cache mounted somewhere else is
# a cache full of dangling links — a guaranteed miss on every package, with no
# way for the missing side to repair it. This is why the container builds mount
# BUN_CACHE at BUN_CACHE and pass it on as the BUN_CACHE_DIR build-arg rather
# than parking it at a fixed /bun-cache; see BUN_BUILD_ARGS in make/lib.sh.
#
# Must be disk-backed. The checkout is; /tmp on Arch and Fedora is not. Bun's
# cache is content-addressed and safe for concurrent use, so parallel test runs
# in one checkout sharing it is the ordinary case, not a hazard — IRON RULE.
BUN_CACHE ?= $(CURDIR)/.cache/bun
# Every host-side bun in this build reads it from the environment; the
# Containerfiles take the same path as a build-arg and mount it there.
BUN_INSTALL_CACHE_DIR := $(BUN_CACHE)
export BUN_CACHE BUN_INSTALL_CACHE_DIR

# Image lists. Every entry is the canonical upstream repository, fully
# qualified: docker.io/library/* for the docker-library official images, and
# the vendor's own namespace otherwise (oven/bun, grafana/grafana,
# gitea/gitea, quay.io/prometheus/*). Never use an unqualified short name —
# it would resolve through podman's unqualified-search-registries and pull
# whatever that host happens to list first.
#
# debian:bookworm-slim is the systemcontroller runtime base (Containerfile);
# debian:bookworm (non-slim) is still the Proton runner base (Containerfile.proton).
# Both must be pre-pulled because release builds run podman with --pull=never.
# caddy:2-alpine is the donor stage the ingress and NC images copy the caddy
# binary out of (Containerfile.ingress, Containerfile.networkcontroller), and
# the test image copies the same binary so the ingress suite validates rendered
# Caddyfiles against the caddy that actually serves them rather than skipping
# (integration/testdata/Containerfile.systemd);
# caddy:latest is the UI runtime base (Containerfile.ui). The only FROM base
# deliberately left out is rust:1-bookworm (~1.5G, gfeh builder only) — see
# the release-gfeh comment in make/build.sh.
BASE_IMAGES := docker.io/library/golang:1.25-bookworm docker.io/oven/bun:latest docker.io/library/debian:bookworm docker.io/library/debian:bookworm-slim docker.io/library/caddy:latest docker.io/library/caddy:2-alpine

# The subset of BASE_IMAGES that a CROSS build wants at the target architecture
# rather than the host's: the bases a cross-buildable Containerfile names with a
# bare FROM, which is to say the stages that ship rather than the ones that
# compile. load-base stages these at $(BUILD_ARCH) and everything else at
# $(HOST_ARCH).
#
# Without the split, load-base staged all six at the host arch on every run —
# and it is a prerequisite of nearly every build target, so a cross build's own
# prerequisites forced debian:bookworm-slim back to amd64 immediately before the
# release arm needed arm64. Each cross invocation paid an rmi plus a load in
# each direction (a network pull, whenever the tar it wanted was missing), and
# `podman image inspect` on the box reported the host arch throughout, which
# reads exactly like the staging not working at all.
#
# golang and bun are absent deliberately: every cross Containerfile pins them
# with FROM --platform=$$BUILDPLATFORM because they run HERE and cross-compile,
# so the host arch is the correct one for them under any TARGET. debian:bookworm
# is absent for a different reason — it is a bare FROM only in the proton image
# (x86_64-only by construction) and in the native-only test and dev images, so
# no cross build ever wants it. TestBaseImagesRuntimeMatchesTheContainerfiles
# checks this list against the Containerfiles rather than trusting it.
BASE_IMAGES_RUNTIME := docker.io/library/debian:bookworm-slim docker.io/library/caddy:latest docker.io/library/caddy:2-alpine
MONITORING_IMAGES := quay.io/prometheus/prometheus:latest quay.io/prometheus/node-exporter:latest docker.io/grafana/grafana:latest
# Rolodex publishes per-arch tags (rc.latest-x86_64 / rc.latest-aarch64) pushed
# natively from each host. Internal Town OS image pulls default to the host's
# per-arch rc tag (rc.latest-<arch>) so the harness, dev, and the runtime all
# track the rc channel by default. The plain rc.latest (no arch suffix) is a
# multi-arch manifest list and must never be pulled directly. Override with
# ROLODEX_IMAGE_TAG=... (e.g. latest-$(HOST_ARCH) for a released rolodex).
# HOST_ARCH, not BUILD_ARCH: this image is pulled to be RUN here, by the test
# harness and by dev, so it tracks the host even when TARGET cross-builds.
ROLODEX_IMAGE_TAG ?= rc.latest-$(HOST_ARCH)
ROLODEX_IMAGE := quay.io/town/rolodex:$(ROLODEX_IMAGE_TAG)
# The UI image for tests is built locally from Containerfile.ui (ui-image
# target) so it always matches the host arch and the in-repo UI source.
# quay.io/town/ui tags are for production/release only — rc.latest must
# never be used for testing.
UI_IMAGE ?= localhost/town-os-ui:$(INSTANCE_ID)
# The NC image for tests and dev is built locally on the host (nc-image /
# nc-image-dev targets) and loaded into the containers. Building it inside
# the test/dev containers required hardcoded public DNS (--dns 1.1.1.1),
# which captive networks block.
NC_IMAGE ?= localhost/town-os-networkcontroller:$(INSTANCE_ID)
INGRESS_IMAGE ?= localhost/town-os-ingress:$(INSTANCE_ID)
GFEH_IMAGE ?= localhost/town-os-gfeh:$(INSTANCE_ID)
# There is deliberately no gfeh crate version knob. Containerfile.gfeh always
# builds the current crates.io release; see the comment there for why the old
# GFEH_VERSION / GFEH_LATEST pair was removed rather than corrected.
# The networkcontroller and UI images are pulled from quay in production but
# test and dev harnesses build them locally and inject NC_IMAGE/UI_IMAGE at
# container start, so their quay tags are intentionally NOT in ALL_IMAGES.
ALL_IMAGES := $(BASE_IMAGES) docker.io/library/registry:2 docker.io/gitea/gitea:latest docker.io/library/nginx:1.27-alpine docker.io/library/alpine:latest $(MONITORING_IMAGES) $(ROLODEX_IMAGE)
export BASE_IMAGES BASE_IMAGES_RUNTIME MONITORING_IMAGES ALL_IMAGES ROLODEX_IMAGE_TAG ROLODEX_IMAGE UI_IMAGE NC_IMAGE INGRESS_IMAGE GFEH_IMAGE
export TEST_RUN TEST_TIMEOUT PUSH_TAG

.DEFAULT_GOAL := help

include make/include.mk
-include .hack/include.mk

# ---------------------------------------------------------------------------
# Dependencies
# ---------------------------------------------------------------------------

# Held in a variable rather than written straight onto `.PHONY` so the `%-log`
# pattern rule can check its stem against it (see LOGGABLE_TARGETS below) — a
# target added here keeps getting its `-log` variant for free.
PHONY_TARGETS := help deps
PHONY_TARGETS += check-go check-bun check-podman check-runc check-btrfs check-binfmt check-golangci-lint check-python3 check-libsystemd
PHONY_TARGETS += test test-race test-ui-unit test-ui-integration-local docker-login ensure-image-cache pull-images pull-images-daily
PHONY_TARGETS += ui-image nc-image nc-image-dev ingress-image gfeh-image ui-integration-image production-image test-image dev-production-image dev-image
PHONY_TARGETS += registry registry-populate registry-stop
PHONY_TARGETS += gitea gitea-populate gitea-stop
PHONY_TARGETS += test-ui-integration test-integration-build test-integration test-integration-rerun test-full
PHONY_TARGETS += dev dev-logs dev-stop dev-stop-all dev-restore-dns dev-btrfs btrfs-dev clean-btrfs-dev
PHONY_TARGETS += preflight-dev clean-dev auto-test auto-test-full build-networkcontroller lint
PHONY_TARGETS += ssh
PHONY_TARGETS += release-build release-image release-ui-image release-nc-image release-ingress-image release-gfeh-image push push-rc manifest-rc push-release manifest-release push-ui-rc push-ui-release push-nc-rc push-nc-release push-ingress-rc push-ingress-release push-gfeh-rc push-gfeh-release push-tag manifest-tag quay-login
ifeq ($(PROTON_ENABLED),1)
PHONY_TARGETS += release-proton-image push-proton-rc push-proton-release
endif
PHONY_TARGETS += btrfs clean-btrfs clean-integration clean clean-build-cache clean-cache clean-image-cache clean-bun-cache clean-containers clean-all

.PHONY: $(PHONY_TARGETS)
# NOTE: no *-log target may be listed above. `make <target>-log` is served by the
# `%-log` pattern rule in make/include.mk, and GNU make skips the
# implicit-rule search entirely for .PHONY targets — listing test-full-log here
# makes it match nothing and fail with "No rule to make target 'test-full-log'".

# What `make <target>-log` will log. The `%-log` rule matches ANY name ending in
# `-log` (a pattern rule with no prerequisites matches anything), so a stem that
# is no target here still reaches the recipe; anything not in this list is
# refused before a log file is created, rather than logged into a transcript of
# make failing to find it.
LOGGABLE_TARGETS := $(PHONY_TARGETS)

test: lint check-bun check-libsystemd
# check-bun is not listed because this target runs no UI tests, but lint does
# (eslint), so it still arrives transitively. Listing it would imply the race
# suite needs bun, which it does not.
test-race: lint check-libsystemd
test-ui-unit: check-bun
test-ui-integration-local: check-bun
$(STATE_DIR)/.images-pulled: ensure-image-cache docker-login
pull-images: check-podman check-runc docker-login quay-login
ui-image: check-podman check-runc $(STATE_DIR)/.images-pulled
nc-image: check-podman check-runc production-image
ingress-image: check-podman check-runc $(STATE_DIR)/.images-pulled
# The rust toolchain image is deliberately NOT in BASE_IMAGES: it is ~1.5G and
# only the gfeh build needs it, so pulling it on every `make test-full` would
# cost every run for one target's benefit.
gfeh-image: check-podman check-runc
nc-image-dev: check-podman check-runc dev-production-image
ui-integration-image: $(STATE_DIR)/.images-pulled
production-image: check-podman check-runc $(STATE_DIR)/.images-pulled
$(PORT_FILES): check-python3
$(STATE_DIR)/.registry-images: gitea-populate
registry: check-podman check-runc ensure-image-cache $(STATE_DIR)/.registry-port
registry-populate: registry $(STATE_DIR)/.registry-images
$(STATE_DIR)/registries.conf: $(STATE_DIR)/.registry-port
gitea: check-podman check-runc ensure-image-cache $(STATE_DIR)/.gitea-port
gitea-populate: gitea
test-ui-integration: test-image ui-image nc-image ingress-image gfeh-image ui-integration-image $(STATE_DIR)/.integration-port $(SYSTEM_PORT_FILES) registry-populate $(STATE_DIR)/registries.conf gitea-populate
test-integration-build: lint test-image ui-image nc-image ingress-image gfeh-image $(STATE_DIR)/.integration-port $(SYSTEM_PORT_FILES) registry-populate $(STATE_DIR)/registries.conf gitea-populate
test-integration: test-integration-build

# pull-images-daily deliberately has no prerequisites of its own: when the stamp
# is fresh the whole step is a stat() and a printed line, with no podman check
# and no registry login. The recursive pull-images it runs on a stale stamp
# brings those in for the pass that actually needs them.
test-full: pull-images-daily test ui-integration-image
test-image: production-image
dev-production-image: $(STATE_DIR)/.images-pulled
dev-image: dev-production-image
btrfs-dev: check-btrfs clean-btrfs-dev
dev-stop:
dev-image: dev-stop
dev: check-podman check-runc check-bun check-btrfs pull-images-daily dev-image nc-image-dev gfeh-image dev-btrfs ensure-image-cache
preflight-dev: ensure-image-cache $(STATE_DIR)/.integration-port
clean-dev: dev-stop-all clean-cache
clean-integration: registry-stop gitea-stop
# `clean` deliberately has no prerequisites: its recipe drives every other
# cleanup target through ${MAKE}, in an order make cannot be asked to guarantee
# (prerequisites of one target are unordered under -j, and .cache must go last
# because it holds the btrfs loopback backing images). See make/clean.sh.
clean-cache: dev-stop clean-btrfs-dev
# Kept as the name people and scripts already type; `clean` is now the aggregate.
clean-all: clean
release-image: check-podman check-runc $(STATE_DIR)/.images-pulled
release-ui-image: check-podman check-runc $(STATE_DIR)/.images-pulled
release-nc-image: check-podman check-runc $(STATE_DIR)/.images-pulled
release-ingress-image: check-podman check-runc $(STATE_DIR)/.images-pulled
release-gfeh-image: check-podman check-runc
ifeq ($(PROTON_ENABLED),1)
release-proton-image: check-podman check-runc $(STATE_DIR)/.images-pulled
release-build: pull-images test-full release-image release-ui-image release-proton-image release-nc-image release-ingress-image release-gfeh-image
push-rc: release-image release-ui-image release-proton-image release-nc-image release-ingress-image release-gfeh-image quay-login
push-tag: release-image release-ui-image release-proton-image release-nc-image release-ingress-image release-gfeh-image quay-login
else
release-build: pull-images test-full release-image release-ui-image release-nc-image release-ingress-image release-gfeh-image
push-rc: release-image release-ui-image release-nc-image release-ingress-image release-gfeh-image quay-login
# push-tag pushes all six images and now TAGS the systemcontroller from the
# staging image instead of building it inline, so it has to build them like
# push-rc does. It previously built only the controller and re-tagged whatever
# the other five had left in local storage — which is the shared-slot bug.
push-tag: release-image release-ui-image release-nc-image release-ingress-image release-gfeh-image quay-login
endif
# Every push-* target must depend on building the image(s) it pushes + quay-login.
push: release-build
# Manifest targets assemble remote per-arch tags; they build nothing locally.
manifest-rc: check-podman quay-login
manifest-release: check-podman quay-login
manifest-tag: check-podman quay-login
push-release: release-build quay-login
push-ui-rc: release-ui-image quay-login
push-ui-release: release-ui-image quay-login
push-nc-rc: release-nc-image quay-login
push-nc-release: release-nc-image quay-login
push-ingress-rc: release-ingress-image quay-login
push-ingress-release: release-ingress-image quay-login
push-gfeh-rc: release-gfeh-image quay-login
push-gfeh-release: release-gfeh-image quay-login
lint: check-go check-golangci-lint check-libsystemd check-bun
btrfs: check-btrfs clean-btrfs

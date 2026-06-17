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

# Host arch in registry tag form (x86_64/aarch64, the uname -m form) for
# per-arch image tags. rc tags are partitioned per arch (rc.latest-x86_64 /
# rc.latest-aarch64) and pushed natively from each host; plain rc.latest exists
# only as a multi-arch manifest list assembled by manifest-rc.
HOST_ARCH := $(shell uname -m | sed -e 's/amd64/x86_64/' -e 's/arm64/aarch64/')
# All architectures a release covers (used by manifest assembly).
ARCHES ?= x86_64 aarch64
export HOST_ARCH ARCHES

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

LOG_DIR := /tmp/town-os/log
export LOG_DIR

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
export PODMAN_IMAGE PODMAN_DEV_BASE PODMAN_TEST_IMAGE PODMAN_DEV_IMAGE PODMAN_UI_IMAGE RELEASE_IMAGE RELEASE_UI_IMAGE RELEASE_PROTON_IMAGE RELEASE_NC_IMAGE RELEASE_INGRESS_IMAGE

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

# Global image cache shared across all working trees. Override with:
#   make IMAGE_CACHE=/some/other/path test-full
IMAGE_CACHE ?= /var/cache/town-os/images
export IMAGE_CACHE

# Image lists.
BASE_IMAGES := docker.io/library/golang:1.25-bookworm docker.io/oven/bun:latest docker.io/library/debian:bookworm docker.io/library/caddy:latest
MONITORING_IMAGES := quay.io/prometheus/prometheus:latest quay.io/prometheus/node-exporter:latest docker.io/grafana/grafana:latest
# Rolodex publishes per-arch tags (rc.latest-x86_64 / rc.latest-aarch64) pushed
# natively from each host. Internal Town OS image pulls default to the host's
# per-arch rc tag (rc.latest-<arch>) so the harness, dev, and the runtime all
# track the rc channel by default. The plain rc.latest (no arch suffix) is a
# multi-arch manifest list and must never be pulled directly. Override with
# ROLODEX_IMAGE_TAG=... (e.g. latest-$(HOST_ARCH) for a released rolodex).
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
# The networkcontroller and UI images are pulled from quay in production but
# test and dev harnesses build them locally and inject NC_IMAGE/UI_IMAGE at
# container start, so their quay tags are intentionally NOT in ALL_IMAGES.
ALL_IMAGES := $(BASE_IMAGES) docker.io/library/registry:2 docker.io/gitea/gitea:latest docker.io/library/nginx:1.27-alpine docker.io/library/alpine:latest $(MONITORING_IMAGES) $(ROLODEX_IMAGE)
export BASE_IMAGES MONITORING_IMAGES ALL_IMAGES ROLODEX_IMAGE_TAG ROLODEX_IMAGE UI_IMAGE NC_IMAGE INGRESS_IMAGE
export TEST_RUN TEST_TIMEOUT PUSH_TAG

.DEFAULT_GOAL := help

include make/include.mk
-include .hack/include.mk

# ---------------------------------------------------------------------------
# Dependencies
# ---------------------------------------------------------------------------

.PHONY: help deps
.PHONY: check-go check-bun check-podman check-runc check-btrfs check-golangci-lint check-python3 check-libsystemd
.PHONY: test test-ui-unit test-ui-integration-local docker-login ensure-image-cache pull-images
.PHONY: ui-image nc-image nc-image-dev ingress-image ui-integration-image production-image test-image dev-production-image dev-image
.PHONY: registry registry-populate registry-stop
.PHONY: gitea gitea-populate gitea-stop
.PHONY: test-ui-integration test-integration-build test-integration test-integration-rerun test-full
.PHONY: dev dev-logs dev-stop dev-stop-all dev-btrfs btrfs-dev clean-btrfs-dev
.PHONY: preflight-dev clean-dev auto-test auto-test-full build-networkcontroller lint test-full-log
.PHONY: ssh
.PHONY: release-build release-image release-ui-image release-nc-image release-ingress-image push push-rc manifest-rc push-release manifest-release push-ui-rc push-ui-release push-nc-rc push-nc-release push-ingress-rc push-ingress-release push-tag quay-login
ifeq ($(PROTON_ENABLED),1)
.PHONY: release-proton-image push-proton-rc push-proton-release
endif
.PHONY: btrfs clean-btrfs clean-integration clean clean-cache clean-image-cache clean-containers clean-all

test: lint check-bun check-libsystemd
test-ui-unit: check-bun
test-ui-integration-local: check-bun
$(STATE_DIR)/.images-pulled: ensure-image-cache docker-login
pull-images: check-podman check-runc docker-login quay-login
ui-image: check-podman check-runc $(STATE_DIR)/.images-pulled
nc-image: check-podman check-runc production-image
ingress-image: check-podman check-runc $(STATE_DIR)/.images-pulled
nc-image-dev: check-podman check-runc dev-production-image
ui-integration-image: $(STATE_DIR)/.images-pulled
production-image: check-podman check-runc $(STATE_DIR)/.images-pulled
$(STATE_DIR)/.integration-port: check-python3
$(STATE_DIR)/.registry-port: check-python3
$(STATE_DIR)/.registry-images: gitea-populate
registry: check-podman check-runc ensure-image-cache $(STATE_DIR)/.registry-port
registry-populate: registry $(STATE_DIR)/.registry-images
$(STATE_DIR)/registries.conf: $(STATE_DIR)/.registry-port
$(STATE_DIR)/.gitea-port: check-python3
gitea: check-podman check-runc ensure-image-cache $(STATE_DIR)/.gitea-port
gitea-populate: gitea
test-ui-integration: test-image ui-image nc-image ingress-image ui-integration-image $(STATE_DIR)/.integration-port registry-populate $(STATE_DIR)/registries.conf gitea-populate
test-integration-build: lint test-image ui-image nc-image ingress-image $(STATE_DIR)/.integration-port registry-populate $(STATE_DIR)/registries.conf gitea-populate
test-integration: test-integration-build
test-full: test ui-integration-image
test-full-log:
test-image: production-image
dev-production-image: $(STATE_DIR)/.images-pulled
dev-image: dev-production-image
btrfs-dev: check-btrfs clean-btrfs-dev
dev-stop:
dev-image: dev-stop
dev: check-podman check-runc check-bun check-btrfs dev-image nc-image-dev dev-btrfs ensure-image-cache
preflight-dev: ensure-image-cache $(STATE_DIR)/.integration-port
clean-dev: dev-stop-all clean-cache
clean-integration: registry-stop gitea-stop
clean: clean-cache
clean-cache: dev-stop clean-btrfs-dev
clean-all: clean-containers clean clean-dev clean-integration clean-btrfs
release-image: check-podman check-runc $(STATE_DIR)/.images-pulled
release-ui-image: check-podman check-runc $(STATE_DIR)/.images-pulled
release-nc-image: check-podman check-runc $(STATE_DIR)/.images-pulled
release-ingress-image: check-podman check-runc $(STATE_DIR)/.images-pulled
ifeq ($(PROTON_ENABLED),1)
release-proton-image: check-podman check-runc $(STATE_DIR)/.images-pulled
release-build: pull-images test-full release-image release-ui-image release-proton-image release-nc-image release-ingress-image
push-rc: release-image release-ui-image release-proton-image release-nc-image release-ingress-image quay-login
else
release-build: pull-images test-full release-image release-ui-image release-nc-image release-ingress-image
push-rc: release-image release-ui-image release-nc-image release-ingress-image quay-login
endif
# Every push-* target must depend on building the image(s) it pushes + quay-login.
push: release-build
# Manifest targets assemble remote per-arch tags; they build nothing locally.
manifest-rc: check-podman quay-login
manifest-release: check-podman quay-login
push-release: release-build quay-login
push-ui-rc: release-ui-image quay-login
push-ui-release: release-ui-image quay-login
push-nc-rc: release-nc-image quay-login
push-nc-release: release-nc-image quay-login
push-ingress-rc: release-ingress-image quay-login
push-ingress-release: release-ingress-image quay-login
lint: check-go check-golangci-lint check-libsystemd check-bun
btrfs: check-btrfs clean-btrfs

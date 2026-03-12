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

TOWN_OS_TAG ?= rc.latest
export TOWN_OS_TAG

# Unique instance ID from working directory path.
INSTANCE_ID := $(shell echo -n "$(CURDIR)" | md5sum | cut -c1-8)
export INSTANCE_ID

# Ephemeral state directory — all port files, btrfs volumes, dev data, and
# per-run artifacts live here instead of the working directory.
STATE_DIR := /tmp/town-os-$(INSTANCE_ID)
export STATE_DIR

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
export PODMAN_IMAGE PODMAN_DEV_BASE PODMAN_TEST_IMAGE PODMAN_DEV_IMAGE PODMAN_UI_IMAGE RELEASE_IMAGE RELEASE_UI_IMAGE RELEASE_PROTON_IMAGE

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
BASE_IMAGES := docker.io/library/golang:1.25-bookworm docker.io/oven/bun:latest docker.io/library/debian:bookworm-slim docker.io/library/caddy:latest
MONITORING_IMAGES := quay.io/prometheus/prometheus:latest quay.io/prometheus/node-exporter:latest docker.io/grafana/grafana:latest
ROLODEX_IMAGE_TAG ?= rc.latest
ROLODEX_IMAGE := quay.io/town/rolodex:$(ROLODEX_IMAGE_TAG)
UI_IMAGE_TAG ?= rc.latest
UI_IMAGE := quay.io/town/ui:$(UI_IMAGE_TAG)
ALL_IMAGES := $(BASE_IMAGES) docker.io/library/registry:2 docker.io/gitea/gitea:latest docker.io/library/nginx:1.27-alpine $(MONITORING_IMAGES) $(ROLODEX_IMAGE) $(UI_IMAGE)
export BASE_IMAGES MONITORING_IMAGES ALL_IMAGES ROLODEX_IMAGE_TAG ROLODEX_IMAGE UI_IMAGE_TAG UI_IMAGE
export TEST_RUN TEST_TIMEOUT

include make/include.mk
-include .hack/include.mk

# ---------------------------------------------------------------------------
# Dependencies
# ---------------------------------------------------------------------------

.PHONY: check-go check-bun check-podman check-runc check-btrfs check-golangci-lint check-python3 check-libsystemd
.PHONY: test test-ui-unit test-ui-integration-local docker-login ensure-image-cache pull-images
.PHONY: ui-integration-image production-image test-image dev-production-image dev-image
.PHONY: registry registry-populate registry-stop
.PHONY: gitea gitea-populate gitea-stop
.PHONY: test-ui-integration test-integration-build test-integration test-integration-rerun test-full
.PHONY: dev dev-logs dev-stop dev-stop-all dev-btrfs btrfs-dev clean-btrfs-dev
.PHONY: preflight-dev clean-dev auto-test auto-test-full build-networkcontroller lint
.PHONY: ssh
.PHONY: release-build release-image release-ui-image release-proton-image push push-rc push-release push-ui-rc push-ui-release push-proton-rc push-proton-release quay-login
.PHONY: btrfs clean-btrfs clean-integration clean clean-cache clean-image-cache clean-containers clean-all

test: lint check-bun check-libsystemd
test-ui-unit: check-bun
test-ui-integration-local: check-bun
$(STATE_DIR)/.images-pulled: ensure-image-cache docker-login
pull-images: check-podman check-runc docker-login quay-login
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
test-ui-integration: test-image ui-integration-image $(STATE_DIR)/.integration-port registry-populate $(STATE_DIR)/registries.conf gitea-populate
test-integration-build: lint test-image $(STATE_DIR)/.integration-port registry-populate $(STATE_DIR)/registries.conf gitea-populate
test-integration: test-integration-build
test-full: test
test-image: production-image
dev-production-image: $(STATE_DIR)/.images-pulled
dev-image: dev-production-image
btrfs-dev: check-btrfs clean-btrfs-dev
dev: check-podman check-runc check-bun check-btrfs dev-image dev-btrfs ensure-image-cache
preflight-dev: ensure-image-cache $(STATE_DIR)/.integration-port
clean-dev: dev-stop-all clean-cache
clean-integration: registry-stop gitea-stop
clean: clean-cache
clean-cache: dev-stop clean-btrfs-dev
clean-all: clean-containers clean clean-dev clean-integration clean-btrfs
release-image: check-podman check-runc $(STATE_DIR)/.images-pulled
release-ui-image: check-podman check-runc $(STATE_DIR)/.images-pulled
release-build: pull-images test-full release-image release-ui-image release-proton-image
# Every push-* target must depend on building the image(s) it pushes + quay-login.
push: release-build
push-rc: release-image release-ui-image release-proton-image quay-login
push-release: release-build quay-login
push-ui-rc: release-ui-image quay-login
push-ui-release: release-ui-image quay-login
lint: check-go check-golangci-lint check-libsystemd
btrfs: check-btrfs clean-btrfs

# make/include.mk - Target recipes.
# Dependencies are defined in the top-level Makefile.

check-go:
	@make/check.sh go

check-bun:
	@make/check.sh bun

check-podman:
	@make/check.sh podman

check-runc:
	@make/check.sh runc

check-btrfs:
	@make/check.sh btrfs

check-golangci-lint:
	@make/check.sh golangci-lint

check-python3:
	@make/check.sh python3

check-libsystemd:
	@make/check.sh libsystemd

test:
	@make/test.sh unit

docker-login:
	@make/images.sh docker-login

quay-login:
	@make/images.sh quay-login

ensure-image-cache:
	@make/images.sh ensure-cache

.cache/.images-pulled:
	@make/images.sh load-base

pull-images:
	@make/images.sh pull

ui-integration-image:
	@make/build.sh ui-integration

production-image:
	@make/build.sh production

.integration-port:
	@make/port.sh .integration-port

.registry-port:
	@make/port.sh .registry-port

.gitea-port:
	@make/port.sh .gitea-port

.cache/.registry-images:
	@make/registry.sh discover-images

registry:
	@make/registry.sh start

registry-populate:
	@make/registry.sh populate

.cache/registries.conf:
	@make/registry.sh gen-config

registry-stop:
	@make/registry.sh stop

gitea:
	@make/gitea.sh start

gitea-populate:
	@make/gitea.sh populate

gitea-stop:
	@make/gitea.sh stop

test-ui-integration:
	@make/test.sh ui-integration

test-integration:
	@make/test.sh integration

test-full:
	@make/test.sh full

test-image:
	@make/build.sh test

dev-production-image:
	@make/build.sh dev-base

dev-image:
	@make/build.sh dev

dev-logs:
	@make/dev.sh logs

btrfs-dev:
	@make/btrfs.sh create-dev

clean-btrfs-dev:
	@make/btrfs.sh clean-dev

dev-btrfs:
	@make/btrfs.sh ensure-dev

dev:
	@make/dev.sh start

preflight-dev:
	@make/preflight.sh

dev-stop:
	@make/dev.sh stop

dev-stop-all:
	@make/dev.sh stop-all

auto-test:
	@make/test.sh auto

auto-test-full:
	@make/test.sh auto-full

build-networkcontroller:
	@make/build.sh networkcontroller

release-image:
	@make/build.sh release

release-ui-image:
	@make/build.sh release-ui

push:
	@$(MAKE) push-rc

push-rc:
	@make/build.sh push-rc

push-release:
	@make/build.sh push-release

push-ui-rc:
	@make/build.sh push-ui-rc

push-ui-release:
	@make/build.sh push-ui-release

lint:
	@make/lint.sh

btrfs:
	@make/btrfs.sh create

clean-btrfs:
	@make/btrfs.sh clean

clean-integration:
	@make/clean.sh integration

clean:
	@make/clean.sh main

clean-cache:
	@make/clean.sh cache

clean-image-cache:
	@make/clean.sh image-cache

clean-containers:
	@make/clean.sh containers

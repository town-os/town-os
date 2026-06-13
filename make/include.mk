# make/include.mk - Target recipes.
# Dependencies are defined in the top-level Makefile.

help:
	@make/help.sh

deps:
	@make/deps.sh

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

$(STATE_DIR)/.images-pulled:
	@make/images.sh load-base

pull-images:
	@make/images.sh pull

ui-image:
	@make/build.sh ui-local

nc-image:
	@make/build.sh nc-local

nc-image-dev:
	@make/build.sh nc-local $(PODMAN_DEV_BASE)

ui-integration-image:
	@make/build.sh ui-integration

production-image:
	@make/build.sh production

$(STATE_DIR)/.integration-port:
	@make/port.sh $(STATE_DIR)/.integration-port

$(STATE_DIR)/.registry-port:
	@make/port.sh $(STATE_DIR)/.registry-port

$(STATE_DIR)/.gitea-port:
	@make/port.sh $(STATE_DIR)/.gitea-port

$(STATE_DIR)/.registry-images:
	@make/registry.sh discover-images

registry:
	@make/registry.sh start

registry-populate:
	@make/registry.sh populate

$(STATE_DIR)/registries.conf:
	@make/registry.sh gen-config

registry-stop:
	@make/registry.sh stop

gitea:
	@make/gitea.sh start

gitea-populate:
	@make/gitea.sh populate

gitea-stop:
	@make/gitea.sh stop

test-ui-unit:
	@make/test.sh ui-unit

test-ui-integration-local:
	@make/test.sh ui-integration-local

test-ui-integration:
	@make/test.sh ui-integration

test-integration-build:
	@make/test.sh integration-build

test-integration:
	@make/test.sh integration

test-integration-rerun:
	@make/test.sh integration-rerun

test-full:
	@make/test.sh full

test-full-log:
	@bash -c 'set -o pipefail; mkdir -p "$(LOG_DIR)"; logfile="$(LOG_DIR)/test-full-$$(date +%s).log"; echo "Logging to: $$logfile"; rc=0; $(MAKE) test-full 2>&1 | tee "$$logfile" || rc=$$?; echo "Log file: $$logfile"; exit $$rc'

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

ifeq ($(PROTON_ENABLED),1)
release-proton-image:
	@make/build.sh release-proton
endif

release-nc-image:
	@make/build.sh release-nc

push:
	@$(MAKE) push-rc

push-rc:
	@make/build.sh push-rc

manifest-rc:
	@make/build.sh manifest-rc

push-release:
	@make/build.sh push-release

manifest-release:
	@make/build.sh manifest-release

push-ui-rc:
	@make/build.sh push-ui-rc

push-ui-release:
	@make/build.sh push-ui-release

ifeq ($(PROTON_ENABLED),1)
push-proton-rc: release-proton-image quay-login
	@make/build.sh push-proton-rc

push-proton-release: release-proton-image quay-login
	@make/build.sh push-proton-release
endif

push-nc-rc: release-nc-image quay-login
	@make/build.sh push-nc-rc

push-nc-release: release-nc-image quay-login
	@make/build.sh push-nc-release

ifeq ($(PROTON_ENABLED),1)
push-tag: release-image release-ui-image release-proton-image release-nc-image quay-login
	@make/build.sh push-tag $(PUSH_TAG)
else
push-tag: release-image release-ui-image release-nc-image quay-login
	@make/build.sh push-tag $(PUSH_TAG)
endif

ssh:
	@ssh-keygen -R town-os.local 2>/dev/null; true
	sshpass -p enjoytownos ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null root@town-os.local

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

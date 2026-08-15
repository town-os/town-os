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

check-binfmt:
	@make/check.sh binfmt

check-golangci-lint:
	@make/check.sh golangci-lint

check-python3:
	@make/check.sh python3

check-libsystemd:
	@make/check.sh libsystemd

test:
	@make/test.sh unit

test-race:
	@make/test.sh unit-race

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

pull-images-daily:
	@make/images.sh pull-daily

ui-image:
	@make/build.sh ui-local

nc-image:
	@make/build.sh nc-local

nc-image-dev:
	@make/build.sh nc-local $(PODMAN_DEV_BASE)

ingress-image:
	@make/build.sh ingress-local

gfeh-image:
	@make/build.sh gfeh-local

ui-integration-image:
	@make/build.sh ui-integration

production-image:
	@make/build.sh production

# Every per-run ephemeral port file is allocated the same way, so one pattern
# rule covers them all: .integration-port, .registry-port, .gitea-port, and the
# system-service ports (.dns-port, .node-exporter-port, .prometheus-port,
# .monitoring-port, .ingress-https-port, .ingress-http-port,
# .ingress-metrics-port) that keep a test
# box from colliding with a dev box in the shared host netns — IRON RULE.
$(STATE_DIR)/.%-port:
	@make/port.sh $@

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

# `make <target>-log` runs `make <target>` with the whole transcript tee'd into a
# timestamped file under $(LOG_DIR) — `make test-full-log`, `make push-rc-log`,
# `make dev-log`. A pattern rule, so a new target gets its logged variant for
# free; it replaces the single hand-written test-full-log, which was the only
# one that existed and left `make push-rc-log` failing with "No rule to make
# target".
#
# The log is always written even when the build FAILS, which is the case that
# matters: `set -o pipefail` makes the pipeline carry make's exit status rather
# than tee's, so it is captured in $$rc, the path is printed, and only then is
# the failure re-raised — tee has already flushed the full transcript by then.
#
# The name carries BUILD_ARCH so a cross build and a native build of the same
# target don't leave two indistinguishable logs, and the shell's PID so two
# concurrent runs in one checkout cannot tee into the same file. That second one
# is the IRON RULE: INSTANCE_ID is derived from CURDIR and so is identical for
# concurrent runs here, and the timestamp only resolves to the second, so
# without the PID two `make test-full-log` runs started together would truncate
# each other's transcript.
#
# The timestamp is sortable rather than epoch seconds so `ls` orders the runs,
# and a `<target>-<arch>-latest.log` symlink tracks the newest. That symlink is
# placed BEFORE the build runs, not after: its whole purpose is `tail -F` from
# another terminal while the build is still going. tee creates the file
# immediately, so there is nothing to race.
#
# Pattern rules are skipped for .PHONY targets, so nothing matched by this may be
# listed there — see the NOTE next to PHONY_TARGETS in the Makefile.
#
# The stem is checked against $(LOGGABLE_TARGETS) FIRST, before the log file or
# the -latest symlink are created — this is the Makefile's only wildcard and
# matches ANY name ending in `-log`, so a stem that is no target here reaches the
# recipe too. Such a stem exits 2 pointing at `make help`, and leaves nothing
# behind, rather than creating a log and printing two lines that read like a run
# that had started. Pointing at `make help` is the whole reason this doesn't just
# forward the stem to a sub-make: make's own "No rule to make target" names the
# STEM, not what was typed, and says nothing about where the real target list is.
%-log:
	@bash -c 'set -o pipefail; \
	  if [ -z "$(filter $*,$(LOGGABLE_TARGETS))" ]; then \
	    echo "make: $* is not a target here, so there is nothing for $*-log to log." >&2; \
	    echo "make: run \"make help\" for the targets, loggable ones included." >&2; \
	    exit 2; \
	  fi; \
	  mkdir -p "$(LOG_DIR)"; \
	  stem="$*-$(BUILD_ARCH)"; \
	  logfile="$(LOG_DIR)/$$stem-$$(date +%Y%m%d-%H%M%S)-$$$$.log"; \
	  : > "$$logfile"; \
	  ln -sfn "$$logfile" "$(LOG_DIR)/$$stem-latest.log"; \
	  echo "Logging to: $$logfile"; \
	  echo "Follow it with: tail -F $(LOG_DIR)/$$stem-latest.log"; \
	  rc=0; $(MAKE) $* 2>&1 | tee "$$logfile" || rc=$$?; \
	  echo "Log file: $$logfile"; \
	  exit $$rc'

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

dev-restore-dns:
	@make/dev.sh restore-dns

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

release-ingress-image:
	@make/build.sh release-ingress

release-gfeh-image:
	@make/build.sh release-gfeh

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

push-ingress-rc: release-ingress-image quay-login
	@make/build.sh push-ingress-rc

push-ingress-release: release-ingress-image quay-login
	@make/build.sh push-ingress-release

push-gfeh-rc: release-gfeh-image quay-login
	@make/build.sh push-gfeh-rc

push-gfeh-release: release-gfeh-image quay-login
	@make/build.sh push-gfeh-release

ifeq ($(PROTON_ENABLED),1)
push-tag: release-image release-ui-image release-proton-image release-nc-image release-ingress-image release-gfeh-image quay-login
	@make/build.sh push-tag $(PUSH_TAG)
else
push-tag: release-image release-ui-image release-nc-image release-ingress-image release-gfeh-image quay-login
	@make/build.sh push-tag $(PUSH_TAG)
endif

# Assembles the plain $(PUSH_TAG) from the $(PUSH_TAG)-<arch> tags push-tag
# leaves behind. No build prerequisites, like manifest-rc and manifest-release:
# it only reads tags that are already in the registry, and it must be run once
# after every architecture has pushed rather than once per architecture.
manifest-tag:
	@make/build.sh manifest-tag $(PUSH_TAG)

ssh:
	@ssh-keygen -R town-os.local 2>/dev/null; true
	sshpass -p enjoytownos ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null root@town-os.local

lint:
	@make/lint.sh

tidy:
	@make/tidy.sh

btrfs:
	@make/btrfs.sh create

clean-btrfs:
	@make/btrfs.sh clean

clean-integration:
	@make/clean.sh integration

# The one target that leaves nothing behind: containers, both btrfs loopbacks,
# the dev data, the image and bun caches, and the rest of .cache — which it verifies
# is actually gone rather than assuming. See make/clean.sh for why the order
# matters. clean-build-cache is the narrower behaviour this target used to have.
clean:
	@make/clean.sh all

clean-build-cache:
	@make/clean.sh build-cache

clean-cache:
	@make/clean.sh cache

clean-image-cache:
	@make/clean.sh image-cache

clean-bun-cache:
	@make/clean.sh bun-cache

clean-containers:
	@make/clean.sh containers

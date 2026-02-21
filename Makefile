-include .env
export TOWN_OS_REPO_USERNAME
export TOWN_OS_REPO_PASSWORD

PODMAN_IMAGE := town-os
PODMAN_TEST_IMAGE := town-os-test
PODMAN_CONTAINER := town-os-test

test: lint
	go test -v ./src/...
	cd ui && bun install && bun run test

PODMAN_UI_IMAGE := town-os-ui-integration
PODMAN_UI_CONTAINER := town-os-ui-integration
PODMAN_UI_BACKEND := town-os-ui-backend

.cache/.images-pulled:
	sudo -E podman pull golang:1.25-bookworm
	sudo -E podman pull oven/bun:latest
	sudo -E podman pull debian:bookworm-slim
	@mkdir -p .cache
	@touch .cache/.images-pulled

pull-images:
	sudo -E podman pull docker.io/library/golang:1.25-bookworm
	sudo -E podman pull docker.io/oven/bun:latest
	sudo -E podman pull docker.io/library/debian:bookworm-slim
	@mkdir -p .cache
	@touch .cache/.images-pulled

ui-integration-image: .cache/.images-pulled
	sudo -E podman build --pull=never \
		-t $(PODMAN_UI_IMAGE) -f integration/testdata/Containerfile.ui-integration .

production-image: .cache/.images-pulled
	mkdir -p .cache/go-mod .cache/go-build
	sudo -E podman build --pull=never \
		--volume $$(pwd)/.cache/go-mod:/go/pkg/mod:z \
		--volume $$(pwd)/.cache/go-build:/root/.cache/go-build:z \
		-t $(PODMAN_IMAGE) -f Containerfile .

test-ui-integration: test-image ui-integration-image btrfs
	@sudo -E podman rm -f $(PODMAN_UI_CONTAINER)
	@sudo -E podman rm -f $(PODMAN_UI_BACKEND)
	sudo -E podman run -e LOG_LEVEL=debug -e DEBUG=1 \
		-e TOWN_OS_REPO_USERNAME=$(TOWN_OS_REPO_USERNAME) \
		-e TOWN_OS_REPO_PASSWORD=$(TOWN_OS_REPO_PASSWORD) \
		-d --systemd=true --privileged \
		--device /dev/btrfs-control:/dev/btrfs-control:rwm \
		-v $$(cat town-os.mount):/data/btrfs:z \
		--name=$(PODMAN_UI_BACKEND) $(PODMAN_TEST_IMAGE)
	@echo "Waiting for systemd to be ready..."
	@for i in $$(seq 1 30); do \
		sudo -E podman exec $(PODMAN_UI_BACKEND) test -S /var/run/dbus/system_bus_socket 2>/dev/null && break; \
		sleep 1; \
	done
	sudo -E podman run \
		--network container:$(PODMAN_UI_BACKEND) \
		-e INTEGRATION_URL=http://localhost:5309 \
		-e VITE_API_URL=http://localhost:5309 \
		-e TOWN_OS_REPO_USERNAME=$(TOWN_OS_REPO_USERNAME) \
		-e TOWN_OS_REPO_PASSWORD=$(TOWN_OS_REPO_PASSWORD) \
		--name $(PODMAN_UI_CONTAINER) $(PODMAN_UI_IMAGE) \
		bun run test:integration -- --reporter=verbose

test-integration: lint test-image btrfs
	@sudo -E podman rm -f $(PODMAN_CONTAINER)
	sudo -E podman run -e LOG_LEVEL=${LOG_LEVEL} \
		-e TOWN_OS_REPO_USERNAME=$(TOWN_OS_REPO_USERNAME) \
		-e TOWN_OS_REPO_PASSWORD=$(TOWN_OS_REPO_PASSWORD) \
		-d --systemd=true --privileged \
		--device /dev/btrfs-control:/dev/btrfs-control:rwm \
		-v $$(cat town-os.mount):/data/btrfs:z \
		--name=$(PODMAN_CONTAINER) $(PODMAN_TEST_IMAGE)
	@echo "Waiting for systemd to be ready..."
	@for i in $$(seq 1 30); do \
		sudo -E podman exec $(PODMAN_CONTAINER) test -S /var/run/dbus/system_bus_socket 2>/dev/null && break; \
		sleep 1; \
	done
	@sudo -E podman exec -w /test $(PODMAN_CONTAINER) /integration-test -test.v

test-full: test test-integration test-ui-integration

test-image: production-image
	mkdir -p .cache/go-mod .cache/go-build
	sudo -E podman build --pull=never \
		--volume $$(pwd)/.cache/go-mod:/go/pkg/mod:z \
		--volume $$(pwd)/.cache/go-build:/root/.cache/go-build:z \
		-t $(PODMAN_TEST_IMAGE) -f integration/testdata/Containerfile.systemd .

PODMAN_DEV_CONTAINER := town-os-dev
DEV_BTRFS_IMAGE ?= $(shell mktemp btrfs-dev.XXXXXX)

dev-logs:
	sudo podman exec -it $(PODMAN_DEV_CONTAINER) journalctl -f

btrfs-dev: clean-btrfs-dev
	echo $(DEV_BTRFS_IMAGE) >town-os-dev.disk
	truncate -s 50G $$(cat town-os-dev.disk)
	mkfs.btrfs -f $$(cat town-os-dev.disk)
	sudo -E losetup -f $$(cat town-os-dev.disk)
	sudo -E losetup -j $$(cat town-os-dev.disk) | awk -F: '{ print $$1 }' > town-os-dev.loop
	mktemp -d > town-os-dev.mount
	sudo -E mount -t btrfs $$(cat town-os-dev.loop) $$(cat town-os-dev.mount)

clean-btrfs-dev:
	@if [ -f town-os-dev.mount ]; then \
		sudo -E umount $$(cat town-os-dev.mount) 2>/dev/null || true; \
		rmdir $$(cat town-os-dev.mount) 2>/dev/null || true; \
	fi
	@if [ -f town-os-dev.disk ]; then \
		sudo -E losetup -j $$(cat town-os-dev.disk) | awk -F: '{ print $$1 }' | xargs -I{} sudo -E losetup -d {} 2>/dev/null || true; \
	fi
	rm -f btrfs-dev.* town-os-dev.disk town-os-dev.loop town-os-dev.mount

dev-btrfs:
	@if [ ! -f town-os-dev.mount ] || ! mountpoint -q $$(cat town-os-dev.mount) 2>/dev/null; then \
		$(MAKE) btrfs-dev; \
	fi

dev: test-image dev-btrfs
	@sudo -E podman rm -f $(PODMAN_DEV_CONTAINER)
	@mkdir -p dev-data dev-repos
	sudo -E podman run -d -p 0.0.0.0:5309:5309 -e LOG_LEVEL=debug -e DEBUG=1 \
		-e TOWN_OS_REPO_USERNAME=$(TOWN_OS_REPO_USERNAME) \
		-e TOWN_OS_REPO_PASSWORD=$(TOWN_OS_REPO_PASSWORD) \
		--systemd=true --privileged \
		--device /dev/btrfs-control:/dev/btrfs-control:rwm \
		-v $$(cat town-os-dev.mount):/data/btrfs:z \
		-v $$(pwd)/dev-data:/data/db:z \
		-v $$(pwd)/dev-repos:/data/repos:z \
		--name $(PODMAN_DEV_CONTAINER) $(PODMAN_TEST_IMAGE)
	@echo "API server: http://$$(hostname):5309"
	cd ui && bun install && VITE_API_URL=http://$$(hostname):5309 bun run dev -- --host; \
		sudo -E podman rm -f $(PODMAN_DEV_CONTAINER)

dev-clean: dev-stop clean-btrfs-dev
	@sudo rm -rf dev-data dev-repos

dev-stop:
	@sudo -E podman rm -f $(PODMAN_DEV_CONTAINER)

auto-test:
	go get github.com/cespare/reflex@latest
	reflex -r '\.(js|go)$$' make test

auto-test-full:
	go get github.com/cespare/reflex@latest
	sudo -E -E $(shell go env GOPATH)/bin/reflex -r '\.(go|js)$$' make test-full

lint:
	go vet ./...
	go vet -tags=podman ./...
	$(shell go env GOPATH)/bin/golangci-lint run

BTRFS_IMAGE ?= $(shell mktemp btrfs.XXXXXX)

btrfs: clean-btrfs
	echo $(BTRFS_IMAGE) >town-os.disk
	truncate -s 50G $$(cat town-os.disk)
	mkfs.btrfs -f $$(cat town-os.disk)
	sudo -E losetup -f $$(cat town-os.disk)
	sudo -E losetup -j $$(cat town-os.disk) | awk -F: '{ print $$1 }' > town-os.loop
	mktemp -d > town-os.mount
	sudo -E mount -t btrfs $$(cat town-os.loop) $$(cat town-os.mount)

clean-btrfs:
	@if [ -f town-os.mount ]; then \
		sudo -E umount $$(cat town-os.mount) 2>/dev/null || true; \
		rmdir $$(cat town-os.mount) 2>/dev/null || true; \
	fi
	@if [ -f town-os.disk ]; then \
		sudo -E losetup -j $$(cat town-os.disk) | awk -F: '{ print $$1 }' | xargs -I{} sudo -E losetup -d {} 2>/dev/null || true; \
	fi
	rm -f btrfs.* town-os.disk town-os.loop town-os.mount

clean-integration:
	@sudo -E podman rm -f $(PODMAN_CONTAINER)
	@sudo -E podman rm -f $(PODMAN_UI_BACKEND)
	@sudo -E podman rm -f $(PODMAN_UI_CONTAINER)

clean: clean-podman
	rm -rf dev-data
	rm -rf .cache

clean-podman: clean-btrfs clean-btrfs-dev
	@sudo -E podman rm -f $(PODMAN_CONTAINER)
	@sudo -E podman rm -f $(PODMAN_UI_BACKEND)
	@sudo -E podman rm -f $(PODMAN_UI_CONTAINER)
	@sudo -E podman rm -f $(PODMAN_DEV_CONTAINER)

test-systemd: test-image btrfs
	@sudo -E podman rm -f $(PODMAN_CONTAINER)
	sudo -E podman run -e LOG_LEVEL=debug \
		-e TOWN_OS_REPO_USERNAME=$(TOWN_OS_REPO_USERNAME) \
		-e TOWN_OS_REPO_PASSWORD=$(TOWN_OS_REPO_PASSWORD) \
		-d --systemd=true --privileged \
		--device /dev/btrfs-control:/dev/btrfs-control:rwm \
		-v $$(cat town-os.mount):/data/btrfs:z \
		--name=$(PODMAN_CONTAINER) $(PODMAN_TEST_IMAGE)
	@echo "Waiting for systemd to be ready..."
	@for i in $$(seq 1 30); do \
		sudo -E podman exec $(PODMAN_CONTAINER) test -S /var/run/dbus/system_bus_socket 2>/dev/null && break; \
		sleep 1; \
	done
	sudo -E podman exec -w /test $(PODMAN_CONTAINER) /integration-test -test.v -test.run TestPodman

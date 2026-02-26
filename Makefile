-include .env
export TOWN_OS_REPO_USERNAME
export TOWN_OS_REPO_PASSWORD
export DOCKER_USERNAME
export DOCKER_PASSWORD

# Unique instance ID from working directory path.
INSTANCE_ID := $(shell echo -n "$(CURDIR)" | md5sum | cut -c1-8)

# Image names (unique per working directory).
# Integration and dev use separate production base images so builds
# cannot interfere with each other.
PODMAN_IMAGE         := town-os-$(INSTANCE_ID)
PODMAN_DEV_BASE      := town-os-dev-base-$(INSTANCE_ID)
PODMAN_TEST_IMAGE    := town-os-test-$(INSTANCE_ID)
PODMAN_DEV_IMAGE     := town-os-dev-$(INSTANCE_ID)
PODMAN_UI_IMAGE      := town-os-ui-integration-$(INSTANCE_ID)

# Container names (unique per working directory).
PODMAN_CONTAINER     := town-os-test-$(INSTANCE_ID)
PODMAN_UI_CONTAINER  := town-os-ui-integration-$(INSTANCE_ID)
PODMAN_UI_BACKEND    := town-os-ui-backend-$(INSTANCE_ID)
PODMAN_DEV_CONTAINER := town-os-dev
PREFLIGHT_CONTAINER  := preflight-test-$(INSTANCE_ID)

test: lint
	go test -v -timeout 60m ./src/...
	cd ui && bun install && bun run test

docker-login:
	@if [ -n "$(DOCKER_USERNAME)" ] && [ -n "$(DOCKER_PASSWORD)" ]; then \
		echo "$(DOCKER_PASSWORD)" | sudo -E podman login -u "$(DOCKER_USERNAME)" --password-stdin docker.io; \
	fi

.cache/.images-pulled: docker-login
	sudo -E podman pull golang:1.25-bookworm
	sudo -E podman pull oven/bun:latest
	sudo -E podman pull debian:bookworm-slim
	@mkdir -p .cache
	@touch .cache/.images-pulled

pull-images: docker-login
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

# Allocate a random available port and write it to .integration-port.
# Uses Python to bind port 0 (kernel picks a free port), then records it.
.integration-port:
	@python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1]); s.close()' > $@
	@echo "Integration port: $$(cat $@)"

test-ui-integration: test-image ui-integration-image btrfs .integration-port
	@sudo -E podman rm -f $(PODMAN_UI_CONTAINER)
	@sudo -E podman rm -f $(PODMAN_UI_BACKEND)
	sudo -E podman run -e LOG_LEVEL=debug -e DEBUG=1 -e TOWN_OS_TEST=1 \
		-e TOWN_OS_REPO_USERNAME=$(TOWN_OS_REPO_USERNAME) \
		-e TOWN_OS_REPO_PASSWORD=$(TOWN_OS_REPO_PASSWORD) \
		-e TOWN_OS_LISTEN=:$$(cat .integration-port) \
		-d --net host --systemd=true --privileged \
		--device /dev/btrfs-control:/dev/btrfs-control:rwm \
		-v $$(cat town-os.mount):/data/btrfs:z \
		-v /run/containers/0/auth.json:/run/containers/0/auth.json:ro,z \
		--name=$(PODMAN_UI_BACKEND) $(PODMAN_TEST_IMAGE)
	@echo "Waiting for systemd to be ready..."
	@for i in $$(seq 1 30); do \
		sudo -E podman exec $(PODMAN_UI_BACKEND) test -S /var/run/dbus/system_bus_socket 2>/dev/null && break; \
		sleep 1; \
	done
	@echo "Waiting for systemcontroller API to be ready..."
	@for i in $$(seq 1 30); do \
		curl -sf http://localhost:$$(cat .integration-port)/ >/dev/null 2>&1 && break; \
		sleep 1; \
	done
	sudo -E podman run \
		--net host \
		-e INTEGRATION_URL=http://localhost:$$(cat .integration-port) \
		-e VITE_API_URL=http://localhost:$$(cat .integration-port) \
		-e TOWN_OS_REPO_USERNAME=$(TOWN_OS_REPO_USERNAME) \
		-e TOWN_OS_REPO_PASSWORD=$(TOWN_OS_REPO_PASSWORD) \
		--name $(PODMAN_UI_CONTAINER) $(PODMAN_UI_IMAGE) \
		bun run test:integration -- --reporter=verbose

test-integration: lint test-image btrfs .integration-port
	@sudo -E podman rm -f $(PODMAN_CONTAINER)
	sudo -E podman run -e LOG_LEVEL=${LOG_LEVEL} -e TOWN_OS_TEST=1 \
		-e TOWN_OS_REPO_USERNAME=$(TOWN_OS_REPO_USERNAME) \
		-e TOWN_OS_REPO_PASSWORD=$(TOWN_OS_REPO_PASSWORD) \
		-e TOWN_OS_LISTEN=:$$(cat .integration-port) \
		-d --net host --systemd=true --privileged \
		--device /dev/btrfs-control:/dev/btrfs-control:rwm \
		-v $$(cat town-os.mount):/data/btrfs:z \
		-v /run/containers/0/auth.json:/run/containers/0/auth.json:ro,z \
		--name=$(PODMAN_CONTAINER) $(PODMAN_TEST_IMAGE)
	@echo "Waiting for systemd to be ready..."
	@for i in $$(seq 1 30); do \
		sudo -E podman exec $(PODMAN_CONTAINER) test -S /var/run/dbus/system_bus_socket 2>/dev/null && break; \
		sleep 1; \
	done
	@sudo -E podman exec -w /test $(PODMAN_CONTAINER) /integration-test -test.v -test.timeout 60m

test-full: test test-integration test-ui-integration

test-image: production-image
	mkdir -p .cache/go-mod .cache/go-build
	sudo -E podman build --pull=never \
		--build-arg TOWN_OS_IMAGE=$(PODMAN_IMAGE) \
		--volume $$(pwd)/.cache/go-mod:/go/pkg/mod:z \
		--volume $$(pwd)/.cache/go-build:/root/.cache/go-build:z \
		-t $(PODMAN_TEST_IMAGE) -f integration/testdata/Containerfile.systemd .

dev-production-image: .cache/.images-pulled
	mkdir -p .cache/dev-go-mod .cache/dev-go-build
	sudo -E podman build --pull=never \
		--volume $$(pwd)/.cache/dev-go-mod:/go/pkg/mod:z \
		--volume $$(pwd)/.cache/dev-go-build:/root/.cache/go-build:z \
		-t $(PODMAN_DEV_BASE) -f Containerfile .

dev-image: dev-production-image
	sudo -E podman build --pull=never \
		--build-arg TOWN_OS_IMAGE=$(PODMAN_DEV_BASE) \
		-t $(PODMAN_DEV_IMAGE) -f integration/testdata/Containerfile.dev .

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

dev: dev-image dev-btrfs
	@sudo -E podman rm -f $(PODMAN_DEV_CONTAINER)
	@mkdir -p dev-data dev-repos
	sudo -E podman run -d --net host -e LOG_LEVEL=debug -e DEBUG=1 \
		-e TOWN_OS_REPO_USERNAME=$(TOWN_OS_REPO_USERNAME) \
		-e TOWN_OS_REPO_PASSWORD=$(TOWN_OS_REPO_PASSWORD) \
		-e TOWN_OS_NETWORK_MODE=host \
		--systemd=true --privileged \
		--device /dev/btrfs-control:/dev/btrfs-control:rwm \
		-v $$(cat town-os-dev.mount):/data/btrfs:z \
		-v $$(pwd)/dev-data:/data/db:z \
		-v $$(pwd)/dev-repos:/data/repos:z \
		-v /run/containers/0/auth.json:/run/containers/0/auth.json:ro,z \
		--name $(PODMAN_DEV_CONTAINER) $(PODMAN_DEV_IMAGE)
	@echo "API server: http://$$(hostname):5309"
	cd ui && bun install && VITE_API_URL=http://$$(hostname):5309 bun run dev -- --host; \
		sudo -E podman rm -f $(PODMAN_DEV_CONTAINER)

preflight-dev: docker-login .integration-port
	@echo "Checking podman..."
	@command -v podman >/dev/null 2>&1 || { echo "ERROR: podman not found"; exit 1; }
	@echo "Checking btrfs-progs..."
	@command -v mkfs.btrfs >/dev/null 2>&1 || { echo "ERROR: mkfs.btrfs not found"; exit 1; }
	@echo "Checking credentials..."
	@test -n "$(TOWN_OS_REPO_USERNAME)" || { echo "ERROR: TOWN_OS_REPO_USERNAME not set"; exit 1; }
	@test -n "$(TOWN_OS_REPO_PASSWORD)" || { echo "ERROR: TOWN_OS_REPO_PASSWORD not set"; exit 1; }
	@echo "Checking bridge networking..."
	@sudo podman run --rm -d --name $(PREFLIGHT_CONTAINER) -p $$(cat .integration-port):80 docker.io/library/nginx:alpine >/dev/null 2>&1 && \
		sleep 2 && \
		curl -sf http://127.0.0.1:$$(cat .integration-port)/ >/dev/null 2>&1 && \
		echo "Bridge networking: OK" && \
		sudo podman rm -f $(PREFLIGHT_CONTAINER) >/dev/null 2>&1 || \
		{ sudo podman rm -f $(PREFLIGHT_CONTAINER) >/dev/null 2>&1; echo "ERROR: bridge networking (-p) not working"; exit 1; }
	@echo "All preflight checks passed."

clean-dev: clean-cache

dev-stop:
	@sudo -E podman rm -f $(PODMAN_DEV_CONTAINER)

auto-test:
	go get github.com/cespare/reflex@latest
	reflex -r '\.(js|go)$$' make test

auto-test-full:
	go get github.com/cespare/reflex@latest
	sudo -E -E $(shell go env GOPATH)/bin/reflex -r '\.(go|js)$$' make test-full

build-networkcontroller:
	CGO_ENABLED=0 go build -o town-os-networkcontroller ./src/networkcontroller/cmd/town-os-networkcontroller

lint:
	go vet ./src/... ./integration/...
	$(shell go env GOPATH)/bin/golangci-lint run ./src/... ./integration/...

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
	@rm -f .integration-port

clean: clean-cache
	sudo rm -rf .cache

clean-cache: dev-stop clean-btrfs-dev
	@sudo rm -rf dev-data dev-repos

clean-all: clean clean-dev clean-integration clean-btrfs

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
REGISTRY_CONTAINER   := town-os-registry-$(INSTANCE_ID)
GITEA_CONTAINER      := town-os-gitea-$(INSTANCE_ID)

# Global image cache shared across all working trees. Override with:
#   make IMAGE_CACHE=/some/other/path test-full
IMAGE_CACHE ?= /var/cache/town-os/images

# Base images needed to build and test.
BASE_IMAGES := docker.io/library/golang:1.25-bookworm docker.io/oven/bun:latest docker.io/library/debian:bookworm-slim

# All images (base + service) that must be cached before integration runs.
ALL_IMAGES := $(BASE_IMAGES) docker.io/library/registry:2 docker.io/gitea/gitea:latest docker.io/library/nginx:1.27-alpine

test: lint
	go test -v -timeout 60m ./src/...
	cd ui && bun install && bun run test

docker-login:
	@if [ -n "$(DOCKER_USERNAME)" ] && [ -n "$(DOCKER_PASSWORD)" ]; then \
		echo "$(DOCKER_PASSWORD)" | sudo -E podman login -u "$(DOCKER_USERNAME)" --password-stdin docker.io; \
	fi

# If any cached image tar is missing, pull everything.
ensure-image-cache:
	@for img in $(ALL_IMAGES); do \
		safe=$$(basename "$$img" | tr ':' '-'); \
		if [ ! -f "$(IMAGE_CACHE)/$$safe.tar" ]; then \
			echo "Image cache incomplete (missing $$safe.tar) — running pull-images..."; \
			$(MAKE) pull-images; \
			break; \
		fi; \
	done

.cache/.images-pulled: ensure-image-cache docker-login
	@sudo -E mkdir -p $(IMAGE_CACHE)
	@for img in $(BASE_IMAGES); do \
		safe=$$(basename "$$img" | tr ':' '-'); \
		if sudo -E podman image exists "$$img" 2>/dev/null; then \
			echo "$$img: already in podman storage"; \
		elif [ -f "$(IMAGE_CACHE)/$$safe.tar" ]; then \
			echo "$$img: loading from cache..."; \
			sudo -E podman load -i "$(IMAGE_CACHE)/$$safe.tar"; \
		else \
			echo "$$img: pulling from Docker Hub..."; \
			sudo -E podman pull "$$img"; \
			echo "$$img: saving to cache..."; \
			sudo -E podman save -o "$(IMAGE_CACHE)/$$safe.tar" "$$img"; \
		fi; \
	done
	@mkdir -p .cache
	@touch .cache/.images-pulled

pull-images: docker-login
	@sudo -E mkdir -p $(IMAGE_CACHE)
	@for img in $(ALL_IMAGES); do \
		echo "Pulling $$img..."; \
		sudo -E podman pull "$$img"; \
		safe=$$(basename "$$img" | tr ':' '-'); \
		echo "$$img: saving to cache..."; \
		sudo -E podman save -o "$(IMAGE_CACHE)/$$safe.tar" "$$img"; \
	done
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

# Allocate a random available port for the local registry.
.registry-port:
	@python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1]); s.close()' > $@
	@echo "Registry port: $$(cat $@)"

# Discover docker.io images from test package repositories.
# Credentials are cleared because the local Gitea repos are public.
.cache/.registry-images: gitea-populate
	@mkdir -p .cache
	TOWN_OS_REPO_USERNAME= TOWN_OS_REPO_PASSWORD= \
	TOWN_OS_TEST_REPO_CORE_URL=http://127.0.0.1:$$(cat .gitea-port)/town-os/test-packages-core.git \
	TOWN_OS_TEST_REPO_EXTRAS_URL=http://127.0.0.1:$$(cat .gitea-port)/town-os/test-packages-extras.git \
		go run ./src/registry/cmd/discover-images/ > $@
	@echo "Discovered $$(wc -l < $@) images"

# Start a local registry:2 container.
registry: ensure-image-cache .registry-port
	@sudo -E podman rm -f $(REGISTRY_CONTAINER) 2>/dev/null || true
	sudo -E podman load -i $(IMAGE_CACHE)/registry-2.tar
	sudo -E podman run -d --pull=never --name $(REGISTRY_CONTAINER) \
		-p $$(cat .registry-port):5000 \
		docker.io/library/registry:2
	@echo "Registry running on port $$(cat .registry-port)"

# Pull each discovered image, re-tag for the local registry, and push.
# Uses IMAGE_CACHE to avoid re-downloading images across working trees.
registry-populate: registry .cache/.registry-images
	@sudo -E mkdir -p $(IMAGE_CACHE)
	@port=$$(cat .registry-port); \
	while IFS= read -r image; do \
		local_tag="localhost:$$port/$${image#docker.io/}"; \
		safe=$$(basename "$$image" | tr ':' '-'); \
		if sudo -E podman image exists "$$image" 2>/dev/null; then \
			echo "$$image: already in podman storage"; \
		elif [ -f "$(IMAGE_CACHE)/$$safe.tar" ]; then \
			echo "$$image: loading from cache..."; \
			sudo -E podman load -i "$(IMAGE_CACHE)/$$safe.tar"; \
		else \
			echo "$$image: pulling..."; \
			sudo -E podman pull "$$image" || { echo "WARNING: failed to pull $$image"; continue; }; \
			echo "$$image: saving to cache..."; \
			sudo -E podman save -o "$(IMAGE_CACHE)/$$safe.tar" "$$image"; \
		fi; \
		echo "Mirroring $$image -> $$local_tag"; \
		sudo -E podman tag "$$image" "$$local_tag" && \
		sudo -E podman push --tls-verify=false "$$local_tag" || \
		{ echo "WARNING: failed to mirror $$image"; }; \
	done < .cache/.registry-images

# Generate registries.conf that redirects docker.io to the local registry.
.cache/registries.conf: .registry-port
	@mkdir -p .cache
	@printf '[[registry]]\nprefix = "docker.io"\nlocation = "docker.io"\n\n[[registry.mirror]]\nlocation = "localhost:%s"\ninsecure = true\n' "$$(cat .registry-port)" > $@
	@echo "Generated registries.conf (mirror on port $$(cat .registry-port))"

# Stop and remove the local registry container.
registry-stop:
	@sudo -E podman rm -f $(REGISTRY_CONTAINER) 2>/dev/null || true
	@rm -f .registry-port

# Allocate a random available port for the local Gitea instance.
.gitea-port:
	@python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1]); s.close()' > $@
	@echo "Gitea port: $$(cat $@)"

# Start a local Gitea container and create the admin user.
gitea: ensure-image-cache .gitea-port
	@sudo -E podman rm -f $(GITEA_CONTAINER) 2>/dev/null || true
	sudo -E podman load -i $(IMAGE_CACHE)/gitea-latest.tar
	sudo -E podman run -d --pull=never --name $(GITEA_CONTAINER) \
		-p $$(cat .gitea-port):3000 \
		-e GITEA__security__INSTALL_LOCK=true \
		docker.io/gitea/gitea:latest
	@echo "Waiting for Gitea to be ready..."
	@for i in $$(seq 1 60); do \
		curl -sf http://127.0.0.1:$$(cat .gitea-port)/api/v1/version >/dev/null 2>&1 && break; \
		sleep 1; \
	done
	@echo "Creating Gitea admin user..."
	@sudo -E podman exec --user git $(GITEA_CONTAINER) \
		gitea admin user create --admin \
		--username town-os --password town-os-test \
		--email town-os@localhost --must-change-password=false 2>/dev/null || true
	@echo "Gitea running on port $$(cat .gitea-port)"

# Populate Gitea with test repos cached from GitHub and pushed via go-git.
gitea-populate: gitea
	@mkdir -p .cache/git-repos
	GITEA_URL=http://127.0.0.1:$$(cat .gitea-port) \
	GIT_CACHE_DIR=.cache/git-repos \
		go run ./src/gitea/cmd/populate-repos/

# Stop and remove the local Gitea container.
gitea-stop:
	@sudo -E podman rm -f $(GITEA_CONTAINER) 2>/dev/null || true
	@rm -f .gitea-port

test-ui-integration: test-image ui-integration-image btrfs .integration-port registry-populate .cache/registries.conf gitea-populate
	@sudo -E podman rm -f $(PODMAN_UI_CONTAINER)
	@sudo -E podman rm -f $(PODMAN_UI_BACKEND)
	sudo -E podman run -e LOG_LEVEL=debug -e DEBUG=1 -e TOWN_OS_TEST=1 \
		-e TOWN_OS_REPO_USERNAME=town-os \
		-e TOWN_OS_REPO_PASSWORD=town-os-test \
		-e TOWN_OS_LISTEN=:$$(cat .integration-port) \
		-e TOWN_OS_TEST_REPO_CORE_URL=http://127.0.0.1:$$(cat .gitea-port)/town-os/test-packages-core.git \
		-e TOWN_OS_TEST_REPO_EXTRAS_URL=http://127.0.0.1:$$(cat .gitea-port)/town-os/test-packages-extras.git \
		-d --net host --systemd=true --privileged \
		--device /dev/btrfs-control:/dev/btrfs-control:rwm \
		-v $$(cat town-os.mount):/data/btrfs:z \
		-v $$(pwd)/.cache/registries.conf:/etc/containers/registries.conf.d/local-registry.conf:ro,z \
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

test-integration: lint test-image btrfs .integration-port registry-populate .cache/registries.conf gitea-populate
	@sudo -E podman rm -f $(PODMAN_CONTAINER)
	sudo -E podman run -e LOG_LEVEL=${LOG_LEVEL} -e TOWN_OS_TEST=1 \
		-e TOWN_OS_REPO_USERNAME=town-os \
		-e TOWN_OS_REPO_PASSWORD=town-os-test \
		-e TOWN_OS_LISTEN=:$$(cat .integration-port) \
		-e TOWN_OS_TEST_REPO_CORE_URL=http://127.0.0.1:$$(cat .gitea-port)/town-os/test-packages-core.git \
		-e TOWN_OS_TEST_REPO_EXTRAS_URL=http://127.0.0.1:$$(cat .gitea-port)/town-os/test-packages-extras.git \
		-d --net host --systemd=true --privileged \
		--device /dev/btrfs-control:/dev/btrfs-control:rwm \
		-v $$(cat town-os.mount):/data/btrfs:z \
		-v $$(pwd)/.cache/registries.conf:/etc/containers/registries.conf.d/local-registry.conf:ro,z \
		--name=$(PODMAN_CONTAINER) $(PODMAN_TEST_IMAGE)
	@echo "Waiting for systemd to be ready..."
	@for i in $$(seq 1 30); do \
		sudo -E podman exec $(PODMAN_CONTAINER) test -S /var/run/dbus/system_bus_socket 2>/dev/null && break; \
		sleep 1; \
	done
	@sudo -E podman exec -w /test $(PODMAN_CONTAINER) /integration-test -test.v -test.timeout 60m

# Run the full test suite and always clean up containers and btrfs afterward.
test-full: test
	@rc=0; $(MAKE) test-integration || rc=$$?; $(MAKE) test-ui-integration || rc=$$?; \
	$(MAKE) clean-integration clean-btrfs; exit $$rc

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
		--name $(PODMAN_DEV_CONTAINER) $(PODMAN_DEV_IMAGE)
	@echo "API server: http://$$(hostname):5309"
	cd ui && bun install && VITE_API_URL=http://$$(hostname):5309 bun run dev -- --host; \
		sudo -E podman rm -f $(PODMAN_DEV_CONTAINER)

preflight-dev: ensure-image-cache .integration-port
	@echo "Checking podman..."
	@command -v podman >/dev/null 2>&1 || { echo "ERROR: podman not found"; exit 1; }
	@echo "Checking btrfs-progs..."
	@command -v mkfs.btrfs >/dev/null 2>&1 || { echo "ERROR: mkfs.btrfs not found"; exit 1; }
	@echo "Checking credentials..."
	@test -n "$(TOWN_OS_REPO_USERNAME)" || { echo "ERROR: TOWN_OS_REPO_USERNAME not set"; exit 1; }
	@test -n "$(TOWN_OS_REPO_PASSWORD)" || { echo "ERROR: TOWN_OS_REPO_PASSWORD not set"; exit 1; }
	@echo "Checking bridge networking..."
	sudo -E podman load -i $(IMAGE_CACHE)/nginx-1.27-alpine.tar
	@sudo podman run --pull=never --rm -d --name $(PREFLIGHT_CONTAINER) -p $$(cat .integration-port):80 docker.io/library/nginx:1.27-alpine >/dev/null 2>&1 && \
		sleep 2 && \
		curl -sf http://127.0.0.1:$$(cat .integration-port)/ >/dev/null 2>&1 && \
		echo "Bridge networking: OK" && \
		sudo podman rm -f $(PREFLIGHT_CONTAINER) >/dev/null 2>&1 || \
		{ sudo podman rm -f $(PREFLIGHT_CONTAINER) >/dev/null 2>&1; echo "ERROR: bridge networking (-p) not working"; exit 1; }
	@echo "All preflight checks passed."

clean-dev: dev-stop-all clean-cache

# Stop and remove the dev container for this working directory.
dev-stop:
	@sudo -E podman rm -f $(PODMAN_DEV_CONTAINER) 2>/dev/null || true

# Stop and remove all town-os dev containers (from any working directory).
dev-stop-all:
	@sudo -E podman ps -a --format '{{.Names}}' 2>/dev/null \
		| grep -E '^town-os-dev$$' \
		| xargs -r -I{} sudo -E podman rm -f {} 2>/dev/null || true

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

clean-integration: registry-stop gitea-stop
	@sudo -E podman rm -f $(PODMAN_CONTAINER) 2>/dev/null || true
	@sudo -E podman rm -f $(PODMAN_UI_BACKEND) 2>/dev/null || true
	@sudo -E podman rm -f $(PODMAN_UI_CONTAINER) 2>/dev/null || true
	@rm -f .integration-port

clean: clean-cache
	sudo rm -rf .cache

clean-cache: dev-stop clean-btrfs-dev
	@sudo rm -rf dev-data dev-repos

clean-image-cache:
	sudo rm -rf $(IMAGE_CACHE)

# Remove all town-os containers from any working directory / instance.
clean-containers:
	@sudo -E podman ps -a --format '{{.Names}}' 2>/dev/null \
		| grep -E '^(town-os-(test|dev|registry|gitea|ui-(integration|backend))|preflight-test)-' \
		| xargs -r -I{} sudo -E podman rm -f {} 2>/dev/null || true
	@sudo -E podman ps -a --format '{{.Names}}' 2>/dev/null \
		| grep -E '^town-os-dev$$' \
		| xargs -r -I{} sudo -E podman rm -f {} 2>/dev/null || true

clean-all: clean-containers clean clean-dev clean-integration clean-btrfs

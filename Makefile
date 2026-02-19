PODMAN_IMAGE := town-os
PODMAN_TEST_IMAGE := town-os-test
PODMAN_CONTAINER := town-os-test

test: lint
	go test -v ./src/...
	cd ui && bun run test

PODMAN_UI_IMAGE := town-os-ui-integration
PODMAN_UI_CONTAINER := town-os-ui-integration
PODMAN_UI_BACKEND := town-os-ui-backend

.cache/.images-pulled:
	sudo podman pull golang:1.25-bookworm
	sudo podman pull oven/bun:latest
	sudo podman pull debian:bookworm-slim
	@mkdir -p .cache
	@touch .cache/.images-pulled

pull-images:
	sudo podman pull docker.io/library/golang:1.25-bookworm
	sudo podman pull docker.io/oven/bun:latest
	sudo podman pull docker.io/library/debian:bookworm-slim
	@mkdir -p .cache
	@touch .cache/.images-pulled

ui-integration-image: .cache/.images-pulled
	sudo podman build --pull=never \
		-t $(PODMAN_UI_IMAGE) -f integration/testdata/Containerfile.ui-integration .

production-image: .cache/.images-pulled
	mkdir -p .cache/go-mod .cache/go-build
	sudo podman build --pull=never \
		--volume $$(pwd)/.cache/go-mod:/go/pkg/mod:z \
		--volume $$(pwd)/.cache/go-build:/root/.cache/go-build:z \
		-t $(PODMAN_IMAGE) -f Containerfile .

test-ui-integration: test-image ui-integration-image btrfs
	@sudo podman rm -f $(PODMAN_UI_CONTAINER) $(PODMAN_UI_BACKEND)
	sudo podman run -d -e DEBUG=1 --systemd=true --privileged \
		--device /dev/btrfs-control:/dev/btrfs-control:rwm \
		-v $$(cat town-os.mount):/data/btrfs:z \
		--name $(PODMAN_UI_BACKEND) $(PODMAN_TEST_IMAGE)
	echo "Waiting for backend availability"
	@sudo podman run --rm -it --network container:$(PODMAN_UI_BACKEND) $(PODMAN_TEST_IMAGE) sh -c 'while ! curl -sSL $(PODMAN_UI_BACKEND):8080; do sleep 1; done'
	sudo podman run \
		--network container:$(PODMAN_UI_BACKEND) \
		-e INTEGRATION_URL=http://$(PODMAN_UI_BACKEND):8080 \
		--name $(PODMAN_UI_CONTAINER) $(PODMAN_UI_IMAGE) \
		bun run test:integration

test-integration: lint test-image btrfs
	@sudo podman rm -f $(PODMAN_CONTAINER)
	@cp $$HOME/.gitconfig .gitconfig.tmp
	@cp $$HOME/.git-credentials .git-credentials.tmp
	@git config --file .gitconfig.tmp credential.helper "store --file /root/.git-credentials"
	sudo podman run -e DEBUG=${DEBUG} -d --systemd=true --privileged \
		--device /dev/btrfs-control:/dev/btrfs-control:rwm \
		-v $$(cat town-os.mount):/data/btrfs:z \
		-v $$(pwd)/.gitconfig.tmp:/root/.gitconfig:ro,z \
		-v $$(pwd)/.git-credentials.tmp:/root/.git-credentials:ro,z \
		--name=$(PODMAN_CONTAINER) $(PODMAN_TEST_IMAGE)
	@echo "Waiting for mount"
	@until sudo podman exec -it $(PODMAN_CONTAINER) btrfs filesystem sync /data/btrfs &>/dev/null; do sleep 1; done
	@sudo podman exec -w /test $(PODMAN_CONTAINER) /integration-test -test.v; \
		EXIT=$$?; \
		rm -f .gitconfig.tmp .git-credentials.tmp; \
		exit $$EXIT

test-full: test test-integration test-ui-integration

test-image: production-image
	mkdir -p .cache/go-mod .cache/go-build
	sudo podman build --pull=never \
		--volume $$(pwd)/.cache/go-mod:/go/pkg/mod:z \
		--volume $$(pwd)/.cache/go-build:/root/.cache/go-build:z \
		-t $(PODMAN_TEST_IMAGE) -f integration/testdata/Containerfile.systemd .

PODMAN_DEV_CONTAINER := town-os-dev

dev: test-image btrfs
	@sudo podman rm -f $(PODMAN_DEV_CONTAINER)
	@touch dev.db
	sudo podman run -d -p 8080:8080 -e DEBUG=1 --systemd=true --privileged \
		--device /dev/btrfs-control:/dev/btrfs-control:rwm \
		-v $$(cat town-os.mount):/data/btrfs:z \
		-v $$(pwd)/dev.db:/data/dev.db:z \
		--name $(PODMAN_DEV_CONTAINER) $(PODMAN_TEST_IMAGE)
	@echo "API server: http://$$(hostname):8080"
	cd ui && VITE_API_URL=http://$$(hostname):8080 bun run dev -- --host; \
		sudo podman rm -f $(PODMAN_DEV_CONTAINER)

dev-clean: clean-btrfs
	@sudo podman rm -f $(PODMAN_DEV_CONTAINER)
	rm -f dev.db dev.db-shm dev.db-wal

dev-stop: clean-btrfs
	@sudo podman rm -f $(PODMAN_DEV_CONTAINER)

auto-test:
	go get github.com/cespare/reflex@latest
	reflex -r '\.(js|go)$$' make test

auto-test-full:
	go get github.com/cespare/reflex@latest
	sudo -E $(shell go env GOPATH)/bin/reflex -r '\.(go|js)$$' make test-full

lint:
	go vet ./...
	go vet -tags=podman ./...
	$(shell go env GOPATH)/bin/golangci-lint run

BTRFS_IMAGE ?= $(shell mktemp btrfs.XXXXXX)

btrfs: clean-btrfs
	echo $(BTRFS_IMAGE) >town-os.disk
	truncate -s 50G $$(cat town-os.disk)
	mkfs.btrfs -f $$(cat town-os.disk)
	sudo losetup -f $$(cat town-os.disk)
	sudo losetup -j $$(cat town-os.disk) | awk -F: '{ print $$1 }' > town-os.loop
	mktemp -d > town-os.mount
	sudo mount -t btrfs $$(cat town-os.loop) $$(cat town-os.mount)

clean-btrfs:
	@if [ -f town-os.mount ]; then \
		sudo umount $$(cat town-os.mount) 2>/dev/null || true; \
		rmdir $$(cat town-os.mount) 2>/dev/null || true; \
	fi
	@if [ -f town-os.disk ]; then \
		sudo losetup -j $$(cat town-os.disk) | awk -F: '{ print $$1 }' | xargs -I{} sudo losetup -d {} 2>/dev/null || true; \
	fi
	rm -f btrfs.* town-os.disk town-os.loop town-os.mount

clean: clean-podman
	rm -f dev.db dev.db-shm dev.db-wal
	rm -rf .cache
	rm -f .gitconfig.tmp .git-credentials.tmp

clean-podman: clean-btrfs
	@sudo podman rm -f $(PODMAN_CONTAINER)
	@sudo podman rm -f $(PODMAN_UI_BACKEND)
	@sudo podman rm -f $(PODMAN_UI_CONTAINER)
	@sudo podman rm -f $(PODMAN_DEV_CONTAINER)

test-systemd: test-image btrfs
	@sudo podman rm -f $(PODMAN_CONTAINER)
	sudo podman run -e DEBUG=1 -d --systemd=true --privileged \
		--device /dev/btrfs-control:/dev/btrfs-control:rwm \
		-v $$(cat town-os.mount):/data/btrfs:z \
		--name=$(PODMAN_CONTAINER) $(PODMAN_TEST_IMAGE)
	sudo podman exec -w /test $(PODMAN_CONTAINER) /integration-test -test.v -test.run TestPodman

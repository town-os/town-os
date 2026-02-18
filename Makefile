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
	podman pull golang:1.25-bookworm
	podman pull oven/bun:latest
	podman pull debian:bookworm-slim
	@mkdir -p .cache
	@touch .cache/.images-pulled

pull-images:
	podman pull golang:1.25-bookworm
	podman pull oven/bun:latest
	podman pull debian:bookworm-slim
	@mkdir -p .cache
	@touch .cache/.images-pulled

ui-integration-image: .cache/.images-pulled
	podman build --pull=never \
		-t $(PODMAN_UI_IMAGE) -f integration/testdata/Containerfile.ui-integration .

production-image: .cache/.images-pulled
	mkdir -p .cache/go-mod .cache/go-build
	podman build --pull=never \
		--volume $$(pwd)/.cache/go-mod:/go/pkg/mod:z \
		--volume $$(pwd)/.cache/go-build:/root/.cache/go-build:z \
		-t $(PODMAN_IMAGE) -f Containerfile .

test-ui-integration: test-image ui-integration-image btrfs
	@podman stop $(PODMAN_UI_BACKEND) 2>/dev/null || true
	@podman rm $(PODMAN_UI_BACKEND) 2>/dev/null || true
	@podman stop $(PODMAN_UI_CONTAINER) 2>/dev/null || true
	@podman rm $(PODMAN_UI_CONTAINER) 2>/dev/null || true
	podman run -d -e DEBUG=1 --systemd=true --privileged \
		--device $$(cat town-os.loop) \
		--name $(PODMAN_UI_BACKEND) $(PODMAN_TEST_IMAGE)
	@sleep 5
	podman exec $(PODMAN_UI_BACKEND) mount -t btrfs /dev/loop0 /data/btrfs
	echo "Waiting for backend availability"
	@podman run --rm -it --network container:$(PODMAN_UI_BACKEND) $(PODMAN_TEST_IMAGE) sh -c 'while ! curl -sSL $(PODMAN_UI_BACKEND):8080; do sleep 1; done'
	podman run \
		--network container:$(PODMAN_UI_BACKEND) \
		-e INTEGRATION_URL=http://$(PODMAN_UI_BACKEND):8080 \
		--name $(PODMAN_UI_CONTAINER) $(PODMAN_UI_IMAGE) \
		bun run test:integration

test-integration: lint test-image btrfs
	@podman stop $(PODMAN_CONTAINER) 2>/dev/null || true
	@podman rm $(PODMAN_CONTAINER) 2>/dev/null || true
	@cp $$HOME/.gitconfig .gitconfig.tmp
	@cp $$HOME/.git-credentials .git-credentials.tmp
	@git config --file .gitconfig.tmp credential.helper "store --file /root/.git-credentials"
	podman run -e DEBUG=${DEBUG} -d --systemd=true --security-opt=label=disable --security-opt=apparmor=unconfined --security-opt=seccomp=unconfined --privileged \
		--device /dev/loop-control:/dev/loop-control:rwm \
		-v $$(pwd)/.gitconfig.tmp:/root/.gitconfig:ro,z \
		-v $$(pwd)/.git-credentials.tmp:/root/.git-credentials:ro,z \
		--name=$(PODMAN_CONTAINER) $(PODMAN_TEST_IMAGE) 
	podman exec --privileged -it $(PODMAN_CONTAINER) truncate -s 50G /town-os.disk
	podman exec --privileged -it $(PODMAN_CONTAINER) mkfs.btrfs -f /town-os.disk
	podman exec --privileged -it $(PODMAN_CONTAINER) losetup -f /town-os.disk
	podman exec --privileged -it $(PODMAN_CONTAINER) mount -t btrfs /dev/loop0 /data/btrfs
	@podman exec -w /test $(PODMAN_CONTAINER) /integration-test -test.v; \
		EXIT=$$?; \
		rm -f .gitconfig.tmp .git-credentials.tmp; \
		exit $$EXIT

test-full: test test-integration

test-image: production-image
	mkdir -p .cache/go-mod .cache/go-build
	podman build --pull=never \
		--volume $$(pwd)/.cache/go-mod:/go/pkg/mod:z \
		--volume $$(pwd)/.cache/go-build:/root/.cache/go-build:z \
		-t $(PODMAN_TEST_IMAGE) -f integration/testdata/Containerfile.systemd .

PODMAN_DEV_CONTAINER := town-os-dev

dev: test-image btrfs
	@podman stop $(PODMAN_DEV_CONTAINER) 2>/dev/null || true
	@podman rm $(PODMAN_DEV_CONTAINER) 2>/dev/null || true
	@touch dev.db
	podman run -d -p 8080:8080 -e DEBUG=1 --systemd=true --privileged \
		--device $$(cat town-os.loop) \
		-v $$(pwd)/dev.db:/data/dev.db:z \
		--name $(PODMAN_DEV_CONTAINER) $(PODMAN_TEST_IMAGE)
	@sleep 5
	podman exec $(PODMAN_DEV_CONTAINER) mount -t btrfs /dev/loop0 /data/btrfs
	@echo "API server: http://$$(hostname):8080"
	cd ui && VITE_API_URL=http://$$(hostname):8080 bun run dev -- --host; \
		podman stop $(PODMAN_DEV_CONTAINER) 2>/dev/null || true; \
		podman rm $(PODMAN_DEV_CONTAINER) 2>/dev/null || true

dev-clean: clean-btrfs
	@podman stop $(PODMAN_DEV_CONTAINER) 2>/dev/null || true
	@podman rm $(PODMAN_DEV_CONTAINER) 2>/dev/null || true
	rm -f dev.db dev.db-shm dev.db-wal

dev-stop: clean-btrfs
	@podman stop $(PODMAN_DEV_CONTAINER) 2>/dev/null || true
	@podman rm $(PODMAN_DEV_CONTAINER) 2>/dev/null || true

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

clean-btrfs:
	@if [ -f town-os.disk ]; then \
		sudo losetup -j $$(cat town-os.disk) | awk -F: '{ print $$1 }' | xargs -I{} sudo losetup -d {} 2>/dev/null || true; \
	fi
	rm -f btrfs.* town-os.disk town-os.loop

clean: clean-podman
	rm -f dev.db dev.db-shm dev.db-wal
	rm -rf .cache
	rm -f .gitconfig.tmp .git-credentials.tmp

clean-podman: clean-btrfs
	@podman stop $(PODMAN_CONTAINER) 2>/dev/null || true
	@podman rm $(PODMAN_CONTAINER) 2>/dev/null || true
	@podman rmi $(PODMAN_TEST_IMAGE) 2>/dev/null || true
	@podman stop $(PODMAN_UI_BACKEND) 2>/dev/null || true
	@podman rm $(PODMAN_UI_BACKEND) 2>/dev/null || true
	@podman stop $(PODMAN_UI_CONTAINER) 2>/dev/null || true
	@podman rm $(PODMAN_UI_CONTAINER) 2>/dev/null || true
	@podman rmi $(PODMAN_UI_IMAGE) 2>/dev/null || true
	@podman stop $(PODMAN_DEV_CONTAINER) 2>/dev/null || true
	@podman rm $(PODMAN_DEV_CONTAINER) 2>/dev/null || true
	@podman rmi $(PODMAN_IMAGE) 2>/dev/null || true

test-systemd: test-image btrfs
	@podman stop $(PODMAN_CONTAINER) 2>/dev/null || true
	@podman rm $(PODMAN_CONTAINER) 2>/dev/null || true
	podman run -e DEBUG=1 -d --systemd=true --privileged \
		--device $$(cat town-os.loop) \
		--name=$(PODMAN_CONTAINER) $(PODMAN_TEST_IMAGE)
	@sleep 5
	podman exec $(PODMAN_CONTAINER) mount -t btrfs /dev/loop0 /data/btrfs
	podman exec -w /test $(PODMAN_CONTAINER) /integration-test -test.v -test.run TestPodman

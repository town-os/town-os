PODMAN_BASE_IMAGE := town-os-systemd-base
PODMAN_IMAGE := town-os-systemd-test
PODMAN_CONTAINER := town-os-systemd-test
PODMAN_BASE_CONTAINERFILE := integration/testdata/Containerfile.systemd-base

test: lint
	go test -v ./src/...

test-integration: lint btrfs podman-image
	sudo go clean -testcache
	sudo -E cp $$HOME/.gitconfig .gitconfig.tmp
	sudo -E cp $$HOME/.git-credentials .git-credentials.tmp
	sudo -E git config --file .gitconfig.tmp credential.helper "store --file $$(pwd)/.git-credentials.tmp"
	sudo -E DEBUG=${DEBUG} GIT_CONFIG_GLOBAL=$$(pwd)/.gitconfig.tmp go test -v ./integration/...
	sudo rm -f .gitconfig.tmp .git-credentials.tmp
	make clean-btrfs
	podman run -e DEBUG=${DEBUG} -d --systemd=true --privileged --name=$(PODMAN_CONTAINER) $(PODMAN_IMAGE)
	@sleep 5
	podman exec $(PODMAN_CONTAINER) /podman-test -test.v -test.run TestPodman; \
		EXIT=$$?; \
		podman stop $(PODMAN_CONTAINER) 2>/dev/null || true; \
		podman rm $(PODMAN_CONTAINER) 2>/dev/null || true; \
		exit $$EXIT

test-full: test test-integration

auto-test:
	go get github.com/cespare/reflex@latest
	reflex -r '\.go$$' make test

auto-test-full:
	go get github.com/cespare/reflex@latest
	sudo -E $(shell go env GOPATH)/bin/reflex -r '\.go$$' make test-full

lint:
	go vet ./...
	go vet -tags=podman ./...
	$(shell go env GOPATH)/bin/golangci-lint run

btrfs: clean-btrfs
	echo $$(mktemp btrfs.XXXXXX) >town-os.disk
	truncate -s 50G $$(cat town-os.disk)
	mkfs.btrfs -L town-os-test $$(cat town-os.disk)
	sudo losetup -f --partscan $$(cat town-os.disk)
	sudo mkdir -p local-mount
	sudo mount -t btrfs $$(sudo losetup -j $$(cat town-os.disk) | awk -F: '{ print $$1 }') local-mount

clean-btrfs:
	sudo umount -Rf local-mount || :
	sudo losetup -j $$(cat town-os.disk) | awk -F: '{ print $$1 }' | xargs -I{} sudo losetup -d {}
	rm -f btrfs.* town-os.disk

podman-base:
	@mkdir -p .cache
	@HASH=$$(sha256sum $(PODMAN_BASE_CONTAINERFILE) | awk '{print $$1}'); \
	if [ -f .cache/podman-base-hash ] && [ "$$(cat .cache/podman-base-hash)" = "$$HASH" ] && \
	   podman image exists $(PODMAN_BASE_IMAGE) 2>/dev/null; then \
		echo "Base image $(PODMAN_BASE_IMAGE) is up to date"; \
	else \
		echo "Building base image $(PODMAN_BASE_IMAGE)..."; \
		podman build -t $(PODMAN_BASE_IMAGE) -f $(PODMAN_BASE_CONTAINERFILE) . && \
		echo "$$HASH" > .cache/podman-base-hash; \
	fi

podman-image: podman-base
	podman stop $(PODMAN_CONTAINER) 2>/dev/null || true
	podman rm $(PODMAN_CONTAINER) 2>/dev/null || true
	mkdir -p .cache/go-mod .cache/go-build
	podman build \
		--volume $$(pwd)/.cache/go-mod:/go/pkg/mod:z \
		--volume $$(pwd)/.cache/go-build:/root/.cache/go-build:z \
		-t $(PODMAN_IMAGE) -f integration/testdata/Containerfile.systemd .

clean-podman:
	@podman stop $(PODMAN_CONTAINER) 2>/dev/null || true
	@podman rm $(PODMAN_CONTAINER) 2>/dev/null || true
	@podman rmi $(PODMAN_IMAGE) 2>/dev/null || true
	@podman rmi $(PODMAN_BASE_IMAGE) 2>/dev/null || true
	rm -f .cache/podman-base-hash

test-systemd: podman-image
	podman run -d --systemd=true --privileged --name=$(PODMAN_CONTAINER) $(PODMAN_IMAGE)
	@sleep 5
	podman exec $(PODMAN_CONTAINER) /podman-test -test.v -test.run TestPodman; \
		EXIT=$$?; \
		podman stop $(PODMAN_CONTAINER) 2>/dev/null || true; \
		podman rm $(PODMAN_CONTAINER) 2>/dev/null || true; \
		exit $$EXIT

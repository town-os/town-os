test: lint
	go test -v ./src/...

test-integration: lint btrfs
	sudo go clean -testcache
	sudo go test -tags=integration -v ./integration/...
	make clean-btrfs

auto-test:
	go get github.com/cespare/reflex@latest
	reflex -r '\.go$$' make test

lint:
	golangci-lint run

btrfs:
	echo $$(mktemp btrfs.XXXXXX) >town-os.disk
	truncate -s 50G $$(cat town-os.disk)
	mkfs.btrfs -L town-os-test $$(cat town-os.disk)
	sudo losetup -f --partscan $$(cat town-os.disk)
	sudo mkdir -p local-mount
	sudo mount -t btrfs $$(sudo losetup -j $$(cat town-os.disk) | awk -F: '{ print $$1 }') local-mount

clean-btrfs:
	rm -f $$(cat town-os.disk) town-os.disk

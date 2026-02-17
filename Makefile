test: lint
	go test -v ./src/...

test-integration: lint btrfs
	sudo go clean -testcache
	cp $$HOME/.gitconfig .gitconfig.tmp
	cp $$HOME/.git-credentials .git-credentials.tmp
	git config --file .gitconfig.tmp credential.helper "store --file $$(pwd)/.git-credentials.tmp"
	sudo -E GIT_CONFIG_GLOBAL=$$(pwd)/.gitconfig.tmp go test -tags=integration -v ./integration/...
	rm -f .gitconfig.tmp .git-credentials.tmp
	make clean-btrfs

auto-test:
	go get github.com/cespare/reflex@latest
	reflex -r '\.go$$' make test

lint:
	golangci-lint run

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

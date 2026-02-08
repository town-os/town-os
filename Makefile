test: lint
	go test -v ./...

test-integration: lint btrfs
	go clean -testcache
	go test -tags=integration -v ./...
	make clean-btrfs

auto-test:
	go get github.com/cespare/reflex@latest
	reflex -r '\.go$$' make test

lint:
	golangci-lint run

btrfs:
	echo $$(mktemp btrfs.XXXXXX) >town-os.disk
	truncate -s 50G $$(cat town-os.disk)

clean-btrfs:
	rm -f $$(cat town-os.disk) town-os.disk

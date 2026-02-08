test:
	go test -v ./...

test-integration:
	go clean -testcache
	go test -tags=integration -v ./...

auto-test:
	go get github.com/cespare/reflex@latest
	reflex -r '\.go$$' make test

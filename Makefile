test:
	go test -v ./...

test-integration:
	go clean -testcache
	go test -p 1 -tags=integration -v ./...

auto-test:
	go get github.com/cespare/reflex
	reflex -r '\.go$$' make test

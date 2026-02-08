test:
	go test -v ./...

test-integration:
	go test -tags=integration -v ./...

auto-test:
	go get github.com/cespare/reflex
	reflex -r '\.go$$' make test

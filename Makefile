.PHONY: all test build vet fmt check

all: check
fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')
test:
	go test ./...
build:
	go build ./cmd/kothar
vet:
	go vet ./...
check: test build vet
	git diff --check

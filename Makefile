BINARY ?= viagh
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf dev)
LDFLAGS ?= -s -w -X main.version=$(VERSION)

.PHONY: build check clean fmt-check install release test test-race vet

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/viagh

fmt-check:
	test -z "$$(gofmt -l .)"

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

check: fmt-check vet test test-race

install:
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/viagh

release:
	VERSION="$(VERSION)" ./scripts/build-release.sh

clean:
	rm -rf bin dist coverage.out

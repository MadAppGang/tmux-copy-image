.PHONY: build test lint shellcheck clean embed-sync

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Build for the current platform.
build:
	CGO_ENABLED=0 go build \
		-ldflags="-X main.version=$(VERSION)" \
		-o bin/rpaster ./cmd/rpaster

# Run all unit tests with race detector.
test:
	go test -race ./...

# Run go vet.
lint:
	go vet ./...

# Run shellcheck on all bash scripts.
shellcheck:
	shellcheck --source-path=plugin/scripts -x plugin/scripts/*.sh plugin/tmux-clip-image.tmux

# Sync plugin files into internal/embedded/plugin so go:embed can find them.
# Must be run after any change to plugin/ before building.
embed-sync:
	rm -rf internal/embedded/plugin
	cp -r plugin internal/embedded/plugin

# Remove build artifacts.
clean:
	rm -rf bin/ dist/

# Cross-compilation targets.
build-darwin-amd64:
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build \
		-ldflags="-X main.version=$(VERSION) -s -w" \
		-o dist/rpaster-darwin-amd64 ./cmd/rpaster

build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build \
		-ldflags="-X main.version=$(VERSION) -s -w" \
		-o dist/rpaster-darwin-arm64 ./cmd/rpaster

build-linux-amd64:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
		-ldflags="-X main.version=$(VERSION) -s -w" \
		-o dist/rpaster-linux-amd64 ./cmd/rpaster

build-linux-arm64:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build \
		-ldflags="-X main.version=$(VERSION) -s -w" \
		-o dist/rpaster-linux-arm64 ./cmd/rpaster

build-all: build-darwin-amd64 build-darwin-arm64 build-linux-amd64 build-linux-arm64

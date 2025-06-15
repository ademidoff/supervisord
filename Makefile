# Get git version and commit for build-time injection
GIT_VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")

# Build flags for version injection
LDFLAGS := -X main.BuildVersion=$(GIT_VERSION) -X main.BuildCommit=$(GIT_COMMIT)

.PHONY: build build-linux build-darwin version

# Default build target (cross-platform)
build:
	go generate
	go build -tags release -ldflags "$(LDFLAGS) -s" -o supervisord

# Show version information that will be injected
version:
	@echo "Version: $(GIT_VERSION)"
	@echo "Commit:  $(GIT_COMMIT)"

build-linux:
	go generate
	GOOS=linux GOARCH=amd64 go build -tags release -a -ldflags "$(LDFLAGS) -s" -o supervisord

build-darwin:
	go generate
	GOOS=darwin GOARCH=arm64 go build -tags release -a -ldflags "$(LDFLAGS) -s" -o supervisord

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

gotest:
	go test -v ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

show:
	ps aux | grep -E '[s]upervisord|[s]leep' || true

kill:
	ps aux | grep -E '[s]upervisord|[s]leep' | awk '{print $$2}' | xargs kill -9

run:
	echo -n > supervisord.log
	./supervisord -c supervisord.conf -d

status:
	./supervisord ctl -v status || true

stop:
	./supervisord ctl shutdown || true

all:
	make kill
	make build
	make run
	sleep 2s
	make status

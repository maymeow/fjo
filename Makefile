# fjo build tasks. On Windows without make, use scripts/build.ps1.

GO       ?= go
BIN      ?= bin/fjo$(shell $(GO) env GOEXE)
PKG      := github.com/maymeow/fjo
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -s -w \
	-X $(PKG)/internal/build.Version=$(VERSION) \
	-X $(PKG)/internal/build.Commit=$(COMMIT) \
	-X $(PKG)/internal/build.Date=$(DATE)

.PHONY: all build test test-integration race vet fmt fmt-check lint vuln check clean

all: check build

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/fjo

test:
	$(GO) test ./...

# Starts Forgejo 15 and 17 containers (Docker) and runs the opt-in
# integration tests. See scripts/test-forgejo.sh.
test-integration:
	./scripts/test-forgejo.sh

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# golangci-lint with project plugins (see .custom-gcl.yml); requires
# golangci-lint v2.15.0 on PATH to build the custom binary.
GOLANGCI ?= ./bin/golangci-lint-fjo

$(GOLANGCI): .custom-gcl.yml $(wildcard tools/fmtwrite/*.go) tools/fmtwrite/go.mod
	golangci-lint custom

lint: $(GOLANGCI)
	cd tools/fmtwrite && $(GO) test ./...
	$(GOLANGCI) run ./...

vuln:
	govulncheck ./...

# Everything a release must pass (IMPLEMENTATION §13.6).
check: fmt-check vet lint test race vuln

clean:
	rm -rf bin dist

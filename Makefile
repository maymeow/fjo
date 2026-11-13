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

.PHONY: all build test race vet fmt fmt-check lint vuln check clean

all: check build

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/fjo

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

lint:
	staticcheck ./...

vuln:
	govulncheck ./...

# Everything a release must pass (IMPLEMENTATION §13.6).
check: fmt-check vet lint test race vuln

clean:
	rm -rf bin dist

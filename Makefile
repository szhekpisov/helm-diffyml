SHELL := /usr/bin/env bash

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
  -X github.com/szhekpisov/helm-diffyml/internal/build.Version=$(VERSION) \
  -X github.com/szhekpisov/helm-diffyml/internal/build.Commit=$(COMMIT) \
  -X github.com/szhekpisov/helm-diffyml/internal/build.Date=$(DATE)

.PHONY: build
build:
	mkdir -p bin
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/helm-diffyml .

.PHONY: install
install: build
	@if [ -z "$$HELM_BIN" ]; then HELM_BIN=$$(command -v helm); fi; \
	plugins="$$($$HELM_BIN env HELM_PLUGINS | tr -d '\"')"; \
	dest="$$plugins/helm-diffyml"; \
	mkdir -p "$$dest/bin"; \
	cp bin/helm-diffyml "$$dest/bin/helm-diffyml"; \
	cp plugin.yaml "$$dest/plugin.yaml"; \
	echo "installed to $$dest"

.PHONY: test
test:
	go test ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: lint
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
	  golangci-lint run ./...; \
	else \
	  echo "golangci-lint not found; run: brew install golangci-lint"; \
	  exit 1; \
	fi

.PHONY: clean
clean:
	rm -rf bin/ dist/

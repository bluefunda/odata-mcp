# Copyright 2025 bluefunda
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

BINARY      := odata-mcp
MODULE      := github.com/bluefunda/odata-mcp
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME  := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS     := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)"

.PHONY: build test fmt lint tidy clean docker help

## build: compile the binary
build:
	go build $(LDFLAGS) -o bin/$(BINARY) .

## test: run unit tests
test:
	go test ./... -v -race -count=1

## fmt: format all Go source files
fmt:
	gofmt -w .
	goimports -w . 2>/dev/null || true

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## tidy: tidy module dependencies
tidy:
	go mod tidy

## clean: remove build artefacts
clean:
	rm -rf bin/

## docker: build the Docker image
docker:
	docker build -t $(BINARY):$(VERSION) .

## help: show this help
help:
	@grep -E '^## ' Makefile | sed 's/## //'

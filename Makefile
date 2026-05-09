BINARY     := relayly
MODULE     := github.com/NIKX-Tech/relayly
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X $(MODULE)/pkg/version.Version=$(VERSION) \
              -X $(MODULE)/pkg/version.Commit=$(COMMIT) \
              -X $(MODULE)/pkg/version.BuildTime=$(BUILD_TIME)

.PHONY: all build run test lint vet clean docker docker-up docker-down deps

all: build

## build: Compile the binary
build:
	@echo "→ Building $(BINARY) $(VERSION)..."
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/relayly

## run: Run the relay server locally
run: build
	./$(BINARY) start

## test: Run all tests
test:
	go test -v -race ./...

## vet: Run go vet
vet:
	go vet ./...

## lint: Run golangci-lint (must be installed)
lint:
	golangci-lint run ./...

## deps: Download and tidy dependencies
deps:
	go mod download
	go mod tidy

## clean: Remove build artifacts
clean:
	rm -f $(BINARY)
	rm -rf dist/

## docker: Build Docker image
docker:
	docker build -t relayly:$(VERSION) .

## docker-up: Start via docker compose
docker-up:
	docker compose up --build -d

## docker-down: Stop docker compose stack
docker-down:
	docker compose down

## help: Print this help
help:
	@echo "Available targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':'

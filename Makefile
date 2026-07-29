## Default target
.DEFAULT_GOAL := help

# Project variables
BINARY   := llm-api-test
PKG      := ./cmd/llm-api-test
GOFLAGS  := -trimpath
GOBIN    := $(shell go env GOBIN)
PREFIX   ?= /usr/local

## help: print available targets
.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make <target>\n\nTargets:\n"} \
	/^[a-zA-Z_-]+:.*##/ { printf "  %-12s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

## build: compile the binary into ./$(BINARY)
.PHONY: build
build: ## Build the binary
	go build $(GOFLAGS) -o $(BINARY) $(PKG)

## fmt: run go fmt on all packages
.PHONY: fmt
fmt: ## Format Go source (go fmt)
	go fmt ./...

## install: install the binary into GOBIN (or $(PREFIX)/bin if unset)
.PHONY: install
install: build ## Install binary to GOBIN or $(PREFIX)/bin
	@if [ -n "$(GOBIN)" ]; then \
		install -d "$(GOBIN)"; \
		install -m 0755 $(BINARY) "$(GOBIN)/$(BINARY)"; \
		echo "installed -> $(GOBIN)/$(BINARY)"; \
	else \
		install -d "$(PREFIX)/bin"; \
		install -m 0755 $(BINARY) "$(PREFIX)/bin/$(BINARY)"; \
		echo "installed -> $(PREFIX)/bin/$(BINARY)"; \
	fi

## run: build then run all cases (pass ARGS=... to filter, e.g. make run ARGS='responses-basic')
.PHONY: run
run: build ## Build and run cases (ARGS="case1 case2")
	./$(BINARY) run $(ARGS)

## list: build then list available cases
.PHONY: list
list: build ## List available cases
	./$(BINARY) list

## vet: run go vet
.PHONY: vet
vet: ## Run go vet
	go vet ./...

## test: run go test
.PHONY: test
test: ## Run go tests
	go test ./...

## tidy: run go mod tidy
.PHONY: tidy
tidy: ## Tidy module dependencies
	go mod tidy

## clean: remove the built binary
.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY)

.PHONY: all web css generate build docker-build clean dev test test-web test-short lint vet fmt deps watch help

BINARY := /tmp/s3-server
WEB_DIR := web
GO := go
TEMPL := $(GO) tool templ

# --- Full build pipeline ---

# Full build: deps → frontend bundle → CSS → generate → Go binary
all: web css generate build

# --- Dependencies ---

deps:
	bun install
	$(GO) mod download

# --- Frontend ---

# Install deps and build Vite bundle (sodium, sia SDK, ky → web/dist)
web:
	cd $(WEB_DIR) && bun run build

# Build minified Tailwind v4 CSS — @source directives in input.css handle content scanning
css:
	bun run build:css

# --- Code generation ---

# Generate build metadata (s3d version from go.mod) + templ files
generate:
	$(GO) generate ./internal/build
	$(TEMPL) generate

# --- Go build ---

build:
	$(GO) build -o $(BINARY) ./cmd/s3-server/

# Build Docker image
docker-build:
	docker compose build

# --- Development ---

# Watch mode: templ + Go hot reload (requires air: go install github.com/air-verse/air@latest)
watch:
	$(TEMPL) generate && air -start

# Quick dev: build everything and run
dev: css web generate build
	$(BINARY) serve --listen-addr 0.0.0.0:8080 --data-dir /tmp/s3d-test-data

# --- Testing ---

test:
	$(GO) test -count=1 -race ./...

test-web:
	cd $(WEB_DIR) && bun run test

test-short:
	$(GO) test -count=1 -short ./...

# --- Linting & formatting ---

fmt:
	$(GO) fmt ./...
	$(TEMPL) fmt .

vet:
	$(GO) vet ./...

lint: vet
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run ./... || echo "golangci-lint not installed, skipping"

# --- Cleanup ---

clean:
	rm -f $(BINARY)
	rm -rf /tmp/s3d-test-data
	rm -rf internal/views/web/dist
	rm -f internal/views/*_templ.go
	rm -f internal/build/build_gen.go

# --- Help ---

help:
	@echo "s3-server build targets:"
	@echo "  all       - Full build: web + css + generate + build"
	@echo "  deps      - Install JS + Go dependencies"
	@echo "  web       - Build Vite frontend bundle"
	@echo "  css       - Build minified Tailwind CSS"
	@echo "  generate  - Run go:generate + templ generate"
	@echo "  build     - Build Go binary"
	@echo "  dev       - Full build + run locally"
	@echo "  watch     - Hot reload mode (requires air)"
	@echo "  test       - Run Go tests with race detector"
	@echo "  test-web   - Run frontend vitest tests (MSW + happy-dom)"
	@echo "  test-short - Run Go tests (short mode, skip integration)"
	@echo "  fmt       - Format Go + templ files"
	@echo "  vet       - Run go vet"
	@echo "  lint      - Run golangci-lint (if installed)"
	@echo "  clean     - Remove binary, dist, generated files"

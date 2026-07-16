.PHONY: all web css generate build docker-build clean dev test test-web test-short test-all test-browser bench cover lint vet fmt tsc deps watch help

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
	$(BINARY) serve --listen-addr 0.0.0.0:8080 --data-dir /tmp/s3-server-test-data

# --- Testing ---

# Go tests with race detector
test:
	TEST_SECRET_KEY=test-key-not-for-prod TEST_ACCESS_KEY=test-key-not-for-prod $(GO) test -count=1 -race ./...

# Frontend tests (real Chromium via Playwright)
test-web: test-browser

# Go tests in short mode (skip integration)
test-short:
	$(GO) test -count=1 -short ./...

# All tests: Go + browser
test-all: test test-browser

# Browser tests with real Chromium via Playwright
test-browser:
	cd $(WEB_DIR) && PATH="$$HOME/.local/bin:$$PATH" PLAYWRIGHT_BROWSERS_PATH=$$HOME/.cache/ms-playwright bunx vitest run --config vitest.browser.config.ts browser/

# Go benchmarks with memory allocation stats
bench:
	$(GO) test -bench=. -benchmem ./...

# Go test coverage (collect only, no gate)
cover:
	$(GO) test -count=1 -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1
	@echo "Coverage report: coverage.out (run 'go tool cover -html=coverage.out' to view)"

# TypeScript type check
tsc:
	cd $(WEB_DIR) && bun x tsc --noEmit

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
	rm -rf /tmp/s3-server-test-data
	rm -rf internal/views/web/dist
	rm -f internal/views/*_templ.go
	rm -f internal/build/build_gen.go

# --- Help ---

help:
	@echo "s3-server build targets:"
	@echo "  all          - Full build: web + css + generate + build"
	@echo "  deps         - Install JS + Go dependencies"
	@echo "  web          - Build Vite frontend bundle"
	@echo "  css          - Build minified Tailwind CSS"
	@echo "  generate     - Run go:generate + templ generate"
	@echo "  build        - Build Go binary"
	@echo "  dev          - Full build + run locally"
	@echo "  watch        - Hot reload mode (requires air)"
	@echo "  test         - Run Go tests with race detector"
	@echo "  test-web     - Run frontend tests (real Chromium via Playwright)"
	@echo "  test-short   - Run Go tests (short mode, skip integration)"
	@echo "  test-all     - Run Go + web unit tests"
	@echo "  test-browser - Run browser tests with real Chromium (Playwright)"
	@echo "  bench        - Run Go benchmarks with memory stats"
	@echo "  cover        - Run Go tests with coverage report"
	@echo "  tsc          - TypeScript type check"
	@echo "  fmt          - Format Go + templ files"
	@echo "  vet          - Run go vet"
	@echo "  lint         - Run golangci-lint (if installed)"
	@echo "  clean        - Remove binary, dist, generated files"

# AGENTS.md

Guidance for AI coding agents working in this repository.

## Build Commands

```bash
make all          # Full build: deps → web → css → generate → build
make deps         # Install JS (bun) + Go dependencies
make web          # Vite frontend bundle → internal/views/web/dist/
make css          # Tailwind CSS → internal/views/css/tailwind.css
make generate     # go:generate + templ generate
make build        # Compile Go binary
make test         # Go tests with -race
make test-browser # Vitest browser tests (real Chromium via Playwright)
make test-all     # Go + browser tests combined
make test-short   # Go tests in -short mode
make bench        # Go benchmarks with memory allocation stats
make cover        # Go test coverage report
make tsc          # TypeScript type check (zero errors required)
make vet          # go vet
make fmt          # go fmt + templ fmt
make lint         # golangci-lint (if installed)
make clean        # Remove binary, dist, generated files
make dev          # Full build + run locally
make watch        # Hot reload (requires air)
```

## Before Pushing

Run these and ensure all pass:

```bash
make generate    # Regenerate templ + build metadata
make build
make vet
make test
make fmt          # Format Go + templ
```

Also run `templ fmt .` and verify `git diff --exit-code` passes (CI checks this).

## Project Structure

```
cmd/s3-server/        CLI entry point
internal/
  admin/              s3d admin API client
  api/                JSON API response helpers and error types
  auth/               Session auth (cookie + bcrypt)
  backend/            s3d factory, store adapter, S3 handler swapper
  build/              Build metadata (generated)
  config/             koanf config: YAML + env vars, panel.yml schema
  handlers/           Panel HTTP handlers + SSE broker interface
  onboarding/         First-run FSM (robot3)
  routes/             Route registration
  sse/                Server-Sent Events broker
  ssl/                TLS config (none, platform, managed/ACME)
  status/             Backend health
  store/              Panel config + session + access key persistence
  updater/            Update sidecar via flag files
  version/            Version utilities
  views/              Templ HTML templates
web/                  Frontend (Vite: Alpine.js, htmx, ky, robot3, libsodium)
```

## Key Interfaces

- **`backend.Factory`** — creates s3d components (store, ASP, logger, S3 handler)
- **`handlers.S3Swapper`** — thread-safe swappable `http.Handler` for the S3 backend
- **`handlers.BackendRestarter`** — restarts s3d after config changes
- **`handlers.SSEBroker`** — SSE notifications (key changes, bucket changes)
- **`handlers.UpdaterManager`** — update sidecar communication
- **`store.Store`** — panel config, sessions, access key persistence

## Coding Conventions

- Go: `&Type{}` not `new(Type)`. No `_ = var` for ignored errors — use `func() { _ = ... }()` in defers.
- Use `samber/lo` for map/filter/reduce in production code (tests can stay explicit for clarity).
- `sync.RWMutex` with `*Locked()` no-lock variants for call sites already holding the write lock.
- Templ for all HTML — no SPA. Alpine.js + htmx for client interactivity.
- Tailwind CSS v4 via `@source` directives in `input.css`.
- Conventional commits: `feat:`, `fix:`, `refactor:`, `docs:`, `ci:`, `chore:`.
- Single squashed commit per PR. Branch name reflects scope (e.g., `refactor/lo-map-filter`).

## Generated Files (Do Not Edit)

These are gitignored and rebuilt by `make generate` / `make web` / `make css`:

- `internal/views/*_templ.go` — generated from `.templ` files
- `internal/views/css/tailwind.css` — built from `input.css`
- `internal/views/web/dist/` — Vite bundle output
- `internal/build/build_gen.go` — s3d version from go.mod

## Testing

- Go tests use `testify` (assert/require). Race detector is on (`-race`).
- Frontend tests use Vitest browser mode with real Chromium (Playwright). No happy-dom or MSW.
- Mocks live in `internal/*/mocks/` directories.
- Flaky tests: fix root cause, never skip.

## Dependencies

- **Bun** is the package manager (not p/npm). `bun.lock` is tracked.
- **Go 1.26** with toolchain go1.26.3.
- **CGO is required** (SQLite via s3d). Ensure GCC is available.
- **templ CLI**: `go install github.com/a-h/templ/cmd/templ@latest`

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
make test         # Go tests with -race (TEST_SECRET_KEY env required)
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
make tsc         # TypeScript type check
make fmt         # Format Go + templ
```

Also run `templ fmt .` and verify `git diff --exit-code` passes (CI checks this).

## Project Structure

```
cmd/s3-server/        CLI entry point (serve command, --data-dir, --listen-addr flags)
internal/
  admin/              s3d admin API client (Prometheus metrics, stats, system endpoints)
  api/                JSON API response helpers and typed error categories
  auth/               Session auth (cookie + bcrypt), HTTP Basic auth for admin endpoints
  backend/            s3d factory, store adapter, S3 handler swapper, backend manager
  build/              Build metadata (s3d version from go.mod, generated)
  config/             koanf config: YAML + env vars, panel.yml schema, indexer contrast colors
  handlers/           Panel HTTP handlers (keys, users, buckets, backups, pages, SSE, config APIs)
  onboarding/         First-run FSM (robot3): pending → admin_password_set → app_key_set → complete
  routes/             Route constants, no middleware logic
  sse/                Server-Sent Events broker, stats fetcher
  ssl/                TLS config (none, platform, managed/ACME), bucket cert middleware
  status/             Backend health status tracking
  store/              Panel config persistence (YAML), sessions, access key store
  testutil/           Shared test helpers (test logger)
  updater/            Update sidecar communication via flag files
  version/            Version checker (polls GitHub releases, notifies panel, never self-applies)
  views/              Templ HTML templates + embedded assets (CSS, JS, WASM)
web/                  Frontend (Vite: Alpine.js, ky, robot3, libsodium, Tailwind CSS v4)
```

## Key Interfaces

- **`backend.Factory`** — creates s3d components (store, ASP, logger, S3 handler)
- **`backend.Manager`** — owns s3d lifecycle, provides `Backend()`, `KeyStore()`, `Status()`, `InitFromConfig()`
- **`backend.Backend`** — running s3d instance
- **`backend.S3DStore`** — s3d store adapter (access keys, buckets)
- **`handlers.S3Swapper`** — thread-safe swappable `http.Handler` for the S3 backend
- **`handlers.BackendRestarter`** — restarts s3d after config changes
- **`handlers.SSEBroker`** — SSE notifications (key changes, bucket changes, stats, init errors)
- **`handlers.UpdaterManager`** — update sidecar communication
- **`handlers.LogLevelUpdater`** — hot-reloads zap log level via `SetLevel()`
- **`store.Store`** — panel config, sessions, access key persistence

## Coding Conventions

- Go: `&Type{}` not `new(Type)`. No `_ = var` for ignored errors — use `func() { _ = ... }()` in defers.
- Use `samber/lo` for map/filter/reduce in production code (tests can stay explicit for clarity).
- `sync.RWMutex` with `*Locked()` no-lock variants for call sites already holding the write lock.
- Templ for all HTML — no SPA. Alpine.js for client interactivity (x-data, x-show, x-for).
- Tailwind CSS v4 via `@source` directives in `input.css`.
- Conventional commits: `feat:`, `fix:`, `refactor:`, `docs:`, `ci:`, `chore:`. No scope.
- Single squashed commit per PR. Branch name reflects scope (e.g., `fix/docker-data-dir`).
- CSRF on all POST/PUT/DELETE forms — Echo CSRF middleware with `form:_csrf` token lookup. Templates render hidden `_csrf` input; frontend JS includes it in `FormData`.

## HTTP Routing

The server composes two routers via `http.ServeMux`:

- `/_panel/` → Echo (panel UI, API, auth, onboarding, SSE, static assets)
- `/` → S3 handler (all S3 API requests)
- `/prometheus`, `/stats/`, `/system/` → s3d admin handler (HTTP Basic auth)

Panel routes are registered via `handlers.RegisterRoutes()` using `routes.Panel*` constants.

## Generated Files (Do Not Edit)

These are gitignored and rebuilt by `make generate` / `make web` / `make css`:

- `internal/views/*_templ.go` — generated from `.templ` files
- `internal/views/css/tailwind.css` — built from `input.css`
- `internal/views/web/dist/` — Vite bundle output
- `internal/build/build_gen.go` — s3d version from go.mod

## Testing

- Go tests use `testify` (assert/require). Race detector is on (`-race`).
- `TEST_SECRET_KEY` env var is required for handler tests (set in Makefile and CI).
- Frontend tests use Vitest browser mode with real Chromium (Playwright). No happy-dom or MSW.
- `vi.mock()` for relative-path modules does NOT work in browser mode. Use window global stubs (`__sseToast`, `__copyToClipboard`, `__reloadAfter`) instead.
- `window.location` is non-configurable in real Chromium — use `vi.spyOn(window, 'setTimeout')` + timer cleanup in `afterEach`.
- Mocks live in `internal/*/mocks/` directories, generated by mockery v3.
- Mockery v3 generates one file per interface (e.g. `Backend.go`) via `filename: '{{.InterfaceName}}.go'`. Since these mocks are imported across test packages, they are non-test `.go` files, not `_test.go`.
- Config is managed via `.mockery.yaml` at the repo root.
- Flaky tests: fix root cause, never skip.

## Dependencies

- **Bun** is the package manager (not p/npm). `bun.lock` is tracked.
- **Go 1.26** with toolchain go1.26.3.
- **CGO is required** (SQLite via s3d). Ensure GCC is available.
- **templ CLI**: `go install github.com/a-h/templ/cmd/templ@latest`
- **mockery v3**: `go install github.com/vektra/mockery/v2/cmd/mockery@latest` (installs v3)
- **air** (optional): `go install github.com/air-verse/air@latest` for watch mode

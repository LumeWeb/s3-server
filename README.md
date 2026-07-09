# s3-server

Private S3-compatible storage server backed by the Sia Network. Ships an S3 API, a web admin panel, onboarding flow, and automatic TLS — all in a single binary.

## Features

- **S3-compatible API** — backed by [s3d](https://github.com/SiaFoundation/s3d) and the Sia storage network
- **Web admin panel** — server-rendered Go templates with Alpine.js + htmx for interactivity
- **Onboarding wizard** — first-run setup with admin password, S3 access key generation, and indexer configuration
- **Access key management** — create/delete S3 access keys, group by user, real-time updates via SSE
- **Bucket lifecycle** — view buckets, configure versioning and lifecycle rules
- **Backup & restore** — create and download database backups from the panel
- **Automatic TLS** — Let's Encrypt via Caddy integration, or bring your own certificates
- **Update sidecar** — optional auto-update mechanism via flag files on a mounted state volume
- **Single binary** — Go backend with embedded frontend assets, no external runtime dependencies

## Architecture

```
cmd/s3-server/        CLI entry point (serve command, flags)
internal/
  admin/              s3d admin API client
  api/                JSON API response helpers and error types
  auth/               Session-based panel authentication (cookie + bcrypt)
  backend/            s3d factory, store adapter, S3 handler swapper
  build/              Build metadata (s3d version from go.mod)
  config/             YAML + env config (koanf), panel.yml schema
  handlers/           Panel HTTP handlers (keys, buckets, backups, pages, SSE)
  onboarding/         First-run setup state machine (robot3 FSM)
  routes/             Route registration and middleware wiring
  sse/                Server-Sent Events broker for real-time panel updates
  ssl/                TLS configuration (none, platform, managed/ACME)
  status/             Backend health status tracking
  store/              Panel config persistence, sessions, access key store
  updater/            Update sidecar communication via flag files
  version/            Version utilities
  views/              Templ templates (server-rendered HTML)
web/                  Frontend source (Vite bundle: Alpine.js, htmx, ky, robot3, libsodium)
```

## Tech Stack

**Backend:** Go 1.26, Echo v5, templ, s3d, SQLite (via s3d), koanf, zap, samber/lo

**Frontend:** Bun, Vite, Tailwind CSS v4, Alpine.js, htmx, ky, robot3, libsodium

**Infrastructure:** Docker (multi-stage build), GitHub Actions CI

## Getting Started

### Prerequisites

- [Go 1.26+](https://go.dev/dl/)
- [Bun](https://bun.sh/) (package manager + bundler)
- [templ CLI](https://github.com/a-h/templ) (`go install github.com/a-h/templ/cmd/templ@latest`)
- GCC (CGO required for SQLite)

### Build

```bash
make all    # deps → frontend bundle → CSS → generate → Go binary
```

Or step-by-step:

```bash
make deps       # Install JS + Go dependencies
make web        # Build Vite frontend bundle → internal/views/web/dist/
make css        # Build minified Tailwind CSS → internal/views/css/tailwind.css
make generate   # Run go:generate + templ generate
make build      # Compile Go binary → /tmp/s3-server
```

### Run

```bash
make dev    # Full build + run with test data directory
```

Or manually:

```bash
/tmp/s3-server serve --listen-addr 0.0.0.0:8080 --data-dir /tmp/s3-server-test-data
```

First run triggers the onboarding wizard at `http://localhost:8080`.

### Docker

```bash
docker compose up -d    # Builds and starts on port 8080
```

## Configuration

Configuration is loaded from `panel.yml` in the data directory (default: `/var/lib/s3-server`), with environment variable overrides using the `S3_SERVER_` prefix and `__` delimiter.

Example `panel.yml`:

```yaml
onboarding_state: ""
log:
  format: json
  level: info
s3:
  directory: /var/lib/s3-server
  indexer_url: https://sia.pinner.xyz
  available_indexers:
    - https://sia.pinner.xyz
    - https://sia.storage
  host_bases: []
ssl:
  mode: none
  acme_email: ""
  acme_dir_url: ""
```

### Environment Variables

All config keys map to environment variables with `S3_SERVER_` prefix and `__` for nested keys:

| Key | Env Var |
|---|---|
| `s3.directory` | `S3_SERVER_S3__DIRECTORY` |
| `s3.indexer_url` | `S3_SERVER_S3__INDEXER_URL` |
| `log.level` | `S3_SERVER_LOG__LEVEL` |
| `ssl.mode` | `S3_SERVER_SSL__MODE` |

## Testing

### Backend

```bash
make test          # Go tests with race detector
make test-short    # Short mode (skip integration tests)
make vet           # go vet
```

### Frontend

```bash
make test-web      # Vitest with happy-dom + MSW
```

## Development

### Watch Mode

```bash
make watch    # Hot reload Go + templ (requires air: go install github.com/air-verse/air@latest)
```

### Formatting

```bash
make fmt    # go fmt + templ fmt
```

### Cleaning Generated Files

```bash
make clean    # Remove binary, dist, generated templ/build files
```

## CI

GitHub Actions runs 4 parallel jobs on every push to `develop` and every PR:

| Job | Description |
|---|---|
| **Backend** | CSS build → templ generate → go build → go vet → go test -race |
| **Frontend** | bun install → vite build → tailwind CSS → vitest |
| **Lint** | golangci-lint v2 + templ fmt check |
| **Docker** | Multi-stage buildx build with GHA layer cache |

## Project Conventions

- **Single squashed commit per PR** — branch per logical change, branch name reflects scope
- **Generated files are gitignored** — `*_templ.go`, `tailwind.css`, `web/dist/`, `build_gen.go` are NOT tracked
- **Bun for frontend** — not p/npm; `bun.lock` IS tracked
- **templ for HTML** — server-rendered, no SPA framework
- **samber/lo** for map/filter/reduce patterns in production Go code
- **Conventional commits** — `feat:`, `fix:`, `refactor:`, `docs:`, `ci:`, `chore:`
- **Go interfaces for testability** — `SSEBroker`, `S3Swapper`, `BackendRestarter`, `UpdaterManager` enable mocking
- **RWMutex for concurrent access** — key management uses `sync.RWMutex` with `*Locked()` no-lock variants to prevent self-deadlock

## License

MIT

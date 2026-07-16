# s3-server

Private S3-compatible storage server backed by the Sia Network. Ships an S3 API, a web admin panel, onboarding flow, and automatic TLS — all in a single binary.

## Features

- **S3-compatible API** — backed by [s3d](https://github.com/SiaFoundation/s3d) and the Sia storage network
- **Web admin panel** — server-rendered Go templates with Alpine.js for interactivity
- **Onboarding wizard** — first-run setup with admin password, S3 access key generation, and indexer configuration
- **Access key management** — create/delete S3 access keys, group by user, real-time updates via SSE
- **User management** — create/delete users, assign access keys per user
- **Bucket lifecycle** — view buckets, configure versioning and lifecycle rules
- **Backup & restore** — create and download database backups from the panel
- **Automatic TLS** — Let's Encrypt via Caddy integration, or bring your own certificates
- **Update sidecar** — optional auto-update mechanism via flag files on a mounted state volume
- **Single binary** — Go backend with embedded frontend assets, no external runtime dependencies

## Architecture

```
cmd/s3-server/        CLI entry point (serve command, flags)
internal/
  admin/              s3d admin API client (Prometheus, stats, system)
  api/                JSON API response helpers and error types
  auth/               Session-based panel authentication (cookie + bcrypt)
  backend/            s3d factory, store adapter, S3 handler swapper, backend manager
  build/              Build metadata (s3d version from go.mod, generated)
  config/             YAML + env config (koanf), panel.yml schema
  handlers/           Panel HTTP handlers (keys, users, buckets, backups, pages, SSE, config)
  onboarding/         First-run setup state machine (robot3 FSM)
  routes/             Route constants and middleware wiring
  sse/                Server-Sent Events broker for real-time panel updates
  ssl/                TLS configuration (none, platform, managed/ACME)
  status/             Backend health status tracking
  store/              Panel config persistence, sessions, access key store
  testutil/           Shared test helpers (test logger)
  updater/            Update sidecar communication via flag files
  version/            Version checker (notifies on new releases, never self-applies)
  views/              Templ templates (server-rendered HTML)
web/                  Frontend source (Vite bundle: Alpine.js, ky, robot3, libsodium)
```

## Tech Stack

**Backend:** Go 1.26, Echo v5, templ, s3d, SQLite (via s3d), koanf, zap, samber/lo

**Frontend:** Bun, Vite, Tailwind CSS v4, Alpine.js, ky, robot3, libsodium

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

### Docker — Production

Uses the published image from GitHub Container Registry:

```bash
docker compose up -d    # Pulls ghcr.io/lumeweb/s3-server:latest, starts on port 8080
```

To pin a specific version:

```yaml
# docker-compose.yml override or env
image: ghcr.io/lumeweb/s3-server:develop    # track develop branch
image: ghcr.io/lumeweb/s3-server:sha-abc123 # specific commit
```

### Docker — Local Development

Builds from source using the Dockerfile (multi-stage: bun frontend → Go build → runtime):

```bash
docker compose -f docker-compose.dev.yml up -d --build
```

CI publishes images to GHCR on every push to `develop`. The following tags are available:

| Tag | Description |
|---|---|
| `latest` | Latest develop build |
| `develop` | Same as latest, explicit |
| `sha-<short>` | Specific commit hash |

## Configuration

Configuration is loaded from `panel.yml` in the data directory (default: `/var/lib/s3-server`, or `/data` in Docker), with environment variable overrides using the `S3_SERVER_` prefix and `__` delimiter.

Example `panel.yml`:

```yaml
onboarding_state: ""
platform_id: ""
log:
  format: json
  level: info
s3:
  directory: /var/lib/s3-server
  indexer_url: https://sia.pinner.xyz
  available_indexers:
    - url: https://sia.pinner.xyz
      name: Pinner
      description: "Our indexer, our support"
      logo: pinner
      brand_color: "#12A596"
    - url: https://sia.storage
      name: Sia Storage
      description: "Are you already using Sia Storage? Connect here."
      logo: sia-storage
      brand_color: "#EFF2ED"
  host_bases: [] # S3 Domains: comma-separated domains for virtual-hosted-style URLs (bucket.example.com)
  disk_usage_limit: 0
  upload_waste_pct: 0.1
ssl:
  mode: none
  acme_email: ""
  acme_dir_url: ""
```

### CLI Flags

| Flag | Env Var | Default | Description |
|---|---|---|---|
| `--data-dir` | `S3_SERVER_DATA_DIR` | `/var/lib/s3-server` | Data directory for panel config and S3 metadata |
| `--listen-addr` | `S3_SERVER_LISTEN_ADDR` | `:8080` | Address to listen on |

### Environment Variables

All config keys map to environment variables with `S3_SERVER_` prefix and `__` for nested keys:

| Key | Env Var |
|---|---|
| `s3.directory` | `S3_SERVER_S3__DIRECTORY` |
| `s3.indexer_url` | `S3_SERVER_S3__INDEXER_URL` |
| `s3.host_bases` | `S3_SERVER_S3__HOST_BASES` (comma-separated) |
| `s3.host_bases` | `S3_SERVER_S3__DOMAINS` (comma-separated, alias) |
| `log.level` | `S3_SERVER_LOG__LEVEL` |
| `log.format` | `S3_SERVER_LOG__FORMAT` |
| `ssl.mode` | `S3_SERVER_SSL__MODE` |
| `ssl.acme_email` | `S3_SERVER_SSL__ACME_EMAIL` |

### SSL Modes

| Mode | Description |
|---|---|
| `none` | No TLS (HTTP only, default) |
| `platform` | TLS terminated by external load balancer/proxy |
| `managed` | Automatic TLS via Let's Encrypt (ACME), HTTP on :80 redirects to HTTPS on :443 |

When `managed` and `s3.host_bases` are configured, bucket creation triggers eager cert issuance for `{bucket}.{hostbase}`.

## Testing

### Backend (Go)

```bash
make test          # Go tests with race detector
make test-short    # Short mode (skip integration tests)
make bench         # Go benchmarks with memory allocation stats
make cover         # Go test coverage report
```

### Frontend (Browser)

```bash
make test-browser  # Vitest browser tests (real Chromium via Playwright)
make tsc            # TypeScript type check (zero errors required)
```

All frontend tests run in real Chromium via Playwright — no happy-dom, no MSW. The test suite covers API calls, SSE, onboarding flow, page rendering, and component behavior.

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

GitHub Actions runs 6 jobs on every push to `develop` (5 on PRs):

| Job | Description |
|---|---|
| **Backend** | CSS build → frontend build → templ generate → go build → vet → test -race → coverage |
| **Browser** | bun install → vite build → CSS → tsc → Playwright install → vitest run |
| **Benchmarks** | Full build → benchmarks with memory stats |
| **Lint** | golangci-lint v2 + templ fmt check |
| **Docker Build** | Multi-stage buildx build with GHA layer cache (PRs only, no push) |
| **Publish** | Builds and pushes image to GHCR with `latest`, `develop`, and `sha-<short>` tags (develop pushes only) |

## Project Conventions

- **Single squashed commit per PR** — branch per logical change, branch name reflects scope
- **Generated files are gitignored** — `*_templ.go`, `tailwind.css`, `web/dist/`, `build_gen.go` are NOT tracked
- **Bun for frontend** — not p/npm; `bun.lock` IS tracked
- **templ for HTML** — server-rendered, no SPA framework
- **samber/lo** for map/filter/reduce patterns in production Go code
- **Conventional commits** — `feat:`, `fix:`, `refactor:`, `docs:`, `ci:`, `chore:`
- **Go interfaces for testability** — `Backend`, `S3DStore`, `S3Swapper`, `Factory`, `SSEBroker`, `UpdaterManager`, `Store` enable mocking via mockery
- **RWMutex for concurrent access** — key management uses `sync.RWMutex` with `*Locked()` no-lock variants to prevent self-deadlock
- **CSRF on all forms** — Echo CSRF middleware with `form:_csrf` lookup; templates render hidden `_csrf` input

## License

MIT

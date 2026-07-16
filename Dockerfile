# syntax=docker/dockerfile:1

# Frontend build stage
FROM oven/bun:1 AS frontend

WORKDIR /src
COPY package.json bun.lock ./
COPY web/ web/
COPY internal/views/ internal/views/
RUN bun install
RUN cd web && bun run build
RUN bun run build:css

# Build stage
FROM golang:1.26-bookworm AS builder

WORKDIR /src

# Cache deps
COPY go.mod go.sum ./
RUN go mod download

# Copy source + generated frontend assets
COPY . .
COPY --from=frontend /src/internal/views/web/dist/ internal/views/web/dist/
COPY --from=frontend /src/internal/views/css/tailwind.css internal/views/css/

# Generate templ + build metadata
RUN go install github.com/a-h/templ/cmd/templ@latest
RUN go generate ./internal/build
RUN templ generate

ARG VERSION=dev
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w -X main.appVersion=${VERSION}" \
    -o /bin/s3-server ./cmd/s3-server

# Runtime stage
FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /bin/s3-server /usr/local/bin/s3-server

# Data directory — use /data as the convention in Docker
ENV S3_SERVER_DATA_DIR=/data
RUN useradd -r -u 1000 -d /data -s /usr/sbin/nologin s3server
VOLUME /data

USER 1000:1000

EXPOSE 8080

ENTRYPOINT ["s3-server"]
CMD ["serve"]

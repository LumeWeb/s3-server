# syntax=docker/dockerfile:1

# Build stage
FROM golang:1.26-bookworm AS builder

WORKDIR /src

# Cache deps
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w -X main.appVersion=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
    -o /bin/s3-server ./cmd/s3-server

# Runtime stage
FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /bin/s3-server /usr/local/bin/s3-server

# Data directory
RUN useradd -r -u 1000 -d /var/lib/s3-server -s /usr/sbin/nologin s3server && \
    mkdir -p /var/lib/s3-server && chown 1000:1000 /var/lib/s3-server

USER 1000:1000

EXPOSE 8080

ENTRYPOINT ["s3-server"]
CMD ["serve"]

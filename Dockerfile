# syntax=docker/dockerfile:1

# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.26-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
        libgme-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o /chipfs ./cmd/chipfs

# ── Production image ──────────────────────────────────────────────────────────
FROM debian:bookworm-slim AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
        libgme0 \
        fuse3 \
    && rm -rf /var/lib/apt/lists/*

# Allow FUSE mounts inside the container (required when running with
# --cap-add SYS_ADMIN --device /dev/fuse).
RUN sed -i 's/#user_allow_other/user_allow_other/' /etc/fuse.conf 2>/dev/null || true

COPY --from=builder /chipfs /usr/local/bin/chipfs

ENTRYPOINT ["chipfs"]

# ── Smoke-test image ──────────────────────────────────────────────────────────
# Build with: docker build --target smoke-test -t chipfs-smoke .
# Run with:   docker run --rm --cap-add SYS_ADMIN --device /dev/fuse chipfs-smoke
FROM runtime AS smoke-test

RUN apt-get update && apt-get install -y --no-install-recommends \
        ffmpeg \
    && rm -rf /var/lib/apt/lists/*

COPY testdata/fixtures /testdata/fixtures
COPY scripts/smoke-test.sh /smoke-test.sh
RUN chmod +x /smoke-test.sh

ENTRYPOINT ["/smoke-test.sh"]

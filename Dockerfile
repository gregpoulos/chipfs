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

# ── Navidrome integration test ────────────────────────────────────────────────
# Runs chipfs and Navidrome in the same container so the FUSE mount is visible
# to both without any cross-container mount propagation gymnastics.
#
# Build: docker build --target navidrome-test -t chipfs-navidrome .
# Run:   docker run --rm --cap-add SYS_ADMIN --device /dev/fuse \
#                   --security-opt apparmor:unconfined \
#                   -p 4533:4533 chipfs-navidrome
# Then open http://localhost:4533, create an admin account, and verify that
# Artist / Album / Title tags are populated correctly for the fixture files.
FROM runtime AS navidrome-test

# Pull the Navidrome binary from the official image — no download at build time.
COPY --from=deluan/navidrome:latest /app/navidrome /usr/local/bin/navidrome

ENV ND_MUSICFOLDER=/mnt/chipfs \
    ND_DATAFOLDER=/data \
    ND_LOGLEVEL=info \
    ND_SCANSCHEDULE="@every 10s" \
    ND_PORT=4533

COPY testdata/fixtures /chips
COPY scripts/navidrome-test-start.sh /navidrome-test-start.sh
RUN chmod +x /navidrome-test-start.sh

EXPOSE 4533
ENTRYPOINT ["/navidrome-test-start.sh"]

# ── Stage 1: Builder ──────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

# Install build essentials (pure-Go sqlite via modernc needs no C compiler)
RUN apk add --no-cache git make

WORKDIR /build

# Cache dependencies first
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build static binary
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w \
      -X github.com/NIKX-Tech/relayly/pkg/version.Version=${VERSION} \
      -X github.com/NIKX-Tech/relayly/pkg/version.Commit=${COMMIT} \
      -X github.com/NIKX-Tech/relayly/pkg/version.BuildTime=${BUILD_TIME}" \
    -o relayly ./cmd/relayly

# ── Stage 2: Final ────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="Relayly"
LABEL org.opencontainers.image.description="Lightweight self-hosted WebSocket relay for local-first apps"
LABEL org.opencontainers.image.source="https://github.com/NIKX-Tech/relayly"
LABEL org.opencontainers.image.licenses="MIT"

# Runtime data directory
VOLUME ["/data"]

COPY --from=builder /build/relayly /relayly

EXPOSE 8080 8081

ENTRYPOINT ["/relayly"]
CMD ["start"]

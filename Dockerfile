# syntax=docker/dockerfile:1
# Multi-stage build — final image is gcr.io/distroless/static:nonroot (~3 MB).
# Build for a specific platform:
#   docker build --platform linux/amd64 -t monita-agent:latest .
#   docker build --platform linux/arm64 -t monita-agent:latest .
# Or let BuildKit pick the target automatically with docker buildx.

FROM --platform=$BUILDPLATFORM golang:1.25 AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w" -o monita-agent ./cmd/monita-agent


# distroless/static:nonroot ships CA certificates (needed for HTTPS to the
# Collector) and runs as uid 65532 (nonroot) by default.
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /build/monita-agent /monita-agent

# The only directory the agent writes to is its state_dir (buffer + offsets).
# Mount a volume there; everything else is read-only.
VOLUME ["/var/lib/monita-agent"]

ENTRYPOINT ["/monita-agent"]
CMD ["--config", "/etc/monita-agent/config.yaml"]
# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache gzip

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY *.go ./
COPY templates/ ./templates/
COPY config/ ./config/
COPY static/ ./static/

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o nostr-server .

# Compress static assets
RUN gzip -k -9 -f static/helm.js static/style.css


# Runtime stage
FROM alpine:3.21

WORKDIR /app

# Install CA certificates for TLS connections to relays
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 nostr && \
    adduser -u 1000 -G nostr -s /bin/sh -D nostr

# Copy binary and assets from builder
COPY --from=builder /build/nostr-server .
COPY --from=builder /build/static/ ./static/
COPY --from=builder /build/config/ ./config/

# Set ownership
RUN chown -R nostr:nostr /app

USER nostr

# Default environment
ENV PORT=8080
ENV GZIP_ENABLED=1

EXPOSE 8080

# Health check using readiness endpoint
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:${PORT}/health/ready || exit 1

ENTRYPOINT ["./nostr-server"]

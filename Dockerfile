# Build stage
FROM golang:1.24-alpine@sha256:c694a4d291a13a9f9d94933395673494fc2cc9d4777b85df3a7e70b3492d3574 AS builder

# Install build dependencies and create user in single layer
RUN apk add --no-cache git=2.45.4-r0 ca-certificates=20240705-r0 tzdata=2025b-r0 && \
    adduser -D -g '' appuser

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build arguments for version info
ARG VERSION=dev
ARG BUILD_TIME
ARG GIT_COMMIT
ARG TARGETARCH

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-amd64} go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME} -X main.gitCommit=${GIT_COMMIT}" \
    -a -installsuffix cgo \
    -o vm-proxy-auth ./cmd/gateway

# Final stage
FROM scratch

# Copy certificates and timezone data from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy user from builder
COPY --from=builder /etc/passwd /etc/passwd

# Copy binary from builder
COPY --from=builder /app/vm-proxy-auth /vm-proxy-auth

# Copy example configurations
COPY --from=builder /app/examples /examples

# Use non-root user
USER appuser

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD ["/vm-proxy-auth", "--health-check"]

# Set entrypoint
ENTRYPOINT ["/vm-proxy-auth"]
CMD ["--config", "/examples/config.example.yaml"]

# Labels following OCI image spec
LABEL org.opencontainers.image.title="VM Proxy Auth"
LABEL org.opencontainers.image.description="VictoriaMetrics Proxy with Authentication and Multi-tenant Support"
LABEL org.opencontainers.image.url="https://github.com/edelwud/vm-proxy-auth"
LABEL org.opencontainers.image.source="https://github.com/edelwud/vm-proxy-auth"
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.created="${BUILD_TIME}"
LABEL org.opencontainers.image.revision="${GIT_COMMIT}"
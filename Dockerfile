# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install git and ca-certificates (needed for go mod download and HTTPS)
RUN apk add --no-cache git ca-certificates tzdata

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o prometheus-oauth-gateway \
    ./cmd/gateway

# Final stage
FROM alpine:3.19

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/prometheus-oauth-gateway .

# Copy example config (optional)
COPY --from=builder /app/config.example.yaml .

# Create a non-root user
RUN adduser -D -s /bin/sh gateway

# Change ownership and switch to non-root user
RUN chown -R gateway:gateway /root
USER gateway

EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["./prometheus-oauth-gateway"]
CMD ["--config", "config.example.yaml"]
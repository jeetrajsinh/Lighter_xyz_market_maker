# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.Version=$(git describe --tags --always --dirty)" \
    -o marketmaker \
    ./cmd/marketmaker

# Runtime stage
FROM alpine:3.18

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 -S marketmaker && \
    adduser -u 1000 -S marketmaker -G marketmaker

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/marketmaker /app/
COPY --from=builder /app/configs /app/configs

# Change ownership
RUN chown -R marketmaker:marketmaker /app

# Use non-root user
USER marketmaker

# Expose metrics port
EXPOSE 9090

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:9090/health || exit 1

# Run the binary
ENTRYPOINT ["/app/marketmaker"]
CMD ["-config", "/app/configs/config.json"]
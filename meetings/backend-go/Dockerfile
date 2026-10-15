# syntax=docker/dockerfile:1

FROM golang:1.24-bookworm AS builder

WORKDIR /build

# Copy go mod first for caching
COPY go.mod go.sum ./
RUN go mod download 2>/dev/null || true

# Copy all source files
COPY . .

# Build the binary with static linking
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /meeting-summarizer .

# Final stage: minimal alpine image
FROM alpine:3.21

RUN apk add --no-cache ca-certificates

WORKDIR /app

# Create data directories
RUN mkdir -p /data/uploads /data/exports /backend-go/frontend-static

# Copy the compiled binary and frontend static files
COPY --from=builder /meeting-summarizer /app/meeting-summarizer
COPY --from=builder /build/frontend-static/ /app/frontend-static/

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/api/health || exit 1

CMD ["/app/meeting-summarizer"]

# ---- Build stage ----
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=0.2.0
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags "-s -w -X main.version=${VERSION}" \
    -o /bin/openbot ./cmd/openbot

# ---- Runtime stage ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

# Non-root user for security
RUN addgroup -S openbot && adduser -S openbot -G openbot

WORKDIR /app

COPY --from=builder /bin/openbot /app/openbot

# Default config and workspace directories
RUN mkdir -p /home/openbot/.openbot/workspace && \
    chown -R openbot:openbot /home/openbot /app

USER openbot

# Web UI (8080) + API Gateway (9090)
EXPOSE 8080 9090

# Health check against the status endpoint
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/status || exit 1

ENTRYPOINT ["/app/openbot"]
CMD ["gateway"]

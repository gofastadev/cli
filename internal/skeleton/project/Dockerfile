# Production Dockerfile
# Multi-stage build: compiles a static binary, runs on minimal Alpine.
# Designed for VPS deployment with Docker — paired with `gofasta deploy`
# (which ships the built image over SSH and starts it via Docker Compose).
#
# Build:  docker build -t myapp .
# Run:    docker run -p 8080:8080 --env-file .env myapp

# ── Stage 1: Build ──
FROM golang:1.25.0-alpine AS builder

RUN apk add --no-cache git

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app ./app/main

# Install migrate CLI for the runner stage
RUN go install -tags 'postgres mysql sqlite3 sqlserver clickhouse' \
    github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1

# ── Stage 2: Run ──
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app /app
COPY --from=builder /go/bin/migrate /usr/local/bin/migrate
COPY db/migrations /migrations
COPY config.yaml /config.yaml
COPY templates /templates

ENV PORT=8080
EXPOSE 8080

ENTRYPOINT ["/app"]
CMD ["serve"]

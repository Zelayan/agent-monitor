# Multi-stage build for Agent Monitor
FROM golang:1.24-alpine AS builder

WORKDIR /src

# Copy dependency definition
COPY go.mod ./
# Download dependencies (none currently, but cache-friendly)
RUN go mod download

# Copy source code
COPY . .

# Compile static binaries
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/agent-monitor main.go
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/agent-reporter cmd/reporter/main.go

# Production minimal image
FROM alpine:3.21

WORKDIR /app

RUN apk --no-cache add ca-certificates tzdata

COPY --from=builder /bin/agent-monitor /usr/local/bin/agent-monitor
COPY --from=builder /bin/agent-reporter /usr/local/bin/agent-reporter
COPY configs /app/configs

ENV PORT=8000
ENV DATA_DIR=/app/data
VOLUME ["/app/data"]

EXPOSE 8000

ENTRYPOINT ["agent-monitor"]

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Stage 1 — Builder
# Uses the full Go image to compile the binary with
# all dependencies available.
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
FROM golang:1.25-alpine AS builder

# Install git (needed by some Go modules that pull via VCS)
RUN apk add --no-cache git

WORKDIR /app

# Cache dependency downloads in a separate layer so they
# are not re-downloaded on every code change.
COPY go.mod go.sum ./
RUN go mod download

# Install the swag CLI to generate Swagger docs at build time.
RUN go install github.com/swaggo/swag/cmd/swag@latest

# Copy the rest of the source.
COPY . .

# Generate Swagger documentation.
RUN swag init -g cmd/api/main.go --output docs

# Compile API
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /finager ./cmd/api

# Compile Seed
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /finager-seed ./cmd/seed

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Stage 2 — Runtime
# Minimal alpine image: includes CA certs and tzdata
# but nothing else, keeping the footprint tiny.
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
FROM alpine:3.21 AS runtime

# CA certificates are necessary for outbound TLS (e.g. MongoDB Atlas).
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy both binaries from builder
COPY --from=builder /finager .
COPY --from=builder /finager-seed .

EXPOSE 8080

# Execute the idempotent seed script first, then launch the API
ENTRYPOINT ["/bin/sh", "-c", "./finager-seed && ./finager"]

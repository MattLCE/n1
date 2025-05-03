FROM golang:1.23-alpine AS builder

# ---> ADD THIS LINE <---
# Install C build tools (gcc, make, etc.) needed for CGO
RUN apk add --no-cache build-base

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the application (needs build-base installed for CGO)
RUN CGO_ENABLED=1 GOOS=linux go build -o /app/bin/bosr ./cmd/bosr
RUN CGO_ENABLED=1 GOOS=linux go build -o /app/bin/mirord ./cmd/mirord

# Create a minimal runtime image
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata sqlite-libs

WORKDIR /app

# Copy the binaries from the builder stage
COPY --from=builder /app/bin/bosr /usr/local/bin/bosr
COPY --from=builder /app/bin/mirord /usr/local/bin/mirord

# Create data directory (this might be better handled by volume mounts in compose)
RUN mkdir -p /data

# Set the working directory to /data (Consider if /app or /usr/local/bin is better if CMD uses binaries directly)
WORKDIR /data

# Default command
# Note: Using mirord here, but vault1/vault2 services might need different CMDs?
# Consider if the CMD should be defined in docker-compose.yml per service instead.
CMD ["sh", "-c", "mirord --vault /data/vault.db --listen ${N1_LISTEN_ADDR} --verbose"]
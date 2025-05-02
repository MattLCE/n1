FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -o /app/bin/bosr ./cmd/bosr
RUN CGO_ENABLED=1 GOOS=linux go build -o /app/bin/mirord ./cmd/mirord

# Create a minimal runtime image
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata sqlite-libs

WORKDIR /app

# Copy the binaries from the builder stage
COPY --from=builder /app/bin/bosr /usr/local/bin/bosr
COPY --from=builder /app/bin/mirord /usr/local/bin/mirord

# Create data directory
RUN mkdir -p /data

# Set the working directory to /data
WORKDIR /data

# Default command
CMD ["sh", "-c", "mirord --vault /data/vault.db --listen ${N1_LISTEN_ADDR} --verbose"]
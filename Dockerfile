# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o proxy-server

# Final stage
FROM alpine:3.19

WORKDIR /app

# Create cache directory
RUN mkdir -p /app/cache && chmod 777 /app/cache

# Copy the binary from builder
COPY --from=builder /app/proxy-server .

# Expose port
EXPOSE 8080

# Set environment variables
ENV CACHE_TTL_SECONDS=3600

# Run the application
CMD ["./proxy-server"]

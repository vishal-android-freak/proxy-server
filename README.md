# API Proxy Cache Server

A simple proxy server that caches API responses using gzip compression.

## Features

- Proxies HTTP requests to target APIs
- Caches responses using gzip compression
- Thread-safe cache implementation
- Configurable cache TTL via environment variable
- Automatic cache cleanup every 10 minutes
- Simple to use with any HTTP API
- Docker support

## Configuration

The server can be configured using environment variables:

- `CACHE_TTL_SECONDS`: Time-to-live for cached responses in seconds (default: 3600)

## Usage

### Running Locally

1. Start the server:
   ```bash
   go run main.go
   ```
   Or with custom TTL:
   ```bash
   CACHE_TTL_SECONDS=60 go run main.go
   ```

### Running with Docker

1. Build the Docker image:
   ```bash
   docker build -t proxy-cache-server .
   ```

2. Run the container:
   ```bash
   docker run -p 8080:8080 proxy-cache-server
   ```

   With custom TTL:
   ```bash
   docker run -p 8080:8080 -e CACHE_TTL_SECONDS=300 proxy-cache-server
   ```

### Making Requests

Make requests through the proxy:
```
http://localhost:8080/proxy?url=YOUR_TARGET_URL
```

The server will cache the response on first request and serve from cache for subsequent identical requests within the TTL period.

## Cache Details

- Cached files are stored in the `/app/cache` directory (in Docker) or `./cache` directory (local)
- Cache keys are generated using SHA256 hash of the request method and URL
- Responses are compressed using gzip before storage
- Cache entries expire after the configured TTL (default: 1 hour)
- Automatic cleanup of expired cache entries runs every 10 minutes
- Cache hits are indicated by the `X-Cache: HIT` header
- Cache misses are indicated by the `X-Cache: MISS` header

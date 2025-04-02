# API Proxy Cache Server

A simple proxy server that caches API responses using gzip compression.

## Features

- Proxies HTTP requests to target APIs
- Caches responses using gzip compression
- Thread-safe cache implementation
- Simple to use with any HTTP API

## Usage

1. Start the server:
   ```
   go run main.go
   ```

2. Make requests through the proxy:
   ```
   http://localhost:8080/proxy?url=YOUR_TARGET_URL
   ```

The server will cache the response on first request and serve from cache for subsequent identical requests.

## Cache Details

- Cached files are stored in the `./cache` directory
- Cache keys are generated using SHA256 hash of the request method and URL
- Responses are compressed using gzip before storage
- Cache hits are indicated by the `X-Cache: HIT` header
- Cache misses are indicated by the `X-Cache: MISS` header

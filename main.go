package main

import (
	"compress/gzip"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/gorilla/mux"
)

type ProxyCache struct {
	cacheDir string
	mu       sync.RWMutex
}

func NewProxyCache(cacheDir string) (*ProxyCache, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %v", err)
	}
	return &ProxyCache{
		cacheDir: cacheDir,
	}, nil
}

func (pc *ProxyCache) getCacheKey(method, url string, headers http.Header) string {
	// Create a unique cache key based on method, URL and relevant headers
	h := sha256.New()
	io.WriteString(h, method)
	io.WriteString(h, url)
	// Add relevant headers to cache key if needed
	return hex.EncodeToString(h.Sum(nil))
}

func (pc *ProxyCache) getCachePath(key string) string {
	return filepath.Join(pc.cacheDir, key+".gz")
}

func (pc *ProxyCache) get(key string) ([]byte, bool, error) {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	path := pc.getCachePath(key)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer file.Close()

	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, false, err
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, false, err
	}

	return data, true, nil
}

func (pc *ProxyCache) set(key string, data []byte) error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	path := pc.getCachePath(key)
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := gzip.NewWriter(file)
	defer writer.Close()

	_, err = writer.Write(data)
	return err
}

func (pc *ProxyCache) proxyHandler(w http.ResponseWriter, r *http.Request) {
	// Get the target URL from the query parameter
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		http.Error(w, "missing url parameter", http.StatusBadRequest)
		return
	}

	// Generate cache key
	cacheKey := pc.getCacheKey(r.Method, targetURL, r.Header)

	// Try to get from cache
	if data, found, err := pc.get(cacheKey); err != nil {
		http.Error(w, fmt.Sprintf("cache error: %v", err), http.StatusInternalServerError)
		return
	} else if found {
		w.Header().Set("X-Cache", "HIT")
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}

	// Forward the request with a custom transport that skips certificate verification
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	req, err := http.NewRequest(r.Method, targetURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("error creating request: %v", err), http.StatusInternalServerError)
		return
	}

	// Copy original headers
	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("proxy request failed: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("error reading response: %v", err), http.StatusInternalServerError)
		return
	}

	// Cache the response
	if err := pc.set(cacheKey, body); err != nil {
		log.Printf("error caching response: %v", err)
	}

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func main() {
	cache, err := NewProxyCache("./cache")
	if err != nil {
		log.Fatalf("Failed to create cache: %v", err)
	}

	r := mux.NewRouter()
	r.HandleFunc("/proxy", cache.proxyHandler).Methods("GET")

	port := "8080"
	log.Printf("Starting proxy server on port %s", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

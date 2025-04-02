package main

import (
	"compress/gzip"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

type CacheItem struct {
	Data      []byte    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

type ProxyCache struct {
	cacheDir string
	ttl      time.Duration
	mu       sync.RWMutex
}

func NewProxyCache(cacheDir string, ttl time.Duration) (*ProxyCache, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %v", err)
	}
	pc := &ProxyCache{
		cacheDir: cacheDir,
		ttl:      ttl,
	}
	
	// Start cache cleanup routine
	go pc.startCleanupRoutine()
	
	return pc, nil
}

func (pc *ProxyCache) startCleanupRoutine() {
	ticker := time.NewTicker(10 * time.Minute) // Run cleanup every 10 minutes
	for range ticker.C {
		if err := pc.cleanExpiredCache(); err != nil {
			log.Printf("Cache cleanup error: %v", err)
		}
	}
}

func (pc *ProxyCache) cleanExpiredCache() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	files, err := os.ReadDir(pc.cacheDir)
	if err != nil {
		return fmt.Errorf("failed to read cache directory: %v", err)
	}

	now := time.Now()
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		path := filepath.Join(pc.cacheDir, file.Name())
		cacheItem, err := pc.readCacheFile(path)
		if err != nil {
			log.Printf("Error reading cache file %s: %v", path, err)
			continue
		}

		if now.Sub(cacheItem.Timestamp) > pc.ttl {
			if err := os.Remove(path); err != nil {
				log.Printf("Error removing expired cache file %s: %v", path, err)
			} else {
				log.Printf("Removed expired cache file: %s", file.Name())
			}
		}
	}
	return nil
}

func (pc *ProxyCache) readCacheFile(path string) (*CacheItem, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var cacheItem CacheItem
	if err := json.NewDecoder(reader).Decode(&cacheItem); err != nil {
		return nil, err
	}

	return &cacheItem, nil
}

func (pc *ProxyCache) getCacheKey(method, url string, headers http.Header) string {
	h := sha256.New()
	io.WriteString(h, method)
	io.WriteString(h, url)
	return hex.EncodeToString(h.Sum(nil))
}

func (pc *ProxyCache) getCachePath(key string) string {
	return filepath.Join(pc.cacheDir, key+".gz")
}

func (pc *ProxyCache) get(key string) ([]byte, bool, error) {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	path := pc.getCachePath(key)
	cacheItem, err := pc.readCacheFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}

	// Check if cache has expired
	if time.Since(cacheItem.Timestamp) > pc.ttl {
		// Remove expired file
		os.Remove(path)
		return nil, false, nil
	}

	return cacheItem.Data, true, nil
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

	cacheItem := CacheItem{
		Data:      data,
		Timestamp: time.Now(),
	}

	return json.NewEncoder(writer).Encode(cacheItem)
}

func (pc *ProxyCache) proxyHandler(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		http.Error(w, "missing url parameter", http.StatusBadRequest)
		return
	}

	cacheKey := pc.getCacheKey(r.Method, targetURL, r.Header)

	if data, found, err := pc.get(cacheKey); err != nil {
		http.Error(w, fmt.Sprintf("cache error: %v", err), http.StatusInternalServerError)
		return
	} else if found {
		w.Header().Set("X-Cache", "HIT")
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("error reading response: %v", err), http.StatusInternalServerError)
		return
	}

	if err := pc.set(cacheKey, body); err != nil {
		log.Printf("error caching response: %v", err)
	}

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
	// Get TTL from environment variable, default to 1 hour
	ttlSeconds := 3600 // default 1 hour
	if ttlStr := os.Getenv("CACHE_TTL_SECONDS"); ttlStr != "" {
		if val, err := strconv.Atoi(ttlStr); err == nil && val > 0 {
			ttlSeconds = val
		} else {
			log.Printf("Invalid CACHE_TTL_SECONDS value: %s, using default: %d seconds", ttlStr, ttlSeconds)
		}
	}
	cacheTTL := time.Duration(ttlSeconds) * time.Second

	cache, err := NewProxyCache("./cache", cacheTTL)
	if err != nil {
		log.Fatalf("Failed to create cache: %v", err)
	}

	r := mux.NewRouter()
	r.HandleFunc("/proxy", cache.proxyHandler).Methods("GET")

	port := "8080"
	log.Printf("Starting proxy server on port %s (Cache TTL: %v)", port, cacheTTL)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

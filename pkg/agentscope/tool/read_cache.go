package tool

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"
)

const (
	DefaultMaxCacheFiles = 100
	DefaultMaxCacheBytes = 25000 * 1024 // 25 MB
)

// ReadCacheEntry holds the cached content and metadata for a single file.
type ReadCacheEntry struct {
	Lines     []string
	UpdatedAt time.Time // file mtime when cached
	Bytes     int       // total byte size of cached lines
	FilePath  string
}

// ReadCache is a FIFO-evicting cache for file contents.
// It limits both the number of entries and total byte size.
type ReadCache struct {
	entries       []ReadCacheEntry
	maxCacheFiles int
	maxCacheBytes int
	mu            sync.Mutex
}

// NewReadCache creates a ReadCache with the given limits.
// Zero values for limits use defaults (100 files, 25MB).
func NewReadCache(maxFiles, maxBytes int) *ReadCache {
	if maxFiles <= 0 {
		maxFiles = DefaultMaxCacheFiles
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxCacheBytes
	}
	return &ReadCache{
		maxCacheFiles: maxFiles,
		maxCacheBytes: maxBytes,
	}
}

// GetCache returns the cached entry for the given file path.
// It validates freshness by checking the file's mtime. If the file has been
// modified since caching, the stale entry is removed and nil is returned.
func (rc *ReadCache) GetCache(filePath string) *ReadCacheEntry {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	for i, e := range rc.entries {
		if e.FilePath == filePath {
			info, err := os.Stat(filePath)
			if err != nil {
				rc.removeAt(i)
				return nil
			}
			if !info.ModTime().Equal(e.UpdatedAt) {
				rc.removeAt(i)
				return nil
			}
			// Upstream #1811: a hit refreshes recency. Without this a
			// repeatedly-read hot file ages out first under FIFO eviction.
			entry := rc.entries[i]
			rc.removeAt(i)
			rc.entries = append(rc.entries, entry)
			return &rc.entries[len(rc.entries)-1]
		}
	}
	return nil
}

// HasBeenRead returns true if the file has an entry in the cache (even if stale).
// Used by Write/Edit as a "read before write" guard without full freshness check.
func (rc *ReadCache) HasBeenRead(filePath string) bool {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	for _, e := range rc.entries {
		if e.FilePath == filePath {
			return true
		}
	}
	return false
}

// CacheFile stores file content in the cache, evicting oldest entries if needed.
func (rc *ReadCache) CacheFile(filePath string, lines []string) {
	info, err := os.Stat(filePath)
	if err != nil {
		return
	}

	byteSize := 0
	for _, line := range lines {
		byteSize += len(line)
	}

	rc.mu.Lock()
	defer rc.mu.Unlock()

	for i, e := range rc.entries {
		if e.FilePath == filePath {
			rc.removeAt(i)
			break
		}
	}

	for len(rc.entries) >= rc.maxCacheFiles {
		rc.removeAt(0)
	}

	totalBytes := rc.totalBytes()
	for len(rc.entries) > 0 && totalBytes+byteSize > rc.maxCacheBytes {
		totalBytes -= rc.entries[0].Bytes
		rc.removeAt(0)
	}

	rc.entries = append(rc.entries, ReadCacheEntry{
		Lines:     lines,
		UpdatedAt: info.ModTime(),
		Bytes:     byteSize,
		FilePath:  filePath,
	})
}

// CleanFileCache removes all entries except those whose FilePath is in the given set.
func (rc *ReadCache) CleanFileCache(keepPaths map[string]bool) {
	if keepPaths == nil {
		rc.mu.Lock()
		rc.entries = nil
		rc.mu.Unlock()
		return
	}

	rc.mu.Lock()
	defer rc.mu.Unlock()

	var kept []ReadCacheEntry
	for _, e := range rc.entries {
		if keepPaths[e.FilePath] {
			kept = append(kept, e)
		}
	}
	rc.entries = kept
}

// Len returns the number of cached entries.
func (rc *ReadCache) Len() int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return len(rc.entries)
}

func (rc *ReadCache) removeAt(i int) {
	rc.entries = append(rc.entries[:i], rc.entries[i+1:]...)
}

func (rc *ReadCache) totalBytes() int {
	total := 0
	for _, e := range rc.entries {
		total += e.Bytes
	}
	return total
}

// readCacheJSON is the JSON-friendly representation of ReadCache.
type readCacheJSON struct {
	Entries       []ReadCacheEntry `json:"entries"`
	MaxCacheFiles int              `json:"max_cache_files"`
	MaxCacheBytes int              `json:"max_cache_bytes"`
}

// MarshalJSON serializes ReadCache to JSON.
func (rc *ReadCache) MarshalJSON() ([]byte, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return json.Marshal(readCacheJSON{
		Entries:       rc.entries,
		MaxCacheFiles: rc.maxCacheFiles,
		MaxCacheBytes: rc.maxCacheBytes,
	})
}

// UnmarshalJSON deserializes ReadCache from JSON.
func (rc *ReadCache) UnmarshalJSON(data []byte) error {
	var raw readCacheJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.entries = raw.Entries
	rc.maxCacheFiles = raw.MaxCacheFiles
	if rc.maxCacheFiles <= 0 {
		rc.maxCacheFiles = DefaultMaxCacheFiles
	}
	rc.maxCacheBytes = raw.MaxCacheBytes
	if rc.maxCacheBytes <= 0 {
		rc.maxCacheBytes = DefaultMaxCacheBytes
	}
	return nil
}

type readCacheKey struct{}

// WithReadCache attaches a ReadCache to a Go context.
func WithReadCache(ctx context.Context, rc *ReadCache) context.Context {
	return context.WithValue(ctx, readCacheKey{}, rc)
}

// GetReadCache retrieves the ReadCache from a Go context, or nil if none.
func GetReadCache(ctx context.Context) *ReadCache {
	v, _ := ctx.Value(readCacheKey{}).(*ReadCache)
	return v
}

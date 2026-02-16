package upload

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

type cacheEntry struct {
	Size  int64     `json:"size"`
	Mtime time.Time `json:"mtime"`
	MD5   string    `json:"md5"`
}

type hashCache struct {
	Files map[string]cacheEntry `json:"files"`
}

func newHashCache() *hashCache {
	return &hashCache{Files: make(map[string]cacheEntry)}
}

// loadHashCache reads the cache from disk. Returns an empty cache if
// the file is missing or corrupt — never returns an error.
func loadHashCache(path string) *hashCache {
	data, err := os.ReadFile(path)
	if err != nil {
		return newHashCache()
	}

	var c hashCache
	if err := json.Unmarshal(data, &c); err != nil {
		log.Printf("warning: corrupt upload cache, rebuilding: %v", err)
		return newHashCache()
	}

	if c.Files == nil {
		c.Files = make(map[string]cacheEntry)
	}
	return &c
}

func (c *hashCache) save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (c *hashCache) lookup(key string, size int64, mtime time.Time) (string, bool) {
	entry, ok := c.Files[key]
	if !ok {
		return "", false
	}
	if entry.Size != size || !entry.Mtime.Equal(mtime) {
		return "", false
	}
	return entry.MD5, true
}

func (c *hashCache) update(key string, size int64, mtime time.Time, md5 string) {
	c.Files[key] = cacheEntry{Size: size, Mtime: mtime, MD5: md5}
}

// prune removes entries not present in the given key set.
func (c *hashCache) prune(validKeys map[string]struct{}) {
	for key := range c.Files {
		if _, ok := validKeys[key]; !ok {
			delete(c.Files, key)
		}
	}
}

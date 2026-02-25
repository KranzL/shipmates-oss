package conversation

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KranzL/shipmates-oss/go-shared"
)

const (
	cacheTTL         = 60 * time.Second
	maxCacheSize     = 10000
	cleanupInterval  = 5 * time.Minute
	cleanupThreshold = 30 * time.Minute
)

type cacheEntry struct {
	config       *goshared.UserConfig
	expiresAt    time.Time
	lastAccessed atomic.Int64
}

type configCache struct {
	mu    sync.RWMutex
	cache map[string]cacheEntry
}

var cache = &configCache{
	cache: make(map[string]cacheEntry),
}

func init() {
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			cache.evictStale()
		}
	}()
}

func (c *configCache) get(key string) (*goshared.UserConfig, bool) {
	c.mu.RLock()
	entry, found := c.cache[key]
	c.mu.RUnlock()

	if !found {
		return nil, false
	}

	now := time.Now()
	if now.After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.cache, key)
		c.mu.Unlock()
		return nil, false
	}

	entry.lastAccessed.Store(now.Unix())

	return entry.config, true
}

func (c *configCache) set(key string, config *goshared.UserConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.cache) >= maxCacheSize {
		c.evictOldestLocked()
	}

	now := time.Now()
	entry := cacheEntry{
		config:    config,
		expiresAt: now.Add(cacheTTL),
	}
	entry.lastAccessed.Store(now.Unix())
	c.cache[key] = entry
}

type cacheEntryWithKey struct {
	key          string
	lastAccessed int64
}

func (c *configCache) evictOldestLocked() {
	if len(c.cache) == 0 {
		return
	}

	toEvict := 100
	if len(c.cache) < toEvict {
		toEvict = 1
	}

	entries := make([]cacheEntryWithKey, 0, len(c.cache))
	for k, v := range c.cache {
		entries = append(entries, cacheEntryWithKey{
			key:          k,
			lastAccessed: v.lastAccessed.Load(),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastAccessed < entries[j].lastAccessed
	})

	for i := 0; i < toEvict && i < len(entries); i++ {
		delete(c.cache, entries[i].key)
	}
}

func (c *configCache) evictStale() {
	c.mu.Lock()
	defer c.mu.Unlock()

	threshold := time.Now().Add(-cleanupThreshold).Unix()
	for k, v := range c.cache {
		if v.lastAccessed.Load() < threshold {
			delete(c.cache, k)
		}
	}
}

func (c *configCache) invalidate(key string) {
	c.mu.Lock()
	delete(c.cache, key)
	c.mu.Unlock()
}

func GetUserConfigByWorkspace(ctx context.Context, db *goshared.DB, platform goshared.Platform, platformID string) (*goshared.UserConfig, error) {
	cacheKey := string(platform) + ":" + platformID

	if config, found := cache.get(cacheKey); found {
		return config, nil
	}

	workspace, err := db.GetWorkspaceByPlatform(ctx, platform, platformID)
	if err != nil {
		return nil, err
	}
	if workspace == nil {
		return nil, nil
	}

	config, err := db.GetUserConfig(ctx, workspace.UserID)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, nil
	}

	cache.set(cacheKey, config)
	return config, nil
}

func InvalidateCache(platform goshared.Platform, platformID string) {
	cacheKey := string(platform) + ":" + platformID
	cache.invalidate(cacheKey)
}

package main

import (
	"sync"
)

type CacheEntry struct {
	Achievements []Achievement
	Playtime     int
}

type AchievementCache struct {
	mu    sync.RWMutex
	store map[uint64]CacheEntry // key is appId
}

func NewAchievementCache() *AchievementCache {
	return &AchievementCache{
		store: make(map[uint64]CacheEntry),
	}
}

func (c *AchievementCache) Get(appId uint64) (CacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, exists := c.store[appId]
	return entry, exists
}

func (c *AchievementCache) Set(appId uint64, achievements []Achievement, playtime int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[appId] = CacheEntry{
		Achievements: achievements,
		Playtime:     playtime,
	}
}

func (c *AchievementCache) ShouldInvalidate(appId uint64, currentPlaytime int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if entry, exists := c.store[appId]; exists {
		return currentPlaytime > entry.Playtime
	}
	return true
}

package main

import (
	"sync"
)

type UserCacheEntry struct {
	UserAchievements []Achievement
	Playtime         int
}

type GlobalCacheEntry struct {
	GlobalAchievements []GlobalAchievement
}

type AchievementCache struct {
	mu          sync.RWMutex
	userStore   map[uint64]UserCacheEntry   // key is appId
	globalStore map[uint64]GlobalCacheEntry // key is appId
}

func NewAchievementCache() *AchievementCache {
	return &AchievementCache{
		userStore:   make(map[uint64]UserCacheEntry),
		globalStore: make(map[uint64]GlobalCacheEntry),
	}
}

func (c *AchievementCache) GetUserAchievements(appId uint64) (UserCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, exists := c.userStore[appId]
	return entry, exists
}

func (c *AchievementCache) GetGlobalAchievements(appId uint64) (GlobalCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, exists := c.globalStore[appId]
	return entry, exists
}

func (c *AchievementCache) SetUserAchievements(appId uint64, userAchievements []Achievement, playtime int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.userStore[appId] = UserCacheEntry{
		UserAchievements: userAchievements,
		Playtime:         playtime,
	}
}

func (c *AchievementCache) SetGlobalAchievements(appId uint64, globalAchievements []GlobalAchievement) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.globalStore[appId] = GlobalCacheEntry{
		GlobalAchievements: globalAchievements,
	}
}

func (c *AchievementCache) ShouldInvalidateUserCache(appId uint64, currentPlaytime int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if entry, exists := c.userStore[appId]; exists {
		return currentPlaytime > entry.Playtime
	}
	return true
}

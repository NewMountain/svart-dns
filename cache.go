package main

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

type cacheEntry struct {
	msg       *dns.Msg
	expiresAt time.Time
}

type dnsCache struct {
	entries sync.Map
	hits    atomic.Int64
	misses  atomic.Int64
}

var cache = &dnsCache{}

type CacheStats struct {
	Hits    int64
	Misses  int64
	HitRate int
	Entries int
}

func getCacheKey(name string, qtype uint16) string {
	return dns.Fqdn(name) + ":" + dns.TypeToString[qtype]
}

func (c *dnsCache) get(name string, qtype uint16) (*dns.Msg, bool) {
	key := getCacheKey(name, qtype)
	
	val, ok := c.entries.Load(key)
	if !ok {
		c.misses.Add(1)
		return nil, false
	}
	
	entry := val.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.entries.Delete(key)
		c.misses.Add(1)
		return nil, false
	}
	
	c.hits.Add(1)
	return entry.msg.Copy(), true
}

func (c *dnsCache) set(name string, qtype uint16, msg *dns.Msg, ttl int) {
	if ttl <= 0 {
		return
	}
	
	key := getCacheKey(name, qtype)
	
	entry := &cacheEntry{
		msg:       msg.Copy(),
		expiresAt: time.Now().Add(time.Duration(ttl) * time.Second),
	}
	
	c.entries.Store(key, entry)
}

func (c *dnsCache) cleanup() {
	now := time.Now()
	deleted := 0
	
	c.entries.Range(func(key, value interface{}) bool {
		entry := value.(*cacheEntry)
		if now.After(entry.expiresAt) {
			c.entries.Delete(key)
			deleted++
		}
		return true
	})
	
	if deleted > 0 {
		log.Printf("Cleaned up %d expired cache entries", deleted)
	}
}

func (c *dnsCache) clear() {
	c.entries.Range(func(key, value interface{}) bool {
		c.entries.Delete(key)
		return true
	})
	c.hits.Store(0)
	c.misses.Store(0)
}

func (c *dnsCache) stats() CacheStats {
	hits := c.hits.Load()
	misses := c.misses.Load()
	total := hits + misses
	
	hitRate := 0
	if total > 0 {
		hitRate = int((hits * 100) / total)
	}
	
	count := 0
	c.entries.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	
	return CacheStats{
		Hits:    hits,
		Misses:  misses,
		HitRate: hitRate,
		Entries: count,
	}
}

func clearDNSCache() {
	cache.clear()
}

func getCacheStats() CacheStats {
	return cache.stats()
}

func startCacheCleanupWorker() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		cache.cleanup()
		cleanupBootstrapCache()
	}
}

func cleanupBootstrapCache() {
	now := time.Now()
	deleted := 0
	
	bsCache.entries.Range(func(key, value interface{}) bool {
		entry := value.(*cacheEntry)
		if now.After(entry.expiresAt) {
			bsCache.entries.Delete(key)
			deleted++
		}
		return true
	})
	
	if deleted > 0 {
		log.Printf("Cleaned up %d expired bootstrap cache entries", deleted)
	}
}

type bootstrapCache struct {
	entries sync.Map
}

var bsCache = &bootstrapCache{}

func (b *bootstrapCache) get(hostname string) (string, bool) {
	val, ok := b.entries.Load(hostname)
	if !ok {
		return "", false
	}
	
	entry := val.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		b.entries.Delete(hostname)
		return "", false
	}
	
	if len(entry.msg.Answer) == 0 {
		return "", false
	}
	
	if a, ok := entry.msg.Answer[0].(*dns.A); ok {
		return a.A.String(), true
	}
	if aaaa, ok := entry.msg.Answer[0].(*dns.AAAA); ok {
		return aaaa.AAAA.String(), true
	}
	
	return "", false
}

func (b *bootstrapCache) set(hostname string, msg *dns.Msg, ttl int) {
	if ttl <= 0 {
		return
	}
	
	entry := &cacheEntry{
		msg:       msg.Copy(),
		expiresAt: time.Now().Add(time.Duration(ttl) * time.Second),
	}
	
	b.entries.Store(hostname, entry)
}

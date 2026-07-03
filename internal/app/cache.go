package app

import (
	"sync"
	"time"
)

// Cache provides an in-memory read cache for stats and individual requests.
// This dramatically reduces DB queries: 10,000 concurrent users viewing
// the same tracking ID result in 1 DB query per TTL window, not 10,000.
//
// Writes invalidate the relevant cache entries and broadcast via WebSocket,
// so clients always see fresh data without polling.
type Cache struct {
	mu sync.RWMutex

	stats     *StatsResponse
	statsTime time.Time
	statsTTL  time.Duration

	requests   map[string]*WebsiteRequest // trackingID -> cached request
	reqTimes   map[string]time.Time       // trackingID -> cache time
	reqTTL     time.Duration
}

// NewCache creates a cache with sensible TTLs.
func NewCache() *Cache {
	return &Cache{
		statsTTL:   60 * time.Second,
		requests:   make(map[string]*WebsiteRequest),
		reqTimes:   make(map[string]time.Time),
		reqTTL:     10 * time.Second,
	}
}

// GetStats returns cached stats if fresh, nil otherwise.
func (c *Cache) GetStats() *StatsResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.stats == nil || time.Since(c.statsTime) > c.statsTTL {
		return nil
	}
	// Return a copy to prevent callers from mutating the cached pointer
	s := *c.stats
	return &s
}

// SetStats stores stats in the cache.
func (c *Cache) SetStats(s *StatsResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats = s
	c.statsTime = time.Now()
}

// InvalidateStats clears the stats cache (called on any mutation).
func (c *Cache) InvalidateStats() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats = nil
}

// GetRequest returns a cached request by tracking ID if fresh.
func (c *Cache) GetRequest(trackingID string) *WebsiteRequest {
	c.mu.RLock()
	defer c.mu.RUnlock()
	req, ok := c.requests[trackingID]
	if !ok || time.Since(c.reqTimes[trackingID]) > c.reqTTL {
		return nil
	}
	// Return a copy
	r := *req
	return &r
}

// SetRequest stores a request in the cache.
func (c *Cache) SetRequest(req *WebsiteRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := *req // copy
	c.requests[req.TrackingID] = &r
	c.reqTimes[req.TrackingID] = time.Now()
}

// InvalidateRequest clears a single request from the cache.
func (c *Cache) InvalidateRequest(trackingID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.requests, trackingID)
	delete(c.reqTimes, trackingID)
}

// InvalidateAll clears everything (useful for admin mutations).
func (c *Cache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats = nil
	c.requests = make(map[string]*WebsiteRequest)
	c.reqTimes = make(map[string]time.Time)
}

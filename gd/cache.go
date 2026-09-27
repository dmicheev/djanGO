package gd

import (
	"bytes"
	"net/http"
	"sync"
	"time"
)

type cacheItem struct {
	value     any
	expiresAt time.Time
}

type Cache struct {
	mu    sync.RWMutex
	ttl   time.Duration
	items map[string]cacheItem
	stop  chan struct{}
}

func NewCache(ttl time.Duration) *Cache {
	c := &Cache{
		ttl:   ttl,
		items: make(map[string]cacheItem),
		stop:  make(chan struct{}),
	}
	go c.janitor()
	return c
}

func (c *Cache) Set(key string, value any) {
	c.SetTTL(key, value, c.ttl)
}

func (c *Cache) SetTTL(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	c.items[key] = cacheItem{value: value, expiresAt: time.Now().Add(ttl)}
	c.mu.Unlock()
}

func (c *Cache) Get(key string) (any, bool) {
	c.mu.RLock()
	item, ok := c.items[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(item.expiresAt) {
		return nil, false
	}
	return item.value, true
}

func (c *Cache) GetOrSet(key string, fn func() (any, error)) (any, error) {
	if v, ok := c.Get(key); ok {
		return v, nil
	}
	v, err := fn()
	if err != nil {
		return nil, err
	}
	c.Set(key, v)
	return v, nil
}

func (c *Cache) Delete(key string) {
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()
}

func (c *Cache) Clear() {
	c.mu.Lock()
	c.items = make(map[string]cacheItem)
	c.mu.Unlock()
}

func (c *Cache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	n := 0
	now := time.Now()
	for _, item := range c.items {
		if now.Before(item.expiresAt) {
			n++
		}
	}
	return n
}

func (c *Cache) Close() {
	close(c.stop)
}

func (c *Cache) janitor() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			c.mu.Lock()
			for key, item := range c.items {
				if now.After(item.expiresAt) {
					delete(c.items, key)
				}
			}
			c.mu.Unlock()
		case <-c.stop:
			return
		}
	}
}

type cachedResponse struct {
	status int
	header http.Header
	body   bytes.Buffer
}

func (r *cachedResponse) Header() http.Header         { return r.header }
func (r *cachedResponse) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *cachedResponse) WriteHeader(code int)        { r.status = code }

func CachePage(cache *Cache, ttl time.Duration, h Handler) Handler {
	return func(c *Ctx) error {
		if c.R.Method != http.MethodGet || c.R.Header.Get("Cache-Control") == "no-cache" {
			return h(c)
		}
		key := "page:" + c.R.URL.RequestURI()
		if v, ok := cache.Get(key); ok {
			if resp, ok := v.(*cachedResponse); ok {
				for k, vals := range resp.header {
					for _, val := range vals {
						c.W.Header().Add(k, val)
					}
				}
				c.W.Header().Set("X-Cache", "HIT")
				c.W.WriteHeader(resp.status)
				_, err := c.W.Write(resp.body.Bytes())
				return err
			}
		}
		rec := &cachedResponse{status: http.StatusOK, header: make(http.Header)}
		origW := c.W
		c.W = rec
		err := h(c)
		c.W = origW
		if err != nil || rec.status >= 400 {
			for k, vals := range rec.header {
				for _, val := range vals {
					c.W.Header().Add(k, val)
				}
			}
			c.W.WriteHeader(rec.status)
			_, werr := c.W.Write(rec.body.Bytes())
			if err != nil {
				return err
			}
			return werr
		}
		cache.SetTTL(key, rec, ttl)
		for k, vals := range rec.header {
			for _, val := range vals {
				c.W.Header().Add(k, val)
			}
		}
		c.W.Header().Set("X-Cache", "MISS")
		c.W.WriteHeader(rec.status)
		_, werr := c.W.Write(rec.body.Bytes())
		return werr
	}
}

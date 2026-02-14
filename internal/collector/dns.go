package collector

import (
	"context"
	"net"
	"sync"
	"time"
)

const (
	dnsCacheTTL     = 5 * time.Minute
	dnsLookupTimeout = 2 * time.Second
	maxCacheSize     = 4096
)

type dnsEntry struct {
	host    string
	expires time.Time
}

// DNSCache provides async, cached reverse DNS resolution.
type DNSCache struct {
	mu      sync.RWMutex
	cache   map[string]dnsEntry
	pending sync.Map // tracks in-flight lookups to avoid duplicates
}

// NewDNSCache creates a new DNS cache.
func NewDNSCache() *DNSCache {
	return &DNSCache{
		cache: make(map[string]dnsEntry),
	}
}

// Resolve returns the cached hostname for an IP, or empty string if not cached.
// It kicks off an async lookup if the IP is not in cache.
func (d *DNSCache) Resolve(ip net.IP) string {
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return ""
	}

	ipStr := ip.String()

	d.mu.RLock()
	entry, ok := d.cache[ipStr]
	d.mu.RUnlock()

	if ok {
		if time.Now().Before(entry.expires) {
			return entry.host
		}
		// Expired — trigger refresh
	}

	// Async lookup (fire and forget, deduplicated)
	if _, loaded := d.pending.LoadOrStore(ipStr, true); !loaded {
		go d.lookup(ipStr)
	}

	if ok {
		return entry.host // return stale while refreshing
	}
	return ""
}

func (d *DNSCache) lookup(ipStr string) {
	defer d.pending.Delete(ipStr)

	ctx, cancel := context.WithTimeout(context.Background(), dnsLookupTimeout)
	defer cancel()

	resolver := &net.Resolver{}
	names, err := resolver.LookupAddr(ctx, ipStr)

	host := ""
	if err == nil && len(names) > 0 {
		host = names[0]
		// Remove trailing dot
		if len(host) > 0 && host[len(host)-1] == '.' {
			host = host[:len(host)-1]
		}
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Evict if cache is too large
	if len(d.cache) >= maxCacheSize {
		d.evictOldest()
	}

	d.cache[ipStr] = dnsEntry{
		host:    host,
		expires: time.Now().Add(dnsCacheTTL),
	}
}

func (d *DNSCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	first := true

	for k, v := range d.cache {
		if first || v.expires.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.expires
			first = false
		}
	}

	if oldestKey != "" {
		delete(d.cache, oldestKey)
	}
}

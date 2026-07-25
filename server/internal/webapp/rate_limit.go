package webapp

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const maxRateLimitClients = 10_000

type rateLimitEntry struct {
	start time.Time
	count int
}

type rateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	now     func() time.Time
	entries map[string]rateLimitEntry
}

func newRateLimiter(limit int, window time.Duration, now func() time.Time) *rateLimiter {
	if now == nil {
		now = time.Now
	}
	return &rateLimiter{
		limit: limit, window: window, now: now, entries: make(map[string]rateLimitEntry),
	}
}

func (l *rateLimiter) allow(key string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, exists := l.entries[key]
	if !exists && len(l.entries) >= maxRateLimitClients {
		key = "__overflow__"
		entry, exists = l.entries[key]
	}
	if !exists || now.Sub(entry.start) >= l.window {
		l.entries[key] = rateLimitEntry{start: now, count: 1}
		return true, 0
	}
	if entry.count >= l.limit {
		return false, max(l.window-now.Sub(entry.start), time.Second)
	}
	entry.count++
	l.entries[key] = entry
	return true, 0
}

func clientIP(request *http.Request, trustProxy bool) string {
	if trustProxy {
		for _, candidate := range []string{
			strings.TrimSpace(request.Header.Get("X-Real-IP")),
			firstForwardedIP(request.Header.Get("X-Forwarded-For")),
		} {
			if parsed := net.ParseIP(candidate); parsed != nil {
				return parsed.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr))
	if err == nil {
		if parsed := net.ParseIP(host); parsed != nil {
			return parsed.String()
		}
	}
	if parsed := net.ParseIP(strings.TrimSpace(request.RemoteAddr)); parsed != nil {
		return parsed.String()
	}
	return "unknown"
}

func firstForwardedIP(value string) string {
	if index := strings.IndexByte(value, ','); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}

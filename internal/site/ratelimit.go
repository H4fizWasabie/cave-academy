package site

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// limiter counts events per key inside a sliding window. In memory, so counts
// reset on restart and are per process (one server is fine).
type limiter struct {
	limit  int
	window time.Duration
	now    func() time.Time // tests replace it

	mu        sync.Mutex
	hits      map[string][]time.Time
	lastSweep time.Time
}

func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{limit: limit, window: window, now: time.Now, hits: map[string][]time.Time{}}
}

// recent returns key's timestamps still inside the window. Caller holds mu.
func (l *limiter) recent(key string, now time.Time) []time.Time {
	ts := l.hits[key]
	i := 0
	for i < len(ts) && !ts[i].After(now.Add(-l.window)) {
		i++
	}
	return ts[i:]
}

// over reports whether key has already used up its limit.
func (l *limiter) over(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(key, l.now())) >= l.limit
}

// add records one event for key and drops expired entries everywhere, so the
// map cannot grow without bound.
func (l *limiter) add(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.lastSweep) >= l.window { // ponytail: full sweep once per window, O(keys); fine for one small server
		for k := range l.hits {
			if ts := l.recent(k, now); len(ts) == 0 {
				delete(l.hits, k)
			} else {
				l.hits[k] = ts
			}
		}
		l.lastSweep = now
	}
	l.hits[key] = append(l.recent(key, now), now)
}

// reset forgets key's events.
func (l *limiter) reset(key string) {
	l.mu.Lock()
	delete(l.hits, key)
	l.mu.Unlock()
}

// rateLimits are the auth limiters held by Server.
type rateLimits struct {
	loginEmail *limiter // failed logins per email
	loginIP    *limiter // failed logins per client IP
	signupIP   *limiter // accounts created per client IP
}

func newRateLimits() *rateLimits {
	return &rateLimits{
		loginEmail: newLimiter(5, 15*time.Minute),
		loginIP:    newLimiter(20, 15*time.Minute),
		signupIP:   newLimiter(5, time.Hour),
	}
}

const tooManyAttempts = "Terlalu banyak cubaan. Sila cuba lagi dalam beberapa minit."

// clientIP is the peer's IP. X-Forwarded-For is only believed when the peer is
// loopback (a reverse proxy on this box), and then its last entry is used,
// the one the proxy appended. IPv6 addresses are cut to their /64, since one
// subscriber controls a whole /64 and would otherwise get unlimited "IPs".
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if fromLoopback(r) {
		xff := r.Header.Get("X-Forwarded-For")
		last := strings.TrimSpace(xff[strings.LastIndex(xff, ",")+1:])
		if net.ParseIP(last) != nil {
			host = last
		}
	}
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		return ip.Mask(net.CIDRMask(64, 128)).String()
	}
	return host
}

package viewer

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type attempt struct {
	start time.Time
	count int
}
type limiter struct {
	mu      sync.Mutex
	entries map[string]attempt
	failed  map[string]attempt
	active  chan struct{}
}

const (
	accountFailureLimit  = 10
	accountFailureWindow = 15 * time.Minute
)

// refund gives back an IP attempt after a successful login.
func (l *limiter) refund(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if v, ok := l.entries[ip]; ok && v.count > 0 {
		v.count--
		l.entries[ip] = v
	}
}

// accountLocked reports whether too many recent failures exist for the account.
func (l *limiter) accountLocked(name string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.failed[name]
	if ok && time.Since(v.start) > accountFailureWindow {
		delete(l.failed, name)
		return false
	}
	return ok && v.count >= accountFailureLimit
}

func (l *limiter) accountFailure(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failed == nil {
		l.failed = map[string]attempt{}
	}
	now := time.Now()
	for k, v := range l.failed {
		if now.Sub(v.start) > accountFailureWindow {
			delete(l.failed, k)
		}
	}
	v, ok := l.failed[name]
	if !ok {
		if len(l.failed) >= 10000 {
			return
		}
		v.start = now
	}
	v.count++
	l.failed[name] = v
}

func (l *limiter) accountSuccess(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failed, name)
}

func (l *limiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for k, v := range l.entries {
		if now.Sub(v.start) > 15*time.Minute {
			delete(l.entries, k)
		}
	}
	v := l.entries[ip]
	if v.start.IsZero() {
		v.start = now
	}
	v.count++
	if len(l.entries) >= 10000 && v.count == 1 {
		return false
	}
	l.entries[ip] = v
	return v.count <= 8
}
func (a *App) clientIP(r *http.Request) string {
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		host = r.RemoteAddr
	}
	ip, e := netip.ParseAddr(host)
	if e != nil {
		return "unknown"
	}
	trusted := func(ip netip.Addr) bool {
		for _, p := range a.config().TrustedProxies {
			if p.Contains(ip) {
				return true
			}
		}
		return false
	}
	if trusted(ip) {
		parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		for i := len(parts) - 1; i >= 0; i-- {
			candidate, e := netip.ParseAddr(strings.TrimSpace(parts[i]))
			if e != nil {
				return ip.String()
			}
			ip = candidate
			if !trusted(ip) {
				break
			}
		}
	}
	return ip.String()
}

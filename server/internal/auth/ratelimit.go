package auth

import (
	"sync"
	"time"
)

// LoginLimiter throttles password attempts on two axes:
//
//   - per client IP: a token bucket (burst of attempts, slow refill), which
//     stops a single host from spraying many usernames;
//   - per username: exponential lockout after consecutive failures, which
//     stops a distributed attack on one account.
//
// State is in memory; a restart resets it, which is acceptable because a
// restart is itself slow and visible.
type LoginLimiter struct {
	mu    sync.Mutex
	ips   map[string]*bucket
	users map[string]*failures
	now   func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

type failures struct {
	count     int
	lockedTil time.Time
	last      time.Time
}

const (
	ipBurst          = 10
	ipRefillPerSec   = 1.0 / 12 // 5 attempts per minute sustained
	userFreeFailures = 5
	userBaseLockout  = 30 * time.Second
	userMaxLockout   = 15 * time.Minute
	stateTTL         = time.Hour
)

func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{
		ips:   map[string]*bucket{},
		users: map[string]*failures{},
		now:   time.Now,
	}
}

// Allow reports whether an attempt may proceed; if not, it returns how long
// the client should wait. It consumes an IP token on success.
func (l *LoginLimiter) Allow(ip, username string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	if f := l.users[username]; f != nil && now.Before(f.lockedTil) {
		return false, f.lockedTil.Sub(now)
	}

	b := l.ips[ip]
	if b == nil {
		b = &bucket{tokens: ipBurst, last: now}
		l.ips[ip] = b
	}
	b.tokens = min(ipBurst, b.tokens+now.Sub(b.last).Seconds()*ipRefillPerSec)
	b.last = now
	if b.tokens < 1 {
		return false, time.Duration((1 - b.tokens) / ipRefillPerSec * float64(time.Second))
	}
	b.tokens--
	return true, 0
}

// Failure records a failed attempt for username.
func (l *LoginLimiter) Failure(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	f := l.users[username]
	if f == nil {
		f = &failures{}
		l.users[username] = f
	}
	f.count++
	f.last = now
	if over := f.count - userFreeFailures; over >= 0 {
		d := userBaseLockout << min(over, 10)
		f.lockedTil = now.Add(min(d, userMaxLockout))
	}
}

// Success clears the failure history of username.
func (l *LoginLimiter) Success(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.users, username)
}

// GC drops stale entries; call periodically.
func (l *LoginLimiter) GC() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for k, b := range l.ips {
		if now.Sub(b.last) > stateTTL {
			delete(l.ips, k)
		}
	}
	for k, f := range l.users {
		if now.Sub(f.last) > stateTTL && now.After(f.lockedTil) {
			delete(l.users, k)
		}
	}
}

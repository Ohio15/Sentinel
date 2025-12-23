package middleware

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// =============================================================================
// DC-001 Security Fix: Authentication Rate Limiting
// Prevents brute force attacks with IP-based rate limiting and exponential backoff
// =============================================================================

const (
	// AuthRateLimitWindow is the time window for rate limiting (15 minutes)
	AuthRateLimitWindow = 15 * time.Minute

	// MaxAuthAttempts is the maximum login attempts per IP within the window
	MaxAuthAttempts = 5

	// LockoutThreshold is the number of failed attempts that triggers a lockout
	LockoutThreshold = 10

	// LockoutDuration is how long an IP is locked out after reaching the threshold
	LockoutDuration = 1 * time.Hour

	// CleanupInterval is how often to clean up expired entries
	CleanupInterval = 5 * time.Minute
)

// AuthAttempt tracks authentication attempts for an IP
type AuthAttempt struct {
	Attempts       int
	FirstAttempt   time.Time
	LastAttempt    time.Time
	FailedAttempts int       // Total failed attempts for lockout tracking
	LockedUntil    time.Time // Lockout expiration time
}

// AuthRateLimiter implements rate limiting for authentication endpoints
type AuthRateLimiter struct {
	mu       sync.RWMutex
	attempts map[string]*AuthAttempt
	stopCh   chan struct{}
}

// NewAuthRateLimiter creates a new authentication rate limiter
func NewAuthRateLimiter() *AuthRateLimiter {
	rl := &AuthRateLimiter{
		attempts: make(map[string]*AuthAttempt),
		stopCh:   make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// Stop stops the cleanup goroutine
func (rl *AuthRateLimiter) Stop() {
	close(rl.stopCh)
}

// cleanupLoop periodically removes expired entries
func (rl *AuthRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopCh:
			return
		}
	}
}

// cleanup removes expired rate limit entries
func (rl *AuthRateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, attempt := range rl.attempts {
		// Remove if window expired and not locked out
		windowExpired := now.Sub(attempt.FirstAttempt) > AuthRateLimitWindow
		lockoutExpired := now.After(attempt.LockedUntil)

		if windowExpired && lockoutExpired {
			delete(rl.attempts, ip)
		}
	}
}

// CheckRateLimit checks if an IP is allowed to attempt authentication
// Returns (allowed bool, retryAfter time.Duration, error)
func (rl *AuthRateLimiter) CheckRateLimit(ip string) (bool, time.Duration, error) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	attempt, exists := rl.attempts[ip]

	if !exists {
		// First attempt from this IP
		rl.attempts[ip] = &AuthAttempt{
			Attempts:     1,
			FirstAttempt: now,
			LastAttempt:  now,
		}
		return true, 0, nil
	}

	// Check if locked out
	if now.Before(attempt.LockedUntil) {
		retryAfter := attempt.LockedUntil.Sub(now)
		log.Printf("[SECURITY] Auth rate limit: IP %s is locked out for %v", ip, retryAfter)
		return false, retryAfter, fmt.Errorf("account locked due to too many failed attempts")
	}

	// Check if window has expired
	if now.Sub(attempt.FirstAttempt) > AuthRateLimitWindow {
		// Reset the window but keep failed attempts count for lockout tracking
		attempt.Attempts = 1
		attempt.FirstAttempt = now
		attempt.LastAttempt = now
		return true, 0, nil
	}

	// Check if rate limit exceeded
	if attempt.Attempts >= MaxAuthAttempts {
		retryAfter := AuthRateLimitWindow - now.Sub(attempt.FirstAttempt)
		// Apply exponential backoff based on failed attempts
		backoffMultiplier := 1 << min(attempt.FailedAttempts/MaxAuthAttempts, 4) // Max 16x
		retryAfter = time.Duration(float64(retryAfter) * float64(backoffMultiplier))

		log.Printf("[SECURITY] Auth rate limit exceeded for IP %s: %d attempts in window, retry after %v",
			ip, attempt.Attempts, retryAfter)
		return false, retryAfter, fmt.Errorf("too many authentication attempts")
	}

	// Allow attempt
	attempt.Attempts++
	attempt.LastAttempt = now
	return true, 0, nil
}

// RecordFailedAttempt records a failed authentication attempt
func (rl *AuthRateLimiter) RecordFailedAttempt(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	attempt, exists := rl.attempts[ip]
	if !exists {
		return
	}

	attempt.FailedAttempts++
	log.Printf("[SECURITY] Failed auth attempt from IP %s: total failed attempts = %d",
		ip, attempt.FailedAttempts)

	// Check for lockout
	if attempt.FailedAttempts >= LockoutThreshold {
		attempt.LockedUntil = time.Now().Add(LockoutDuration)
		log.Printf("[SECURITY] IP %s locked out until %v due to %d failed attempts",
			ip, attempt.LockedUntil, attempt.FailedAttempts)
	}
}

// RecordSuccessfulAttempt resets the failed attempts counter on successful auth
func (rl *AuthRateLimiter) RecordSuccessfulAttempt(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	attempt, exists := rl.attempts[ip]
	if !exists {
		return
	}

	// Reset failed attempts on successful login
	attempt.FailedAttempts = 0
	attempt.LockedUntil = time.Time{}
}

// GetAttemptInfo returns information about an IP's rate limit status (for debugging/monitoring)
func (rl *AuthRateLimiter) GetAttemptInfo(ip string) (attempts int, failedAttempts int, lockedUntil time.Time, exists bool) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	attempt, ok := rl.attempts[ip]
	if !ok {
		return 0, 0, time.Time{}, false
	}

	return attempt.Attempts, attempt.FailedAttempts, attempt.LockedUntil, true
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Global rate limiter instance
var GlobalAuthRateLimiter = NewAuthRateLimiter()

// AuthRateLimitMiddleware creates a Gin middleware for auth rate limiting
// DC-001: Apply to login and other authentication endpoints
func AuthRateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		allowed, retryAfter, err := GlobalAuthRateLimiter.CheckRateLimit(ip)
		if !allowed {
			// Set Retry-After header
			c.Header("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())))
			c.Header("X-RateLimit-Remaining", "0")

			status := http.StatusTooManyRequests
			message := "Too many authentication attempts. Please try again later."
			if err != nil && err.Error() == "account locked due to too many failed attempts" {
				message = "Account temporarily locked due to too many failed attempts."
			}

			c.JSON(status, gin.H{
				"error":       message,
				"retry_after": int(retryAfter.Seconds()),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RecordAuthResult should be called after authentication to record the result
// DC-001: Call this in the login handler after authentication attempt
func RecordAuthResult(c *gin.Context, success bool) {
	ip := c.ClientIP()
	if success {
		GlobalAuthRateLimiter.RecordSuccessfulAttempt(ip)
	} else {
		GlobalAuthRateLimiter.RecordFailedAttempt(ip)
	}
}

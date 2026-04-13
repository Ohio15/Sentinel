package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// AgentRateLimiter implements per-source-IP rate limiting for the agent mTLS
// listener. It replaces the Traefik agentRateLimit middleware:
//
//	average: 300   (requests per period)
//	burst:   100
//	period:  1m
//	sourceCriterion.ipStrategy.depth: 1
//
// Each unique client IP gets its own token bucket (300 tokens/min, burst 100).
// Stale entries are cleaned up by a background goroutine.
type AgentRateLimiter struct {
	limiters sync.Map // string -> *clientLimiter
	rate     rate.Limit
	burst    int
}

type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
	mu       sync.Mutex // protects lastSeen
}

// NewAgentRateLimiter creates a rate limiter matching the Traefik agentRateLimit
// policy: 300 requests per minute with a burst allowance of 100.
func NewAgentRateLimiter() *AgentRateLimiter {
	l := &AgentRateLimiter{
		rate:  rate.Every(time.Minute / 300), // 5 tokens/sec = 300/min
		burst: 100,
	}
	go l.cleanupLoop()
	return l
}

// Middleware returns a Gin middleware that enforces the rate limit. Returns
// HTTP 429 Too Many Requests when the limit is exceeded.
func (l *AgentRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		val, _ := l.limiters.LoadOrStore(ip, &clientLimiter{
			limiter:  rate.NewLimiter(l.rate, l.burst),
			lastSeen: time.Now(),
		})
		cl := val.(*clientLimiter)

		cl.mu.Lock()
		cl.lastSeen = time.Now()
		cl.mu.Unlock()

		if !cl.limiter.Allow() {
			c.Header("Retry-After", "60")
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded. Please try again later.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// cleanupLoop removes rate limiter entries for IPs that haven't been seen in
// the last 15 minutes. Runs every 5 minutes until the process exits.
func (l *AgentRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cutoff := time.Now().Add(-15 * time.Minute)
		l.limiters.Range(func(key, value any) bool {
			cl := value.(*clientLimiter)
			cl.mu.Lock()
			stale := cl.lastSeen.Before(cutoff)
			cl.mu.Unlock()
			if stale {
				l.limiters.Delete(key)
			}
			return true
		})
	}
}

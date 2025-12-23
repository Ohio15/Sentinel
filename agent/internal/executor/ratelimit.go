package executor

import (
	"fmt"
	"sync"
	"time"
)

// =============================================================================
// CW-002 Security Fix: Rate Limiting for Command Execution
// Prevents command flooding attacks by limiting execution frequency
// =============================================================================

// RateLimiter implements a token bucket rate limiter for command execution
type RateLimiter struct {
	mu             sync.Mutex
	tokens         int
	maxTokens      int
	refillRate     int           // tokens per interval
	refillInterval time.Duration
	lastRefill     time.Time
}

// NewRateLimiter creates a new rate limiter with specified parameters
func NewRateLimiter(maxTokens int, refillRate int, refillInterval time.Duration) *RateLimiter {
	return &RateLimiter{
		tokens:         maxTokens,
		maxTokens:      maxTokens,
		refillRate:     refillRate,
		refillInterval: refillInterval,
		lastRefill:     time.Now(),
	}
}

// DefaultCommandRateLimiter returns a rate limiter allowing 30 commands per minute
// with burst capacity of 10 commands
func DefaultCommandRateLimiter() *RateLimiter {
	return NewRateLimiter(10, 30, time.Minute)
}

// Allow checks if a command can be executed under the rate limit
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.refill()

	if rl.tokens > 0 {
		rl.tokens--
		return true
	}
	return false
}

// refill adds tokens based on elapsed time
func (rl *RateLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill)

	// Calculate how many refill intervals have passed
	intervals := int(elapsed / rl.refillInterval)
	if intervals > 0 {
		rl.tokens += intervals * rl.refillRate
		if rl.tokens > rl.maxTokens {
			rl.tokens = rl.maxTokens
		}
		rl.lastRefill = now
	}
}

// GlobalCommandRateLimiter is the global rate limiter instance for command execution
var GlobalCommandRateLimiter = DefaultCommandRateLimiter()

// CheckRateLimit verifies if command execution is allowed under rate limits
func CheckRateLimit() error {
	if !GlobalCommandRateLimiter.Allow() {
		return fmt.Errorf("rate limit exceeded: too many commands executed, please wait before trying again")
	}
	return nil
}

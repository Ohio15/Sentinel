package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestAuthRateLimiter_FirstAttempt verifies first attempt is always allowed
func TestAuthRateLimiter_FirstAttempt(t *testing.T) {
	rl := NewAuthRateLimiter()
	defer rl.Stop()

	ip := "192.168.1.1"
	allowed, retryAfter, err := rl.CheckRateLimit(ip)

	if !allowed {
		t.Error("First attempt should be allowed")
	}
	if retryAfter != 0 {
		t.Errorf("Expected retryAfter=0, got %v", retryAfter)
	}
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

// TestAuthRateLimiter_WithinLimit verifies attempts within limit are allowed
func TestAuthRateLimiter_WithinLimit(t *testing.T) {
	rl := NewAuthRateLimiter()
	defer rl.Stop()

	ip := "192.168.1.2"

	// Make MaxAuthAttempts-1 attempts (should all be allowed)
	for i := 0; i < MaxAuthAttempts-1; i++ {
		allowed, _, err := rl.CheckRateLimit(ip)
		if !allowed {
			t.Errorf("Attempt %d should be allowed (within limit)", i+1)
		}
		if err != nil {
			t.Errorf("Attempt %d should not error, got %v", i+1, err)
		}
	}
}

// TestAuthRateLimiter_ExceedsLimit verifies rate limit is enforced
func TestAuthRateLimiter_ExceedsLimit(t *testing.T) {
	rl := NewAuthRateLimiter()
	defer rl.Stop()

	ip := "192.168.1.3"

	// Make MaxAuthAttempts attempts
	for i := 0; i < MaxAuthAttempts; i++ {
		rl.CheckRateLimit(ip)
	}

	// Next attempt should be blocked
	allowed, retryAfter, err := rl.CheckRateLimit(ip)

	if allowed {
		t.Error("Attempt exceeding limit should be blocked")
	}
	if retryAfter == 0 {
		t.Error("Expected non-zero retryAfter when rate limited")
	}
	if err == nil {
		t.Error("Expected error when rate limited")
	}
}

// TestAuthRateLimiter_WindowExpiry verifies rate limit window expiration
func TestAuthRateLimiter_WindowExpiry(t *testing.T) {
	rl := NewAuthRateLimiter()
	defer rl.Stop()

	ip := "192.168.1.4"

	// Make max attempts
	for i := 0; i < MaxAuthAttempts; i++ {
		rl.CheckRateLimit(ip)
	}

	// Verify blocked
	allowed, _, _ := rl.CheckRateLimit(ip)
	if allowed {
		t.Error("Should be blocked immediately after max attempts")
	}

	// Manipulate the first attempt time to simulate window expiry
	rl.mu.Lock()
	rl.attempts[ip].FirstAttempt = time.Now().Add(-AuthRateLimitWindow - time.Minute)
	rl.mu.Unlock()

	// Should be allowed again after window expires
	allowed, retryAfter, err := rl.CheckRateLimit(ip)
	if !allowed {
		t.Error("Should be allowed after window expiry")
	}
	if retryAfter != 0 {
		t.Error("Expected zero retryAfter after window expiry")
	}
	if err != nil {
		t.Errorf("Expected no error after window expiry, got %v", err)
	}
}

// TestAuthRateLimiter_FailedAttemptTracking verifies failed attempt counting
func TestAuthRateLimiter_FailedAttemptTracking(t *testing.T) {
	rl := NewAuthRateLimiter()
	defer rl.Stop()

	ip := "192.168.1.5"

	// Make an attempt
	rl.CheckRateLimit(ip)

	// Record 3 failed attempts
	for i := 0; i < 3; i++ {
		rl.RecordFailedAttempt(ip)
	}

	attempts, failedAttempts, _, exists := rl.GetAttemptInfo(ip)
	if !exists {
		t.Fatal("Expected attempt info to exist")
	}
	if attempts != 1 {
		t.Errorf("Expected 1 attempt, got %d", attempts)
	}
	if failedAttempts != 3 {
		t.Errorf("Expected 3 failed attempts, got %d", failedAttempts)
	}
}

// TestAuthRateLimiter_Lockout verifies lockout after threshold
func TestAuthRateLimiter_Lockout(t *testing.T) {
	rl := NewAuthRateLimiter()
	defer rl.Stop()

	ip := "192.168.1.6"

	// Make an attempt
	rl.CheckRateLimit(ip)

	// Record enough failed attempts to trigger lockout
	for i := 0; i < LockoutThreshold; i++ {
		rl.RecordFailedAttempt(ip)
	}

	// Verify lockout is active
	_, _, lockedUntil, _ := rl.GetAttemptInfo(ip)
	if lockedUntil.IsZero() {
		t.Error("Expected lockout to be set")
	}
	if time.Until(lockedUntil) <= 0 {
		t.Error("Expected lockout to be in the future")
	}

	// Attempt during lockout should be blocked
	allowed, retryAfter, err := rl.CheckRateLimit(ip)
	if allowed {
		t.Error("Should be blocked during lockout")
	}
	if retryAfter == 0 {
		t.Error("Expected non-zero retryAfter during lockout")
	}
	if err == nil || err.Error() != "account locked due to too many failed attempts" {
		t.Errorf("Expected lockout error, got %v", err)
	}
}

// TestAuthRateLimiter_SuccessfulAttemptReset verifies successful login resets counter
func TestAuthRateLimiter_SuccessfulAttemptReset(t *testing.T) {
	rl := NewAuthRateLimiter()
	defer rl.Stop()

	ip := "192.168.1.7"

	// Make an attempt
	rl.CheckRateLimit(ip)

	// Record some failed attempts
	for i := 0; i < 5; i++ {
		rl.RecordFailedAttempt(ip)
	}

	// Record successful attempt
	rl.RecordSuccessfulAttempt(ip)

	// Verify failed attempts were reset
	_, failedAttempts, lockedUntil, _ := rl.GetAttemptInfo(ip)
	if failedAttempts != 0 {
		t.Errorf("Expected failed attempts to be reset, got %d", failedAttempts)
	}
	if !lockedUntil.IsZero() {
		t.Error("Expected lockout to be cleared")
	}
}

// TestAuthRateLimiter_Cleanup verifies cleanup removes expired entries
func TestAuthRateLimiter_Cleanup(t *testing.T) {
	rl := NewAuthRateLimiter()
	defer rl.Stop()

	ip := "192.168.1.8"

	// Make an attempt
	rl.CheckRateLimit(ip)

	// Verify entry exists
	_, _, _, exists := rl.GetAttemptInfo(ip)
	if !exists {
		t.Fatal("Expected attempt info to exist")
	}

	// Simulate old entry
	rl.mu.Lock()
	rl.attempts[ip].FirstAttempt = time.Now().Add(-AuthRateLimitWindow - time.Hour)
	rl.mu.Unlock()

	// Run cleanup
	rl.cleanup()

	// Verify entry was removed
	_, _, _, exists = rl.GetAttemptInfo(ip)
	if exists {
		t.Error("Expected expired entry to be cleaned up")
	}
}

// TestAuthRateLimiter_MultipleIPs verifies isolation between IPs
func TestAuthRateLimiter_MultipleIPs(t *testing.T) {
	rl := NewAuthRateLimiter()
	defer rl.Stop()

	ip1 := "192.168.1.9"
	ip2 := "192.168.1.10"

	// Max out ip1
	for i := 0; i < MaxAuthAttempts; i++ {
		rl.CheckRateLimit(ip1)
	}

	// Verify ip1 is blocked
	allowed, _, _ := rl.CheckRateLimit(ip1)
	if allowed {
		t.Error("IP1 should be blocked")
	}

	// Verify ip2 is still allowed
	allowed, _, _ = rl.CheckRateLimit(ip2)
	if !allowed {
		t.Error("IP2 should not be affected by IP1's rate limit")
	}
}

// TestAuthRateLimitMiddleware_Allowed tests middleware allows valid requests
func TestAuthRateLimitMiddleware_Allowed(t *testing.T) {
	// Reset global rate limiter
	GlobalAuthRateLimiter.Stop()
	GlobalAuthRateLimiter = NewAuthRateLimiter()

	router := gin.New()
	router.Use(AuthRateLimitMiddleware())
	router.POST("/auth/login", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("POST", "/auth/login", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

// TestAuthRateLimitMiddleware_Blocked tests middleware blocks exceeded requests
func TestAuthRateLimitMiddleware_Blocked(t *testing.T) {
	// Reset global rate limiter
	GlobalAuthRateLimiter.Stop()
	GlobalAuthRateLimiter = NewAuthRateLimiter()

	router := gin.New()
	router.Use(AuthRateLimitMiddleware())
	router.POST("/auth/login", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	// Make max attempts
	for i := 0; i < MaxAuthAttempts; i++ {
		req := httptest.NewRequest("POST", "/auth/login", nil)
		req.RemoteAddr = "192.168.1.101:12345"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}

	// Next attempt should be blocked
	req := httptest.NewRequest("POST", "/auth/login", nil)
	req.RemoteAddr = "192.168.1.101:12345"
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status %d, got %d", http.StatusTooManyRequests, w.Code)
	}

	// Verify Retry-After header is set
	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("Expected Retry-After header to be set")
	}
}

// TestAuthRateLimitMiddleware_Lockout tests lockout response
func TestAuthRateLimitMiddleware_Lockout(t *testing.T) {
	// Reset global rate limiter
	GlobalAuthRateLimiter.Stop()
	GlobalAuthRateLimiter = NewAuthRateLimiter()

	router := gin.New()
	router.Use(AuthRateLimitMiddleware())
	router.POST("/auth/login", func(c *gin.Context) {
		// Simulate failed login
		RecordAuthResult(c, false)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
	})

	// Make enough failed attempts to trigger lockout
	for i := 0; i <= LockoutThreshold; i++ {
		req := httptest.NewRequest("POST", "/auth/login", nil)
		req.RemoteAddr = "192.168.1.102:12345"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}

	// Next attempt should show lockout message
	req := httptest.NewRequest("POST", "/auth/login", nil)
	req.RemoteAddr = "192.168.1.102:12345"
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status %d for locked account, got %d", http.StatusTooManyRequests, w.Code)
	}
}

// TestRecordAuthResult_Success verifies successful auth resets counters
func TestRecordAuthResult_Success(t *testing.T) {
	GlobalAuthRateLimiter.Stop()
	GlobalAuthRateLimiter = NewAuthRateLimiter()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/auth/login", nil)
	c.Request.RemoteAddr = "192.168.1.103:12345"

	// Record some failures
	for i := 0; i < 3; i++ {
		RecordAuthResult(c, false)
	}

	// Record success
	RecordAuthResult(c, true)

	// Verify failed attempts were reset
	ip := c.ClientIP()
	_, failedAttempts, _, _ := GlobalAuthRateLimiter.GetAttemptInfo(ip)
	if failedAttempts != 0 {
		t.Errorf("Expected failed attempts to be reset, got %d", failedAttempts)
	}
}

// TestRecordAuthResult_Failure verifies failed auth increments counter
func TestRecordAuthResult_Failure(t *testing.T) {
	GlobalAuthRateLimiter.Stop()
	GlobalAuthRateLimiter = NewAuthRateLimiter()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/auth/login", nil)
	c.Request.RemoteAddr = "192.168.1.104:12345"

	// Make initial attempt to create entry
	GlobalAuthRateLimiter.CheckRateLimit(c.ClientIP())

	// Record 3 failures
	for i := 0; i < 3; i++ {
		RecordAuthResult(c, false)
	}

	// Verify failed attempts were recorded
	ip := c.ClientIP()
	_, failedAttempts, _, _ := GlobalAuthRateLimiter.GetAttemptInfo(ip)
	if failedAttempts != 3 {
		t.Errorf("Expected 3 failed attempts, got %d", failedAttempts)
	}
}

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAgentRateLimiter_BurstAllowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	limiter := NewAgentRateLimiter()

	router := gin.New()
	router.Use(limiter.Middleware())
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// First 100 requests (burst) should all succeed
	for i := 0; i < 100; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, w.Code)
		}
	}
}

func TestAgentRateLimiter_ExceedBurstReturns429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	limiter := NewAgentRateLimiter()

	router := gin.New()
	router.Use(limiter.Middleware())
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Exhaust the burst
	for i := 0; i < 100; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.2:12345"
		router.ServeHTTP(w, req)
	}

	// Next request should be rate limited (tokens refill at 5/sec, but we
	// haven't waited, so the bucket is empty)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.2:12345"
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after burst exhaustion, got %d", w.Code)
	}

	// Verify Retry-After header is present
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on 429 response")
	}
}

func TestAgentRateLimiter_DifferentIPsIndependent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	limiter := NewAgentRateLimiter()

	router := gin.New()
	router.Use(limiter.Middleware())
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Exhaust burst for IP A
	for i := 0; i < 100; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.3:12345"
		router.ServeHTTP(w, req)
	}

	// IP B should still work fine
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.4:12345"
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for different IP, got %d", w.Code)
	}
}

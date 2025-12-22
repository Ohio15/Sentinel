package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	csrfTokenLength = 32
	csrfCookieName  = "csrf_token"
	csrfHeaderName  = "X-CSRF-Token"
)

// CSRFConfig holds CSRF middleware configuration
type CSRFConfig struct {
	// Secure determines if the cookie should only be sent over HTTPS
	Secure bool
	// Domain is the cookie domain
	Domain string
	// Path is the cookie path
	Path string
	// SameSite controls the SameSite attribute
	SameSite http.SameSite
	// SkipPaths are paths that should skip CSRF validation (e.g., agent endpoints)
	SkipPaths []string
	// SkipMethods are HTTP methods that don't require CSRF validation
	SkipMethods []string
}

// DefaultCSRFConfig returns a default CSRF configuration
func DefaultCSRFConfig() CSRFConfig {
	return CSRFConfig{
		Secure:      true,
		Path:        "/",
		SameSite:    http.SameSiteStrictMode,
		SkipPaths:   []string{"/api/auth/login", "/api/agent/", "/ws/", "/health"},
		SkipMethods: []string{"GET", "HEAD", "OPTIONS"},
	}
}

// CSRFMiddleware provides CSRF protection for state-changing requests
func CSRFMiddleware(config CSRFConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip CSRF for safe methods
		for _, method := range config.SkipMethods {
			if c.Request.Method == method {
				// Still set token for future use
				ensureCSRFToken(c, config)
				c.Next()
				return
			}
		}

		// Skip CSRF for specific paths (agent endpoints, WebSocket, etc.)
		path := c.Request.URL.Path
		for _, skipPath := range config.SkipPaths {
			if strings.HasPrefix(path, skipPath) {
				c.Next()
				return
			}
		}

		// Validate CSRF token for state-changing requests
		cookieToken, err := c.Cookie(csrfCookieName)
		if err != nil || cookieToken == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "CSRF token missing from cookie"})
			c.Abort()
			return
		}

		headerToken := c.GetHeader(csrfHeaderName)
		if headerToken == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "CSRF token missing from header"})
			c.Abort()
			return
		}

		// Constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(cookieToken), []byte(headerToken)) != 1 {
			c.JSON(http.StatusForbidden, gin.H{"error": "CSRF token mismatch"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// ensureCSRFToken ensures a CSRF token exists in the response cookie
func ensureCSRFToken(c *gin.Context, config CSRFConfig) {
	// Check if cookie already exists
	if _, err := c.Cookie(csrfCookieName); err == nil {
		return
	}

	// Generate new token
	token, err := generateCSRFToken()
	if err != nil {
		return
	}

	// Set cookie
	maxAge := 86400 // 24 hours
	c.SetSameSite(config.SameSite)
	c.SetCookie(csrfCookieName, token, maxAge, config.Path, config.Domain, config.Secure, false)
}

// generateCSRFToken generates a cryptographically secure random token
func generateCSRFToken() (string, error) {
	b := make([]byte, csrfTokenLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// GetCSRFToken returns the current CSRF token from the request context
// This can be used by handlers that need to include the token in responses
func GetCSRFToken(c *gin.Context) string {
	token, _ := c.Cookie(csrfCookieName)
	return token
}

// SetNewCSRFToken generates and sets a new CSRF token (useful after login)
func SetNewCSRFToken(c *gin.Context, config CSRFConfig) string {
	token, err := generateCSRFToken()
	if err != nil {
		return ""
	}

	maxAge := 86400 // 24 hours
	c.SetSameSite(config.SameSite)
	c.SetCookie(csrfCookieName, token, maxAge, config.Path, config.Domain, config.Secure, false)
	return token
}

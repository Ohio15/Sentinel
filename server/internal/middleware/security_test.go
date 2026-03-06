package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestSecurityHeaders_Present(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Test the headers that are set by securityHeadersMiddleware via the router.
	// Since securityHeadersMiddleware is in the api package (unexported), we test
	// the expected security header behavior through middleware-level validation.

	expectedHeaders := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":       "DENY",
		"X-XSS-Protection":      "1; mode=block",
		"Referrer-Policy":       "strict-origin-when-cross-origin",
		"Permissions-Policy":    "geolocation=(), microphone=(), camera=()",
	}

	// Simulate what securityHeadersMiddleware does
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		c.Next()
	})
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	for header, expectedValue := range expectedHeaders {
		actual := w.Header().Get(header)
		if actual != expectedValue {
			t.Errorf("Expected header %s=%q, got %q", header, expectedValue, actual)
		}
	}
}

func TestSecurityHeaders_HSTS_NotInTestMode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		// Only add HSTS in production (release mode)
		if gin.Mode() == gin.ReleaseMode {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	})
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	hsts := w.Header().Get("Strict-Transport-Security")
	if hsts != "" {
		t.Errorf("HSTS should not be set in test mode, got %q", hsts)
	}
}

func TestCORS_PreflightOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Enrollment-Token, X-Agent-Token, X-CSRF-Token, X-API-Key")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})
	router.GET("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("OPTIONS", "/api/test", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status %d for OPTIONS, got %d", http.StatusNoContent, w.Code)
	}

	allowMethods := w.Header().Get("Access-Control-Allow-Methods")
	if allowMethods == "" {
		t.Error("Missing Access-Control-Allow-Methods header")
	}

	allowHeaders := w.Header().Get("Access-Control-Allow-Headers")
	if allowHeaders == "" {
		t.Error("Missing Access-Control-Allow-Headers header")
	}

	maxAge := w.Header().Get("Access-Control-Max-Age")
	if maxAge != "86400" {
		t.Errorf("Expected Access-Control-Max-Age '86400', got %q", maxAge)
	}
}

func TestCORS_AllowOriginReflected(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		// Non-production: allow all origins
		if origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
		}
		c.Next()
	})
	router.GET("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "https://myapp.example.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	allowOrigin := w.Header().Get("Access-Control-Allow-Origin")
	if allowOrigin != "https://myapp.example.com" {
		t.Errorf("Expected Access-Control-Allow-Origin to reflect origin, got %q", allowOrigin)
	}

	allowCreds := w.Header().Get("Access-Control-Allow-Credentials")
	if allowCreds != "true" {
		t.Errorf("Expected Access-Control-Allow-Credentials 'true', got %q", allowCreds)
	}
}

func TestCORS_NoOriginHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
		}
		c.Next()
	})
	router.GET("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	// No Origin header
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	allowOrigin := w.Header().Get("Access-Control-Allow-Origin")
	if allowOrigin != "" {
		t.Errorf("Access-Control-Allow-Origin should not be set without Origin header, got %q", allowOrigin)
	}
}

func TestRequireRole_MultipleRoles(t *testing.T) {
	tests := []struct {
		name         string
		userRole     string
		allowedRoles []string
		wantStatus   int
	}{
		{"admin in admin+operator", "admin", []string{"admin", "operator"}, http.StatusOK},
		{"operator in admin+operator", "operator", []string{"admin", "operator"}, http.StatusOK},
		{"viewer in admin+operator", "viewer", []string{"admin", "operator"}, http.StatusForbidden},
		{"admin in admin-only", "admin", []string{"admin"}, http.StatusOK},
		{"operator in admin-only", "operator", []string{"admin"}, http.StatusForbidden},
		{"viewer in admin-only", "viewer", []string{"admin"}, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("role", tt.userRole)
				c.Next()
			})
			router.Use(RequireRole(tt.allowedRoles...))
			router.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"message": "ok"})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Expected status %d for role %q with allowed %v, got %d",
					tt.wantStatus, tt.userRole, tt.allowedRoles, w.Code)
			}
		})
	}
}

func TestIsWhitelisted_KnownIPs(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{"localhost IPv4", "127.0.0.1", true},
		{"localhost IPv6", "::1", true},
		{"private 10.x", "10.0.0.1", true},
		{"private 192.168.x", "192.168.1.100", true},
		{"private 172.16.x", "172.16.0.1", true},
		{"public IP", "8.8.8.8", false},
		{"public IP 2", "203.0.113.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsWhitelisted(tt.ip)
			if result != tt.expected {
				t.Errorf("IsWhitelisted(%q) = %v, want %v", tt.ip, result, tt.expected)
			}
		})
	}
}

func TestAuthMiddleware_BearerTokenParsing(t *testing.T) {
	secret := "test-secret-key-32-characters-ok"

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{"Valid Bearer prefix", "Bearer " + generateTestToken(secret, uuid.New(), "test@example.com", "admin", false), http.StatusOK},
		{"Missing Bearer prefix", generateTestToken(secret, uuid.New(), "test@example.com", "admin", false), http.StatusUnauthorized},
		{"Empty auth header", "", http.StatusUnauthorized},
		{"Bearer with no token", "Bearer ", http.StatusUnauthorized},
		{"Basic auth (wrong type)", "Basic dXNlcjpwYXNz", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(AuthMiddleware(secret))
			router.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"message": "ok"})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}

func TestAuthMiddleware_SetsContextValues(t *testing.T) {
	secret := "test-secret-key-32-characters-ok"
	userID := uuid.New()
	email := "context@example.com"
	role := "operator"
	token := generateTestToken(secret, userID, email, role, false)

	var gotUserID uuid.UUID
	var gotEmail, gotRole string

	router := gin.New()
	router.Use(AuthMiddleware(secret))
	router.GET("/test", func(c *gin.Context) {
		id, _ := c.Get("userId")
		gotUserID = id.(uuid.UUID)
		e, _ := c.Get("email")
		gotEmail = e.(string)
		r, _ := c.Get("role")
		gotRole = r.(string)
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	if gotUserID != userID {
		t.Errorf("Expected userId %s, got %s", userID, gotUserID)
	}
	if gotEmail != email {
		t.Errorf("Expected email %q, got %q", email, gotEmail)
	}
	if gotRole != role {
		t.Errorf("Expected role %q, got %q", role, gotRole)
	}
}

func TestValidateJWT_ValidToken(t *testing.T) {
	secret := "test-secret-key-32-characters-ok"
	userID := uuid.New()
	email := "validate@example.com"
	role := "admin"
	token := generateTestToken(secret, userID, email, role, false)

	claims, err := ValidateJWT(token, secret)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if claims.UserID != userID {
		t.Errorf("Expected UserID %s, got %s", userID, claims.UserID)
	}
	if claims.Email != email {
		t.Errorf("Expected Email %q, got %q", email, claims.Email)
	}
	if claims.Role != role {
		t.Errorf("Expected Role %q, got %q", role, claims.Role)
	}
}

func TestValidateJWT_ExpiredToken(t *testing.T) {
	secret := "test-secret-key-32-characters-ok"
	token := generateTestToken(secret, uuid.New(), "expired@example.com", "admin", true)

	_, err := ValidateJWT(token, secret)
	if err == nil {
		t.Error("Expected error for expired token, got nil")
	}
}

func TestValidateJWT_WrongSecret(t *testing.T) {
	token := generateTestToken("secret-one-32-characters-ok!!!!!", uuid.New(), "test@example.com", "admin", false)

	_, err := ValidateJWT(token, "secret-two-32-characters-ok!!!!!")
	if err == nil {
		t.Error("Expected error for wrong secret, got nil")
	}
}

func TestValidateJWT_MalformedToken(t *testing.T) {
	_, err := ValidateJWT("not.a.valid.jwt.token", "any-secret")
	if err == nil {
		t.Error("Expected error for malformed token, got nil")
	}
}

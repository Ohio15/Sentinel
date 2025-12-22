package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCSRFMiddleware_SkipSafeMethods(t *testing.T) {
	config := DefaultCSRFConfig()

	router := gin.New()
	router.Use(CSRFMiddleware(config))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	// GET should be skipped
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET should pass without CSRF token, got status %d", w.Code)
	}
}

func TestCSRFMiddleware_SkipPaths(t *testing.T) {
	config := DefaultCSRFConfig()

	router := gin.New()
	router.Use(CSRFMiddleware(config))
	router.POST("/api/auth/login", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	// Login should be skipped
	req := httptest.NewRequest("POST", "/api/auth/login", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Login should pass without CSRF token, got status %d", w.Code)
	}
}

func TestCSRFMiddleware_MissingCookie(t *testing.T) {
	config := DefaultCSRFConfig()

	router := gin.New()
	router.Use(CSRFMiddleware(config))
	router.POST("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("POST", "/api/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for missing cookie, got %d", http.StatusForbidden, w.Code)
	}
}

func TestCSRFMiddleware_MissingHeader(t *testing.T) {
	config := DefaultCSRFConfig()

	router := gin.New()
	router.Use(CSRFMiddleware(config))
	router.POST("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("POST", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "test-csrf-token"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for missing header, got %d", http.StatusForbidden, w.Code)
	}
}

func TestCSRFMiddleware_TokenMismatch(t *testing.T) {
	config := DefaultCSRFConfig()

	router := gin.New()
	router.Use(CSRFMiddleware(config))
	router.POST("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("POST", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "cookie-token"})
	req.Header.Set(csrfHeaderName, "different-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for token mismatch, got %d", http.StatusForbidden, w.Code)
	}
}

func TestCSRFMiddleware_ValidToken(t *testing.T) {
	config := DefaultCSRFConfig()

	router := gin.New()
	router.Use(CSRFMiddleware(config))
	router.POST("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	csrfToken := "valid-csrf-token-for-testing"
	req := httptest.NewRequest("POST", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	req.Header.Set(csrfHeaderName, csrfToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d for valid token, got %d", http.StatusOK, w.Code)
	}
}

func TestCSRFMiddleware_PUTRequest(t *testing.T) {
	config := DefaultCSRFConfig()

	router := gin.New()
	router.Use(CSRFMiddleware(config))
	router.PUT("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	csrfToken := "valid-csrf-token-for-testing"
	req := httptest.NewRequest("PUT", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	req.Header.Set(csrfHeaderName, csrfToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d for PUT with valid token, got %d", http.StatusOK, w.Code)
	}
}

func TestCSRFMiddleware_DELETERequest(t *testing.T) {
	config := DefaultCSRFConfig()

	router := gin.New()
	router.Use(CSRFMiddleware(config))
	router.DELETE("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	csrfToken := "valid-csrf-token-for-testing"
	req := httptest.NewRequest("DELETE", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	req.Header.Set(csrfHeaderName, csrfToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d for DELETE with valid token, got %d", http.StatusOK, w.Code)
	}
}

func TestGenerateCSRFToken(t *testing.T) {
	token1, err1 := generateCSRFToken()
	if err1 != nil {
		t.Errorf("Failed to generate CSRF token: %v", err1)
	}

	token2, err2 := generateCSRFToken()
	if err2 != nil {
		t.Errorf("Failed to generate CSRF token: %v", err2)
	}

	if token1 == token2 {
		t.Error("Generated tokens should be unique")
	}

	if len(token1) < 32 {
		t.Errorf("Token should be at least 32 characters, got %d", len(token1))
	}
}

func TestSetNewCSRFToken(t *testing.T) {
	config := DefaultCSRFConfig()

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		token := SetNewCSRFToken(c, config)
		c.JSON(http.StatusOK, gin.H{"token": token})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Check that Set-Cookie header is present
	cookies := w.Result().Cookies()
	found := false
	for _, cookie := range cookies {
		if cookie.Name == csrfCookieName {
			found = true
			if cookie.Value == "" {
				t.Error("CSRF cookie value should not be empty")
			}
			break
		}
	}

	if !found {
		t.Error("CSRF cookie should be set")
	}
}

func TestDefaultCSRFConfig(t *testing.T) {
	config := DefaultCSRFConfig()

	if !config.Secure {
		t.Error("Secure should be true by default")
	}

	if config.Path != "/" {
		t.Errorf("Path should be '/', got '%s'", config.Path)
	}

	if config.SameSite != http.SameSiteStrictMode {
		t.Error("SameSite should be Strict by default")
	}

	if len(config.SkipPaths) == 0 {
		t.Error("SkipPaths should have default values")
	}

	if len(config.SkipMethods) == 0 {
		t.Error("SkipMethods should have default values")
	}
}

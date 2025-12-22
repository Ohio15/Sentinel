package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func generateTestToken(secret string, userID uuid.UUID, email, role string, expired bool) string {
	expiresAt := time.Now().Add(time.Hour)
	if expired {
		expiresAt = time.Now().Add(-time.Hour)
	}

	claims := Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "sentinel",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte(secret))
	return tokenString
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	secret := "test-secret-key-32-characters-ok"
	userID := uuid.New()
	token := generateTestToken(secret, userID, "test@example.com", "admin", false)

	router := gin.New()
	router.Use(AuthMiddleware(secret))
	router.GET("/test", func(c *gin.Context) {
		id, _ := c.Get("userId")
		email, _ := c.Get("email")
		role, _ := c.Get("role")

		c.JSON(http.StatusOK, gin.H{
			"userId": id,
			"email":  email,
			"role":   role,
		})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	secret := "test-secret-key-32-characters-ok"

	router := gin.New()
	router.Use(AuthMiddleware(secret))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	secret := "test-secret-key-32-characters-ok"

	router := gin.New()
	router.Use(AuthMiddleware(secret))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestAuthMiddleware_ExpiredToken(t *testing.T) {
	secret := "test-secret-key-32-characters-ok"
	userID := uuid.New()
	token := generateTestToken(secret, userID, "test@example.com", "admin", true)

	router := gin.New()
	router.Use(AuthMiddleware(secret))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestAuthMiddleware_WrongSecret(t *testing.T) {
	correctSecret := "test-secret-key-32-characters-ok"
	wrongSecret := "wrong-secret-key-32-characters!!"
	userID := uuid.New()
	token := generateTestToken(wrongSecret, userID, "test@example.com", "admin", false)

	router := gin.New()
	router.Use(AuthMiddleware(correctSecret))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestAuthMiddleware_TokenFromQuery(t *testing.T) {
	secret := "test-secret-key-32-characters-ok"
	userID := uuid.New()
	token := generateTestToken(secret, userID, "test@example.com", "admin", false)

	router := gin.New()
	router.Use(AuthMiddleware(secret))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("GET", "/test?token="+token, nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestAgentAuthMiddleware_ValidToken(t *testing.T) {
	enrollmentToken := "valid-enrollment-token"

	router := gin.New()
	router.Use(AgentAuthMiddleware(enrollmentToken))
	router.POST("/agent/enroll", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("POST", "/agent/enroll", nil)
	req.Header.Set("X-Enrollment-Token", enrollmentToken)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestAgentAuthMiddleware_InvalidToken(t *testing.T) {
	enrollmentToken := "valid-enrollment-token"

	router := gin.New()
	router.Use(AgentAuthMiddleware(enrollmentToken))
	router.POST("/agent/enroll", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("POST", "/agent/enroll", nil)
	req.Header.Set("X-Enrollment-Token", "invalid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestAgentAuthMiddleware_MissingToken(t *testing.T) {
	enrollmentToken := "valid-enrollment-token"

	router := gin.New()
	router.Use(AgentAuthMiddleware(enrollmentToken))
	router.POST("/agent/enroll", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("POST", "/agent/enroll", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestRequireRole_AllowedRole(t *testing.T) {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Next()
	})
	router.Use(RequireRole("admin", "operator"))
	router.GET("/admin", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestRequireRole_DeniedRole(t *testing.T) {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("role", "viewer")
		c.Next()
	})
	router.Use(RequireRole("admin", "operator"))
	router.GET("/admin", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, w.Code)
	}
}

func TestRequireRole_NoRole(t *testing.T) {
	router := gin.New()
	router.Use(RequireRole("admin"))
	router.GET("/admin", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

package middleware

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"log"
	"time"
)

var ErrInvalidSigningMethod = errors.New("invalid signing method")

type Claims struct {
	UserID uuid.UUID `json:"userId"`
	Email  string    `json:"email"`
	Role   string    `json:"role"`
	jwt.RegisteredClaims
}

// ValidateJWT parses and validates a JWT token string, returning the claims if valid.
func ValidateJWT(tokenString string, jwtSecret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidSigningMethod
		}
		return []byte(jwtSecret), nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, errors.New("invalid token claims")
}

func AuthMiddleware(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var tokenString string

		// Try Authorization header first
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			// Parse Bearer token
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				tokenString = parts[1]
			}
		}

		// For WebSocket connections, also check query parameter
		if tokenString == "" {
			tokenString = c.Query("token")
			if tokenString != "" {
				log.Printf("[DEPRECATION] Token in query string from %s path=%s — use Authorization header", c.ClientIP(), c.Request.URL.Path)
			}
		}

		// Debug logging for WebSocket auth failures
		isWS := strings.Contains(c.Request.URL.Path, "/ws/")
		if isWS {
			log.Printf("[AUTH-WS] Path=%s Query=%s HasHeader=%v TokenLen=%d ClientIP=%s",
				c.Request.URL.Path, c.Request.URL.RawQuery, authHeader != "", len(tokenString), c.ClientIP())
		}

		if tokenString == "" {
			if isWS {
				log.Printf("[AUTH-WS] REJECTED: No token found for %s from %s", c.Request.URL.Path, c.ClientIP())
			}
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization required"})
			c.Abort()
			return
		}

		// Parse and validate token with algorithm validation
		token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			// Validate the signing method to prevent "none" algorithm attacks
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, ErrInvalidSigningMethod
			}
			return []byte(jwtSecret), nil
		})

		if err != nil {
			if isWS {
				log.Printf("[AUTH-WS] REJECTED: Invalid token for %s from %s: %v", c.Request.URL.Path, c.ClientIP(), err)
			}
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		if claims, ok := token.Claims.(*Claims); ok && token.Valid {
			c.Set("userId", claims.UserID)
			c.Set("email", claims.Email)
			c.Set("role", claims.Role)
			c.Next()
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
			c.Abort()
			return
		}
	}
}

func AgentAuthMiddleware(enrollmentToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("X-Enrollment-Token")
		if token == "" {
			token = c.GetHeader("X-Agent-Token")
		}

		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Agent token required"})
			c.Abort()
			return
		}

		// Use constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(token), []byte(enrollmentToken)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid agent token"})
			c.Abort()
			return
		}

		c.Next()
	}
}

func AuthOrAPIKeyMiddleware(jwtSecret, apiKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check for API key first
		if key := c.GetHeader("X-API-Key"); key != "" && apiKey != "" {
			if subtle.ConstantTimeCompare([]byte(key), []byte(apiKey)) == 1 {
				c.Set("userId", uuid.Nil)
				c.Set("email", "api-key")
				c.Set("role", "operator") // Previously "admin" — principle of least privilege
				log.Printf("[WARN] API key used without explicit role, defaulting to operator. Set role in api_keys table.")
				c.Next()
				return
			}
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
			c.Abort()
			return
		}

		// Fall back to JWT auth
		var tokenString string
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				tokenString = parts[1]
			}
		}

		if tokenString == "" {
			tokenString = c.Query("token")
			if tokenString != "" {
				log.Printf("[DEPRECATION] Token in query string from %s path=%s — use Authorization header", c.ClientIP(), c.Request.URL.Path)
			}
		}

		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization required"})
			c.Abort()
			return
		}

		token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, ErrInvalidSigningMethod
			}
			return []byte(jwtSecret), nil
		})

		if err != nil {
			log.Printf("[AUTH] JWT parse error: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		if claims, ok := token.Claims.(*Claims); ok && token.Valid {
			c.Set("userId", claims.UserID)
			c.Set("email", claims.Email)
			c.Set("role", claims.Role)
			c.Next()
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
			c.Abort()
		}
	}
}

func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Role not found"})
			c.Abort()
			return
		}

		role := userRole.(string)
		for _, r := range roles {
			if r == role {
				c.Next()
				return
			}
		}

		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
		c.Abort()
	}
}

// NewAgentAuthMiddleware validates agent tokens against the database
// CW-003: Supports both legacy (plain text) and bcrypt-hashed tokens
func NewAgentAuthMiddleware(pool *pgxpool.Pool, fallbackToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("X-Enrollment-Token")
		if token == "" {
			token = c.GetHeader("X-Agent-Token")
		}

		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Agent token required"})
			c.Abort()
			return
		}

		// Try database validation first
		if pool != nil {
			valid, tokenID := validateDatabaseToken(c.Request.Context(), pool, token)
			if valid {
				c.Set("enrollmentTokenID", tokenID)
				c.Next()
				return
			}
		}

		// Fallback to static token for backwards compatibility
		if fallbackToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(fallbackToken)) == 1 {
			c.Next()
			return
		}

		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid agent token"})
		c.Abort()
	}
}

// validateDatabaseToken checks if a token is valid in the database
func validateDatabaseToken(ctx context.Context, pool *pgxpool.Pool, token string) (bool, uuid.UUID) {
	// Fast path: try plaintext exact match for legacy tokens first (O(1) via index)
	var legacyTokenID uuid.UUID
	err := pool.QueryRow(ctx, `
		SELECT id FROM enrollment_tokens
		WHERE token = $1 AND is_active = TRUE AND is_legacy = TRUE
		AND (expires_at IS NULL OR expires_at > NOW())
		AND (max_uses IS NULL OR use_count < max_uses)
		LIMIT 1
	`, token).Scan(&legacyTokenID)
	if err == nil {
		log.Printf("[PERF] Enrollment token validation checked 1 tokens")
		return true, legacyTokenID
	}

	// Slow path: check bcrypt-hashed tokens (O(n) but limited)
	rows, err := pool.Query(ctx, `
		SELECT id, token, token_hash, is_legacy, expires_at, max_uses, use_count
		FROM enrollment_tokens
		WHERE is_active = TRUE
		AND (expires_at IS NULL OR expires_at > NOW())
		AND (max_uses IS NULL OR use_count < max_uses)
		LIMIT 50
	`)
	if err != nil {
		log.Printf("[AUTH] Failed to query enrollment tokens: %v", err)
		return false, uuid.Nil
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var tokenID uuid.UUID
		var plainToken *string
		var tokenHash *string
		var isLegacy bool
		var expiresAt *time.Time
		var maxUses *int
		var useCount int

		err := rows.Scan(&tokenID, &plainToken, &tokenHash, &isLegacy, &expiresAt, &maxUses, &useCount)
		if err != nil {
			log.Printf("[AUTH] Error scanning token row: %v", err)
			continue
		}
		count++

		// Validate token based on type
		var valid bool
		if tokenHash != nil && *tokenHash != "" {
			// Hashed token: use bcrypt comparison (preferred)
			err := bcrypt.CompareHashAndPassword([]byte(*tokenHash), []byte(token))
			valid = err == nil
		} else if plainToken != nil && *plainToken != "" {
			// Plain text token: use constant-time comparison
			// Handles both legacy tokens and tokens created without a hash
			valid = subtle.ConstantTimeCompare([]byte(token), []byte(*plainToken)) == 1
		}

		if valid {
			log.Printf("[PERF] Enrollment token validation checked %d tokens", count)
			return true, tokenID
		}
	}

	log.Printf("[PERF] Enrollment token validation checked %d tokens", count)
	return false, uuid.Nil
}

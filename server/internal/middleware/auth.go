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
				c.Set("role", "admin")
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
			log.Printf("[AUTH-ENROLL] REJECTED: No token header from IP %s for %s", c.ClientIP(), c.Request.URL.Path)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Agent token required"})
			c.Abort()
			return
		}

		// TEMP DEBUG: Log enrollment attempt with masked token
		maskedToken := token
		if len(maskedToken) > 8 {
			maskedToken = maskedToken[:4] + "..." + maskedToken[len(maskedToken)-4:]
		}
		log.Printf("[AUTH-ENROLL] Attempt from IP %s, token=%s, path=%s", c.ClientIP(), maskedToken, c.Request.URL.Path)

		// Try database validation first
		if pool != nil {
			valid, tokenID := validateDatabaseToken(c.Request.Context(), pool, token)
			if valid {
				log.Printf("[AUTH-ENROLL] SUCCESS: Token matched DB token %s for IP %s", tokenID, c.ClientIP())
				c.Set("enrollmentTokenID", tokenID)
				c.Next()
				return
			}
			log.Printf("[AUTH-ENROLL] No database token match for IP %s", c.ClientIP())
		}

		// Fallback to static token for backwards compatibility
		if fallbackToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(fallbackToken)) == 1 {
			log.Printf("[AUTH-ENROLL] SUCCESS: Matched fallback token for IP %s", c.ClientIP())
			c.Next()
			return
		}

		log.Printf("[AUTH-ENROLL] REJECTED: No matching token for IP %s (token=%s, hasFallback=%v)", c.ClientIP(), maskedToken, fallbackToken != "")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid agent token"})
		c.Abort()
	}
}

// validateDatabaseToken checks if a token is valid in the database
func validateDatabaseToken(ctx context.Context, pool *pgxpool.Pool, token string) (bool, uuid.UUID) {
	// Query all active tokens and check each one
	rows, err := pool.Query(ctx, `
		SELECT id, token, token_hash, is_legacy, expires_at, max_uses, use_count, name
		FROM enrollment_tokens
		WHERE is_active = TRUE
	`)
	if err != nil {
		log.Printf("[AUTH] Failed to query enrollment tokens: %v", err)
		return false, uuid.Nil
	}
	defer rows.Close()

	for rows.Next() {
		var tokenID uuid.UUID
		var plainToken *string
		var tokenHash *string
		var isLegacy bool
		var expiresAt *time.Time
		var maxUses *int
		var useCount int
		var tokenName *string

		err := rows.Scan(&tokenID, &plainToken, &tokenHash, &isLegacy, &expiresAt, &maxUses, &useCount, &tokenName)
		if err != nil {
			log.Printf("[AUTH] Error scanning token row: %v", err)
			continue
		}

		name := "<unnamed>"
		if tokenName != nil {
			name = *tokenName
		}

		// Check expiration
		if expiresAt != nil && time.Now().After(*expiresAt) {
			log.Printf("[AUTH-ENROLL-DBG] Token %s (%s) skipped: expired at %v", tokenID, name, expiresAt)
			continue
		}

		// Check usage limits
		if maxUses != nil && useCount >= *maxUses {
			log.Printf("[AUTH-ENROLL-DBG] Token %s (%s) skipped: use_count %d >= max_uses %d", tokenID, name, useCount, *maxUses)
			continue
		}

		// Validate token based on type
		var valid bool
		if isLegacy && plainToken != nil {
			// Legacy token: use constant-time comparison
			valid = subtle.ConstantTimeCompare([]byte(token), []byte(*plainToken)) == 1
		} else if tokenHash != nil && *tokenHash != "" {
			// Hashed token: use bcrypt comparison
			err := bcrypt.CompareHashAndPassword([]byte(*tokenHash), []byte(token))
			valid = err == nil
		}

		if valid {
			return true, tokenID
		}
	}

	return false, uuid.Nil
}

package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/middleware"
	"golang.org/x/crypto/bcrypt"
)

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

type LoginResponse struct {
	AccessToken  string       `json:"accessToken"`
	RefreshToken string       `json:"refreshToken"`
	ExpiresIn    int64        `json:"expiresIn"`
	User         UserResponse `json:"user"`
}

type UserResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	FirstName string    `json:"firstName"`
	LastName  string    `json:"lastName"`
	Role      string    `json:"role"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refreshToken" binding:"required"`
}

func (r *Router) login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	ctx := context.Background()

	// Get user by email
	var user struct {
		ID           uuid.UUID
		Email        string
		PasswordHash string
		FirstName    string
		LastName     string
		Role         string
		IsActive     bool
	}

	err := r.db.Pool.QueryRow(ctx, `
		SELECT id, email, password_hash, first_name, last_name, role, is_active
		FROM users WHERE email = $1
	`, req.Email).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.FirstName, &user.LastName, &user.Role, &user.IsActive)

	if err != nil {
		// Constant-time comparison to prevent timing attacks
		bcrypt.CompareHashAndPassword([]byte("$2b$10$dummy"), []byte(req.Password))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	if !user.IsActive {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Account is disabled"})
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Generate tokens
	accessToken, err := r.generateAccessToken(user.ID, user.Email, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	refreshToken, err := r.generateRefreshToken(user.ID, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate refresh token"})
		return
	}

	// Update last login
	r.db.Pool.Exec(ctx, "UPDATE users SET last_login = NOW() WHERE id = $1", user.ID)

	c.JSON(http.StatusOK, LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    3600, // 1 hour
		User: UserResponse{
			ID:        user.ID,
			Email:     user.Email,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			Role:      user.Role,
		},
	})
}

func (r *Router) refreshToken(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	ctx := context.Background()

	// Hash the refresh token to look it up
	tokenHash := hashToken(req.RefreshToken)

	// Find session
	var session struct {
		ID        uuid.UUID
		UserID    uuid.UUID
		ExpiresAt time.Time
	}

	err := r.db.Pool.QueryRow(ctx, `
		SELECT id, user_id, expires_at FROM sessions
		WHERE refresh_token_hash = $1
	`, tokenHash).Scan(&session.ID, &session.UserID, &session.ExpiresAt)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid refresh token"})
		return
	}

	if time.Now().After(session.ExpiresAt) {
		r.db.Pool.Exec(ctx, "DELETE FROM sessions WHERE id = $1", session.ID)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Refresh token expired"})
		return
	}

	// Get user
	var user struct {
		Email    string
		Role     string
		IsActive bool
	}

	err = r.db.Pool.QueryRow(ctx, `
		SELECT email, role, is_active FROM users WHERE id = $1
	`, session.UserID).Scan(&user.Email, &user.Role, &user.IsActive)

	if err != nil || !user.IsActive {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found or inactive"})
		return
	}

	// Generate new access token
	accessToken, err := r.generateAccessToken(session.UserID, user.Email, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"accessToken": accessToken,
		"expiresIn":   3600,
	})
}

func (r *Router) logout(c *gin.Context) {
	userID := c.MustGet("userId").(uuid.UUID)
	ctx := context.Background()

	// Delete all sessions for user
	r.db.Pool.Exec(ctx, "DELETE FROM sessions WHERE user_id = $1", userID)

	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

func (r *Router) me(c *gin.Context) {
	userID := c.MustGet("userId").(uuid.UUID)
	ctx := context.Background()

	var user UserResponse
	err := r.db.Pool.QueryRow(ctx, `
		SELECT id, email, first_name, last_name, role
		FROM users WHERE id = $1
	`, userID).Scan(&user.ID, &user.Email, &user.FirstName, &user.LastName, &user.Role)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, user)
}

func (r *Router) generateAccessToken(userID uuid.UUID, email, role string) (string, error) {
	claims := middleware.Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "sentinel",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(r.config.JWTSecret))
}

func (r *Router) generateRefreshToken(userID uuid.UUID, ipAddress, userAgent string) (string, error) {
	ctx := context.Background()
	token := uuid.New().String()
	tokenHash := hashToken(token)
	expiresAt := time.Now().Add(7 * 24 * time.Hour) // 7 days

	_, err := r.db.Pool.Exec(ctx, `
		INSERT INTO sessions (user_id, refresh_token_hash, ip_address, user_agent, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, userID, tokenHash, ipAddress, userAgent, expiresAt)

	if err != nil {
		return "", err
	}

	return token, nil
}

func hashToken(token string) string {
	// Use SHA-256 for deterministic token hashing (consistent hash for lookups)
	h := sha256.New()
	h.Write([]byte(token))
	return hex.EncodeToString(h.Sum(nil))
}


func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

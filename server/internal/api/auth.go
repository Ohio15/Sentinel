package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sentinel/server/internal/constants"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/middleware"
	"golang.org/x/crypto/bcrypt"
)

type LoginRequest struct {
	Identifier string `json:"identifier" binding:"required"` // Username or email
	Password   string `json:"password" binding:"required,min=6"`
}

type LoginResponse struct {
	AccessToken  string       `json:"accessToken"`
	RefreshToken string       `json:"refreshToken"`
	ExpiresIn    int64        `json:"expiresIn"`
	User         UserResponse `json:"user"`
}

type UserResponse struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
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

	// Get user by username OR email
	var user struct {
		ID           uuid.UUID
		Username     string
		Email        string
		PasswordHash string
		FirstName    string
		LastName     string
		Role         string
		IsActive     bool
	}

	err := r.db.Pool().QueryRow(ctx, `
		SELECT id, username, email, password_hash, first_name, last_name, role, is_active
		FROM users WHERE username = $1 OR email = $1
	`, req.Identifier).Scan(&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.FirstName, &user.LastName, &user.Role, &user.IsActive)

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
	if _, err := r.db.Pool().Exec(ctx, "UPDATE users SET last_login = NOW() WHERE id = $1 AND organization_id = $2", user.ID, constants.CurrentOrganizationID); err != nil {
		log.Printf("Error updating last login for user %s: %v", user.ID, err)
	}

	// Generate new CSRF token on login
	csrfConfig := middleware.DefaultCSRFConfig()
	csrfToken := middleware.SetNewCSRFToken(c, csrfConfig)

	c.JSON(http.StatusOK, gin.H{
		"accessToken":  accessToken,
		"refreshToken": refreshToken,
		"expiresIn":    3600,
		"csrfToken":    csrfToken,
		"user": UserResponse{
			ID:        user.ID,
			Username:  user.Username,
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

	err := r.db.Pool().QueryRow(ctx, `
		SELECT id, user_id, expires_at FROM sessions
		WHERE refresh_token_hash = $1
	`, tokenHash).Scan(&session.ID, &session.UserID, &session.ExpiresAt)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid refresh token"})
		return
	}

	if time.Now().After(session.ExpiresAt) {
		if _, err := r.db.Pool().Exec(ctx, "DELETE FROM sessions WHERE id = $1", session.ID); err != nil {
			log.Printf("Error deleting expired session %s: %v", session.ID, err)
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Refresh token expired"})
		return
	}

	// Get user
	var user struct {
		Email    string
		Role     string
		IsActive bool
	}

	err = r.db.Pool().QueryRow(ctx, `
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
	if _, err := r.db.Pool().Exec(ctx, "DELETE FROM sessions WHERE user_id = $1", userID); err != nil {
		log.Printf("Error deleting sessions for user %s: %v", userID, err)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

func (r *Router) me(c *gin.Context) {
	userID := c.MustGet("userId").(uuid.UUID)
	ctx := context.Background()

	var user UserResponse
	err := r.db.Pool().QueryRow(ctx, `
		SELECT id, username, email, first_name, last_name, role
		FROM users WHERE id = $1 AND organization_id = $2
		`, userID, constants.CurrentOrganizationID).Scan(&user.ID, &user.Username, &user.Email, &user.FirstName, &user.LastName, &user.Role)

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

	_, err := r.db.Pool().Exec(ctx, `
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

// Invitation types
type CreateInvitationRequest struct {
	Email string `json:"email"` // Optional - pre-fill email for invitee
	Role  string `json:"role" binding:"required,oneof=admin operator viewer"`
}

type InvitationResponse struct {
	ID        uuid.UUID  `json:"id"`
	Token     string     `json:"token,omitempty"` // Only returned on creation
	Email     string     `json:"email,omitempty"`
	Role      string     `json:"role"`
	ExpiresAt time.Time  `json:"expiresAt"`
	UsedAt    *time.Time `json:"usedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

type RegisterRequest struct {
	Token     string `json:"token" binding:"required"`
	Username  string `json:"username" binding:"required,min=3,max=50"`
	Email     string `json:"email" binding:"required,email"`
	Password  string `json:"password" binding:"required,min=8"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

type ValidateInvitationResponse struct {
	Valid bool   `json:"valid"`
	Email string `json:"email,omitempty"`
	Role  string `json:"role,omitempty"`
}

// createInvitation creates a new invitation (admin only)
func (r *Router) createInvitation(c *gin.Context) {
	var req CreateInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	userID := c.MustGet("userId").(uuid.UUID)
	ctx := context.Background()

	// Generate secure token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}
	token := hex.EncodeToString(tokenBytes)
	expiresAt := time.Now().Add(48 * time.Hour) // 48 hour expiry

	var inv InvitationResponse
	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO invitations (token, email, role, created_by, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, email, role, expires_at, created_at
	`, token, req.Email, req.Role, userID, expiresAt).Scan(
		&inv.ID, &inv.Email, &inv.Role, &inv.ExpiresAt, &inv.CreatedAt)

	if err != nil {
		log.Printf("Error creating invitation: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create invitation"})
		return
	}

	inv.Token = token // Include token in response for sharing
	c.JSON(http.StatusCreated, inv)
}

// listInvitations lists all invitations (admin only)
func (r *Router) listInvitations(c *gin.Context) {
	ctx := context.Background()

	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, email, role, expires_at, used_at, created_at
		FROM invitations
		ORDER BY created_at DESC
		LIMIT 100
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch invitations"})
		return
	}
	defer rows.Close()

	invitations := []InvitationResponse{}
	for rows.Next() {
		var inv InvitationResponse
		if err := rows.Scan(&inv.ID, &inv.Email, &inv.Role, &inv.ExpiresAt, &inv.UsedAt, &inv.CreatedAt); err != nil {
			continue
		}
		invitations = append(invitations, inv)
	}

	c.JSON(http.StatusOK, invitations)
}

// deleteInvitation deletes an invitation (admin only)
func (r *Router) deleteInvitation(c *gin.Context) {
	id := c.Param("id")
	ctx := context.Background()

	result, err := r.db.Pool().Exec(ctx, "DELETE FROM invitations WHERE id = $1 AND used_at IS NULL", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete invitation"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invitation not found or already used"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Invitation deleted"})
}

// validateInvitation checks if an invitation token is valid (public)
func (r *Router) validateInvitation(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token required"})
		return
	}

	ctx := context.Background()

	var inv struct {
		Email     string
		Role      string
		ExpiresAt time.Time
		UsedAt    *time.Time
	}

	err := r.db.Pool().QueryRow(ctx, `
		SELECT email, role, expires_at, used_at
		FROM invitations WHERE token = $1
	`, token).Scan(&inv.Email, &inv.Role, &inv.ExpiresAt, &inv.UsedAt)

	if err != nil {
		c.JSON(http.StatusOK, ValidateInvitationResponse{Valid: false})
		return
	}

	if inv.UsedAt != nil || time.Now().After(inv.ExpiresAt) {
		c.JSON(http.StatusOK, ValidateInvitationResponse{Valid: false})
		return
	}

	c.JSON(http.StatusOK, ValidateInvitationResponse{
		Valid: true,
		Email: inv.Email,
		Role:  inv.Role,
	})
}

// register creates a new user account using an invitation token (public)
func (r *Router) register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	ctx := context.Background()

	// Validate invitation token
	var inv struct {
		ID        uuid.UUID
		Email     string
		Role      string
		ExpiresAt time.Time
		UsedAt    *time.Time
	}

	err := r.db.Pool().QueryRow(ctx, `
		SELECT id, email, role, expires_at, used_at
		FROM invitations WHERE token = $1
	`, req.Token).Scan(&inv.ID, &inv.Email, &inv.Role, &inv.ExpiresAt, &inv.UsedAt)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid invitation token"})
		return
	}

	if inv.UsedAt != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invitation has already been used"})
		return
	}

	if time.Now().After(inv.ExpiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invitation has expired"})
		return
	}

	// If invitation has a pre-set email, verify it matches
	if inv.Email != "" && inv.Email != req.Email {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email does not match invitation"})
		return
	}

	// Check if username or email already exists
	var exists bool
	err = r.db.Pool().QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM users WHERE (username = $1 OR email = $2) AND organization_id = $3)
		`, req.Username, req.Email, constants.CurrentOrganizationID).Scan(&exists)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check existing user"})
		return
	}

	if exists {
		c.JSON(http.StatusConflict, gin.H{"error": "Username or email already exists"})
		return
	}

	// Hash password
	passwordHash, err := hashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process password"})
		return
	}

	// Create user in transaction
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback(ctx)

	var userID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO users (username, email, password_hash, first_name, last_name, role, organization_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id
	`, req.Username, req.Email, passwordHash, req.FirstName, req.LastName, inv.Role).Scan(&userID)

	if err != nil {
		log.Printf("Error creating user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	// Mark invitation as used
	_, err = tx.Exec(ctx, `
		UPDATE invitations SET used_at = NOW(), used_by = $1 WHERE id = $2
	`, userID, inv.ID)

	if err != nil {
		log.Printf("Error marking invitation as used: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to complete registration"})
		return
	}

	if err := tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to complete registration"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Account created successfully",
		"user": UserResponse{
			ID:        userID,
			Username:  req.Username,
			Email:     req.Email,
			FirstName: req.FirstName,
			LastName:  req.LastName,
			Role:      inv.Role,
		},
	})
}

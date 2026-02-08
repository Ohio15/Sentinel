package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"fmt"
	"image/png"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// MFAStatus represents the MFA status for a user
type MFAStatus struct {
	Enabled        bool       `json:"enabled"`
	VerifiedAt     *time.Time `json:"verifiedAt,omitempty"`
	HasBackupCodes bool       `json:"hasBackupCodes"`
	MFARequired    bool       `json:"mfaRequired"`
}

// MFASetupResponse is returned when setting up MFA
type MFASetupResponse struct {
	Secret    string `json:"secret"`
	QRCode    string `json:"qrCode"` // Base64 encoded PNG
	OTPAuthURL string `json:"otpAuthUrl"`
}

// getMFAStatusHandler returns the MFA status for the current user
func getMFAStatusHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.MustGet("userId").(uuid.UUID)
		ctx := context.Background()

		var status MFAStatus
		var backupCodes []string
		var verifiedAt *time.Time

		err := services.DB.Pool().QueryRow(ctx, `
			SELECT totp_enabled, totp_verified_at, COALESCE(backup_codes, '{}'), mfa_required
			FROM users WHERE id = $1
		`, userID).Scan(&status.Enabled, &verifiedAt, &backupCodes, &status.MFARequired)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get MFA status"})
			return
		}

		status.VerifiedAt = verifiedAt
		status.HasBackupCodes = len(backupCodes) > 0

		c.JSON(http.StatusOK, status)
	}
}

// setupMFAHandler initiates MFA setup
func setupMFAHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.MustGet("userId").(uuid.UUID)
		ctx := context.Background()

		// Get user email for the TOTP key
		var email string
		err := services.DB.Pool().QueryRow(ctx, `
			SELECT email FROM users WHERE id = $1
		`, userID).Scan(&email)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user info"})
			return
		}

		// Generate TOTP key
		key, err := totp.Generate(totp.GenerateOpts{
			Issuer:      "Sentinel RMM",
			AccountName: email,
			Period:      30,
			SecretSize:  20,
			Digits:      otp.DigitsSix,
			Algorithm:   otp.AlgorithmSHA1,
		})
		if err != nil {
			log.Printf("[MFA] Error generating TOTP key: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate MFA secret"})
			return
		}

		// Store the secret (not yet enabled)
		_, err = services.DB.Pool().Exec(ctx, `
			UPDATE users SET totp_secret = $2, totp_enabled = false, totp_verified_at = NULL
			WHERE id = $1
		`, userID, key.Secret())
		if err != nil {
			log.Printf("[MFA] Error storing TOTP secret: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store MFA secret"})
			return
		}

		// Generate QR code image
		var qrCodeBase64 string
		img, err := key.Image(200, 200)
		if err == nil {
			var buf bytes.Buffer
			if err := png.Encode(&buf, img); err == nil {
				qrCodeBase64 = base64.StdEncoding.EncodeToString(buf.Bytes())
			}
		}

		c.JSON(http.StatusOK, MFASetupResponse{
			Secret:     key.Secret(),
			QRCode:     qrCodeBase64,
			OTPAuthURL: key.URL(),
		})
	}
}

// verifyMFASetupHandler completes MFA setup by verifying a code
func verifyMFASetupHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.MustGet("userId").(uuid.UUID)

		var req struct {
			Code string `json:"code" binding:"required,len=6"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid code format"})
			return
		}

		ctx := context.Background()

		// Get the stored secret
		var secret string
		err := services.DB.Pool().QueryRow(ctx, `
			SELECT COALESCE(totp_secret, '') FROM users WHERE id = $1
		`, userID).Scan(&secret)
		if err != nil || secret == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "MFA not initialized. Please start setup first."})
			return
		}

		// Verify the code
		valid := totp.Validate(req.Code, secret)
		if !valid {
			logMFAEvent(services, userID, "failed", c)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid verification code"})
			return
		}

		// Generate backup codes
		backupCodes := generateBackupCodes(8)

		// Enable MFA
		_, err = services.DB.Pool().Exec(ctx, `
			UPDATE users SET
				totp_enabled = true,
				totp_verified_at = NOW(),
				backup_codes = $2
			WHERE id = $1
		`, userID, backupCodes)
		if err != nil {
			log.Printf("[MFA] Error enabling MFA: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enable MFA"})
			return
		}

		logMFAEvent(services, userID, "enabled", c)

		c.JSON(http.StatusOK, gin.H{
			"message":     "MFA enabled successfully",
			"backupCodes": backupCodes,
		})
	}
}

// disableMFAHandler disables MFA for the current user
func disableMFAHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.MustGet("userId").(uuid.UUID)

		var req struct {
			Code     string `json:"code"`     // TOTP code
			Password string `json:"password"` // Optional password verification
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		ctx := context.Background()

		// Verify the TOTP code first
		var secret string
		var enabled bool
		err := services.DB.Pool().QueryRow(ctx, `
			SELECT COALESCE(totp_secret, ''), totp_enabled FROM users WHERE id = $1
		`, userID).Scan(&secret, &enabled)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get MFA status"})
			return
		}

		if !enabled {
			c.JSON(http.StatusBadRequest, gin.H{"error": "MFA is not enabled"})
			return
		}

		// Verify the code
		if req.Code != "" {
			valid := totp.Validate(req.Code, secret)
			if !valid {
				logMFAEvent(services, userID, "failed", c)
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid verification code"})
				return
			}
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Verification code required"})
			return
		}

		// Disable MFA
		_, err = services.DB.Pool().Exec(ctx, `
			UPDATE users SET
				totp_enabled = false,
				totp_secret = NULL,
				totp_verified_at = NULL,
				backup_codes = NULL
			WHERE id = $1
		`, userID)
		if err != nil {
			log.Printf("[MFA] Error disabling MFA: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to disable MFA"})
			return
		}

		logMFAEvent(services, userID, "disabled", c)

		c.JSON(http.StatusOK, gin.H{"message": "MFA disabled successfully"})
	}
}

// verifyMFACodeHandler verifies a TOTP code during login
func verifyMFACodeHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			UserID string `json:"userId" binding:"required"`
			Code   string `json:"code" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		userID, err := uuid.Parse(req.UserID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
			return
		}

		ctx := context.Background()

		// Get user's TOTP secret and backup codes
		var secret string
		var backupCodes []string
		err = services.DB.Pool().QueryRow(ctx, `
			SELECT COALESCE(totp_secret, ''), COALESCE(backup_codes, '{}')
			FROM users WHERE id = $1 AND totp_enabled = true
		`, userID).Scan(&secret, &backupCodes)
		if err != nil || secret == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "MFA not enabled for this user"})
			return
		}

		// Try TOTP code first
		valid := totp.Validate(req.Code, secret)

		// If TOTP fails, try backup codes
		usedBackupCode := false
		if !valid {
			for i, code := range backupCodes {
				if code == req.Code {
					valid = true
					usedBackupCode = true
					// Remove used backup code
					backupCodes = append(backupCodes[:i], backupCodes[i+1:]...)
					services.DB.Pool().Exec(ctx, `
						UPDATE users SET backup_codes = $2 WHERE id = $1
					`, userID, backupCodes)
					logMFAEvent(services, userID, "backup_used", c)
					break
				}
			}
		}

		if !valid {
			logMFAEvent(services, userID, "failed", c)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid verification code"})
			return
		}

		logMFAEvent(services, userID, "verified", c)

		c.JSON(http.StatusOK, gin.H{
			"verified":       true,
			"usedBackupCode": usedBackupCode,
		})
	}
}

// regenerateBackupCodesHandler generates new backup codes
func regenerateBackupCodesHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.MustGet("userId").(uuid.UUID)

		var req struct {
			Code string `json:"code" binding:"required,len=6"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid code format"})
			return
		}

		ctx := context.Background()

		// Verify current TOTP code
		var secret string
		err := services.DB.Pool().QueryRow(ctx, `
			SELECT COALESCE(totp_secret, '') FROM users WHERE id = $1 AND totp_enabled = true
		`, userID).Scan(&secret)
		if err != nil || secret == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "MFA not enabled"})
			return
		}

		valid := totp.Validate(req.Code, secret)
		if !valid {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid verification code"})
			return
		}

		// Generate new backup codes
		backupCodes := generateBackupCodes(8)

		_, err = services.DB.Pool().Exec(ctx, `
			UPDATE users SET backup_codes = $2 WHERE id = $1
		`, userID, backupCodes)
		if err != nil {
			log.Printf("[MFA] Error regenerating backup codes: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to regenerate backup codes"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":     "Backup codes regenerated",
			"backupCodes": backupCodes,
		})
	}
}

// Helper functions

func generateBackupCodes(count int) []string {
	codes := make([]string, count)
	for i := 0; i < count; i++ {
		// Generate 8 random bytes
		b := make([]byte, 5)
		rand.Read(b)
		// Convert to base32 and format
		code := base32.StdEncoding.EncodeToString(b)[:8]
		codes[i] = fmt.Sprintf("%s-%s", code[:4], code[4:])
	}
	return codes
}

func logMFAEvent(services *Services, userID uuid.UUID, eventType string, c *gin.Context) {
	ctx := context.Background()
	services.DB.Pool().Exec(ctx, `
		INSERT INTO mfa_events (user_id, event_type, ip_address, user_agent)
		VALUES ($1, $2, $3, $4)
	`, userID, eventType, c.ClientIP(), c.GetHeader("User-Agent"))
}

// CheckMFARequired checks if user has MFA enabled and needs to verify
func CheckMFARequired(services *Services, userID uuid.UUID) (bool, error) {
	ctx := context.Background()
	var enabled bool
	err := services.DB.Pool().QueryRow(ctx, `
		SELECT totp_enabled FROM users WHERE id = $1
	`, userID).Scan(&enabled)
	return enabled, err
}

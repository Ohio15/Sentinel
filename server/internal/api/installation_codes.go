package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sentinel/server/internal/constants"
	"golang.org/x/crypto/bcrypt"
)

// Characters used for installation codes (excluding ambiguous: 0,O,1,I,L)
const installCodeChars = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// CreateInstallCodeRequest is the request body for creating an installation code
type CreateInstallCodeRequest struct {
	DeviceName      string  `json:"deviceName" binding:"required"`
	UserName        *string `json:"userName"`
	Notes           *string `json:"notes"`
	ExpirationDays  int     `json:"expirationDays"`
}

// CreateInstallCodeResponse is the response after creating an installation code
type CreateInstallCodeResponse struct {
	Success          bool      `json:"success"`
	Code             string    `json:"code"`
	DeviceName       string    `json:"deviceName"`
	DownloadURL      string    `json:"downloadUrl"`
	ExpiresAt        time.Time `json:"expiresAt"`
	Instructions     string    `json:"instructions"`
}

// ValidateCodeResponse is the response for code validation (used by installer)
//
// Token-leakage hardening (2026-05-21):
//   - BootstrapURL is the new preferred field — a single-use, HMAC-signed URL
//     (5-min TTL) that the installer GETs to retrieve the enrollment token.
//     This keeps the token out of any intermediate proxy log / DevTools capture
//     of the validate-code response itself.
//   - EnrollmentToken is kept for backward compatibility with installer
//     templates already in the field. Set INSTALL_TOKEN_OMIT_LEGACY=1 to drop
//     it from responses once the fleet's installer templates have been rotated.
type ValidateCodeResponse struct {
	Valid           bool   `json:"valid"`
	ServerURL       string `json:"serverUrl,omitempty"`       // Bootstrap API URL
	AgentURL        string `json:"agentUrl,omitempty"`        // Agent connection URL (mTLS)
	BootstrapURL    string `json:"bootstrapUrl,omitempty"`    // Signed redeem URL (preferred)
	EnrollmentToken string `json:"enrollmentToken,omitempty"` // Legacy plaintext token (deprecated)
	DeviceName      string `json:"deviceName,omitempty"`
	Error           string `json:"error,omitempty"`
}

// InstallCodeListItem represents an installation code in a list
type InstallCodeListItem struct {
	ID                uuid.UUID  `json:"id"`
	Code              string     `json:"code"`
	DeviceName        string     `json:"deviceName"`
	Status            string     `json:"status"`
	CreatedAt         time.Time  `json:"createdAt"`
	ExpiresAt         time.Time  `json:"expiresAt"`
	UsedAt             *time.Time `json:"usedAt,omitempty"`
	CreatedByName     *string    `json:"createdByName,omitempty"`
}

// generateInstallationCode generates a random 8-character code in XXXX-XXXX format
func generateInstallationCode() string {
	code := make([]byte, 8)
	for i := range code {
		randByte := make([]byte, 1)
		rand.Read(randByte)
		code[i] = installCodeChars[int(randByte[0])%len(installCodeChars)]
	}
	return string(code[0:4]) + "-" + string(code[4:8])
}

// createInstallationCodeHandler creates a new installation code (admin only)
func createInstallationCodeHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateInstallCodeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			sanitizedError(c, http.StatusBadRequest, "Invalid request body", err)
			return
		}

		// Validate device name
		req.DeviceName = strings.TrimSpace(req.DeviceName)
		if len(req.DeviceName) < 2 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Device name must be at least 2 characters"})
			return
		}

		// Get user ID from context — need both UUID (for enrollment_tokens) and int (for installation_links)
		var createdByInt *int
		var createdByUUID *uuid.UUID
		if userID, exists := c.Get("userID"); exists {
			if uid, ok := userID.(uuid.UUID); ok {
				createdByUUID = &uid
				var userIntID int
				err := services.DB.Pool().QueryRow(c.Request.Context(),
					`SELECT id FROM users WHERE id = $1::text::int OR id::text = $1::text LIMIT 1`, uid.String()).Scan(&userIntID)
				if err == nil {
					createdByInt = &userIntID
				}
			}
		}

		// Set expiration (default 7 days)
		expirationDays := req.ExpirationDays
		if expirationDays <= 0 {
			expirationDays = 7
		}
		if expirationDays > 30 {
			expirationDays = 30
		}
		expiresAt := time.Now().Add(time.Duration(expirationDays) * 24 * time.Hour)

		// Generate unique installation code
		var installCode string
		for attempts := 0; attempts < 10; attempts++ {
			installCode = generateInstallationCode()
			var exists bool
			err := services.DB.Pool().QueryRow(c.Request.Context(),
				`SELECT EXISTS(SELECT 1 FROM agent_installation_links WHERE installation_code = $1)`,
				installCode).Scan(&exists)
			if err == nil && !exists {
				break
			}
		}

		// Generate download token (for backward compatibility and direct link access)
		tokenBytes := make([]byte, 30)
		if _, err := rand.Read(tokenBytes); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
			return
		}
		downloadToken := "DL-" + hex.EncodeToString(tokenBytes)

		// Generate enrollment token
		enrollmentBytes := make([]byte, 32)
		if _, err := rand.Read(enrollmentBytes); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate enrollment token"})
			return
		}
		enrollmentToken := hex.EncodeToString(enrollmentBytes)

		// Hash the enrollment token
		tokenHash, err := bcrypt.GenerateFromPassword([]byte(enrollmentToken), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash token"})
			return
		}

		// Create enrollment token in database
		var enrollmentTokenID uuid.UUID
		err = services.DB.Pool().QueryRow(c.Request.Context(), `
			INSERT INTO enrollment_tokens (
				token, token_hash, name, description, created_by, expires_at, max_uses, is_active, is_legacy
			) VALUES ($1, $2, $3, $4, $5, $6, 1, TRUE, FALSE)
			RETURNING id
		`, enrollmentToken, string(tokenHash),
			fmt.Sprintf("Install Code: %s (%s)", installCode, req.DeviceName),
			fmt.Sprintf("Auto-generated for installation code %s", installCode),
			createdByUUID, expiresAt).Scan(&enrollmentTokenID)
		if err != nil {
			log.Printf("Failed to create enrollment token: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create enrollment token"})
			return
		}

		// Create the installation link with code
		var linkID uuid.UUID
		err = services.DB.Pool().QueryRow(c.Request.Context(), `
			INSERT INTO agent_installation_links (
				download_token, installation_code, device_name, user_name,
				enrollment_token_id, created_by, expires_at, notes, status, organization_id
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'pending', $9)
			RETURNING id
		`, downloadToken, installCode, req.DeviceName, req.UserName,
			enrollmentTokenID, createdByInt, expiresAt, req.Notes, constants.CurrentOrganizationID).Scan(&linkID)
		if err != nil {
			log.Printf("Failed to create installation link: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create installation code"})
			return
		}

		// Build download URL
		publicURL := services.Config.PublicURL
		if publicURL == "" {
			publicURL = services.Config.ServerURL
		}
		if publicURL == "" {
			publicURL = fmt.Sprintf("%s://%s", detectRequestScheme(c), c.Request.Host)
		}
		downloadURL := fmt.Sprintf("%s/api/download/agent", publicURL)

		instructions := fmt.Sprintf(`1. Download the installer from: %s
2. Run the installer
3. When prompted, enter this code: %s

Code expires: %s`, downloadURL, installCode, expiresAt.Format("January 2, 2006"))

		c.JSON(http.StatusCreated, CreateInstallCodeResponse{
			Success:      true,
			Code:         installCode,
			DeviceName:   req.DeviceName,
			DownloadURL:  downloadURL,
			ExpiresAt:    expiresAt,
			Instructions: instructions,
		})
	}
}

// validateInstallationCodeHandler validates a code and returns config (public endpoint for installer)
func validateInstallationCodeHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		code := c.Query("code")
		if code == "" {
			c.JSON(http.StatusBadRequest, ValidateCodeResponse{
				Valid: false,
				Error: "invalid",
			})
			return
		}

		// Normalize code: remove spaces, uppercase, ensure dash in middle
		code = strings.ToUpper(strings.ReplaceAll(code, " ", ""))
		code = strings.ReplaceAll(code, "-", "")
		if len(code) == 8 {
			code = code[0:4] + "-" + code[4:8]
		}

		// Log validation attempt
		logCodeValidation(services, code, c.ClientIP(), c.Request.UserAgent(), false, nil)

		// Query the link by installation code
		var link struct {
			ID                uuid.UUID
			DeviceName        string
			Status            string
			ExpiresAt         time.Time
			EnrollmentTokenID uuid.UUID
			AgentConnectedAt  *time.Time
		}
		var enrollmentToken string

		err := services.DB.Pool().QueryRow(c.Request.Context(), `
			SELECT l.id, l.device_name, l.status, l.expires_at, l.enrollment_token_id, l.agent_connected_at,
			       e.token
			FROM agent_installation_links l
			JOIN enrollment_tokens e ON l.enrollment_token_id = e.id
			WHERE l.installation_code = $1 AND l.deleted_at IS NULL
		`, code).Scan(
			&link.ID, &link.DeviceName, &link.Status, &link.ExpiresAt, &link.EnrollmentTokenID, &link.AgentConnectedAt,
			&enrollmentToken,
		)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Generic error to prevent code enumeration
				c.JSON(http.StatusOK, ValidateCodeResponse{
					Valid: false,
					Error: "invalid",
				})
				return
			}
			log.Printf("Failed to query code: %v", err)
			c.JSON(http.StatusInternalServerError, ValidateCodeResponse{
				Valid: false,
				Error: "error",
			})
			return
		}

		// Check if already used
		if link.AgentConnectedAt != nil || link.Status == "installed" {
			logCodeValidation(services, code, c.ClientIP(), c.Request.UserAgent(), false, strPtr("Code already used"))
			c.JSON(http.StatusOK, ValidateCodeResponse{
				Valid: false,
				Error: "already_used",
			})
			return
		}

		// Check if expired
		if time.Now().After(link.ExpiresAt) {
			logCodeValidation(services, code, c.ClientIP(), c.Request.UserAgent(), false, strPtr("Code expired"))
			c.JSON(http.StatusOK, ValidateCodeResponse{
				Valid: false,
				Error: "expired",
			})
			return
		}

		// Check if revoked
		if link.Status == "revoked" {
			logCodeValidation(services, code, c.ClientIP(), c.Request.UserAgent(), false, strPtr("Code revoked"))
			c.JSON(http.StatusOK, ValidateCodeResponse{
				Valid: false,
				Error: "invalid",
			})
			return
		}

		// Mark code as used
		_, err = services.DB.Pool().Exec(c.Request.Context(), `
			UPDATE agent_installation_links
			SET downloaded_at = NOW(),
			    download_ip = $1,
			    download_user_agent = $2,
			    download_count = download_count + 1,
			    status = CASE WHEN status = 'pending' THEN 'downloaded' ELSE status END
			WHERE id = $3
		`, c.ClientIP(), c.Request.UserAgent(), link.ID)
		if err != nil {
			log.Printf("Failed to mark code as used: %v", err)
		}

		// I-11: Tighten enrollment token expiry to 15 minutes from validation.
		// The token was created with the install code's long expiry (up to 30 days),
		// but once a code is validated the agent should enroll within minutes.
		// This limits the window for token interception/reuse.
		shortExpiry := time.Now().Add(15 * time.Minute)
		_, err = services.DB.Pool().Exec(c.Request.Context(), `
			UPDATE enrollment_tokens
			SET expires_at = LEAST(expires_at, $1)
			WHERE id = $2
		`, shortExpiry, link.EnrollmentTokenID)
		if err != nil {
			log.Printf("Warning: failed to tighten enrollment token expiry: %v", err)
		}

		// Log successful validation
		logCodeValidation(services, code, c.ClientIP(), c.Request.UserAgent(), true, nil)

		// Get bootstrap URL (PUBLIC_URL for installer API calls)
		bootstrapURL := services.Config.PublicURL
		if bootstrapURL == "" {
			bootstrapURL = services.Config.ServerURL
		}
		if bootstrapURL == "" {
			bootstrapURL = fmt.Sprintf("%s://%s", detectRequestScheme(c), c.Request.Host)
		}

		// Build signed bootstrap URL (preferred path for new installers)
		signedBootstrapURL := signBootstrapRedeemURL(bootstrapURL, link.ID)

		resp := ValidateCodeResponse{
			Valid:        true,
			ServerURL:    bootstrapURL,
			AgentURL:     services.Config.ServerURL,
			BootstrapURL: signedBootstrapURL,
			DeviceName:   link.DeviceName,
		}
		// Omit the plaintext token when explicitly disabled. Flip the env var
		// only after the fleet's installer templates have been rotated to the
		// new BootstrapURL-redeem flow.
		if strings.ToLower(strings.TrimSpace(os.Getenv("INSTALL_TOKEN_OMIT_LEGACY"))) != "1" {
			resp.EnrollmentToken = enrollmentToken
		}
		c.JSON(http.StatusOK, resp)
	}
}

// redeemInstallCodeHandler exchanges a signed bootstrap URL for the enrollment
// token. The signed URL is single-use (enforced by the link.status transition
// to "redeemed") and time-boxed by the signature exp claim.
//
// Public endpoint — auth is the signature itself. Rate limit is applied at the
// router level alongside validate-code.
func redeemInstallCodeHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		linkIDStr := c.Query("link")
		if linkIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing link parameter"})
			return
		}
		linkID, err := uuid.Parse(linkIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid link parameter"})
			return
		}

		if err := verifyBootstrapRedeemSig(c.Request.URL.Query(), linkID); err != nil {
			log.Printf("[InstallRedeem] Rejected: %v (ip=%s ua=%q)", err, c.ClientIP(), c.Request.UserAgent())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired redeem URL"})
			return
		}

		ctx := c.Request.Context()
		var enrollmentTokenID uuid.UUID
		var enrollmentToken string
		var status string
		err = services.DB.Pool().QueryRow(ctx, `
			SELECT l.enrollment_token_id, e.token, l.status
			FROM agent_installation_links l
			JOIN enrollment_tokens e ON l.enrollment_token_id = e.id
			WHERE l.id = $1 AND l.deleted_at IS NULL
		`, linkID).Scan(&enrollmentTokenID, &enrollmentToken, &status)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "link not found"})
			return
		}
		if status == "redeemed" || status == "installed" {
			c.JSON(http.StatusGone, gin.H{"error": "bootstrap URL already redeemed"})
			return
		}

		// Atomically mark the link redeemed so a second call to this URL fails.
		tag, err := services.DB.Pool().Exec(ctx, `
			UPDATE agent_installation_links
			SET status = 'redeemed', redeemed_at = NOW(), redeemed_ip = $1
			WHERE id = $2 AND status NOT IN ('redeemed','installed')
		`, c.ClientIP(), linkID)
		if err != nil || tag.RowsAffected() == 0 {
			c.JSON(http.StatusConflict, gin.H{"error": "bootstrap URL race lost"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"enrollmentToken": enrollmentToken,
		})
	}
}

// listInstallationCodesHandler returns all installation codes (admin)
func listInstallationCodesHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := services.DB.Pool().Query(c.Request.Context(), `
			SELECT l.id, l.installation_code, l.device_name, l.status,
			       l.created_at, l.expires_at, l.downloaded_at,
			       COALESCE(CONCAT(u.first_name, ' ', u.last_name), '') as created_by_name
			FROM agent_installation_links l
			LEFT JOIN users u ON l.created_by = u.id
			WHERE l.installation_code IS NOT NULL
			  AND l.deleted_at IS NULL
			  AND l.organization_id = $1
			ORDER BY l.created_at DESC
			LIMIT 100
		`, constants.CurrentOrganizationID)
		if err != nil {
			log.Printf("Failed to list installation codes: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list codes"})
			return
		}
		defer rows.Close()

		var codes []InstallCodeListItem
		for rows.Next() {
			var item InstallCodeListItem
			var createdByName string
			if err := rows.Scan(
				&item.ID, &item.Code, &item.DeviceName, &item.Status,
				&item.CreatedAt, &item.ExpiresAt, &item.UsedAt,
				&createdByName,
			); err != nil {
				continue
			}
			if createdByName != "" {
				item.CreatedByName = &createdByName
			}

			// Update status display based on expiration
			if item.Status == "pending" && time.Now().After(item.ExpiresAt) {
				item.Status = "expired"
			}

			codes = append(codes, item)
		}

		if codes == nil {
			codes = []InstallCodeListItem{}
		}

		c.JSON(http.StatusOK, gin.H{"codes": codes})
	}
}

// logCodeValidation logs a code validation attempt
func logCodeValidation(services *Services, code, ipAddress, userAgent string, success bool, errorMessage *string) {
	// Get link ID
	var linkID uuid.UUID
	err := services.DB.Pool().QueryRow(context.Background(), `
		SELECT id FROM agent_installation_links WHERE installation_code = $1
	`, code).Scan(&linkID)
	if err != nil {
		return
	}

	_, _ = services.DB.Pool().Exec(context.Background(), `
		INSERT INTO agent_link_access_log (link_id, ip_address, user_agent, action, success, error_message)
		VALUES ($1, $2, $3, 'validate_code', $4, $5)
	`, linkID, ipAddress, userAgent, success, errorMessage)
}

// serveGenericInstallerHandler serves the generic installer (no embedded config)
func serveGenericInstallerHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Find generic installer: dedicated generic binaries first, fall back to
		// the patcher template (still a valid Windows installer when not patched).
		installerPath := findArtifact(
			"sentinel-installer.exe",
			"sentinel-installer-generic.exe",
			"sentinel-installer-template.exe",
		)
		if installerPath == "" {
			log.Printf("Generic installer not found")
			c.JSON(http.StatusNotFound, gin.H{"error": "Installer not available"})
			return
		}
		installerData, err := os.ReadFile(installerPath)
		if err != nil {
			log.Printf("Failed to read generic installer %s: %v", installerPath, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Installer read failed"})
			return
		}
		log.Printf("Serving generic installer from: %s", installerPath)

		// Serve the installer
		c.Header("Content-Disposition", "attachment; filename=\"SentinelAgent-Installer.exe\"")
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Content-Length", fmt.Sprintf("%d", len(installerData)))
		c.Data(http.StatusOK, "application/octet-stream", installerData)
	}
}

// serveTestInstallerHandler serves a minimal test binary for debugging
func serveTestInstallerHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		testPaths := []string{
			"installers/test-minimal.exe",
			"release/agent/test-minimal.exe",
		}

		var installerData []byte
		var err error
		for _, path := range testPaths {
			installerData, err = os.ReadFile(path)
			if err == nil {
				log.Printf("Serving test installer from: %s", path)
				break
			}
		}
		if err != nil {
			log.Printf("Test installer not found: %v", err)
			c.JSON(http.StatusNotFound, gin.H{"error": "Test installer not available"})
			return
		}

		c.Header("Content-Disposition", "attachment; filename=\"test-minimal.exe\"")
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Content-Length", fmt.Sprintf("%d", len(installerData)))
		c.Data(http.StatusOK, "application/octet-stream", installerData)
	}
}

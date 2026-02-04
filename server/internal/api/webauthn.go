package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/constants"
	"github.com/sentinel/server/internal/middleware"
	"github.com/sentinel/server/pkg/config"
	"github.com/sentinel/server/pkg/database"
)

// WebAuthnUser implements the webauthn.User interface
type WebAuthnUser struct {
	ID          uuid.UUID
	Username    string
	Email       string
	DisplayName string
	Credentials []webauthn.Credential
}

func (u *WebAuthnUser) WebAuthnID() []byte {
	return u.ID[:]
}

func (u *WebAuthnUser) WebAuthnName() string {
	return u.Username
}

func (u *WebAuthnUser) WebAuthnDisplayName() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

func (u *WebAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return u.Credentials
}

func (u *WebAuthnUser) WebAuthnIcon() string {
	return ""
}

// WebAuthnService handles WebAuthn operations
type WebAuthnService struct {
	webauthn *webauthn.WebAuthn
	db       *database.Database
	config   *config.Config
}

// NewWebAuthnService creates a new WebAuthn service
func NewWebAuthnService(cfg *config.Config, db *database.Database) (*WebAuthnService, error) {
	// Parse user verification preference
	var userVerification protocol.UserVerificationRequirement
	switch cfg.WebAuthnUserVerification {
	case "required":
		userVerification = protocol.VerificationRequired
	case "discouraged":
		userVerification = protocol.VerificationDiscouraged
	default:
		userVerification = protocol.VerificationPreferred
	}

	residentKeyPreferred := protocol.ResidentKeyRequirementPreferred
	wconfig := &webauthn.Config{
		RPDisplayName: cfg.WebAuthnRPName,
		RPID:          cfg.WebAuthnRPID,
		RPOrigins:     cfg.WebAuthnRPOrigins,
		Timeouts: webauthn.TimeoutsConfig{
			Login: webauthn.TimeoutConfig{
				Enforce:    true,
				Timeout:    time.Duration(cfg.WebAuthnTimeout) * time.Millisecond,
				TimeoutUVD: time.Duration(cfg.WebAuthnTimeout) * time.Millisecond,
			},
			Registration: webauthn.TimeoutConfig{
				Enforce:    true,
				Timeout:    time.Duration(cfg.WebAuthnTimeout) * time.Millisecond,
				TimeoutUVD: time.Duration(cfg.WebAuthnTimeout) * time.Millisecond,
			},
		},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      residentKeyPreferred,
			UserVerification: userVerification,
		},
	}

	w, err := webauthn.New(wconfig)
	if err != nil {
		return nil, err
	}

	return &WebAuthnService{
		webauthn: w,
		db:       db,
		config:   cfg,
	}, nil
}

// Passkey response types
type PasskeyResponse struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

type BeginAuthenticationResponse struct {
	SessionID string                               `json:"sessionId"`
	Options   *protocol.CredentialAssertion        `json:"options"`
}

type FinishAuthenticationRequest struct {
	SessionID string          `json:"sessionId"`
	Response  json.RawMessage `json:"response"`
}

type BeginRegistrationResponse struct {
	SessionID string                            `json:"sessionId"`
	Options   *protocol.CredentialCreation      `json:"options"`
}

type FinishRegistrationRequest struct {
	SessionID string          `json:"sessionId"`
	Response  json.RawMessage `json:"response"`
	Name      string          `json:"name"`
}

type RenamePasskeyRequest struct {
	Name string `json:"name" binding:"required,min=1,max=255"`
}

// generateSessionID creates a random session ID for WebAuthn ceremonies
func generateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// ---- Authentication (Login with Passkey) ----

// BeginAuthentication starts the passkey login process
func beginPasskeyAuthenticationHandler(services *Services, waService *WebAuthnService) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// Get all users with passkeys for discoverable credential login
		users, err := getUsersWithPasskeys(ctx, services.DB)
		if err != nil {
			log.Printf("[WEBAUTHN] Error getting users with passkeys: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start authentication"})
			return
		}

		if len(users) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No passkeys registered"})
			return
		}

		// Collect all credentials for all users
		var allowedCredentials []protocol.CredentialDescriptor
		for _, user := range users {
			for _, cred := range user.Credentials {
				allowedCredentials = append(allowedCredentials, protocol.CredentialDescriptor{
					Type:         protocol.PublicKeyCredentialType,
					CredentialID: cred.ID,
					Transport:    cred.Transport,
				})
			}
		}

		// Begin authentication - using discoverable credentials
		options, sessionData, err := waService.webauthn.BeginDiscoverableLogin(
			webauthn.WithAllowedCredentials(allowedCredentials),
		)
		if err != nil {
			log.Printf("[WEBAUTHN] BeginDiscoverableLogin error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start authentication"})
			return
		}

		// Store session in database
		sessionID, err := generateSessionID()
		if err != nil {
			log.Printf("[WEBAUTHN] Failed to generate session ID: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start authentication"})
			return
		}

		sessionBytes, err := json.Marshal(sessionData)
		if err != nil {
			log.Printf("[WEBAUTHN] Failed to marshal session data: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start authentication"})
			return
		}

		expiresAt := time.Now().Add(5 * time.Minute)
		_, err = services.DB.Pool().Exec(ctx, `
			INSERT INTO webauthn_sessions (session_id, challenge, session_type, user_verification, expires_at, organization_id)
			VALUES ($1, $2, 'authentication', $3, $4, $5)
		`, sessionID, sessionBytes, waService.config.WebAuthnUserVerification, expiresAt, constants.CurrentOrganizationID)

		if err != nil {
			log.Printf("[WEBAUTHN] Failed to store session: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start authentication"})
			return
		}

		c.JSON(http.StatusOK, BeginAuthenticationResponse{
			SessionID: sessionID,
			Options:   options,
		})
	}
}

// FinishAuthentication completes the passkey login process
func finishPasskeyAuthenticationHandler(services *Services, waService *WebAuthnService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req FinishAuthenticationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		ctx := c.Request.Context()

		// Retrieve and validate session
		var sessionBytes []byte
		var expiresAt time.Time
		err := services.DB.Pool().QueryRow(ctx, `
			SELECT challenge, expires_at FROM webauthn_sessions
			WHERE session_id = $1 AND session_type = 'authentication'
		`, req.SessionID).Scan(&sessionBytes, &expiresAt)

		if err != nil {
			log.Printf("[WEBAUTHN] Session not found: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired session"})
			return
		}

		if time.Now().After(expiresAt) {
			// Clean up expired session
			services.DB.Pool().Exec(ctx, "DELETE FROM webauthn_sessions WHERE session_id = $1", req.SessionID)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Session expired"})
			return
		}

		var sessionData webauthn.SessionData
		if err := json.Unmarshal(sessionBytes, &sessionData); err != nil {
			log.Printf("[WEBAUTHN] Failed to unmarshal session: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid session data"})
			return
		}

		// Parse the credential assertion response
		parsedResponse, err := protocol.ParseCredentialRequestResponse(c.Request)
		if err != nil {
			// Try parsing from raw bytes if request body already consumed
			log.Printf("[WEBAUTHN] Failed to parse response from request: %v, trying raw bytes", err)
			parsedResponse, err = parseCredentialAssertionFromBytes(req.Response)
			if err != nil {
				log.Printf("[WEBAUTHN] Failed to parse response: %v", err)
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid credential response"})
				return
			}
		}

		// Find the user by credential ID
		credentialID := parsedResponse.RawID
		user, err := getUserByCredentialID(ctx, services.DB, credentialID)
		if err != nil {
			log.Printf("[WEBAUTHN] User not found for credential: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credential"})
			return
		}

		// Validate the assertion
		credential, err := waService.webauthn.ValidateDiscoverableLogin(
			func(rawID, userHandle []byte) (webauthn.User, error) {
				return user, nil
			},
			sessionData,
			parsedResponse,
		)
		if err != nil {
			log.Printf("[WEBAUTHN] Validation failed: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}

		// Update sign count and last used
		_, err = services.DB.Pool().Exec(ctx, `
			UPDATE webauthn_credentials
			SET sign_count = $1, last_used_at = NOW()
			WHERE credential_id = $2
		`, credential.Authenticator.SignCount, credentialID)
		if err != nil {
			log.Printf("[WEBAUTHN] Failed to update sign count: %v", err)
		}

		// Delete the session
		services.DB.Pool().Exec(ctx, "DELETE FROM webauthn_sessions WHERE session_id = $1", req.SessionID)

		// Get user details for token generation
		var userDetails struct {
			Email    string
			Role     string
			IsActive bool
		}
		err = services.DB.Pool().QueryRow(ctx, `
			SELECT email, role, is_active FROM users WHERE id = $1 AND organization_id = $2
		`, user.ID, constants.CurrentOrganizationID).Scan(&userDetails.Email, &userDetails.Role, &userDetails.IsActive)
		if err != nil {
			log.Printf("[WEBAUTHN] Failed to get user details: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to complete authentication"})
			return
		}

		if !userDetails.IsActive {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Account is disabled"})
			return
		}

		// Generate JWT token
		accessToken, err := generateAccessTokenForUser(user.ID, userDetails.Email, userDetails.Role, services.Config.JWTSecret)
		if err != nil {
			log.Printf("[WEBAUTHN] Failed to generate token: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
			return
		}

		// Generate refresh token
		refreshToken, err := generateRefreshTokenForUser(ctx, services.DB, user.ID, c.ClientIP(), c.GetHeader("User-Agent"))
		if err != nil {
			log.Printf("[WEBAUTHN] Failed to generate refresh token: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate refresh token"})
			return
		}

		// Update last login
		services.DB.Pool().Exec(ctx, "UPDATE users SET last_login = NOW() WHERE id = $1", user.ID)

		// Generate new CSRF token
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
				Email:     userDetails.Email,
				FirstName: user.DisplayName,
				LastName:  "",
				Role:      userDetails.Role,
			},
		})
	}
}

// ---- Registration (Add New Passkey) ----

// BeginRegistration starts the passkey registration process (requires auth)
func beginPasskeyRegistrationHandler(services *Services, waService *WebAuthnService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.MustGet("userId").(uuid.UUID)
		ctx := c.Request.Context()

		// Get user
		user, err := getUserWithPasskeys(ctx, services.DB, userID)
		if err != nil {
			log.Printf("[WEBAUTHN] Failed to get user: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start registration"})
			return
		}

		// Build exclusion list
		var excludeCredentials []protocol.CredentialDescriptor
		for _, cred := range user.Credentials {
			excludeCredentials = append(excludeCredentials, protocol.CredentialDescriptor{
				Type:         protocol.PublicKeyCredentialType,
				CredentialID: cred.ID,
				Transport:    cred.Transport,
			})
		}

		// Begin registration
		options, sessionData, err := waService.webauthn.BeginRegistration(user,
			webauthn.WithExclusions(excludeCredentials),
		)
		if err != nil {
			log.Printf("[WEBAUTHN] BeginRegistration error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start registration"})
			return
		}

		// Store session
		sessionID, err := generateSessionID()
		if err != nil {
			log.Printf("[WEBAUTHN] Failed to generate session ID: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start registration"})
			return
		}

		sessionBytes, err := json.Marshal(sessionData)
		if err != nil {
			log.Printf("[WEBAUTHN] Failed to marshal session: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start registration"})
			return
		}

		expiresAt := time.Now().Add(5 * time.Minute)
		_, err = services.DB.Pool().Exec(ctx, `
			INSERT INTO webauthn_sessions (session_id, user_id, challenge, session_type, user_verification, expires_at, organization_id)
			VALUES ($1, $2, $3, 'registration', $4, $5, $6)
		`, sessionID, userID, sessionBytes, waService.config.WebAuthnUserVerification, expiresAt, constants.CurrentOrganizationID)

		if err != nil {
			log.Printf("[WEBAUTHN] Failed to store session: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start registration"})
			return
		}

		c.JSON(http.StatusOK, BeginRegistrationResponse{
			SessionID: sessionID,
			Options:   options,
		})
	}
}

// FinishRegistration completes the passkey registration process
func finishPasskeyRegistrationHandler(services *Services, waService *WebAuthnService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.MustGet("userId").(uuid.UUID)

		var req FinishRegistrationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		ctx := c.Request.Context()

		// Retrieve session
		var sessionBytes []byte
		var expiresAt time.Time
		var sessionUserID uuid.UUID
		err := services.DB.Pool().QueryRow(ctx, `
			SELECT user_id, challenge, expires_at FROM webauthn_sessions
			WHERE session_id = $1 AND session_type = 'registration'
		`, req.SessionID).Scan(&sessionUserID, &sessionBytes, &expiresAt)

		if err != nil {
			log.Printf("[WEBAUTHN] Session not found: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired session"})
			return
		}

		// Verify session belongs to current user
		if sessionUserID != userID {
			c.JSON(http.StatusForbidden, gin.H{"error": "Session does not belong to user"})
			return
		}

		if time.Now().After(expiresAt) {
			services.DB.Pool().Exec(ctx, "DELETE FROM webauthn_sessions WHERE session_id = $1", req.SessionID)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Session expired"})
			return
		}

		var sessionData webauthn.SessionData
		if err := json.Unmarshal(sessionBytes, &sessionData); err != nil {
			log.Printf("[WEBAUTHN] Failed to unmarshal session: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid session data"})
			return
		}

		// Get user for validation
		user, err := getUserWithPasskeys(ctx, services.DB, userID)
		if err != nil {
			log.Printf("[WEBAUTHN] Failed to get user: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to complete registration"})
			return
		}

		// Parse the credential creation response
		parsedResponse, err := parseCredentialCreationFromBytes(req.Response)
		if err != nil {
			log.Printf("[WEBAUTHN] Failed to parse response: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid credential response"})
			return
		}

		// Validate and create the credential
		credential, err := waService.webauthn.CreateCredential(user, sessionData, parsedResponse)
		if err != nil {
			log.Printf("[WEBAUTHN] CreateCredential error: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to validate credential"})
			return
		}

		// Determine passkey name
		passkeyName := req.Name
		if passkeyName == "" {
			passkeyName = "Passkey " + time.Now().Format("Jan 2, 2006")
		}

		// Convert transports to string array
		var transports []string
		for _, t := range credential.Transport {
			transports = append(transports, string(t))
		}

		// Store credential in database
		var credID uuid.UUID
		err = services.DB.Pool().QueryRow(ctx, `
			INSERT INTO webauthn_credentials (
				user_id, credential_id, public_key, name, aaguid, sign_count,
				backup_eligible, backup_state, transports, organization_id
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			RETURNING id
		`, userID, credential.ID, credential.PublicKey, passkeyName,
			credential.Authenticator.AAGUID, credential.Authenticator.SignCount,
			credential.Flags.BackupEligible, credential.Flags.BackupState,
			transports, constants.CurrentOrganizationID).Scan(&credID)

		if err != nil {
			log.Printf("[WEBAUTHN] Failed to store credential: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store credential"})
			return
		}

		// Delete session
		services.DB.Pool().Exec(ctx, "DELETE FROM webauthn_sessions WHERE session_id = $1", req.SessionID)

		c.JSON(http.StatusCreated, gin.H{
			"message": "Passkey registered successfully",
			"passkey": PasskeyResponse{
				ID:        credID,
				Name:      passkeyName,
				CreatedAt: time.Now(),
			},
		})
	}
}

// ---- Passkey Management ----

// ListPasskeys returns all passkeys for the current user
func listPasskeysHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.MustGet("userId").(uuid.UUID)
		ctx := c.Request.Context()

		rows, err := services.DB.Pool().Query(ctx, `
			SELECT id, name, created_at, last_used_at
			FROM webauthn_credentials
			WHERE user_id = $1 AND organization_id = $2
			ORDER BY created_at DESC
		`, userID, constants.CurrentOrganizationID)
		if err != nil {
			log.Printf("[WEBAUTHN] Failed to list passkeys: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list passkeys"})
			return
		}
		defer rows.Close()

		passkeys := []PasskeyResponse{}
		for rows.Next() {
			var p PasskeyResponse
			if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.LastUsedAt); err != nil {
				continue
			}
			passkeys = append(passkeys, p)
		}

		c.JSON(http.StatusOK, passkeys)
	}
}

// DeletePasskey removes a passkey
func deletePasskeyHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.MustGet("userId").(uuid.UUID)
		passkeyID := c.Param("id")
		ctx := c.Request.Context()

		result, err := services.DB.Pool().Exec(ctx, `
			DELETE FROM webauthn_credentials
			WHERE id = $1 AND user_id = $2 AND organization_id = $3
		`, passkeyID, userID, constants.CurrentOrganizationID)
		if err != nil {
			log.Printf("[WEBAUTHN] Failed to delete passkey: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete passkey"})
			return
		}

		if result.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Passkey not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Passkey deleted"})
	}
}

// RenamePasskey updates a passkey's name
func renamePasskeyHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.MustGet("userId").(uuid.UUID)
		passkeyID := c.Param("id")

		var req RenamePasskeyRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		ctx := c.Request.Context()

		result, err := services.DB.Pool().Exec(ctx, `
			UPDATE webauthn_credentials
			SET name = $1
			WHERE id = $2 AND user_id = $3 AND organization_id = $4
		`, req.Name, passkeyID, userID, constants.CurrentOrganizationID)
		if err != nil {
			log.Printf("[WEBAUTHN] Failed to rename passkey: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to rename passkey"})
			return
		}

		if result.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Passkey not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Passkey renamed"})
	}
}

// ---- Helper Functions ----

// getUserWithPasskeys gets a user and their WebAuthn credentials
func getUserWithPasskeys(ctx context.Context, db *database.Database, userID uuid.UUID) (*WebAuthnUser, error) {
	var user WebAuthnUser
	err := db.Pool().QueryRow(ctx, `
		SELECT id, username, email, COALESCE(first_name || ' ' || last_name, username) as display_name
		FROM users WHERE id = $1 AND organization_id = $2
	`, userID, constants.CurrentOrganizationID).Scan(&user.ID, &user.Username, &user.Email, &user.DisplayName)
	if err != nil {
		return nil, err
	}

	// Get credentials
	rows, err := db.Pool().Query(ctx, `
		SELECT credential_id, public_key, aaguid, sign_count, backup_eligible, backup_state, transports
		FROM webauthn_credentials
		WHERE user_id = $1 AND organization_id = $2
	`, userID, constants.CurrentOrganizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var credID, publicKey, aaguid []byte
		var signCount uint32
		var backupEligible, backupState bool
		var transports []string

		err := rows.Scan(&credID, &publicKey, &aaguid, &signCount, &backupEligible, &backupState, &transports)
		if err != nil {
			continue
		}

		// Convert transports
		var credTransports []protocol.AuthenticatorTransport
		for _, t := range transports {
			credTransports = append(credTransports, protocol.AuthenticatorTransport(t))
		}

		cred := webauthn.Credential{
			ID:        credID,
			PublicKey: publicKey,
			Transport: credTransports,
			Flags: webauthn.CredentialFlags{
				BackupEligible: backupEligible,
				BackupState:    backupState,
			},
			Authenticator: webauthn.Authenticator{
				AAGUID:    aaguid,
				SignCount: signCount,
			},
		}
		user.Credentials = append(user.Credentials, cred)
	}

	return &user, nil
}

// getUsersWithPasskeys gets all users who have registered passkeys
func getUsersWithPasskeys(ctx context.Context, db *database.Database) ([]*WebAuthnUser, error) {
	// Get users with at least one credential
	rows, err := db.Pool().Query(ctx, `
		SELECT DISTINCT u.id, u.username, u.email, COALESCE(u.first_name || ' ' || u.last_name, u.username) as display_name
		FROM users u
		INNER JOIN webauthn_credentials wc ON u.id = wc.user_id
		WHERE u.organization_id = $1 AND u.is_active = true
	`, constants.CurrentOrganizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*WebAuthnUser
	for rows.Next() {
		var user WebAuthnUser
		if err := rows.Scan(&user.ID, &user.Username, &user.Email, &user.DisplayName); err != nil {
			continue
		}

		// Get credentials for this user
		credRows, err := db.Pool().Query(ctx, `
			SELECT credential_id, public_key, aaguid, sign_count, backup_eligible, backup_state, transports
			FROM webauthn_credentials
			WHERE user_id = $1 AND organization_id = $2
		`, user.ID, constants.CurrentOrganizationID)
		if err != nil {
			continue
		}

		for credRows.Next() {
			var credID, publicKey, aaguid []byte
			var signCount uint32
			var backupEligible, backupState bool
			var transports []string

			err := credRows.Scan(&credID, &publicKey, &aaguid, &signCount, &backupEligible, &backupState, &transports)
			if err != nil {
				continue
			}

			var credTransports []protocol.AuthenticatorTransport
			for _, t := range transports {
				credTransports = append(credTransports, protocol.AuthenticatorTransport(t))
			}

			cred := webauthn.Credential{
				ID:        credID,
				PublicKey: publicKey,
				Transport: credTransports,
				Flags: webauthn.CredentialFlags{
					BackupEligible: backupEligible,
					BackupState:    backupState,
				},
				Authenticator: webauthn.Authenticator{
					AAGUID:    aaguid,
					SignCount: signCount,
				},
			}
			user.Credentials = append(user.Credentials, cred)
		}
		credRows.Close()

		users = append(users, &user)
	}

	return users, nil
}

// getUserByCredentialID finds a user by their credential ID
func getUserByCredentialID(ctx context.Context, db *database.Database, credentialID []byte) (*WebAuthnUser, error) {
	var userID uuid.UUID
	err := db.Pool().QueryRow(ctx, `
		SELECT user_id FROM webauthn_credentials
		WHERE credential_id = $1 AND organization_id = $2
	`, credentialID, constants.CurrentOrganizationID).Scan(&userID)
	if err != nil {
		return nil, err
	}

	return getUserWithPasskeys(ctx, db, userID)
}

// generateAccessTokenForUser generates a JWT access token
func generateAccessTokenForUser(userID uuid.UUID, email, role, jwtSecret string) (string, error) {
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
	return token.SignedString([]byte(jwtSecret))
}

// generateRefreshTokenForUser generates a refresh token
func generateRefreshTokenForUser(ctx context.Context, db *database.Database, userID uuid.UUID, ipAddress, userAgent string) (string, error) {
	token := uuid.New().String()

	// Hash token for storage
	h := make([]byte, 32)
	rand.Read(h)
	tokenHash := hashTokenForStorage(token)

	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	_, err := db.Pool().Exec(ctx, `
		INSERT INTO sessions (user_id, refresh_token_hash, ip_address, user_agent, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, userID, tokenHash, ipAddress, userAgent, expiresAt)

	if err != nil {
		return "", err
	}

	return token, nil
}

// hashTokenForStorage creates a SHA-256 hash of a token
func hashTokenForStorage(token string) string {
	h := sha256.New()
	h.Write([]byte(token))
	return hex.EncodeToString(h.Sum(nil))
}

// parseCredentialAssertionFromBytes parses a WebAuthn assertion response from raw JSON bytes
func parseCredentialAssertionFromBytes(data []byte) (*protocol.ParsedCredentialAssertionData, error) {
	var car protocol.CredentialAssertionResponse
	if err := json.Unmarshal(data, &car); err != nil {
		return nil, err
	}
	return car.Parse()
}

// parseCredentialCreationFromBytes parses a WebAuthn registration response from raw JSON bytes
func parseCredentialCreationFromBytes(data []byte) (*protocol.ParsedCredentialCreationData, error) {
	var ccr protocol.CredentialCreationResponse
	if err := json.Unmarshal(data, &ccr); err != nil {
		return nil, err
	}
	return ccr.Parse()
}

// RegisterWebAuthnRoutes registers WebAuthn routes
func RegisterWebAuthnRoutes(api *gin.RouterGroup, protected *gin.RouterGroup, services *Services) error {
	waService, err := NewWebAuthnService(services.Config, services.DB)
	if err != nil {
		return err
	}

	// Public routes (for authentication/login)
	webauthn := api.Group("/webauthn")
	webauthn.Use(rateLimitMiddleware(services.Redis, services.Config.RateLimitRequests, services.Config.RateLimitWindow))
	{
		webauthn.POST("/authenticate/begin", beginPasskeyAuthenticationHandler(services, waService))
		webauthn.POST("/authenticate/finish", finishPasskeyAuthenticationHandler(services, waService))
	}

	// Protected routes (for registration/management)
	webauthnProtected := protected.Group("/webauthn")
	{
		webauthnProtected.POST("/register/begin", beginPasskeyRegistrationHandler(services, waService))
		webauthnProtected.POST("/register/finish", finishPasskeyRegistrationHandler(services, waService))
	}

	// Passkey management routes
	passkeys := protected.Group("/passkeys")
	{
		passkeys.GET("", listPasskeysHandler(services))
		passkeys.DELETE("/:id", deletePasskeyHandler(services))
		passkeys.PUT("/:id", renamePasskeyHandler(services))
	}

	return nil
}

package credentials

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNoActiveKey      = errors.New("no active JWT signing key found")
	ErrInvalidToken     = errors.New("invalid token")
	ErrTokenExpired     = errors.New("token expired")
	ErrRotationInProgress = errors.New("rotation already in progress")
	ErrHealthCheckFailed = errors.New("pre-rotation health check failed")
)

// KeyStatus represents the lifecycle state of a credential key
type KeyStatus string

const (
	KeyStatusActive      KeyStatus = "active"
	KeyStatusGracePeriod KeyStatus = "grace_period"
	KeyStatusRetired     KeyStatus = "retired"
	KeyStatusRevoked     KeyStatus = "revoked"
)

// JWTKey represents a JWT signing key with its metadata
type JWTKey struct {
	ID              uuid.UUID
	Secret          []byte
	Version         int
	Status          KeyStatus
	CreatedAt       time.Time
	ActivatedAt     *time.Time
	GracePeriodEnds *time.Time
	RetiredAt       *time.Time
}

// RotationResult contains the outcome of a rotation operation
type RotationResult struct {
	Success          bool      `json:"success"`
	OldVersion       int       `json:"oldVersion,omitempty"`
	NewVersion       int       `json:"newVersion"`
	GracePeriodEnds  time.Time `json:"gracePeriodEnds"`
	AffectedSessions int       `json:"affectedSessions"`
	LogID            uuid.UUID `json:"logId"`
	Message          string    `json:"message,omitempty"`
}

// JWTStatus represents the current state of JWT key management
type JWTStatus struct {
	CurrentVersion      int        `json:"currentVersion"`
	Status              string     `json:"status"`
	CreatedAt           time.Time  `json:"createdAt"`
	LastRotation        *time.Time `json:"lastRotation,omitempty"`
	NextScheduledRotation *time.Time `json:"nextScheduledRotation,omitempty"`
	GracePeriodActive   bool       `json:"gracePeriodActive"`
	GracePeriodEnds     *time.Time `json:"gracePeriodEnds,omitempty"`
	ActiveKeyCount      int        `json:"activeKeyCount"`
	HealthStatus        string     `json:"healthStatus"` // healthy, warning, overdue, grace_period
}

// Claims represents JWT token claims
type Claims struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// JWTManager handles JWT signing keys with dual-key rotation support
type JWTManager struct {
	db              *pgxpool.Pool
	encryptor       *KeyEncryptor
	activeKeys      []JWTKey         // Keys eligible for validation (active + grace_period)
	signingKey      *JWTKey          // Current key for signing new tokens
	gracePeriod     time.Duration
	mu              sync.RWMutex
	rotationMu      sync.Mutex       // Prevents concurrent rotations
	initialized     bool
	legacySecret    []byte           // Fallback for existing tokens signed with env var secret
}

// NewJWTManager creates a new JWT manager
func NewJWTManager(db *pgxpool.Pool, masterKey []byte, legacySecret string) (*JWTManager, error) {
	encryptor, err := NewKeyEncryptor(masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create encryptor: %w", err)
	}

	m := &JWTManager{
		db:           db,
		encryptor:    encryptor,
		gracePeriod:  24 * time.Hour, // Default 24-hour grace period
		activeKeys:   make([]JWTKey, 0),
	}

	// Store legacy secret for backward compatibility
	if legacySecret != "" {
		m.legacySecret = []byte(legacySecret)
	}

	return m, nil
}

// Initialize loads active keys from database or creates initial key
func (m *JWTManager) Initialize(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.initialized {
		return nil
	}

	// Load active keys from database
	if err := m.loadKeysLocked(ctx); err != nil {
		return fmt.Errorf("failed to load keys: %w", err)
	}

	// If no keys exist, create initial key
	if len(m.activeKeys) == 0 {
		log.Printf("[JWTManager] No keys found, creating initial key")
		if err := m.createInitialKeyLocked(ctx); err != nil {
			return fmt.Errorf("failed to create initial key: %w", err)
		}
	}

	m.initialized = true
	log.Printf("[JWTManager] Initialized with %d active keys, signing with version %d",
		len(m.activeKeys), m.signingKey.Version)

	return nil
}

// loadKeysLocked loads all active and grace period keys from database
// Must be called with m.mu held
func (m *JWTManager) loadKeysLocked(ctx context.Context) error {
	rows, err := m.db.Query(ctx, `
		SELECT id, key_value_encrypted, version, status, created_at,
		       activated_at, grace_period_ends, retired_at
		FROM credential_keys
		WHERE credential_type = 'jwt_secret'
		  AND status IN ('active', 'grace_period')
		ORDER BY version DESC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	m.activeKeys = make([]JWTKey, 0)

	for rows.Next() {
		var key JWTKey
		var encryptedValue []byte

		err := rows.Scan(
			&key.ID, &encryptedValue, &key.Version, &key.Status,
			&key.CreatedAt, &key.ActivatedAt, &key.GracePeriodEnds, &key.RetiredAt,
		)
		if err != nil {
			return err
		}

		// Decrypt the key
		decrypted, err := m.encryptor.Decrypt(encryptedValue)
		if err != nil {
			log.Printf("[JWTManager] Warning: failed to decrypt key version %d: %v", key.Version, err)
			continue
		}
		key.Secret = decrypted

		m.activeKeys = append(m.activeKeys, key)

		// First active key is the signing key
		if key.Status == KeyStatusActive && m.signingKey == nil {
			keyCopy := key
			m.signingKey = &keyCopy
		}
	}

	return rows.Err()
}

// createInitialKeyLocked creates the first JWT signing key
// Must be called with m.mu held
func (m *JWTManager) createInitialKeyLocked(ctx context.Context) error {
	// Generate 64-byte secret
	secret := make([]byte, 64)
	if _, err := rand.Read(secret); err != nil {
		return fmt.Errorf("failed to generate secret: %w", err)
	}

	// Encrypt for storage
	encrypted, err := m.encryptor.Encrypt(secret)
	if err != nil {
		return fmt.Errorf("failed to encrypt secret: %w", err)
	}

	// Hash for quick lookup
	hash := sha256.Sum256(secret)
	hashHex := hex.EncodeToString(hash[:])

	keyID := uuid.New()
	now := time.Now()

	_, err = m.db.Exec(ctx, `
		INSERT INTO credential_keys (
			id, credential_type, key_value_encrypted, key_hash,
			version, status, created_at, activated_at
		) VALUES ($1, 'jwt_secret', $2, $3, 1, 'active', $4, $4)
	`, keyID, encrypted, hashHex, now)
	if err != nil {
		return err
	}

	// Log the creation
	_, err = m.db.Exec(ctx, `
		INSERT INTO credential_rotation_log (
			credential_type, credential_key_id, new_version, action,
			status, completed_at, metadata
		) VALUES ('jwt_secret', $1, 1, 'create', 'success', $2, '{"initial": true}')
	`, keyID, now)
	if err != nil {
		log.Printf("[JWTManager] Warning: failed to log initial key creation: %v", err)
	}

	key := JWTKey{
		ID:          keyID,
		Secret:      secret,
		Version:     1,
		Status:      KeyStatusActive,
		CreatedAt:   now,
		ActivatedAt: &now,
	}

	m.activeKeys = []JWTKey{key}
	m.signingKey = &key

	return nil
}

// SignToken creates a new JWT token signed with the current active key
func (m *JWTManager) SignToken(claims *Claims) (string, error) {
	m.mu.RLock()
	signingKey := m.signingKey
	m.mu.RUnlock()

	if signingKey == nil {
		return "", ErrNoActiveKey
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(signingKey.Secret)
}

// ValidateToken validates a JWT token against all active keys (including grace period)
// This is the core of zero-downtime rotation - tokens signed with old keys remain valid
func (m *JWTManager) ValidateToken(tokenString string) (*Claims, error) {
	m.mu.RLock()
	keys := m.activeKeys
	legacySecret := m.legacySecret
	m.mu.RUnlock()

	// Try each active key
	for _, key := range keys {
		claims, err := m.validateWithKey(tokenString, key.Secret)
		if err == nil {
			return claims, nil
		}
	}

	// Fallback to legacy secret for backward compatibility
	if legacySecret != nil {
		claims, err := m.validateWithKey(tokenString, legacySecret)
		if err == nil {
			log.Printf("[JWTManager] Token validated with legacy secret - user should re-authenticate")
			return claims, nil
		}
	}

	return nil, ErrInvalidToken
}

// validateWithKey attempts to validate a token with a specific key
func (m *JWTManager) validateWithKey(tokenString string, secret []byte) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// Rotate performs a zero-downtime key rotation
func (m *JWTManager) Rotate(ctx context.Context, initiatedBy uuid.UUID) (*RotationResult, error) {
	// Prevent concurrent rotations
	if !m.rotationMu.TryLock() {
		return nil, ErrRotationInProgress
	}
	defer m.rotationMu.Unlock()

	// Start rotation log entry
	logID := uuid.New()
	_, err := m.db.Exec(ctx, `
		INSERT INTO credential_rotation_log (
			id, credential_type, action, initiated_by, status
		) VALUES ($1, 'jwt_secret', 'rotate', $2, 'in_progress')
	`, logID, initiatedBy)
	if err != nil {
		return nil, fmt.Errorf("failed to create rotation log: %w", err)
	}

	// Perform rotation in transaction
	result, err := m.performRotation(ctx, logID, initiatedBy)
	if err != nil {
		// Update log with failure
		m.db.Exec(ctx, `
			UPDATE credential_rotation_log
			SET status = 'failed', failure_reason = $2, completed_at = NOW()
			WHERE id = $1
		`, logID, err.Error())
		return nil, err
	}

	return result, nil
}

// performRotation executes the actual rotation logic
func (m *JWTManager) performRotation(ctx context.Context, logID, initiatedBy uuid.UUID) (*RotationResult, error) {
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Get current active key version
	var oldVersion int
	var oldKeyID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id, version FROM credential_keys
		WHERE credential_type = 'jwt_secret' AND status = 'active'
		ORDER BY version DESC LIMIT 1
	`).Scan(&oldKeyID, &oldVersion)
	if err != nil && err != pgx.ErrNoRows {
		return nil, fmt.Errorf("failed to get current key: %w", err)
	}

	newVersion := oldVersion + 1
	gracePeriodEnds := time.Now().Add(m.gracePeriod)

	// Move current active key to grace period
	if oldVersion > 0 {
		_, err = tx.Exec(ctx, `
			UPDATE credential_keys
			SET status = 'grace_period',
			    grace_period_start = NOW(),
			    grace_period_ends = $2
			WHERE id = $1
		`, oldKeyID, gracePeriodEnds)
		if err != nil {
			return nil, fmt.Errorf("failed to transition old key: %w", err)
		}
	}

	// Generate new secret
	secret := make([]byte, 64)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("failed to generate secret: %w", err)
	}

	// Encrypt for storage
	encrypted, err := m.encryptor.Encrypt(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt secret: %w", err)
	}

	// Hash for quick lookup
	hash := sha256.Sum256(secret)
	hashHex := hex.EncodeToString(hash[:])

	// Insert new key
	newKeyID := uuid.New()
	now := time.Now()
	_, err = tx.Exec(ctx, `
		INSERT INTO credential_keys (
			id, credential_type, key_value_encrypted, key_hash,
			version, status, created_at, activated_at, created_by
		) VALUES ($1, 'jwt_secret', $2, $3, $4, 'active', $5, $5, $6)
	`, newKeyID, encrypted, hashHex, newVersion, now, initiatedBy)
	if err != nil {
		return nil, fmt.Errorf("failed to insert new key: %w", err)
	}

	// Update rotation log
	_, err = tx.Exec(ctx, `
		UPDATE credential_rotation_log
		SET old_version = $2, new_version = $3, status = 'success',
		    completed_at = NOW(), credential_key_id = $4,
		    grace_period_hours = $5, affected_sessions = 0
		WHERE id = $1
	`, logID, oldVersion, newVersion, newKeyID, int(m.gracePeriod.Hours()))
	if err != nil {
		return nil, fmt.Errorf("failed to update rotation log: %w", err)
	}

	// Update rotation schedule
	_, err = tx.Exec(ctx, `
		UPDATE credential_rotation_schedule
		SET last_rotation_at = NOW(),
		    last_rotation_log_id = $1,
		    next_scheduled_rotation = NOW() + (rotation_interval_days || ' days')::INTERVAL,
		    updated_at = NOW()
		WHERE credential_type = 'jwt_secret'
	`, logID)
	if err != nil {
		log.Printf("[JWTManager] Warning: failed to update rotation schedule: %v", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit rotation: %w", err)
	}

	// Reload keys into memory
	if err := m.loadKeysLocked(ctx); err != nil {
		// This is serious but the database is already updated
		log.Printf("[JWTManager] CRITICAL: failed to reload keys after rotation: %v", err)
		return nil, fmt.Errorf("rotation committed but failed to reload: %w", err)
	}

	log.Printf("[JWTManager] Rotation complete: v%d -> v%d, grace period ends %s",
		oldVersion, newVersion, gracePeriodEnds.Format(time.RFC3339))

	// Schedule grace period expiration check
	go m.scheduleGraceExpiration(ctx, oldKeyID, m.gracePeriod)

	return &RotationResult{
		Success:          true,
		OldVersion:       oldVersion,
		NewVersion:       newVersion,
		GracePeriodEnds:  gracePeriodEnds,
		AffectedSessions: 0, // Zero because of dual-key support!
		LogID:            logID,
		Message:          fmt.Sprintf("Rotated from v%d to v%d. Old key valid until %s", oldVersion, newVersion, gracePeriodEnds.Format(time.RFC3339)),
	}, nil
}

// scheduleGraceExpiration waits for grace period to end and retires the old key
func (m *JWTManager) scheduleGraceExpiration(ctx context.Context, keyID uuid.UUID, gracePeriod time.Duration) {
	timer := time.NewTimer(gracePeriod)
	defer timer.Stop()

	select {
	case <-timer.C:
		// Grace period ended, retire the key
		_, err := m.db.Exec(context.Background(), `
			UPDATE credential_keys
			SET status = 'retired', retired_at = NOW()
			WHERE id = $1 AND status = 'grace_period'
		`, keyID)
		if err != nil {
			log.Printf("[JWTManager] Failed to retire key %s: %v", keyID, err)
			return
		}

		// Reload keys
		m.mu.Lock()
		m.loadKeysLocked(context.Background())
		m.mu.Unlock()

		log.Printf("[JWTManager] Key %s retired after grace period", keyID)

	case <-ctx.Done():
		log.Printf("[JWTManager] Grace period check cancelled for key %s", keyID)
	}
}

// GetStatus returns the current JWT key management status
func (m *JWTManager) GetStatus(ctx context.Context) (*JWTStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.signingKey == nil {
		return &JWTStatus{
			Status:       "no_key",
			HealthStatus: "critical",
		}, nil
	}

	status := &JWTStatus{
		CurrentVersion: m.signingKey.Version,
		Status:         string(m.signingKey.Status),
		CreatedAt:      m.signingKey.CreatedAt,
		ActiveKeyCount: len(m.activeKeys),
	}

	// Check for keys in grace period
	for _, key := range m.activeKeys {
		if key.Status == KeyStatusGracePeriod {
			status.GracePeriodActive = true
			status.GracePeriodEnds = key.GracePeriodEnds
			break
		}
	}

	// Get schedule info
	var nextRotation *time.Time
	var lastRotation *time.Time
	err := m.db.QueryRow(ctx, `
		SELECT next_scheduled_rotation, last_rotation_at
		FROM credential_rotation_schedule
		WHERE credential_type = 'jwt_secret'
	`).Scan(&nextRotation, &lastRotation)
	if err == nil {
		status.NextScheduledRotation = nextRotation
		status.LastRotation = lastRotation
	}

	// Determine health status
	status.HealthStatus = "healthy"
	if status.GracePeriodActive {
		status.HealthStatus = "grace_period"
	} else if nextRotation != nil && nextRotation.Before(time.Now()) {
		status.HealthStatus = "overdue"
	} else if nextRotation != nil && nextRotation.Before(time.Now().Add(7*24*time.Hour)) {
		status.HealthStatus = "warning"
	}

	return status, nil
}

// SetGracePeriod configures the grace period for rotations
func (m *JWTManager) SetGracePeriod(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gracePeriod = duration
}

// ForceReload reloads keys from the database
func (m *JWTManager) ForceReload(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadKeysLocked(ctx)
}

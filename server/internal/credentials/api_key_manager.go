package credentials

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrKeyNotFound      = errors.New("API key not found")
	ErrKeyRevoked       = errors.New("API key has been revoked")
	ErrKeyExpired       = errors.New("API key has expired")
	ErrInvalidKeyFormat = errors.New("invalid API key format")
	ErrPermissionDenied = errors.New("permission denied for this API key")
	ErrIPNotAllowed     = errors.New("IP address not in allowlist")
)

// APIKeyPrefix is prepended to all API keys for identification
const APIKeyPrefix = "sk_live_"

// APIKey represents a managed API key with permissions
type APIKey struct {
	ID           uuid.UUID   `json:"id"`
	KeyPrefix    string      `json:"keyPrefix"`    // First 16 chars for display (sk_live_xxxxxxxx)
	Name         string      `json:"name"`
	Description  string      `json:"description,omitempty"`
	Permissions  []string    `json:"permissions"`
	IPAllowlist  []string    `json:"ipAllowlist,omitempty"`
	CreatedAt    time.Time   `json:"createdAt"`
	CreatedBy    uuid.UUID   `json:"createdBy"`
	LastUsedAt   *time.Time  `json:"lastUsedAt,omitempty"`
	ExpiresAt    *time.Time  `json:"expiresAt,omitempty"`
	RevokedAt    *time.Time  `json:"revokedAt,omitempty"`
	UseCount     int64       `json:"useCount"`
}

// APIKeyWithSecret includes the full key (only returned at creation time)
type APIKeyWithSecret struct {
	APIKey
	FullKey string `json:"fullKey"` // Only populated on creation
}

// APIKeyStatus represents the status of the API key system
type APIKeyStatus struct {
	TotalKeys       int        `json:"totalKeys"`
	ActiveKeys      int        `json:"activeKeys"`
	ExpiringSoon    int        `json:"expiringSoon"`    // Expiring within 7 days
	RecentlyUsed    int        `json:"recentlyUsed"`    // Used within 24 hours
	LastKeyCreated  *time.Time `json:"lastKeyCreated,omitempty"`
}

// CreateAPIKeyRequest contains parameters for creating a new API key
type CreateAPIKeyRequest struct {
	Name         string     `json:"name" binding:"required"`
	Description  string     `json:"description"`
	Permissions  []string   `json:"permissions" binding:"required"`
	IPAllowlist  []string   `json:"ipAllowlist"`
	ExpiresAt    *time.Time `json:"expiresAt"`
	CreatedBy    uuid.UUID  `json:"-"`
}

// APIKeyManager handles API key operations
type APIKeyManager struct {
	db        *pgxpool.Pool
	encryptor *KeyEncryptor
	cache     sync.Map // Quick lookup cache: prefix -> *APIKey
}

// NewAPIKeyManager creates a new API key manager
func NewAPIKeyManager(db *pgxpool.Pool, encryptor *KeyEncryptor) *APIKeyManager {
	return &APIKeyManager{
		db:        db,
		encryptor: encryptor,
	}
}

// CreateKey generates a new API key with specified permissions
func (m *APIKeyManager) CreateKey(ctx context.Context, req CreateAPIKeyRequest) (*APIKeyWithSecret, error) {
	// Generate secure random key: 32 bytes = 64 hex chars
	rawKey := make([]byte, 32)
	if _, err := rand.Read(rawKey); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}
	fullKey := APIKeyPrefix + hex.EncodeToString(rawKey)
	keyPrefix := fullKey[:16] // sk_live_ + first 8 hex chars

	// Hash for storage (bcrypt)
	hash, err := bcrypt.GenerateFromPassword([]byte(fullKey), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash key: %w", err)
	}

	keyID := uuid.New()
	now := time.Now()

	// Insert into database
	_, err = m.db.Exec(ctx, `
		INSERT INTO credential_keys (
			id, credential_type, key_value_encrypted, key_hash,
			version, status, name, description, permissions, ip_allowlist,
			created_at, activated_at, expires_at, created_by
		) VALUES (
			$1, 'api_key', $2, $3,
			1, 'active', $4, $5, $6, $7,
			$8, $8, $9, $10
		)
	`, keyID, hash, keyPrefix, req.Name, req.Description,
		req.Permissions, req.IPAllowlist, now, req.ExpiresAt, req.CreatedBy)
	if err != nil {
		return nil, fmt.Errorf("failed to store key: %w", err)
	}

	// Log creation
	m.db.Exec(ctx, `
		INSERT INTO credential_rotation_log (
			credential_type, credential_key_id, new_version, action,
			initiated_by, status, completed_at
		) VALUES ('api_key', $1, 1, 'create', $2, 'success', $3)
	`, keyID, req.CreatedBy, now)

	log.Printf("[APIKeyManager] Created API key %s (%s) with %d permissions",
		keyID, req.Name, len(req.Permissions))

	return &APIKeyWithSecret{
		APIKey: APIKey{
			ID:          keyID,
			KeyPrefix:   keyPrefix,
			Name:        req.Name,
			Description: req.Description,
			Permissions: req.Permissions,
			IPAllowlist: req.IPAllowlist,
			CreatedAt:   now,
			CreatedBy:   req.CreatedBy,
			ExpiresAt:   req.ExpiresAt,
		},
		FullKey: fullKey, // Only returned once!
	}, nil
}

// ValidateKey validates an API key and returns its permissions
func (m *APIKeyManager) ValidateKey(ctx context.Context, providedKey string, clientIP string) (*APIKey, error) {
	// Check format
	if !strings.HasPrefix(providedKey, APIKeyPrefix) || len(providedKey) < 16 {
		return nil, ErrInvalidKeyFormat
	}

	prefix := providedKey[:16]

	// Check cache first
	if cached, ok := m.cache.Load(prefix); ok {
		key := cached.(*APIKey)
		if err := m.validateKeyState(key, providedKey, clientIP); err != nil {
			return nil, err
		}
		go m.updateLastUsed(key.ID)
		return key, nil
	}

	// Query database
	var key APIKey
	var keyHash string
	var status string
	err := m.db.QueryRow(ctx, `
		SELECT id, key_hash, name, description, permissions, ip_allowlist,
		       status, created_at, created_by, last_used_at, expires_at,
		       revoked_by IS NOT NULL as is_revoked, use_count
		FROM credential_keys
		WHERE credential_type = 'api_key'
		  AND key_hash = $1
	`, prefix).Scan(
		&key.ID, &keyHash, &key.Name, &key.Description, &key.Permissions,
		&key.IPAllowlist, &status, &key.CreatedAt, &key.CreatedBy,
		&key.LastUsedAt, &key.ExpiresAt, &key.RevokedAt, &key.UseCount,
	)
	if err != nil {
		return nil, ErrKeyNotFound
	}

	key.KeyPrefix = prefix

	// Validate bcrypt hash
	if err := bcrypt.CompareHashAndPassword([]byte(keyHash), []byte(providedKey)); err != nil {
		return nil, ErrKeyNotFound
	}

	// Validate key state
	if err := m.validateKeyState(&key, providedKey, clientIP); err != nil {
		return nil, err
	}

	// Cache for future lookups
	m.cache.Store(prefix, &key)

	// Update usage stats
	go m.updateLastUsed(key.ID)

	return &key, nil
}

// validateKeyState checks if the key is valid (not revoked, not expired, IP allowed)
func (m *APIKeyManager) validateKeyState(key *APIKey, providedKey, clientIP string) error {
	if key.RevokedAt != nil {
		return ErrKeyRevoked
	}

	if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
		return ErrKeyExpired
	}

	// Check IP allowlist if configured
	if len(key.IPAllowlist) > 0 && clientIP != "" {
		allowed := false
		for _, ip := range key.IPAllowlist {
			if ip == clientIP || ip == "*" {
				allowed = true
				break
			}
		}
		if !allowed {
			return ErrIPNotAllowed
		}
	}

	return nil
}

// HasPermission checks if a key has a specific permission
func (m *APIKeyManager) HasPermission(key *APIKey, required string) bool {
	for _, perm := range key.Permissions {
		// Exact match
		if perm == required {
			return true
		}
		// Wildcard match: "devices:*" matches "devices:read"
		if strings.HasSuffix(perm, ":*") {
			prefix := strings.TrimSuffix(perm, "*")
			if strings.HasPrefix(required, prefix) {
				return true
			}
		}
		// Admin wildcard
		if perm == "*" || perm == "admin:*" {
			return true
		}
	}
	return false
}

// updateLastUsed updates the last_used_at and use_count in background
func (m *APIKeyManager) updateLastUsed(keyID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m.db.Exec(ctx, `
		UPDATE credential_keys
		SET last_used_at = NOW(), use_count = use_count + 1
		WHERE id = $1
	`, keyID)
}

// ListKeys returns all API keys (without secrets)
func (m *APIKeyManager) ListKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := m.db.Query(ctx, `
		SELECT id, key_hash, name, description, permissions, ip_allowlist,
		       status, created_at, created_by, last_used_at, expires_at,
		       revoked_by IS NOT NULL as is_revoked, use_count
		FROM credential_keys
		WHERE credential_type = 'api_key'
		  AND status != 'retired'
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var key APIKey
		var status string
		var isRevoked bool
		err := rows.Scan(
			&key.ID, &key.KeyPrefix, &key.Name, &key.Description,
			&key.Permissions, &key.IPAllowlist, &status, &key.CreatedAt,
			&key.CreatedBy, &key.LastUsedAt, &key.ExpiresAt, &isRevoked, &key.UseCount,
		)
		if err != nil {
			continue
		}
		if isRevoked {
			now := time.Now()
			key.RevokedAt = &now
		}
		keys = append(keys, key)
	}

	return keys, rows.Err()
}

// RevokeKey revokes an API key
func (m *APIKeyManager) RevokeKey(ctx context.Context, keyID uuid.UUID, revokedBy uuid.UUID, reason string) error {
	result, err := m.db.Exec(ctx, `
		UPDATE credential_keys
		SET status = 'revoked',
		    revoked_by = $2,
		    revocation_reason = $3,
		    retired_at = NOW()
		WHERE id = $1 AND credential_type = 'api_key' AND status = 'active'
	`, keyID, revokedBy, reason)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrKeyNotFound
	}

	// Remove from cache
	m.cache.Range(func(key, value interface{}) bool {
		if apiKey, ok := value.(*APIKey); ok && apiKey.ID == keyID {
			m.cache.Delete(key)
			return false
		}
		return true
	})

	// Log revocation
	m.db.Exec(ctx, `
		INSERT INTO credential_rotation_log (
			credential_type, credential_key_id, new_version, action,
			initiated_by, status, completed_at, metadata
		) VALUES ('api_key', $1, 0, 'revoke', $2, 'success', NOW(), $3)
	`, keyID, revokedBy, fmt.Sprintf(`{"reason": "%s"}`, reason))

	log.Printf("[APIKeyManager] Revoked API key %s by %s: %s", keyID, revokedBy, reason)

	return nil
}

// GetStatus returns statistics about API keys
func (m *APIKeyManager) GetStatus(ctx context.Context) (*APIKeyStatus, error) {
	var status APIKeyStatus

	err := m.db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status != 'retired'),
			COUNT(*) FILTER (WHERE status = 'active'),
			COUNT(*) FILTER (WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at < NOW() + INTERVAL '7 days'),
			COUNT(*) FILTER (WHERE last_used_at > NOW() - INTERVAL '24 hours'),
			MAX(created_at) FILTER (WHERE status = 'active')
		FROM credential_keys
		WHERE credential_type = 'api_key'
	`).Scan(&status.TotalKeys, &status.ActiveKeys, &status.ExpiringSoon,
		&status.RecentlyUsed, &status.LastKeyCreated)
	if err != nil {
		return nil, err
	}

	return &status, nil
}

// ClearCache clears the in-memory key cache
func (m *APIKeyManager) ClearCache() {
	m.cache.Range(func(key, _ interface{}) bool {
		m.cache.Delete(key)
		return true
	})
}

// ValidateKeyWithPermission validates a key and checks for a specific permission
func (m *APIKeyManager) ValidateKeyWithPermission(ctx context.Context, providedKey, clientIP, requiredPermission string) (*APIKey, error) {
	key, err := m.ValidateKey(ctx, providedKey, clientIP)
	if err != nil {
		return nil, err
	}

	if !m.HasPermission(key, requiredPermission) {
		return nil, ErrPermissionDenied
	}

	return key, nil
}

// MigrateStaticAPIKey migrates from the static API_KEY env var to the database
func (m *APIKeyManager) MigrateStaticAPIKey(ctx context.Context, staticKey string, createdBy uuid.UUID) error {
	if staticKey == "" {
		return nil
	}

	// Check if we already have a migrated key
	var count int
	err := m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM credential_keys
		WHERE credential_type = 'api_key'
		  AND metadata->>'migrated_from' = 'env_var'
	`).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil // Already migrated
	}

	// Create a new managed key with full permissions
	req := CreateAPIKeyRequest{
		Name:        "Legacy API Key (Migrated)",
		Description: "Migrated from API_KEY environment variable. Consider rotating this key.",
		Permissions: []string{"*"}, // Full access like the original
		CreatedBy:   createdBy,
	}

	key, err := m.CreateKey(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create migrated key: %w", err)
	}

	// Store the static key hash so it continues to work during transition
	hash, _ := bcrypt.GenerateFromPassword([]byte(staticKey), bcrypt.DefaultCost)
	_, err = m.db.Exec(ctx, `
		UPDATE credential_keys
		SET key_value_encrypted = $2,
		    metadata = '{"migrated_from": "env_var", "original_still_valid": true}'::jsonb
		WHERE id = $1
	`, key.ID, hash)
	if err != nil {
		return fmt.Errorf("failed to update migrated key: %w", err)
	}

	log.Printf("[APIKeyManager] Migrated static API key to managed key %s", key.ID)
	log.Printf("[APIKeyManager] New API key (save this!): %s", key.FullKey)

	return nil
}

// CompareKeys performs constant-time comparison of two keys
func CompareKeys(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// Package pki provides certificate generation and management for agent authentication.
// It handles issuing client certificates during enrollment for mTLS connections.
package pki

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PKI provides certificate issuance and management services.
type PKI struct {
	caCert    *x509.Certificate
	caKey     interface{} // *ecdsa.PrivateKey or *rsa.PrivateKey
	caCertPEM []byte
	db        *pgxpool.Pool
	mu        sync.RWMutex
}

// CertificateBundle contains the issued client certificate and key.
type CertificateBundle struct {
	ClientCert    string    `json:"clientCert"`    // PEM-encoded client certificate
	ClientKey     string    `json:"clientKey"`     // PEM-encoded private key
	CACert        string    `json:"caCert"`        // PEM-encoded CA certificate
	SerialNumber  string    `json:"serialNumber"`  // Certificate serial number (hex)
	Fingerprint   string    `json:"fingerprint"`   // SHA-256 fingerprint of cert
	ExpiresAt     time.Time `json:"expiresAt"`     // Certificate expiration time
	IssuedAt      time.Time `json:"issuedAt"`      // Certificate issue time
}

// Config holds PKI configuration options.
type Config struct {
	CACertPath string // Path to CA certificate PEM file
	CAKeyPath  string // Path to CA private key PEM file
}

// New creates a new PKI service.
func New(cfg Config, db *pgxpool.Pool) (*PKI, error) {
	pki := &PKI{db: db}

	// Load CA certificate
	caCertPEM, err := os.ReadFile(cfg.CACertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}
	pki.caCertPEM = caCertPEM

	block, _ := pem.Decode(caCertPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode CA certificate PEM")
	}

	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}
	pki.caCert = caCert

	// Load CA private key
	caKeyPEM, err := os.ReadFile(cfg.CAKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA key: %w", err)
	}

	block, _ = pem.Decode(caKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode CA key PEM")
	}

	// Try parsing as different key types
	var caKey interface{}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		caKey = key
	} else if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		caKey = key
	} else if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		caKey = key
	} else {
		return nil, fmt.Errorf("failed to parse CA private key: unsupported key type")
	}
	pki.caKey = caKey

	log.Printf("[PKI] Loaded CA certificate: Subject=%s, Expires=%s",
		caCert.Subject.CommonName, caCert.NotAfter.Format(time.RFC3339))

	return pki, nil
}

// IssueClientCertificate generates a new client certificate for an agent.
// The agentID is embedded in the certificate's Subject CN for identification.
func (p *PKI) IssueClientCertificate(ctx context.Context, agentID string, deviceID uuid.UUID, orgID int, validityYears int) (*CertificateBundle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Generate ECDSA P-256 key pair for the client
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate client key: %w", err)
	}

	// Generate random serial number
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	now := time.Now().UTC()
	notAfter := now.AddDate(validityYears, 0, 0)

	// Create certificate template
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   agentID,
			Organization: []string{"Sentinel Agent"},
		},
		NotBefore:             now,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}

	// Sign the certificate with the CA
	certDER, err := x509.CreateCertificate(rand.Reader, template, p.caCert, &privateKey.PublicKey, p.caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// Encode private key to PEM
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	})

	// Calculate fingerprint
	fingerprint := sha256.Sum256(certDER)
	fingerprintHex := hex.EncodeToString(fingerprint[:])

	// Serial number as hex string
	serialHex := hex.EncodeToString(serialNumber.Bytes())

	bundle := &CertificateBundle{
		ClientCert:   string(certPEM),
		ClientKey:    string(keyPEM),
		CACert:       string(p.caCertPEM),
		SerialNumber: serialHex,
		Fingerprint:  fingerprintHex,
		ExpiresAt:    notAfter,
		IssuedAt:     now,
	}

	// Record in database. Failing to record is treated as a hard failure —
	// returning a cert that isn't in client_certificates creates a "ghost
	// cert" that the revocation check (IsCertificateRevoked) cannot find
	// and so always reports unrevoked, no matter what an operator does in
	// the UI. That's exactly the bypass we cannot ship. Fail loudly so the
	// caller can retry or surface the DB failure instead of issuing a cert
	// that is silently outside the revocation regime.
	if err := p.recordCertificate(ctx, agentID, deviceID, orgID, bundle); err != nil {
		log.Printf("[PKI] Failed to record certificate for agent=%s serial=%s: %v", agentID, serialHex, err)
		return nil, fmt.Errorf("failed to record certificate (refusing to issue ghost cert): %w", err)
	}

	log.Printf("[PKI] Issued client certificate for agent %s, serial=%s, expires=%s",
		agentID, serialHex, notAfter.Format(time.RFC3339))

	return bundle, nil
}

// recordCertificate stores certificate metadata in the database.
func (p *PKI) recordCertificate(ctx context.Context, agentID string, deviceID uuid.UUID, orgID int, bundle *CertificateBundle) error {
	certID := uuid.New()

	// Insert into client_certificates table
	_, err := p.db.Exec(ctx, `
		INSERT INTO client_certificates (
			id, agent_id, device_id, serial_number, fingerprint,
			issued_at, expires_at, organization_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, certID, agentID, deviceID, bundle.SerialNumber, bundle.Fingerprint,
		bundle.IssuedAt, bundle.ExpiresAt, orgID)
	if err != nil {
		return fmt.Errorf("failed to insert certificate record: %w", err)
	}

	// Update devices table with certificate info
	_, err = p.db.Exec(ctx, `
		UPDATE devices SET
			client_cert_serial = $1,
			client_cert_issued_at = $2,
			client_cert_expires_at = $3,
			client_cert_fingerprint = $4
		WHERE id = $5
	`, bundle.SerialNumber, bundle.IssuedAt, bundle.ExpiresAt, bundle.Fingerprint, deviceID)
	if err != nil {
		return fmt.Errorf("failed to update device certificate info: %w", err)
	}

	return nil
}

// RevokeCertificate marks a certificate as revoked.
func (p *PKI) RevokeCertificate(ctx context.Context, serialNumber string, reason string) error {
	now := time.Now().UTC()

	result, err := p.db.Exec(ctx, `
		UPDATE client_certificates
		SET revoked_at = $1, revoked_reason = $2
		WHERE serial_number = $3 AND revoked_at IS NULL
	`, now, reason, serialNumber)
	if err != nil {
		return fmt.Errorf("failed to revoke certificate: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("certificate not found or already revoked: %s", serialNumber)
	}

	log.Printf("[PKI] Revoked certificate serial=%s, reason=%s", serialNumber, reason)
	return nil
}

// IsCertificateRevoked checks if a certificate is revoked.
func (p *PKI) IsCertificateRevoked(ctx context.Context, serialNumber string) (bool, error) {
	var revokedAt *time.Time
	err := p.db.QueryRow(ctx, `
		SELECT revoked_at FROM client_certificates
		WHERE serial_number = $1
	`, serialNumber).Scan(&revokedAt)
	if err != nil {
		// Not found in database - not issued by us or not recorded
		return false, nil
	}
	return revokedAt != nil, nil
}

// GetCertificateByFingerprint retrieves certificate info by fingerprint.
func (p *PKI) GetCertificateByFingerprint(ctx context.Context, fingerprint string) (*CertificateInfo, error) {
	var info CertificateInfo
	err := p.db.QueryRow(ctx, `
		SELECT agent_id, device_id, serial_number, fingerprint,
			   issued_at, expires_at, revoked_at, organization_id
		FROM client_certificates
		WHERE fingerprint = $1
	`, fingerprint).Scan(
		&info.AgentID, &info.DeviceID, &info.SerialNumber, &info.Fingerprint,
		&info.IssuedAt, &info.ExpiresAt, &info.RevokedAt, &info.OrganizationID,
	)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// GetCertificateBySerial retrieves certificate info by serial number.
func (p *PKI) GetCertificateBySerial(ctx context.Context, serial string) (*CertificateInfo, error) {
	var info CertificateInfo
	err := p.db.QueryRow(ctx, `
		SELECT agent_id, device_id, serial_number, fingerprint,
			   issued_at, expires_at, revoked_at, organization_id
		FROM client_certificates
		WHERE serial_number = $1
	`, serial).Scan(
		&info.AgentID, &info.DeviceID, &info.SerialNumber, &info.Fingerprint,
		&info.IssuedAt, &info.ExpiresAt, &info.RevokedAt, &info.OrganizationID,
	)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// CertificateInfo contains metadata about an issued certificate.
type CertificateInfo struct {
	AgentID        string
	DeviceID       uuid.UUID
	SerialNumber   string
	Fingerprint    string
	IssuedAt       time.Time
	ExpiresAt      time.Time
	RevokedAt      *time.Time
	OrganizationID int
}

// NeedsRenewal checks if a certificate needs renewal (expires within 30 days).
func (c *CertificateInfo) NeedsRenewal() bool {
	return time.Until(c.ExpiresAt) < 30*24*time.Hour
}

// IsValid checks if the certificate is valid (not expired and not revoked).
func (c *CertificateInfo) IsValid() bool {
	now := time.Now().UTC()
	return c.RevokedAt == nil && now.Before(c.ExpiresAt) && now.After(c.IssuedAt)
}

// GetAgentIDFromCert extracts the agent ID from a client certificate's Common Name.
func GetAgentIDFromCert(cert *x509.Certificate) string {
	return cert.Subject.CommonName
}

// GetSerialFromCert returns the serial number of a certificate as a hex string.
func GetSerialFromCert(cert *x509.Certificate) string {
	return hex.EncodeToString(cert.SerialNumber.Bytes())
}

// RenewCertificate issues a new certificate and revokes the old one.
func (p *PKI) RenewCertificate(ctx context.Context, agentID string, deviceID uuid.UUID, orgID int, oldSerial string, validityYears int) (*CertificateBundle, error) {
	// Issue new certificate
	bundle, err := p.IssueClientCertificate(ctx, agentID, deviceID, orgID, validityYears)
	if err != nil {
		return nil, fmt.Errorf("failed to issue renewal certificate: %w", err)
	}

	// Revoke old certificate
	if oldSerial != "" {
		if err := p.RevokeCertificate(ctx, oldSerial, "renewed"); err != nil {
			log.Printf("[PKI] Warning: Failed to revoke old certificate during renewal: %v", err)
			// Continue - new cert is still valid
		}
	}

	log.Printf("[PKI] Renewed certificate for agent %s, old_serial=%s, new_serial=%s",
		agentID, oldSerial, bundle.SerialNumber)

	return bundle, nil
}

// GetCACert returns the CA certificate in PEM format.
func (p *PKI) GetCACert() []byte {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.caCertPEM
}

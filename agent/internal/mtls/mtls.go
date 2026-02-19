// Package mtls provides mutual TLS configuration for the Sentinel agent.
// It handles loading client certificates and CA certificates for secure
// communication with the Sentinel server.
package mtls

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sentinel/agent/internal/paths"
)

var (
	tlsConfig    *tls.Config
	tlsConfigErr error
	tlsConfigMu  sync.RWMutex
	tlsLoaded    bool
)

// GetTLSConfig returns a TLS configuration for mTLS connections.
// If client certificates are available, it configures mutual TLS.
// Otherwise, it returns a config that only verifies the server (or skips verification for development).
func GetTLSConfig() (*tls.Config, error) {
	tlsConfigMu.RLock()
	if tlsLoaded {
		defer tlsConfigMu.RUnlock()
		return tlsConfig, tlsConfigErr
	}
	tlsConfigMu.RUnlock()

	tlsConfigMu.Lock()
	defer tlsConfigMu.Unlock()
	if tlsLoaded {
		return tlsConfig, tlsConfigErr
	}
	tlsConfig, tlsConfigErr = loadTLSConfig()
	tlsLoaded = true
	return tlsConfig, tlsConfigErr
}

// ReloadTLSConfig forces a reload of TLS configuration.
// This should be called after new certificates are installed.
func ReloadTLSConfig() (*tls.Config, error) {
	tlsConfigMu.Lock()
	defer tlsConfigMu.Unlock()
	tlsConfig, tlsConfigErr = loadTLSConfig()
	tlsLoaded = true
	return tlsConfig, tlsConfigErr
}

// loadTLSConfig loads the TLS configuration from disk.
func loadTLSConfig() (*tls.Config, error) {
	config := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// Start with system root CAs so we trust public CAs (like Let's Encrypt)
	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		log.Printf("[mTLS] Warning: Could not load system cert pool: %v, creating new pool", err)
		caCertPool = x509.NewCertPool()
	}

	// Also load our internal CA certificate if available (for mTLS and internal CA-signed certs)
	caCertPath := paths.CACertPath()
	if _, err := os.Stat(caCertPath); err == nil {
		caCert, err := os.ReadFile(caCertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		if !caCertPool.AppendCertsFromPEM(caCert) {
			log.Println("[mTLS] Warning: Failed to parse internal CA certificate")
		} else {
			log.Println("[mTLS] Loaded internal CA certificate (in addition to system CAs)")
		}
	} else {
		log.Println("[mTLS] No internal CA certificate found, using system root CAs only")
	}

	config.RootCAs = caCertPool

	// Load client certificate and key if available (for mTLS)
	certPath := paths.ClientCertPath()
	keyPath := paths.ClientKeyPath()

	if paths.HasClientCertificate() {
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}

		config.Certificates = []tls.Certificate{cert}
		log.Printf("[mTLS] Loaded client certificate: %s", certPath)
		log.Println("[mTLS] mTLS enabled - client will authenticate with certificate")
	} else {
		log.Println("[mTLS] No client certificate found, mTLS not enabled")
	}

	return config, nil
}

// HasMTLS returns true if the agent has valid mTLS certificates installed.
func HasMTLS() bool {
	return paths.HasClientCertificate()
}

// InstallCertificates saves the client certificate and key to disk.
// This is typically called after enrollment when the server provides certificates.
func InstallCertificates(clientCert, clientKey, caCert []byte) error {
	if err := paths.EnsureCertsDir(); err != nil {
		return fmt.Errorf("failed to create certs directory: %w", err)
	}

	// Save CA certificate
	if len(caCert) > 0 {
		if err := os.WriteFile(paths.CACertPath(), caCert, 0644); err != nil {
			return fmt.Errorf("failed to write CA certificate: %w", err)
		}
		log.Printf("[mTLS] Installed CA certificate: %s", paths.CACertPath())
	}

	// Save client certificate
	if len(clientCert) > 0 {
		if err := os.WriteFile(paths.ClientCertPath(), clientCert, 0644); err != nil {
			return fmt.Errorf("failed to write client certificate: %w", err)
		}
		log.Printf("[mTLS] Installed client certificate: %s", paths.ClientCertPath())
	}

	// Save client key with restrictive permissions
	if len(clientKey) > 0 {
		if err := os.WriteFile(paths.ClientKeyPath(), clientKey, 0600); err != nil {
			return fmt.Errorf("failed to write client key: %w", err)
		}
		log.Printf("[mTLS] Installed client key: %s", paths.ClientKeyPath())
	}

	// Reload TLS config to pick up new certificates
	if _, err := ReloadTLSConfig(); err != nil {
		return fmt.Errorf("failed to reload TLS config: %w", err)
	}

	return nil
}

// GetMTLSPort returns the mTLS port for agent connections (8443).
// This is the dedicated port that requires client certificates.
func GetMTLSPort() string {
	return "8443"
}

// GetMTLSServerURL converts a standard server URL to use the mTLS port.
// Example: https://example.com:443 -> https://example.com:8443
// Example: wss://example.com:443/ws/agent -> wss://example.com:8443/ws/agent/mtls
func GetMTLSServerURL(serverURL string) string {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		log.Printf("[mTLS] Failed to parse server URL: %v", err)
		return serverURL
	}

	// Get host and current port
	host := parsed.Hostname()
	port := parsed.Port()

	// Map standard ports to mTLS port
	mtlsPort := GetMTLSPort()
	if port == "" || port == "443" || port == "4443" {
		parsed.Host = host + ":" + mtlsPort
	} else if port == "8443" {
		// Already on mTLS port
	} else {
		// For non-standard ports, append mTLS port
		parsed.Host = host + ":" + mtlsPort
	}

	// Update path to mTLS endpoint if it's a WebSocket path
	if strings.Contains(parsed.Path, "/ws/agent") && !strings.HasSuffix(parsed.Path, "/mtls") {
		parsed.Path = strings.Replace(parsed.Path, "/ws/agent", "/ws/agent/mtls", 1)
	} else if parsed.Path == "/ws" {
		parsed.Path = "/ws/agent/mtls"
	}

	return parsed.String()
}

// GetCertificateExpiry returns the expiration time of the client certificate.
// Returns nil if no certificate is installed or it cannot be parsed.
func GetCertificateExpiry() (*time.Time, error) {
	certPath := paths.ClientCertPath()
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return &cert.NotAfter, nil
}

// NeedsRenewal checks if the client certificate needs renewal.
// Returns true if the certificate expires within 30 days.
func NeedsRenewal() bool {
	expiry, err := GetCertificateExpiry()
	if err != nil {
		// If we can't determine expiry, assume renewal is needed
		return true
	}

	// Renew if expiring within 30 days
	renewalWindow := 30 * 24 * time.Hour
	return time.Until(*expiry) < renewalWindow
}

// GetCertificateSerial returns the serial number of the client certificate.
func GetCertificateSerial() (string, error) {
	certPath := paths.ClientCertPath()
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return "", fmt.Errorf("failed to read certificate: %w", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse certificate: %w", err)
	}

	return fmt.Sprintf("%x", cert.SerialNumber), nil
}

// Package tlsconfig provides a shared TLS configuration factory for the Sentinel
// backend's mTLS listeners (HTTP :8443 and gRPC :4444). It centralizes cert
// loading, cipher/curve policy, and hot-reload so both listeners stay consistent.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// Config holds the file paths needed to build an mTLS tls.Config.
type Config struct {
	CertPath   string // Server certificate PEM
	KeyPath    string // Server private key PEM
	CACertPath string // CA certificate PEM for client verification
}

// certHolder stores a certificate and supports atomic swap for hot-reload.
type certHolder struct {
	mu   sync.RWMutex
	cert *tls.Certificate
}

func (h *certHolder) get() *tls.Certificate {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cert
}

func (h *certHolder) swap(c *tls.Certificate) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cert = c
}

// LoadAgentMTLSConfig builds a *tls.Config suitable for both the HTTP mTLS
// listener (:8443) and the gRPC server (:4444). It mirrors the cipher suite,
// curve, and version policy from configs/traefik-agent/dynamic/tls.yml so the
// switchover from Traefik to native Go is transparent to agents.
//
// The returned config uses a GetCertificate callback that hot-reloads the server
// certificate when the file changes on disk, avoiding backend restarts for cert
// rotation. A background goroutine polls the cert file's modtime every 30 seconds.
func LoadAgentMTLSConfig(cfg Config) (*tls.Config, error) {
	// Load initial server certificate
	cert, err := tls.LoadX509KeyPair(cfg.CertPath, cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("load server cert/key: %w", err)
	}

	holder := &certHolder{cert: &cert}

	// Load CA certificate for client verification
	caCertPEM, err := os.ReadFile(cfg.CACertPath)
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCertPEM) {
		return nil, fmt.Errorf("failed to parse CA certificate from %s", cfg.CACertPath)
	}

	// Start background cert watcher for hot-reload
	go watchCertFiles(cfg.CertPath, cfg.KeyPath, holder)

	tlsCfg := &tls.Config{
		// GetCertificate is called on every TLS handshake, returning the
		// latest cert from the atomic holder. This enables zero-downtime
		// cert rotation.
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return holder.get(), nil
		},
		ClientCAs:  caPool,
		ClientAuth: tls.VerifyClientCertIfGiven,
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS13,

		// Match Traefik agent-gateway cipher suites exactly:
		//   TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
		//   TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
		//   TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384
		//   TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		},
		CurvePreferences: []tls.CurveID{
			tls.X25519,
			tls.CurveP384,
			tls.CurveP256,
		},
	}

	return tlsCfg, nil
}

// watchCertFiles polls the server cert file for modtime changes and reloads the
// certificate pair when a change is detected. Runs until the process exits.
func watchCertFiles(certPath, keyPath string, holder *certHolder) {
	var lastMod time.Time
	if info, err := os.Stat(certPath); err == nil {
		lastMod = info.ModTime()
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		info, err := os.Stat(certPath)
		if err != nil {
			continue
		}
		if info.ModTime().Equal(lastMod) {
			continue
		}

		newCert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			log.Printf("[TLS] Failed to reload certificate: %v", err)
			continue
		}

		holder.swap(&newCert)
		lastMod = info.ModTime()
		log.Printf("[TLS] Server certificate reloaded (new modtime: %s)", lastMod.Format(time.RFC3339))
	}
}

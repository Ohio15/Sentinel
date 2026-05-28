package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"
	"time"
)

// TestParseCSR_Valid generates a fresh CSR with a real ECDSA key and verifies
// parseCSR accepts it. Real key material (no mocks) to ensure the PEM decode
// and CheckSignature paths execute end-to-end.
func TestParseCSR_Valid(t *testing.T) {
	csrPEM := makeTestCSR(t, "test-agent-001")
	csr, err := parseCSR(csrPEM)
	if err != nil {
		t.Fatalf("parseCSR rejected a valid CSR: %v", err)
	}
	if csr.Subject.CommonName != "test-agent-001" {
		t.Errorf("expected CN test-agent-001, got %q", csr.Subject.CommonName)
	}
}

func TestParseCSR_InvalidPEM(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"not pem", "this is not a pem block"},
		{"wrong block type", "-----BEGIN CERTIFICATE-----\nMIICk==\n-----END CERTIFICATE-----\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseCSR(tc.in); err == nil {
				t.Errorf("parseCSR accepted bad input %q", tc.name)
			}
		})
	}
}

func TestParseCSR_TamperedSignature(t *testing.T) {
	// Generate a valid CSR, then flip one byte in the signature.
	csrPEM := makeTestCSR(t, "tamper-target")
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil {
		t.Fatal("failed to decode test CSR")
	}
	// CSR ASN.1 ends with the signature; flipping the last byte breaks it.
	tampered := append([]byte(nil), block.Bytes...)
	tampered[len(tampered)-1] ^= 0xFF
	tamperedPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: tampered})
	if _, err := parseCSR(string(tamperedPEM)); err == nil {
		t.Error("parseCSR accepted CSR with tampered signature")
	}
}

// TestReCertRateLimiter_Allow verifies the bucket logic: first 5 requests
// in a window are allowed, the 6th is denied with a non-zero Retry-After,
// and a new bucket starts cleanly after the window elapses.
func TestReCertRateLimiter_Allow(t *testing.T) {
	l := &reCertRateLimiter{buckets: make(map[string]*reCertBucket)}

	for i := 0; i < reCertMaxRequests; i++ {
		ok, retry := l.allow("agent-a")
		if !ok {
			t.Fatalf("request %d/%d denied unexpectedly", i+1, reCertMaxRequests)
		}
		if retry != 0 {
			t.Errorf("request %d had non-zero Retry-After: %v", i+1, retry)
		}
	}
	ok, retry := l.allow("agent-a")
	if ok {
		t.Fatal("request beyond limit was allowed")
	}
	if retry <= 0 || retry > reCertWindow {
		t.Errorf("Retry-After out of bounds: %v (expected 0 < x <= %v)", retry, reCertWindow)
	}
}

// TestReCertRateLimiter_IndependentAgents verifies that one agent hitting its
// limit does not impact another agent's bucket. Critical because a misbehaving
// agent must not be able to deny service to others by exhausting a shared pool.
func TestReCertRateLimiter_IndependentAgents(t *testing.T) {
	l := &reCertRateLimiter{buckets: make(map[string]*reCertBucket)}

	for i := 0; i < reCertMaxRequests; i++ {
		l.allow("noisy-agent")
	}
	if ok, _ := l.allow("noisy-agent"); ok {
		t.Fatal("noisy agent should be over limit")
	}
	if ok, _ := l.allow("quiet-agent"); !ok {
		t.Fatal("quiet agent was incorrectly blocked by noisy-agent's bucket")
	}
}

// TestReCertRateLimiter_WindowReset simulates the window expiring by directly
// rewinding the bucket's windowStart. Verifies that after the window the bucket
// resets cleanly and the agent gets a fresh allowance.
func TestReCertRateLimiter_WindowReset(t *testing.T) {
	l := &reCertRateLimiter{buckets: make(map[string]*reCertBucket)}

	for i := 0; i < reCertMaxRequests; i++ {
		l.allow("agent-b")
	}
	// Rewind the bucket past the window boundary.
	l.mu.Lock()
	l.buckets["agent-b"].windowStart = time.Now().Add(-2 * reCertWindow)
	l.mu.Unlock()

	ok, retry := l.allow("agent-b")
	if !ok {
		t.Fatal("bucket did not reset after window elapsed")
	}
	if retry != 0 {
		t.Errorf("fresh bucket should have zero Retry-After, got %v", retry)
	}
}

// makeTestCSR generates a real ECDSA P-256 keypair and returns a PEM-encoded
// CertificateRequest with the supplied CN. Helper used by parseCSR tests so we
// never carry static test-fixture key material in the repo.
func makeTestCSR(t *testing.T, cn string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	template := x509.CertificateRequest{
		Subject:            pkix.Name{CommonName: cn},
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &template, key)
	if err != nil {
		t.Fatalf("x509.CreateCertificateRequest: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"
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

// NOTE: TestReCertRateLimiter_* tests were removed when H3 moved rate limiting
// from in-process buckets to a DB-backed query against client_certificates.
// The DB-backed version is tested by an integration test that needs a real
// test DB (5 inserted cert rows -> 6th re-cert returns 429) — that test is
// tracked as a follow-up; the existing handler tests cover the pure-logic
// pieces (parseCSR shape validation).

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

package api

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAgentUpdateURLSigRoundtrip(t *testing.T) {
	t.Setenv("INSTALL_URL_SIGNING_KEY", "test-secret-do-not-use-in-prod")

	signed := signAgentUpdateURL("https://example/api/agent/update/download?platform=linux&arch=amd64", "linux", "amd64", "1.77.10")
	if !strings.Contains(signed, "sig=") || !strings.Contains(signed, "exp=") {
		t.Fatalf("expected signed URL to carry sig and exp, got %q", signed)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	if err := verifyAgentUpdateURLSig(u.Query(), "linux", "amd64", "1.77.10", true); err != nil {
		t.Fatalf("verify good URL: %v", err)
	}

	// Tampering with any signed field invalidates the URL.
	if err := verifyAgentUpdateURLSig(u.Query(), "linux", "arm64", "1.77.10", true); err == nil {
		t.Fatal("expected verify to fail on arch tamper")
	}
	if err := verifyAgentUpdateURLSig(u.Query(), "linux", "amd64", "1.77.11", true); err == nil {
		t.Fatal("expected verify to fail on version tamper")
	}
}

func TestAgentUpdateURLSigExpiry(t *testing.T) {
	t.Setenv("INSTALL_URL_SIGNING_KEY", "test-secret")
	// Build a URL with an exp 1s in the past + a valid signature for that exp.
	// Must still be rejected — otherwise the TTL provides no security value.
	expPast := time.Now().Add(-1 * time.Second).Unix()
	sig := computeInstallURLSig("test-secret", "linux", "amd64", "1.77.10", expPast)
	values := url.Values{}
	values.Set("exp", strconv.FormatInt(expPast, 10))
	values.Set("sig", sig)
	if err := verifyAgentUpdateURLSig(values, "linux", "amd64", "1.77.10", true); err == nil {
		t.Fatal("expected expired URL to be rejected even with valid sig")
	}
}

func TestSigningDisabledWithoutSecret(t *testing.T) {
	t.Setenv("INSTALL_URL_SIGNING_KEY", "")
	t.Setenv("JWT_SECRET", "")
	signed := signAgentUpdateURL("https://example/foo?a=1", "linux", "amd64", "1.0.0")
	if strings.Contains(signed, "sig=") {
		t.Fatalf("expected unsigned URL when no secret configured, got %q", signed)
	}
	// requireSig=false → no-op when no secret
	if err := verifyAgentUpdateURLSig(url.Values{}, "linux", "amd64", "1.0.0", false); err != nil {
		t.Fatalf("expected no-op verify when no secret, got %v", err)
	}
}

func TestBootstrapRedeemSigRoundtrip(t *testing.T) {
	t.Setenv("INSTALL_URL_SIGNING_KEY", "test-secret")
	fake := fakeID("00000000-0000-0000-0000-000000000123")
	signed := signBootstrapRedeemURL("https://example", fake)
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := verifyBootstrapRedeemSig(u.Query(), fake); err != nil {
		t.Fatalf("verify good: %v", err)
	}
	// Different link ID under same exp must fail.
	other := fakeID("00000000-0000-0000-0000-000000000999")
	if err := verifyBootstrapRedeemSig(u.Query(), other); err == nil {
		t.Fatal("expected verify to fail with mismatched link id")
	}
}

type fakeID string

func (f fakeID) String() string { return string(f) }

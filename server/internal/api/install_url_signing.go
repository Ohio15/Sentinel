package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// detectRequestScheme returns "https" or "http" for the inbound request,
// preferring X-Forwarded-Proto (set by Traefik / Cloudflare in production) over
// c.Request.TLS (which is nil behind a TLS-terminating proxy). In production
// (SERVER_ENV=production) it never returns "http" — a request that appears
// non-TLS is treated as misconfigured rather than silently leaking a token
// over plaintext.
func detectRequestScheme(c *gin.Context) string {
	if fwd := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); fwd != "" {
		return fwd
	}
	if c.Request.TLS != nil {
		return "https"
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SERVER_ENV")), "production") {
		return "https"
	}
	return "http"
}

// installURLSigningSecret returns the HMAC key for signing transient install
// and update download URLs. Prefers a dedicated INSTALL_URL_SIGNING_KEY, falls
// back to JWT_SECRET so deployments without the dedicated key still function.
// Returns "" only if both are unset — callers MUST handle that and skip
// signing (with a log warning) rather than emit unsigned URLs silently.
func installURLSigningSecret() string {
	if v := strings.TrimSpace(os.Getenv("INSTALL_URL_SIGNING_KEY")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("JWT_SECRET"))
}

// signedURLDefaultTTL is the validity window of a signed agent-update URL.
// Long enough to absorb network/DNS hiccups and a slow large-binary download,
// short enough that a leaked URL from server logs becomes useless quickly.
const signedURLDefaultTTL = 10 * time.Minute

// signAgentUpdateURL appends &exp=&sig= signature parameters to a base download
// URL. The signature covers platform, arch, version, and the expiry — any of
// these being tampered with invalidates the URL. Returns the URL unchanged
// if no signing secret is available (logged once at boot, never silently).
func signAgentUpdateURL(rawURL, platform, arch, version string) string {
	secret := installURLSigningSecret()
	if secret == "" {
		return rawURL
	}
	exp := time.Now().Add(signedURLDefaultTTL).Unix()
	sig := computeInstallURLSig(secret, platform, arch, version, exp)

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := parsed.Query()
	q.Set("exp", strconv.FormatInt(exp, 10))
	q.Set("sig", sig)
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

// verifyAgentUpdateURLSig checks the signature on an incoming download request.
// Returns nil on success or if no signing secret is configured (server-wide
// signing disabled — backward-compatible mode). Returns an error when a sig
// is present but invalid/expired, or when a sig is required but missing.
//
// requireSig controls whether unsigned requests are rejected. Set true once
// the fleet is fully on the new signed-URL flow; set false during rollout.
func verifyAgentUpdateURLSig(values url.Values, platform, arch, version string, requireSig bool) error {
	secret := installURLSigningSecret()
	if secret == "" {
		return nil // signing disabled server-wide
	}

	expStr := values.Get("exp")
	gotSig := values.Get("sig")
	if expStr == "" || gotSig == "" {
		if requireSig {
			return errors.New("signed URL required (missing exp or sig)")
		}
		return nil
	}

	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid exp parameter: %w", err)
	}
	if time.Now().Unix() > exp {
		return errors.New("signed URL expired")
	}

	want := computeInstallURLSig(secret, platform, arch, version, exp)
	// Constant-time compare to prevent timing oracle on the HMAC tag.
	if !hmac.Equal([]byte(want), []byte(gotSig)) {
		return errors.New("signed URL signature mismatch")
	}
	return nil
}

// computeInstallURLSig produces the canonical HMAC-SHA256 of the URL fields.
// The signed payload format is intentionally simple ("platform|arch|version|exp")
// — adding new fields here is a breaking change for in-flight URLs and should
// be done with care.
func computeInstallURLSig(secret, platform, arch, version string, exp int64) string {
	payload := fmt.Sprintf("%s|%s|%s|%d", platform, arch, version, exp)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// bootstrapRedeemTTL is the validity window of a signed bootstrap-redeem URL.
// Shorter than the agent-update window because the installer follows it
// immediately after a validate-code call.
const bootstrapRedeemTTL = 5 * time.Minute

// signBootstrapRedeemURL produces a single-use URL the installer GETs to fetch
// its enrollment token after validate-code. The signature is over the link UUID
// and expiry so the URL cannot be transplanted to a different installation
// link. The link's status row is the single-use guard (atomic UPDATE in
// redeemInstallCodeHandler).
func signBootstrapRedeemURL(serverURL string, linkID interface{ String() string }) string {
	secret := installURLSigningSecret()
	if secret == "" {
		return ""
	}
	exp := time.Now().Add(bootstrapRedeemTTL).Unix()
	sig := computeBootstrapRedeemSig(secret, linkID.String(), exp)
	return fmt.Sprintf("%s/api/public/install/redeem?link=%s&exp=%d&sig=%s",
		strings.TrimRight(serverURL, "/"), url.QueryEscape(linkID.String()), exp, sig)
}

// verifyBootstrapRedeemSig validates the signature on an incoming redeem URL.
// Returns an error if the signature is missing, expired, or doesn't match.
func verifyBootstrapRedeemSig(values url.Values, linkID interface{ String() string }) error {
	secret := installURLSigningSecret()
	if secret == "" {
		return errors.New("server has no install URL signing key configured")
	}
	expStr := values.Get("exp")
	gotSig := values.Get("sig")
	if expStr == "" || gotSig == "" {
		return errors.New("missing exp or sig")
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid exp: %w", err)
	}
	if time.Now().Unix() > exp {
		return errors.New("redeem URL expired")
	}
	want := computeBootstrapRedeemSig(secret, linkID.String(), exp)
	if !hmac.Equal([]byte(want), []byte(gotSig)) {
		return errors.New("signature mismatch")
	}
	return nil
}

func computeBootstrapRedeemSig(secret, linkID string, exp int64) string {
	payload := fmt.Sprintf("redeem|%s|%d", linkID, exp)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

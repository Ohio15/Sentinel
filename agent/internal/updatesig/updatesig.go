// Package updatesig provides authenticity verification for agent/watchdog
// update artifacts using Ed25519 detached signatures.
//
// Trust anchor: the Ed25519 PUBLIC key is embedded into the binary at build
// time via -ldflags "-X ...updatesig.SigningPublicKeyHex=<hex>". It is NEVER
// supplied by the server or the network. Every update path verifies the
// detached signature over the exact bytes it downloaded, immediately before the
// binary is swapped and executed. This makes the artifact self-authenticating
// and independent of the transport channel (RW-1 / closes AG-C1, AG-C2, AG-H1,
// AG-H4, AG-H5, WD-H2, WD-H3, WD-H4).
//
// Fail-closed: a build with no embedded public key CANNOT self-update. An empty
// or malformed signature is rejected. There is no self-computed-checksum
// fallback and no "unsigned is OK" path.
package updatesig

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// SigningPublicKeyHex is the hex-encoded 32-byte Ed25519 public key used to
// verify update artifacts. It is injected at build time by the release
// pipeline:
//
//	-ldflags "-X github.com/sentinel/agent/internal/updatesig.SigningPublicKeyHex=<hex>"
//
// The default is intentionally empty. An empty value means this binary was not
// produced by the signed-release pipeline and therefore MUST NOT self-update
// (see Verify, which fails closed).
var SigningPublicKeyHex string

// ErrNoEmbeddedKey is returned when the binary has no embedded signing public
// key. Callers must treat this as a hard failure — an unsigned build is not
// permitted to self-update.
var ErrNoEmbeddedKey = errors.New("updatesig: no signing public key embedded in this build; refusing to self-update (fail closed)")

// ErrEmptySignature is returned when no signature accompanies the artifact.
var ErrEmptySignature = errors.New("updatesig: empty signature; refusing to apply unsigned update")

// ErrVerificationFailed is returned when the signature does not match the data
// under the embedded public key.
var ErrVerificationFailed = errors.New("updatesig: signature verification failed")

// PublicKey decodes and validates the embedded signing public key.
func PublicKey() (ed25519.PublicKey, error) {
	hexKey := strings.TrimSpace(SigningPublicKeyHex)
	if hexKey == "" {
		return nil, ErrNoEmbeddedKey
	}
	raw, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("updatesig: embedded public key is not valid hex: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("updatesig: embedded public key must be %d bytes, got %d", ed25519.PublicKeySize, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// Verify checks that sigB64 is a valid Ed25519 detached signature over data,
// produced by the private key corresponding to the embedded public key.
//
// It fails closed:
//   - no embedded public key            -> ErrNoEmbeddedKey
//   - empty/whitespace signature         -> ErrEmptySignature
//   - malformed base64 / wrong length    -> descriptive error
//   - cryptographic mismatch             -> ErrVerificationFailed
func Verify(data []byte, sigB64 string) error {
	pub, err := PublicKey()
	if err != nil {
		return err
	}

	sigB64 = strings.TrimSpace(sigB64)
	if sigB64 == "" {
		return ErrEmptySignature
	}

	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("updatesig: signature is not valid base64: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("updatesig: signature must be %d bytes, got %d", ed25519.SignatureSize, len(sig))
	}

	if !ed25519.Verify(pub, data, sig) {
		return ErrVerificationFailed
	}
	return nil
}

// ParseVersion parses a strict "MAJOR.MINOR.PATCH" semver into a [3]int. An
// optional single leading 'v' is tolerated. Any other form (missing component,
// extra component, non-numeric, empty, pre-release/build metadata) is rejected
// so anti-rollback logic can never be fooled by an unparseable version.
func ParseVersion(s string) ([3]int, error) {
	var out [3]int
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return out, errors.New("updatesig: empty version string")
	}

	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("updatesig: version %q is not strict MAJOR.MINOR.PATCH", s)
	}
	for i, p := range parts {
		if p == "" {
			return out, fmt.Errorf("updatesig: version %q has an empty component", s)
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, fmt.Errorf("updatesig: version %q component %q is not numeric: %w", s, p, err)
		}
		if n < 0 {
			return out, fmt.Errorf("updatesig: version %q component %q is negative", s, p)
		}
		out[i] = n
	}
	return out, nil
}

// CompareVersions returns -1 if a < b, 0 if a == b, and +1 if a > b. It returns
// an error if either version is not strict semver.
func CompareVersions(a, b string) (int, error) {
	av, err := ParseVersion(a)
	if err != nil {
		return 0, err
	}
	bv, err := ParseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 3; i++ {
		switch {
		case av[i] < bv[i]:
			return -1, nil
		case av[i] > bv[i]:
			return 1, nil
		}
	}
	return 0, nil
}

// IsUpgrade reports whether target is strictly greater than current under
// strict semver. If either version fails to parse, it returns false — an
// unparseable version is never treated as an upgrade (anti-rollback fails
// closed). This is the anti-rollback gate for AG-H4.
func IsUpgrade(current, target string) bool {
	cmp, err := CompareVersions(target, current)
	if err != nil {
		return false
	}
	return cmp > 0
}

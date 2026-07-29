package updatesig

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

// withEmbeddedKey sets the package-level embedded public key for the duration
// of a test and restores the prior value afterward.
func withEmbeddedKey(t *testing.T, hexKey string) {
	t.Helper()
	prev := SigningPublicKeyHex
	SigningPublicKeyHex = hexKey
	t.Cleanup(func() { SigningPublicKeyHex = prev })
}

// genKeypair produces a fresh Ed25519 keypair for the test and returns the
// hex-encoded public key plus the private key for signing.
func genKeypair(t *testing.T) (pubHex string, priv ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}
	return hex.EncodeToString(pub), priv
}

func sign(t *testing.T, priv ed25519.PrivateKey, data []byte) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data))
}

func TestVerify_ValidSignaturePasses(t *testing.T) {
	pubHex, priv := genKeypair(t)
	withEmbeddedKey(t, pubHex)

	data := []byte("this is the update binary content")
	sig := sign(t, priv, data)

	if err := Verify(data, sig); err != nil {
		t.Fatalf("expected valid signature to pass, got: %v", err)
	}
}

func TestVerify_TamperedBytesFail(t *testing.T) {
	pubHex, priv := genKeypair(t)
	withEmbeddedKey(t, pubHex)

	data := []byte("original binary bytes")
	sig := sign(t, priv, data)

	tampered := append([]byte{}, data...)
	tampered[0] ^= 0xFF

	if err := Verify(tampered, sig); err == nil {
		t.Fatal("expected tampered bytes to fail verification")
	}
}

func TestVerify_EmptySignatureFails(t *testing.T) {
	pubHex, _ := genKeypair(t)
	withEmbeddedKey(t, pubHex)

	if err := Verify([]byte("data"), ""); err == nil {
		t.Fatal("expected empty signature to fail")
	}
	if err := Verify([]byte("data"), "   "); err == nil {
		t.Fatal("expected whitespace-only signature to fail")
	}
}

func TestVerify_EmptyPublicKeyFailsClosed(t *testing.T) {
	withEmbeddedKey(t, "")

	// Even with a syntactically valid signature, an unsigned build must refuse.
	_, priv := genKeypair(t)
	data := []byte("data")
	sig := sign(t, priv, data)

	err := Verify(data, sig)
	if err == nil {
		t.Fatal("expected fail-closed when no public key is embedded")
	}
	if err != ErrNoEmbeddedKey {
		t.Fatalf("expected ErrNoEmbeddedKey, got: %v", err)
	}
}

func TestVerify_WrongKeyFails(t *testing.T) {
	pubHex, _ := genKeypair(t)
	withEmbeddedKey(t, pubHex)

	// Sign with a DIFFERENT private key than the embedded public key.
	_, otherPriv := genKeypair(t)
	data := []byte("data signed by a foreign key")
	sig := sign(t, otherPriv, data)

	if err := Verify(data, sig); err == nil {
		t.Fatal("expected signature from a foreign key to fail")
	}
}

func TestVerify_MalformedInputs(t *testing.T) {
	pubHex, _ := genKeypair(t)

	// Malformed base64 signature.
	withEmbeddedKey(t, pubHex)
	if err := Verify([]byte("data"), "!!!not-base64!!!"); err == nil {
		t.Fatal("expected malformed base64 signature to fail")
	}

	// Correct base64 but wrong signature length.
	shortSig := base64.StdEncoding.EncodeToString([]byte("too short"))
	if err := Verify([]byte("data"), shortSig); err == nil {
		t.Fatal("expected wrong-length signature to fail")
	}

	// Malformed embedded public key hex.
	withEmbeddedKey(t, "zzzz")
	if err := Verify([]byte("data"), base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))); err == nil {
		t.Fatal("expected malformed public key hex to fail")
	}

	// Correct hex but wrong public key length.
	withEmbeddedKey(t, hex.EncodeToString([]byte("short-key")))
	if err := Verify([]byte("data"), base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))); err == nil {
		t.Fatal("expected wrong-length public key to fail")
	}
}

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in      string
		want    [3]int
		wantErr bool
	}{
		{"1.2.3", [3]int{1, 2, 3}, false},
		{"v1.77.39", [3]int{1, 77, 39}, false},
		{"0.0.0", [3]int{0, 0, 0}, false},
		{"  1.2.3  ", [3]int{1, 2, 3}, false},
		{"1.2", [3]int{}, true},
		{"1.2.3.4", [3]int{}, true},
		{"1.2.x", [3]int{}, true},
		{"1.2.3-rc1", [3]int{}, true},
		{"", [3]int{}, true},
		{"abc", [3]int{}, true},
		{"1..3", [3]int{}, true},
	}
	for _, c := range cases {
		got, err := ParseVersion(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseVersion(%q): expected error, got %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseVersion(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseVersion(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestIsUpgrade(t *testing.T) {
	cases := []struct {
		current, target string
		want            bool
	}{
		{"1.2.3", "1.2.4", true},
		{"1.2.3", "1.3.0", true},
		{"1.2.3", "2.0.0", true},
		{"1.2.3", "1.2.3", false}, // equal is not an upgrade
		{"1.2.3", "1.2.2", false}, // downgrade rejected
		{"1.2.3", "1.1.9", false}, // downgrade rejected
		{"2.0.0", "1.99.99", false},
		{"1.2.3", "garbage", false}, // unparseable target fails closed
		{"garbage", "1.2.3", false}, // unparseable current fails closed
	}
	for _, c := range cases {
		if got := IsUpgrade(c.current, c.target); got != c.want {
			t.Errorf("IsUpgrade(%q, %q) = %v, want %v", c.current, c.target, got, c.want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cmp, err := CompareVersions("1.2.3", "1.2.4")
	if err != nil || cmp != -1 {
		t.Fatalf("expected -1, got %d err=%v", cmp, err)
	}
	cmp, err = CompareVersions("1.2.4", "1.2.3")
	if err != nil || cmp != 1 {
		t.Fatalf("expected 1, got %d err=%v", cmp, err)
	}
	cmp, err = CompareVersions("1.2.3", "1.2.3")
	if err != nil || cmp != 0 {
		t.Fatalf("expected 0, got %d err=%v", cmp, err)
	}
	if _, err := CompareVersions("bad", "1.2.3"); err == nil {
		t.Fatal("expected error for unparseable version")
	}
}

// Command sign produces an Ed25519 signature over the CANONICAL UPDATE MANIFEST
// for a binary, using the release signing private key. It is invoked by the
// release pipeline (scripts/release.ps1) once per built binary.
//
// The manifest binds the binary's identity (version, platform, arch, sha256) and
// the downgrade authorization together under one signature, so none of those
// fields can be tampered with independently of the bytes. The signed value is
// exactly updatesig.CanonicalManifest(...) — imported here so the signer and all
// verifiers share one definition and can never drift.
//
// The private key is NEVER committed. It is read at signing time from the PEM
// file referenced by the SENTINEL_UPDATE_SIGNING_KEY environment variable (an
// Ed25519 private key in PKCS#8 PEM form). Output is a JSON sidecar written next
// to the binary as "<file>.manifest.json":
//
//	{"version","platform","arch","sha256","signedDowngrade","signature"}
//
// Usage:
//
//	SENTINEL_UPDATE_SIGNING_KEY=/path/to/key.pem \
//	  sign -version 1.77.40 -platform windows -arch amd64 [-signed-downgrade] \
//	       [-out <path>] <file>
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/sentinel/agent/internal/updatesig"
)

const signingKeyEnv = "SENTINEL_UPDATE_SIGNING_KEY"

// manifestSidecar mirrors the server's BinaryManifest and the fields the client
// rebuilds for verification.
type manifestSidecar struct {
	Version         string `json:"version"`
	Platform        string `json:"platform"`
	Arch            string `json:"arch"`
	SHA256          string `json:"sha256"`
	SignedDowngrade bool   `json:"signedDowngrade"`
	Signature       string `json:"signature"`
}

func main() {
	version := flag.String("version", "", "artifact version (strict semver, e.g. 1.77.40)")
	platform := flag.String("platform", "", "artifact platform (e.g. windows, linux, darwin)")
	arch := flag.String("arch", "", "artifact arch (e.g. amd64, arm64)")
	signedDowngrade := flag.Bool("signed-downgrade", false, "authorize this artifact as a signed downgrade (non-upgrade target)")
	out := flag.String("out", "", "explicit sidecar output path (default: <file>.manifest.json)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s -version V -platform P -arch A [-signed-downgrade] [-out <path>] <file>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  requires %s to point at an Ed25519 private key in PEM (PKCS#8) form\n", signingKeyEnv)
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 || *version == "" || *platform == "" || *arch == "" {
		flag.Usage()
		os.Exit(2)
	}
	target := flag.Arg(0)

	priv, err := loadSigningKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign: %v\n", err)
		os.Exit(1)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign: failed to read %q: %v\n", target, err)
		os.Exit(1)
	}

	sum := sha256.Sum256(data)
	sha256hex := hex.EncodeToString(sum[:])

	// Sign the exact canonical manifest the verifiers rebuild.
	manifest := updatesig.CanonicalManifest(*version, *platform, *arch, sha256hex, *signedDowngrade)
	sigB64 := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(manifest)))

	// Print the signature to stdout so the caller can capture it (version.json).
	fmt.Println(sigB64)

	sidecarPath := *out
	if sidecarPath == "" {
		sidecarPath = target + ".manifest.json"
	}
	sidecar := manifestSidecar{
		Version:         *version,
		Platform:        *platform,
		Arch:            *arch,
		SHA256:          sha256hex,
		SignedDowngrade: *signedDowngrade,
		Signature:       sigB64,
	}
	encoded, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign: failed to encode manifest: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(sidecarPath, append(encoded, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "sign: failed to write sidecar %q: %v\n", sidecarPath, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "sign: wrote %s\n", sidecarPath)
}

// loadSigningKey reads and parses the Ed25519 private key referenced by the
// SENTINEL_UPDATE_SIGNING_KEY environment variable. It accepts a PKCS#8 PEM
// ("PRIVATE KEY") and validates that the key is Ed25519.
func loadSigningKey() (ed25519.PrivateKey, error) {
	keyPath := os.Getenv(signingKeyEnv)
	if keyPath == "" {
		return nil, fmt.Errorf("%s is not set; refusing to sign without a signing key", signingKeyEnv)
	}

	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read signing key %q: %w", keyPath, err)
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("signing key %q is not valid PEM", keyPath)
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKCS#8 private key: %w", err)
	}

	priv, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("signing key is not an Ed25519 private key")
	}
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("Ed25519 private key has wrong size: %d", len(priv))
	}
	return priv, nil
}

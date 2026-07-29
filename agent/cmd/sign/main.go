// Command sign produces an Ed25519 detached signature over a file, using the
// release signing private key. It is invoked by the release pipeline
// (scripts/release.ps1) once per built binary.
//
// The private key is NEVER committed. It is read at signing time from the PEM
// file referenced by the SENTINEL_UPDATE_SIGNING_KEY environment variable
// (an Ed25519 private key in PKCS#8 PEM form). The signature is emitted as
// standard-base64 to stdout and, by default, written to a sidecar "<file>.sig"
// that the update server serves alongside the binary.
//
// Usage:
//
//	SENTINEL_UPDATE_SIGNING_KEY=/path/to/key.pem sign [-no-sidecar] [-out <path>] <file>
//
// The signature format is byte-for-byte identical to what
// internal/updatesig.Verify expects: base64(ed25519.Sign(priv, rawFileBytes)).
package main

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
)

const signingKeyEnv = "SENTINEL_UPDATE_SIGNING_KEY"

func main() {
	noSidecar := flag.Bool("no-sidecar", false, "do not write a <file>.sig sidecar; only print the signature to stdout")
	out := flag.String("out", "", "explicit sidecar output path (default: <file>.sig)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [-no-sidecar] [-out <path>] <file>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  requires %s to point at an Ed25519 private key in PEM (PKCS#8) form\n", signingKeyEnv)
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
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

	sig := ed25519.Sign(priv, data)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	// Print to stdout so the caller can capture it (e.g. into version.json).
	fmt.Println(sigB64)

	if !*noSidecar {
		sidecar := *out
		if sidecar == "" {
			sidecar = target + ".sig"
		}
		if err := os.WriteFile(sidecar, []byte(sigB64+"\n"), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "sign: failed to write sidecar %q: %v\n", sidecar, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "sign: wrote %s\n", sidecar)
	}
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

// Command verify checks a downloaded binary against its signed manifest sidecar
// and exits 0 only if the manifest signature validates against the embedded
// public key AND the binary's sha256 matches the manifest. It is the
// authenticity gate for the Layer-4 bootstrap-recovery scripts (H1), which
// otherwise download+exec a binary as SYSTEM/root with only a size check.
//
// The public key is embedded at build time via the SAME ldflags the agent and
// watchdog use, so a build with no embedded key can never verify anything:
//
//	-ldflags "-X github.com/sentinel/agent/internal/updatesig.SigningPublicKeyHex=<hex>"
//
// Usage:
//
//	verify -binary <path> -manifest <path.manifest.json>
//
// Exit codes: 0 = verified; non-zero = reject (bootstrap MUST NOT copy/exec).
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sentinel/agent/internal/updatesig"
)

type manifestSidecar struct {
	Version         string `json:"version"`
	Platform        string `json:"platform"`
	Arch            string `json:"arch"`
	SHA256          string `json:"sha256"`
	SignedDowngrade bool   `json:"signedDowngrade"`
	Signature       string `json:"signature"`
}

func main() {
	binaryPath := flag.String("binary", "", "path to the downloaded binary to verify")
	manifestPath := flag.String("manifest", "", "path to the signed <binary>.manifest.json sidecar")
	flag.Parse()

	if *binaryPath == "" || *manifestPath == "" {
		fmt.Fprintf(os.Stderr, "usage: %s -binary <path> -manifest <path.manifest.json>\n", os.Args[0])
		os.Exit(2)
	}

	if err := run(*binaryPath, *manifestPath); err != nil {
		fmt.Fprintf(os.Stderr, "verify: REJECT: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "verify: OK")
	os.Exit(0)
}

func run(binaryPath, manifestPath string) error {
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}
	var m manifestSidecar
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}
	if strings.TrimSpace(m.Signature) == "" {
		return fmt.Errorf("manifest has no signature")
	}

	data, err := os.ReadFile(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to read binary: %w", err)
	}
	sum := sha256.Sum256(data)
	sha256hex := hex.EncodeToString(sum[:])

	// The binary bytes must match the sha256 the manifest claims (which is itself
	// covered by the signature). Reject on any mismatch.
	if !strings.EqualFold(sha256hex, m.SHA256) {
		return fmt.Errorf("sha256 mismatch: binary=%s manifest=%s", sha256hex, m.SHA256)
	}

	// Verify the manifest signature over the LOCALLY computed sha256 against the
	// embedded public key. Fails closed if no key is embedded.
	if err := updatesig.VerifyManifest(m.Version, m.Platform, m.Arch, sha256hex, m.SignedDowngrade, m.Signature); err != nil {
		return fmt.Errorf("manifest signature verification failed: %w", err)
	}
	return nil
}

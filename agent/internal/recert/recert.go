// Package recert performs in-place mTLS certificate rotation for an
// already-enrolled Sentinel agent.
//
// Rotation is triggered either by the --re-cert CLI flag or by the
// presence of a marker file at <DataDir>/.re-cert-pending (e.g.
// C:\ProgramData\Sentinel\.re-cert-pending). The marker is normally
// dropped by the Windows installer when a reinstall is performed on a
// machine that already has an enrolled agent.
//
// The rotation procedure:
//
//  1. Read encrypted config (server_url, agent_id) and existing
//     client.crt / client.key / ca-cert.pem.
//  2. POST {server}/api/agent/re-cert over mTLS with an empty body
//     (server generates the new key in v1).
//  3. Atomically replace the three cert files, keeping the last 3
//     timestamped .bak copies of each.
//  4. Refresh agent-info.json's LastSeen.
//  5. Delete the marker on success.
//
// Error classification:
//
//   - ErrDeviceNotFound (HTTP 404)  -> orphaned agent, marker deleted,
//     caller proceeds to heartbeat (which will keep failing — by design).
//   - ErrUnauthorized  (HTTP 401)   -> our cert is no longer valid for
//     this device, marker deleted, caller proceeds.
//   - ErrRetryable     (5xx, network) -> marker kept so the next agent
//     restart can try again. Caller should NOT delete the marker.
//
// Concurrency: rotation runs synchronously before any heartbeat loops
// start. There is no shared state to protect — callers MUST sequence
// rotation -> heartbeat, not run them in parallel.
package recert

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sentinel/agent/internal/crypto"
	"github.com/sentinel/agent/internal/ipc"
	"github.com/sentinel/agent/internal/paths"
)

// MarkerFileName is the name of the file that, when present in the data
// directory, signals the agent to rotate its cert on startup.
const MarkerFileName = ".re-cert-pending"

// MaxBackupsPerFile is the number of timestamped .bak.<ts> copies of each
// cert file that are retained after a successful rotation. Older backups
// are pruned to bound disk usage.
const MaxBackupsPerFile = 3

// reCertPath is the HTTP path appended to the mTLS server URL for the
// rotation endpoint. The server team must mount the handler at this
// exact path.
const reCertPath = "/api/agent/re-cert"

// requestTimeout bounds a single rotation HTTP call. Network failures
// and server-side stalls return ErrRetryable so the next agent restart
// can try again.
const requestTimeout = 30 * time.Second

// Sentinel errors returned by Rotate so callers can decide whether to
// delete the marker (terminal) or keep it (retryable).
var (
	// ErrDeviceNotFound is returned when the server replies 404 — the
	// device row backing our cert no longer exists. The agent is
	// orphaned and cannot recover via re-cert.
	ErrDeviceNotFound = errors.New("re-cert: server reports device not found")

	// ErrUnauthorized is returned for HTTP 401 — the cert we presented
	// is not accepted for this device anymore.
	ErrUnauthorized = errors.New("re-cert: server rejected our certificate")

	// ErrRetryable is wrapped around any error where the rotation may
	// succeed on a future attempt (network errors, 5xx, partial writes
	// recovered from backup). Callers should leave the marker in place.
	ErrRetryable = errors.New("re-cert: retryable failure")

	// ErrMissingIdentity is returned when the on-disk state is
	// incomplete (no config, no cert, no key, or no CA). Treated as
	// retryable — admin intervention may be needed but the next
	// startup will try again rather than silently moving on.
	ErrMissingIdentity = errors.New("re-cert: agent identity files are missing")
)

// MarkerPath returns the absolute path of the rotation marker file,
// e.g. C:\ProgramData\Sentinel\.re-cert-pending on Windows.
func MarkerPath() string {
	return filepath.Join(paths.DataDir(), MarkerFileName)
}

// MarkerPresent reports whether the marker file currently exists on disk.
// Read errors other than "not exist" are treated as "present" so the
// agent rotates conservatively (re-enrolling on a permission error is
// safer than silently skipping it).
func MarkerPresent() bool {
	_, err := os.Stat(MarkerPath())
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	log.Printf("[ReCert] Warning: marker stat failed, treating as present: %v", err)
	return true
}

// DeleteMarker removes the marker file. Missing-file errors are ignored
// so callers can use this idempotently.
func DeleteMarker() error {
	err := os.Remove(MarkerPath())
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return fmt.Errorf("failed to remove re-cert marker %s: %w", MarkerPath(), err)
}

// Response is the JSON body returned by the server's re-cert endpoint.
// Field names match the spec: cert/key/ca_cert/issued_at/expires_at.
type Response struct {
	Cert      string `json:"cert"`
	Key       string `json:"key"`
	CACert    string `json:"ca_cert"`
	IssuedAt  string `json:"issued_at"`
	ExpiresAt string `json:"expires_at"`
}

// errorResponse is the JSON shape returned by the server on 4xx.
type errorResponse struct {
	Error string `json:"error"`
}

// Options controls Rotate's behavior. Production callers can use
// DefaultOptions(); tests inject a custom ServerURL/HTTPClient to
// avoid hitting a real server or shipping a real config on disk.
type Options struct {
	// ServerURL, when non-empty, overrides the URL read from the
	// encrypted config. Used by tests pointing at httptest servers.
	ServerURL string

	// HTTPClient, when non-nil, is used instead of building a new
	// mTLS-enabled client from the on-disk certs. Tests use this with
	// a client that points at an httptest TLS server.
	HTTPClient *http.Client

	// Now is the clock used for backup timestamps. Tests override this
	// to get deterministic filenames.
	Now func() time.Time
}

// DefaultOptions returns the options used for real rotations: no URL
// override, no client override, wall-clock time.
func DefaultOptions() Options {
	return Options{Now: time.Now}
}

// Rotate performs the full re-cert sequence. On terminal errors
// (device not found, unauthorized) the marker is deleted before
// returning. On retryable errors the marker is left in place.
//
// The boolean return is markerDeleted, which the caller can use to
// distinguish "we cleaned up" from "we left it for next time".
func Rotate(ctx context.Context, opts Options) (markerDeleted bool, err error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}

	log.Println("[ReCert] Starting in-place certificate rotation")

	// 1. Load existing identity.
	ident, err := loadIdentity(opts.ServerURL)
	if err != nil {
		// Missing identity is retryable in the sense that we DO NOT
		// delete the marker — but we surface the error so the caller
		// can log it. The next service restart will try again, and if
		// the files are still missing the admin needs to intervene.
		log.Printf("[ReCert] %v", err)
		return false, err
	}

	// 2. Build mTLS HTTP client (or use the injected one for tests).
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient, err = newMTLSClient(ident)
		if err != nil {
			log.Printf("[ReCert] Failed to build mTLS client: %v", err)
			return false, fmt.Errorf("%w: %v", ErrRetryable, err)
		}
	}

	// 3. Make the re-cert call.
	resp, err := callReCert(ctx, httpClient, ident.ServerURL)
	if err != nil {
		// Classify the error to decide marker fate.
		if errors.Is(err, ErrDeviceNotFound) || errors.Is(err, ErrUnauthorized) {
			// Terminal: server has explicitly rejected us. Drop the
			// marker so we don't churn on every restart.
			if delErr := DeleteMarker(); delErr != nil {
				log.Printf("[ReCert] Warning: failed to delete marker after terminal error: %v", delErr)
			}
			return true, err
		}
		// Retryable: keep marker.
		return false, err
	}

	// 4. Validate the response before touching disk.
	if err := validateResponse(resp); err != nil {
		log.Printf("[ReCert] Server response invalid: %v", err)
		return false, fmt.Errorf("%w: %v", ErrRetryable, err)
	}

	// 5. Atomically replace cert files, backing up old ones.
	if err := installNewCerts(resp, opts.Now()); err != nil {
		log.Printf("[ReCert] Failed to install new certificates: %v", err)
		return false, fmt.Errorf("%w: %v", ErrRetryable, err)
	}

	// 6. Bump agent-info.json's LastSeen so the watchdog sees fresh
	// activity after rotation. Non-fatal if it fails.
	if err := bumpAgentInfo(ident.AgentID); err != nil {
		log.Printf("[ReCert] Warning: failed to refresh agent-info.json: %v", err)
	}

	// 7. Delete the marker — only on full success.
	if err := DeleteMarker(); err != nil {
		log.Printf("[ReCert] Warning: rotation succeeded but marker delete failed: %v", err)
		// Don't fail the rotation just because the marker file is
		// stuck; the next start will just rotate again, which is
		// wasteful but not harmful.
	}

	log.Printf("[ReCert] Rotation complete: issued=%s expires=%s", resp.IssuedAt, resp.ExpiresAt)
	return true, nil
}

// identity bundles everything Rotate loads from disk before touching
// the network. Keeping it in one place makes the mTLS client builder
// trivial and the test seam obvious.
type identity struct {
	ServerURL string
	AgentID   string
	ClientCrt []byte
	ClientKey []byte
	CACert    []byte
}

// loadIdentity reads the encrypted config + the three cert files. If
// serverURLOverride is non-empty it is used in place of the config
// value (test seam).
func loadIdentity(serverURLOverride string) (*identity, error) {
	ident := &identity{ServerURL: serverURLOverride}

	// Config (encrypted). Skip if caller supplied an override AND the
	// caller is a test — but in production we always read config so we
	// get the agent_id. We always read it; the override only replaces
	// ServerURL.
	configPath := paths.ConfigPath()
	cfgData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot read config %s: %v", ErrMissingIdentity, configPath, err)
	}

	var plaintext []byte
	if crypto.IsEncrypted(cfgData) {
		plaintext, err = crypto.DecryptConfig(cfgData)
		if err != nil {
			return nil, fmt.Errorf("%w: cannot decrypt config: %v", ErrMissingIdentity, err)
		}
	} else {
		plaintext = cfgData
	}

	// Decode just the fields we need (server_url, agent_id) — we don't
	// want a hard dep on the full config struct so tests can write a
	// minimal config blob.
	var partial struct {
		AgentID   string `json:"agent_id"`
		ServerURL string `json:"server_url"`
	}
	if err := json.Unmarshal(plaintext, &partial); err != nil {
		return nil, fmt.Errorf("%w: cannot parse config: %v", ErrMissingIdentity, err)
	}
	if partial.AgentID == "" {
		return nil, fmt.Errorf("%w: config has no agent_id", ErrMissingIdentity)
	}
	if ident.ServerURL == "" {
		ident.ServerURL = partial.ServerURL
	}
	if ident.ServerURL == "" {
		return nil, fmt.Errorf("%w: config has no server_url", ErrMissingIdentity)
	}
	ident.AgentID = partial.AgentID

	// Cert files. We read all three up front so a missing one fails
	// fast before we touch the network.
	ident.ClientCrt, err = os.ReadFile(paths.ClientCertPath())
	if err != nil {
		return nil, fmt.Errorf("%w: cannot read client cert: %v", ErrMissingIdentity, err)
	}
	sealedKey, err := os.ReadFile(paths.ClientKeyPath())
	if err != nil {
		return nil, fmt.Errorf("%w: cannot read client key: %v", ErrMissingIdentity, err)
	}
	// Unseal the key if it was DPAPI-sealed at rest (Windows). A legacy
	// plaintext key is returned unchanged so pre-sealing installs still rotate.
	ident.ClientKey, err = crypto.UnsealMachineData(sealedKey)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot unseal client key: %v", ErrMissingIdentity, err)
	}
	ident.CACert, err = os.ReadFile(paths.CACertPath())
	if err != nil {
		return nil, fmt.Errorf("%w: cannot read CA cert: %v", ErrMissingIdentity, err)
	}

	return ident, nil
}

// newMTLSClient constructs an http.Client that presents the agent's
// current client cert and trusts the CA bundle on disk. We deliberately
// do NOT touch the cached config in internal/mtls — that cache would
// hold the OLD cert until the process restarts, which is fine for the
// heartbeat loop that starts after Rotate returns, but during Rotate
// itself we want a clean, scoped client.
func newMTLSClient(ident *identity) (*http.Client, error) {
	cert, err := tls.X509KeyPair(ident.ClientCrt, ident.ClientKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse client keypair: %w", err)
	}

	// H5 (qa-butcher): pin to the Sentinel CA only. The system trust store is
	// an attack surface here — the agent only ever talks to a Sentinel server
	// signed by the Sentinel CA, and including system roots would let any
	// compromised / rogue CA in the OS trust store intercept the cert rotation
	// channel.
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(ident.CACert) {
		return nil, fmt.Errorf("failed to append CA cert to pool")
	}

	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
	}

	return &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
		},
	}, nil
}

// callReCert sends the rotation request and returns either a parsed
// Response or one of the classified sentinel errors.
func callReCert(ctx context.Context, client *http.Client, serverURL string) (*Response, error) {
	endpoint, err := buildReCertURL(serverURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRetryable, err)
	}

	log.Printf("[ReCert] POST %s", endpoint)

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	body := bytes.NewReader([]byte("{}"))
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrRetryable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	httpResp, err := client.Do(req)
	if err != nil {
		// Network errors, TLS handshake failures, context-deadline —
		// all retryable.
		return nil, fmt.Errorf("%w: HTTP call: %v", ErrRetryable, err)
	}
	defer httpResp.Body.Close()

	respBody, readErr := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20)) // 1MB cap
	if readErr != nil {
		return nil, fmt.Errorf("%w: read response: %v", ErrRetryable, readErr)
	}

	switch httpResp.StatusCode {
	case http.StatusOK:
		var parsed Response
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return nil, fmt.Errorf("%w: malformed JSON: %v", ErrRetryable, err)
		}
		return &parsed, nil

	case http.StatusNotFound:
		log.Printf("[ReCert] Server reports our device row is missing. Cannot recover via re-cert. Admin must re-enroll via a fresh install code. body=%s", truncateForLog(respBody))
		return nil, ErrDeviceNotFound

	case http.StatusUnauthorized:
		log.Printf("[ReCert] Server rejected our certificate (401). Cannot recover via re-cert; admin must re-enroll. body=%s", truncateForLog(respBody))
		return nil, ErrUnauthorized

	default:
		if httpResp.StatusCode >= 500 {
			return nil, fmt.Errorf("%w: server status %d: %s", ErrRetryable, httpResp.StatusCode, truncateForLog(respBody))
		}
		// Other 4xx: treat as retryable so a fix on the server side
		// (e.g. a 429 rate limit lifting, a 400 schema mismatch being
		// patched) heals on next restart rather than orphaning the
		// agent.
		return nil, fmt.Errorf("%w: server status %d: %s", ErrRetryable, httpResp.StatusCode, truncateForLog(respBody))
	}
}

// buildReCertURL takes the configured server URL (which may be a ws://,
// wss://, http://, or https:// URL, possibly with a websocket path) and
// returns the absolute URL of the re-cert endpoint. This mirrors the
// transformation done in main.requestCertRenewal so re-cert traffic
// goes to the same mTLS-capable port (8443) as cert renewal.
func buildReCertURL(serverURL string) (string, error) {
	if serverURL == "" {
		return "", fmt.Errorf("server URL is empty")
	}

	// Normalize ws/wss to http/https — the re-cert endpoint is HTTP, not WS.
	normalized := serverURL
	switch {
	case strings.HasPrefix(normalized, "wss://"):
		normalized = "https://" + strings.TrimPrefix(normalized, "wss://")
	case strings.HasPrefix(normalized, "ws://"):
		normalized = "http://" + strings.TrimPrefix(normalized, "ws://")
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", fmt.Errorf("parse server URL %q: %w", serverURL, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("server URL %q missing scheme or host", serverURL)
	}

	// Drop any path the user supplied (e.g. /ws/agent) — re-cert lives
	// at the API root.
	parsed.Path = reCertPath
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed.String(), nil
}

// validateResponse sanity-checks the server's payload before we touch
// disk. We require all three PEM blocks; without any of them the agent
// would be left in a broken state after the swap.
func validateResponse(resp *Response) error {
	if resp == nil {
		return fmt.Errorf("nil response")
	}
	if strings.TrimSpace(resp.Cert) == "" {
		return fmt.Errorf("response missing 'cert'")
	}
	if strings.TrimSpace(resp.Key) == "" {
		return fmt.Errorf("response missing 'key'")
	}
	if strings.TrimSpace(resp.CACert) == "" {
		return fmt.Errorf("response missing 'ca_cert'")
	}

	// Validate the cert + key actually form a usable pair before we
	// commit them — catching a server bug here prevents bricking the
	// agent.
	if _, err := tls.X509KeyPair([]byte(resp.Cert), []byte(resp.Key)); err != nil {
		return fmt.Errorf("server returned invalid cert/key pair: %w", err)
	}
	return nil
}

// certEntry describes one of the three cert files that participate in
// rotation: its live path, the perms to write, and the new bytes.
type certEntry struct {
	live string
	perm os.FileMode
	data []byte
}

// backupPair tracks a successful pre-swap rename from live -> backup so
// we can roll it forward if the subsequent rename phase fails.
type backupPair struct {
	live   string
	backup string
}

// installNewCerts performs the atomic-swap dance for the three cert
// files. The sequence is:
//
//  1. Write client.crt.new, client.key.new, ca-cert.pem.new (fsync each).
//  2. Move existing live files to .bak.<ts>.
//  3. Rename .new files into place.
//  4. Prune backups older than MaxBackupsPerFile per family.
//
// If step 3 fails partway, rollbackBackups() restores so we end up in a
// consistent (old-but-working) state, then returns an error so the
// marker stays put for the next attempt.
func installNewCerts(resp *Response, now time.Time) error {
	if err := paths.EnsureCertsDir(); err != nil {
		return fmt.Errorf("ensure certs dir: %w", err)
	}

	// Seal the private key at rest (DPAPI machine scope on Windows). The bytes
	// written to client.key are the sealed blob; loadIdentity unseals on read.
	sealedKey, err := crypto.SealMachineData([]byte(resp.Key))
	if err != nil {
		return fmt.Errorf("seal client key: %w", err)
	}

	// All three identity files are written 0600 and, before being renamed into
	// place, receive a SYSTEM+Administrators-only DACL so the rotated key never
	// exists on disk with the inherited world-readable ACL (AG-CRIT1/AG-H2).
	entries := []certEntry{
		{live: paths.ClientCertPath(), perm: 0600, data: []byte(resp.Cert)},
		{live: paths.ClientKeyPath(), perm: 0600, data: sealedKey},
		{live: paths.CACertPath(), perm: 0600, data: []byte(resp.CACert)},
	}

	ts := now.UTC().Format("20060102T150405Z")

	// Phase 1: write .new files with fsync, then seal each with a protected
	// DACL before it is renamed into place (an explicit file DACL survives the
	// rename). If the DACL cannot be applied we refuse to proceed rather than
	// leaving a world-readable key.
	for _, e := range entries {
		newPath := e.live + ".new"
		if err := writeAndSync(newPath, e.data, e.perm); err != nil {
			// Clean up any partial .new files before bailing.
			cleanupPartial(entries)
			return fmt.Errorf("write %s: %w", newPath, err)
		}
		if err := ipc.SecureFileStrict(newPath); err != nil {
			cleanupPartial(entries)
			return fmt.Errorf("secure %s: %w", newPath, err)
		}
	}

	// Phase 2: back up existing live files. We track which ones we
	// successfully renamed so we can roll back on failure.
	var backups []backupPair
	for _, e := range entries {
		// If the live file doesn't exist (fresh install? shouldn't
		// happen here, but be defensive), skip backup.
		if _, err := os.Stat(e.live); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			cleanupPartial(entries)
			rollbackBackups(backups)
			return fmt.Errorf("stat live cert %s: %w", e.live, err)
		}
		bakPath := fmt.Sprintf("%s.bak.%s", e.live, ts)
		if err := os.Rename(e.live, bakPath); err != nil {
			cleanupPartial(entries)
			rollbackBackups(backups)
			return fmt.Errorf("backup %s -> %s: %w", e.live, bakPath, err)
		}
		// Seal the backup with a protected DACL (H-2). On the first rotation
		// after upgrade the live file is a legacy, world-readable plaintext key
		// (0600 with no DACL); the backup copy must not preserve that exposure.
		if err := ipc.SecureFileStrict(bakPath); err != nil {
			cleanupPartial(entries)
			rollbackBackups(backups)
			return fmt.Errorf("secure backup %s: %w", bakPath, err)
		}
		backups = append(backups, backupPair{live: e.live, backup: bakPath})
	}

	// Phase 3: rename .new -> live. If anything fails here, we have a
	// real problem — some files are live-new, some are live-missing.
	// Try a best-effort rollback from the backups we just created.
	for _, e := range entries {
		newPath := e.live + ".new"
		if err := os.Rename(newPath, e.live); err != nil {
			log.Printf("[ReCert] CRITICAL: rename %s -> %s failed: %v. Attempting rollback from backups.", newPath, e.live, err)
			rollbackErr := rollbackBackups(backups)
			cleanupPartial(entries)
			if rollbackErr != nil {
				// Best-effort failed too. Surface both errors.
				return fmt.Errorf("rename failed (%v) AND rollback failed (%v); cert directory may be in an inconsistent state", err, rollbackErr)
			}
			return fmt.Errorf("rename failed (%v); rolled back to previous certs", err)
		}
	}

	// Phase 4: prune old backups (keep most recent MaxBackupsPerFile per family).
	for _, e := range entries {
		if err := pruneBackups(e.live, MaxBackupsPerFile); err != nil {
			// Non-fatal — log and continue.
			log.Printf("[ReCert] Warning: prune backups for %s: %v", e.live, err)
		}
	}

	return nil
}

// writeAndSync writes data to path with the given permissions and
// fsyncs to force the bytes to stable storage before returning. It creates the
// file EXCLUSIVELY: a private key must never be written into a pre-existing
// file (an attacker who pre-created the .new path keeps a handle and reads the
// key before its DACL is applied). Any file we legitimately own — e.g. a .new
// lingering from a failed run — is removed first; a racing re-creation then
// fails closed via O_EXCL rather than leaking into the other file.
func writeAndSync(path string, data []byte, perm os.FileMode) error {
	_ = os.Remove(path)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return fmt.Errorf("refusing to write %s: exclusive create failed: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// cleanupPartial removes any .new files left behind by a failed
// installNewCerts attempt. Best-effort — errors are intentionally
// ignored because the caller is already returning a failure.
func cleanupPartial(entries []certEntry) {
	for _, e := range entries {
		_ = os.Remove(e.live + ".new")
	}
}

// rollbackBackups restores files from their .bak.<ts> copies after a
// failed install. Returns the first error encountered (if any) but
// always tries every entry.
func rollbackBackups(backups []backupPair) error {
	var firstErr error
	for _, b := range backups {
		if _, err := os.Stat(b.live); err == nil {
			// The live file came back somehow (partial rename) — remove
			// it so we can move the backup back into place.
			if rmErr := os.Remove(b.live); rmErr != nil && firstErr == nil {
				firstErr = rmErr
			}
		}
		if err := os.Rename(b.backup, b.live); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// pruneBackups removes all but the most recent `keep` backups for the
// given live file. Backups are matched by the prefix "<live>.bak.".
func pruneBackups(live string, keep int) error {
	dir := filepath.Dir(live)
	base := filepath.Base(live)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	prefix := base + ".bak."
	var matches []string
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if strings.HasPrefix(name, prefix) {
			matches = append(matches, filepath.Join(dir, name))
		}
	}

	// Sort ascending; oldest first. Timestamp portion is sortable as a
	// string because we emit it in UTC RFC-like form (YYYYMMDDTHHMMSSZ).
	sort.Strings(matches)

	if len(matches) <= keep {
		return nil
	}

	toDelete := matches[:len(matches)-keep]
	for _, m := range toDelete {
		if err := os.Remove(m); err != nil {
			log.Printf("[ReCert] Warning: failed to prune backup %s: %v", m, err)
		}
	}
	return nil
}

// bumpAgentInfo refreshes the LastSeen field in agent-info.json. If the
// file doesn't exist yet (first run after install) we leave it alone —
// the agent's normal startup path will write it.
func bumpAgentInfo(agentID string) error {
	existing, err := ipc.ReadAgentInfo()
	if err != nil {
		// Could be a stale signature from before rotation; treat as
		// "no existing info" and skip rather than failing the whole
		// rotation over an HMAC mismatch on an unrelated file.
		return fmt.Errorf("read agent-info: %w", err)
	}
	if existing == nil {
		// Nothing to bump.
		return nil
	}
	existing.LastSeen = time.Now()
	if agentID != "" && existing.AgentID == "" {
		existing.AgentID = agentID
	}
	return ipc.WriteAgentInfo(existing)
}

// truncateForLog clips response bodies to a manageable length when
// surfacing them in error logs.
func truncateForLog(b []byte) string {
	const max = 256
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...[truncated]"
}

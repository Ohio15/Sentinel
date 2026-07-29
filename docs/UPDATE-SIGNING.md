# Update Signing (RW-1, Wave B)

Ed25519 detached-signature enforcement for the Sentinel agent/watchdog update
chain. Closes AG-C1, AG-C2, AG-H1, AG-H4, AG-H5, WD-H2, WD-H3, WD-H4.

## Design

- The release pipeline signs every artifact with an Ed25519 **private** key held
  only on the NEXUS build host's protected store (`SENTINEL_UPDATE_SIGNING_KEY`,
  a PKCS#8 PEM). It never enters the repo.
- The corresponding **public** key is embedded into every binary at build time
  via `-ldflags "-X github.com/sentinel/agent/internal/updatesig.SigningPublicKeyHex=<hex>"`
  (`SENTINEL_UPDATE_SIGNING_PUBKEY`). It is never supplied by the server or the
  network.
- Signatures are transported as base64 (`base64(ed25519.Sign(priv, rawBytes))`)
  both in a sidecar `<binary>.sig` served next to the artifact and in the
  update-check response (`versionInfo.signature`).
- Every update path verifies the signature over the **exact downloaded bytes**
  against the **embedded** public key, immediately before any swap:
  1. agent updater (`agent/internal/updater/updater.go`) — after download, before staging/handoff;
  2. watchdog agent-binary swap (`agent/cmd/sentinel-watchdog/main.go` `verifyStagedFile`) — before `atomicReplace`;
  3. watchdog self-update (`agent/internal/selfupdate/selfupdate.go`) — before the self-update swap;
  4. watchdog independent poller (`agent/cmd/sentinel-watchdog/main.go` `downloadAndStageUpdate`) — over the bytes it downloaded.

## Fail-closed guarantees

- A build with **no embedded public key cannot self-update** (`updatesig.Verify`
  returns `ErrNoEmbeddedKey`). The empty default is intentional build-time
  config, not a placeholder.
- An **empty or malformed signature is rejected**. There is no self-computed
  checksum fallback — a checksum derived from the same bytes it is meant to
  verify (or from the same channel that supplied the bytes) proves nothing.
- **Anti-rollback (AG-H4):** the agent refuses any target that is not strictly
  greater than the running version (strict semver via `updatesig.IsUpgrade`)
  unless the signed metadata carries `signedDowngrade: true`.
- **Origin pinning (WD-H3):** download URLs are constrained to the configured
  server host over `https` on both the agent updater and the watchdog poller.
- **Path identity (WD-H4 / AG-H5):** the staged path must be inside the staging
  directory and the target path must equal the known executable path
  (`filepath.Clean` + exact match) before any swap — no shell templating.
- The release pipeline **fails** if `SENTINEL_UPDATE_SIGNING_KEY` or
  `SENTINEL_UPDATE_SIGNING_PUBKEY` are absent — an unsigned release cannot be
  produced.

## Bootstrap of trust (one-time)

The first signed build must be delivered over the **current unsigned channel
once** — existing fielded agents have no embedded public key yet, so they cannot
verify anything, and the signed build they receive is what establishes the trust
anchor. Ship this as a single deliberate "trust-establishing" release: publish
the signed `sentinel-agent.exe` / `sentinel-watchdog.exe` (with the pubkey baked
in and `.sig` sidecars present) over the existing update path. Once a host is
running that build, every subsequent update it accepts is signature-enforced and
fail-closed. Key rotation follows the same shape: a new public key ships inside a
signed update, so the rotation itself is authenticated by the outgoing key.

## Signing tool

`agent/cmd/sign` reads the private key from `SENTINEL_UPDATE_SIGNING_KEY`, signs
a file, prints the base64 signature to stdout, and writes `<file>.sig`. No key
material is committed. Generate a keypair once and store the private PEM in the
build host's protected store:

```
# private key (keep OFF the repo, in the protected store)
openssl genpkey -algorithm ed25519 -out sentinel-update-signing.pem
# public key hex for SENTINEL_UPDATE_SIGNING_PUBKEY (32-byte raw, hex-encoded)
openssl pkey -in sentinel-update-signing.pem -pubout -outform DER \
  | tail -c 32 | xxd -p -c 64
```

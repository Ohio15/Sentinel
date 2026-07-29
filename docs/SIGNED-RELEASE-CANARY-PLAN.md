# First Signed Release — Canary Plan (RW-1 trust establishment)

Date: 2026-07-29 · Follows: docs/SECURITY-REMEDIATION-2026-07-29.md, handoff 432196ef step 1, signing-host decision 2d7325c8 (NEXUS).

## Why a canary

The fleet currently runs pre-RW-1 agents (1.77.10) with an **empty embedded
signing key**: they do not verify updates, and the first signed build is
delivered over that unauthenticated channel (TOFU). Once a device runs a signed
build, it **fails closed** on every future update — a defective first signed
build is therefore the single riskiest artifact this pipeline will ever ship.
The verify loop must be proven on one device before the fleet sees it.

## Constraints discovered (2026-07-29)

- **The announce gate is fleet-wide.** Both the heartbeat-ack path and
  `GET /api/agent/version` suppress `updateAvailable` unless an
  `agent_releases` row exists for the advertised version (60s cache). The
  moment the row exists, every online agent is announced to. `force_update`
  goes through the same `CheckForUpdate` → same gate — it cannot bypass it.
- **Rollout MVP does not scope the announcement.** `POST /api/rollouts` with
  `target_type=device-list` tracks per-device state but dispatches "via the
  heartbeat-ack path" — i.e. the global announce. Staged/update-group/canary
  values are schema-reserved and handler-rejected (400).
- **Fleet census**: all windows/amd64 (3–4 online, ~8 offline) + one offline
  ubuntu/amd64. No arm64/darwin/386 devices exist.
- Old force-update push was removed (SEC-007); the live per-device lever is
  `handleForceUpdate` (WS `force_update`), gate-bound as above.

## Plan

### Phase 0 — deploy dark (gate closed)

> **Hard precondition (new, from review round 2):** the release script now
> refuses to finish a deploy while the serving directory contains any
> `sentinel-{agent,watchdog,bootstrap,verify}` binary that is not from the
> release being deployed (no signed sidecar, or a sidecar for another version).
> The stale artifacts listed in Phase 3 step 10 — `sentinel-agent-windows-386.exe`,
> the `*-arm64*` binaries, `*.bak-*`, and the deploy tree's unsuffixed
> `release/agent/sentinel-agent` — therefore have to be **archived before**
> Phase 0, not after. This is deliberate: the server advertises seven
> (platform, arch) tuples and falls back to unsuffixed names, so any stale file
> left there is announced as the new version and served unsigned, which puts
> agents on those targets into a permanent fail-closed retry loop. Archiving is
> a Ron-gated action (it changes what the live server can serve).
>
> **Trust-anchor pin status:** `installers/version.json` at HEAD has no
> `signingPublicKeyHex`, so v1.77.41 is genuinely TOFU and the pin cannot apply.
> Publishing v1.77.41 writes that field, which makes the pin **active** for
> v1.77.42 onward — from Phase 2 on, a wrong or rotated signing key aborts the
> release instead of bricking the fleet (override is an explicit
> `-RotateSigningKey`).

1. Cut **v1.77.41** on NEXUS: `pwsh scripts/release.ps1 -Version 1.77.41 -Deploy`
   from `~/repos/Sentinel-build` (env from `~/.sentinel-signing/release-signing-vars.sh`).
   Binaries + signed sidecars go live in `~/Sentinel/installers`; **no
   `agent_releases` row is inserted**, so nothing is announced to anyone.
2. Verify served state (script does this; independently spot-check sha256 vs
   sidecar and the HTTP endpoint with `SENTINEL_UPDATE_CHECK_URL` set to a
   host-reachable URL — `sentinel.nexus` does not resolve on NEXUS itself).

### Phase 1 — manual canary (one device)
3. Canary device: one online windows/amd64 device (Ron picks).
4. Install v1.77.41 on the canary **manually** (download
   `/api/agent/update/download` artifact or copy from NEXUS; verify sha256
   against the signed sidecar before swap). No announce-gate change.
5. Soak: agent healthy, heartbeats, telemetry, cert-binding WARN logs normal.
   The canary now enforces signatures on all future updates.

### Phase 2 — prove the verify loop (second signed release)
6. Cut **v1.77.42** signed, `-Deploy`.
7. Insert the `agent_releases` row for 1.77.42 (idempotent INSERT, see
   `scripts/publish-1.77.10-agent_releases.sql` shape). This announces to the
   whole online fleet — accepted: the canary must VERIFY 1.77.42 (fail-closed
   path proven live); the remaining 1.77.10 agents jump straight to 1.77.42
   over the same unsigned-trust channel they have always used.
8. Confirm: canary self-updated with signature verification logged; other
   online agents updated; no download retry storms (watch
   `[ReleaseStatus]`/update-failure alerts).

### Phase 3 — offline stragglers + hygiene
9. Offline agents update as they reappear. The one ubuntu/amd64 box (1.77.3,
   offline) gets linux-amd64 signed artifacts — deployed by the same release.
10. Archive stale never-signed binaries out of `installers/`
    (`sentinel-agent-windows-386.exe`, `*-arm64*`, `*.bak-*`) so unsupported
    targets fail cleanly instead of serving stale unsigned bytes.

## Rollback

- Phase 0/1: restore from the script's `.backup-*` dir; canary device can be
  manually reverted to 1.77.10 (it accepted 1.77.41 unsigned-trust, but going
  BACK requires manual swap — signed agents reject unsigned downgrades).
- Phase 2: delete the `agent_releases` row (announce stops within 60s cache);
  agents already updated stay on 1.77.42.

## Follow-up build item (labeled debt)

The manual canary is a workaround for a real capability gap: the rollout MVP
reserved but never implemented scoped announcement
(`mode=staged`, `target_type=update-group`, `channel=canary`). Building that —
heartbeat announce consults rollout membership — is the correct mechanism and
should be scheduled; until then every gate-open is fleet-wide.

## Preconditions checklist (all must hold before Phase 0)

- [ ] release.ps1 passed independent adversarial re-review. Round 2 returned
      5 HIGH / 10 MEDIUM / 8 LOW and all substantive findings were fixed
      (backups moved out of the served volume, mandatory git-HEAD key pin,
      Linux watchdog stub dropped from the matrix, `-DeployOnly` retry path,
      served-artifact staleness gate, per-run staging + deploy lock, atomic
      verified restore, TLS-verified polling probe, all published binaries'
      version strings bumped, host tools in a mode-700 mktemp dir). Round 3
      re-review of those fixes is REQUIRED before Phase 0.
- [ ] Stale served artifacts archived (see Phase 0 hard precondition)
- [x] pwsh 7.6.4 on NEXUS; clean build checkout `~/repos/Sentinel-build`
- [x] Signing key present/0600; pubkey derived `aac3c014…347e70`; sign→verify
      →tamper-reject chain proven on NEXUS with the real key
- [x] Full target matrix (agent/watchdog/bootstrap/verify × win-amd64,
      linux-amd64) compiles on NEXUS, CGO=0
- [ ] Branch merged to main and pulled into `~/repos/Sentinel-build`
- [ ] Ron's go on: canary device choice; accepting fleet-wide announce at
      Phase 2; archiving stale unsupported-target binaries

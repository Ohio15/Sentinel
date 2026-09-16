# Sentinel — Production Schema Drift: Investigation & Remediation Plan

**Date:** 2026-08-05
**Author:** Claude (investigation agent)
**Scope:** `Ohio15/Sentinel` — production database `sentinel` on NEXUS (`192.168.1.20`, container `sentinel-postgres`, PostgreSQL 16.13)
**Change status:** READ-ONLY. No migration applied, no production data or schema modified, nothing pushed. Two throwaway PostgreSQL containers were created and destroyed on NEXUS for schema comparison; production was touched only with `SELECT` / catalog reads.
**Related:** PR #70 (open, unmerged) — `fix/fresh-install-migration-conflict`

---

## 1. Executive summary

The briefed defect is real but **understated in three ways**, and the leading hypothesis is **wrong in mechanism though right in substance**.

1. **It is 8 missing tables, not 6.** `mfa_events` (migration `000043_mfa_totp`) and `usb_file_transfers` (migration `000047_usb_file_transfers`) are also absent from production, along with 6 missing columns — five of which are the entire **MFA/TOTP** column set on `users`.
2. **No `migrate force` was ever run.** golang-migrate was not the runner when the damage occurred. Versions 34–48 were marked applied by a **single manual bulk `INSERT`** into the legacy tracking table on **2026-02-25 13:38:23.024567+00**. The May 2026 golang-migrate cutover then collapsed that record into `version=59, dirty=false`, laundering the false state into a clean-looking one.
3. **PR #70 does not fix fresh installs for these tables.** Four migration files carry **sql-migrate** directives (`-- +migrate Up` / `-- +migrate Down`) while the runner is **golang-migrate**, which treats them as ordinary comments and executes the whole file. Those migrations **create the tables and then immediately `DROP` them in the same migration**. I verified this empirically: applying PR #70's migration set to an empty PostgreSQL 16.13 database succeeds with exit 0 and produces **no** `webhooks`, `patch_*`, `script_*`, or `mfa_events` tables. PR #70's FK type fix is necessary but not sufficient.

Additionally, drift runs in **both directions**. A fresh install today produces a database that is broken in ways production is not — most seriously `devices.boot_time` is `BIGINT` from migration `000010`, while the Go code requires `TIMESTAMPTZ` (`EXTRACT(EPOCH FROM boot_time)`, `boot_time = to_timestamp($14)`). Production has `TIMESTAMPTZ`, sourced from out-of-band DDL that exists in no migration.

**Nothing is currently failing in production.** Five days of complete, unrotated backend logs (6,431,705 lines) contain zero `42P01` errors, zero 5xx responses, and zero requests to any affected endpoint. The affected features have no UI client of any kind. The exposure is **latent**, plus one real control gap: **MFA/TOTP cannot be enrolled or enforced on this deployment.**

**Overall severity: MEDIUM.** Availability impact today: none. Data-integrity impact today: negligible (1 user, 1 organization, 13 devices). The material risks are the missing MFA control, the false-success bulk-approval endpoint, and the fact that every fresh install / new tenant / restore-to-empty produces a materially different and partly broken schema.

---

## 2. Method

Production was read with catalog queries and `psql` `SELECT`s only.

To obtain the fresh-install schema I built two throwaway databases on NEXUS (`postgres:16-alpine`, `--tmpfs` storage, no volumes, no published ports, both `docker rm -f`'d afterwards), matching production's 16.13:

- **Build A** — all 59 `*.up.sql` files from PR #70's HEAD applied verbatim in filename order, each with `psql -v ON_ERROR_STOP=1 --single-transaction` (mirroring golang-migrate's per-migration transaction). Result: `DONE fail=0` — and no `webhooks`, `patch_*`, `script_*`, or `mfa_events` tables. **This is what a fresh install actually produces today.**
- **Build B** — the same files with everything from `-- +migrate Down` onward stripped. Result: `DONE fail=0`, tables present. **This is what the migrations were *intended* to produce**, and is the baseline used for the drift inventory below.

Both builds and production were then dumped to a canonical, sorted text form (tables, columns with type/nullability/default, indexes with full `indexdef`, constraints with full `pg_get_constraintdef`) and diffed with `comm`.

**Known comparison noise, excluded and labelled as non-drift:**
- Date-partitioned tables `device_metrics_2026_MM` / `mobile_metrics_2026_MM` — created at runtime by the application; fresh has 08–11, production has 02–05. Expected.
- `schema_migrations`, `schema_migrations_legacy` — tracking tables.
- ~20 `CHECK` constraints and 8 partial indexes differ only in catalog deparse rendering — `ANY ((ARRAY['a'::varchar, ...])::text[])` vs `ANY (ARRAY[('a'::varchar)::text, ...])`. Semantically identical; production's form reflects an older stored parse tree. **Not drift.** These are excluded from the inventory to avoid inflating it.

---

## 3. Root cause

### 3.1 What actually happened (well supported)

The pre-May-2026 migration runner (`server/pkg/database/database.go` before commit `6c30f17`) was a hand-maintained manifest that mapped filenames to version numbers (`"migrations/031_patch_approvals.sql", // v42`), executed each file as one `pool.Exec`, and recorded each success as its own row:

```go
_, err = db.pool.Exec(ctx, string(schema))
if err != nil { return fmt.Errorf("failed to apply migration %d: %w", i+1, err) }
_, err = db.pool.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", i+1)
```

Because `applied_at` defaults to `NOW()` — the transaction timestamp — genuine runner-applied rows **must** carry distinct timestamps. The surviving forensic table shows exactly that for real runs, and the opposite for the damage window:

```
  29 | 2026-02-25 13:33:46.574125+00
  30 | 2026-02-25 13:35:59.473781+00
  31 | 2026-02-25 13:35:59.477002+00
  32 | 2026-02-25 13:35:59.482567+00
  33 | 2026-02-25 13:35:59.504993+00
  34 | 2026-02-25 13:38:23.024567+00   <-- 15 rows share this exact microsecond
  ...
  48 | 2026-02-25 13:38:23.024567+00
```

Fifteen migrations creating dozens of tables cannot share one microsecond across fifteen separate transactions. **This is a single manual `INSERT` statement.** The same fingerprint appears earlier at versions 8–28 (21 rows sharing `2026-02-07 15:42:27.062903+00`).

The bulk insert landed **37 seconds after** commit `27cf442` (2026-02-25 08:37:46 EST, "Fix migration 34: organization_id type mismatch (UUID vs INTEGER)"), which repaired that defect in `034_usb_devices.sql` (v45) **only** — leaving the identical defect in `031_patch_approvals.sql` (v42) and `033_script_scheduling.sql` (v44). This is exactly consistent with production having `usb_devices` and `webhooks` but not `patch_policies` or `script_schedules`.

Contemporary commit language confirms manual SQL was standing practice. Commit `45e95d7` (2026-02-25, three minutes earlier): *"Add all migrations v10-v48 to ensure future deployments auto-apply pending migrations **without manual SQL intervention**."* Commit `229d90e` (2026-05-06) states outright: *"NEXUS production state was correct because **the equivalent transaction was applied manually 2026-05-06**."*

The May 2026 cutover to golang-migrate (`6c30f17`) then ran `bootstrapLegacySchemaMigrations`, which renames the legacy table and seeds the new one with **one row at `MAX(version)`**:

```sql
SELECT COALESCE(MAX(version), 0) INTO legacy_max FROM schema_migrations;
ALTER TABLE schema_migrations RENAME TO schema_migrations_legacy;
INSERT INTO schema_migrations (version, dirty) VALUES (legacy_max, FALSE);
```

golang-migrate keeps **no per-version record**. "42 and 44 recorded applied" only ever meant `59 >= 44`. Nothing was forced; the false state was inherited.

**Independent corroboration:** the 2026-03-10 pre-migration backup on NEXUS (`~/migration-backup/sentinel-postgres.sql`, 470 MB) contains zero occurrences of `patch_policies` or `script_schedules`, contains `usb_devices`, and its `COPY public.schema_migrations` block reproduces the identical `.024567` timestamps. The anomaly is five months old and predates all recent activity.

### 3.2 The second, independent root cause — sql-migrate directives under golang-migrate

`server/go.mod:11` pins `github.com/golang-migrate/migrate/v4 v4.19.1`, which expresses direction through `.up.sql`/`.down.sql` **filenames** and treats `-- +migrate Down` as a comment. Four migration files, all authored 2026-02-08 in commit `6050c42`, embed a sql-migrate-style `Down` section inside the `.up.sql` file:

| File | Trailing self-drop |
|---|---|
| `000041_webhooks.up.sql` | `DROP TABLE IF EXISTS webhook_deliveries; DROP TABLE IF EXISTS webhooks;` |
| `000042_patch_approvals.up.sql` | drops all 4 patch tables |
| `000043_mfa_totp.up.sql` | drops `mfa_events` (+ reverses the `users` columns) |
| `000044_script_scheduling.up.sql` | `DROP TABLE IF EXISTS script_executions; DROP TABLE IF EXISTS script_schedules;` |

Only 3 `.down.sql` files exist for 59 up-migrations, so this is not a naming-convention artefact.

**This defect is live and unfixed on PR #70's branch.** Build A proves it: a fresh install today creates and then drops these tables and exits successfully. The legacy runner had the same blind spot (`pool.Exec(ctx, string(schema))` — whole file, one implicit transaction), so this defect and the FK type defect were both present from 2026-02-08 onward.

### 3.3 Verdict on the briefed hypothesis

| Claim | Verdict |
|---|---|
| Someone ran `migrate force` to unblock deploys | **REFUTED.** `Grep` for `\.Force\(`/`ErrDirty`/`IgnoreDirty` across `server/` → no matches. `git log --all -S"migrate force"` → zero commits. golang-migrate was not the runner until 2026-05-07. |
| Someone advanced the version without executing the DDL | **CONFIRMED**, by a manual bulk `INSERT` into the legacy table, not by `migrate force`. |
| Production is clean at 59 because the failure was papered over | **CONFIRMED**, with the additional mechanism that the golang-migrate bootstrap seeded from `MAX(legacy_version)` and thereby inherited the lie. |
| The FK type error is why the tables are missing | **PARTIALLY.** It is why the DDL could not succeed in Feb 2026. Even with PR #70's FK fix, the trailing `DROP` (§3.2) independently guarantees the tables do not survive. |

### 3.4 What I could not determine

- **Who** ran the bulk `INSERT`, and its exact statement text. `~/.bash_history` on NEXUS is 2,020 bytes, last written 2026-07-14, with zero `migrat`/`force` matches — it has rotated well past February. No deploy script on the box references migrations. Only PostgreSQL server logs from 2026-02-25 (if `log_statement` was enabled — not verified) or the operator's session transcript would settle it.
- Whether the operator knew 42/44 had failed, or bulk-stamped 34–48 blind after fixing only `034_usb_devices.sql`.
- **Why `webhooks` / `webhook_deliveries` exist in production** despite `000041`'s self-drop and its own UUID FK defect. The `-- +migrate Down` block was present in that file from its first commit (2026-02-08), so the runner cannot have created them. The best-supported inference — consistent with the documented "manual SQL intervention" practice and with `000052_webhooks_fix_org_id_type` later needing to convert a **UUID** `organization_id` to INTEGER — is that `webhooks` was created **out-of-band by hand** with the UUID column. I did not find direct evidence and do **not** assert it. This matters only in that the repair must not assume uniformity.
- The provenance of production's `devices.boot_time` (`TIMESTAMPTZ`), `devices.last_force_update_at`, and `idx_agent_health_status_v2` — none is produced by any migration in the tree. Also presumed out-of-band; not established.
- Backend logs before 2026-07-31T11:43Z. The prior container's log was not located, so the "zero errors" finding covers 5 days, not the full 161-day exposure.

---

## 4. Full drift inventory

Production `schema_migrations` = `version 59, dirty f`. 127 relations in `public`.

### 4.1 Present in intended-fresh, MISSING from production

**Tables (8):**

| Table | Source migration | Recorded applied |
|---|---|---|
| `patch_policies` | `000042_patch_approvals` | 2026-02-25 13:38:23 UTC |
| `patch_approvals` | `000042_patch_approvals` | 2026-02-25 13:38:23 UTC |
| `device_patch_assignments` | `000042_patch_approvals` | 2026-02-25 13:38:23 UTC |
| `patch_installations` | `000042_patch_approvals` | 2026-02-25 13:38:23 UTC |
| **`mfa_events`** | `000043_mfa_totp` | 2026-02-25 13:38:23 UTC |
| `script_schedules` | `000044_script_scheduling` | 2026-02-25 13:38:23 UTC |
| `script_executions` | `000044_script_scheduling` | 2026-02-25 13:38:23 UTC |
| **`usb_file_transfers`** | `000047_usb_file_transfers` | 2026-02-25 13:38:23 UTC |

**Columns (6):**

| Column | Fresh definition | Source |
|---|---|---|
| `users.totp_enabled` | `boolean NOT NULL DEFAULT false` | `000043` |
| `users.totp_secret` | `text NULL` | `000043` |
| `users.totp_verified_at` | `timestamptz NULL` | `000043` |
| `users.backup_codes` | `ARRAY NULL` | `000043` |
| `users.mfa_required` | `boolean NOT NULL DEFAULT false` | `000043` |
| `alerts.metadata` | `jsonb NULL DEFAULT '{}'::jsonb` | `000047` |

**Indexes (5, excluding those on missing tables):**

| Index | Note |
|---|---|
| `idx_alerts_metadata` (GIN) | depends on the missing `alerts.metadata` |
| `idx_agent_installation_links_org` | btree(`organization_id`) |
| `idx_agent_links_org` | btree(`organization_id`) — duplicate of the above in the fresh tree |
| `idx_agent_logs_device_level` | btree(`device_id`, `level`) — production has `idx_agent_logs_device_timestamp` on (`device_id`,`logged_at`) instead |
| `users_username_key` | UNIQUE(`username`) |

**Constraints (7)** — each verified directly against production's `pg_constraint`, not inferred from the diff:

| Constraint | Definition | Production state |
|---|---|---|
| `users.users_username_key` | `UNIQUE (username)` | **absent** (production has only `users_email_key`, `users_pkey`, `users_organization_id_fkey`) |
| `alerts.alerts_device_id_fkey` | `FK (device_id) → devices(id) ON DELETE CASCADE` | **absent** |
| `alerts.fk_alerts_device_id` | `FK (device_id) → devices(id) ON DELETE CASCADE` | **absent** (duplicate of the above in the fresh tree) |
| `device_metrics.device_metrics_device_id_fkey` | `FK (device_id) → devices(id) ON DELETE CASCADE` | **absent** |
| `client_certificates.client_certificates_device_id_fkey` | `FK (device_id) → devices(id) ON DELETE SET NULL` | **absent** |
| `kb_article_views.kb_article_views_article_id_fkey` | `FK (article_id) → kb_articles(id) ON DELETE CASCADE` | **absent** |
| `agent_link_access_log.agent_link_access_log_link_id_fkey` | `FK (link_id) → agent_installation_links(id) ON DELETE CASCADE` | **absent** |

The six missing foreign keys are **referential-integrity gaps predating the February event** (they originate in the early migrations, which production did run). Their most likely cause is out-of-band table recreation. Cause **undetermined**; the gap itself is confirmed.

### 4.2 Present in production, MISSING from a fresh install (reverse drift — current-tree defects)

These are defects in the repository, not in production, and PR #70 does not address them.

| Object | Production | Fresh install | Assessment |
|---|---|---|---|
| `webhooks`, `webhook_deliveries` | present | **absent** | `000041` self-drop (§3.2). Fresh installs get no webhooks feature at all. |
| `devices.boot_time` | `timestamptz` | `bigint DEFAULT 0` | **Fresh install is broken.** `server/internal/api/devices.go:76,154` run `EXTRACT(EPOCH FROM boot_time)::bigint` and `:767` runs `boot_time = to_timestamp($14)`. Both fail against `bigint`. Only migration `000010_extended_device_info.up.sql:13` defines the column, as `BIGINT DEFAULT 0`. Production's `timestamptz` came from out-of-band DDL. Device listing and enrollment would fail on a fresh install. |
| `devices.last_force_update_at` | `timestamptz` | absent | Produced by no migration. Out-of-band. |
| `agent_logs.level` | `varchar(20)` | `varchar(10)` | Introduced by PR #70's change to `000013` (which removed the duplicate `agent_logs` definition, making `000008` authoritative). Production runs `000013`'s shape. |
| `agent_logs.source` | `varchar(255) NULL` | `varchar(50) NOT NULL DEFAULT 'agent'` | same cause |
| `agent_logs.received_at` | `NULL`, `CURRENT_TIMESTAMP` | `NOT NULL`, `now()` | same cause |
| `agent_logs.metadata` | default `'{}'::jsonb` | no default | same cause |
| `users.username` | `varchar(100)` | `varchar(50)` | length divergence |
| `devices.platform` | `varchar(100)` | `varchar(20)` | length divergence |
| `agent_installation_links_organization_id_fkey` | present | absent | FK → `organizations(id)` |
| `idx_agent_health_status_v2` | present | absent | produced by no migration; out-of-band |

The `agent_logs` row is worth flagging to the PR #70 author: choosing `000008` as the owner of `agent_logs` is defensible for fresh installs, but it makes fresh installs diverge from production on four column definitions. That decision should be made explicitly, not as a side effect.

---

## 5. Blast radius

### 5.1 Code and endpoints

All references to the patch and scheduling tables live in exactly two handler files. There is no repository/store layer; SQL is inlined in gin handlers via `services.DB.Pool()`.

| Missing table | Go references | API endpoints (all under `/api`, `router.go:62`/`264`) |
|---|---|---|
| `patch_policies` | `server/internal/api/patches.go` :76,133,138,179,224,229,266 | `GET/POST /patch-policies`; `GET/PUT/DELETE /patch-policies/:id` (`router.go:448-452`) |
| `patch_approvals` | `server/internal/api/patches.go` :292,352,360,411,417,453 | `GET /patch-approvals`; `POST /patch-approvals/:id/approve`; `POST /patch-approvals/bulk`; `GET /devices/:id/pending-patches` (`router.go:453-456`) |
| `device_patch_assignments` | **migration only** — zero Go/TS references | none |
| `patch_installations` | **migration only** — zero Go/TS references | none |
| `script_schedules` | `server/internal/api/schedules.go` :90,178,227,265,304,339,381,435 | `GET/POST /schedules`; `GET/PUT/DELETE /schedules/:id`; `POST /schedules/:id/toggle`; `POST /schedules/:id/run` (`router.go:466-472`) |
| `script_executions` | `server/internal/api/schedules.go` :422,462,542 | `GET /executions`; `GET /executions/:id` (`router.go:473-474`) |
| `mfa_events` + 5 `users` columns | `server/internal/api/mfa.go` (14 sites, incl. `INSERT INTO mfa_events` at :384); `server/internal/api/export.go:244-255` | 5 MFA routes (`router.go:459-463`); `GET /export/users` |
| `usb_file_transfers`, `alerts.metadata` | `server/internal/api/usb.go:906` | USB file-transfer surface |

### 5.2 UI reachability — the patch and scheduling features are unreachable

- Repo-wide `git grep` for `patch-polic|patch-approval|pending-patches` returns **only the 9 route-registration lines in `router.go`**. No client exists in the Electron renderer (`src/renderer/`, 98 files), the portal (`src/portal/`), or `mobile/`.
- `src/renderer/components/layout/Sidebar.tsx:14-24` navigation is: dashboard, clients, devices, tickets, alerts, scripts, network, certificates, knowledge-base, settings. The `onNavigate` page union (`Sidebar.tsx:8`) has no patch or schedule member, so there is not even an unlinked-but-routable page.
- The **deployed** frontend bundle (`sentinel-frontend:/usr/share/nginx/html/assets`) has zero hits for these endpoint paths, with the grep sanity-validated against paths that do appear (`dashboard/stats`, `/devices`, `/alerts`).
- No feature flag is involved. The UI was simply never built.

### 5.3 Error handling if the endpoints are called

**(a) Hard 500** — 10 endpoints. Representative, `patches.go:80-84`:
```go
if err != nil {
    log.Printf("[Patches] Error listing policies: %v", err)
    c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list policies"})
```
Same shape at `patches.go:149,205/241,268,305,368`; `schedules.go:95,499`.

**(b) Silently swallowed — false success (3):**
- `patches.go:460-464` — returns HTTP 200 `[]`:
  ```go
  if err != nil {
      // If the query fails (possibly due to schema differences), return empty
      c.JSON(http.StatusOK, []map[string]interface{}{})
      return
  }
  ```
- `patches.go:386-432` `bulkApprovePatchesHandler` — `if err == nil { updated += ... }`; on `42P01` returns **HTTP 200 `{"message":"Patches updated successfully","updated":0}`**. This is the worst of the three: an operator would be told a bulk patch approval succeeded when nothing happened.
- `schedules.go:419-436` `runScheduleNowHandler` — discards the `INSERT INTO script_executions` and `UPDATE script_schedules` results and returns 200 "Schedule triggered successfully". Unreachable in practice: the preceding `SELECT` at :378-389 fails first and returns 404.

**(c) Misleading 404** — `getPatchPolicyHandler` (`patches.go:187`), `getScheduleHandler`, `getExecutionHandler` (`schedules.go:552`) collapse any error, including `42P01`, into "not found".

### 5.4 Background jobs: none

Every `Ticker`/`cron`/scheduler in `server/` was reviewed. The only relevant tickers are `rollouts_ticker.go` (rollouts) and `router_network.go:48` (`router_scheduled_actions` — a different, working feature). **Nothing ticks over `script_schedules`.** `schedules.go:172-175` concedes cron is unimplemented (`// Would need a cron parser here - for now just set to nil`). There is therefore no recurring log noise, and its absence is not evidence of health.

### 5.5 Production evidence

Backend log retention is genuine and adequate: `sentinel-backend` log driver is `json-file` with **no** `max-size` and **no rotation**. The file is 1.27 GB / **6,431,705 lines**, spanning **2026-07-31T11:43:35Z → 2026-08-05T11:57:05Z** continuously. `Created=2026-07-31T11:43:23Z`, `RestartCount=0` — this is the complete life of the current container.

> **Methodology note.** `docker logs sentinel-backend | grep` returned 0 matches, but that was a **false negative**: `docker logs` returned only 288,327 of 6,431,705 lines and stopped at 07-31 16:35. All figures below come from `sudo grep -a` against the raw `json.log`.

| Pattern | Occurrences over 5 days |
|---|---|
| `does not exist` / `42P01` | **0** |
| `patch_polic\|patch_approval\|device_patch\|patch_installation\|script_schedule\|script_execution` | **0** |
| `[Patches]` / `[Schedules]` / `[Executions]` | **0** |
| `patch-polic\|patch-approval\|pending-patches\|/api/schedules\|/api/executions` | **0** |
| `mfa` (any case) | **0** |
| `export/users` | **0** |
| HTTP 5xx | **0** |
| HTTP 4xx | 1,762 |
| `level.*WARN` | 65 |

Grep is proven working by the non-zero rows. Distinct `/api` paths actually observed: `agent/enroll`, `agent/update/download`, `agent/version`, `agent/watchdog/version`, `alerts`, `auth/login`, `auth/me`, `auth/refresh`, `bootstrap/agent-info`, `clients`, `dashboard/stats`, `devices`, `devices/:id/commands`, `devices/:id/hide`, `download/agent/windows`, `openapi.json`, `public/install/validate-code`, `scripts`, `scripts/:id/execute`, `settings`, `system`, `users`, plus internet scanner probes. **No affected endpoint appears.**

### 5.6 Were the features ever used?

- `scripts` = **104 rows**; `/api/scripts/:id/execute` is actively used. Ad-hoc script execution works; only *scheduling* is absent.
- `device_updates` = **0 rows** — the companion table `getPendingPatchesForDeviceHandler` reads. Even with `patch_approvals` restored, that endpoint returns `[]`: no patch inventory has ever been ingested.
- No companion data anywhere indicates patch approval or script scheduling was ever exercised.
- Deployment scale: **13 devices, 1 organization, 1 user, 1 webhook, 56,945 alerts.**

**Verdict: (c) unreachable — dead code, not currently failing.** The exposure is latent. The one live consequence is §6.2.

---

## 6. How long, and severity

### 6.1 Duration

Migrations `000042`, `000043`, `000044` and `000047` were recorded applied at **2026-02-25 13:38:23.024567+00** — an exact timestamp from `schema_migrations_legacy.applied_at`, independently corroborated by the 2026-03-10 backup. Front-bounded by commit `27cf442` at 13:37:46 UTC.

**Elapsed: 2026-02-25 → 2026-08-05 = 161 days (~5.3 months).** The features have never functioned on this deployment at any point in its history.

### 6.2 Security assessment — CVSS v3.1

**MFA/TOTP unavailable (migration `000043`).** No defensible CVSS v3.1 vector exists for this finding, and I will not manufacture one: there is no attacker-reachable weakness. MFA fails **closed** — the enrolment endpoints error rather than granting access, login continues to work as single-factor, and no bypass is introduced. Scored strictly, `AV:N/AC:H/PR:N/UI:N/S:U/C:N/I:N/A:N` = **0.0**, which is correct and also unhelpful.

The honest framing is a **missing security control** (CWE-1390, *Weak Authentication*, in its "second factor unavailable" sense), not a vulnerability:
- Impact is **conditional**: it raises the consequence of credential compromise from "attacker needs a password" to "attacker needs a password", where the operator believes it is "password + TOTP".
- Mitigating: 1 user account, single-tenant, admin console not exposed to the public internet as a self-service surface, WebAuthn tables (`webauthn_credentials`, `webauthn_sessions`) **are** present so a second-factor path may exist independently.
- Aggravating: the gap is **silent** — nothing in the UI or logs indicates MFA is unavailable, so the control was plausibly assumed present for 161 days.

**Rating: MEDIUM.** The severity is in the false assurance, not in exploitability.

**`POST /api/patch-approvals/bulk` false success.** This is a genuine integrity-of-reporting defect: authenticated caller receives `200 {"updated":0,"message":"Patches updated successfully"}`.
`CVSS:3.1/AV:N/AC:L/PR:L/UI:N/S:U/C:N/I:L/A:N` = **4.3 (MEDIUM)**.
With environmental metrics for this deployment — endpoint unreachable, no UI client, zero calls in the log window — modified impact drops it to **LOW**. It becomes MEDIUM the day a UI is built.

**The 6 missing foreign keys and missing `UNIQUE(username)`.** No security impact. `UNIQUE(username)` is absent, but production has exactly **1 user** with 1 distinct username (verified: `users_total=1, distinct_usernames=1, distinct_ci=1`), so there is no current violation and no authentication-confusion risk today. It becomes a real integrity risk on the first multi-user deployment.

### 6.3 Availability and data-integrity assessment (plain)

| Dimension | Rating | Justification |
|---|---|---|
| **Production availability, today** | **NONE** | 0 5xx over 6.4 M log lines / 5 days. No affected endpoint is called. No background job touches the missing tables. |
| **Production data integrity, today** | **LOW** | No data loss: the missing tables never held data. Missing FKs permit orphan rows in `alerts` (56,945), `device_metrics`, `client_certificates`, `kb_article_views`, `agent_link_access_log` — no orphans were observed to have caused a fault, but constraint enforcement is absent. Missing `UNIQUE(username)` is inert at 1 user. |
| **Latent production risk** | **MEDIUM** | 10 endpoints hard-500 and 3 return false success the moment a UI or API consumer appears. Routes are published in `openapi.json`, so an external consumer could already discover them. |
| **Fresh install / new tenant / restore-to-empty** | **HIGH** | A fresh install today yields no `webhooks`, no patch, scheduling or MFA tables, and `devices.boot_time` as `bigint` where the code requires `timestamptz` — breaking device listing and enrolment. Any new tenant, dev bring-up, or restore-to-empty produces a materially different and partly broken system. DR from `pg_dump` is unaffected (it restores real schema, not migrations). |
| **Change safety / confidence in the migration system** | **HIGH concern** | `version=59, dirty=false` is not evidence that 59 migrations ran. There is no checksum, no per-version record, and a demonstrated history of manual version stamping. Every future change inherits this uncertainty until a drift gate exists. |

**Overall: MEDIUM.** Deliberately not rated higher: the features were never used, nothing is failing, no data was lost, and the deployment is a single-tenant 13-device install. Deliberately not rated lower: a security control is silently absent, the schema system cannot currently be trusted to tell the truth, and every fresh install is broken.

---

## 7. Remediation plan

### 7.1 Why forward-only

The repair **must** be a new migration at version 60+, not a version reset or a re-run of 42/43/44/47.

- **golang-migrate has no content checksums** — only `(version, dirty)`. Resetting the version to 41 and re-running would replay 42–59 against a database populated with 5 months of production data and 5 months of out-of-band DDL. Migrations 45–59 are **not idempotent** and were never written to run against this schema.
- **Never re-run a migration against a schema it was not written for.** This is the standard rule behind Flyway's distinction between `repair` (fix the *history table*) and a new corrective migration (fix the *schema*): Flyway explicitly does not re-execute already-applied migrations, and its documentation directs schema corrections to a new versioned migration.
- **Expand-and-contract / additive-only.** Ambler & Sadalage, *Refactoring Databases* (Addison-Wesley), and the Evolutionary Database Design pattern: applied migrations are immutable; corrections are new, additive, forward migrations. Editing a shipped migration only ever helps deployments that have not yet reached it.
- **Consistency with the repo's own precedent.** `000052_webhooks_fix_org_id_type.up.sql` already repaired exactly this class of defect forward-only, and documents the reasoning in its header: *"Editing a shipped migration in place is unsafe because deployments past v041 would skip 030 entirely. This forward-only fix is the correct mechanism."*
- **Google SRE change-management:** schema changes to a live system must be additive, individually reversible, and verifiable at the observable boundary.

The new migration must be **idempotent** (`CREATE TABLE IF NOT EXISTS`, `ADD COLUMN IF NOT EXISTS`, guarded constraint adds), because after step 2 a fresh install will already have created these objects at v42–47, and `000060` must be a clean no-op there.

### 7.2 Sequencing with PR #70

PR #70 is **necessary but insufficient and must not merge as-is** — it repairs the FK types but leaves the self-dropping `-- +migrate Down` blocks intact, so fresh installs still end up without the tables. Order matters:

```
Step 1  Backup                                    (production, no change)
Step 2  Amend PR #70 — fix the class              (repo)
Step 3  Merge PR #70                              (repo)
Step 4  Add migration 000060 — repair production  (repo)
Step 5  Add CI schema-drift gate                  (repo)
Step 6  Deploy to production                      (production, the only mutating step)
Step 7  Verify at the boundary                    (production, read-only)
```

`000060` must be authored **after** the PR #70 amendments land, so that the DDL it writes is byte-for-byte what the repaired `000042`/`000043`/`000044`/`000047` produce on a fresh install. Authoring it against the current (broken) tree would bake the drift in permanently.

### 7.3 Numbered steps

---

**Step 1 — Pre-change backup (production; no schema change)**

The repo documents `pg_dump` before major changes (`scripts/backup.sh:18-20`, `scripts/deploy-safe.sh` "Automatic backup before deployment"). Take both a full and a schema-only snapshot; the schema-only dump is the rollback comparison artefact.

```bash
ssh ohio_@192.168.1.20
TS=$(date -u +%Y%m%dT%H%M%SZ)
docker exec sentinel-postgres pg_dump -U sentinel -d sentinel -Fc \
  > ~/backups/sentinel-pre-000060-${TS}.dump
docker exec sentinel-postgres pg_dump -U sentinel -d sentinel \
  --schema-only --no-owner --no-privileges \
  > ~/backups/sentinel-pre-000060-${TS}.schema.sql
ls -la ~/backups/sentinel-pre-000060-${TS}.*
# Prove the dump is restorable before relying on it:
pg_restore --list ~/backups/sentinel-pre-000060-${TS}.dump | head
```

Do not proceed unless `pg_restore --list` succeeds.

---

**Step 2 — Amend PR #70: fix the sql-migrate directive class (repo)**

This is the **class fix** for §3.2. For each of the 4 affected files:

1. Move everything from `-- +migrate Down` onward into a proper `NNNNNN_<name>.down.sql`.
2. Delete the `-- +migrate Up` / `-- +migrate Down` markers from the `.up.sql`.

Files: `000041_webhooks`, `000042_patch_approvals`, `000043_mfa_totp`, `000044_script_scheduling`.

Then add a **regression guard** next to the existing `TestNoUnreviewedDuplicateTableDefinitions` in `server/pkg/database/migrations_test.go`:

```go
// TestNoSqlMigrateDirectivesInUpFiles guards the class: golang-migrate expresses
// direction through .up.sql/.down.sql filenames and treats "-- +migrate Down" as
// a comment, so a Down block inside an .up.sql executes as part of the up
// migration. 000041-000044 each created their tables and then dropped them in
// the same migration, and the migration still reported success.
```
The guard must fail if any `*.up.sql` matches `(?m)^\s*--\s*\+migrate\s+(Up|Down)`. **Mutation-test it**: re-add the directive to one file and confirm the test fails.

Two further defects surfaced by this investigation should be fixed in the same PR, or explicitly deferred with a written reason:

- **`000010_extended_device_info.up.sql:13`** — `boot_time BIGINT DEFAULT 0` contradicts the Go code (`EXTRACT(EPOCH FROM boot_time)`, `to_timestamp($14)`) and contradicts production. Fix forward-only: leave `000010` alone (production is past it) and have `000060` normalise the type only where it is still `bigint`, so fresh installs converge on `timestamptz`. **This is a fresh-install-fatal defect independent of everything else in this report.**
- **`agent_logs` shape** (§4.2) — PR #70 makes `000008` authoritative, diverging four column definitions from production. Decide deliberately: either accept and record it, or converge in `000060`.

---

**Step 3 — Merge PR #70** with the amendments and CI green.

---

**Step 4 — Author migration `000060_repair_missing_tables` (repo)**

`server/pkg/database/migrations/000060_repair_missing_tables.up.sql` — and a matching `.down.sql` (Step 8).

Content requirements:

1. Create the **8 missing tables** with `CREATE TABLE IF NOT EXISTS`, DDL copied verbatim from the *repaired* `000042`, `000043`, `000044`, `000047` — with `organization_id INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE`, matching `organizations.id` (SERIAL/integer). **No UUID FKs.**
2. Add the **6 missing columns** with `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` (5 `users` MFA columns, `alerts.metadata`).
3. Create the **missing indexes** with `CREATE INDEX IF NOT EXISTS`, including every index belonging to the 8 tables plus `idx_alerts_metadata`.
4. Add `webhooks`/`webhook_deliveries` guarded by `IF NOT EXISTS` — production has them, fresh installs (post-fix) have them; this is belt-and-braces for any deployment where the out-of-band creation never happened.
5. Add the **7 missing constraints** inside guarded `DO` blocks that first check `pg_constraint`, and — critically — **pre-validate** each FK before adding it, since production data was never constrained:
   ```sql
   -- refuse rather than corrupt: fail loudly if orphans exist
   IF EXISTS (SELECT 1 FROM alerts a LEFT JOIN devices d ON d.id = a.device_id
              WHERE a.device_id IS NOT NULL AND d.id IS NULL) THEN
       RAISE EXCEPTION 'alerts has orphan device_id rows; resolve before adding FK';
   END IF;
   ```
   Run these orphan checks **read-only against production first** (Step 4a) so the migration is not the place you discover them. With 56,945 alerts and 13 devices this is a real possibility.
6. Normalise `devices.boot_time` to `timestamptz` **only if** it is currently `bigint` (no-op on production, corrective on fresh).
7. Add a header comment stating what this migration repairs, why forward-only, and referencing this report.

**Step 4a — orphan pre-check (production, read-only), before finalising `000060`:**
```sql
SELECT 'alerts'          AS t, count(*) FROM alerts a LEFT JOIN devices d ON d.id=a.device_id WHERE a.device_id IS NOT NULL AND d.id IS NULL
UNION ALL SELECT 'device_metrics',      count(*) FROM device_metrics m LEFT JOIN devices d ON d.id=m.device_id WHERE d.id IS NULL
UNION ALL SELECT 'client_certificates', count(*) FROM client_certificates c LEFT JOIN devices d ON d.id=c.device_id WHERE c.device_id IS NOT NULL AND d.id IS NULL
UNION ALL SELECT 'kb_article_views',    count(*) FROM kb_article_views v LEFT JOIN kb_articles k ON k.id=v.article_id WHERE k.id IS NULL
UNION ALL SELECT 'agent_link_access_log', count(*) FROM agent_link_access_log l LEFT JOIN agent_installation_links i ON i.id=l.link_id WHERE i.id IS NULL
UNION ALL SELECT 'users_dup_username',  count(*) FROM (SELECT username FROM users GROUP BY username HAVING count(*)>1) x;
```
Every row must be `0`. If not, the FK additions for that table move to a separate follow-up with a documented data-cleanup step — do **not** add the constraint with `NOT VALID` and forget it.

**Step 4b — rehearse on a throwaway restored from the Step 1 dump**, never on production:
```bash
docker run -d --name sentinel-rehearsal -e POSTGRES_USER=sentinel \
  -e POSTGRES_PASSWORD=rehearsal -e POSTGRES_DB=sentinel \
  --tmpfs /var/lib/postgresql/data postgres:16-alpine
docker exec -i sentinel-rehearsal pg_restore -U sentinel -d sentinel --no-owner \
  < ~/backups/sentinel-pre-000060-${TS}.dump
# apply 000060, run the Step 7 verification queries, then:
docker rm -f sentinel-rehearsal
```
This is a purpose-created throwaway target. Rehearsal on production is not acceptable.

---

**Step 5 — CI schema-drift gate (the class fix for the whole incident)**

The gate belongs in `.github/workflows/ci.yml`, where `integration-tests` (line 140) already stands up a fresh `docker-compose.test.yml` stack with a tmpfs postgres on a self-hosted runner. Mechanism: **migrate-then-dump-then-diff against a committed baseline.**

New job `schema-drift`, `needs: [integration-tests]`:

1. Bring up the fresh test stack; the backend runs `db.Migrate()` from empty.
2. Dump the resulting schema canonically:
   ```bash
   docker compose -f docker-compose.test.yml exec -T postgres-test \
     pg_dump -U sentinel -d sentinel --schema-only --no-owner --no-privileges --no-comments \
   | grep -vE '^--|^$|_2026_[0-9]{2}|schema_migrations' \
   | sed -E 's/[[:space:]]+$//' \
   > /tmp/fresh.sql
   ```
   Filter runtime date partitions and the tracking table — the documented noise from §2.
3. `diff -u server/pkg/database/schema.baseline.sql /tmp/fresh.sql` — **fail the job on any difference.**
4. The baseline is a committed artefact. Any intentional schema change requires regenerating and committing it **in the same PR**, which makes every schema change visible in review. This is the standard practice used by sqldef/Atlas/`pg_dump`-baseline setups and by Rails' committed `schema.rb`.

Because canonical `pg_dump` output is ordering-stable for a given server version, pin the postgres image in `docker-compose.test.yml` to the production major (`postgres:16-alpine`) so CI and production compare like with like.

**Second gate — production vs baseline (catches the failure this report describes, which a fresh-vs-baseline gate alone would not have caught):**

A scheduled job (weekly) that:
1. Pulls the most recent production `pg_dump --schema-only` from the existing backup rotation (`scripts/backup.sh`) — **never connects to production**.
2. Normalises it identically.
3. Diffs against `schema.baseline.sql` and alerts on drift via the existing ntfy `infra-alerts` channel.

This is the check that would have caught the February bulk-stamp within a week instead of 161 days. It should be **warn-only for its first two runs** to establish the known-drift allowlist (the out-of-band objects in §4.2), then **enforcing** — with the flip date and preconditions recorded in the PR that adds it, not left as an open-ended "warn" mode.

**Third guard — assert version means what it says.** Add a startup check, or a CI assertion, that after `Migrate()` every table the migrations declare actually exists. A cheap version: a test that parses `CREATE TABLE (IF NOT EXISTS )?(\w+)` out of all `*.up.sql`, subtracts anything dropped by a later migration, and asserts each survives in the freshly migrated CI database. That single assertion would have failed on 2026-02-08, the day the self-dropping migrations were authored.

---

**Step 6 — Deploy (the only mutating production step)**

Deploy through the normal path so the backend applies `000060` at startup via `db.Migrate()`. Confirm the deployment target before deploying — a `git checkout`/`pull` in the deploy tree *is* a deployment. Capture the migrate log line:
```bash
ssh ohio_@192.168.1.20 'docker logs sentinel-backend 2>&1 | grep "\[migrate\]" | tail -5'
# expect: [migrate] applied; current version 60
```
If the migration fails, golang-migrate sets `dirty=true` and blocks. **Do not clear the flag by hand** — that is the practice that produced this incident. Go to Step 8.

---

**Step 7 — Boundary verification (production, read-only)**

Verify at the observable boundary — the catalog and the wire — not by re-reading the migration file.

**7a. Version:**
```sql
SELECT version, dirty FROM schema_migrations;
-- expect exactly: 60 | f
```

**7b. All 8 tables exist:**
```sql
SELECT t AS table_name, to_regclass('public.'||t) IS NOT NULL AS exists
FROM unnest(ARRAY['patch_policies','patch_approvals','device_patch_assignments',
                  'patch_installations','mfa_events','script_schedules',
                  'script_executions','usb_file_transfers']) AS t
ORDER BY 1;
-- every row must be exists = t
```

**7c. FK types are INTEGER, not UUID — the specific defect:**
```sql
SELECT c.table_name, c.column_name, c.data_type
FROM information_schema.columns c
WHERE c.table_schema='public' AND c.column_name='organization_id'
ORDER BY 1;
-- every row must be data_type = 'integer'. Any 'uuid' is a failed repair.
```

**7d. Foreign keys actually resolve to `organizations(id)`:**
```sql
SELECT rel.relname, con.conname, pg_get_constraintdef(con.oid)
FROM pg_constraint con
JOIN pg_class rel ON rel.oid = con.conrelid
WHERE rel.relname IN ('patch_policies','patch_approvals','device_patch_assignments',
                      'patch_installations','mfa_events','script_schedules',
                      'script_executions','usb_file_transfers')
  AND con.contype = 'f'
ORDER BY 1,2;
```

**7e. Missing columns present:**
```sql
SELECT column_name, data_type, is_nullable, column_default
FROM information_schema.columns
WHERE table_schema='public'
  AND ((table_name='users' AND column_name IN
        ('totp_enabled','totp_secret','totp_verified_at','backup_codes','mfa_required'))
    OR (table_name='alerts' AND column_name='metadata'))
ORDER BY 1;
-- expect 6 rows
```

**7f. Constraints and indexes:**
```sql
SELECT conname FROM pg_constraint WHERE conname IN
 ('users_username_key','alerts_device_id_fkey','device_metrics_device_id_fkey',
  'client_certificates_device_id_fkey','kb_article_views_article_id_fkey',
  'agent_link_access_log_link_id_fkey');
SELECT indexname FROM pg_indexes WHERE schemaname='public' AND indexname='idx_alerts_metadata';
```

**7g. Endpoints actually work — the wire, not the catalog.** A catalog check proves the tables exist; it does not prove the handlers work. Authenticate and exercise each surface:
```bash
TOKEN=...   # from POST /api/auth/login
BASE=https://<sentinel-host>/api
for p in patch-policies patch-approvals schedules executions; do
  printf '%-18s ' "$p"
  curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $TOKEN" "$BASE/$p"
done
# expect 200 on all four (empty arrays), NOT 500
curl -s -H "Authorization: Bearer $TOKEN" "$BASE/mfa/status"      # expect 200
curl -s -H "Authorization: Bearer $TOKEN" "$BASE/export/users"    # expect 200, not 500
```
Then confirm a write round-trips, since `SELECT` on an empty table passes even with a wrong FK type — a wrong `organization_id` type only fails on `INSERT`:
```bash
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"verification-probe","description":"delete me"}' "$BASE/patch-policies"
# expect 201/200, then DELETE it. THIS is the test that proves the FK type is right.
```
Do this on the Step 4b rehearsal instance first; on production, delete the probe row and record it.

**7h. Confirm the absence of new errors — positively:**
```bash
ssh ohio_@192.168.1.20 \
 'sudo grep -ac "42P01\|does not exist" \
    $(docker inspect --format "{{.LogPath}}" sentinel-backend)'
# expect 0, and confirm the grep works by also grepping a pattern known to be present
```
Track *unexamined* separately from *examined and clean*: a zero from a grep that silently failed is not a pass.

---

**Step 8 — Rollback**

Three tiers, least to most destructive.

**Tier 1 — migration down (preferred).** `000060_repair_missing_tables.down.sql` drops **only** what `000060` created, in reverse dependency order, using `IF EXISTS`, and drops the added constraints/columns. It must **not** drop `webhooks`/`webhook_deliveries` (pre-existing in production) and must **not** revert `devices.boot_time` (production's `timestamptz` is correct). Execute via `migrate down 1` equivalent — or, since the app runs `Up` only, by deploying the previous image and running the down SQL explicitly. Verify with Step 7a expecting `59 | f`.

**Tier 2 — dirty flag.** If `000060` fails mid-way, golang-migrate marks `dirty=true` at version 60 and blocks. Because `000060` is wrapped per-migration in a transaction, the DDL will have rolled back; the flag is the only residue. Resolve by **fixing the migration and redeploying**, not by hand-clearing the flag. If the flag must be cleared, do it as an explicit, logged, reviewed action with the reason recorded — the undocumented hand-clearing precedent (`~/migration-058-recovery.sql`) is part of how this incident stayed invisible.

**Tier 3 — restore from the Step 1 dump.** Only if the schema is left inconsistent:
```bash
docker compose stop backend
docker exec -i sentinel-postgres pg_restore -U sentinel -d sentinel \
  --clean --if-exists --no-owner < ~/backups/sentinel-pre-000060-${TS}.dump
```
Data loss window = time since Step 1. Because `000060` is purely additive and touches no application data, Tier 3 should never be needed; it exists so that the decision is pre-made rather than improvised.

**Rollback trigger conditions:** any 5xx on previously-working endpoints; `dirty=true` persisting after redeploy; any orphan-check `RAISE EXCEPTION`; verification 7c returning `uuid`.

---

## 8. What I could not determine — consolidated

1. **Who** executed the 2026-02-25 bulk `INSERT`, and its exact SQL. Shell history has rotated; no deploy script on the box references migrations. Only PostgreSQL server logs from that date (if `log_statement` was on — not verified) or the operator's session transcript would settle it.
2. **Whether the operator knew** migrations 42/44 had failed, or bulk-stamped 34–48 blind after fixing only `034_usb_devices.sql`.
3. **Why `webhooks` / `webhook_deliveries` exist in production** despite `000041`'s self-drop, which was present from the file's first commit. Out-of-band manual creation is the best-supported inference — consistent with the documented practice and with `000052` needing to convert a UUID column — but I found no direct evidence and do not assert it.
4. **Provenance of `devices.boot_time` (timestamptz), `devices.last_force_update_at`, `idx_agent_health_status_v2`, and the `varchar` length differences** in production. No migration produces them. Presumed out-of-band; not established.
5. **Backend logs before 2026-07-31T11:43Z.** The prior container's log was not located, so "zero `42P01`, zero 5xx" covers 5 days of a 161-day exposure. Given zero UI clients and zero background jobs, I assess the earlier period as very likely identical — but that is inference, not observation.
6. **Whether any non-browser API consumer outside NEXUS calls the affected routes.** Only this container's logs were examined. The routes are published in `openapi.json`.
7. **Whether orphan rows exist** that would block the FK additions in Step 4a. I did not run the orphan queries, because doing so was not needed for the report and Step 4a is the right place for them. Treat those five tables as **unexamined**, not clean.
8. **The full drift inventory is authoritative for tables, columns, indexes and constraints only.** I did not compare functions, triggers, sequences, views, extensions, or column-level privileges. Those remain **unexamined**; the CI gate in Step 5 covers them from the day it lands because `pg_dump --schema-only` includes them.

---

## 9. Recommended immediate actions

| # | Action | Owner | Urgency |
|---|---|---|---|
| 1 | Amend PR #70 to remove the 4 self-dropping `-- +migrate Down` blocks + add the mutation-tested guard. **Without this, PR #70 does not fix fresh installs.** | dev | **Now** — blocks the PR |
| 2 | Fix `000010` `boot_time` BIGINT → TIMESTAMPTZ for fresh installs | dev | **Now** — fresh-install-fatal |
| 3 | Land the CI schema-drift gate | dev | High — this is the class fix |
| 4 | Author + rehearse `000060`, then deploy | dev | Medium — nothing is failing today |
| 5 | Decide on the `agent_logs` shape divergence PR #70 introduces | dev | Medium |
| 6 | Run the Step 4a orphan checks | dev | Before `000060` |
| 7 | Note in `SECURITY.md` that MFA/TOTP is non-functional on this deployment until `000060` ships | dev | High — silent control gap |

---

*Investigation performed read-only against production. Two throwaway PostgreSQL 16.13 containers were created and destroyed on NEXUS; no production schema, data, or repository state was modified, and nothing was pushed.*

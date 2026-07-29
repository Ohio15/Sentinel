# WAN Sentinel — Patriot Companies ISP-line monitoring

Localizes a Patriot site outage to the correct layer — AT&T circuit vs AT&T
gateway vs USG vs power — from evidence, instead of waiting for the ISP.
Born from the 2026-07-29 outage (shared-brain incident `858e8538`), where
remote diagnosis could confirm *that* the site was down but not *whose fault
it was*. Plan of record: shared-brain decision `f3181b09`.

## Phase 1 — external witness on NEXUS (DEPLOYED 2026-07-29)

Commits `abdfb58`, `116cc91`. Observation vantage entirely outside the
site's hardware chain:

- `blackbox-exporter` `icmp` module (unprivileged ping sockets via the
  `net.ipv4.ping_group_range` sysctl; exporter stays non-root with
  `no-new-privileges`).
- Prometheus job `blackbox-wan-patriot`: ICMP probe of the site WAN IP
  `104.189.209.197` (AT&T) every 30s.
- `configs/prometheus/rules/wan-patriot.yml`:
  - `PatriotWANDown` (critical, 3m) — includes the on-call discriminator
    runbook in its description.
  - `PatriotWANProbeAbsent` (warning, 15m) — fires if the witness itself
    stops being scraped; silence must never read as health.
- Routing: existing alertmanager → alertmanager-ntfy → ntfy topic
  `cortex-alerts`.

Verified live against the real 2026-07-29 outage: `probe_success=0`,
`PatriotWANDown` fired and reached Alertmanager.

**Standing caveat:** the target assumes the WAN IP is static. If
`PatriotWANDown` stays active while the site verifiably has internet, the
AT&T gateway was likely reissued a new IP — update the target in
`configs/prometheus/prometheus.yml`.

## Phase 2 — on-site probe (DESIGN; blocked on site access)

A probe at the site that observes the demarc itself and pushes an
authenticated heartbeat out, so NEXUS gets both detection (heartbeat gone)
and localization (what the probe saw last).

### Probe host

Recommended: dedicated low-power box (Raspberry Pi class, ~$60) on the LAN.
The Admin VM (`DESKTOP-R08HDUK`) is rejected as permanent home: it hosts the
Tailscale subnet router and shares power/boot failure modes with the things
being monitored — exactly the coupling that blinded us on 2026-07-29. The
Admin VM is acceptable as an interim heartbeat sender only.

### Probe loop (every 30–60s, local JSONL ring buffer)

1. **Demarc**: scrape the AT&T gateway local status page — Broadband
   Connection state, uptime, and line/optical signal levels + error
   counters. This is AT&T's own equipment declaring the line up or down,
   and signal-level trending can flag a degrading line before it fails.
2. **First hop**: ping the first AT&T hop past the gateway (discovered via
   traceroute at install time, re-verified periodically).
3. **Anchors**: ping 1.1.1.1 and 8.8.8.8.
4. **Heartbeat**: POST the latest probe matrix to the Sentinel backend at
   `https://sentinelrmm.us` (authenticated; see below). On WAN restore,
   upload the buffered timeline — post-hoc proof of when the line dropped
   per the AT&T gateway's own status.

### Server side (Sentinel backend)

- Authenticated heartbeat ingest endpoint (token per probe, same secret
  discipline as agent enrollment — NOT an unauthenticated pushgateway).
- Exported via `/metrics`: heartbeat timestamp, broadband-up gauge,
  first-hop/anchor reachability, signal-level gauges.
- New rules: `PatriotHeartbeatMissing` (`time() - <heartbeat metric> >
  300`; inherently silent until the first heartbeat ever arrives), plus a
  combined-state rule that pairs with `PatriotWANDown` to emit the
  localized diagnosis (both down → site-wide; witness down + heartbeat
  alive → asymmetric/inbound path or IP change; heartbeat reports
  broadband-down → ISP fault, with timestamp evidence for the AT&T ticket).

### Blockers (need the site back online / on-site eyes)

| # | Blocker | Why it matters |
|---|---------|----------------|
| 1 | AT&T gateway model (BGW210 / BGW320 / other) and LAN address | Status-page URL and scrape format differ per model; no scraper can be written correctly without it |
| 2 | IP passthrough mode yes/no | Changes the gateway's reachable LAN address and what the USG sees |
| 3 | Static IP confirmation with AT&T | Decides whether the Phase 1 ping target is durable or needs dynamic tracking |
| 4 | Probe host decision (Pi purchase vs interim Admin VM) | Pi decouples the monitor from monitored failure domains |

### Optional tier

LTE out-of-band (USB LTE dongle on the probe) for real-time alerting and
remote eyes *during* an outage. Without it, detection is still immediate
(missing heartbeat seen from NEXUS); only the detailed timeline waits for
restore.

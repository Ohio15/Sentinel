# Security Policy

## Supported Versions

Security fixes are released on the latest minor-version line. Older versions
receive fixes only for critical issues and only when a straightforward backport
is possible.

| Version | Supported |
|---------|-----------|
| Latest stable (1.76.x) | ✓ |
| < 1.76 | ✗ |

## Reporting a Vulnerability

**Please do not open public GitHub issues for security reports.**

Email: **security@sentinelrmm.us**

Include:

- Affected version(s) and commit hash
- Reproduction steps or proof of concept
- Your assessment of impact and any mitigations you've already identified
- Whether you intend to publicly disclose, and on what timeline

You'll receive an acknowledgement within 72 hours. We aim to have a triage
assessment back within 7 days and a fix or mitigation plan within 30 days for
high-severity findings. Lower-severity issues may take longer.

## Scope

In scope:

- Authentication, authorization, and session management (server and agent)
- Agent-to-server transport security (mTLS, WebSocket, gRPC dataplane)
- Command execution paths (script library, remote command execution)
- Installer, watchdog, and tamper-protection logic
- Update-pipeline integrity (signed binaries, manifest handling)
- SQL injection, XSS, CSRF, SSRF, and similar web-application issue classes
- Privilege escalation via the agent or installer

Out of scope:

- Denial-of-service via resource exhaustion on self-hosted deployments
- Social engineering of operators
- Physical attacks on endpoints running the agent
- Issues in third-party dependencies already addressed by upstream fixes
  (please report those to the upstream project; we track them via Dependabot)

## Safe Harbor

We will not pursue legal action against security researchers who:

- Report findings promptly and privately to the address above
- Do not exfiltrate data beyond what is necessary to demonstrate the issue
- Do not degrade service availability for other operators or users
- Give us a reasonable window (typically 90 days) before public disclosure

## Past Audits

See `ARCHITECTURE_REVIEW.md` and `QA_REPORT.md` in this repository for prior
hardening notes. A full finding log is maintained internally.

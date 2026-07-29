#!/usr/bin/env bash
#
# check-version-consistency.sh
# -----------------------------------------------------------------------------
# Guard against version skew across the files that MUST stay in lockstep.
#
# Source of truth:  agent/version.json  (the file sentinel-backend serves to
# agents as "latest available"). Every other lockstep file is compared to it.
#
# Scope note: root package.json, frontend/package.json and mobile/package.json
# are intentionally NOT checked. Root package.json tracks the SERVER/repo
# version line, which legitimately diverges from the agent line on server-only
# releases (e.g. v1.77.15-17 were server-only; production server reached 1.78.0
# while agents were on 1.77.x). Only the agent binary/release files are
# lockstep. See CLAUDE.md "Version Files".
#
# Exit 0 = all in sync. Exit 1 = drift detected (prints a table).
# Run locally:  bash scripts/check-version-consistency.sh
# In CI: wired as a fast, blocking job (see .github/workflows/ci.yml).
# -----------------------------------------------------------------------------
set -euo pipefail
cd "$(dirname "$0")/.."

fail=0

# --- extract the canonical version (no jq dependency) ---
sot=$(grep -m1 '"version"' agent/version.json | sed -E 's/.*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')
if [[ -z "${sot:-}" ]]; then
  echo "ERROR: could not read source-of-truth version from agent/version.json" >&2
  exit 2
fi
echo "Source of truth (agent/version.json): $sot"
echo "-----------------------------------------------------------"

# helper: extract a "version": "x.y.z" style value from a JSON file
json_ver() { grep -m1 '"version"' "$1" 2>/dev/null | sed -E 's/.*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/'; }
# helper: extract Go  `Version = "x.y.z"` (var or const) literal
go_ver()   { grep -m1 -E 'Version[[:space:]]*=[[:space:]]*"' "$1" 2>/dev/null | sed -E 's/.*"([^"]+)".*/\1/'; }

check() { # name  actual
  local name="$1" actual="$2"
  if [[ "$actual" == "$sot" ]]; then
    printf '  OK   %-42s %s\n' "$name" "$actual"
  else
    printf '  DRIFT %-41s %s  (expected %s)\n' "$name" "${actual:-<missing>}" "$sot"
    fail=1
  fi
}

check "release/agent/version.json"                    "$(json_ver release/agent/version.json)"
check "installers/version.json"                       "$(json_ver installers/version.json)"
check "agent/cmd/sentinel-agent/main.go (fallback)"   "$(go_ver agent/cmd/sentinel-agent/main.go)"
check "agent/cmd/sentinel-watchdog/main.go (fallback)" "$(go_ver agent/cmd/sentinel-watchdog/main.go)"

echo "-----------------------------------------------------------"
if [[ "$fail" -ne 0 ]]; then
  echo "FAIL: version drift detected. Run scripts/release.ps1 to re-sync, or fix the"
  echo "      offending file(s) to match agent/version.json ($sot)."
  exit 1
fi
echo "PASS: all lockstep version files match $sot"

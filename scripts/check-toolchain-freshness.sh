#!/usr/bin/env bash
# check-toolchain-freshness.sh — detects a stale go.mod `toolchain` pin.
#
# govulncheck (run in `make quality-gates`) only flags vulnerabilities in code
# actually reachable from this module's own call graph — it does not treat an
# outdated Go toolchain itself as a finding unless a reachable stdlib function
# happens to be the vulnerable one. go.mod's `toolchain` pin accumulated 13
# unpatched CVEs before anyone noticed, entirely undetected by that gate. This
# script closes that gap directly: Go patch releases (the third number) are
# security/bugfix-only by policy, so a pin that isn't the latest patch of its
# own major.minor branch is a real, actionable drift signal on its own,
# independent of whether govulncheck's reachability analysis happens to flag
# anything.
#
# Not wired into pre-commit/pre-push: it needs network access to go.dev, and a
# transient fetch failure should not block every local commit. Run it
# on a schedule instead (see .github/workflows/toolchain-freshness.yml).
set -euo pipefail

mod_file="${1:-go.mod}"

pinned="$(grep -E '^toolchain[[:space:]]+go[0-9]' "$mod_file" | awk '{print $2}' || true)"
if [ -z "$pinned" ]; then
  go_directive="$(grep -E '^go[[:space:]]+[0-9]' "$mod_file" | awk '{print $2}' || true)"
  if [ -n "$go_directive" ]; then
    pinned="go$go_directive"
  fi
fi
if [ -z "$pinned" ]; then
  echo "FAIL toolchain-freshness: could not find a 'toolchain' or 'go' directive in $mod_file"
  exit 1
fi

releases_file="$(mktemp)"
trap 'rm -f "$releases_file"' EXIT
curl -fsSL --max-time 15 'https://go.dev/dl/?mode=json' -o "$releases_file"

result="$(python3 - "$pinned" "$releases_file" <<'PY'
import json, re, sys

pinned, releases_path = sys.argv[1], sys.argv[2]
with open(releases_path) as f:
    data = json.load(f)
stable = sorted({d["version"] for d in data if d.get("stable")})

def branch(v):
    m = re.match(r"go(\d+)\.(\d+)", v)
    return (int(m.group(1)), int(m.group(2))) if m else None

pinned_branch = branch(pinned)
if pinned_branch is None:
    print("MALFORMED " + pinned)
    sys.exit(0)

if pinned in stable:
    print("OK " + pinned)
    sys.exit(0)

same_branch = [v for v in stable if branch(v) == pinned_branch]
if same_branch:
    print("STALE-PATCH " + same_branch[-1])
    sys.exit(0)

newer = [v for v in stable if branch(v) > pinned_branch]
if newer:
    print("UNSUPPORTED-BRANCH " + newer[0])
    sys.exit(0)

# Pinned branch is newer than every branch the feed currently lists as
# stable (e.g. a pre-release toolchain) — nothing actionable to report.
print("OK " + pinned)
PY
)"

status="${result%% *}"
latest="${result#* }"

case "$status" in
  OK)
    echo "OK toolchain-freshness: $pinned is current"
    ;;
  STALE-PATCH)
    echo "FAIL toolchain-freshness: $mod_file pins $pinned, but $latest is the latest patch on that branch (Go patch releases are security/bugfix-only)"
    exit 1
    ;;
  UNSUPPORTED-BRANCH)
    echo "FAIL toolchain-freshness: $mod_file pins $pinned, whose major.minor branch no longer receives security patches; $latest is the oldest still-supported release"
    exit 1
    ;;
  *)
    echo "FAIL toolchain-freshness: could not parse $pinned as a Go version"
    exit 1
    ;;
esac

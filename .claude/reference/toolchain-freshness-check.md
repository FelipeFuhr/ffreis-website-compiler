## Toolchain freshness check (`scripts/check-toolchain-freshness.sh`, `make check-toolchain`)

Detects a stale `go.mod` `toolchain` pin. `govulncheck` (part of `make quality-gates`,
run on every push via lefthook's pre-push hook) only flags vulnerabilities reachable
from this module's own call graph — an outdated Go toolchain is not itself a finding
unless a reachable stdlib function happens to be the vulnerable one. `go.mod`'s
toolchain pin once accumulated 13 unpatched CVEs before anyone noticed, entirely
undetected by that gate.

This script closes that gap directly: it fetches `https://go.dev/dl/?mode=json` and
compares `go.mod`'s pinned version against the latest stable release on the same
major.minor branch. Go patch releases are security/bugfix-only by policy, so anything
but the latest patch of the pinned branch is itself the actionable signal — independent
of whether `govulncheck`'s reachability analysis happens to flag anything. It also
catches a pin on a major.minor branch no longer listed as supported (older than the two
currently-supported branches).

**Not wired into pre-commit/pre-push** — it needs network access to `go.dev`, and a
transient fetch failure should not block every local commit or push. Instead:

- `.github/workflows/toolchain-freshness.yml` runs it on a weekly schedule (opens a
  GitHub issue on failure), on `workflow_dispatch`, and on any PR/push touching
  `go.mod` itself (for fast feedback on a toolchain bump introduced in that change).
- Run `make check-toolchain` locally any time to check on demand.

Exit codes: `0` (current), `1` (stale patch, unsupported branch, or missing/malformed
`toolchain`/`go` directive — each case prints which).

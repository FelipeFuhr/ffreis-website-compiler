## Versioning (and why the `stable` pointer is retired)

- **`main` is the development branch.** Every merge lands here first.
- **Releases are semver tags cut by [release-please](https://github.com/googleapis/release-please)**
  (`release-type: simple` — a Go module has no manifest file with a version
  field to bump, so release-please only tags and generates a changelog; it
  does not edit `go.mod`). The `release-please` job in
  `.github/workflows/devops-automation.yml` runs on every push to `main` and
  opens/updates a release PR; merging that PR cuts the tag. Config:
  `.release-please-config.json`; version state: `.release-please-manifest.json`.
- **The manifest records the last version that actually shipped, so it starts
  at `0.0.0`.** It was originally seeded at `0.1.0` while the repo had zero
  tags, which asserts a v0.1.0 release that never happened — release-please
  then skips straight past it, making that tag permanently uncuttable. Only
  reset a manifest like this while **no** tags exist; rewriting it over live
  tags re-cuts them.
- **Every job in `devops-automation.yml` must target GitHub-hosted runners**
  (`runner: '["ubuntu-latest"]'`), not the homelab pool. The self-hosted fleet
  registers at org scope to `ffreis-org` and this repo is owned by the separate
  `FelipeFuhr` personal account; a runner binds to exactly one owner, so a
  `'["self-hosted","local"]'` request here is structurally unservable whatever
  the labels say. Such a job queues for GitHub's 24-hour maximum and is then
  auto-cancelled with `runnerName: null`. That — not a missing token, config
  file, trigger or permission — is why this repo had zero releases for its
  first months despite release-please being correctly configured in every other
  respect. **A cancelled automation run with no runner name is this bug, not a
  flake; do not re-run it.**
- **`stable` is RETIRED (2026-10-17).** The branch still exists, frozen at
  `d1526ab`, so no existing clone breaks — but **nothing should pin it, ever
  again**. It was the fleet's "last-known-good" pointer, moved only by a
  private consumer's `promote-compiler.yml` on a green fleet-wide build. That
  promotion never once succeeded (9 runs, all failed; its narrowly-scoped PAT
  was never provisioned, and a PAT cannot be minted via the API). So the
  pointer sat frozen from the day it was created — 17 commits behind v1.0.1,
  predating the `css/pages/<page>.css` per-page CSS convention entirely.
  A site pinned to it built **green and shipped unstyled pages**. The
  promotion workflow has been removed and every consumer now pins a release.
- **Consumers must pin an exact release tag — never `main`, never `stable`.**
  Floating on `main` means every commit here, including work-in-progress,
  reaches production consumers immediately. Pinning `stable` means freezing on
  a commit nobody chose.
- **Keep the two refs a consumer has EQUAL**: its `compiler.ref` in the fleet
  inventory (which drives the real deploy) and its own
  `WEBSITE_COMPILER_REF` / workflow `compiler-ref` (which drives CI). When they
  disagree, CI gates a different artefact than production ships — the same
  class of failure `stable` caused, arrived at another way.
- **Canary / early access to an unreleased change** happens through the
  consumer's own `WEBSITE_COMPILER_REF` repo variable override, not by editing
  the pinned ref. Point the variable at the SHA or tag under test, verify, then
  bump the pin to the release that carries it — **and delete the override**. A
  canary nobody removes holds that repo on an old compiler indefinitely, which
  is what happened to ffreis-forma: its variable was still set to a SHA from
  four releases earlier, silently overriding a correct Makefile pin.

### Implication for compiler development

Consumers pin releases, so a breaking or newly-fatal validation change merged
to `main` does not reach anyone until someone bumps a pin. There is no longer a
promotion gate absorbing it, and no stuck promotion to notice either: the cost
lands on whoever does the next bump, site by site.

What replaces the gate is **measurement before merge**. The deployer's
`verify-compiler.yml` builds every fleet site against any candidate ref with no
AWS credentials and no deploy step:

```
gh workflow run "Verify Compiler" -R FelipeFuhr/ffreis-website-deployer \
  --field compiler_ref=<branch, tag or SHA>
```

Run it against the candidate and against the current pin, then diff the
per-site table. That turns "will this break the fleet" into a measured answer —
used that way on 2026-10-17 to show a candidate changed the state of exactly
one site out of 24.

Because of that, **ship a breaking or newly-fatal check with an opt-in or
warning period where practical** — e.g. a flag that defaults to warn-only for
one release, or a documented grace-period release before the check becomes
fatal by default — rather than making it fatal in the same commit that
introduces it. This gives consumers time to fix their sites before the check
starts blocking promotion outright, instead of turning every affected site's
fix into a precondition for anyone bumping their pin at all.

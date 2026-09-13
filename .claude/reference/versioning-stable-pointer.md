## Versioning & the stable pointer

- **`main` is the development branch.** Every merge lands here first.
- **Releases are semver tags cut by [release-please](https://github.com/googleapis/release-please)**
  (`release-type: simple` — a Go module has no manifest file with a version
  field to bump, so release-please only tags and generates a changelog; it
  does not edit `go.mod`). The `release-please` job in
  `.github/workflows/devops-automation.yml` runs on every push to `main` and
  opens/updates a release PR; merging that PR cuts the tag. Config:
  `.release-please-config.json`; version state: `.release-please-manifest.json`
  (starts at `0.1.0` — this repo had zero prior tags).
- **`stable` is the fleet-verified last-known-good pointer**, created once at
  a fixed commit. It moves **only** when a private consumer's
  `promote-compiler.yml` workflow confirms a green fleet-wide build against a
  release candidate. Nothing in this repo pushes to `stable` directly — there
  is no local `make` target or script that touches it.
- **Consumers must pin `stable` or an exact release tag — never `main`.**
  Floating on `main` means every commit here, including work-in-progress and
  not-yet-fleet-verified changes, reaches production consumers immediately —
  which is exactly the failure mode this pointer exists to close.
- **Canary / early access to an unreleased change** happens through the
  consumer's own `WEBSITE_COMPILER_REF` repo variable override, not by
  editing the pinned ref in inventory or CI files. Point the variable at the
  SHA or tag under test, verify, then let the change reach `stable` through
  the normal promotion path — never hand-edit a pin to jump the queue.

### Implication for compiler development

Because `stable` — not `main` — is what the fleet actually consumes, a
breaking or newly-fatal validation change merged to `main` no longer risks
instant fleet breakage: the promotion gate (a green fleet-wide build) absorbs
it before it ever reaches `stable`. It **will**, however, block promotion
until every affected site is fixed. A stuck promotion is now the expected
failure mode for a breaking change — not a silent production break — but it
is still a real cost paid by whoever is waiting on the next promotion.

Because of that, **ship a breaking or newly-fatal check with an opt-in or
warning period where practical** — e.g. a flag that defaults to warn-only for
one release, or a documented grace-period release before the check becomes
fatal by default — rather than making it fatal in the same commit that
introduces it. This gives consumers time to fix their sites before the check
starts blocking promotion outright, instead of turning every affected site's
fix into a precondition for the next release reaching `stable` at all.

---
paths:
  - "internal/versioncmd/**"
  - "internal/cli/**"
---

## `version` command — the capability manifest consumers feature-detect against

```
website-compiler version           # just the semver build version
website-compiler version --json    # full machine-readable capability manifest
make version                       # the manifest this working tree would report
```

`version --json` emits `{version, commands, capabilities, sections,
template_functions}`. It exists because a consuming site has no other way to
ask "does the compiler ref my CI pins actually support the feature I just
adopted?" — and the failure mode without it is silent rather than loud: a site
adopting the `css/pages/<pageName>.css` convention against a ref that predates
it builds **green** and ships every page unstyled.

**Internal package:** `internal/versioncmd/versioncmd.go`

- `Version` is stamped at build time via
  `-ldflags "-X ffreis-website-compiler/internal/versioncmd.Version=<semver>"`.
  The `install` and `version` Make targets wire this from `git describe` —
  deliberately not Go's own `-buildvcs` stamping, which reports a stale commit
  when built from a linked git worktree. Unset, it falls back to the Go
  pseudo-version, then to `0.0.0-dev` (container builds have no `.git`).
- `sections` comes from `buildcmd.SectionNames()`; `template_functions` from
  `sitegen.TemplateFunctionNames()`, which introspects the package-level
  `templateFuncMap` that template parsing itself uses — one source of truth, so
  the manifest cannot drift from what is actually registered.
- `commands` is the **one hand-maintained list**, because `internal/cli` imports
  this package to dispatch `version` and the reverse import would cycle. It is
  guarded mechanically rather than by comment: `TestCommands_MatchCLISwitch`
  parses `internal/cli/cli.go`'s dispatch switch with `go/ast` and fails when
  the two disagree. **Adding a subcommand without adding it to `commands` fails
  the build.** Fix the list, never loosen the test — an inaccurate manifest is
  worse than no manifest, because consumers act on it.
- `capabilities` inclusion criterion: an entry belongs there when a consumer
  could build against a ref lacking it and get a green build with wrong output.
  Long-standing unconditional transforms that predate every live consumer pin
  (LQIP, SVG inlining, fingerprinting) are deliberately omitted — enumerating
  them invites drift without answering a real feature-detection question.

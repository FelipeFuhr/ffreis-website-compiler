# Adopting `ffreis-org/ffreis-seo-core`

Four `internal/` packages here were lifted into the fleet's new SEO library and
were expected to come back as a pure import swap. Two of them did. Two did not,
and this file records exactly why, so the next person does not re-measure it.

| package here | library package | status |
| --- | --- | --- |
| `internal/outputcheck` | `pkg/outputcheck` | **adopted** — byte-identical |
| `internal/sitemap` | `pkg/sitemap` | **adopted** — byte-identical |
| `internal/conventions` | `pkg/conventions` | **not adopted** — library is behind |
| `internal/linkcheck` | `pkg/linkcheck` | **not adopted** — library API is narrower |

## Why `go test ./...` cannot answer this question

Every test for all four packages lives *inside* the package. Deleting the
package deletes its tests, so the module suite loses 191 test results and then
reports green — against any library at all, correct or not. Measured: 914 test
results before, 723 after a full four-package swap, **zero failures**, because
the only tests that could have failed were the ones removed.

The check that does work is the opposite direction: take this repo's test files
*unchanged* (import path aside) and run them against the library package. That
is what produced the table above.

| package | this repo's tests, run against the library |
| --- | --- |
| `outputcheck` | 25/25 pass |
| `sitemap` | fuzz-seed corpus passes; `sitemap.go` diffs clean |
| `conventions` | 87/87 pass — *except* the 10 `resolvable-asset-refs` tests, which do not compile |
| `linkcheck` | does not compile: 34 tests, every `Validate`/`ValidateAndReport` call site |

## `pkg/linkcheck` — the library dropped a parameter

`Validate` and `ValidateAndReport` here take a fourth argument the library does
not have:

```go
// here
func Validate(outDir, basePath string, siblingBasePaths, runtimePrefixes []string) (Result, error)
// library
func Validate(outDir, basePath string, siblingBasePaths []string) (Result, error)
```

`runtimePrefixes` exempts URL paths served by a *non-static* origin behind the
same hostname — an HTTP API fronted by the same distribution. Nothing is ever
compiled for those paths, so without the exemption every link to one is
reported as broken and the build fails. It arrived in `feat(linkcheck): exempt
paths a non-static origin serves (#167)` on 2026-11-06, which is after the
lift, and it is not theoretical: the fleet inventory declares
`runtime_prefixes` for `markforge/watermarker-dev`, and the deployer threads it
through `-runtime-prefixes`.

The library also drops `normaliseRuntimePrefixes`, whose entire job is to
*reject* a bare `/` rather than accept it — a `/` prefix matches every link and
silently turns link validation into a no-op. The edge-layer function that takes
a same-shaped list treats `/` as valid, so copying that value here is a
plausible mistake, not a hypothetical one.

There is no way to re-add this from the outside: the exemption is applied inside
`resolveLink`, before base-path stripping and before clean-URL rewriting,
because that is the form the edge itself routes on.

## `pkg/conventions` — the library is two commits behind

### 1. `resolvable-asset-refs` is missing entirely

`rules_assetrefs.go` (+ its 10 tests) and the `Tree.Exists` method it needs are
absent from the library. Added here by `feat(conventions): add
resolvable-asset-refs, catching assets that 404 (#165)` on 2026-10-26.

This is a live, hard break, not a lost nicety. `knowalchemy-website`'s
committed `conventions.yaml` enables the rule, and an unknown rule name is a
config *error* in this package by design — a typo'd rule name that silently
disables a gate is the worst failure a gate can have. Built with the library's
conventions, that site's gate stops dead:

```
validate-conventions: knowalchemy-website/conventions.yaml:
  unknown rule "resolvable-asset-refs" (known rules: css-reset-present, ...)
```

`validate-conventions -list` loses the same entry, and the rule registry goes
from 16 rules to 15.

### 2. `no-html-extension-urls` carries reverted remediation advice

The library's `scanSitemapForHTMLExtensions` still tells the reader to *delete*
a committed sitemap and fall back on generation. That advice was removed here by
`fix(conventions): correct the sitemap remediation advice (#159)` on 2026-10-21,
because the generated sitemap has no `robots:noindex` awareness: following it
would publish account, cart, checkout and login URLs. It trades a redirect for
an indexing leak.

No test asserts on that string, so this one passes the suite silently. It is
still a behaviour regression — the text *is* the product of a gate.

### 3. `htmlscan` moved out — this part is clean

`htmlscan.go`/`htmlscan_test.go` became `pkg/htmlscan` with the unexported
helpers exported (`tokenizeHTML` → `Tokenize`, `htmlTag` → `Tag`, and so on).
Verified byte-identical modulo that mechanical rename, in both the source and
the test file. Nothing to do here beyond the rename.

## Upstream-first: what has to land in `ffreis-seo-core`

1. Port `rules_assetrefs.go` + `rules_assetrefs_test.go`, and re-add
   `Tree.Exists` plus the `rootTree.resolve` split it is built on.
2. Restore the `runtimePrefixes` parameter on `Validate`/`ValidateAndReport`,
   with `normaliseRuntimePrefixes`, `hasRuntimePrefix` and the `skipRules`
   struct, and the 8 tests that pin them.
3. Take the `#159` wording fix into `rules_cleanurls.go`.
4. Re-sync `pkg/conventions`' registry so `RuleNames()` reports 16 rules.

Until then, `internal/conventions` and `internal/linkcheck` stay here. Do not
"adopt" them by deleting local behaviour to match the library — that is a
regression wearing a refactor's clothes, and the tests that would have caught it
get deleted in the same commit.

## Two blockers before any of this can merge

* **No release tag.** `ffreis-seo-core` has `main` and no tags. `go.mod` carries
  a local absolute `replace`, which works on one machine and nowhere else, so
  CI on this branch cannot pass. Replace it with
  `require github.com/ffreis-org/ffreis-seo-core vX.Y.Z` and drop the
  `replace`, in one commit, once a tag exists.
* **Visibility.** This module is public (`FelipeFuhr/ffreis-website-compiler`);
  `ffreis-seo-core` is private under `ffreis-org`. A public Go module that
  depends on a private one is unbuildable for everyone else. The library is
  being made public precisely so this import is legitimate — wait for the flip.
  Do **not** reach for `GOPRIVATE` or a `GIT_AUTH_TOKEN` in a workflow here: a
  public module that needs a credential to build is broken, and needing one is
  the signal that the flip has not happened yet.

Note for local work: `GOPRIVATE` is empty on the dev host, so a `go get` against
a private fleet module falls through to an HTTPS clone that cannot prompt — and
`go mod tidy` then *silently succeeds* by resolving `@latest` to a months-old
pseudo-version. A `v0.0.0-<date>-<sha>` in `go.mod` where a tag was expected is
that bug, never a valid pin. Export `GOPRIVATE="github.com/ffreis-org/*"` for
any module operation and grep `go.mod` afterwards.

## Coverage

Whole-module line coverage, `go test ./... -coverprofile`:

| state | total | vs fleet floor (75) | vs this repo's gate (68) |
| --- | --- | --- | --- |
| before | 76.1% | pass | pass |
| after (two packages adopted) | 76.0% | pass | pass |
| hypothetical, all four adopted | 73.8% | **fail** | pass |

Every surviving package's own coverage figure is unchanged. Worth knowing before
the remaining two are adopted: doing so drops the module under the fleet's 75
floor, so it needs backfill elsewhere (`internal/cli` at 39.3% and
`internal/paritycmd` at 59.9% are the obvious candidates) in the same change.

## How the no-op was established end to end

Four real fleet sites plus `knowalchemy-website` were built with the compiler
before and after, and the whole `dist/` tree compared by sha256 — 166 files,
all identical, every `sitemap.xml` byte-for-byte:

| site | shape it exercises |
| --- | --- |
| `ffreis-website` | multi-language, committed `sitemap.yaml` |
| `flemming-website` | `flemming-data` overlay, committed `sitemap.yaml` |
| `casaboa-website` | single-language `pt`, sitemap *generated* from `-sitemap-base-url` |
| `petlook-website` | `petlook-data` overlay, `conventions.yaml` with a rule enabled |
| `knowalchemy-website` | `conventions.yaml` with all 16 rules enabled |

`validate-conventions` was then run over the built trees for the two sites that
have a `conventions.yaml`, and `validate-conventions -list` captured too; all
outputs are identical apart from timestamps and temp paths.

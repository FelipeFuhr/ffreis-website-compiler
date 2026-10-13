# forma.ffreis.com site-quality audit — compiler backlog

Aggregated 2026-10-13 while working on `ffreis-forma`'s body-content
translation and CSS-templatization follow-ups. Each finding below is a
distinct, concrete, verified gap in what `ffreis-website-compiler` validates
or supports at build time — not a forma-specific bug report (those are
tracked/fixed in `ffreis-forma` itself; this doc is the backlog of compiler
*capability* gaps those bugs exposed). One item (per-page CSS scoping) was
fixed this round and is included here only as precedent/context for the
others.

## Fixed this round (context, not open backlog)

**Every page loaded the entire site's CSS regardless of what it used.**
Confirmed: every page shipped an identical 148KB (25KB gzip) stylesheet;
`login.html` used under 7% of it. Fixed site-side in `ffreis-forma`
(`feat/css-per-page-modules` — split `styles.css` into `base.css` +
per-feature modules under `src/assets/css/modules/`, driven by a new
`pages.*.styles` list mirroring the existing `pages.*.scripts` JS pattern)
with **zero compiler changes** — the compiler's `transformStylesheets` /
`inlineLocalStylesheetsPreserveURLs` (`internal/buildcmd/transform.go:255`,
`internal/buildcmd/inlining.go:123`) already walks every local
`<link rel="stylesheet">` tag found in a page's `<head>` generically, in
source order, via `replaceTagWith`. It was a missing *convention*
(`pages.*.styles` didn't exist as a schema pattern), not a missing
*mechanism*.

**Worth promoting into the compiler itself** (see item 6 below): nothing
currently stops a FUTURE page from silently reverting to loading everything,
or flags that a module contains rules a page doesn't use. The fix landed at
the site layer because the generic mechanism already existed; the next site
onboarding this pattern (flemming, casaboa, koe) will have to rediscover it
by hand unless it becomes a documented, checked convention here.

## Open backlog

### 1. `check-lang-parity` never looks inside arrays

`internal/paritycmd/paritycmd.go`'s `flattenKeys` (around line 195-211)
only recurses into `map[string]any`; it has no case for `[]any`. A YAML
list (testimonials, FAQ items, nav groups, product cards — anything
`{{range}}`-rendered) is recorded as one opaque leaf key. Parity checking
between `en/` and `pt/` content repos only ever compares top-level key
*structure*, never array length or per-item field drift.

**Concretely**: if a content editor adds a 4th testimonial to `en/` and
forgets the matching item in `pt/`, or drops one field from item #2 of a
5-item FAQ list, `make parity-check` stays green. This was called out
explicitly as a known, accepted gap in an earlier `ffreis-forma` round
(deliberately out of scope there, shared fleet-wide — flemming/casaboa/koe
all depend on the current limited check, so fixing it needs to be its own
coordinated change, not a surprise behind a routine content PR).

**Suggested fix**: extend `flattenKeys` to recurse into `[]any`, either
comparing list *length* per language (catches drift at the "someone forgot
an item" level without requiring item-by-item content parity, which would
conflict with intentionally different per-language counts in some
contexts) or, more usefully, comparing the *field-set* of corresponding
list items positionally.

### 2. `-strict-contract`'s tracer only sees `dig(...)` calls, never plain `{{.field}}`

The strict-contract build only records a site-data path as "used" when
reached through an explicit `dig(...)` call inside a template. Plain
`{{.field}}` access inside a `{{range}}` body is invisible to the tracer —
it renders correctly (no runtime error) but the contract check cannot tell
the difference between "unused, dead `allowed` entry" and "used, but via
an untraced access pattern." A template author who writes `{{.field}}`
instead of `{{dig . "field"}}` inside a loop gets no warning at all; a
template author who writes `{{dig . "field"}}` for a field that genuinely
isn't in `allowed` gets a correct, helpful error. The asymmetry means the
*safe* failure mode (consistent `dig` usage) is undocumented tribal
knowledge rather than a checked convention.

**Suggested fix**: either (a) document this prominently in the compiler's
own template-authoring docs/AGENTS.md as a hard requirement with a
one-line rationale, or (b) add a lightweight static lint over `.gohtml`
files flagging `{{.foo}}`/`{{.Foo}}` patterns inside a `{{range dig ...}}`
block as "untraceable — use dig" before it ships as a real contract gap
for someone else.

### 3. `collections.yaml` is unimplemented scaffolding, not live code

A full generic-collections schema exists (`source.kind`, `shape`,
`id_field`, `detail.enabled`/`template`/`path`/`data_key`/
`page_data_from`) with zero Go implementation behind it — confirmed by
grepping the actual compiled binary's source tree; no file in `internal/`
reads `collections.yaml` or implements a `CollectionConfig` type. This
isn't merely "missing," it's actively misleading: a planning pass earlier
this session (a Plan subagent, not a human) reasonably assumed it was live
and built an implementation plan around it, discovered wrong only via
direct binary-source verification. The *actual* mechanism the 9 forma
product pages use today is a hand-written pattern: one shared partial
template plus 9 one-line stub `.gohtml` files each delegating to it — a
reasonable pattern, but it means `collections.yaml` describes a capability
the compiler doesn't have.

**Suggested fix**: either implement it for real (there's clearly been
design intent and at least one doc/decision-record branch referencing
collection schema work), or delete the file and any reference to it so
the next person doesn't plan around a capability that isn't there.

### 4. No cross-repo URL-convention check (clean-URL vs CDN canonicalization)

`ffreis-website-inventory`'s `compiler.clean_urls` (consumed by
`ffreis-website-deployer` to decide whether to pass `-clean-urls` to this
compiler) and each site's own infra repo's CloudFront-function
`canonicalize_html` Terraform variable are two independently-set flags
that must agree for a site to serve consistent URLs (both control whether
`/login.html` or `/login` is the canonical served form). Confirmed they
currently DO agree for every fleet site checked (forma: both `false`;
flemming/casaboa: both effectively `true` by omission) — this is not a
live bug — but nothing actually *checks* that agreement. The compiler
itself can't see this at build time (the CDN function variable lives in a
separate repo/pipeline, resolved only at deploy time), so this would need
to be a deploy-time or cross-repo CI check, not a `-strict-contract`
addition — noted here because it surfaced during this audit and no
existing tool owns it.

**Suggested fix**: a small check in `ffreis-website-deployer` (which
already reads `compiler.clean_urls` from inventory) that also reads the
target site's infra repo's `canonicalize_html` variable (via `terraform
output` or a shared convention file) and fails the deploy if they
disagree, rather than relying on both being hand-kept in sync by whoever
edits either side.

### 5. No build-time accessibility or responsive-coverage checks

Confirmed via grep across `internal/`: nothing touches `aria-*` attributes,
`<label>`/`<fieldset>` structure, or form-field accessibility at all. Two
concrete bugs this surfaced on forma (tracked/fixed site-side, not here):
a `premium.html` form missing `autocomplete` attributes its sibling
`contact.html` form had, and zero form-related CSS rules below a 480px
viewport width anywhere in the stylesheet. Neither would be caught by
anything in this compiler today — `html-validate` (already wired into
forma's own CI) catches *structural* HTML validity, not accessibility
semantics or responsive coverage.

**Suggested fix, lower priority than items 1-3**: this is a bigger lift
than the others (needs an actual a11y linter like `axe-core` wired into
the build, and some notion of "which viewport widths must this page's
critical UI support" to check against) — flagging it here for visibility,
not proposing a specific implementation.

### 6. No "this template is mostly hardcoded prose" detector

The exact class of bug the `ffreis-forma` body-content translation effort
(this session, prior rounds) existed to fix: a template with large blocks
of literal English text not sourced from site data, which silently defeats
translation (the pt build renders the identical English text, with no
error, no `TODO(PT)` marker, nothing to signal it was never wired up).
`ffreis-forma/src/templates/partials/product-detail.gohtml` (shared by all
9 product pages) is 90%+ hardcoded marketing prose today — explicitly
flagged and deferred as its own future slice in an earlier round, not
fixed. The only way anyone found the OTHER 56 pages' version of this bug
was a manual/LLM-assisted audit; there's no mechanical way to find it.

**Suggested fix**: a simple build-time or CI heuristic — for each
template, the ratio of literal text-node bytes to `{{...}}`-bound bytes —
flagged above some threshold as "candidate for data-binding review." Would
not need to be strict-contract-integrated; a standalone lint script
reading `.gohtml` files directly would catch most of the value here.

### 7. No sanity check on generated `<title>`/meta field shape

Found during the pt-BR translation pass: 5 of forma's product-detail page
`<title>` fields are truncated mid-word in the English source (e.g.
`"Objects Product Catalog — Editable Catalog &amp; Lookbook Template |
For"`, missing the final "ma" of "Forma"; one is `"...Event Stationery "`
with nothing after the trailing space at all). Looks like a historical
content-generation or SEO-title-length-cap bug, inconsistently applied.
Nothing currently checks that a `title` field ends in a complete word, or
matches an expected `"... | Forma"` suffix convention already followed by
every other page on the site.

**Suggested fix**: a cheap regex sanity check ("title field doesn't end
mid-word" / "title doesn't end with the site's own established suffix
convention") would have caught this without any translation work needed
to surface it.

## Summary table

| # | Gap | Where it'd live | Severity |
|---|---|---|---|
| 1 | `check-lang-parity` ignores array/list drift | `internal/paritycmd` | High — silent, fleet-wide, recurring risk |
| 2 | `dig`-only tracer, plain `{{.field}}` invisible | `internal/sitegen` + docs | Medium — tribal knowledge today |
| 3 | `collections.yaml` unimplemented | `internal/buildcmd` or delete the file | Medium — actively misleading |
| 4 | No clean-URL/CDN-canonicalization cross-check | `ffreis-website-deployer` | Low — currently in sync everywhere, but unchecked |
| 5 | No a11y / responsive-coverage checks | new tooling | Low-Medium — real bugs found, bigger lift |
| 6 | No hardcoded-prose-ratio detector | new lint script | Medium — exactly the bug class this session fixed, repeatedly |
| 7 | No title/meta shape sanity check | cheap regex lint | Low — one concrete bug found, cheap to add |
| 8 | `-site-data` resolves against CWD, not `-website-root` | `internal/sitegen` | High — proven bug, silent until website-root ≠ CWD |
| 9 | No `version`/capability mechanism; a consumer greps internal source | `internal/cli` | Medium-High — already caused a wrong diagnosis |
| 10 | ~~Per-page asset attribution discarded~~ — closed by `validate-css` (#128) | — | Withdrawn; residual: it is advisory-only, nothing enforces |
| 11 | Layer provenance computed, then discarded; contract check is layer-blind | `internal/sitegen` | Medium — blocks "this field must come from the content repo" |
| 12 | Layer merge forbids override, so sites simulate overlay with `rm -rf` | `internal/sitegen` (design) | Medium — override semantics live outside the compiler |
| 13 | CLI/binary surfaces disagree; `\|` layer syntax undocumented | `internal/cli` + flag help | Low — discoverability only |
| 14 | No gate would have caught 8 or 9 mechanically | compiler CI | Medium — meta; cheapest durable win |

---

## Addendum — gaps found while externalizing `ffreis-urbs`'s content (2026-10-13)

Second session, same day. Added here rather than filed separately so there is
one compiler backlog, not two. These surfaced from a different direction:
moving `ffreis-urbs`'s `i18n/{en,pt}/` into a sibling data repo, which put the
compiler in a configuration no site had used before — **website-root ≠ CWD**,
and required content arriving from a separate layer.

A note on item 1 above: it reasons that "flemming/casaboa/koe all depend on
the current limited check." Worth knowing that `ffreis-urbs` depended on it
*not at all* — its `make check` ran `validate-site-data` + `validate-assets`
per language and never invoked `check-lang-parity`, despite being a
two-language site. So the adoption surface is narrower than assumed: making
item 1's check deeper only helps sites that already call it, and nothing
surfaces which multi-language sites don't.

### 8. `-site-data` resolves against the process CWD, not `-website-root`

Templates and assets honour `-website-root`; `-site-data` layer paths are
opened relative to the working directory. Both flags being present implies
both are rooted the same way.

**Proven, not inferred.** `build-static -website-root "$WORK"` run from a
different CWD fails with

```
error="loading site data: open src/data/i18n/en: no such file or directory"
```

for a file that exists under `$WORK`. `cd "$WORK" && build-static
-website-root .` succeeds with otherwise identical arguments.

Invisible for every site that builds in place (CWD == `-website-root` ==
`.`). The instant a site assembles into a temp dir — the pattern any
externalized-content repo needs — the two diverge, and the error names a
relative path that looks correct, which is what makes it expensive.

**Standing cost:** every `build`/`check` recipe in `ffreis-urbs` now `cd`s
into the assembled directory and reaches the binaries via `$(abspath …)`
purely to work around this.

**Suggested fix:** resolve `-site-data` relative to `-website-root`. If
existing callers depend on current behaviour, then at minimum print the
attempted absolute path *and* the `-website-root` value in the error.

### 9. No version or capability mechanism — and a consumer already greps internal source

Confirmed absent: no `version` case in the dispatch
(`internal/cli/cli.go:30-54`), none in `printUsage` (`:62-87`), no
`debug.ReadBuildInfo`, no `-ldflags` in `internal/` or `cmd/`, and zero
repo-wide hits for `capabilit`, `min-version`, `compiler-version`,
`required-version`, `api-version`.

This already causes wrong diagnoses. `make test-output` against a compiler
checkout on a branch that had dropped the subcommand produced
`unknown command: test-output`, which reads as a consumer bug — it was
reported as a pre-existing site failure in a PR body and had to be retracted
once `origin/stable` was found to still have `case "test-output":`.

The stronger evidence is `flemming-website`'s CI
(`templates-ci.yml:53-81`), which decides whether the compiler is usable by
grepping its internal packages:

```bash
grep -q 'validate-sanity' "${dir}/internal/cli/cli.go" || return 1
grep -rq 'required'       "${dir}/internal/sitegen"    || return 1
```

A consumer feature-detecting by reading internal source is the clearest
signal the CLI should answer this itself.

**Suggested fix:** a `version` subcommand — in-fleet precedent is
`vaultctl`, whose consumers gate on `VAULTCTL_MIN_VERSION` — plus a
machine-readable capability list, so a site fails with "needs `test-output`;
compiler at <ref> lacks it" rather than `unknown command`.

### 10. ~~Per-page asset attribution is computed, then thrown away~~ — mostly CLOSED; one residual question

**Withdrawn as a gap.** I drafted this as "the missing gate for the CSS fix
above" and it was solved while I was writing. Recorded because the CSS story
moved three times in two days and the sequence is easy to misread:

| | |
|---|---|
| #121 (10-12) | compiler-side zero-config scoping — `src/assets/css/pages/<pageName>.css` auto-appended to that page's `<head>`, threaded through `transformPage`. Its message deferred the checker: *"a separate validate-css command … is a deliberately separate future task."* |
| this doc, Fixed-this-round (10-13) | forma's site-side `pages.*.styles` list, "zero compiler changes" |
| #128 (10-13) | `validate-css` **exists** — `internal/cssusage` + `internal/validatecsscmd`, wired at `internal/cli/cli.go:40` |

`validate-css`'s own help: *"Warn when a page's own CSS has rules unused by
that page's own rendered output (advisory, never fails)."* That is the
**stronger** check — selector-level coverage against the rendered DOM — not
the cheaper per-page-reference map I was about to propose. The gap is closed
and then some.

**The residual question, and it is a real design choice rather than an
oversight:** `validate-css` is *advisory and never fails*. So nothing
*prevents* a page regressing to loading everything; it only says so in output
somebody has to read. "Never fails" is defensible for a coverage heuristic —
a false positive that blocks a build is worse than a warning nobody reads —
but it does mean neither per-page mechanism has an enforcing gate, which is
what the Fixed-this-round section was reaching for.

If an enforcing variant is ever wanted, the cheap deterministic half is worth
knowing about: `assetusage` already receives every rendered page keyed by name
(`assetusage.Validate(assetsRoot string, renderedPages map[string]string)`,
`internal/assetusage/assetusage.go:30`) and `collectInitialUsedAssets` uses
`pageName` **only inside an error string** (`:57-61`), unioning all refs into
one flat set with no page dimension on `Result`. So *"this page loads a
stylesheet it never references"* — deterministic, no heuristic, safe to fail
on — needs no new analysis, only that attribution kept rather than discarded.
Strictly narrower than unused-rule detection, and complementary to it.

**Also worth documenting somewhere:** there are now two per-page CSS
mechanisms, landed a day apart, and they are not redundant — #121's is
strictly 1:1 with a page name, forma's `pages.*.styles` can share a module
across several pages. A site author has to discover both and infer which
applies.

`assetusage.Validate(assetsRoot string, renderedPages map[string]string)`
(`internal/assetusage/assetusage.go:30`) receives every rendered page keyed
by name. `collectInitialUsedAssets` iterates
`for pageName, html := range renderedPages` and uses `pageName`
**only inside an error string** (`:57-61`), unioning every page's references
into one flat set. The returned shape has no page dimension:

```go
type Result struct {
    UsedCSS, UsedJS, UnusedCSS, UnusedJS, MissingRef []string
}
```

It already does the hard parts — following `@import` chains
(`expandImportedAssets`, `collectCSSImports`) and JS import/export-from
graphs (`collectJSImports`).

**Consequence:** `validate-assets` can answer *"is any asset unreferenced
site-wide"* but **cannot** answer *"does this page load CSS it never uses"* —
different defects, and only the weaker one is checked. So neither per-page
mechanism has a gate holding it in place.

**Suggested fix:** the planned `validate-css` needs no new static analysis to
get its page→assets input. Give `Result` a per-page dimension — the
attribution is already in hand at the point it is discarded — and
`validate-assets` can then fail (or warn on) a page that *loads* an asset it
does not *reference*. Note that is a strictly weaker check than #121's stated
goal of "rules unused by that page's rendered output", which additionally
needs CSS-selector-level coverage against the rendered DOM; the per-page
reference map is the cheap half that already exists.

### 11. Layer provenance is also computed, then thrown away

`site_data_layers.go:94,162-168` builds `origins map[string]string` recording
which file first defined each path — then discards it. `SiteDataLoadResult`
(`sitegen.go:19-26`) exposes only `Layers []string`, and every consumer uses
that purely for logging (`cmdutil.go:59`, `validateassetscmd.go:54`,
`validatedatacmd.go:39`, `validatesanitycmd.go:55`).

So the compiler knows exactly which layer satisfied each contract field and
cannot tell anyone. Contract validation (`LoadSiteDataContract`,
`ValidateSiteDataAndUsage`) is correspondingly layer-blind: a site cannot
express *"this field must come from the content repo, not the site repo"* —
the exact invariant an externalized content repo depends on, and the thing
that would have made `ffreis-urbs`'s extraction verifiable rather than merely
tested.

**Suggested fix:** return `origins` on `SiteDataLoadResult`, and let the
contract constrain which layer may satisfy a path. `export-site-data` is the
natural place to surface provenance for humans.

### 12. The layer merge forbids override, so sites simulate an overlay with `rm -rf`

Worth recording because it is the opposite of what the `|` syntax suggests.
`mergeIntoStrict` (`site_data_layers.go:117-147`) deep-merges maps but
rejects any leaf redefinition outright:

```go
return fmt.Errorf("conflicting site data path %q first defined in %s and again in %s", path, first, layer)
```

There is no precedence model — a later layer **cannot** override an earlier
one. Sites that need override therefore perform it by **filesystem deletion
before the compiler runs**: `ffreis-urbs`'s `assemble` target does
`rm -rf "$WORK/src/data/i18n"` and then copies the data repo's `i18n/` into
place. The override semantics live in a Makefile `rm -rf`, invisible to the
compiler and unvalidated by it.

Three sites hand-roll assembly today with genuinely different failure modes:
`ffreis-urbs` fails loudly when the layer is absent; `casaboa-website` falls
back and **silently builds a site without its listings**; `flemming-website`
depends on a CI `cp` step having run.

**Suggested fix (design question, not a patch):** should `-site-data` gain a
declared-precedence overlay mode, so override is expressed to — and checkable
by — the compiler instead of simulated behind its back? Strict-disjoint is a
defensible default; the gap is that there is no alternative.

### 13. Two small surface inconsistencies

- **`emit-content-bundle` is not in the CLI dispatch.** `internal/bundlecmd`
  is reachable only via `cmd/emit-content-bundle/main.go`, so the documented
  CLI surface and the binary surface disagree; the capability is discoverable
  only by reading the tree.
- **The `|` layer syntax is undocumented.** Every command's `-site-data` help
  reads *"optional site data source override; supports file/URL sources or a
  directory containing YAML layers"* (`buildcmd/options.go:102`) with no
  mention of `|`, which is parsed at `sitegen.go:156-158` and is the exact
  mechanism every multi-layer site depends on.

### 14. Neither 8 nor 9 would have been caught by anything

Both are the kind of defect a gate finds for free and a human finds by
accident. Items 8-13 are an incidental sample from one session that happened
to stress the compiler unusually; the durable fix is not a longer list but two
checks:

- **a build test with `website-root` ≠ CWD** — catches item 8 directly, and
  any future path-resolution asymmetry between the two roots;
- **a generate-and-build job per consumer archetype** — catches the item 9
  class (a subcommand disappearing under a consumer) when it breaks rather
  than when someone notices.

The second has a precedent argument from the same day: `templates/rust-service`
shipped two red CI gates precisely because nothing generates and builds each
archetype (see `ffreis-project-templates#121`).

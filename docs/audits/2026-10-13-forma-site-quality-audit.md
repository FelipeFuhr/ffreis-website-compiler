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

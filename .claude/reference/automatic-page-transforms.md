## Automatic page transforms (`transformPage` in `internal/buildcmd/buildcmd.go`)

Every page produced by `build` / `build-static` passes through `transformPage`, which
applies these transforms in order:

### 1. Position-based CSS loading

Document position signals loading priority — mirroring the JS-at-end convention:

- **`<link rel="stylesheet">` in `<head>`** → inlined as a `<style>` block (critical path).
  Zero HTTP requests; page is fully styled from the first byte. Inlined CSS is **minified**
  (comments stripped, whitespace collapsed, `/*! preserved */` bang-comments kept). Local
  `@import` rules are **flattened recursively** — imported CSS is inlined verbatim with its
  own `url()` refs resolved; external `@import` (CDN fonts) are left untouched. `url()` refs
  (fonts, backgrounds) are rewritten to root-relative paths (`/fonts/file.woff2`) so they
  stay external and benefit from fingerprinting and long-lived caching.

- **`<link rel="stylesheet">` in `<body>`** → kept external, transformed to the deferred
  pattern:
  ```html
  <link rel="stylesheet" href="component.a1b2c3d4.css"
        media="print" onload="this.media='all'">
  <noscript><link rel="stylesheet" href="component.a1b2c3d4.css"></noscript>
  ```
  `media="print"` allows the browser to fetch without blocking rendering; `onload` applies
  the styles once ready. The `<noscript>` fallback handles JS-disabled environments. The
  href is fingerprinted by the next step.

**Convention for template authors:** place a stylesheet `<link>` in `<head>` if it affects
above-fold content (layout, header, typography). Place it in `<body>` if it can wait —
widgets below the fold, form validation, cookie banners, etc. No attribute or naming change
needed; position alone is the signal.

`<link media="screen and (...)">` in `<head>` are inlined as matching `<style media="...">`
blocks so responsive CSS continues to behave correctly.

When `-inline-assets` is set, full inlining (including data-URI fonts) is used for both
head and body instead, bypassing this step.

**Optional flags:**

- **`-embed-fonts`** (default off): embeds font files (`woff2`/`woff`/`ttf`/`otf`/`eot`)
  referenced by `url()` in inlined CSS as base64 data URIs, eliminating font files from
  dist. Increases HTML size by ~1.37× per font file. Appropriate for single-page sites or
  intranet deployments; avoid for multi-page sites where font caching is beneficial.

- **`-inline-body-css`** (default off): inlines body `<link rel=stylesheet>` as `<style>`
  blocks instead of the deferred external pattern. Eliminates all CSS files from dist but
  prevents cross-page CSS cache reuse. Both `-embed-fonts` and `-inline-body-css` apply to
  body CSS when used together.

### 2. Navigation enhancement injection (progressive enhancement)

Two `<head>` elements are injected before `</head>` by `injectNavigationEnhancements`:

- **Cross-document view transitions** (`<style>@view-transition{navigation:auto}</style>`)
  — fades between pages instead of a hard repaint on same-origin navigation.
  Chrome 126+/Edge 126+; silently ignored elsewhere.

- **Speculation Rules** (`<script type="speculationrules">…</script>`)
  — prerenders same-origin pages on hover (`eagerness: "moderate"`), making navigation
  near-instant. Chrome 121+; silently ignored elsewhere.

These run on top of CSS inlining and serve as a progressive enhancement layer. They are
secondary to CSS inlining: even without them, FOUC is already eliminated by the inline CSS.

### 3. SVG icon inlining (`buildcmd.go` — `inlineLocalSVGs`)

For every `<img src="local.svg">` referencing a local SVG file smaller than 8 KB:
- Replaces the `<img>` tag with the full `<svg>` XML content inline.
- Strips the `<?xml ...?>` processing instruction (invalid in HTML5).
- Merges the `<img>`'s `class` attribute into the `<svg>` root element's `class`.
- Non-empty `alt` → `aria-label="..."` + `role="img"` on `<svg>`.
  Empty `alt` → `aria-hidden="true"` (decorative icon).
- Copies `width`/`height` from `<img>` to `<svg>` when the SVG root lacks them.
- SVGs ≥ 8 KB are skipped and fingerprinted as external files instead.

Benefits: eliminates one HTTP request per icon, enables CSS styling of SVG internals
(fill, stroke), and removes small icon files from S3 output entirely. Runs in the
normal build pipeline only (skipped when `-inline-assets` is set, where
`inlineLocalImages` already handles SVGs as data URIs).

### 4. JS size-threshold inlining with cross-page sharing awareness (`buildcmd.go` — `inlineSmallLocalScripts`, `collectSharedScripts`)

For every `<script src="...">` referencing a local file below the threshold:
- Replaces the tag with an inline `<script>` block containing the file's content.
- `type="module"` scripts are always skipped (module scoping/`import` semantics break
  when inlined).
- Files at or above the threshold stay external and are fingerprinted.

**Flags:**
- **`-js-inline-threshold`** (default 8192 bytes; set 0 to disable) — applies to all scripts,
  or only to single-page scripts when `-js-shared-inline-threshold` is also set.
- **`-js-shared-inline-threshold`** (default -1 = disabled) — when set to a value ≥ 0,
  scripts that appear on **more than one rendered page** use this (lower) threshold instead
  of `-js-inline-threshold`. Set to 0 to never inline shared scripts (cache all of them).
  Set to 8192 to inline small shared utilities but cache larger shared scripts like ask.js.

**Cross-page analysis pre-pass (`collectSharedScripts`):** After all regular pages are
rendered (before `writePages`), `collectSharedScripts` scans every rendered HTML string with
`scriptTagRE` and counts how many pages reference each local script. Scripts with a count > 1
are placed in `buildOptions.sharedScripts`. Post pages and paginated pages inherit the same
base layout, so the regular-page analysis correctly identifies shared scripts for all outputs.

**Why this matters:** A script on every page (e.g. an ask-widget JS) benefits from CloudFront
caching — one request per browser session shared across all pages. A script on one page
benefits from inlining — no extra HTTP request, no cross-page caching opportunity lost.
The default (`-1`) preserves the original behaviour: threshold applies uniformly.

**`defer` semantics when inlining:** Inlined scripts lose the `defer` attribute and run
synchronously. Scripts inlined in `<head>` must handle early execution (via `DOMContentLoaded`
or similar); scripts inlined in `<body>` are safe since the preceding DOM is already parsed.

Runs after CSS transforms and SVG inlining, before LQIP and fingerprinting. Since
`assetusage.Validate()` runs on pre-transform HTML, the original `<script src>` tags are
still present at validation time — no change needed to asset validation.

### 4a. Script preload injection (`transform.go` — `injectCachedScriptPreloads`)

Runs immediately after the JS inlining step. For every local `<script src="...">` that
survived inlining (will be served as a separate cached file), injects a
`<link rel="preload" as="script" href="...">` before `</head>`. The fingerprinting step
then rewrites both the preload hint and the script tag to the same content-hashed filename,
so the browser's preload cache fires exactly when the deferred script is encountered —
typically 100-300 ms earlier on cold page loads. External CDN scripts and already-present
preloads are skipped. This runs unconditionally (no flag); it is always beneficial when
external local scripts exist.

### 4b. Default lazy-loading for non-eager images (`transform.go` — `injectDefaultLazyLoading`)

Runs after SVG inlining (so icons already converted to inline `<svg>` are
correctly skipped) and before LQIP. Adds `loading="lazy"` to every `<img>`
that doesn't already declare a `loading` attribute. Templates opt an image
out of the default (typically above-the-fold content, and a prerequisite for
LQIP below) by setting `loading="eager"` explicitly; an image with any
existing `loading` value (`eager`, `lazy`, or otherwise) is left untouched.
Runs unconditionally (no flag) — like script preload injection, this is
always beneficial and never load-order breaking.

### 5. LQIP — blur-up placeholders for above-fold images (`lqip.go`)

For every `<img loading="eager" src="local.file">` (raster only — SVGs skipped):
- Decodes the image, scales to 20 px wide (nearest-neighbour), encodes as quality-20 JPEG.
- Replaces `src` with the base64 data URI (shows the blurry thumbnail immediately),
  moves the original path to `data-src`.
- Adds `class="lqip-pending"` (CSS: `filter: blur(8px); transition: filter .3s`).
- Injects one `<style>` block and one `<script>` block per page: the script swaps in
  the full image (from `data-src`) when it loads, removing the blur class.

Requires `golang.org/x/image/webp` for WebP decode. JPEG and PNG use stdlib.
Runs before fingerprinting so `data-src` gets fingerprinted to the hashed filename.

### 6. Small raster image inlining (`buildcmd.go` — `inlineSmallLocalRasterImages`)

Optional; controlled by **`-raster-inline-threshold`** (default 0 = disabled).

For every `<img src="...">` whose src is a local raster file smaller than the threshold:
- Replaces `src` with a base64 data URI (same mechanism as `-inline-assets` images).
- Skips images whose `src` is already a data URI (i.e., LQIP-processed images).
- Skips SVG files (handled by `inlineLocalSVGs` above).
- Runs **after** LQIP so the `isDataURI` guard correctly excludes LQIP placeholders.

Trade-off: inlined images cannot be cached separately from the HTML page. Appropriate for
tiny icons (< 4 KB) that change with every deploy. Large images should stay external.

### 7. Asset fingerprinting (`fingerprint.go`)

Rewrites all local asset references to content-hashed filenames:
`portrait.webp` → `portrait.a1b2c3d4.webp` (SHA-256 of file, first 8 hex chars).

The packer (`ffreis-website-packer`) assigns `Cache-Control: immutable` (1 year) to
files whose names match `[._-][a-f0-9]{8,}[._-]`, so fingerprinted assets are
automatically cached long-term. Fingerprinting covers: `<img src>`, `<img data-src>`
(LQIP), `<link rel="preload" href>`, `<link rel="icon" href>` (also matches
`apple-touch-icon`), `<link rel="manifest" href>`, `<script src>`, and
`url()` inside inline `<style>` blocks. Data URIs and external URLs are left unchanged.

Only fingerprinted copies are written to the output directory. Originals (css/, fonts/,
images/, js/) are **not** copied to dist — only `ld/` (JSON-LD), `.well-known/` (e.g.
`security.txt`), and root discovery files (`favicon.ico`, `robots.txt`, `llms.txt`,
`humans.txt`) are copied wholesale, if present in `src/assets/`. This prevents dead
unreferenced files from accumulating in S3.

`manifest.json` and `site.webmanifest` are handled differently from the other
discovery files: they are **not** copied wholesale, because their `icons[].src`
entries name other local assets that must stay in sync with those assets'
fingerprinted filenames (`writeManifestFiles` in `assets.go`, called from
`writePages` once `basePath` is known). Each file (if present) is parsed as
JSON, every `icons[].src` is fingerprinted via the same mechanism as HTML
references (`fingerprintManifestIcons` in `fingerprint.go`), the referenced
icon files are copied to their hashed paths alongside the page-referenced
assets, and the rewritten JSON — unchanged if there is no `icons` key — is
written to dist. A manifest with no icons, an empty icons array, or invalid
JSON is left as-is rather than failing the build.

### 8. External asset mirroring (flag: `-mirror-external-assets`)

Optional; downloads external CSS/JS/images and rewrites URLs to local copies.
Also processes `url()` references inside inline `<style>` blocks via `styleBlockRE`.

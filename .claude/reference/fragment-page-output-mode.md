## Fragment page output mode (`pages.<name>.fragment`)

An opt-in, per-page output mode for consumers that are **client-side routed
single-page apps** rather than multi-page static sites: a private consumer in
this fleet resolves all of its content from a live API at view time and owns its
own persistent chrome, but still wants each of its routes authored as a compiler
template with its own scoped CSS. A fragment page is compiled to a reusable
`<template>` the app clones on navigation, instead of to a standalone HTML
document.

Everything below is **additive and opt-in**. A site that marks no page as a
fragment builds byte-for-byte what it built before this mode existed — see the
backward-compatibility guarantee at the end.

### Opting a page in

Set `fragment: true` on the page's entry in site data:

```yaml
pages:
  orders:
    fragment: true
  profile:
    fragment: true
```

This follows the existing `pages.<name>.internal` convention exactly, including
its contract handling: `internal` and `fragment` are both listed in
`compilerManagedPageFlags` (`internal/buildcmd/siteinputs.go`), so they are
compiler build metadata rather than site content. A site may set either one
without declaring it in `site.contract.yaml`, and declaring it anyway does not
make it a dangling path. There is no CLI flag — per-page behaviour in this
compiler is expressed in site data.

### Rendering

`splitFragmentPages` partitions the page list after `filterInternalPages`.
Fragment pages are removed from everything that writes a page: no
`<name>.html` / `<name>/index.html`, no `sitemap.xml` entry, no hreflang or
`<base href>` handling.

`sitegen.RenderFragment` then renders each fragment through the **same data
pipeline as a full page** — identical `.PageName` / `.SiteData`, every template
function available — but executes only the page's inner content block instead of
the shared `layout` block. The block name is resolved against
`sitegen.FragmentContentBlocks`, in order:

1. `fragment` — define this when a template wants a different body mounted
   client-side than it renders as a full page.
2. `page` — the convention most page templates in this fleet already use.
3. `content` — the convention `examples/hello-world` uses.

So an existing page template usually needs no edit to become a fragment. The
shared layout's `<html>/<head>/<body>` and its header/footer partials are never
rendered: the consuming app owns that chrome entirely. A fragment whose content
block renders `<html>`, `<head>` or `<body>` itself **fails the build** rather
than emitting a nested document shell.

Fragment pages are still rendered through the layout once by `RenderPages`, so
their CSS/JS references are counted by `assetusage.Validate` and
`collectSharedScripts` exactly like any other page's.

### Generated files

Written to the output root only when the site has at least one fragment page:

| File | Contents |
| --- | --- |
| `fragments.html` | One `<template id="tpl-<pagename>">` per fragment, in page-name order. |
| `fragments.js` | The dependency-free mount script (see below). |
| `css/fragment-<pagename>.<hash>.css` | The stylesheets only that fragment references. Absent when it has none. |
| `css/fragment-shared.<hash>.css` | Every stylesheet referenced by more than one fragment. Absent when none is shared. |

The `<template>` id is built from the **page name** (the `pages/<name>.gohtml`
key), not the slug. Both generated entry points keep stable, un-fingerprinted
names — like `sitemap.xml` and `blog/feed.xml` — because the consuming app
references them by name; the CSS files they point at are content-hashed.

A regular page whose slug would resolve to `fragments.html` fails the build
instead of being silently overwritten.

### Per-fragment CSS

A fragment has no `<head>`, so the position-based head/body CSS split cannot
apply. **Every stylesheet link a fragment's content declares is treated as that
fragment's own CSS**: the `<link>` is stripped from the template and the
stylesheet becomes a separate file. It is deliberately *not* inlined as
`<style>` — that would copy the whole stylesheet into every clone of the
template in the DOM.

Each generated file goes through the same helpers head-CSS inlining uses —
`flattenCSSImports` with `preserveURLs`, then `minifyCSS` — and its `url()`
references then get the same content-hash rewrite `fingerprintStyleBlocks`
applies inside an inline `<style>`, with the referenced files copied to dist
under their hashed names. An un-rewritten `url()` would 404: the originals under
`css/`, `fonts/` and `images/` are never copied.

**Shared-CSS hoisting** mirrors `collectSharedScripts`'s precedent: a stylesheet
referenced by **more than one** fragment is hoisted into the single
`css/fragment-shared.<hash>.css` the app loads once, rather than being treated
as any one fragment's route-scoped CSS. A stylesheet referenced by exactly one
fragment stays that fragment's own. Same principle as scripts — used on more
than one page earns caching, used on one does not.

Two details worth knowing: a fragment stylesheet's `media` attribute is not
carried into the generated file (put the media query inside the CSS instead),
and an **external** stylesheet URL is left in the template markup untouched
because it cannot be read, minified or hashed at build time.

### The mount script's contract

`fragments.js` defines one global and no dependencies:

```js
window.websiteFragments = {
  names,      // every fragment name
  ownCSS,     // name → its own stylesheet URL, or null
  sharedCSS,  // the hoisted stylesheet URL, or null
  mount       // mount(name, targetElement) → boolean
};
```

`mount(name, target)` points `<link id="fragment-shared-css">` at `sharedCSS`,
points the singleton `<link id="fragment-css">` at that fragment's `ownCSS`
entry (**no-op when the entry is `null`**, so the current stylesheet is left
alone rather than cleared), clears `target` and appends a clone of the
fragment's template content. It returns `false` when no `<template>` for that
name is in the document or `target` is missing.

The script is DOM-only: it never fetches. The app is responsible for getting
`fragments.html`'s markup into the document first (it is a plain HTML partial —
fetch and insert it, or include it server-side). Both `<link>` ids are created
on first use if absent.

### Which page transforms apply to a fragment

Applied: small-SVG inlining, default lazy-loading, asset fingerprinting.

Deliberately skipped, because a `<template>` is not a document:

- **CSS inlining** — replaced by the per-fragment CSS files above.
- **JS inlining and script preloads** — a `<script>` inside template content
  never executes when the template is cloned, so the consuming app owns the
  fragment's behaviour. Fragments should carry no `<script>`.
- **LQIP** — it injects a per-page `<style>` + `<script>` pair, which inside
  template content would be duplicated per clone and never run.
- **Navigation enhancements, tracker and dev-data injection** — all inject
  before `</head>`, which a fragment does not have. The consuming app's own
  document carries them.
- **The internal link check** — the bundle is written after
  `linkcheck.ValidateAndReport` on purpose: a fragment's links are client-side
  routes (`href="#/orders"`) resolved by the app at view time, not static pages
  this build emits.

`-inline-assets` does not change fragment output: a fragment's CSS is always a
separate file, which is the entire point of the mode.

### Backward-compatibility guarantee

A site with **zero** fragment pages must produce byte-for-byte the output it
produced before this mode existed: no new files, no new tags, nothing.
`splitFragmentPages` returns the caller's own page slice untouched when no page
is marked, and `maybeWriteFragmentBundle` returns immediately.

This is locked by
`TestRun_NoFragmentPages_MatchesPreFragmentOutputByteForByte`, which builds
`examples/hello-world`, asserts the complete output tree file-by-file, and
checks the rendered page against `helloWorldIndexSHA256` — a digest captured
from the compiler at the commit this mode was added on. If you intentionally
change output for ordinary sites, that constant has to be updated in the same
commit, and the updated digest is the review artifact.

The digest is stored rather than a golden file because the compiler emits
meaningful trailing whitespace (the indented injection point before `</head>`)
and the fleet's pre-commit `trailing-whitespace` hook silently rewrites any text
file carrying it — which would corrupt a committed golden on its way into the
repo. Regenerate it with:

```bash
tmp=$(mktemp -d) && go run ./cmd/build-static \
  -website-root ./examples/hello-world -out "$tmp" >/dev/null \
  && sha256sum "$tmp/index.html"
```

One related trap: the generated mount script carries **no strict-mode pragma**,
and must not grow one. libmagic classifies any file containing that pragma's
exact quoted literal as `application/javascript`, and the fleet's staged-file
hygiene hook rejects a staged file whose MIME type is not `text/*` — so writing
it into `fragments.go`, even inside a comment, makes that file unstageable.
Nothing in the snippet needs strict mode.

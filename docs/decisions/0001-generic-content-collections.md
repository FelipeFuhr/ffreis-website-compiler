# Decision Record: Generic Content Collections for `ffreis-website-compiler`

**Status:** design proposal, no code written
**Date:** 2026-09-04
**Scope:** replace `internal/projects`, `internal/courses`, `internal/listings`, and forma's `data_page`/stub-template pattern with one configurable engine

---

## 0. Evidence base and a note on the checkout

The working tree at `/media/ffreis/second/projects/website/ffreis-website-compiler` is on branch `fix/post-lang-validation-fail-on-unknown` (`f4b3521`), which **predates** `internal/listings/`. Everything below is grounded in:

- `origin/main` (`0502211`) — has `internal/listings/` (PR #99), `internal/outputcheck`, `internal/outputtestcmd`
- `integration/p0-round1-all` (`88ea000`) — main plus the not-yet-merged default-on sitemap work (`fc4d221`, `feat/p0-4-sitemap-default-on`) and the section-registry unification (`deff23d`)

This was read from a worktree at `88ea000` because it is the only tree containing both. Implementers should note that **`feat/p0-4-sitemap-default-on` is not yet in `origin/main`** — this design assumes it lands first.

---

## 1. Ground truth: what the four mechanisms actually do today

### 1.1 Comparison matrix

| Axis | projects | courses | listings | forma products |
|---|---|---|---|---|
| Loader | `internal/projects/projects.go` | `internal/courses/courses.go` | `internal/listings/listings.go` | none (site-data layer) |
| Flag | `-projects-file` | `-courses-file` | `-listings-file` | none |
| File shape | bare YAML list | bare YAML list | `listings:` wrapped list | keyed map under `products:` in `site.d/40-products.yaml` |
| Record type | typed struct, 6 fields | typed struct, 17 fields | typed struct, 19 fields | untyped `map[string]any` |
| Ordering | sort by `order`, then `title` | sort by `order`, then `title` | **file order preserved** | map iteration (irrelevant; addressed by page name) |
| Required fields | `title` | `title` | `id`, `title` | none enforced |
| Derived fields | none | `slug` (from `Slugify(title)`), `href`, `price_usd_display`, `price_brl_display` | `href` | none |
| Index page | `/projects/` + `/projects/page/N/` | `/courses/` + `/courses/page/N/` | none (hub page is a normal page) | `products.html` (normal page, JS-rendered) |
| Detail page | none | `/courses/<slug>/index.html` | `/listings/<id>/index.html` | `/product-<slug>.html` |
| Detail template | — | page template named `course`, marked `pages.course.internal: true` | page template named `listing`, marked `pages.listing.internal: true` | **9 one-line stub templates**, each `{{define "content"}}{{template "product-detail-content" .}}{{end}}` |
| Detail data key | — | `.CurrentCourse` | `.CurrentListing` | `.SiteData.products.<slug>` looked up via `trimPrefix .PageName "product-"` |
| Home carousel | `siteData["projects"]` (first N) | `pages.index.courses_carousel_items` (first N) | `siteData["listings"]` (all) | n/a |
| Sitemap: index | `{Path}` only, no attrs | `{Path}` only, no attrs | n/a | n/a (hand-written `src/assets/sitemap.xml`) |
| Sitemap: detail | — | `{Path, Changefreq: "monthly", Priority: "0.6"}` | `{Path, Changefreq: "weekly", Priority: "0.7"}` | hand-written |
| JSON-LD | none | none | none | `Product` + `BreadcrumbList`, **emitted in `partials/head.gohtml`** |
| JS blob | none | none | `window.CASABOA_LISTINGS`, **emitted in casaboa's `layout/base.gohtml`** | `window.FORMA_CATALOG` from a static JS file (not data-driven) |
| `href` includes `base_path`? | **no** (raw from YAML) | **no** (template prepends) | **no** (`app.js` prepends via `window.CASABOA_BASE_PATH`) | n/a |
| Section gating | `sectionTable` entry `projects` | `courses` (+ prune `pages.index.courses_section`) | `listings` (+ prune `siteData["listings"]`) | not gated |
| Lang availability | none | `supported_languages` (template-side only) | none | none |

Blog posts (`internal/posts`, `-posts-dir`) are the fifth, older instance of the same pattern and share `writePaginatedPages`. It is deliberately **out of scope** for migration (see §7.5) but the schema must not preclude it.

### 1.2 Per-type quirks that any replacement must reproduce

These are behaviours a naive rewrite would silently change. They are the acceptance criteria for the golden-file tests.

**Q1 — Typed structs silently drop YAML fields, and two templates already read dropped fields.**
`yaml.v3` is used non-strict, so keys absent from the Go struct vanish. Two concrete live cases:

- `ffreis-content-mocks/projects.yaml` sets `available_languages` on all 100 records. `projects.Project` has no such field. `src/templates/partials/components.gohtml:209-210` in `ffreis-website` does `{{$availLangs := dig $p "available_languages"}}` and filters language badges on it. Today `$availLangs` is always nil ⇒ **every badge always renders**. A map-passthrough loader would start filtering.
- `ffreis-courses/courses.yaml` and `ffreis-content-mocks/courses.yaml` set `duration_hours` (and `date`) on every record. `courses.Course` has neither. `src/templates/pages/course.gohtml:49` does `{{with dig $c "duration_hours"}}<span class="course-meta-item">⏱ {{.}}h</span>{{end}}` ⇒ **the duration chip never renders today**. A passthrough would light it up on every `/courses/<slug>/` page.

Conversely, the typed structs *add* zero-valued keys the YAML never set (`courses.yaml` omits 11 of the 17 struct fields; the maps still carry `"udemy_url": ""`, `"price_usd_cents": 0`, `"curriculum": []any{}`…). Under passthrough those keys become absent, and `dig` returns `nil` rather than `""`/`0`. For `{{with}}` this is equivalent; for a bare `{{dig ...}}` print it is not necessarily equivalent in `html/template`. **This must be pinned by test, not assumed.**

**Q2 — `/projects/index.html` is written twice, and the surviving copy skips three transforms.**
`writePages` (buildcmd.go:416) writes every non-internal page, including `projects` and `courses`. With `-clean-urls` (which the deployer always passes, `deploy.yml:536`) the target is `dist/projects/index.html`. `writeAllPaginatedContent` then runs and `writePaginatedPages` overwrites the same path (`resolvePagedTarget`, paginatedpages.go:134). The surviving file therefore never went through:
- `validatePageBaseHref`
- `validateRenderedPageStructure`
- `injectHreflangAlternates` / `injectLangSwitcherHrefs` (buildcmd.go:435-436)

⇒ **No collection page — index, paged, or detail — gets hreflang alternates or lang-switcher href rewriting today.** Only `writePages` output does.

**Q3 — Both `/projects` and `/projects/` land in sitemap.xml.**
`generateSitemapFromPages` emits `"/" + page.Name` (= `/projects`, no trailing slash) for the static page, and `writePaginatedPages` contributes `pagination.PageHref("/projects", 1)` = `/projects/`. Same for `/courses`. Duplicate-ish entries are current behaviour.

**Q4 — Collection URLs ignore per-language slug translation.**
`writePages` honours `pages.<name>.slug` via `resolvePageSlug`; `writePaginatedPages` hardcodes `sectionName`, and `writeCourseLandingPages` / `writeListingDetailPages` hardcode `"courses"` / `"listings"`. Currently latent (no `slug:` override exists for `projects`/`courses` in `ffreis-website-data/data/pt/site.d/60-pages.yaml`), but it is a real divergence.

**Q5 — Empty collection ⇒ no index page from the collection path at all.**
`maybeWriteProjectListings` returns early on `len(...) == 0`. The `/projects/` page that ships is then the plain `writePages` render with `pages.projects.items` unset ⇒ the template's `empty_state` branch. `pagination.Paginate`'s zero-item branch (`pagination.go:41`) is therefore dead code in practice.

**Q6 — `pagination.Paginate`'s zero-item `Page` has an empty `BaseHref`** (it omits the field). Harmless today only because `Number == TotalPages == 1`.

**Q7 — Ordering semantics differ deliberately.** Listings preserve file order because it encodes upstream ranking (see the `score` field and the comment at `listings.go:52-55`); projects/courses re-sort. A generic engine must make this configurable, not pick one.

**Q8 — Detail-page templates are executed *twice*.** Once by `sitegen.RenderPages` with no `.CurrentX` (for asset-usage validation), once per record. `pages.<name>.internal: true` keeps the first render off disk. Every detail template must guard on `{{if .CurrentX}}` — casaboa's `listing.gohtml:9-15` documents this explicitly. The generic engine must preserve this two-phase contract.

**Q9 — `-content-source` `/mock/` guard enumerates content paths by hand** (`options.go:177`). Any new source-path mechanism must be added to that list or the anti-leak guard silently stops covering it.

**Q10 — Section gating is a four-way table.** `sectionTable` (`buildcmd/sections.go`) maps a section name to its page-template names plus a `prune` hook, and drives `pageSection`, `sectionForPath`, `-disable-sections` help text, `buildDevDataPayload`, and `pruneDisabledSectionData`. A collection must register into that table, not around it.

**Q11 — `buildDevDataPayload` hardcodes four fields** (`posts`, `courses`, `projects`, `listings`) with per-type ID semantics (`p.Meta.Slug` for posts, `c.Title` for courses, `p.Title` for projects, `l.ID` for listings) and per-type lang sources.

### 1.3 The `window.CASABOA_LISTINGS` byte contract — exact shape

This is the hard constraint, so it is worth stating precisely, because **the compiler does not produce this blob today.**

`casaboa-website/src/templates/layout/base.gohtml:9`:

```gotemplate
window.CASABOA_LISTINGS = {{with dig .SiteData "listings"}}{{toJSON .}}{{else}}[]{{end}};
```

`toJSON` is `sitegen.toJSON` (`internal/sitegen/sitegen_funcs.go:71`) — a plain `json.Marshal` returning `template.JS`. So the byte shape is fully determined by:

1. **The value at `siteData["listings"]`**, which is `[]any` of `map[string]any` produced by `listings.ToSiteDataList` (`injectListingsData`, `buildcmd/listing_detail.go:19`) when `-listings-file` is passed, or the **raw merged site-data layer** (`casaboa-data/site.d/30-listings.yaml`, no `href`) when it is not.
2. **`encoding/json`'s map marshaling**: keys sorted lexicographically, `SetEscapeHTML` on, no indentation, no trailing newline.

Concrete per-entry key order today: `breakdown, concern, href, id, imageLabel, locationLabel, meta, photoLabel, place, price, priceLabel, reasons, score, source, sourceNote, sponsored, title, tone, total, transaction`.

Verified against `casaboa-data/site.d/30-listings.yaml`: **25 records, exactly the 19 struct-mapped keys present on every record, zero extra keys, zero missing keys.** So for casaboa specifically, a map-passthrough loader is byte-identical to the typed struct plus computed `href`. (This is luck, not design — see Q1. It is also why casaboa is the *safest* of the JS-blob migrations and why the check must be a byte-diff, not a spot check.)

**Design consequence:** the safest way to satisfy "byte-compatible" is not to build a JS emitter at all, but to keep the engine's job as *"publish `[]any` of `map[string]any` to a configurable site-data key"* and leave `toJSON` in the website template. That is byte-identical **by construction**, because it is literally the same code path. See §3.6.

---

## 2. What the four mechanisms have in common (the actual abstraction)

Stripping the accidents, every one of these is:

> **A named set of records, loaded from somewhere, optionally sorted, optionally projected/derived, then published to (a) site-data keys for templates, (b) zero or more paginated index pages, (c) zero or one detail page per record, with (d) sitemap attributes and (e) section gating.**

Five orthogonal axes: **source**, **shape/projection**, **publish (site-data injection)**, **pages (index + detail)**, **sitemap**. JSON-LD and JS blobs are *not* a sixth axis — today they are both entirely template-side, and the design keeps them there.

---

## 3. The schema

### 3.1 Where it lives

A new **website-owned** file `<website-root>/collections.yaml`, discovered exactly like `sitemap.yaml` is (`resolveSitemapConfigPath`, buildcmd.go:660): default path, overridable by `-collections-config <path>`.

Record *file paths* stay on the CLI, because they live in separate repos that only CI knows the checkout path of:

```
-collection-source <name>=<path>     # repeatable
```

This split is the single most important structural decision: **shape is website-owned and versioned with the templates that consume it; the data location is deployment-owned.** It mirrors exactly how `-projects-file` works today (`ffreis-website-deployer/.github/workflows/deploy.yml:464-484` computes the path; `ffreis-website` owns `projects.gohtml`).

### 3.2 YAML shape

```yaml
# <website-root>/collections.yaml
version: 1

collections:
  projects:
    section: projects          # sectionTable key; gates -disable-sections + sitemap + pruning

    source:
      kind: file               # file | site_data | dir
      shape: list              # list | wrapped_list | keyed_map
      # wrapped_list: key: listings
      # keyed_map:    id_field: slug     (map key is copied into this field)
      path_from_flag: projects-file      # legacy alias accepted for -collection-source

    records:
      # Explicit projection. Each entry defines one key in the emitted record map.
      # This is what makes "reproduce today's output exactly" provable rather than
      # plausible: it reproduces the typed struct's field set, including its
      # zero-value defaults, without a Go struct.
      fields:
        - {name: title,       type: string, required: true}
        - {name: description, type: string, default: ""}
        - {name: stack,       type: list,   default: []}
        - {name: href,        type: string, default: ""}
        - {name: date,        type: string, default: ""}
        - {name: order,       type: int,    default: 0}
      passthrough: false       # false = drop unlisted YAML keys (today's behaviour)
      sort:
        - {by: order, dir: asc}
        - {by: title, dir: asc}
      # derive: []             # see 3.4

    publish:
      # site-data keys written AFTER contract validation, so they need no contract entry
      - {key: projects, value: records, limit_from_flag: items-per-page}

    index:
      enabled: true
      template: projects                    # page-template name
      base_path: /projects                  # page 1 -> /projects/, page N -> /projects/page/N/
      paginate:
        per_page_from_flag: items-per-page  # or: per_page: 12
      inject:
        items: pages.projects.items
        pagination: pages.projects.pagination
      sitemap: {}                           # {} == Path only, matching today

    detail:
      enabled: false

  courses:
    section: courses
    source: {kind: file, shape: list, path_from_flag: courses-file}
    records:
      fields:
        - {name: title, type: string, required: true}
        - {name: slug,  type: string, default_from: {slugify: title}}
        - {name: description, type: string, default: ""}
        - {name: platform, type: string, default: ""}
        - {name: level, type: string, default: ""}
        - {name: availability_type, type: string, default: ""}
        - {name: udemy_url, type: string, default: ""}
        - {name: supported_languages, type: list, default: []}
        - {name: localized_cta_labels, type: map, default: {}}
        - {name: localized_price_notes, type: string, default: ""}
        - {name: order, type: int, default: 0}
        - {name: sale_mode, type: string, default: ""}
        - {name: price_usd_cents, type: int, default: 0}
        - {name: price_brl_cents, type: int, default: 0}
        - {name: long_description, type: string, default: ""}
        - {name: what_you_learn, type: list, default: []}
        - {name: curriculum, type: list, default: []}
      derive:
        - {name: href, template: "/courses/{{.slug}}/"}
        - {name: price_usd_display, money: {field: price_usd_cents, symbol: "$"}}
        - {name: price_brl_display, money: {field: price_brl_cents, symbol: "R$"}}
      sort: [{by: order}, {by: title}]

    publish:
      - key: pages.index.courses_carousel_items
        value: records
        limit_from_flag: items-per-page
        # NOTE: today this is a no-op if pages.index is absent (projects.go:29-32).

    index:
      enabled: true
      template: courses
      base_path: /courses
      paginate: {per_page_from_flag: items-per-page}
      inject: {items: pages.courses.items, pagination: pages.courses.pagination}
      sitemap: {}

    detail:
      enabled: true
      template: course                 # requires pages.course.internal: true
      path: "/courses/{{.slug}}/"      # trailing "/" -> <path>index.html
      data_key: CurrentCourse
      # fields visible on the detail page beyond the index projection
      extra_fields: [what_you_learn, curriculum, long_description]
      sitemap: {changefreq: monthly, priority: "0.6"}

  listings:
    section: listings
    source:
      kind: file
      shape: wrapped_list
      key: listings
      path_from_flag: listings-file
      fallback: {kind: site_data, key: listings}   # no flag -> use the merged layer
    records:
      fields: [ ... 19 casaboa fields, sponsored type: bool default: false ... ]
      passthrough: false
      sort: []                      # explicit: preserve file order
      derive:
        - {name: href, template: "/listings/{{.id}}/"}
      require: [id, title]
    publish:
      - {key: listings, value: records}   # <- this is what feeds window.CASABOA_LISTINGS
    index:
      enabled: false
    detail:
      enabled: true
      template: listing
      path: "/listings/{{.id}}/"
      data_key: CurrentListing
      sitemap: {changefreq: weekly, priority: "0.7"}
```

### 3.3 Go types (illustrative, for conveying shape only)

```go
package collections

type Config struct {
    Version     int                   `yaml:"version"`
    Collections map[string]Collection `yaml:"collections"`
}

type Collection struct {
    Section string      `yaml:"section"`
    Source  Source      `yaml:"source"`
    Records RecordSpec  `yaml:"records"`
    Publish []Publish   `yaml:"publish"`
    Index   *IndexSpec  `yaml:"index"`
    Detail  *DetailSpec `yaml:"detail"`
}

type Source struct {
    Kind          string  `yaml:"kind"`            // file | site_data | dir
    Shape         string  `yaml:"shape"`           // list | wrapped_list | keyed_map
    Key           string  `yaml:"key"`             // wrapped_list / site_data key
    IDField       string  `yaml:"id_field"`        // keyed_map: where the map key goes
    Path          string  `yaml:"path"`            // literal, rarely used
    PathFromFlag  string  `yaml:"path_from_flag"`  // legacy flag name / -collection-source key
    Fallback      *Source `yaml:"fallback"`
}

type RecordSpec struct {
    Fields      []Field  `yaml:"fields"`
    Passthrough bool     `yaml:"passthrough"`
    Require     []string `yaml:"require"`
    Sort        []SortBy `yaml:"sort"`
    Derive      []Derive `yaml:"derive"`
}

type Field struct {
    Name        string  `yaml:"name"`
    Type        string  `yaml:"type"`         // string|int|bool|list|map|any
    Required    bool    `yaml:"required"`
    Default     any     `yaml:"default"`
    DefaultFrom *DefFrom `yaml:"default_from"` // {slugify: title}
}

type Derive struct {
    Name     string     `yaml:"name"`
    Template string     `yaml:"template"`  // Go text/template over the record map
    Money    *MoneySpec `yaml:"money"`     // {field, symbol} -> formatMoney semantics
}

type Publish struct {
    Key           string `yaml:"key"`             // dotted site-data path
    Value         string `yaml:"value"`           // records
    Limit         int    `yaml:"limit"`
    LimitFromFlag string `yaml:"limit_from_flag"`
}

type IndexSpec struct {
    Enabled  bool              `yaml:"enabled"`
    Template string            `yaml:"template"`
    BasePath string            `yaml:"base_path"`
    Paginate PaginateSpec      `yaml:"paginate"`
    Inject   map[string]string `yaml:"inject"`   // items / pagination -> dotted paths
    Sitemap  SitemapAttrs      `yaml:"sitemap"`
}

type DetailSpec struct {
    Enabled     bool         `yaml:"enabled"`
    Template    string       `yaml:"template"`
    Path        string       `yaml:"path"`       // Go template over the record map
    DataKey     string       `yaml:"data_key"`   // CurrentCourse / CurrentListing / ...
    ExtraFields []string     `yaml:"extra_fields"`
    Sitemap     SitemapAttrs `yaml:"sitemap"`
}

type SitemapAttrs struct {
    Changefreq  string `yaml:"changefreq"`
    Priority    string `yaml:"priority"`
    Lastmod     string `yaml:"lastmod"`      // literal YYYY-MM-DD
    LastmodFrom string `yaml:"lastmod_from"` // record field name, or a file path
}
```

`SitemapAttrs` deliberately mirrors `sitemap.URLItem` field-for-field (`internal/sitemap/sitemap.go:22-28`) so composition with the sitemap package is by construction: the engine returns `[]sitemap.URLItem`, exactly like `writePaginatedPages`, `writeCourseLandingPages`, and `writeListingDetailPages` already do, and those flow into `extraSitemapURLs` in `writeAllPaginatedContent`. **This design does not touch `internal/sitemap` at all.** `lastmod_from` is the only genuinely new capability, and it is what flemming's `sitemap.yaml` already uses at the config level.

### 3.4 Derived fields

Three derivation forms, exactly covering what exists today and nothing more:

- `template:` — a Go `text/template` over the record map. Covers `href` for courses (`/courses/{{.slug}}/`), listings (`/listings/{{.id}}/`), and posts (`{{.base_path}}/blog/{{.slug}}/`, which is the one that *does* prepend `base_path`).
- `money: {field, symbol}` — reproduces `courses.formatMoney` exactly (drops `.00`, returns `""` for `cents <= 0`).
- `default_from: {slugify: <field>}` — reproduces `courses.Slugify` exactly.

**On `base_path`:** posts prepend it (`postToMap`, posts.go:30); courses and listings do not. The template makes this expressible per collection (`{{.__base_path}}` exposed as a reserved key) without hardcoding either convention. **Do not "fix" this during migration** — casaboa's `app.js:9-12` and ffreis's `course.gohtml:42` both compensate on the consuming side and would double-prefix.

### 3.5 JSON-LD

**Decision: the schema emits no JSON-LD, and provides no JSON-LD axis.**

Rationale from evidence: *every* JSON-LD in the fleet today is template-side, and each one is different in ways a config schema would model badly.

- `ffreis-website/src/templates/pages/post.gohtml:34` — `BlogPosting`, uses `toJSON` for safe string interpolation
- `flemming-website/src/templates/pages/ssyb.gohtml:6-24` — `Course`, plus a separate `breadcrumb_jsonld` block calling a `breadcrumb_jsonld_script` partial
- `ffreis-forma/src/templates/partials/head.gohtml:34-36` — a three-way branch: `WebSite`+`Organization` on index, `Product`+`BreadcrumbList` when `pages.<n>.schema_type == "Product"`, generic `{schema_type}` otherwise
- courses/projects/listings — none at all

A schema that could express all of these would just be a template engine. The collection schema's contribution to JSON-LD is that **it puts the record on the page as `.CurrentX`**, which is all any of these templates need. Forma's `Product` block currently reaches `dig .SiteData "products" (trimPrefix .PageName "product-")`; after migration it becomes `dig .CurrentProduct "name"` — strictly simpler.

If a future need for compiler-emitted JSON-LD appears, add it as a *separate* feature keyed off `.CurrentX`, not as a collection axis.

### 3.6 JS-blob export — the hard constraint

**Decision: satisfy it via `publish:`, not via a JS emitter.**

`publish: [{key: listings, value: records}]` writes `[]any` of `map[string]any` to `siteData["listings"]` — the same type, from the same conversion, into the same key, that `injectListingsData` writes today. casaboa's `base.gohtml` then does `{{toJSON .}}` unchanged. The output is byte-identical **because it is the same `json.Marshal` call on a structurally identical value**, not because someone reimplemented the serialization.

That reduces the byte-compat risk to exactly one question — *does the engine's record map have the same keys and values as `listings.baseMap`?* — which is a unit test on 25 known records, not a serialization audit.

An optional `expose:` axis is still worth having for *new* sites:

```yaml
expose:
  js_global:
    name: CASABOA_LISTINGS
    inject: before_head_end     # same injection point as the tracker/dev-data transforms
```

but it must be built **after** the casaboa migration lands and verified with a byte-diff against the template-emitted form. **Casaboa must not be the first user of it.** Reason: the JS-blob is the one place in this whole refactor where a defect ships silently — a wrong key order or a mis-escaped character still renders a valid page, still passes linkcheck, still passes the sitemap check, and only breaks `app.js`'s filtering at runtime in the browser.

### 3.7 What the engine does NOT own

Deliberately excluded, with reasons:

| Excluded | Why |
|---|---|
| JSON-LD emission | §3.5 |
| Scheduling / sessions / date math | Template-side today (`components.gohtml` `course_cronograma`); see acid test 1 |
| Language redirect stubs | Only posts do this (`writePostLangStub`); adding it generically changes three sites' output |
| RSS | Blog-only, and blog is out of migration scope |
| Section gating policy | Stays in `sectionTable`; collections *register* into it |
| Sitemap XML generation | Stays in `internal/sitemap`; collections return `[]sitemap.URLItem` |

---

## 4. Rationale, and what was rejected

**Rejected: one Go interface, four implementations.** This is what exists now in spirit (three near-identical `ToSiteDataList`/`ToCurrentX` pairs plus three near-identical `writeXPages` functions). It removes duplication but leaves every new collection needing a Go change, a new flag, a new `sectionTable` entry, and a compiler release. Forma's nine stub templates and flemming's seven copy-pasted templates are the symptom that the *website* is the right place to declare shape.

**Rejected: records as pure `map[string]any` passthrough.** Simplest and most "generic", but it is exactly the thing that breaks the golden-file requirement — see Q1, where two templates already read fields the structs drop. Passthrough would change rendered HTML on day one of the migration. The explicit `fields:` projection with `passthrough: false` reproduces the current field set exactly; flipping `passthrough: true` (or adding a field) then becomes a deliberate, reviewable, single-line diff per site.

**Rejected: put the collection config in site data (`site.d/…`).** Tempting — it is already merged, already contract-validated, already language-layered. But `mergeSiteDataStrict` (`internal/sitegen/site_data_layers.go:117-146`) **errors on any leaf path defined in two layers**, so shared+per-language config could not override each other; and the config would then need contract entries, and the compiler would be reading its own configuration through the validator it configures. A sibling file next to `sitemap.yaml` is the established pattern (`sitemap.yaml`, `tests/output.yaml`) and has none of these problems.

**Rejected: a `collections` CLI flag taking inline YAML.** Puts shape in CI. Every shape change would need a deployer PR.

**Rejected: making the engine own JSON-LD and scheduling.** §3.5 and acid test 1.

**Accepted with reservations: `derive:` as a mini-language.** Three forms (`template`, `money`, `slugify`) is small enough to not be a DSL, and each maps 1:1 to code that exists. If a fourth form is ever needed, that is the signal to stop and ask whether the template should compute it instead.

**Key insight driving `source.kind`:** three of the four consumers do *not* read a dedicated file.
- flemming's courses come from `siteData["courses"]`, assembled by strict deep-merge across **three** layers (`10-courses.yaml` structure, `20-calendar.yaml` schedule, `30-investment.yaml` pricing) whose leaf paths are provably disjoint (verified: `courses.ssmbb.variants.presencial` has `investment` only in the investment layer, `cronograma`/`start_text` only in the calendar layer).
- forma's products come from `siteData["products"]` (`site.d/40-products.yaml`).
- casaboa's listings come from `siteData["listings"]` when `-listings-file` is absent, and from the file when it is present — the file being *the same file*, copied into `src/data/site.d/` by `casaboa-website/Makefile:51` and *also* passed by path at line 64.

A file-only source model would break all three. `source.kind: site_data` is not a convenience — it is required.

---

## 5. Acid tests

### 5.1 flemming's course-with-variants-with-sessions — **partial fit, needs an escape hatch**

This is the honest answer, so it gets the most space.

**The shape.** `flemming-data/data/shared/site.d/10-courses.yaml` (1768 lines) defines 7 courses keyed by slug (`ssyb ssgb ssbb ssmbb cqia cqe cre`). Each has `details_href`, `variant_order` (an ordered list of variant keys), `agenda` (anchor/menu_title/heading — used by a *different* page), `styled_title` (a list of `{text, class}` parts), and `variants` (a map). Each variant has `enabled`, `label`, three separate styled-title variants (`offering_`, `section_`, plain), `mode`, `location_text`, `start_text`, `duration`, `duration_hours`, `cronograma` (`{text, href, layout, sessions[]}` where each session is `{date, start_time, end_time, hours, notes?}`), and `investment` (`{total, installments_text, discounts:{stackable, cash, company, referral}}`). `ssbb` has **six** variants across two families (`standard-*` / `upgrade-*`); the rest have three.

**What the generic schema handles cleanly:**

- **Records:** `source: {kind: site_data, key: courses, shape: keyed_map, id_field: slug}` — exact fit, and it preserves the three-layer merge because the engine reads *after* merge.
- **URL pattern:** `detail.path: "/{{.slug}}"` (flemming builds without `-clean-urls`, so `/ssyb.html`). Exact fit.
- **Templates:** one `course.gohtml` replaces seven. The three real per-record differences are all data:
  - stylesheet `css/common/{{.slug}}/{{.slug}}.css` → `{{dig .CurrentCourse "slug"}}`
  - CSS classes `ssyb-content` / `ssyb-aside` / `ssyb` → same
  - `missing pages.ssyb.title` error strings → `printf` with the slug
- **Pagination:** `index.enabled: false`. flemming's hub (`treinamentos.gohtml`) is a hand-written marketing page, not a paginated index. Exact fit — and confirms the schema must make pagination optional rather than assume it.
- **Sitemap:** `sitemap: {changefreq: monthly, priority: "0.8", lastmod_from: <template file>}` reproduces all seven `sitemap.yaml` entries, and *removes* seven hand-maintained blocks from `flemming-website/sitemap.yaml`. Clean fit, and a genuine improvement.
- **Scheduling rendering:** entirely template-side (`components.gohtml:364-633`, `course_cronograma`). The engine hands the whole record to the template; sessions ride along as nested `[]any`/`map[string]any`. **No engine support needed.** This is the good news: the "hard" part of flemming isn't hard for *this* abstraction.

**Where it does NOT fit — three specific gaps:**

**(a) Variant-family template dispatch (`ssbb`).** `ssbb.gohtml` defines two extra 50-line sub-templates, `ssbb_standard_body` and `ssbb_upgrade_body`, which read *differently-prefixed* page-copy fields (`standard_description_html` vs `upgrade_description_html`, ×6 fields each) and different UI labels (`billing_note_ssbb_standard` vs `billing_note`). This is not "one record, one page" — it is "one record, one page, N sub-renders each choosing a copy namespace by variant family". The generic schema has no axis for it, and adding one (`variant_families`) would be a special case with exactly one user.

*Recommendation:* leave it in the template. `ssbb.gohtml`'s two sub-templates move into the shared `course.gohtml` guarded by `{{if eq (dig .CurrentCourse "slug") "ssbb"}}`, or better, the copy-field prefix becomes a per-variant data field (`copy_prefix: standard`) and the shared template does `{{dig $pg (printf "%s_description_html" $prefix)}}`. Either way it is a **flemming-data change, not a compiler change**. This should be called out as a finding: *the engine can collapse 7 templates to 1 only if flemming-data absorbs ssbb's variant-family split as data.*

**(b) The copy/structure split across two site-data namespaces.** Every course page reads *both* `courses.<slug>` (structure, shared across languages) and `pages.<slug>` (copy: `title`, `meta_description`, `canonical_url`, `breadcrumb_label`, `aside_aria`, `description_html`, `results_intro`, `results_html`, `results_note`, `program_intro_html`, `modules_html`, `requirements_intro`, `requirements_html`, `differentials_html`, `jsonld_course_name`, …). The generic model's `.CurrentX` supplies only the record.

*Recommendation:* add a narrow, well-defined knob rather than pretending it fits:

```yaml
detail:
  page_data_from: "pages.{{.slug}}"   # merged into template data as .CurrentPageData
```

This is small, expressible, and useful beyond flemming (forma needs the mirror image — see 5.3). It is **not** a general "merge arbitrary site data" feature; it is one dotted-path template resolved per record.

**(c) The `agenda` page is a cross-record aggregation the engine does not model.** `agenda.gohtml` + `agenda_course_section` / `agenda_course_offering_section` partials iterate every course × every enabled variant × every session to build a site-wide schedule view. That is neither an index page nor a detail page. It reads `siteData["courses"]` directly and will keep working untouched, because `source.kind: site_data` means the engine *reads* that key rather than replacing it.

*Explicit non-goal:* the schema will not gain a "cross-record aggregate page" axis. One user, and the template already does it.

**(d) Pre-existing flemming-specific code the refactor must not orphan.** `internal/sitegen/sanity.go` **already hardcodes flemming's domain into the compiler** — `ValidateSiteSanity` reads `siteData["courses"]` as a map, walks `variants.<v>.cronograma.sessions[]`, checks `start_text` matches the first session date and `duration_hours == sum(sessions.hours)`, and includes a Portuguese month-name parser (`parsePtBRMonth`, `janeiro`…`dezembro`). It runs by default (`-sanity` defaults true) and silently no-ops on ffreis/casaboa/forma because their `siteData["courses"]` is not a `map[string]any`.

This is the *existing* escape hatch, and it is the wrong shape: a domain-specific validator wearing the name "generic sanity checks". The collection work should either (i) leave it exactly alone (safest, recommended for this phase) or (ii) later re-express it as a per-collection `validate:` block. **Do not delete or "generalize" it during the migration** — it is the only thing preventing a schedule/pricing desync from shipping to a live commercial site.

**Verdict on acid test 1:**

> The generic schema handles flemming on the record, URL, template, pagination, and sitemap axes cleanly, and collapses 7 templates to 1 and 7 hand-written sitemap entries to 1 rule. It does **not** handle three things, which need escape hatches of specific shapes:
> 1. **Per-record page copy** — needs `detail.page_data_from: "pages.{{.slug}}"`. Small, general, recommended.
> 2. **`ssbb`'s variant-family copy split** — needs *no compiler feature*; it needs `flemming-data` to express the standard/upgrade prefix as a per-variant data field. If that data change is refused, `ssbb` stays a bespoke template and the collapse is 6-to-1, not 7-to-1.
> 3. **The `agenda` cross-record aggregation** — out of scope by design; keeps working because the source is site data.
>
> Scheduling itself needs **no** engine support: it is template-rendered from nested record data today and stays that way. The one place scheduling is in Go — `internal/sitegen/sanity.go` — is a pre-existing flemming-shaped special case that this refactor should leave untouched.

### 5.2 casaboa — **clean fit, with the one high-risk detail**

- Records: `wrapped_list` under `listings:`, from `-listings-file` with `site_data` fallback. Exact fit, including the file-order-preserving `sort: []`.
- URL: `detail.path: "/listings/{{.id}}/"` → `dist/listings/<id>/index.html`. Exact.
- Template: `listing` (already `internal: true` via `30-listing-template.yaml`). Exact, and the two-phase render guard (`{{if .CurrentListing}}`) is preserved by keeping the same `RenderPages`-then-per-record flow.
- Pagination: `index.enabled: false`. casaboa's hub is `index.gohtml`, client-rendered. Exact.
- Sitemap: `{changefreq: weekly, priority: "0.7"}`. Exact.
- JS blob: `publish: [{key: listings, value: records}]`. Byte-identical by construction (§3.6), verified against 25 records with zero field drift.

**The risk:** `sponsored` is `bool`. Under a typed struct an absent `sponsored:` yields `"sponsored":false`; under passthrough it would be *absent*. All 25 records set it, so today it is moot — but if casaboa-data ever adds a record without it, the `fields:` projection with `type: bool, default: false` keeps behaviour stable and passthrough would not. This is the concrete argument for `passthrough: false` as the default.

**Also note:** casaboa is not in the deployer. It builds from its own `Makefile` (which conditionally passes `-listings-file` at line 62-65) and has an empty-ish inventory entry. So its migration is a `casaboa-website` PR, not a deployer PR.

### 5.3 forma's products — **clean fit, biggest win, but forma isn't a compiler consumer yet**

**What actually exists** (confirmed exactly 9):

- 9 templates in `src/templates/pages/product-*.gohtml`, **each exactly one line**: `{{define "content"}}{{template "product-detail-content" .}}{{end}}`
- 9 entries in `src/data/site.d/30-pages.yaml:79-221` using YAML anchors — `product-objects-product-catalog: &product_detail_defaults` with `<<: *product_detail_defaults`, `footer_columns: *product_detail_footer`, `scripts: *product_detail_scripts` on the other 8; only `title`, `description`, `path` differ
- 9 entries in `src/data/site.d/40-products.yaml` keyed by slug (`name`, `price`, `category_name`, `category_href`)
- the "data_page hack": `pages.<n>.data_page` → `<body data-page="product-detail">` (`layout/base.gohtml:4`), read by `product-detail.js` for client-side routing
- the join: `{{$slug := trimPrefix .PageName "product-"}}{{$pd := dig .SiteData "products" $slug}}` in both `partials/head.gohtml:35` and `partials/product-detail.gohtml:1`

**Under the schema:**

```yaml
products:
  section: products
  source: {kind: site_data, key: products, shape: keyed_map, id_field: slug}
  index:  {enabled: false}                 # products.html stays a normal page
  detail:
    enabled: true
    template: product                      # ONE template
    path: "/product-{{.slug}}.html"
    data_key: CurrentProduct
    page_data_from: "pages.product-{{.slug}}"   # transitional; see below
    sitemap: {changefreq: monthly, priority: "0.6"}
```

Deletes: 9 stub templates → 1; the `trimPrefix .PageName "product-"` join in two partials → `.CurrentProduct`; and once the 9 `pages.product-*` entries fold into `40-products.yaml` records (adding `title`, `description`, `path` to each), also ~145 lines of anchored YAML in `30-pages.yaml`. `data_page`, `chrome`, `mega`, `footer_variant`, `footer_columns`, `scripts` all become one set of collection-level constants rather than nine merge-key copies.

The `data_page` mechanism itself is **not** removed by this — it is a body-attribute convention for client-side JS routing, orthogonal to collections. It just stops being copy-pasted 9 times.

**Where it does not fit / what to watch:**

- `path: "/product-{{.slug}}.html"` is a *flat* URL. Confirm the generic detail writer derives the on-disk target from the path string (`…/` → `index.html` inside; otherwise the literal file). Both course/listing writers hardcode `filepath.Join(outDir, "<section>", id, "index.html")` today, so this is genuinely new code, not a refactor.
- Forma has **no inventory entry** and builds only from its own `Makefile:34` (`-website-root . -out dist -js-inline-threshold=0`). It is a prototype, not a deployed site.
- Forma commits `src/assets/sitemap.xml` by hand and has `base_url: "https://forma.ffreis.com"` in `site.d/10-site.yaml`. `copyStaticAssets` copies `sitemap.xml` verbatim (`assets.go:90`), then `maybeGenerateSitemap` **overwrites it** once default-on sitemap generation lands, because `resolveSitemapBaseURL` will now resolve `base_url`. That is an existing collision independent of this work, but it will surface during forma's compiler adaptation and should be resolved first.
- There is no `FORMA-ROADMAP-2026-08-29.md` in the repo. The planning context is `IMPLEMENTATION_NOTES.md`, `docs/PROTOTYPE-HISTORY.md`, and `AGENTS.md`.

### 5.4 ffreis projects + courses — **exact fit, and the only two that must be byte-identical**

Both map directly onto the schema (see §3.2). The `fields:` projection reproduces the struct field sets including zero-value defaults; `derive:` reproduces `Slugify`, `coursePath`, and `formatMoney`; `sort:` reproduces the `(order, title)` comparator; `publish:` reproduces `injectProjectsHomeCarousel` (`siteData["projects"]`, first N) and `injectCoursesHomeCarousel` (`pages.index.courses_carousel_items`, first N — including its silent no-op when `pages.index` is absent); `index:`/`detail:` reproduce `writePaginatedPages` and `writeCourseLandingPages` exactly, sitemap attrs included.

**What must be pinned by golden files before any of this moves:**

1. `dist/projects/index.html`, `dist/projects/page/2/index.html`, `dist/courses/index.html`, `dist/courses/<slug>/index.html` byte-for-byte for both `pt` and `en`, against both real (`ffreis-projects`, `ffreis-courses`) and mock (`ffreis-content-mocks`, 100 records ⇒ 9 pages at 12/page) content.
2. `dist/sitemap.xml` byte-for-byte, including the `/projects` + `/projects/` duplication (Q3).
3. The double-write / transform-skip behaviour (Q2) — assert that `dist/projects/index.html` has **no** `<link rel="alternate" hreflang=…>`, so a well-meaning fix during the refactor gets caught.
4. `available_languages` (Q1) — assert that `dist/projects/index.html` on the mock build renders **all** language badges on every card.
5. `duration_hours` (Q1) — assert that `dist/courses/<slug>/index.html` contains **no** `course-meta-item` duration chip.

Items 3–5 are tests that assert current *bugs*. They must be written that way, with a comment saying so, and fixed in separate follow-up PRs after the migration is green. That is the whole point of golden files here.

---

## 6. Migration plan, ordered by risk

**Phase 0 — prerequisites (no engine yet).**
- Land `feat/p0-4-sitemap-default-on` and the section-registry unification first. The engine registers into `sectionTable` and returns `[]sitemap.URLItem`; building against a moving target on either is avoidable churn.
- Write the golden-file harness for `ffreis-website` (pt+en × real+mock) and capture baselines from `origin/main`. Use `internal/outputtestcmd` where the assertion is structural; use raw byte comparison for the five items in §5.4. **Nothing else in this plan may start before these baselines exist.**

**Phase 1 — the engine, dark.** Add `internal/collections` (loader, projection, sort, derive) + `internal/buildcmd/collections.go` (publish, index writer, detail writer). Wire the config file discovery and `-collections-config`. **Do not delete anything. Do not wire any existing site to it.** Unit tests only. Risk: none — it is unreachable code until a `collections.yaml` exists.

**Phase 2 — projects (safest first).** *Why first:* smallest record type (6 fields), no detail pages, no JS blob, no derived fields beyond none, and the golden files already exist from Phase 0. Add `collections.yaml` to `ffreis-website` with only the `projects` collection; make `-projects-file` a deprecated adapter that populates `-collection-source projects=<path>` (see §7). Delete `internal/projects` and `injectProjectsHomeCarousel`/`writeProjectPages`. **Gate: golden files identical, including items 3 and 4 from §5.4.**

**Phase 3 — courses.** *Why second:* same site, same golden harness, but adds detail pages, `Slugify`, `formatMoney`, and `extra_fields`. Every one of those is a place a subtle bug lands (`formatMoney`'s `cents <= 0 → ""` and its `%02d` fraction are easy to get wrong). **Gate: `/courses/<slug>/index.html` byte-identical for all 3 real + 100 mock courses, and item 5 from §5.4.**

**Phase 4 — forma products.** *Why third:* highest ratio of deletion to risk (9 templates → 1), and forma is a **prototype with no production deployment and no inventory entry** — a defect there costs nothing. It also proves the flat-`.html` detail path and `page_data_from` before either is used on a live site. Do forma's sitemap.xml collision resolution (§5.3) as a separate prior PR.

**Phase 5 — casaboa listings.** *Why fourth, not earlier:* the JS blob is the only place a bug ships silently. Everything else in this refactor fails loudly (a missing page fails `linkcheck.ValidateAndReport`; a bad sitemap fails the output tests; a broken template fails `ExecuteTemplate`). A wrong `window.CASABOA_LISTINGS` renders a perfectly valid page and breaks filtering at runtime.

Required gate, all four:
1. Unit test: `collections` output for `casaboa-data/site.d/30-listings.yaml` deep-equals `listings.ToSiteDataList` output, record by record, key by key. (Keep `internal/listings` in the tree until this passes, purely as the oracle.)
2. Build both ways and `diff` the extracted `window.CASABOA_LISTINGS = …;` line byte-for-byte.
3. `diff -r` the whole `dist/pt/listings/` tree.
4. Manual browser check of the filter chips and the "reset search" path (`app.js:507`), because that is the code path a key-order or type change actually breaks.

Only then delete `internal/listings` and `injectListingsData`.

**Phase 6 — flemming (riskiest, last).** *Why last:* it is a live commercial site, it is bilingual, its data spans three strict-merged layers plus a per-language file-level replace done by the deployer (`deploy.yml:306-310` — `shared/site.d/` then `<lang>/site.d/`, same-named files **overwrite**, which is why `data/en/site.d/10-courses.yaml` is a full 917-line copy), it has the `agenda` cross-aggregation, `ssbb`'s variant families, and the `sanity.go` validator watching it. It also requires the two new schema features (`page_data_from`, `source.kind: site_data` with `keyed_map`) to be proven by forma first.

Sub-order within Phase 6:
- 6a. Land `page_data_from` and `keyed_map` (already exercised by forma).
- 6b. flemming-data PR: express `ssbb`'s standard/upgrade split as a per-variant `copy_prefix` field. **Data change only, no compiler involvement, verify output unchanged.**
- 6c. flemming-website PR: collapse `ssyb/ssgb/ssmbb/cqia/cqe/cre` (six, not seven) into one `course.gohtml`; keep `ssbb.gohtml` until 6b is proven in production.
- 6d. Move the seven `sitemap.yaml` entries to the collection's `sitemap:` block with `lastmod_from`.
- 6e. `ssbb` last.

**Deferred indefinitely — blog posts.** `internal/posts` shares `writePaginatedPages` and would fit the schema, but it adds Markdown parsing, image copying, RSS, per-post language redirect stubs, and `available_languages` validation. Migrating it buys little and risks the highest-traffic content on `ffreis.com`. Revisit only after all six phases are stable.

### 6.1 What could break silently — the watch list

| Risk | Where | Detection |
|---|---|---|
| **`window.CASABOA_LISTINGS` key order / escaping drift** | Phase 5 | Byte-diff of the emitted line; nothing else catches it |
| **Field-drift lighting up dead template branches** | Phases 2, 3 | Golden files items 4 and 5 (§5.4) |
| `nil` vs `""` from `dig` on absent-vs-zero-value fields | Phases 2, 3 | Golden files; verify `html/template`'s nil print behaviour explicitly |
| Losing the `/projects` + `/projects/` sitemap duplication | Phase 2 | Sitemap golden file |
| Accidentally *adding* hreflang to collection pages | Phases 2–5 | Golden file item 3, asserted as a negative |
| `formatMoney` `0 → ""` and `.00`-dropping | Phase 3 | Unit test on `{0, 1, 99, 100, 4900, 4999}` |
| `-content-source` `/mock/` guard stops covering new source paths | Phase 1 | Add `-collection-source` values to the loop at `options.go:177` **in the same PR** as the flag |
| Section pruning divergence | Every phase | `sectionTable` registration test; per-collection disabled-section test |
| Detail-template two-phase render (`{{if .CurrentX}}`) regression | Phases 3–6 | Assert no `/course/index.html` or `/listing/index.html` in `dist` |
| flemming schedule/price desync once `sanity.go` interacts with a new code path | Phase 6 | Do not touch `sanity.go`; assert it still runs and still fails on a seeded bad fixture |
| forma's hand-written sitemap.xml silently replaced | Phase 4 prerequisite | Resolve before Phase 4 |

---

## 7. Backward compatibility

### 7.1 The general pattern

The three existing flags become thin adapters in `parseBuildOptions`:

```
-projects-file <p>  ==  -collection-source projects=<p>
-courses-file  <p>  ==  -collection-source courses=<p>
-listings-file <p>  ==  -collection-source listings=<p>
```

Each logs a deprecation warning via the existing `slog` logger, and each is an error if the named collection is absent from `collections.yaml` (so a typo fails loudly rather than silently skipping content). Both spellings work for at least one release cycle.

`-items-per-page` stays as-is and keeps its current meaning for any collection whose config says `per_page_from_flag: items-per-page` — which is all of them initially. Its help text becomes `"…for any collection declaring per_page_from_flag: items-per-page, and blog"`.

`-disable-sections` is unchanged; collections register their `section:` into `sectionTable` at config-load time.

### 7.2 Does the pattern generalise to all four? No — and this matters

**projects, courses:** yes, straightforwardly. The deployer (`deploy.yml:464-484`) computes the path and passes the flag; it keeps working untouched. The deployer PR to switch to `-collection-source` is optional and can trail by months.

**listings:** yes for the flag, but with an extra wrinkle. `casaboa-website/Makefile:62-65` passes `-listings-file` **conditionally on the file existing**, and the same file is *also* copied into `src/data/site.d/` (line 51) so it lands in merged site data regardless. The `source.fallback: {kind: site_data, key: listings}` in the schema is what preserves the no-flag path — without it, a build with the layer but no flag would produce an empty `window.CASABOA_LISTINGS` instead of the raw (href-less) list, which is a silent regression in exactly the mode nobody tests.

**forma products: the flag pattern does not apply at all.** There is no flag to deprecate and no CLI path to adapt — the records already live in site data. Its transition story is a **pure website-repo refactor**: add `collections.yaml`, delete 8 stub templates, rename the 9th to `product.gohtml`, add `pages.product.internal: true`, and (optionally, later) fold the 9 `pages.product-*` entries into `40-products.yaml`. The compiler ships nothing forma-specific. The `page_data_from` knob exists precisely so the *second* step can be deferred: forma can migrate templates first and keep its `pages.product-*` entries, then fold them later.

**flemming courses: likewise no flag** — same site-data story as forma, but with a three-phase transition of its own (6a–6e above) because the template collapse and the `ssbb` data change are independent and must be sequenced.

So the honest generalisation is: **the deprecated-flag adapter pattern covers exactly the collections that have flags today (projects, courses, listings), and the other two need a "site-data-sourced collection" story that involves no CLI compatibility at all.** That is a feature of the design, not a gap — it is why `source.kind` exists.

### 7.3 Sites that never adopt collections

A website with no `collections.yaml` behaves exactly as today, minus the deleted Go paths. Since the three legacy flags map onto collections, a site passing `-projects-file` **without** a `collections.yaml` would break. Two options:

- **(a) Built-in default collections.** Ship `projects`, `courses`, `listings` definitions compiled into the binary, used when `collections.yaml` is absent or omits them. Zero-change compatibility for every consumer; `collections.yaml` becomes purely opt-in for *new* collections and *overrides*.
- **(b) Require `collections.yaml`.** Simpler engine, but every consumer needs a coordinated PR.

**Recommend (a).** It makes Phases 2 and 3 a pure internal refactor with *no* website-repo change at all, which means the golden files test the engine in isolation before any site's config becomes a variable. The built-in defaults are then deleted, one at a time, as each site adopts an explicit config — and each deletion is its own small, revertible PR.

### 7.4 Contract and section-data compatibility

Collections publish **after** contract validation (same as `loadOptionalContent` today, buildcmd.go:118 vs :105), so published keys need no contract entries and cannot dangle.

`buildDevDataPayload`'s four hardcoded fields (Q11) should become a map keyed by collection name — but note that changes the `window.__devBuild` JSON shape, which `casaboa-website/src/assets/js/dev-panel.js` and `ffreis-website`'s dev panel read. **Do this as a separate PR after Phase 5**, not inside a migration phase.

### 7.5 Explicitly unchanged

`-posts-dir` and `internal/posts`; `internal/sitemap`; `internal/sitegen/sanity.go`; RSS; hreflang; the `sitemap.yaml` config format; the `-disable-sections` flag surface.

---

## 8. Findings to report back

1. **`internal/sitegen/sanity.go` is a pre-existing, undeclared flemming special case inside the "generic" compiler** — Portuguese month names and all — enabled by default and silently inert on every other site. It should be acknowledged as the existing scheduling escape hatch and left alone by this refactor.
2. **Two live templates read record fields the compiler silently drops** (`projects[].available_languages`, `courses[].duration_hours`). Both are latent bugs whose "fix" is a one-word config change (`passthrough: true`) — but the fix must be deliberate and separate from the migration.
3. **`/projects/index.html` is written twice; the copy that ships skips hreflang injection, base-href validation, and structure validation.** True for every collection index page today.
4. **`/projects` and `/projects/` both appear in sitemap.xml.**
5. **Collection URLs ignore `pages.<name>.slug`,** so per-language slug translation does not reach `/courses/` or `/courses/<slug>/`. Latent (no site sets it yet) but real.
6. **flemming's EN data duplicates 917 lines of schedule/pricing structure** because the deployer's data assembly does file-level replace, not merge. Collections do not fix this; it is a deployer/data-layout question worth raising separately.
7. **forma commits a hand-written `src/assets/sitemap.xml` that default-on sitemap generation will overwrite** once `feat/p0-4-sitemap-default-on` lands. Independent of this work, but it blocks forma's compiler adaptation.
8. **The `-content-source` `/mock/` anti-leak guard enumerates content flags by hand** (`options.go:177`) and will silently stop covering content paths supplied by any new mechanism.

---

### Critical files for implementation

- `internal/buildcmd/buildcmd.go` — `loadOptionalContent`, `writeAllPaginatedContent`, `findPaginationTemplates`, `buildDevDataPayload`, `maybeGenerateSitemap`; the orchestration the engine plugs into
- `internal/buildcmd/paginatedpages.go` — `writePaginatedPages`, `shallowCopySiteDataForSection`, `injectPaginatedSection`, `resolvePagedTarget`; becomes the generic index writer
- `internal/buildcmd/course_landing.go` and `.../listing_detail.go` — the two near-identical detail writers that merge into one, plus `injectListingsData` (the `window.CASABOA_LISTINGS` producer)
- `internal/buildcmd/sections.go` and `.../options.go` — `sectionTable` (collections must register here) and the flag surface / `/mock/` guard
- `internal/courses/courses.go` — the richest existing loader (`Slugify`, `formatMoney`, `baseMap`/`ToCurrentCourse` split); the reference semantics `records.fields` + `derive` must reproduce exactly

Supporting references the implementer will need open: `casaboa-website/src/templates/layout/base.gohtml` (line 9, the JS-blob byte contract), `casaboa-data/site.d/30-listings.yaml` (the 25-record oracle), `flemming-data/data/shared/site.d/10-courses.yaml` (variants/sessions shape), `flemming-website/src/templates/pages/ssbb.gohtml` (the variant-family special case), `ffreis-forma/src/data/site.d/30-pages.yaml` lines 76–221 (the anchored 9-page block), and `ffreis-website-deployer/.github/workflows/deploy.yml` lines 448–540 (how the flags are actually passed).

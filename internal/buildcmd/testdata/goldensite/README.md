# `goldensite` — frozen golden-file fixture for the migrating collections

This is the Phase 0 harness from
`docs/decisions/0001-generic-content-collections.md` §6: a byte-for-byte
baseline of what the compiler emits for the content collections being migrated
onto `internal/collections`, captured from the **unmodified** engine and
re-asserted against every later code path.

It covers `projects` (Phase 2) and `courses` (Phase 3). `courses` is the harder
of the two, and is why the fixture grew: it adds per-record **detail** pages, a
`Slugify`-derived slug, two `formatMoney` display fields, and detail-only
`extra_fields` — four places the decision record flags as easy to get subtly
wrong.

## Why a vendored fixture rather than the real `ffreis-website` checkout

The decision record asks for `ffreis-website` × (pt, en) × (real, mock). Driving
the *live* sibling checkouts was tried first and is not usable as a CI gate:

1. CI for this repo checks out only this repo. `ffreis-website`,
   `ffreis-website-data`, `ffreis-projects`, `ffreis-courses` and
   `ffreis-content-mocks` are not present, so such a test could only ever
   `t.Skip()` — a gate that never runs.
2. A real `ffreis-website` build additionally needs `js/ffreis-web-forms.js`,
   injected from `platform/ffreis-web-forms` at deploy time. Without it the
   build fails in `assetusage` before it reaches a single project card.
3. A golden file whose input can change under it is not a golden file. The
   fixture is deliberately **frozen**: it pins the *compiler*, not the website.

So the fixture is a faithful reduction of the real thing. Everything that
decides the bytes of `/projects/` and `/courses/` is copied verbatim from
`ffreis-website`:

| Fixture file | Copied from |
|---|---|
| `partials/components.gohtml` → `project-card-full` | `ffreis-website/src/templates/partials/components.gohtml` |
| `partials/components.gohtml` → `pagination` | same file, `{{define "pagination"}}` |
| `pages/projects.gohtml` | `ffreis-website/src/templates/pages/projects.gohtml` |
| `pages/courses.gohtml` | `ffreis-website/src/templates/pages/courses.gohtml` (card markup verbatim) |
| `pages/course.gohtml` | `ffreis-website/src/templates/pages/course.gohtml` (`page` block verbatim) |
| `data/{pt,en}/site.d/50-ui.yaml` → `ui.nav.lang_links` | `ffreis-website-data/data/{pt,en}/site.d/50-ui.yaml` |
| `data/{pt,en}/site.d/50-ui.yaml` → `ui.course_detail` | same file |
| `site/src/data/site.d/05-languages.yaml` | `ffreis-website-data/data/shared/site.d/05-languages.yaml` |
| `content/projects.yaml` | `ffreis-projects/projects.yaml` (3 records) |
| `content/courses.yaml` | `ffreis-courses/courses.yaml` (3 records) |
| `content/mock/projects.yaml` | `ffreis-content-mocks/projects.yaml` (100 records) |
| `content/mock/courses.yaml` | `ffreis-content-mocks/courses.yaml` (100 records) |

The page chrome (real `nav`/`footer`/`head` partials, images, the other 20
page templates) is reduced, because none of it participates in the record
projection, sorting, pagination, hreflang or sitemap behaviour under test —
it would only add noise to the golden bytes. Two reductions in the course
templates are worth naming explicitly:

- `course.gohtml` drops the real template's `page_canonical` and
  `page_breadcrumbs` blocks, because this fixture's `head` partial defines
  neither. Every detail page therefore carries the same `pages.course`
  meta/canonical rather than a per-course one. Nothing under test reads them.
- `course.gohtml` drops the `<script src="/js/course.js">` tag, which the real
  template emits only for a sellable course. No fixture record sets
  `sale_mode`, so the file would be an unused asset and fail `assetusage`.

`ui.courses.<x>_badge` is deliberately **absent** from the fixture data, because
it is absent from `ffreis-website-data` too — the availability badges on
`/courses/` render empty on the live site, and the fixture reproduces that
rather than quietly fixing it.

## Layout

```
site/      the "website repo" half: templates, assets, shared data layer
data/      the "data repo" half: per-language site.yaml + site.d overlays
content/   projects.yaml + courses.yaml (real) and mock/ copies of both
golden/    committed expected output, one directory per matrix cell
```

`assembleGoldenSite` in `goldensite_test.go` merges these exactly the way
`ffreis-website-deployer/.github/workflows/deploy.yml` does:
`shared/site.d` -> `<lang>/site.d` -> `src/data/site.d`, then
`<lang>/site.yaml` -> `src/data/site.yaml`.

`content/mock/` is under a directory literally named `mock` on purpose: it
exercises the `-content-source` `/mock/` anti-leak guard (decision record Q9)
with a real path, the same shape the deployer produces
(`checkout/projects/mock/projects.yaml`).

The two single-collection cells (`pt-projects-only`, `pt-courses-only`) pin that
one collection's output does not depend on the other being present, and pin the
empty-collection branch (decision record Q5) of whichever template is left
without content.

## Regenerating

```
go test ./internal/buildcmd -run TestGoldenSite -update-golden
```

Only ever regenerate when the change to the emitted bytes is **intended and
reviewed**. Several of these goldens pin behaviour the decision record
classifies as *bugs* (Q1 for both collections, Q2, Q3); they are asserted
explicitly and by name in `golden_projects_quirks_test.go` and
`golden_courses_quirks_test.go` so that "fixing" one without meaning to fails
loudly rather than silently rewriting a baseline.

## Status

- **projects** — captured from the pre-`internal/collections` engine and
  re-verified, unchanged, against the migrated code path (Phase 2:
  `internal/projects` deleted).
- **courses** — captured from the pre-migration engine (`internal/courses` +
  `injectCoursesHomeCarousel` + `writeCoursePages` + `writeCourseLandingPages`)
  and re-verified, unchanged, against the migrated code path (Phase 3:
  `internal/courses` deleted).

Every byte in `golden/` is therefore output that BOTH implementations produced.

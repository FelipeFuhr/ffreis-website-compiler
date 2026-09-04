# `goldensite` — frozen golden-file fixture for the projects collection

This is the Phase 0 harness from
`docs/decisions/0001-generic-content-collections.md` §6: a byte-for-byte
baseline of what the compiler emits for the `projects` content collection,
captured from the **unmodified** (pre-`internal/collections`) engine and
re-asserted against every later code path.

## Why a vendored fixture rather than the real `ffreis-website` checkout

The decision record asks for `ffreis-website` × (pt, en) × (real, mock). Driving
the *live* sibling checkouts was tried first and is not usable as a CI gate:

1. CI for this repo checks out only this repo. `ffreis-website`,
   `ffreis-website-data`, `ffreis-projects` and `ffreis-content-mocks` are not
   present, so such a test could only ever `t.Skip()` — a gate that never runs.
2. A real `ffreis-website` build additionally needs `js/ffreis-web-forms.js`,
   injected from `platform/ffreis-web-forms` at deploy time. Without it the
   build fails in `assetusage` before it reaches a single project card.
3. A golden file whose input can change under it is not a golden file. The
   fixture is deliberately **frozen**: it pins the *compiler*, not the website.

So the fixture is a faithful reduction of the real thing. Everything that
decides the bytes of `/projects/` is copied verbatim from `ffreis-website`:

| Fixture file | Copied from |
|---|---|
| `partials/components.gohtml` → `project-card-full` | `ffreis-website/src/templates/partials/components.gohtml` |
| `partials/components.gohtml` → `pagination` | same file, `{{define "pagination"}}` |
| `pages/projects.gohtml` | `ffreis-website/src/templates/pages/projects.gohtml` |
| `data/{pt,en}/site.d/50-ui.yaml` → `ui.nav.lang_links` | `ffreis-website-data/data/{pt,en}/site.d/50-ui.yaml` |
| `site/src/data/site.d/05-languages.yaml` | `ffreis-website-data/data/shared/site.d/05-languages.yaml` |
| `content/projects.yaml` | `ffreis-projects/projects.yaml` (3 records) |
| `content/mock/projects.yaml` | `ffreis-content-mocks/projects.yaml` (100 records) |

The page chrome (real `nav`/`footer`/`head` partials, images, the other 20
page templates) is reduced, because none of it participates in the record
projection, sorting, pagination, hreflang or sitemap behaviour under test —
it would only add noise to the golden bytes.

## Layout

```
site/      the "website repo" half: templates, assets, shared data layer
data/      the "data repo" half: per-language site.yaml + site.d overlays
content/   projects.yaml (real) and mock/projects.yaml (mock)
golden/    committed expected output, one directory per matrix cell
```

`assembleGoldenSite` in `golden_projects_test.go` merges these exactly the way
`ffreis-website-deployer/.github/workflows/deploy.yml` does:
`shared/site.d` -> `<lang>/site.d` -> `src/data/site.d`, then
`<lang>/site.yaml` -> `src/data/site.yaml`.

`content/mock/` is under a directory literally named `mock` on purpose: it
exercises the `-content-source` `/mock/` anti-leak guard (decision record Q9)
with a real path, the same shape the deployer produces
(`checkout/projects/mock/projects.yaml`).

## Regenerating

```
go test ./internal/buildcmd -run TestGoldenProjects -update-golden
```

Only ever regenerate when the change to the emitted bytes is **intended and
reviewed**. Three of these goldens pin behaviour the decision record classifies
as *bugs* (Q1, Q2, Q3); they are asserted explicitly and by name in
`golden_projects_test.go` so that "fixing" one without meaning to fails loudly
rather than silently rewriting a baseline.

## Status

These baselines were captured from the pre-`internal/collections` engine and
have since been re-verified, unchanged, against the migrated code path
(Phase 2: the `projects` collection now loads through `internal/collections`
and `internal/projects` is deleted). Every byte in `golden/` is therefore
output that BOTH implementations produced.

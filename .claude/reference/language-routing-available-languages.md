## Content-level language routing (`available_languages`)

Blog posts, courses, and projects may not exist in all languages a site supports.
The compiler handles this via an `available_languages` field on each content item.

**How it works:**

- If `available_languages` is absent or empty, the item is considered available in all
  languages — no redirect stub is generated.
- If `available_languages` is a non-empty list of URL prefix codes (e.g. `[en]`), the
  compiler checks whether the current build language (derived from `base_path` in site
  data) is in the list.
  - **If present:** item is rendered normally.
  - **If absent:** a redirect stub (`dist/<section>/<slug>/index.html`) is written
    instead. The stub uses `window.location.replace()` + `<meta http-equiv="refresh">`
    to forward the visitor to the best available language version. Redirect target
    priority: `language_default` (from shared site data) → `en` → first in the list.

**Blog posts** (`<posts-repo>/<slug>/index.md` frontmatter):
```yaml
available_languages: [en]
```

**Courses / Projects** (YAML data, per item):
```yaml
available_languages: [en, pt]
```

**Shared site data required** (`data/shared/site.d/05-languages.yaml`):
```yaml
language_default: en        # URL prefix of the site's default language
language_variants:
  - hreflang: en
    path: /en
  - hreflang: pt-BR
    path: /pt
```
`language_default` uses **URL prefix** codes (not hreflang codes). List
both in the website template repo contract under `compiler_consumed`.

**Template flag display** — listing templates use the `has` function to show which
languages each item is available in, reading `lang_links[].prefix` (URL prefix,
separate from `lang_links[].lang` which is the hreflang code):
```gohtml
{{range $langLinks}}{{if or (not $availLangs) (has $availLangs .prefix)}}<span class="lang-badge">{{.flag}}</span>{{end}}{{end}}
```

**Key packages:**
- `internal/buildcmd/langutil.go` — `currentLangPrefix`, `isAvailable`, `redirectTarget`
- `internal/buildcmd/redirectstub.go` — `writeRedirectStub`
- `internal/buildcmd/posts.go` — `writePostLangStub`, `renderAndWritePost`

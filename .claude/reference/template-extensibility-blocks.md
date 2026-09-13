## Template extensibility blocks

Two `{{block}}` overrides in `head.gohtml` allow page templates to inject custom
meta tags without duplicating the head partial:

- `{{block "page_canonical" .}}` — overrides the `<meta name="description">` and
  `<link rel="canonical">` tags. The `post.gohtml` template overrides this to use
  `.CurrentPost` data instead of `pages.post.*` from site data.

- `{{block "page_og_meta" .}}` — overrides the full og:/twitter: meta block. The
  `post.gohtml` template overrides this with article-specific OG tags and structured data.

Both blocks receive `.` (the full template data map) as their pipeline.

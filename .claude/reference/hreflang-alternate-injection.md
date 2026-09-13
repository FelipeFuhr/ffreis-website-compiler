## Hreflang alternate injection (`injectHreflangAlternates` in `internal/buildcmd/hreflang.go`)

Runs in `writePages` immediately after `transformPage` for every regular page.

If site data contains a top-level `language_variants` array, the compiler injects
`<link rel="alternate" hreflang="...">` tags into each page's `<head>` before `</head>`.
An `x-default` link is also emitted for the `language_default` value.

Expected data shape (typically in `shared/site.d/05-languages.yaml`):

```yaml
language_variants:
  - hreflang: "en"
    path: "/en"
  - hreflang: "pt-BR"
    path: "/pt"
language_default: "en"
```

URLs are constructed as `base_url + variant.path + "/" + pageName + "/"` (clean URLs).
Blog post pages and paginated listing pages are not injected (single-language content).

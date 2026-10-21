---
paths:
  - "internal/posts/**"
  - "internal/rss/**"
  - "internal/buildcmd/posts.go"
---

## Blog post processing (`-posts-dir` flag)

When `-posts-dir <path>` is passed to `build-static`, the compiler processes Markdown
blog posts in addition to the normal page build:

**Input layout expected in `<posts-dir>`:**
```
posts-dir/
  <slug>/
    index.md        # YAML frontmatter + Markdown body
    images/         # post images; copied to dist/blog/<slug>/images/
    videos/         # post videos; copied to dist/blog/<slug>/videos/
```

Only `images/` and `videos/` are published (`mediaSubdirs` in `posts.go`), so
anything else beside a post — drafts, sources, unprocessed originals — stays
out of the build output.

**Media rendering (`internal/posts/media.go`).** Markdown has exactly one
media syntax, `![alt](dest "title")`, and goldmark renders every one of them as
a bare `<img>`. A custom `ast.KindImage` renderer replaces that:

- **A video destination renders a real `<video>`.** `.mp4 .webm .ogv .ogg .mov
  .m4v` (see `internal/mediameta`). It carries `controls preload="metadata"
  playsinline`, an inner `<a href>` fallback, and `aria-label` from the alt
  text — `<video>` has no `alt` attribute. Before this, `![x](./videos/x.mp4)`
  emitted an `<img>` pointing at a video, which browsers render as a broken
  image.
- **A poster frame is auto-detected** beside the video: the same basename with
  `.webp/.jpg/.jpeg/.png`, first match wins. It supplies both the pre-play
  paint and the `width`/`height` that prevent layout shift — mp4/webm headers
  are never parsed, so a video with no poster gets no dimensions.
- **The `<video>` fallback is plain text, never an `<a href>` download link.**
  `internal/linkcheck` validates every `<a href>` against the set of generated
  *pages*, so an anchor pointing at an `.mp4` is reported as a broken internal
  link even though the file is in the output. Verified: it fails the build.
- **Images get intrinsic `width`/`height`** read off the file on disk. A
  missing or undecodable file omits them rather than failing the build.
- **No `loading`/`decoding`/`fetchpriority` is emitted here.** That is
  `injectDefaultLazyLoading`'s job (`internal/buildcmd/transform.go`), and it
  must stay the single owner: it works at *page* scope, which is the correct
  scope because a post page's template can place images above the post body,
  so "first image in the post" is not "first image on the page". It also
  defers to any `loading` value already present — so emitting one here would
  override its LCP exemption, and a post whose first media is a video would
  stamp `loading="lazy"` on the page's actual LCP image and trip the
  `lcp-image-not-lazy` convention gate.
- **A caption becomes `<figure>`/`<figcaption>` only in block context** — a
  paragraph whose sole child is the image. `<figure>` is flow content and is
  *invalid* inside `<p>`: emitting one there silently closes the paragraph in
  every browser, so the `<p>` wrapper is suppressed instead (the paragraph
  renderer is overridden for exactly this case). An image among text keeps its
  markdown title as a `title` attribute.
- **Destinations are resolved once**, by `ResolvePostPath`, shared with the
  frontmatter `thumbnail`. Post-relative paths become `/blog/<slug>/...`;
  absolute paths, `//` protocol-relative URLs and anything with a URI scheme
  pass through untouched — including a hostile `javascript:`, which goldmark's
  own `IsDangerousURL` guard blanks. Prefixing a scheme into a relative path
  would defuse that one case by accident while hiding it from the guard.

**What it generates:**
- `dist/blog/<slug>/index.html` — rendered post page (uses `src/templates/pages/post.gohtml`)
- `dist/blog/feed.xml` — RSS 2.0 feed for Substack import
- Replaces `pages.blog.posts` in site data with post metadata (injected after contract validation)

**Template data for post pages:** In addition to the standard `PageName` and `SiteData`,
post pages receive a `CurrentPost` map with: `title`, `date`, `summary`, `thumbnail`,
`canonical_url`, `tags`, `body_html`. The `post.gohtml` template accesses these via
`.CurrentPost`.

**Key packages:**
- `internal/posts/posts.go` — `LoadPostsDir`, `CopyPostMedia`, `ResolvePostPath`
- `internal/posts/media.go` — the `ast.KindImage`/`ast.KindParagraph` renderer
- `internal/mediameta` — media classification by extension, poster candidates
  and `ImageDimensions`. A third package rather than a helper in
  `internal/buildcmd`, because `buildcmd` imports `posts`: a dimension reader
  living there could not be reused by `posts` without an import cycle. It
  replaced a duplicate copy in `buildcmd/socialimage.go`.
- `internal/rss/rss.go` — `GenerateRSS`
- `internal/buildcmd/posts.go` — `injectPostsBlogList`, `writePostPages`, `writeRSSFeed`

**New dependency:** `github.com/yuin/goldmark` + `github.com/yuin/goldmark-meta`
for Markdown rendering and frontmatter parsing.

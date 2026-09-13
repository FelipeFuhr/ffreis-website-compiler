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
```

**What it generates:**
- `dist/blog/<slug>/index.html` — rendered post page (uses `src/templates/pages/post.gohtml`)
- `dist/blog/feed.xml` — RSS 2.0 feed for Substack import
- Replaces `pages.blog.posts` in site data with post metadata (injected after contract validation)

**Template data for post pages:** In addition to the standard `PageName` and `SiteData`,
post pages receive a `CurrentPost` map with: `title`, `date`, `summary`, `thumbnail`,
`canonical_url`, `tags`, `body_html`. The `post.gohtml` template accesses these via
`.CurrentPost`.

**Key packages:**
- `internal/posts/posts.go` — `LoadPostsDir`, `CopyPostImages`
- `internal/rss/rss.go` — `GenerateRSS`
- `internal/buildcmd/posts.go` — `injectPostsBlogList`, `writePostPages`, `writeRSSFeed`

**New dependency:** `github.com/yuin/goldmark` + `github.com/yuin/goldmark-meta`
for Markdown rendering and frontmatter parsing.

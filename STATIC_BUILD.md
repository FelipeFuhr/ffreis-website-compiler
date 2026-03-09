# Static Build

CLI entrypoint:

```bash
./ffreis-website-compiler/website-compiler help
```

This wrapper uses `podman` by default and falls back to `docker` when needed; no local Go installation is required.
Override the engine explicitly with:

```bash
CONTAINER_COMMAND=podman ./ffreis-website-compiler/website-compiler help
```

Image naming follows the same variable model used in `ffreis-python-onnx-model-converter`:

```bash
PREFIX=ffreis
IMAGE_PROVIDER=
IMAGE_TAG=local
IMAGE_PREFIX="${IMAGE_PROVIDER:+$IMAGE_PROVIDER/}${PREFIX}"
IMAGE_ROOT="${IMAGE_PREFIX}"
```

Default compiler image:

```bash
${IMAGE_ROOT}/website-compiler-cli:${IMAGE_TAG}
```

## Logging

The compiler uses structured logging via Go `slog`.

Environment variables:
- `LOG_LEVEL`: `debug|info|warn|error` (default: `info`)
- `LOG_FORMAT`: `text|json` (default: `text`)
- `LOG_SOURCE`: `true|false` (default: `false`)

Examples:

```bash
LOG_LEVEL=debug LOG_FORMAT=text ./ffreis-website-compiler/website-compiler build -website-root /path/to/website -out dist
```

```bash
LOG_LEVEL=info LOG_FORMAT=json ./ffreis-website-compiler/website-compiler serve -website-root /path/to/website -addr :8080
```

Serve hardening env vars (defaults shown):
- `SERVE_READ_TIMEOUT=10s`
- `SERVE_WRITE_TIMEOUT=15s`
- `SERVE_IDLE_TIMEOUT=60s`
- `SERVE_READ_HEADER_TIMEOUT=5s`
- `SERVE_SHUTDOWN_TIMEOUT=10s`
- `SERVE_MAX_HEADER_BYTES=1048576`

You can override fully with:

```bash
WEBSITE_COMPILER_IMAGE=docker.io/myorg/website-compiler-cli:dev ./ffreis-website-compiler/website-compiler help
```

Generate static HTML from `<website-root>/src/templates/pages/*.gohtml` using shared layout/components:

```bash
./ffreis-website-compiler/website-compiler build \
  -website-root /path/to/website \
  -out ffreis-website-compiler/dist
```

If `<website-root>/sitemap.yaml` exists, the compiler also generates `sitemap.xml` in output (including `lastmod`, `changefreq`, and `priority` when configured).
If no sitemap config is present, you can still generate sitemap automatically from page templates:

```bash
./ffreis-website-compiler/website-compiler build \
  -website-root /path/to/website \
  -out ffreis-website-compiler/dist \
  -sitemap-base-url https://www.example.com
```

The compiler expects this structure under `-website-root`:
- `<website-root>/src/templates` (gohtml)
- `<website-root>/src/assets` (css/js/images/fonts)

This writes:
- `dist/*.html` (rendered pages)
- copied assets from `src/assets/` (`css`, `fonts`, `images`, `js`, and known root files)

To regenerate directly into your original folder structure:

```bash
  ./ffreis-website-compiler/website-compiler build \
  -website-root /path/to/website \
  -out /path/to/website/dist
```

To export only HTML files (no asset copy):

```bash
./ffreis-website-compiler/website-compiler build \
  -website-root /path/to/website \
  -out ffreis-website-compiler/dist \
  -copy-assets=false
```

To use a custom sitemap config path:

```bash
./ffreis-website-compiler/website-compiler build \
  -website-root /path/to/website \
  -out ffreis-website-compiler/dist \
  -sitemap-config /path/to/sitemap.yaml
```

To generate **self-contained HTML** (CSS/JS/images inlined into each page):

```bash
./ffreis-website-compiler/website-compiler build \
  -website-root /path/to/website \
  -out ffreis-website-compiler/dist \
  -inline-assets
```

In `-inline-assets` mode, external assets are not copied, because each HTML file is standalone and ready for static hosting (for example, S3 bucket upload).

## Build A Different Site Path

Compile one different site folder by passing root/paths via CLI:

```bash
./ffreis-website-compiler/website-compiler build \
  -website-root /path/to/another-website \
  -out ffreis-website-compiler/dist
```

Or pass paths explicitly:

```bash
./ffreis-website-compiler/website-compiler build \
  -templates-dir ../another-website/src/templates \
  -assets-dir ../another-website/src/assets \
  -out ffreis-website-compiler/dist
```

## Gateway Endpoints (Centralized JS)

Endpoint mapping is centralized in:
- `<website-root>/src/assets/js/gateway-config.js`
- `<website-root>/src/assets/js/gateway.js`

Current page scripts use endpoint names instead of hardcoded URLs:
- `submitEnrollment`
- `submitContact`

Adjust `baseUrl` and endpoint paths in `gateway-config.js` to point to API Gateway/Lambda routes.

## Compose Real-Time Compile

From the stitcher folder:

```bash
cd ffreis-website-stitcher
make compose-up
```

This starts:
- `compiler-watch`: watches file changes and recompiles automatically
- `preview`: serves `ffreis-website-compiler/dist` at `http://localhost:8088`

Edit files under:
- `<website-root>/src/templates`
- `<website-root>/src/assets`

If you change compiler source code under `ffreis-website-compiler`, rebuild the watch service image:

```bash
cd ffreis-website-stitcher
make compose-rebuild
```

To run self-contained output mode, set `INLINE_ASSETS: \"true\"` in `docker-compose.yml` and restart.

## Hello World Example

A minimal example lives in:
- `ffreis-website-compiler/examples/hello-world`

Build it with:

```bash
./ffreis-website-compiler/website-compiler build \
  -website-root ffreis-website-compiler/examples/hello-world \
  -out ffreis-website-compiler/examples/hello-world/dist
```

## Sitemap YAML Format

The sitemap config file supports per-URL `lastmod` metadata:

```yaml
base_url: "https://www.example.com"
default_lastmod: "2026-03-07"
urls:
  - path: "/"
    lastmod_from: "src/templates/pages/index.gohtml"
    changefreq: "weekly"
    priority: "1.0"
  - path: "/contato.html"
    lastmod: "2026-03-01"
```

Rules:
- `base_url` is required.
- `urls` must have at least one entry.
- `lastmod` must be `YYYY-MM-DD` when set.
- `lastmod_from` can be relative to `website-root` or absolute path.
- If both `lastmod` and `lastmod_from` are missing, `default_lastmod` is used (if set).

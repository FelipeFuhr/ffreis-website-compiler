package buildcmd

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestPNG writes a real, decodable PNG of the given dimensions to path
// — injectSocialImage decodes the file to compute og:image:width/height, so
// a fixture with real image bytes (not a placeholder string) is required to
// exercise that path honestly.
func writeTestPNG(t *testing.T, path string, width, height int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 100, G: 61, B: 95, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write test PNG: %v", err)
	}
}

func TestInjectSocialImage_NoOpWithoutBaseURL(t *testing.T) {
	assetsDir := t.TempDir()
	writeTestPNG(t, filepath.Join(assetsDir, "img", "og-image.png"), 1200, 630)

	html := "<head></head>"
	got, toCopy, err := injectSocialImage(html, map[string]any{}, assetsDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != html {
		t.Fatalf("expected unchanged html without base_url, got %q", got)
	}
	if toCopy != nil {
		t.Fatalf("expected nil toCopy without base_url, got %v", toCopy)
	}
}

func TestInjectSocialImage_NoOpWithoutImageFile(t *testing.T) {
	assetsDir := t.TempDir() // no img/og-image.* written

	html := "<head></head>"
	got, toCopy, err := injectSocialImage(html, map[string]any{"base_url": "https://example.com"}, assetsDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != html {
		t.Fatalf("expected unchanged html without an og-image file, got %q", got)
	}
	if toCopy != nil {
		t.Fatalf("expected nil toCopy without an og-image file, got %v", toCopy)
	}
}

// An authored og:image always suppresses the site-wide og-image.* default.
// Here the authored reference names a file this build does not produce, so
// there is additionally nothing to fingerprint and the html is returned
// byte-identical — see TestInjectSocialImage_FingerprintsAuthoredAbsoluteOGImage
// for the case where the authored file does exist and is rewritten.
func TestInjectSocialImage_NoOpWhenTemplateAlreadyDeclaresOGImage(t *testing.T) {
	assetsDir := t.TempDir()
	writeTestPNG(t, filepath.Join(assetsDir, "img", "og-image.png"), 1200, 630)

	html := `<head><meta property="og:image" content="https://example.com/custom.png"></head>`
	got, toCopy, err := injectSocialImage(html, map[string]any{"base_url": "https://example.com"}, assetsDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != html {
		t.Fatalf("expected template's own og:image to win unchanged, got %q", got)
	}
	if toCopy != nil {
		t.Fatalf("expected nil toCopy when template already declares og:image, got %v", toCopy)
	}
}

func TestInjectSocialImage_NoOpWhenTemplateAlreadyDeclaresTwitterImage(t *testing.T) {
	assetsDir := t.TempDir()
	writeTestPNG(t, filepath.Join(assetsDir, "img", "og-image.png"), 1200, 630)

	html := `<head><meta name="twitter:image" content="https://example.com/custom.png"></head>`
	got, _, err := injectSocialImage(html, map[string]any{"base_url": "https://example.com"}, assetsDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != html {
		t.Fatalf("expected template's own twitter:image to win unchanged, got %q", got)
	}
}

func TestInjectSocialImage_InjectsAbsoluteURLAndDimensions(t *testing.T) {
	assetsDir := t.TempDir()
	writeTestPNG(t, filepath.Join(assetsDir, "img", "og-image.png"), 1200, 630)

	html := "<head></head>"
	got, toCopy, err := injectSocialImage(html, map[string]any{"base_url": "https://example.com/"}, assetsDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{
		`<meta property="og:image:width" content="1200">`,
		`<meta property="og:image:height" content="630">`,
		`<meta name="twitter:image" content="https://example.com/img/og-image.`,
	} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Errorf("expected output to contain %q, got:\n%s", want, got)
		}
	}
	// base_url's trailing slash must not produce a double slash before "img/".
	if bytes.Contains([]byte(got), []byte("https://example.com//img/")) {
		t.Errorf("double slash after base_url, got:\n%s", got)
	}

	if len(toCopy) != 1 {
		t.Fatalf("expected exactly one asset to copy, got %d: %v", len(toCopy), toCopy)
	}
	for hashed, orig := range toCopy {
		if orig != "img/og-image.png" {
			t.Errorf("expected original path img/og-image.png, got %q", orig)
		}
		if !bytes.Contains([]byte(got), []byte(hashed)) {
			t.Errorf("expected injected tags to reference the fingerprinted path %q", hashed)
		}
	}
}

func TestInjectSocialImage_ExtensionPriorityOrder(t *testing.T) {
	assetsDir := t.TempDir()
	// Write BOTH .jpg and .png — .png must win per socialImageExtensions order.
	writeTestPNG(t, filepath.Join(assetsDir, "img", "og-image.png"), 10, 10)
	if err := os.WriteFile(filepath.Join(assetsDir, "img", "og-image.jpg"), []byte("not a real jpg, must not be read"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	relPath, _, ok := findSocialImage(assetsDir)
	if !ok {
		t.Fatal("expected a social image to be found")
	}
	if relPath != filepath.ToSlash(filepath.Join("img", "og-image.png")) {
		t.Fatalf("expected og-image.png to win over og-image.jpg, got %q", relPath)
	}
}

func TestInjectSocialImage_BasePathPrefixesTheURL(t *testing.T) {
	assetsDir := t.TempDir()
	writeTestPNG(t, filepath.Join(assetsDir, "img", "og-image.png"), 100, 100)

	html := "<head></head>"
	got, _, err := injectSocialImage(html, map[string]any{"base_url": "https://example.com"}, assetsDir, "/pt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains([]byte(got), []byte(`content="https://example.com/pt/img/og-image.`)) {
		t.Errorf("expected base_url + basePath prefix in the image URL, got:\n%s", got)
	}
}

// ── template-authored social images (the un-fingerprinted 404 bug) ───────────

// TestInjectSocialImage_FingerprintsAuthoredAbsoluteOGImage covers the shape a
// correctly-written template actually produces: Open Graph requires an absolute
// URL, so a head partial builds base_url + base_path + path (ffreis-arcade does
// exactly this). The generic fingerprinting pass treats any http(s):// value as
// external and skips it, so before this fix the authored tag named a file the
// build never wrote — a 404 social preview after every deploy, with a green
// build.
func TestInjectSocialImage_FingerprintsAuthoredAbsoluteOGImage(t *testing.T) {
	dir := t.TempDir()
	writeTestPNG(t, filepath.Join(dir, "assets", "social", "og-image.png"), 1200, 630)

	siteData := map[string]any{"base_url": "https://arcade.example.com"}
	html := `<html><head>` +
		`<meta property="og:image" content="https://arcade.example.com/assets/social/og-image.png">` +
		`</head></html>`

	got, toCopy, err := injectSocialImage(html, siteData, dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(got, `content="https://arcade.example.com/assets/social/og-image.png"`) {
		t.Error("authored og:image was left un-fingerprinted — the file is never copied, so the URL 404s")
	}
	if !strings.Contains(got, "https://arcade.example.com/assets/social/og-image.") {
		t.Errorf("rewritten og:image must stay an absolute same-origin URL, got: %s", got)
	}
	if len(toCopy) != 1 {
		t.Fatalf("expected exactly one copy instruction for the authored image, got %v", toCopy)
	}
	for hashed, original := range toCopy {
		if original != "assets/social/og-image.png" {
			t.Errorf("copy source = %q, want the authored image path", original)
		}
		if !strings.Contains(got, hashed) {
			t.Errorf("the hashed path %q registered for copying does not appear in the output: %s", hashed, got)
		}
	}
}

// TestInjectSocialImage_AuthoredOGImageDoesNotDoublePrefixBasePath guards the
// trap in reassembling these URLs: an authored absolute URL already contains
// the base path, and the fingerprinter prepends the base path again for
// root-absolute refs, so a naive implementation yields /en/en/assets/...
func TestInjectSocialImage_AuthoredOGImageDoesNotDoublePrefixBasePath(t *testing.T) {
	dir := t.TempDir()
	writeTestPNG(t, filepath.Join(dir, "assets", "social", "og-image.png"), 1200, 630)

	siteData := map[string]any{"base_url": "https://arcade.example.com"}
	html := `<html><head>` +
		`<meta property="og:image" content="https://arcade.example.com/en/assets/social/og-image.png">` +
		`</head></html>`

	got, _, err := injectSocialImage(html, siteData, dir, "/en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(got, "/en/en/") {
		t.Errorf("base path was prepended twice: %s", got)
	}
	if !strings.Contains(got, "https://arcade.example.com/en/assets/social/og-image.") {
		t.Errorf("expected one base-path prefix preserved in the absolute URL, got: %s", got)
	}
}

// TestInjectSocialImage_FingerprintsAuthoredRootRelativeOGImage covers the
// other authored shape — a site-relative path rather than an absolute URL.
func TestInjectSocialImage_FingerprintsAuthoredRootRelativeOGImage(t *testing.T) {
	dir := t.TempDir()
	writeTestPNG(t, filepath.Join(dir, "img", "card.png"), 800, 418)

	siteData := map[string]any{"base_url": "https://site.example.com"}
	html := `<html><head><meta property="og:image" content="/img/card.png"></head></html>`

	got, toCopy, err := injectSocialImage(html, siteData, dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, `content="/img/card.png"`) {
		t.Errorf("authored root-relative og:image was not fingerprinted: %s", got)
	}
	if len(toCopy) != 1 {
		t.Errorf("expected the authored image registered for copying, got %v", toCopy)
	}
}

// TestInjectSocialImage_LeavesExternallyHostedOGImageAlone keeps a genuinely
// third-party (CDN-hosted) preview image untouched — it is not ours to
// fingerprint and the file does not exist locally.
func TestInjectSocialImage_LeavesExternallyHostedOGImageAlone(t *testing.T) {
	dir := t.TempDir()
	siteData := map[string]any{"base_url": "https://site.example.com"}
	html := `<html><head><meta property="og:image" content="https://cdn.other.com/promo.png"></head></html>`

	got, toCopy, err := injectSocialImage(html, siteData, dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `content="https://cdn.other.com/promo.png"`) {
		t.Errorf("externally hosted og:image must be left unchanged, got: %s", got)
	}
	if len(toCopy) != 0 {
		t.Errorf("externally hosted og:image must not be registered for copying, got %v", toCopy)
	}
}

// TestInjectSocialImage_FingerprintsAuthoredTwitterImage covers the
// name="twitter:image" spelling, which uses `name` rather than `property` and
// so needs attribute-order-independent parsing.
func TestInjectSocialImage_FingerprintsAuthoredTwitterImage(t *testing.T) {
	dir := t.TempDir()
	writeTestPNG(t, filepath.Join(dir, "img", "card.png"), 800, 418)

	siteData := map[string]any{"base_url": "https://site.example.com"}
	// content before name, to prove order independence.
	html := `<html><head><meta content="/img/card.png" name="twitter:image"></head></html>`

	got, toCopy, err := injectSocialImage(html, siteData, dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, `content="/img/card.png"`) {
		t.Errorf("authored twitter:image was not fingerprinted regardless of attribute order: %s", got)
	}
	if len(toCopy) != 1 {
		t.Errorf("expected the authored twitter:image registered for copying, got %v", toCopy)
	}
}

// TestInjectSocialImage_AuthoredTagForMissingFileIsLeftAlone keeps the build
// non-fatal for a tag naming a file that does not exist: the reference is
// already broken and rewriting it to a hashed name nothing wrote would only
// obscure that.
func TestInjectSocialImage_AuthoredTagForMissingFileIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	siteData := map[string]any{"base_url": "https://site.example.com"}
	html := `<html><head><meta property="og:image" content="/img/absent.png"></head></html>`

	got, toCopy, err := injectSocialImage(html, siteData, dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `content="/img/absent.png"`) {
		t.Errorf("a tag naming a missing file should be left as-is, got: %s", got)
	}
	if len(toCopy) != 0 {
		t.Errorf("nothing to copy for a missing file, got %v", toCopy)
	}
}

// TestInjectSocialImage_AuthoredTagSuppressesTheSiteWideDefault locks the
// precedence this function has always had: template-authored content wins, so
// the og-image.* convention must not also inject a second, competing tag.
func TestInjectSocialImage_AuthoredTagSuppressesTheSiteWideDefault(t *testing.T) {
	dir := t.TempDir()
	writeTestPNG(t, filepath.Join(dir, "img", "og-image.png"), 1200, 630)
	writeTestPNG(t, filepath.Join(dir, "img", "card.png"), 800, 418)

	siteData := map[string]any{"base_url": "https://site.example.com"}
	html := `<html><head><meta property="og:image" content="/img/card.png"></head></html>`

	got, _, err := injectSocialImage(html, siteData, dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := strings.Count(got, "og:image\""); n > 1 {
		t.Errorf("expected the authored og:image to remain the only one, found %d: %s", n, got)
	}
	if strings.Contains(got, "img/og-image.") {
		t.Errorf("site-wide default was injected despite an authored tag: %s", got)
	}
}

package buildcmd

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
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

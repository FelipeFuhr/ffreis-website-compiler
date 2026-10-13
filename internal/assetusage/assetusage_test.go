package assetusage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate_PassesForReachableHTMLCSSAndJSAssets(t *testing.T) {
	assetsRoot := t.TempDir()
	files := map[string]string{
		filepath.Join(assetsRoot, "css", "main.css"):           `@import "nested/base.css"; body { color: #000; }`,
		filepath.Join(assetsRoot, "css", "nested", "base.css"): `.hero { color: #02accf; }`,
		filepath.Join(assetsRoot, "js", "main.js"):             `import "./shared.js"; console.log("ok");`,
		filepath.Join(assetsRoot, "js", "shared.js"):           `console.log("shared");`,
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	result, err := Validate(assetsRoot, map[string]string{
		"index": `<!doctype html><html><head><link rel="stylesheet" href="/css/main.css"></head><body><script src="js/main.js"></script></body></html>`,
	})
	if err != nil {
		t.Fatalf("expected validation success, got %v", err)
	}
	if len(result.UnusedCSS) != 0 || len(result.UnusedJS) != 0 || len(result.MissingRef) != 0 {
		t.Fatalf("expected no unused assets, got %#v", result)
	}
}

func TestValidate_FailsForUnusedAssets(t *testing.T) {
	assetsRoot := t.TempDir()
	files := map[string]string{
		filepath.Join(assetsRoot, "css", "main.css"):  `body { color: #000; }`,
		filepath.Join(assetsRoot, "css", "stale.css"): `.stale { display: none; }`,
		filepath.Join(assetsRoot, "js", "main.js"):    `console.log("ok");`,
		filepath.Join(assetsRoot, "js", "stale.js"):   `console.log("stale");`,
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	_, err := Validate(assetsRoot, map[string]string{
		"index": `<!doctype html><html><head><link rel="stylesheet" href="css/main.css"></head><body><script src="/js/main.js"></script></body></html>`,
	})
	if err == nil {
		t.Fatal("expected unused asset validation failure")
	}
	if !strings.Contains(err.Error(), "unused local css asset: css/stale.css") {
		t.Fatalf("expected stale css error, got %v", err)
	}
	if !strings.Contains(err.Error(), "unused local js asset: js/stale.js") {
		t.Fatalf("expected stale js error, got %v", err)
	}
}

func TestLocalStylesheetHrefs(t *testing.T) {
	html := `<!doctype html><html><head>
		<link rel="stylesheet" href="css/base.css">
		<link rel="stylesheet" href="/css/pages/index.css">
		<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Foo">
		<link rel="icon" href="favicon.ico">
	</head><body></body></html>`

	hrefs, err := LocalStylesheetHrefs(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"css/base.css", "css/pages/index.css"}
	if len(hrefs) != len(want) {
		t.Fatalf("got %#v, want %#v", hrefs, want)
	}
	for i, h := range want {
		if hrefs[i] != h {
			t.Fatalf("got %#v, want %#v", hrefs, want)
		}
	}
}

func TestValidate_FailsForMissingReferencedAsset(t *testing.T) {
	assetsRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(assetsRoot, "css"), 0o755); err != nil {
		t.Fatalf("mkdir css: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetsRoot, "css", "main.css"), []byte(`body { color: #000; }`), 0o644); err != nil {
		t.Fatalf("write css: %v", err)
	}

	_, err := Validate(assetsRoot, map[string]string{
		"index": `<!doctype html><html><head><link rel="stylesheet" href="css/missing.css"></head><body></body></html>`,
	})
	if err == nil {
		t.Fatal("expected missing asset validation failure")
	}
	if !strings.Contains(err.Error(), "missing local asset reference: css/missing.css") {
		t.Fatalf("expected missing asset error, got %v", err)
	}
}

// ── css/pages/<pageName>.css convention ────────────────────────────────────
//
// renderedPages here is deliberately the PRE-transform HTML (as Validate
// always receives it in the real pipeline — see buildcmd.go's call site
// comment) — these tests never put the per-page stylesheet <link> in the
// HTML themselves, proving the validator has to know the convention on its
// own rather than happening to find a tag transformPage hasn't injected yet.

func TestValidate_PerPageStylesheetExists_NotFlaggedUnused(t *testing.T) {
	assetsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(assetsRoot, "css", "pages", "login.css"), `.login{}`)

	result, err := Validate(assetsRoot, map[string]string{
		"login": `<!doctype html><html><head></head><body></body></html>`,
	})
	if err != nil {
		t.Fatalf("expected validation success, got %v", err)
	}
	if len(result.UnusedCSS) != 0 {
		t.Fatalf("expected css/pages/login.css not flagged unused, got %#v", result.UnusedCSS)
	}
}

func TestValidate_PerPageStylesheetAbsent_NoMissingRefError(t *testing.T) {
	// No css/pages/ directory at all — the overwhelming majority of pages in
	// the overwhelming majority of sites. Must NOT produce a "missing local
	// asset reference" error for a file nobody ever claimed to need.
	assetsRoot := t.TempDir()

	result, err := Validate(assetsRoot, map[string]string{
		"login": `<!doctype html><html><head></head><body></body></html>`,
	})
	if err != nil {
		t.Fatalf("expected validation success when no per-page stylesheet exists, got %v", err)
	}
	if len(result.MissingRef) != 0 {
		t.Fatalf("expected no missing-reference entries, got %#v", result.MissingRef)
	}
}

func TestValidate_PerPageStylesheetForDifferentPage_StillFlaggedUnused(t *testing.T) {
	// signup.css exists but only "login" is being rendered — a genuinely
	// orphaned file (e.g. a page that got renamed/removed) must still be
	// caught, not swallowed by the convention.
	assetsRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(assetsRoot, "css", "pages", "signup.css"), `.signup{}`)

	_, err := Validate(assetsRoot, map[string]string{
		"login": `<!doctype html><html><head></head><body></body></html>`,
	})
	if err == nil {
		t.Fatal("expected unused asset validation failure")
	}
	if !strings.Contains(err.Error(), "unused local css asset: css/pages/signup.css") {
		t.Fatalf("expected signup.css flagged unused, got %v", err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

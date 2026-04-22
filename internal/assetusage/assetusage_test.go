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

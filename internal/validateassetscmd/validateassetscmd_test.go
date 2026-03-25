package validateassetscmd

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_ValidatesReachableAssets(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	websiteRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(websiteRoot, "src", "assets", "css"),
		filepath.Join(websiteRoot, "src", "assets", "js"),
		filepath.Join(websiteRoot, "src", "data"),
		filepath.Join(websiteRoot, "src", "templates", "layout"),
		filepath.Join(websiteRoot, "src", "templates", "partials"),
		filepath.Join(websiteRoot, "src", "templates", "pages"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	files := map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", "main.css"):            `body { color: #000; }`,
		filepath.Join(websiteRoot, "src", "assets", "js", "main.js"):              `console.log("ok");`,
		filepath.Join(websiteRoot, "src", "data", "site.contract.yaml"):           "",
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>{{template "page" .}}</body></html>{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}<link rel="stylesheet" href="/css/main.css">{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "index.gohtml"):   `{{define "page"}}<script src="/js/main.js"></script><main>ok</main>{{end}}`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	if err := Run([]string{"-website-root", websiteRoot}, logger); err != nil {
		t.Fatalf("expected validate-assets success, got %v", err)
	}
}

func TestRun_FailsForUnusedAssets(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	websiteRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(websiteRoot, "src", "assets", "css"),
		filepath.Join(websiteRoot, "src", "assets", "js"),
		filepath.Join(websiteRoot, "src", "data"),
		filepath.Join(websiteRoot, "src", "templates", "layout"),
		filepath.Join(websiteRoot, "src", "templates", "partials"),
		filepath.Join(websiteRoot, "src", "templates", "pages"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	files := map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", "main.css"):            `body { color: #000; }`,
		filepath.Join(websiteRoot, "src", "assets", "css", "stale.css"):           `.stale { display: none; }`,
		filepath.Join(websiteRoot, "src", "data", "site.contract.yaml"):           "",
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>{{template "page" .}}</body></html>{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}<link rel="stylesheet" href="/css/main.css">{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "index.gohtml"):   `{{define "page"}}<main>ok</main>{{end}}`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	err := Run([]string{"-website-root", websiteRoot}, logger)
	if err == nil {
		t.Fatal("expected validate-assets failure")
	}
	if !strings.Contains(err.Error(), "unused local css asset: css/stale.css") {
		t.Fatalf("expected stale asset error, got %v", err)
	}
}

package sitegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newFragmentTestTemplates writes a minimal layout/partials/pages tree where
// "widget" is a fragment page (defines its own template name instead of
// "page") and "index" is an ordinary page that goes through the shared
// layout, so both code paths can be exercised from the same fixture.
func newFragmentTestTemplates(t *testing.T) []PageTemplate {
	t.Helper()
	websiteRoot := t.TempDir()
	templatesRoot := filepath.Join(websiteRoot, "src", "templates")
	for _, dir := range []string{
		filepath.Join(templatesRoot, "layout"),
		filepath.Join(templatesRoot, "partials"),
		filepath.Join(templatesRoot, "pages"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	files := map[string]string{
		filepath.Join(templatesRoot, "layout", "base.gohtml"):   `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>{{template "page" .}}</body></html>{{end}}`,
		filepath.Join(templatesRoot, "partials", "head.gohtml"): `{{define "head"}}<title>Site</title>{{end}}`,
		filepath.Join(templatesRoot, "pages", "index.gohtml"):   `{{define "page"}}<main>home</main>{{end}}`,
		filepath.Join(templatesRoot, "pages", "widget.gohtml"):  `{{define "widget"}}<div class="widget">{{dig .SiteData "pages" "widget" "label"}}</div>{{end}}`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	pages, err := LoadPageTemplatesFromRoot(templatesRoot)
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	return pages
}

func TestRenderPages_FragmentPageSkipsLayout(t *testing.T) {
	pages := newFragmentTestTemplates(t)
	siteData := map[string]any{
		"pages": map[string]any{
			"widget": map[string]any{"fragment": true, "label": "hi"},
		},
	}

	rendered, err := RenderPages(pages, siteData)
	if err != nil {
		t.Fatalf("RenderPages: %v", err)
	}

	widget := rendered["widget"]
	if strings.Contains(widget, "<html>") || strings.Contains(widget, "<head>") {
		t.Fatalf("expected fragment page to skip the layout wrapper, got %q", widget)
	}
	if !strings.Contains(widget, `<div class="widget">hi</div>`) {
		t.Fatalf("expected fragment page's own markup, got %q", widget)
	}
}

func TestRenderPages_NonFragmentPageUsesLayout(t *testing.T) {
	pages := newFragmentTestTemplates(t)
	siteData := map[string]any{
		"pages": map[string]any{
			"widget": map[string]any{"fragment": true, "label": "hi"},
		},
	}

	rendered, err := RenderPages(pages, siteData)
	if err != nil {
		t.Fatalf("RenderPages: %v", err)
	}

	index := rendered["index"]
	if !strings.Contains(index, "<html>") || !strings.Contains(index, "<head>") {
		t.Fatalf("expected non-fragment page to keep the layout wrapper, got %q", index)
	}
	if !strings.Contains(index, "<main>home</main>") {
		t.Fatalf("expected index content, got %q", index)
	}
}

func TestTraceSiteDataUsage_FragmentPageDoesNotRequireLayoutContentBlock(t *testing.T) {
	pages := newFragmentTestTemplates(t)
	siteData := map[string]any{
		"pages": map[string]any{
			"widget": map[string]any{"fragment": true, "label": "hi"},
		},
	}

	usedPaths, err := TraceSiteDataUsage(pages, siteData)
	if err != nil {
		t.Fatalf("expected tracing a fragment page (which never defines \"page\") to succeed, got %v", err)
	}

	found := false
	for _, p := range usedPaths {
		if p == "pages.widget.label" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected pages.widget.label traced as used, got %v", usedPaths)
	}
}

func TestIsPageFragment(t *testing.T) {
	siteData := map[string]any{
		"pages": map[string]any{
			"widget": map[string]any{"fragment": true},
			"index":  map[string]any{},
		},
	}
	if !IsPageFragment(siteData, "widget") {
		t.Error("expected widget to be a fragment page")
	}
	if IsPageFragment(siteData, "index") {
		t.Error("expected index not to be a fragment page")
	}
	if IsPageFragment(siteData, "missing") {
		t.Error("expected an unknown page name to default to non-fragment")
	}
	if IsPageFragment(map[string]any{}, "widget") {
		t.Error("expected no pages map at all to default to non-fragment")
	}
}

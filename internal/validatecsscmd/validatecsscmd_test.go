package validatecsscmd

import (
	"path/filepath"
	"testing"

	"ffreis-website-compiler/internal/testutil"
)

const (
	validateCSSLayoutTmpl   = `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>{{template "page" .}}</body></html>{{end}}`
	validateCSSHeadTmpl     = `{{define "head"}}<link rel="stylesheet" href="/css/main.css">{{end}}`
	validateCSSContractYAML = ""
)

func setupValidateCSSWebsiteRoot(t *testing.T) string {
	t.Helper()
	websiteRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(websiteRoot, "src", "assets", "css"),
		filepath.Join(websiteRoot, "src", "data"),
		filepath.Join(websiteRoot, "src", "templates", "layout"),
		filepath.Join(websiteRoot, "src", "templates", "partials"),
		filepath.Join(websiteRoot, "src", "templates", "pages"),
	} {
		testutil.MustMkdirAll(t, dir)
	}
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "data", "site.contract.yaml"):           validateCSSContractYAML,
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   validateCSSLayoutTmpl,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): validateCSSHeadTmpl,
	})
	return websiteRoot
}

func TestRun_SucceedsWithNoFindings(t *testing.T) {
	websiteRoot := setupValidateCSSWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", "main.css"):          `.hero { color: red; }`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "index.gohtml"): `{{define "page"}}<main class="hero">ok</main>{{end}}`,
	})

	if err := Run([]string{"-website-root", websiteRoot}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("expected validate-css to succeed, got %v", err)
	}
}

func TestRun_SucceedsButWarnsForUnusedRule(t *testing.T) {
	websiteRoot := setupValidateCSSWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", "main.css"):          `.hero { color: red; } .never-rendered { color: blue; }`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "index.gohtml"): `{{define "page"}}<main class="hero">ok</main>{{end}}`,
	})

	// validate-css is advisory -- it must never fail the build even when it
	// finds unused rules, since its class-presence heuristic has a known
	// false-positive source (classes added by client-side JS after load).
	if err := Run([]string{"-website-root", websiteRoot}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("expected validate-css to still succeed despite findings, got %v", err)
	}
}

func TestRun_FailsForInvalidWebsiteRoot(t *testing.T) {
	if err := Run([]string{"-website-root", "/nonexistent/path/does/not/exist"}, testutil.DiscardLogger()); err == nil {
		t.Fatal("expected an error for a nonexistent website root")
	}
}

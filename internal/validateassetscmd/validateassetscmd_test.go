package validateassetscmd

import (
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/testutil"
)

const (
	validateAssetsLayoutTmpl   = `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>{{template "page" .}}</body></html>{{end}}`
	validateAssetsHeadTmpl     = `{{define "head"}}<link rel="stylesheet" href="/css/main.css">{{end}}`
	validateAssetsContractYAML = ""
)

func setupValidateAssetsWebsiteRoot(t *testing.T) string {
	t.Helper()
	websiteRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(websiteRoot, "src", "assets", "css"),
		filepath.Join(websiteRoot, "src", "assets", "js"),
		filepath.Join(websiteRoot, "src", "data"),
		filepath.Join(websiteRoot, "src", "templates", "layout"),
		filepath.Join(websiteRoot, "src", "templates", "partials"),
		filepath.Join(websiteRoot, "src", "templates", "pages"),
	} {
		testutil.MustMkdirAll(t, dir)
	}
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "data", "site.contract.yaml"):           validateAssetsContractYAML,
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   validateAssetsLayoutTmpl,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): validateAssetsHeadTmpl,
	})
	return websiteRoot
}

func TestRun_ValidatesReachableAssets(t *testing.T) {
	websiteRoot := setupValidateAssetsWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", "main.css"):          `body { color: #000; }`,
		filepath.Join(websiteRoot, "src", "assets", "js", "main.js"):            `console.log("ok");`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "index.gohtml"): `{{define "page"}}<script src="/js/main.js"></script><main>ok</main>{{end}}`,
	})

	if err := Run([]string{"-website-root", websiteRoot}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("expected validate-assets success, got %v", err)
	}
}

func TestRun_FailsForUnusedAssets(t *testing.T) {
	websiteRoot := setupValidateAssetsWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", "main.css"):          `body { color: #000; }`,
		filepath.Join(websiteRoot, "src", "assets", "css", "stale.css"):         `.stale { display: none; }`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "index.gohtml"): `{{define "page"}}<main>ok</main>{{end}}`,
	})

	err := Run([]string{"-website-root", websiteRoot}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected validate-assets failure")
	}
	if !strings.Contains(err.Error(), "unused local css asset: css/stale.css") {
		t.Fatalf("expected stale asset error, got %v", err)
	}
}

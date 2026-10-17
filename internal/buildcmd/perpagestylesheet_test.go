package buildcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assetsWithPerPageSheet returns an assets dir containing
// css/pages/<page>.css, which is what injectPerPageStylesheet keys off.
func assetsWithPerPageSheet(t *testing.T, page string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "css", "pages")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, page+".css"), []byte(".x{color:red}"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInjectPerPageStylesheet_AddsTheLinkWhenTheTemplateHasNone(t *testing.T) {
	assets := assetsWithPerPageSheet(t, "studio")
	got := injectPerPageStylesheet(`<head><link rel="stylesheet" href="/css/base.css"></head>`, assets, "studio")
	if !strings.Contains(got, `href="/css/pages/studio.css"`) {
		t.Fatalf("expected the per-page link injected, got: %s", got)
	}
}

// TestInjectPerPageStylesheet_SkipsWhenTheTemplateAlreadyLinksIt is a
// regression test for a real production defect.
//
// ffreis-urbs hand-rolls this convention in head.gohtml so it can put
// css/responsive.css AFTER the page sheet — deliberately, because a media
// query adds no specificity, so a narrow-viewport override only wins if it
// comes later in source order (responsive.css's own header documents this).
// Appending a second copy at the end of <head> landed it after responsive.css
// and silently defeated every one of those overrides: `.metric-grid` rendered
// four columns on a phone. The page also shipped the same stylesheet twice.
//
// It surfaced only when urbs' compiler pin moved off the frozen `stable`
// pointer, which predated per-page CSS entirely — so the site had been
// building with NO per-page CSS at all, and the duplicate could not appear.
func TestInjectPerPageStylesheet_SkipsWhenTheTemplateAlreadyLinksIt(t *testing.T) {
	assets := assetsWithPerPageSheet(t, "studio")
	html := `<head>` +
		`<link rel="stylesheet" href="/css/base.css">` +
		`<link rel="stylesheet" href="/css/pages/studio.css">` +
		`<link rel="stylesheet" href="/css/responsive.css">` +
		`</head>`
	got := injectPerPageStylesheet(html, assets, "studio")
	if got != html {
		t.Fatalf("injected a duplicate stylesheet:\n%s", got)
	}
	if n := strings.Count(got, "css/pages/studio.css"); n != 1 {
		t.Errorf("expected the sheet linked exactly once, got %d", n)
	}
}

func TestInjectPerPageStylesheet_RecognisesEveryHrefSpelling(t *testing.T) {
	// A template may legitimately write the path root-absolute,
	// document-relative, or explicitly same-directory, and may append a cache
	// query. Any of them must count as "already linked".
	assets := assetsWithPerPageSheet(t, "studio")
	for _, href := range []string{
		"/css/pages/studio.css",
		"css/pages/studio.css",
		"./css/pages/studio.css",
		"/css/pages/studio.css?v=3",
	} {
		html := `<head><link rel="stylesheet" href="` + href + `"></head>`
		if got := injectPerPageStylesheet(html, assets, "studio"); got != html {
			t.Errorf("href %q was not recognised; got: %s", href, got)
		}
	}
}

func TestInjectPerPageStylesheet_ReadsRelRegardlessOfAttributeOrder(t *testing.T) {
	assets := assetsWithPerPageSheet(t, "studio")
	html := `<head><link href="/css/pages/studio.css" rel="stylesheet"></head>`
	if got := injectPerPageStylesheet(html, assets, "studio"); got != html {
		t.Fatalf("attribute order defeated the check; got: %s", got)
	}
}

func TestInjectPerPageStylesheet_ANonStylesheetLinkToTheSamePathDoesNotCount(t *testing.T) {
	// A preload names the file without applying it, so the stylesheet link
	// still has to be injected.
	assets := assetsWithPerPageSheet(t, "studio")
	html := `<head><link rel="preload" as="style" href="/css/pages/studio.css"></head>`
	got := injectPerPageStylesheet(html, assets, "studio")
	if !strings.Contains(got, `<link rel="stylesheet" href="/css/pages/studio.css">`) {
		t.Fatalf("a preload must not be mistaken for the stylesheet link; got: %s", got)
	}
}

func TestInjectPerPageStylesheet_ADifferentPagesSheetDoesNotCount(t *testing.T) {
	assets := assetsWithPerPageSheet(t, "studio")
	html := `<head><link rel="stylesheet" href="/css/pages/account.css"></head>`
	got := injectPerPageStylesheet(html, assets, "studio")
	if !strings.Contains(got, `href="/css/pages/studio.css"`) {
		t.Fatalf("expected studio's own sheet injected, got: %s", got)
	}
}

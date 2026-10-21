package buildcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/testutil"
)

// widgetFragmentTmpl is a fragment page that links its own stylesheet
// directly — since it skips the shared "head" partial entirely, it is
// responsible for its own asset references, same as any other page.
const widgetFragmentTmpl = `{{define "widget"}}<link rel="stylesheet" href="/css/main.css"><div class="widget">standalone</div>{{end}}`

// TestRun_FragmentPageRendersWithoutLayoutOrContractEntry proves that
// pages.<name>.fragment: true (a) makes RenderPages execute the page's own
// template name instead of "layout", so the output HTML carries none of the
// shared head/nav/footer chrome, (b) needs no site.contract.yaml entry — like
// pages.<name>.internal, it is compiler-managed metadata exempt from both the
// "required/allowed" and dangling-leaf contract checks, and (c) still gets
// its own CSS inlined: transformStylesheets treats a document with no </head>
// as entirely head content, so a fragment page's stylesheet is inlined
// exactly like a normal page's head CSS, never left as a separate file.
func TestRun_FragmentPageRendersWithoutLayoutOrContractEntry(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS): mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteYAML): `pages:
  widget:
    fragment: true
`,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):          "",
		filepath.Join(websiteRoot, "src", "templates", "pages", "widget.gohtml"): widgetFragmentTmpl,
	})

	outDir := t.TempDir()
	if err := Run([]string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-sitemap-base-url", "https://example.com",
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	widget := string(mustReadFile(t, filepath.Join(outDir, "widget.html")))
	for _, unwanted := range []string{"<html>", "<head>"} {
		if strings.Contains(widget, unwanted) {
			t.Errorf("expected fragment page output to omit %q, got %s", unwanted, widget)
		}
	}
	if !strings.Contains(widget, `<div class="widget">standalone</div>`) {
		t.Errorf("expected fragment page's own markup in output, got %s", widget)
	}
	if !strings.Contains(widget, "<style>") {
		t.Errorf("expected fragment page's own stylesheet to be inlined, got %s", widget)
	}
	if strings.Contains(widget, `<link rel="stylesheet"`) {
		t.Errorf("expected no surviving external stylesheet link, got %s", widget)
	}
}

// TestRun_FragmentAndInternalCompose proves fragment and internal are
// orthogonal flags: a page marked both renders (so its assets still count as
// "used") but produces no on-disk HTML file, exactly like an internal-only
// page — and still needs no contract entry for either flag.
func TestRun_FragmentAndInternalCompose(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS): mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteYAML): `pages:
  widget:
    fragment: true
    internal: true
`,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):          "",
		filepath.Join(websiteRoot, "src", "templates", "pages", "widget.gohtml"): widgetFragmentTmpl,
	})

	outDir := t.TempDir()
	if err := Run([]string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-sitemap-base-url", "https://example.com",
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	if _, err := os.Stat(filepath.Join(outDir, "widget.html")); !os.IsNotExist(err) {
		t.Fatalf("expected fragment+internal page to be excluded from output, got err=%v", err)
	}
}

// TestRun_NonFragmentPageStillUsesLayout is the contrast case: an ordinary
// page (no fragment flag) in the same build keeps the full layout wrapper.
func TestRun_NonFragmentPageStillUsesLayout(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):         mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteYAML):                 "",
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):         "",
		filepath.Join(websiteRoot, "src", "templates", "pages", "index.gohtml"): `{{define "page"}}<main>home</main>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-sitemap-base-url", "https://example.com",
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	index := string(mustReadFile(t, filepath.Join(outDir, fileIndexHTML)))
	if !strings.Contains(index, "<html>") || !strings.Contains(index, "<head>") {
		t.Errorf("expected non-fragment page to keep the layout wrapper, got %s", index)
	}
}

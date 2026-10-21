package buildcmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/testutil"
)

// ── injectNavigationEnhancements ──────────────────────────────────────────────

func TestInjectNavigationEnhancements_AddsViewTransitionStyle(t *testing.T) {
	html := `<html><head><title>X</title></head><body></body></html>`
	got := injectNavigationEnhancements(html)
	if !strings.Contains(got, "@view-transition") {
		t.Errorf("expected view-transition style injected, got: %s", got)
	}
}

func TestInjectNavigationEnhancements_AddsSpeculationRulesScript(t *testing.T) {
	html := `<html><head></head><body></body></html>`
	got := injectNavigationEnhancements(html)
	if !strings.Contains(got, "speculationrules") {
		t.Errorf("expected speculationrules script injected, got: %s", got)
	}
}

func TestInjectNavigationEnhancements_InjectedBeforeHeadClose(t *testing.T) {
	html := `<html><head></head><body></body></html>`
	got := injectNavigationEnhancements(html)
	headCloseIdx := strings.Index(got, "</head>")
	specIdx := strings.Index(got, "speculationrules")
	if specIdx > headCloseIdx {
		t.Errorf("expected speculations rules before </head>; headClose=%d specIdx=%d", headCloseIdx, specIdx)
	}
}

func TestInjectNavigationEnhancements_NoHeadTagIsNoOp(t *testing.T) {
	// If there's no </head>, nothing should be injected (no panic, no modification).
	html := `<html><body><p>hello</p></body></html>`
	got := injectNavigationEnhancements(html)
	if got != html {
		t.Errorf("expected no change when no </head>, got: %s", got)
	}
}

func TestInjectNavigationEnhancements_InjectedExactlyOnce(t *testing.T) {
	html := `<html><head></head><body></body></html>`
	got := injectNavigationEnhancements(html)
	if strings.Count(got, "speculationrules") != 1 {
		t.Errorf("expected speculationrules injected exactly once, got: %s", got)
	}
}

// ── injectPerPageStylesheet ────────────────────────────────────────────────────

func TestInjectPerPageStylesheet_FileExists_LinkInjected(t *testing.T) {
	dir := makeStylesheetDir(t, map[string]string{"css/pages/login.css": ".login{}"})
	html := `<html><head><link rel="stylesheet" href="/css/app.css"></head><body></body></html>`
	got := injectPerPageStylesheet(html, dir, "login")
	if !strings.Contains(got, `<link rel="stylesheet" href="/css/pages/login.css">`) {
		t.Errorf("expected per-page stylesheet link injected, got: %s", got)
	}
}

func TestInjectPerPageStylesheet_FileAbsent_NoOp(t *testing.T) {
	dir := t.TempDir() // no css/pages/ directory at all
	html := `<html><head><link rel="stylesheet" href="/css/app.css"></head><body></body></html>`
	got := injectPerPageStylesheet(html, dir, "login")
	if got != html {
		t.Errorf("expected byte-identical output when no per-page stylesheet exists, got: %s", got)
	}
}

func TestInjectPerPageStylesheet_EmptyPageName_NoOp(t *testing.T) {
	dir := makeStylesheetDir(t, map[string]string{"css/pages/.css": "x"}) // pathological, shouldn't matter
	html := `<html><head></head><body></body></html>`
	got := injectPerPageStylesheet(html, dir, "")
	if got != html {
		t.Errorf("expected no-op for empty pageName, got: %s", got)
	}
}

func TestInjectPerPageStylesheet_InjectedAfterExistingHeadLinks(t *testing.T) {
	// Cascade order: the per-page link must land AFTER any stylesheet links
	// already in the template (right before </head>) so page-specific rules
	// can override shared ones.
	dir := makeStylesheetDir(t, map[string]string{"css/pages/login.css": ".login{}"})
	html := `<html><head><link rel="stylesheet" href="/css/app.css"></head><body></body></html>`
	got := injectPerPageStylesheet(html, dir, "login")
	appIdx := strings.Index(got, "app.css")
	pageIdx := strings.Index(got, "pages/login.css")
	if appIdx < 0 || pageIdx < 0 || pageIdx < appIdx {
		t.Errorf("expected pages/login.css after app.css in document order, got: %s", got)
	}
	if strings.Index(got, "</head>") < pageIdx {
		t.Errorf("expected per-page link still inside <head>, got: %s", got)
	}
}

func TestInjectPerPageStylesheet_DifferentPageNameNotMatched(t *testing.T) {
	// Only the exact css/pages/<pageName>.css file is picked up — a stylesheet
	// for a DIFFERENT page must not leak onto this one.
	dir := makeStylesheetDir(t, map[string]string{"css/pages/signup.css": ".signup{}"})
	html := `<html><head></head><body></body></html>`
	got := injectPerPageStylesheet(html, dir, "login")
	if got != html {
		t.Errorf("expected no-op when only a different page's stylesheet exists, got: %s", got)
	}
}

func TestInjectPerPageStylesheet_FlowsThroughExistingInlinePipeline(t *testing.T) {
	// End-to-end proof that the injected link needs no new CSS-handling code:
	// transformStylesheets (the same function every other stylesheet goes
	// through) must pick it up and inline it exactly like an explicit
	// template-authored <link> tag.
	dir := makeStylesheetDir(t, map[string]string{
		"css/app.css":         "body{margin:0}",
		"css/pages/login.css": ".login{color:red}",
	})
	html := `<html><head><link rel="stylesheet" href="/css/app.css"></head><body></body></html>`
	withPageCSS := injectPerPageStylesheet(html, dir, "login")
	got, err := transformStylesheets(withPageCSS, dir, false, false, cssPruneOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, `<link rel="stylesheet"`) {
		t.Error("expected both stylesheets converted to <style> blocks, found a surviving <link>")
	}
	if !strings.Contains(got, "margin:0") || !strings.Contains(got, "color:red") {
		t.Errorf("expected both shared and per-page CSS content inlined, got: %s", got)
	}
}

// ── transformStylesheets ──────────────────────────────────────────────────────

func makeStylesheetDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		testutil.MustMkdirAll(t, filepath.Dir(p))
		testutil.WriteFiles(t, map[string]string{p: content})
	}
	return dir
}

func TestTransformStylesheets_HeadLinkBecomesStyleBlock(t *testing.T) {
	dir := makeStylesheetDir(t, map[string]string{"css/main.css": "body{color:red}"})
	html := `<html><head><link rel="stylesheet" href="/css/main.css"></head><body></body></html>`
	got, err := transformStylesheets(html, dir, false, false, cssPruneOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, `<link rel="stylesheet"`) {
		t.Error("expected head <link> replaced by <style>")
	}
	if !strings.Contains(got, "<style>") {
		t.Error("expected <style> block in output")
	}
	// Minified CSS should appear.
	if !strings.Contains(got, "color:red") {
		t.Error("expected CSS content inlined")
	}
}

func TestTransformStylesheets_BodyLinkGetsDeferredPattern(t *testing.T) {
	dir := makeStylesheetDir(t, map[string]string{"css/widget.css": ".w{}"})
	html := `<html><head></head><body><link rel="stylesheet" href="/css/widget.css"><p>hi</p></body></html>`
	got, err := transformStylesheets(html, dir, false, false, cssPruneOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `media="print"`) {
		t.Error("expected deferred body CSS with media=print")
	}
	if !strings.Contains(got, `onload="this.media='all'"`) {
		t.Error("expected onload handler in deferred CSS")
	}
	if !strings.Contains(got, "<noscript>") {
		t.Error("expected noscript fallback for deferred CSS")
	}
}

func TestTransformStylesheets_HeadMediaAttributePreserved(t *testing.T) {
	dir := makeStylesheetDir(t, map[string]string{"css/print.css": "@media print{body{font-size:12pt}}"})
	html := `<html><head><link rel="stylesheet" href="/css/print.css" media="print"></head></html>`
	got, err := transformStylesheets(html, dir, false, false, cssPruneOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `<style media="print">`) {
		t.Errorf("expected <style media=\"print\">, got: %s", got)
	}
}

func TestTransformStylesheets_InlineBodyCSSFlagInlinesBodyLink(t *testing.T) {
	dir := makeStylesheetDir(t, map[string]string{"css/widget.css": ".w{color:blue}"})
	html := `<html><head></head><body><link rel="stylesheet" href="/css/widget.css"></body></html>`
	got, err := transformStylesheets(html, dir, false, true, cssPruneOptions{}) // inlineBodyCSS=true
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, `media="print"`) {
		t.Error("expected body CSS inlined as <style>, not deferred")
	}
	if !strings.Contains(got, "<style>") {
		t.Error("expected inline <style> block for body CSS")
	}
}

func TestTransformStylesheets_NoHeadTagInlinesEverything(t *testing.T) {
	// If there's no </head>, the entire document is treated as head.
	dir := makeStylesheetDir(t, map[string]string{"css/main.css": "body{}"})
	html := `<html><link rel="stylesheet" href="/css/main.css"><p>content</p></html>`
	got, err := transformStylesheets(html, dir, false, false, cssPruneOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, `<link rel="stylesheet"`) {
		t.Error("expected all CSS inlined when no </head>")
	}
}

func TestTransformStylesheets_ExternalStylesheetsUntouched(t *testing.T) {
	dir := t.TempDir()
	html := `<html><head><link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Roboto"></head></html>`
	got, err := transformStylesheets(html, dir, false, false, cssPruneOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "fonts.googleapis.com") {
		t.Error("expected external stylesheet left unchanged")
	}
}

func TestTransformStylesheets_EmbedFontsTurnsURLsToDataURIs(t *testing.T) {
	dir := makeStylesheetDir(t, map[string]string{
		"css/main.css":  `@font-face{src:url("/fonts/f.woff2")}`,
		"fonts/f.woff2": "woff2bytes",
	})
	html := `<html><head><link rel="stylesheet" href="/css/main.css"></head></html>`
	got, err := transformStylesheets(html, dir, true, false, cssPruneOptions{}) // embedFonts=true
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, `"/fonts/f.woff2"`) {
		t.Error("expected font url() replaced with data URI")
	}
	if !strings.Contains(got, "data:font/woff2;base64,") {
		t.Error("expected base64 font data URI in inlined CSS")
	}
}

// ── deferLocalStylesheets ─────────────────────────────────────────────────────

func TestDeferLocalStylesheets_ProducesCorrectDeferredPattern(t *testing.T) {
	dir := makeStylesheetDir(t, map[string]string{"css/widget.css": ".w{}"})
	html := `<div><link rel="stylesheet" href="/css/widget.css"></div>`
	got, err := deferLocalStylesheets(html, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `media="print"`) {
		t.Errorf("expected media=print in deferred CSS, got: %s", got)
	}
	if !strings.Contains(got, `onload="this.media='all'"`) {
		t.Errorf("expected onload in deferred CSS, got: %s", got)
	}
	if !strings.Contains(got, `<noscript><link rel="stylesheet" href="/css/widget.css"></noscript>`) {
		t.Errorf("expected noscript fallback with original href, got: %s", got)
	}
}

func TestDeferLocalStylesheets_MissingFileLeftAsIs(t *testing.T) {
	dir := t.TempDir()
	html := `<link rel="stylesheet" href="/css/ghost.css">`
	got, err := deferLocalStylesheets(html, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Missing file should not get the deferred treatment (left unchanged).
	if strings.Contains(got, `media="print"`) {
		t.Error("expected missing CSS file not deferred")
	}
}

func TestDeferLocalStylesheets_ExternalLeftUntouched(t *testing.T) {
	dir := t.TempDir()
	html := `<link rel="stylesheet" href="https://cdn.example.com/style.css">`
	got, err := deferLocalStylesheets(html, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "cdn.example.com") {
		t.Error("expected external stylesheet unchanged")
	}
}

// ── addOrReplaceAttr ──────────────────────────────────────────────────────────

func TestAddOrReplaceAttr_ReplacesExistingValue(t *testing.T) {
	tag := `<link rel="stylesheet" href="/css/main.css" media="all">`
	got := addOrReplaceAttr(tag, "media", "print")
	if !strings.Contains(got, `media="print"`) {
		t.Errorf("expected media=print, got: %s", got)
	}
	if strings.Contains(got, `media="all"`) {
		t.Errorf("expected old media=all removed, got: %s", got)
	}
}

func TestAddOrReplaceAttr_AddsWhenAbsent(t *testing.T) {
	tag := `<link rel="stylesheet" href="/css/main.css">`
	got := addOrReplaceAttr(tag, "media", "print")
	if !strings.Contains(got, `media="print"`) {
		t.Errorf("expected media=print added, got: %s", got)
	}
}

func TestAddOrReplaceAttr_ReplacesOnloadValue(t *testing.T) {
	tag := `<link rel="stylesheet" href="/x.css" media="print" onload="old()">`
	got := addOrReplaceAttr(tag, "onload", "this.media='all'")
	if !strings.Contains(got, `onload="this.media='all'"`) {
		t.Errorf("expected onload replaced, got: %s", got)
	}
}

// ── resolveCSSAssetRef ────────────────────────────────────────────────────────

func TestResolveCSSAssetRef_AbsoluteRefStripsLeadingSlash(t *testing.T) {
	got := resolveCSSAssetRef("css/main.css", "/fonts/inter.woff2")
	if got != "fonts/inter.woff2" {
		t.Errorf("expected fonts/inter.woff2, got %q", got)
	}
}

func TestResolveCSSAssetRef_RelativeRefResolvedFromCSSDir(t *testing.T) {
	got := resolveCSSAssetRef("css/components/button.css", "../images/bg.png")
	if got != "css/images/bg.png" {
		t.Errorf("expected css/images/bg.png, got %q", got)
	}
}

func TestResolveCSSAssetRef_SameDirRelative(t *testing.T) {
	got := resolveCSSAssetRef("css/main.css", "fonts/inter.woff2")
	if got != "css/fonts/inter.woff2" {
		t.Errorf("expected css/fonts/inter.woff2, got %q", got)
	}
}

func TestResolveCSSAssetRef_RootCSSFile(t *testing.T) {
	// CSS file at the root level.
	got := resolveCSSAssetRef("main.css", "fonts/x.woff2")
	if got != "fonts/x.woff2" {
		t.Errorf("expected fonts/x.woff2, got %q", got)
	}
}

// ── resolvePageTarget ─────────────────────────────────────────────────────────

// Extension-free output is the only mode: there is no flag and no flat variant,
// so the old TestResolvePageTarget_NormalURL (asserting `about.html`) is gone
// rather than inverted — a test whose name says "normal" and asserts the shape
// the compiler no longer produces is worse than no test.
func TestResolvePageTarget_PageBecomesDirectoryIndex(t *testing.T) {
	outDir := t.TempDir()
	path, err := resolvePageTarget(outDir, "about", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join("about", "index.html")) {
		t.Errorf("expected about/index.html, got %q", path)
	}
}

// A fragment stays FLAT: it is fetched by the site's own JS at a literal URL
// (forma's `data-signed-in-fragment="fragment-x.html"`), so a directory index
// would 404 that fetch with no link for linkcheck to catch.
func TestResolvePageTarget_FragmentStaysFlat(t *testing.T) {
	outDir := t.TempDir()
	path, err := resolvePageTarget(outDir, "fragment-account-billing", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(path, "fragment-account-billing.html") {
		t.Errorf("expected fragment-account-billing.html, got %q", path)
	}
	if strings.Contains(path, filepath.Join("fragment-account-billing", "index.html")) {
		t.Errorf("a fragment must not become a directory index, got %q", path)
	}
}

// The index page is the one exception, and not an exception to the convention:
// `<dir>/index.html` IS its extension-free form, because the directory is the URL.
func TestResolvePageTarget_IndexStaysAtRoot(t *testing.T) {
	outDir := t.TempDir()
	path, err := resolvePageTarget(outDir, "index", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(path, "index.html") {
		t.Errorf("expected index.html, got %q", path)
	}
	if strings.HasSuffix(path, filepath.Join("index", "index.html")) {
		t.Errorf("index must not be nested into its own directory, got %q", path)
	}
}

// ── inlineLocalAssets (full -inline-assets mode) ──────────────────────────────

func TestInlineLocalAssets_InlinesCSSWithDataURIFonts(t *testing.T) {
	dir := makeStylesheetDir(t, map[string]string{
		"css/main.css":  `@font-face{src:url("/fonts/f.woff2")}`,
		"fonts/f.woff2": "fontdata",
	})
	html := `<html><head><link rel="stylesheet" href="/css/main.css"></head></html>`
	got, err := inlineLocalAssets(html, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// In full inline mode, fonts must be data URIs.
	if strings.Contains(got, `url("/fonts/f.woff2")`) {
		t.Error("expected font url() embedded as data URI in full inline mode")
	}
	if !strings.Contains(got, "data:font/woff2;base64,") {
		t.Error("expected base64 font data URI in full inline mode")
	}
}

func TestInlineLocalAssets_InlinesScript(t *testing.T) {
	dir := makeStylesheetDir(t, map[string]string{
		"css/main.css": "body{}",
		"js/app.js":    `alert("hello");`,
	})
	html := `<html><head><link rel="stylesheet" href="/css/main.css"></head>` +
		`<body><script src="/js/app.js"></script></body></html>`
	got, err := inlineLocalAssets(html, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, `src="/js/app.js"`) {
		t.Error("expected script src inlined in full inline mode")
	}
	if !strings.Contains(got, `alert("hello")`) {
		t.Error("expected script content present")
	}
}

func TestInlineLocalAssets_InlinesImage(t *testing.T) {
	dir := makeStylesheetDir(t, map[string]string{"css/main.css": "body{}"})
	mustWriteBytes(t, filepath.Join(dir, "images", "logo.png"), minimalPNG)
	html := `<html><head><link rel="stylesheet" href="/css/main.css"></head>` +
		`<body><img src="/images/logo.png" alt="logo"></body></html>`
	got, err := inlineLocalAssets(html, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, `src="/images/logo.png"`) {
		t.Error("expected image inlined as data URI in full inline mode")
	}
	if !strings.Contains(got, "data:image/png;base64,") {
		t.Error("expected base64 PNG data URI")
	}
}

// ── clean URLs integration ────────────────────────────────────────────────────

func TestRun_PagesAreEmittedAsDirectoryIndexes(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>hello</p>{{end}}`,
	})

	outDir := t.TempDir()
	args := []string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
	}
	if err := Run(args, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	// No flag: extension-free output is the only mode. agenda must be at
	// agenda/index.html, never agenda.html.
	agendaIndexPath := filepath.Join(outDir, "agenda", "index.html")
	if _, err := os.Stat(agendaIndexPath); err != nil {
		t.Fatalf("expected %s to exist: %v", agendaIndexPath, err)
	}
	// The old .html path must not exist.
	if _, err := os.Stat(filepath.Join(outDir, "agenda.html")); !os.IsNotExist(err) {
		t.Fatalf("expected agenda.html absent, got err=%v", err)
	}
}

// ── pipeline ordering: LQIP + fingerprint ────────────────────────────────────

func TestRun_LQIPDataSrcIsFingerprinted(t *testing.T) {
	// This test verifies that the LQIP data-src gets a fingerprinted URL,
	// while the LQIP src (data URI) is left as-is.
	websiteRoot := newTestWebsiteRoot(t)
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "assets", "images"))
	writePNG(t, filepath.Join(websiteRoot, "src", "assets"), "images/hero.png", 100, 50)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<img src="/images/hero.png" loading="eager" alt="hero">{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	html := string(mustReadFile(t, filepath.Join(outDir, fileAgendaHTML)))
	// The src must be a LQIP data URI.
	if !strings.Contains(html, `src="data:image/jpeg;base64,`) {
		t.Error("expected LQIP data URI in src")
	}
	// The data-src must be a fingerprinted path, not the original.
	if strings.Contains(html, `data-src="/images/hero.png"`) {
		t.Error("expected data-src to be fingerprinted (hashed), not original path")
	}
	if !strings.Contains(html, "data-src=\"/images/hero.") {
		t.Error("expected data-src to contain fingerprinted image path")
	}
}

// ── favicon & robots.txt ─────────────────────────────────────────────────────

func TestRun_FaviconAndRobotsCopiedToRoot(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "assets", "favicon.ico"):                "icodata",
		filepath.Join(websiteRoot, "src", "assets", "robots.txt"):                 "User-agent: *\nDisallow:",
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>ok</p>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	mustStat(t, filepath.Join(outDir, "favicon.ico"))
	mustStat(t, filepath.Join(outDir, "robots.txt"))
}

func TestRun_WellKnownDiscoveryAssetsCopiedToRoot(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "assets", ".well-known"))
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):            mainCSSContent,
		filepath.Join(websiteRoot, "src", "assets", "llms.txt"):                    "# Example\n\nAn example site.",
		filepath.Join(websiteRoot, "src", "assets", "humans.txt"):                  "/* TEAM */\nFelipe Fuhr",
		filepath.Join(websiteRoot, "src", "assets", "manifest.json"):               `{"name":"Example"}`,
		filepath.Join(websiteRoot, "src", "assets", ".well-known", "security.txt"): "Contact: mailto:security@example.com",
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):            "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML):  `{{define "page"}}<p>ok</p>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	mustStat(t, filepath.Join(outDir, "llms.txt"))
	mustStat(t, filepath.Join(outDir, "humans.txt"))
	mustStat(t, filepath.Join(outDir, "manifest.json"))
	mustStat(t, filepath.Join(outDir, ".well-known", "security.txt"))
}

func TestRun_SiteWebmanifestCopiedToRoot(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "assets", "site.webmanifest"):           `{"name":"Example"}`,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>ok</p>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	mustStat(t, filepath.Join(outDir, "site.webmanifest"))
}

// TestRun_SiteWebmanifestIconSrcRewrittenToFingerprintedPath reproduces the
// P0-3 bug: site.webmanifest/manifest.json used to be copied byte-for-byte
// verbatim (via copyExistingFiles), so a JSON payload like
// `{"icons":[{"src":"/images/logo-256.png"}]}` reached dist unchanged even
// though the actual icon file — like every other locally-referenced asset —
// is fingerprinted (renamed with a content hash) before it is written to
// dist. The manifest's icons[].src therefore pointed at a filename that never
// existed in the compiled output, 404ing in production. The icon is also
// referenced from a <link rel="icon"> tag here, mirroring the realistic
// favicon-generator output the bug was found in: that reference IS
// fingerprinted correctly (a different, already-working code path), so the
// hashed file really is the one present in dist — the manifest just needs to
// agree on the name.
func TestRun_SiteWebmanifestIconSrcRewrittenToFingerprintedPath(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "assets", "images"))
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "assets", "images", "logo-256.png"):     "pngdata",
		filepath.Join(websiteRoot, "src", "assets", "site.webmanifest"):           `{"name":"Example","icons":[{"src":"/images/logo-256.png","sizes":"256x256","type":"image/png"}]}`,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}<link rel="stylesheet" href="/css/main.css"><link rel="icon" href="/images/logo-256.png"><link rel="manifest" href="/site.webmanifest">{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>ok</p>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	manifestRaw := mustReadFile(t, filepath.Join(outDir, "site.webmanifest"))
	var manifest struct {
		Icons []struct {
			Src string `json:"src"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatalf("parsing output site.webmanifest: %v", err)
	}
	if len(manifest.Icons) != 1 {
		t.Fatalf("expected 1 icon in output manifest, got %d: %s", len(manifest.Icons), manifestRaw)
	}
	iconSrc := manifest.Icons[0].Src
	if iconSrc == "/images/logo-256.png" {
		t.Fatalf("expected icons[0].src to be rewritten to a fingerprinted path, got unfingerprinted %q", iconSrc)
	}

	// The path the manifest now points at must exist in dist — this is the
	// actual production symptom: browsers 404 fetching a stale icon name.
	iconPath := filepath.Join(outDir, filepath.FromSlash(strings.TrimPrefix(iconSrc, "/")))
	if _, err := os.Stat(iconPath); err != nil {
		t.Fatalf("manifest icons[0].src %q does not exist in dist: %v", iconSrc, err)
	}
}

func TestRun_WellKnownDiscoveryAssetsAbsentAreNoop(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>ok</p>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	for _, missing := range []string{"llms.txt", "humans.txt", "manifest.json", "site.webmanifest", filepath.Join(".well-known", "security.txt")} {
		if _, err := os.Stat(filepath.Join(outDir, missing)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be absent from output when not present in source, got err=%v", missing, err)
		}
	}
}

// ── CSS url() path rewriting in head (integration) ────────────────────────────

func TestRun_HeadCSSURLRewrittenToRootRelative(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "assets", "fonts"))
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "fonts", "f.woff2"):           "fontdata",
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           `@font-face{src:url("../fonts/f.woff2")}`,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>ok</p>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	html := string(mustReadFile(t, filepath.Join(outDir, fileAgendaHTML)))
	// The url() should be root-relative and fingerprinted, not relative to the CSS file.
	if strings.Contains(html, `url("../fonts/f.woff2")`) {
		t.Error("expected relative url() resolved to root-relative path")
	}
	if !strings.Contains(html, `url("/fonts/f.`) {
		t.Errorf("expected root-relative fingerprinted url in inlined CSS, got: %s", html)
	}
}

// ── copy-assets=false ────────────────────────────────────────────────────────

func TestRun_CopyAssetsFalseSkipsAllAssets(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>ok</p>{{end}}`,
	})

	outDir := t.TempDir()
	args := []string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-copy-assets=false",
	}
	if err := Run(args, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	// With copy-assets=false nothing should be in dist except the HTML.
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("reading outDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	// The agenda page is a DIRECTORY (agenda/index.html), not a file, because
	// every page but the index is emitted as a directory index. The assertion is
	// still "nothing but the page and the sitemap" — only the page's shape changed.
	for _, name := range names {
		if name != "agenda" && name != "sitemap.xml" {
			t.Errorf("unexpected entry in dist with copy-assets=false: %s", name)
		}
	}
}

// ── multiple pages (fingerprint cross-page consistency) ───────────────────────

func TestRun_SameAssetFingerprintedConsistentlyAcrossPages(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "assets", "images"))
	mustWriteBytes(t, filepath.Join(websiteRoot, "src", "assets", "images", "logo.png"), minimalPNG)

	const logoTag = `<img src="/images/logo.png" alt="logo">`
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", "index.gohtml"):   `{{define "page"}}` + logoTag + `{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}` + logoTag + `{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	extractImgSrc := func(html string) string {
		for _, line := range strings.Split(html, "\n") {
			if strings.Contains(line, "logo.") && strings.Contains(line, "<img") {
				start := strings.Index(line, `src="`) + 5
				end := strings.Index(line[start:], `"`)
				if start > 4 && end > 0 {
					return line[start : start+end]
				}
			}
		}
		return ""
	}

	indexHTML := string(mustReadFile(t, filepath.Join(outDir, "index.html")))
	agendaHTML := string(mustReadFile(t, filepath.Join(outDir, "agenda", "index.html")))
	srcIndex := extractImgSrc(indexHTML)
	srcAgenda := extractImgSrc(agendaHTML)

	if srcIndex == "" || srcAgenda == "" {
		t.Fatalf("could not extract img src from pages: index=%q agenda=%q", srcIndex, srcAgenda)
	}
	if srcIndex != srcAgenda {
		t.Errorf("expected same fingerprinted path on both pages, got:\n  index:  %q\n  agenda: %q", srcIndex, srcAgenda)
	}
}

// ── injectTracker ─────────────────────────────────────────────────────────────

func TestInjectTracker_NoopWhenDisabled(t *testing.T) {
	html := `<html><head><title>X</title></head><body></body></html>`
	got := injectTracker(html, buildOptions{trackerEnabled: false})
	if got != html {
		t.Errorf("expected no change when disabled, got: %s", got)
	}
}

func TestInjectTracker_NoopWhenRequiredFieldsMissing(t *testing.T) {
	html := `<html><head></head><body></body></html>`
	// enabled=true but version/site/endpoint empty → no injection
	got := injectTracker(html, buildOptions{trackerEnabled: true})
	if strings.Contains(got, "tracker-sdk") {
		t.Errorf("expected no injection when fields missing, got: %s", got)
	}
}

func TestInjectTracker_InjectsScriptAndInitBeforeHeadClose(t *testing.T) {
	html := `<html><head><title>X</title></head><body></body></html>`
	got := injectTracker(html, buildOptions{
		trackerEnabled:    true,
		trackerSDKVersion: "1.2.3",
		trackerSiteID:     "flemming",
		trackerEndpoint:   "https://events.flemming.com.br",
	})
	if !strings.Contains(got, `src="https://cdn.ffreis.com/tracker-sdk/v1.2.3/tracker.min.js"`) {
		t.Errorf("expected SDK script src with pinned version, got: %s", got)
	}
	if !strings.Contains(got, `siteId:"flemming"`) {
		t.Errorf("expected JSON-quoted siteId, got: %s", got)
	}
	if !strings.Contains(got, `endpoint:"https://events.flemming.com.br"`) {
		t.Errorf("expected JSON-quoted endpoint, got: %s", got)
	}
	if !strings.Contains(got, "DOMContentLoaded") {
		t.Errorf("expected DOMContentLoaded gate, got: %s", got)
	}
	// Injected block must end before </head>.
	headEnd := strings.Index(got, "</head>")
	scriptStart := strings.Index(got, "tracker-sdk")
	if scriptStart < 0 || headEnd < 0 || scriptStart > headEnd {
		t.Errorf("expected script before </head>; scriptStart=%d headEnd=%d", scriptStart, headEnd)
	}
}

func TestInjectTracker_OverrideCDNBase(t *testing.T) {
	html := `<html><head></head><body></body></html>`
	got := injectTracker(html, buildOptions{
		trackerEnabled:    true,
		trackerSDKVersion: "0.9.0",
		trackerSiteID:     "ffreis",
		trackerEndpoint:   "https://events.ffreis.com",
		trackerCDNBase:    "https://cdn-staging.example.com/",
	})
	if !strings.Contains(got, `src="https://cdn-staging.example.com/tracker-sdk/v0.9.0/tracker.min.js"`) {
		t.Errorf("expected overridden CDN base (trailing slash trimmed), got: %s", got)
	}
}

func TestInjectTracker_QuotesEmbeddedInValuesAreEscaped(t *testing.T) {
	html := `<html><head></head><body></body></html>`
	// A pathological siteId with a quote should be safely encoded.
	got := injectTracker(html, buildOptions{
		trackerEnabled:    true,
		trackerSDKVersion: "1.0.0",
		trackerSiteID:     `flem"ming`,
		trackerEndpoint:   "https://events.example.com",
	})
	if !strings.Contains(got, `siteId:"flem\"ming"`) {
		t.Errorf("expected escaped quote in siteId, got: %s", got)
	}
}

// ── injectScriptDefer ──────────────────────────────────────────────────────

func TestInjectScriptDefer_AddsDeferToLocalScript(t *testing.T) {
	html := `<body><script src="/app.js"></script></body>`
	got, err := injectScriptDefer(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Exact-shape check, not just substring presence: a regression here once
	// inserted the attribute into the CLOSING tag ("</script defer>"), which
	// still contains the substring ` defer` and so passed a looser
	// Contains-only check despite being invalid HTML.
	//
	// The attribute is BARE, not `defer="defer"`: html-validate's default
	// attribute-boolean-style ("omit") rejects the value form, so the XHTML
	// spelling put one markup error in every consumer's output per deferred
	// script.
	want := `<body><script src="/app.js" defer></script></body>`
	if got != want {
		t.Errorf("expected defer on the opening tag, got: %s\nwant: %s", got, want)
	}
}

// TestInjectScriptDefer_UsesBareBooleanAttribute pins the SPELLING, not just
// the presence, of the injected attribute.
//
// `defer="defer"` and a bare `defer` are both valid HTML5, so no amount of
// "is it deferred?" testing distinguishes them -- which is why the XHTML form
// survived here. It is not cosmetic: html-validate's default
// attribute-boolean-style is "omit", so every consumer running the fleet's
// markup gate got one error per deferred script in output THIS compiler
// generated. Measured on ffreis-editify at v1.0.1: 8 of its 35 html-validate
// errors were exactly this, in 9 generated pages.
//
// Asserting the absence of the value form is the half that matters; a presence
// check alone passes for both spellings.
func TestInjectScriptDefer_UsesBareBooleanAttribute(t *testing.T) {
	html := `<body><script src="/app.js"></script><script src="/vendor/lib.js"></script></body>`
	got, err := injectScriptDefer(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, `defer="defer"`) {
		t.Errorf("injected the XHTML value form, which html-validate's attribute-boolean-style rejects; got: %s", got)
	}
	if strings.Count(got, " defer>") != 2 {
		t.Errorf("expected a bare `defer` on both local scripts, got: %s", got)
	}
}

func TestInjectScriptDefer_LeavesExternalScriptUntouched(t *testing.T) {
	html := `<body><script src="https://cdn.example.com/lib.js"></script></body>`
	got, err := injectScriptDefer(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "defer") {
		t.Errorf("expected external script left untouched, got: %s", got)
	}
}

func TestInjectScriptDefer_LeavesModuleScriptUntouched(t *testing.T) {
	html := `<body><script type="module" src="/app.js"></script></body>`
	got, err := injectScriptDefer(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "defer") {
		t.Errorf("expected type=module script left untouched (already deferred by spec), got: %s", got)
	}
}

func TestInjectScriptDefer_RespectsExistingDefer(t *testing.T) {
	html := `<body><script defer src="/app.js"></script></body>`
	got, err := injectScriptDefer(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Count(got, "defer") != 1 {
		t.Errorf("expected exactly one defer (not duplicated), got: %s", got)
	}
}

func TestInjectScriptDefer_RespectsExistingAsync(t *testing.T) {
	html := `<body><script async src="/app.js"></script></body>`
	got, err := injectScriptDefer(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "defer") {
		t.Errorf("expected async script left as-is, not also given defer, got: %s", got)
	}
}

func TestInjectScriptDefer_DoesNotMatchDeferSubstringInSrcPath(t *testing.T) {
	// A src path that happens to contain "defer" (e.g. a directory name) must
	// not be mistaken for an already-deferred script.
	html := `<body><script src="/deferred-utils/app.js"></script></body>`
	got, err := injectScriptDefer(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `<script src="/deferred-utils/app.js" defer>`) {
		t.Errorf("expected defer added despite 'defer' substring in src path, got: %s", got)
	}
}

func TestInjectScriptDefer_MultipleScriptsAllDeferred(t *testing.T) {
	html := `<body><script src="/a.js"></script><script src="/b.js"></script><script src="/c.js"></script></body>`
	got, err := injectScriptDefer(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `<body><script src="/a.js" defer></script><script src="/b.js" defer></script><script src="/c.js" defer></script></body>`
	if got != want {
		t.Errorf("expected all three local scripts deferred on their opening tags in order, got: %s\nwant: %s", got, want)
	}
	if strings.Contains(got, "</script defer") {
		t.Errorf("expected no closing tag to carry an attribute, got: %s", got)
	}
}

// ── hoistExternalCSSImports ─────────────────────────────────────────────────

func TestHoistExternalCSSImports_HoistsImportToLinkAndPreconnect(t *testing.T) {
	html := `<head><style>@import url("https://fonts.example.com/css?family=X");body{color:red}</style></head>`
	got, err := hoistExternalCSSImports(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "@import") {
		t.Errorf("expected @import removed from style block, got: %s", got)
	}
	if !strings.Contains(got, `<link rel="stylesheet" href="https://fonts.example.com/css?family=X">`) {
		t.Errorf("expected hoisted stylesheet link, got: %s", got)
	}
	if !strings.Contains(got, `<link rel="preconnect" href="https://fonts.example.com">`) {
		t.Errorf("expected preconnect for the import's origin, got: %s", got)
	}
	if !strings.Contains(got, "color:red") {
		t.Errorf("expected the rest of the style block's CSS preserved, got: %s", got)
	}
}

func TestHoistExternalCSSImports_HoistedLinkPrecedesStyleBlock(t *testing.T) {
	html := `<head><style>@import url("https://fonts.example.com/css");body{}</style></head>`
	got, err := hoistExternalCSSImports(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	linkIdx := strings.Index(got, "<link")
	styleIdx := strings.Index(got, "<style")
	if linkIdx == -1 || styleIdx == -1 || linkIdx > styleIdx {
		t.Errorf("expected hoisted <link> to precede <style> (cascade order), got: %s", got)
	}
}

func TestHoistExternalCSSImports_GoogleFontsGetsBonusGstaticPreconnect(t *testing.T) {
	html := `<head><style>@import url("https://fonts.googleapis.com/css2?family=Roboto");</style></head>`
	got, err := hoistExternalCSSImports(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>`) {
		t.Errorf("expected bonus fonts.gstatic.com preconnect for a Google Fonts import, got: %s", got)
	}
}

func TestHoistExternalCSSImports_NonGoogleFontsImportNoBonusPreconnect(t *testing.T) {
	html := `<head><style>@import url("https://fonts.example.com/css");</style></head>`
	got, err := hoistExternalCSSImports(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "gstatic") {
		t.Errorf("expected no gstatic preconnect for a non-Google-Fonts import, got: %s", got)
	}
}

func TestHoistExternalCSSImports_SameImportAcrossTwoStyleBlocksHoistedOnce(t *testing.T) {
	html := `<head>` +
		`<style>@import url("https://fonts.example.com/css");body{}</style>` +
		`<style>@import url("https://fonts.example.com/css");.w{}</style>` +
		`</head>`
	got, err := hoistExternalCSSImports(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := strings.Count(got, `<link rel="stylesheet" href="https://fonts.example.com/css">`); n != 1 {
		t.Errorf("expected the duplicate import hoisted exactly once, got %d: %s", n, got)
	}
	if n := strings.Count(got, `<link rel="preconnect" href="https://fonts.example.com">`); n != 1 {
		t.Errorf("expected preconnect deduplicated exactly once, got %d: %s", n, got)
	}
}

func TestHoistExternalCSSImports_NoOpWhenNoExternalImport(t *testing.T) {
	html := `<head><style>body{color:red}</style></head>`
	got, err := hoistExternalCSSImports(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != html {
		t.Errorf("expected no change when there's no external @import, got: %s", got)
	}
}

func TestHoistExternalCSSImports_LocalImportLeftAlone(t *testing.T) {
	// A local @import should never appear here in practice (flattenCSSImports
	// already expands it before this runs), but this function must not
	// mistake one for external if it somehow does.
	html := `<head><style>@import url("/css/shared.css");body{}</style></head>`
	got, err := hoistExternalCSSImports(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `@import url("/css/shared.css")`) {
		t.Errorf("expected local @import left untouched, got: %s", got)
	}
	if strings.Contains(got, `<link rel="stylesheet"`) {
		t.Errorf("expected no hoisted link for a local import, got: %s", got)
	}
}

func TestHoistExternalCSSImports_PreservesMediaQuerySuffix(t *testing.T) {
	// "@import url(...) print;" must become a <link media="print">, not an
	// unconditional one -- dropping the suffix would silently make a
	// conditional import apply unconditionally.
	html := `<head><style>@import url("https://fonts.example.com/print.css") print;</style></head>`
	got, err := hoistExternalCSSImports(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `<link rel="stylesheet" href="https://fonts.example.com/print.css" media="print">`) {
		t.Errorf("expected hoisted link to carry the media=print condition, got: %s", got)
	}
}

func TestRun_HoistsExternalFontImportThroughFullPipeline(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           `@import url("https://fonts.googleapis.com/css2?family=Roboto");body{color:#000}`,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>hello</p>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	html := string(mustReadFile(t, filepath.Join(outDir, fileAgendaHTML)))
	if strings.Contains(html, "@import") {
		t.Fatalf("expected @import hoisted out of the rendered page, got: %s", html)
	}
	if !strings.Contains(html, `<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Roboto">`) {
		t.Fatalf("expected hoisted Google Fonts stylesheet link in rendered page, got: %s", html)
	}
	if !strings.Contains(html, `<link rel="preconnect" href="https://fonts.googleapis.com">`) {
		t.Fatalf("expected preconnect to fonts.googleapis.com in rendered page, got: %s", html)
	}
	if !strings.Contains(html, `<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>`) {
		t.Fatalf("expected bonus preconnect to fonts.gstatic.com in rendered page, got: %s", html)
	}
}

// ── blankIndentedLines ─────────────────────────────────────────────────────

func TestBlankIndentedLines_EmptiesWhitespaceOnlyLines(t *testing.T) {
	// The shape a Go template leaves behind when an action on its own indented
	// line renders nothing: the indentation and the newline survive.
	html := "<head>\n  <meta charset=\"utf-8\">\n  \n\t\n  <title>x</title>\n</head>"
	got := blankIndentedLines(html)
	want := "<head>\n  <meta charset=\"utf-8\">\n\n\n  <title>x</title>\n</head>"
	if got != want {
		t.Errorf("whitespace-only lines not emptied\ngot:  %q\nwant: %q", got, want)
	}
}

func TestBlankIndentedLines_PreservesLineCount(t *testing.T) {
	// Emptying, not deleting: every other line keeps its number, so a
	// validator's reported line numbers still point at the right source line.
	html := "a\n  \nb\n   \nc"
	got := blankIndentedLines(html)
	if gotN, wantN := strings.Count(got, "\n"), strings.Count(html, "\n"); gotN != wantN {
		t.Errorf("line count changed: got %d newlines, want %d (%q)", gotN, wantN, got)
	}
}

func TestBlankIndentedLines_LeavesTrailingWhitespaceAfterContent(t *testing.T) {
	// Deliberately narrow: trailing whitespace AFTER real content is left
	// alone, because inside <pre>/<textarea> removing it is a visible change.
	// A whitespace-only line renders identically empty either way, which is
	// why only that case is safe to normalise.
	html := "<pre>\nkeep me   \n  \n</pre>"
	got := blankIndentedLines(html)
	if !strings.Contains(got, "keep me   \n") {
		t.Errorf("trailing whitespace after content was stripped: %q", got)
	}
	if !strings.Contains(got, "\n\n</pre>") {
		t.Errorf("whitespace-only line inside <pre> was not emptied: %q", got)
	}
}

func TestBlankIndentedLines_NoOpWhenAlreadyClean(t *testing.T) {
	html := "<head>\n  <title>x</title>\n\n</head>"
	if got := blankIndentedLines(html); got != html {
		t.Errorf("modified already-clean HTML\ngot:  %q\nwant: %q", got, html)
	}
}

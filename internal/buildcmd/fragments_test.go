package buildcmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"ffreis-website-compiler/internal/sitegen"
	"ffreis-website-compiler/internal/testutil"
)

const (
	fragmentTemplateOpen = `<template id="tpl-`
	fragmentOrdersCSS    = "orders.css"
	fragmentSharedCSS    = "shared.css"
)

// ── backward compatibility ────────────────────────────────────────────────────

// helloWorldIndexSHA256 is the SHA-256 of examples/hello-world's rendered
// dist/index.html as produced by the compiler at the commit the fragment output
// mode was added on — captured by building the example with that pre-change
// compiler and with this one and diffing the two output trees, which were
// byte-for-byte identical.
//
// A digest rather than a committed golden file: the compiler emits meaningful
// trailing whitespace (the indented injection point before </head>), and the
// fleet's pre-commit trailing-whitespace hook silently rewrites any text file
// carrying it, which would corrupt a golden on its way into the repo.
const helloWorldIndexSHA256 = "e54ef9a9c0dde25c54ed7a24823790e1328c0e00f9d7c37ba1e3ebdc3c38cc61"

// TestRun_NoFragmentPages_MatchesPreFragmentOutputByteForByte is the
// backward-compatibility lock required by the fragment output mode: a site that
// marks no page as a fragment must build exactly what it built before the mode
// existed, so a difference here means fragment support changed output for every
// existing site.
func TestRun_NoFragmentPages_MatchesPreFragmentOutputByteForByte(t *testing.T) {
	websiteRoot, err := filepath.Abs(filepath.Join("..", "..", "examples", "hello-world"))
	if err != nil {
		t.Fatalf("resolving website root: %v", err)
	}
	outDir := t.TempDir()

	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	// The whole output tree, not just the page: a new generated file would be a
	// backward-compatibility break even if every existing file were unchanged.
	if got, want := outputTree(t, outDir), []string{fileIndexHTML}; !equalStrings(got, want) {
		t.Fatalf("output tree changed: got %v, want %v", got, want)
	}

	built := mustReadFile(t, filepath.Join(outDir, fileIndexHTML))
	if got := sha256.Sum256(built); hex.EncodeToString(got[:]) != helloWorldIndexSHA256 {
		t.Fatalf("index.html is no longer byte-for-byte identical to the pre-fragment build:\n"+
			"got sha256 %s, want %s\n--- built ---\n%s",
			hex.EncodeToString(got[:]), helloWorldIndexSHA256, built)
	}
}

// TestRun_NoFragmentPages_EmitsNoFragmentArtefacts guards the other half of the
// opt-in: none of the fragment mode's tags or files may appear in a build that
// declared no fragment page.
func TestRun_NoFragmentPages_EmitsNoFragmentArtefacts(t *testing.T) {
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

	for _, name := range []string{fragmentsHTMLFile, fragmentsJSFile} {
		if _, err := os.Stat(filepath.Join(outDir, name)); !os.IsNotExist(err) {
			t.Fatalf("expected no %s without fragment pages, got err=%v", name, err)
		}
	}
	html := string(mustReadFile(t, filepath.Join(outDir, fileAgendaHTML)))
	if strings.Contains(html, fragmentTemplateOpen) {
		t.Fatalf("expected no fragment <template> in a regular page, got: %s", html)
	}
}

// TestSplitFragmentPages_LeavesPagesUntouchedWithoutFragments locks the cheap
// guarantee the backward-compatibility promise rests on: with no page marked,
// the caller gets its own slice back and no fragment work is scheduled.
func TestSplitFragmentPages_LeavesPagesUntouchedWithoutFragments(t *testing.T) {
	pages := []sitegen.PageTemplate{{Name: "index"}, {Name: "about"}}

	cases := []struct {
		name     string
		siteData map[string]any
	}{
		{name: "no pages key", siteData: map[string]any{}},
		{name: "pages without flag", siteData: map[string]any{"pages": map[string]any{"about": map[string]any{"title": "About"}}}},
		{name: "flag explicitly false", siteData: map[string]any{"pages": map[string]any{"about": map[string]any{"fragment": false}}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			regular, fragments := splitFragmentPages(pages, tc.siteData)
			if fragments != nil {
				t.Fatalf("expected no fragment pages, got %v", fragments)
			}
			if len(regular) != len(pages) || regular[0].Name != "index" || regular[1].Name != "about" {
				t.Fatalf("expected the original pages back, got %v", regular)
			}
		})
	}
}

// ── fragment rendering ────────────────────────────────────────────────────────

// TestRun_FragmentPageRendersAsBareTemplate covers the core of the mode: the
// fragment's content goes through the normal data pipeline but comes out as a
// bare <template>, with no document shell and no header/footer partials, and
// the page itself is no longer written as a standalone document.
func TestRun_FragmentPageRendersAsBareTemplate(t *testing.T) {
	websiteRoot := newFragmentWebsiteRoot(t, map[string]string{
		"orders": `{{define "page"}}<section data-route="{{.PageName}}">` +
			`<h2>{{required (dig .SiteData "pages" "orders" "title") "missing title"}}</h2></section>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-sitemap-base-url", "https://example.com",
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	bundle := string(mustReadFile(t, filepath.Join(outDir, fragmentsHTMLFile)))
	if !strings.Contains(bundle, `<template id="tpl-orders">`) {
		t.Fatalf("expected <template id=\"tpl-orders\"> in the bundle, got: %s", bundle)
	}
	// Rendered through the same data pipeline as a full page: .PageName and
	// .SiteData both resolved.
	for _, want := range []string{`data-route="orders"`, "<h2>Orders</h2>"} {
		if !strings.Contains(bundle, want) {
			t.Fatalf("expected %q in the bundle, got: %s", want, bundle)
		}
	}
	// No layout: no document shell, no header/footer partials.
	for _, forbidden := range []string{"<html", "<head", "<body", "<!doctype"} {
		if strings.Contains(strings.ToLower(bundle), forbidden) {
			t.Fatalf("expected no %q in a fragment bundle, got: %s", forbidden, bundle)
		}
	}
	// The fragment page is not also emitted as a standalone document, nor listed
	// in the sitemap.
	if _, err := os.Stat(filepath.Join(outDir, "orders.html")); !os.IsNotExist(err) {
		t.Fatalf("expected no standalone page for a fragment, got err=%v", err)
	}
	sitemapXMLText := string(mustReadFile(t, filepath.Join(outDir, sitemapXML)))
	if strings.Contains(sitemapXMLText, "/orders") {
		t.Fatalf("expected a fragment page to be absent from sitemap.xml, got: %s", sitemapXMLText)
	}
}

// TestRun_FragmentPageDeclaringDocumentShellFails: a fragment is mounted inside
// the consuming app's document, so a content block that renders its own shell is
// a template bug worth failing the build over rather than emitting.
func TestRun_FragmentPageDeclaringDocumentShellFails(t *testing.T) {
	websiteRoot := newFragmentWebsiteRoot(t, map[string]string{
		"orders": `{{define "page"}}<body><p>orders</p></body>{{end}}`,
	})

	err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, t.TempDir()}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected the build to fail for a fragment carrying a document shell")
	}
	if !strings.Contains(err.Error(), "must not carry a document shell") {
		t.Fatalf("expected a document-shell error, got %v", err)
	}
}

// TestRun_FragmentFlagNeedsNoContractEntry: pages.<name>.fragment is compiler
// build metadata, exactly like pages.<name>.internal — a site may set it with
// or without declaring it in its contract, and neither way is a dangling path.
func TestRun_FragmentFlagNeedsNoContractEntry(t *testing.T) {
	cases := []struct {
		name     string
		contract string
	}{
		{name: "fragment flag without contract path", contract: "allowed:\n  - pages.*.title\n"},
		{name: "fragment flag declared in contract", contract: "allowed:\n  - pages.*.title\n  - pages.*.fragment\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			websiteRoot := newFragmentWebsiteRoot(t, map[string]string{
				"orders": `{{define "page"}}<p>{{required (dig .SiteData "pages" "orders" "title") "missing title"}}</p>{{end}}`,
			})
			testutil.WriteFiles(t, map[string]string{
				filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML): tc.contract,
			})

			outDir := t.TempDir()
			if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
				t.Fatalf(buildRunFailed, err)
			}
			mustStat(t, filepath.Join(outDir, fragmentsHTMLFile))
		})
	}
}

// TestRun_PageSlugCollidingWithFragmentBundleFails: the fragment bundle claims
// fragments.html in the output root, so a regular page resolving to the same
// file must be reported, never silently overwritten.
func TestRun_PageSlugCollidingWithFragmentBundleFails(t *testing.T) {
	websiteRoot := newFragmentWebsiteRoot(t, map[string]string{
		"orders": `{{define "page"}}<p>orders</p>{{end}}`,
	})
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):             "",
		filepath.Join(websiteRoot, "src", "templates", "pages", "fragments.gohtml"): `{{define "page"}}<p>a real page</p>{{end}}`,
	})

	err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, t.TempDir()}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected the build to fail when a page collides with fragments.html")
	}
	if !strings.Contains(err.Error(), "fragment output also claims") {
		t.Fatalf("expected a collision error, got %v", err)
	}
}

// TestRun_FragmentBodyGetsTemplateSafeTransformsOnly: a fragment gets the page
// transforms that behave correctly inside a <template> — small-SVG inlining,
// default lazy-loading, asset fingerprinting — and not the ones that need a
// document (LQIP's injected <style>/<script> pair, script preloads).
func TestRun_FragmentBodyGetsTemplateSafeTransformsOnly(t *testing.T) {
	websiteRoot := newFragmentWebsiteRoot(t, map[string]string{
		"orders": `{{define "page"}}<img src="/images/icon.svg" alt="icon" class="ico">` +
			`<img src="/images/hero.png" loading="eager" alt="hero">` +
			`<img src="/images/thumb.png" alt="thumb">` +
			`<script src="/js/widget.js"></script>{{end}}`,
	})
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "assets", "images"))
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "assets", "js"))
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "images", "icon.svg"):  `<svg viewBox="0 0 8 8"><path d="M0 0h8v8H0z"/></svg>`,
		filepath.Join(websiteRoot, "src", "assets", "images", "hero.png"):  "pngbytes-hero",
		filepath.Join(websiteRoot, "src", "assets", "images", "thumb.png"): "pngbytes-thumb",
		filepath.Join(websiteRoot, "src", "assets", "js", "widget.js"):     "window.widget=1;\n",
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	bundle := string(mustReadFile(t, filepath.Join(outDir, fragmentsHTMLFile)))

	// Small SVG inlined as <svg>, with the img's class merged and alt mapped to
	// aria-label — the same treatment a regular page's icon gets.
	if !strings.Contains(bundle, "<svg") || strings.Contains(bundle, `src="/images/icon`) {
		t.Fatalf("expected the small SVG inlined, got: %s", bundle)
	}
	for _, want := range []string{`class="ico"`, `aria-label="icon"`} {
		if !strings.Contains(bundle, want) {
			t.Fatalf("expected %q on the inlined <svg>, got: %s", want, bundle)
		}
	}
	// Default lazy-loading applied, and an explicit loading="eager" respected.
	if !strings.Contains(bundle, `alt="thumb" loading="lazy"`) {
		t.Fatalf("expected loading=lazy on the undecorated image, got: %s", bundle)
	}
	if !strings.Contains(bundle, `loading="eager"`) {
		t.Fatalf("expected the explicit loading=eager to survive, got: %s", bundle)
	}
	// Raster images fingerprinted, and the hashed copies actually in dist.
	hashed := regexp.MustCompile(`src="(/images/(?:hero|thumb)\.[a-f0-9]{8}\.png)"`).FindAllStringSubmatch(bundle, -1)
	if len(hashed) != 2 {
		t.Fatalf("expected both raster images fingerprinted, got: %s", bundle)
	}
	for _, m := range hashed {
		mustStat(t, filepath.Join(outDir, filepath.FromSlash(strings.TrimPrefix(m[1], "/"))))
	}
	// An eager image is NOT turned into an LQIP data URI: LQIP injects a
	// per-page <style>/<script> pair that cannot work inside a <template>.
	for _, forbidden := range []string{"data:image/jpeg;base64,", "lqip-pending", "<style", `rel="preload"`} {
		if strings.Contains(bundle, forbidden) {
			t.Fatalf("expected no %q in a fragment, got: %s", forbidden, bundle)
		}
	}
	// The script tag is left exactly as authored — not inlined, not preloaded.
	if !strings.Contains(bundle, `<script src="/js/widget.`) {
		t.Fatalf("expected the script tag left external, got: %s", bundle)
	}
}

// ── per-fragment CSS ──────────────────────────────────────────────────────────

// TestRun_FragmentOwnCSSLandsInItsOwnFingerprintedFile: a fragment's stylesheet
// becomes a separate content-hashed file rather than a <style> block, because
// inlining would copy the whole stylesheet into every clone of the template.
func TestRun_FragmentOwnCSSLandsInItsOwnFingerprintedFile(t *testing.T) {
	websiteRoot := newFragmentWebsiteRoot(t, map[string]string{
		"orders": `{{define "page"}}<link rel="stylesheet" href="/css/orders.css"><p>orders</p>{{end}}`,
	})
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fragmentOrdersCSS): "/* orders route */\n.orders   { color : red; }\n",
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	cssPath := soleMatch(t, outDir, filepath.Join("css", "fragment-orders.*.css"))
	// Fingerprinted with the same 8-hex-char content hash every other asset gets.
	if !regexp.MustCompile(`^fragment-orders\.[a-f0-9]{8}\.css$`).MatchString(filepath.Base(cssPath)) {
		t.Fatalf("expected a content-hashed fragment CSS filename, got %s", filepath.Base(cssPath))
	}
	// Minified by the same helper head-CSS inlining uses: comment stripped,
	// whitespace around structural characters collapsed.
	css := string(mustReadFile(t, cssPath))
	if css != ".orders{color:red}" {
		t.Fatalf("expected minified fragment CSS, got %q", css)
	}
	// Neither the stylesheet link nor its content may survive in the template.
	bundle := string(mustReadFile(t, filepath.Join(outDir, fragmentsHTMLFile)))
	for _, forbidden := range []string{"<style", "rel=\"stylesheet\"", "color:red"} {
		if strings.Contains(bundle, forbidden) {
			t.Fatalf("expected %q to be extracted out of the fragment, got: %s", forbidden, bundle)
		}
	}
	// The mount script points at the generated file.
	script := string(mustReadFile(t, filepath.Join(outDir, fragmentsJSFile)))
	wantHref := "/css/" + filepath.Base(cssPath)
	if !strings.Contains(script, `"orders":"`+wantHref+`"`) {
		t.Fatalf("expected the mount script to map orders to %s, got: %s", wantHref, script)
	}
}

// TestRun_FragmentCSSFlattensImportsAndFingerprintsURLs: the generated file goes
// through the same @import flattening and url() content-hash rewrite an inlined
// critical-path <style> block does — the originals under css/ and images/ are
// never copied to dist, so an un-rewritten url() would 404.
func TestRun_FragmentCSSFlattensImportsAndFingerprintsURLs(t *testing.T) {
	websiteRoot := newFragmentWebsiteRoot(t, map[string]string{
		"orders": `{{define "page"}}<link rel="stylesheet" href="/css/orders.css"><p>orders</p>{{end}}`,
	})
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "assets", "images"))
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fragmentOrdersCSS): "@import \"partial.css\";\n.orders { color: red; }\n",
		filepath.Join(websiteRoot, "src", "assets", "css", "partial.css"):     ".card { background: url(\"/images/dot.png\"); }\n",
		filepath.Join(websiteRoot, "src", "assets", "images", "dot.png"):      "pngbytes",
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	css := string(mustReadFile(t, soleMatch(t, outDir, filepath.Join("css", "fragment-orders.*.css"))))
	if strings.Contains(css, "@import") {
		t.Fatalf("expected @import to be flattened, got %q", css)
	}
	if !strings.Contains(css, ".card{") || !strings.Contains(css, ".orders{") {
		t.Fatalf("expected both the imported and the importing rules, got %q", css)
	}
	hashedURL := regexp.MustCompile(`url\("(/images/dot\.[a-f0-9]{8}\.png)"\)`).FindStringSubmatch(css)
	if hashedURL == nil {
		t.Fatalf("expected the url() ref to be content-hashed, got %q", css)
	}
	// The referenced file must actually be in dist under that hashed name.
	mustStat(t, filepath.Join(outDir, filepath.FromSlash(strings.TrimPrefix(hashedURL[1], "/"))))
}

// TestRun_StylesheetSharedByTwoFragmentsIsHoisted mirrors the shared-script
// precedent: a stylesheet used by more than one fragment is loaded once from a
// single hoisted file instead of being copied into each fragment's own CSS.
func TestRun_StylesheetSharedByTwoFragmentsIsHoisted(t *testing.T) {
	websiteRoot := newFragmentWebsiteRoot(t, map[string]string{
		"orders": `{{define "page"}}<link rel="stylesheet" href="/css/shared.css">` +
			`<link rel="stylesheet" href="/css/orders.css"><p>orders</p>{{end}}`,
		"profile": `{{define "page"}}<link rel="stylesheet" href="/css/shared.css"><p>profile</p>{{end}}`,
	})
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fragmentSharedCSS): ".card { padding: 1rem; }\n",
		filepath.Join(websiteRoot, "src", "assets", "css", fragmentOrdersCSS): ".orders { color: red; }\n",
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	shared := string(mustReadFile(t, soleMatch(t, outDir, filepath.Join("css", "fragment-shared.*.css"))))
	if shared != ".card{padding:1rem}" {
		t.Fatalf("expected the shared stylesheet hoisted verbatim, got %q", shared)
	}

	// orders keeps only the stylesheet no other fragment references; the shared
	// one is not duplicated into it.
	own := string(mustReadFile(t, soleMatch(t, outDir, filepath.Join("css", "fragment-orders.*.css"))))
	if own != ".orders{color:red}" {
		t.Fatalf("expected only orders' own CSS in its file, got %q", own)
	}

	// profile referenced nothing but the shared stylesheet, so it has no own
	// file at all and the mount script reports null for it.
	if matches := globMatches(t, outDir, filepath.Join("css", "fragment-profile.*.css")); len(matches) != 0 {
		t.Fatalf("expected no own CSS file for a fragment whose only stylesheet was hoisted, got %v", matches)
	}
	script := string(mustReadFile(t, filepath.Join(outDir, fragmentsJSFile)))
	if !strings.Contains(script, `"profile":null`) {
		t.Fatalf("expected profile mapped to null in the mount script, got: %s", script)
	}
	if !strings.Contains(script, "var SHARED_CSS = \"/css/fragment-shared.") {
		t.Fatalf("expected the hoisted stylesheet in the mount script, got: %s", script)
	}
}

// ── mount script ──────────────────────────────────────────────────────────────

// TestRun_MountScriptExistsOnlyWithFragments pins the generated script to the
// opt-in, and TestRenderFragmentMountScript_* below check its contents.
func TestRun_MountScriptExistsOnlyWithFragments(t *testing.T) {
	websiteRoot := newFragmentWebsiteRoot(t, map[string]string{
		"orders": `{{define "page"}}<p>orders</p>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	script := string(mustReadFile(t, filepath.Join(outDir, fragmentsJSFile)))
	assertValidJS(t, fragmentsJSFile, script)
	// A fragment with no stylesheet of its own reports null, so mount() skips
	// the <link> swap rather than clearing the current stylesheet.
	if !strings.Contains(script, `var OWN_CSS = {"orders":null};`) {
		t.Fatalf("expected a null own-CSS entry, got: %s", script)
	}
	if !strings.Contains(script, "var SHARED_CSS = null;") {
		t.Fatalf("expected a null shared-CSS entry, got: %s", script)
	}
	// The documented API surface.
	for _, want := range []string{
		"global.websiteFragments",
		"mount: mount",
		"function mount(name, target)",
		`document.getElementById(TEMPLATE_ID_PREFIX + name)`,
		"template.content.cloneNode(true)",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected %q in the mount script, got: %s", want, script)
		}
	}
}

// TestRenderFragmentMountScript_IsSyntacticallyValidJS checks the generated
// snippet parses, for both the empty and the populated CSS tables.
func TestRenderFragmentMountScript_IsSyntacticallyValidJS(t *testing.T) {
	cases := []struct {
		name string
		css  fragmentCSSBundle
	}{
		{
			name: "no css at all",
			css:  fragmentCSSBundle{own: map[string]any{"orders": nil}},
		},
		{
			name: "own and shared css",
			css: fragmentCSSBundle{
				own:    map[string]any{"orders": "/css/fragment-orders.aabbccdd.css", "profile": nil},
				shared: "/css/fragment-shared.11223344.css",
			},
		},
		{
			// A name needing JSON escaping must not be able to break out of the
			// generated string literal.
			name: "name needing escaping",
			css:  fragmentCSSBundle{own: map[string]any{`a"b\c`: "/css/x.aabbccdd.css"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			script, err := renderFragmentMountScript(tc.css)
			if err != nil {
				t.Fatalf("rendering mount script: %v", err)
			}
			assertValidJS(t, tc.name, script)
			assertMountScriptTablesParse(t, script, tc.css)
		})
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// newFragmentWebsiteRoot builds a website root whose pages map marks every
// entry in fragmentTemplates as a fragment. Each value is the page template's
// full source. A regular "index" page is always included so the site also
// exercises the normal path alongside the fragment one.
func newFragmentWebsiteRoot(t *testing.T, fragmentTemplates map[string]string) string {
	t.Helper()

	websiteRoot := newTestWebsiteRoot(t)
	names := make([]string, 0, len(fragmentTemplates))
	for name := range fragmentTemplates {
		names = append(names, name)
	}
	sort.Strings(names)

	var siteYAML strings.Builder
	siteYAML.WriteString("pages:\n")
	// An empty contract by default: strict-contract requires every *declared*
	// path to be used by some template, and most cases here do not read a title.
	// A case that cares declares its own contract on top.
	files := map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):         mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):         "",
		filepath.Join(websiteRoot, "src", "templates", "pages", "index.gohtml"): `{{define "page"}}<main>shell</main>{{end}}`,
	}
	for _, name := range names {
		siteYAML.WriteString("  " + name + ":\n")
		siteYAML.WriteString("    title: " + strings.ToUpper(name[:1]) + name[1:] + "\n")
		siteYAML.WriteString("    fragment: true\n")
		files[filepath.Join(websiteRoot, "src", "templates", "pages", name+".gohtml")] = fragmentTemplates[name]
	}
	// index carries a title too, so a case that declares pages.*.title in its
	// own contract has no pages entry missing one.
	siteYAML.WriteString("  index:\n    title: Shell\n")
	files[filepath.Join(websiteRoot, "src", "data", fileSiteYAML)] = siteYAML.String()

	testutil.WriteFiles(t, files)
	return websiteRoot
}

// outputTree returns every file in dir as a sorted list of slash-separated
// paths relative to dir.
func outputTree(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	sort.Strings(paths)
	return paths
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// globMatches returns the files in dir matching a slash-separated glob pattern.
func globMatches(t *testing.T, dir, pattern string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		t.Fatalf("globbing %s in %s: %v", pattern, dir, err)
	}
	sort.Strings(matches)
	return matches
}

// soleMatch returns the single file in dir matching pattern, failing when there
// is not exactly one — a content-hashed filename is unknown up front, but a
// second match would mean the build wrote a file it should not have.
func soleMatch(t *testing.T, dir, pattern string) string {
	t.Helper()
	matches := globMatches(t, dir, pattern)
	if len(matches) != 1 {
		t.Fatalf("expected exactly one file matching %s in %s, got %v", pattern, dir, matches)
	}
	return matches[0]
}

// assertValidJS checks that script parses as JavaScript. Delimiter balance
// outside string literals and comments is checked always and everywhere; when a
// node binary is on PATH, `node --check` additionally parses the file for real.
func assertValidJS(t *testing.T, label, script string) {
	t.Helper()
	if err := checkJSDelimiterBalance(script); err != nil {
		t.Fatalf("%s is not valid JS: %v\n%s", label, err, script)
	}

	node, err := exec.LookPath("node")
	if err != nil {
		t.Logf("node not on PATH: %s checked for delimiter balance only", label)
		return
	}
	path := filepath.Join(t.TempDir(), "script.js")
	if writeErr := os.WriteFile(path, []byte(script), 0o600); writeErr != nil {
		t.Fatalf("writing %s for node --check: %v", path, writeErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, checkErr := exec.CommandContext(ctx, node, "--check", path).CombinedOutput(); checkErr != nil {
		t.Fatalf("node --check rejected %s: %v\n%s", label, checkErr, out)
	}
}

// jsDelimiterPairs maps each opening delimiter to the closer it requires.
var jsDelimiterPairs = map[byte]byte{'(': ')', '[': ']', '{': '}'}

// checkJSDelimiterBalance scans src as JavaScript source and reports unbalanced
// (), [] or {} and unterminated string literals and block comments. String and
// comment bodies are skipped so a brace inside a URL or a prose comment is not
// mistaken for code. The generated script uses no template literals or regex
// literals, so neither needs handling here.
func checkJSDelimiterBalance(src string) error {
	var stack []byte
	for i := 0; i < len(src); i++ {
		switch c := src[i]; {
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			i = skipTo(src, i+2, "\n")
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return errUnterminated("block comment")
			}
			i += 2 + end + 1
		case c == '"' || c == '\'':
			end, ok := endOfJSString(src, i)
			if !ok {
				return errUnterminated("string literal")
			}
			i = end
		case jsDelimiterPairs[c] != 0:
			stack = append(stack, jsDelimiterPairs[c])
		case c == ')' || c == ']' || c == '}':
			if len(stack) == 0 || stack[len(stack)-1] != c {
				return errUnbalanced(c, i)
			}
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) > 0 {
		return errUnterminated("delimiter " + string(stack[len(stack)-1]))
	}
	return nil
}

// endOfJSString returns the index of the quote closing the literal that opens at
// start, honouring backslash escapes.
func endOfJSString(src string, start int) (int, bool) {
	quote := src[start]
	for i := start + 1; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case quote:
			return i, true
		case '\n':
			return 0, false
		}
	}
	return 0, false
}

// skipTo returns the index of the character before the next occurrence of sep at
// or after from, or the last index when sep is absent.
func skipTo(src string, from int, sep string) int {
	if idx := strings.Index(src[from:], sep); idx >= 0 {
		return from + idx
	}
	return len(src) - 1
}

func errUnterminated(what string) error {
	return &jsSyntaxError{msg: "unterminated " + what}
}

func errUnbalanced(c byte, at int) error {
	return &jsSyntaxError{msg: "unbalanced " + string(c) + " at offset " + itoa(at)}
}

type jsSyntaxError struct{ msg string }

func (e *jsSyntaxError) Error() string { return e.msg }

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// mountScriptOwnCSSRE and mountScriptSharedCSSRE extract the two generated data
// tables so a test can parse them back as JSON.
var (
	mountScriptOwnCSSRE    = regexp.MustCompile(`(?m)^  var OWN_CSS = (.*);$`)
	mountScriptSharedCSSRE = regexp.MustCompile(`(?m)^  var SHARED_CSS = (.*);$`)
)

// assertMountScriptTablesParse confirms the generated OWN_CSS and SHARED_CSS
// literals are valid JSON that round-trips to the bundle they were built from.
func assertMountScriptTablesParse(t *testing.T, script string, want fragmentCSSBundle) {
	t.Helper()

	ownMatch := mountScriptOwnCSSRE.FindStringSubmatch(script)
	if ownMatch == nil {
		t.Fatalf("no OWN_CSS table in the mount script:\n%s", script)
	}
	var gotOwn map[string]any
	if err := json.Unmarshal([]byte(ownMatch[1]), &gotOwn); err != nil {
		t.Fatalf("OWN_CSS is not valid JSON (%v): %s", err, ownMatch[1])
	}
	if len(gotOwn) != len(want.own) {
		t.Fatalf("OWN_CSS has %d entries, want %d: %s", len(gotOwn), len(want.own), ownMatch[1])
	}
	for name, href := range want.own {
		if gotOwn[name] != href {
			t.Fatalf("OWN_CSS[%q] = %v, want %v", name, gotOwn[name], href)
		}
	}

	sharedMatch := mountScriptSharedCSSRE.FindStringSubmatch(script)
	if sharedMatch == nil {
		t.Fatalf("no SHARED_CSS value in the mount script:\n%s", script)
	}
	var gotShared any
	if err := json.Unmarshal([]byte(sharedMatch[1]), &gotShared); err != nil {
		t.Fatalf("SHARED_CSS is not valid JSON (%v): %s", err, sharedMatch[1])
	}
	if gotShared != want.shared {
		t.Fatalf("SHARED_CSS = %v, want %v", gotShared, want.shared)
	}
}

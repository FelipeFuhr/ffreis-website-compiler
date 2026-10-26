package conventions

import (
	"strings"
	"testing"
)

const assetRule = "resolvable-asset-refs"

// The defect this rule exists for, reproduced exactly. knowalchemy-website
// wrote a bare url() in src/assets/css/simulator.css; the compiler resolved it
// against the stylesheet's own directory, emitted "/css/<name>.jpg" into the
// inlined <style>, found nothing there, and shipped the broken path.
func TestResolvableAssetRefs_CatchesTheRewrittenBareURL(t *testing.T) {
	got := builtSite(t, assetRule, map[string]string{
		"dist/index.html": `<html><head><style>` +
			`body{background:url("/css/alchemy-nebula-backdrop.jpg")}` +
			`</style></head><body>x</body></html>`,
		"dist/alchemy-nebula-backdrop.1b855388.jpg": "\xff\xd8\xff",
	})
	f := wantOneFinding(t, got, "/css/alchemy-nebula-backdrop.jpg")
	if !strings.Contains(f.Detail, "root-absolute") {
		t.Errorf("the finding should say how to fix it, got %q", f.Detail)
	}
}

// A url() in a linked stylesheet is relative to that stylesheet, not to the
// page. Getting this backwards is the whole bug, so both directions are
// pinned: the same bare name passes from css/ and fails from the page.
func TestResolvableAssetRefs_URLIsRelativeToItsStylesheet(t *testing.T) {
	wantNoFindings(t, builtSite(t, assetRule, map[string]string{
		"dist/index.html":  `<html><head><link rel="stylesheet" href="/css/app.css"></head><body>x</body></html>`,
		"dist/css/app.css": `body{background:url("bg.jpg")}`,
		"dist/css/bg.jpg":  "\xff\xd8\xff",
	}))

	got := builtSite(t, assetRule, map[string]string{
		"dist/index.html":  `<html><head><link rel="stylesheet" href="/css/app.css"></head><body>x</body></html>`,
		"dist/css/app.css": `body{background:url("bg.jpg")}`,
		"dist/bg.jpg":      "\xff\xd8\xff", // at the root, not beside the sheet
	})
	wantOneFinding(t, got, "bg.jpg")
}

func TestResolvableAssetRefs_AcceptsAFullyResolvedPage(t *testing.T) {
	wantNoFindings(t, builtSite(t, assetRule, map[string]string{
		"dist/index.html": `<html><head>` +
			`<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=DM+Sans">` +
			`<link rel="icon" href="/favicon.50986245.svg">` +
			`<link rel="manifest" href="/site.f113f8fd.webmanifest">` +
			`<link rel="canonical" href="https://example.com/">` +
			`</head><body>` +
			`<img src="/img/hero.abc123.png" srcset="/img/hero.abc123.png 1x, /img/hero@2x.def456.png 2x">` +
			`<script src="/js/app.99887766.js"></script>` +
			`</body></html>`,
		"dist/favicon.50986245.svg":      "<svg/>",
		"dist/site.f113f8fd.webmanifest": "{}",
		"dist/img/hero.abc123.png":       "\x89PNG",
		"dist/img/hero@2x.def456.png":    "\x89PNG",
		"dist/js/app.99887766.js":        "//",
	}))
}

// Each srcset candidate is a real fetch, so each one is checked.
func TestResolvableAssetRefs_ChecksEverySrcsetCandidate(t *testing.T) {
	got := builtSite(t, assetRule, map[string]string{
		"dist/index.html": `<html><body>` +
			`<img src="/img/a.png" srcset="/img/a.png 1x, /img/missing@2x.png 2x">` +
			`</body></html>`,
		"dist/img/a.png": "\x89PNG",
	})
	wantOneFinding(t, got, "/img/missing@2x.png")
}

// A navigation target is not a subresource. Clean URLs are directories, and
// reporting them would make this rule unusable on every site in the fleet.
func TestResolvableAssetRefs_IgnoresNavigationAndNonFetchableRefs(t *testing.T) {
	wantNoFindings(t, builtSite(t, assetRule, map[string]string{
		"dist/index.html": `<html><body>` +
			`<a href="/about/">About</a>` +
			`<a href="/nowhere">Gone</a>` +
			`<a href="#main">Skip</a>` +
			`<a href="mailto:x@example.com">Mail</a>` +
			`<a href="tel:+100">Call</a>` +
			`<form action="/submit"></form>` +
			`<img src="data:image/gif;base64,R0lGOD">` +
			`<img src="//cdn.example.com/x.png">` +
			`<img src="https://example.com/y.png">` +
			`</body></html>`,
	}))
}

// A site deployed under a base path emits "/en/..." while the tree it was
// built into holds "css/...". Without this tolerance the rule reports every
// asset on every multi-language site.
func TestResolvableAssetRefs_ToleratesADeployBasePath(t *testing.T) {
	wantNoFindings(t, builtSite(t, assetRule, map[string]string{
		"dist/index.html":  `<html><head><link rel="stylesheet" href="/en/css/app.css"></head><body>x</body></html>`,
		"dist/css/app.css": `body{margin:0}`,
	}))

	// The tolerance must not swallow a genuinely missing file.
	got := builtSite(t, assetRule, map[string]string{
		"dist/index.html":  `<html><head><link rel="stylesheet" href="/en/css/gone.css"></head><body>x</body></html>`,
		"dist/css/app.css": `body{margin:0}`,
	})
	wantOneFinding(t, got, "/en/css/gone.css")
}

func TestResolvableAssetRefs_StripsQueryAndFragment(t *testing.T) {
	wantNoFindings(t, builtSite(t, assetRule, map[string]string{
		"dist/index.html": `<html><head><link rel="stylesheet" href="/css/app.css?v=2"></head><body>` +
			`<img src="/img/sprite.svg#icon-star"></body></html>`,
		"dist/css/app.css":    `body{margin:0}`,
		"dist/img/sprite.svg": "<svg/>",
	}))
}

// A reference that lands on a directory fetches nothing, so it is as broken as
// one that lands on nothing.
func TestResolvableAssetRefs_ADirectoryIsNotAFile(t *testing.T) {
	got := builtSite(t, assetRule, map[string]string{
		"dist/index.html":     `<html><body><img src="/img/hero"></body></html>`,
		"dist/img/hero/a.png": "\x89PNG",
	})
	wantOneFinding(t, got, "/img/hero")
}

// The same broken reference repeated across a page is one defect to fix.
func TestResolvableAssetRefs_ReportsEachBrokenReferenceOnce(t *testing.T) {
	got := builtSite(t, assetRule, map[string]string{
		"dist/index.html": `<html><body>` +
			`<img src="/img/missing.png"><img src="/img/missing.png">` +
			`</body></html>`,
	})
	wantOneFinding(t, got, "/img/missing.png")
}

// A url() inside an inline <style> must report the line it sits on in the
// page, not its offset within the block.
func TestResolvableAssetRefs_InlineStyleFindingCarriesThePageLine(t *testing.T) {
	page := "<html>\n<head>\n<style>\nbody{color:red}\n.x{background:url(\"/gone.png\")}\n</style>\n</head>\n<body>x</body>\n</html>"
	got := builtSite(t, assetRule, map[string]string{"dist/index.html": page})
	f := wantOneFinding(t, got, "/gone.png")
	if f.Line != 5 {
		t.Errorf("expected the page line 5, got %d", f.Line)
	}
}

package buildcmd

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/listings"
	"ffreis-website-compiler/internal/testutil"
)

const (
	flagWebsiteRoot = "-website-root"
	flagOut         = "-out"

	fileMainCSS          = "main.css"
	fileSiteContractYAML = "site.contract.yaml"
	fileSiteYAML         = "site.yaml"
	fileAgendaGoHTML     = "agenda.gohtml"
	fileAgendaHTML       = "agenda.html"
	fileIndexHTML        = "index.html"

	mainCSSContent   = "body { color: #000; }\n"
	buildRunFailed   = "build run failed: %v"
	readingAgendaFmt = "reading agenda output: %v"
	httpContentType  = "Content-Type"

	stdLayoutTmpl = `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>{{template "page" .}}</body></html>{{end}}`
	stdHeadTmpl   = `{{define "head"}}<link rel="stylesheet" href="/css/main.css">{{end}}`
)

func TestRun_GeneratesHelloWorldOutput(t *testing.T) {
	websiteRoot, err := filepath.Abs(filepath.Join("..", "..", "examples", "hello-world"))
	if err != nil {
		t.Fatalf("resolving website root: %v", err)
	}
	outDir := t.TempDir()

	args := []string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-sitemap-base-url", "https://example.com",
	}
	if err := Run(args, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	indexPath := filepath.Join(outDir, fileIndexHTML)
	content, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("reading %s: %v", indexPath, err)
	}
	html := string(content)
	for _, expected := range []string{
		"<title>Hello World</title>",
		"<p>Hello, world.</p>",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("expected %q in rendered html", expected)
		}
	}

	// CSS from <head> is inlined as a <style> block; the original file is not copied.
	if !strings.Contains(html, "<style>") {
		t.Fatalf("expected inlined <style> block in rendered html")
	}
	if _, err := os.Stat(filepath.Join(outDir, "css", fileMainCSS)); !os.IsNotExist(err) {
		t.Fatalf("expected original css not to be copied to dist, got err=%v", err)
	}

	sitemapPath := filepath.Join(outDir, "sitemap.xml")
	sitemapRaw, err := os.ReadFile(sitemapPath)
	if err != nil {
		t.Fatalf("expected generated sitemap: %v", err)
	}
	if !strings.Contains(string(sitemapRaw), "<urlset") {
		t.Fatalf("expected sitemap.xml to contain urlset")
	}
}

func TestRun_GeneratesListingDetailPagesFromListingsFile(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS): mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteYAML): `pages:
  listing:
    internal: true
`,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", "listing.gohtml"): `{{define "page"}}<main data-listing-id="{{dig .CurrentListing "id"}}"><h1>{{dig .CurrentListing "title"}}</h1><p>{{dig .CurrentListing "price"}}</p></main>{{end}}`,
	})

	listingsPath := filepath.Join(t.TempDir(), "listings.yaml")
	testutil.WriteFiles(t, map[string]string{
		listingsPath: `
listings:
  - id: test-listing-1
    title: "Test Listing One"
    price: "$100"
    reasons: ["good location"]
    breakdown: { "Preço": 90 }
  - id: test-listing-2
    title: "Test Listing Two"
    price: "$200"
`,
	})

	outDir := t.TempDir()
	if err := Run([]string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-listings-file", listingsPath,
		"-sitemap-base-url", "https://example.com",
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	// The internal "listing" template itself must not produce a standalone
	// /listing.html page.
	if _, err := os.Stat(filepath.Join(outDir, "listing.html")); !os.IsNotExist(err) {
		t.Fatalf("expected internal listing template to be excluded from direct output, got err=%v", err)
	}

	page1 := mustReadFile(t, filepath.Join(outDir, "listings", "test-listing-1", "index.html"))
	if !strings.Contains(string(page1), `data-listing-id="test-listing-1"`) {
		t.Fatalf("expected listing id in rendered detail page, got %s", string(page1))
	}
	if !strings.Contains(string(page1), "<h1>Test Listing One</h1>") {
		t.Fatalf("expected listing title in rendered detail page, got %s", string(page1))
	}
	if !strings.Contains(string(page1), "$100") {
		t.Fatalf("expected listing price in rendered detail page, got %s", string(page1))
	}

	page2 := mustReadFile(t, filepath.Join(outDir, "listings", "test-listing-2", "index.html"))
	if !strings.Contains(string(page2), "Test Listing Two") {
		t.Fatalf("expected second listing detail page to render, got %s", string(page2))
	}

	sitemapRaw := mustReadFile(t, filepath.Join(outDir, "sitemap.xml"))
	for _, wantURL := range []string{"/listings/test-listing-1/", "/listings/test-listing-2/"} {
		if !strings.Contains(string(sitemapRaw), wantURL) {
			t.Fatalf("expected sitemap.xml to contain %s, got %s", wantURL, string(sitemapRaw))
		}
	}
}

// setupListingsSectionTestSite builds a website root with:
//   - a "listing" internal detail template (per PR #99's -listings-file flow),
//   - a "listings" hub page that renders the raw siteData["listings"] count
//     (mirroring casaboa's pre-PR#99 window.CASABOA_LISTINGS grid page),
//   - an "index" home page with its own carousel block reading the same
//     siteData["listings"] key, standing in for a home-page preview that is
//     NOT dropped when the listings section is disabled,
//   - raw site data that already declares a "listings" entry directly (the
//     stale, pre-existing data a real site's checked-in site-data YAML would
//     carry independent of -listings-file), and
//   - a sitemap.yaml declaring a static "/listings/" URL, so the section's
//     hub page has a sitemap entry beyond the per-item detail pages.
//
// Returns the website root and a temp -listings-file path with two items.
func setupListingsSectionTestSite(t *testing.T) (websiteRoot, listingsPath string) {
	t.Helper()
	websiteRoot = newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS): mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteYAML): `pages:
  listing:
    internal: true
listings:
  - id: stale-carousel-item
    title: Stale Carousel Item
`,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):            "",
		filepath.Join(websiteRoot, "src", "templates", "pages", "listing.gohtml"):  `{{define "page"}}<main data-listing-id="{{dig .CurrentListing "id"}}"><h1>{{dig .CurrentListing "title"}}</h1></main>{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "listings.gohtml"): `{{define "page"}}<main data-listings-count="{{len .SiteData.listings}}">Listings Hub</main>{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "index.gohtml"):    `{{define "page"}}<main><div class="carousel" data-count="{{len .SiteData.listings}}"></div></main>{{end}}`,
		filepath.Join(websiteRoot, "sitemap.yaml"): `base_url: https://example.com
urls:
  - path: /
  - path: /listings/
`,
	})

	listingsPath = filepath.Join(t.TempDir(), "listings.yaml")
	testutil.WriteFiles(t, map[string]string{
		listingsPath: `
listings:
  - id: test-listing-1
    title: "Test Listing One"
    price: "$100"
  - id: test-listing-2
    title: "Test Listing Two"
    price: "$200"
`,
	})
	return websiteRoot, listingsPath
}

// TestRun_ListingsSectionEnabled_KeepsHubPageSitemapEntryAndCarouselData is the
// contrast case for TestRun_DisablingListingsSection_Removes...: with the
// listings section left enabled (the default), the hub page, its sitemap
// entries, and the carousel data must all be present.
func TestRun_ListingsSectionEnabled_KeepsHubPageSitemapEntryAndCarouselData(t *testing.T) {
	websiteRoot, listingsPath := setupListingsSectionTestSite(t)
	outDir := t.TempDir()

	if err := Run([]string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-listings-file", listingsPath,
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	mustStat(t, filepath.Join(outDir, "listings.html"))

	index := mustReadFile(t, filepath.Join(outDir, fileIndexHTML))
	if !strings.Contains(string(index), `data-count="2"`) {
		t.Fatalf("expected home carousel to show the 2 loaded listings, got %s", string(index))
	}

	sitemapRaw := mustReadFile(t, filepath.Join(outDir, "sitemap.xml"))
	for _, wantURL := range []string{"/listings/", "/listings/test-listing-1/", "/listings/test-listing-2/"} {
		if !strings.Contains(string(sitemapRaw), wantURL) {
			t.Fatalf("expected sitemap.xml to contain %s, got %s", wantURL, string(sitemapRaw))
		}
	}
}

// TestRun_DisablingListingsSection_RemovesHubPageSitemapEntryAndCarouselData
// proves that -disable-sections=listings fully hides the listings section:
// (a) the listings hub page itself is dropped, (b) every /listings/ sitemap
// entry (static and per-item) is dropped, and (c) no stale carousel data
// (siteData["listings"] predating -listings-file) leaks into a page that
// survives the drop, such as the home page.
func TestRun_DisablingListingsSection_RemovesHubPageSitemapEntryAndCarouselData(t *testing.T) {
	websiteRoot, listingsPath := setupListingsSectionTestSite(t)
	outDir := t.TempDir()

	if err := Run([]string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-listings-file", listingsPath,
		"-disable-sections", "listings",
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	// (a) the listings hub page itself must not be generated.
	if _, err := os.Stat(filepath.Join(outDir, "listings.html")); !os.IsNotExist(err) {
		t.Errorf("expected listings hub page to be dropped when the section is disabled, got err=%v", err)
	}

	// (b) no /listings/ URL — static or per-item detail — should reach sitemap.xml.
	sitemapRaw := mustReadFile(t, filepath.Join(outDir, "sitemap.xml"))
	for _, unwantedURL := range []string{"/listings/", "/listings/test-listing-1/", "/listings/test-listing-2/"} {
		if strings.Contains(string(sitemapRaw), unwantedURL) {
			t.Errorf("expected sitemap.xml NOT to contain %s when listings is disabled, got %s", unwantedURL, string(sitemapRaw))
		}
	}

	// (c) stale carousel data must not leak into the home page.
	index := mustReadFile(t, filepath.Join(outDir, fileIndexHTML))
	if !strings.Contains(string(index), `data-count="0"`) {
		t.Fatalf("expected disabled listings section to leave no stale carousel data on the home page, got %s", string(index))
	}
}

func TestRun_ExecutesWebsiteOwnedOutputTests(t *testing.T) {
	websiteRoot, err := filepath.Abs(filepath.Join("..", "..", "examples", "hello-world"))
	if err != nil {
		t.Fatalf("resolving website root: %v", err)
	}
	outDir := t.TempDir()
	if err := Run([]string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-run-output-tests",
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("Run() with output tests: %v", err)
	}
}

func TestRun_PassesPageNameToTemplates(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<main data-page="{{.PageName}}">agenda</main>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	rendered, err := os.ReadFile(filepath.Join(outDir, fileAgendaHTML))
	if err != nil {
		t.Fatalf(readingAgendaFmt, err)
	}
	if !strings.Contains(string(rendered), `data-page="agenda"`) {
		t.Fatalf("expected page name in rendered html, got %s", string(rendered))
	}
}

func TestRun_PassesSiteDataToTemplates(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "data", fileSiteYAML):                   "courses:\n  agenda:\n    title: Agenda Centralizada\n",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<main data-title="{{required (dig .SiteData "courses" "agenda" "title") "missing courses.agenda.title"}}">{{required (dig .SiteData "courses" "agenda" "title") "missing courses.agenda.title"}}</main>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	rendered, err := os.ReadFile(filepath.Join(outDir, fileAgendaHTML))
	if err != nil {
		t.Fatalf(readingAgendaFmt, err)
	}
	if !strings.Contains(string(rendered), `data-title="Agenda Centralizada"`) {
		t.Fatalf("expected site data in rendered html, got %s", string(rendered))
	}
}

func TestRun_RequiredFailsForMissingSiteData(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}{{required (dig .SiteData "courses" "agenda" "title") "missing courses.agenda.title"}}{{end}}`,
	})

	outDir := t.TempDir()
	err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected build run to fail when required site data is missing")
	}
	if !strings.Contains(err.Error(), "missing courses.agenda.title") {
		t.Fatalf("expected missing site data error, got %v", err)
	}
}

func TestRun_FailsWhenSiteDataContractMissing(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}ok{{end}}`,
	})

	err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, t.TempDir()}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected build run to fail without site contract")
	}
	if !strings.Contains(err.Error(), "required site data contract not found") {
		t.Fatalf("expected missing contract error, got %v", err)
	}
}

func TestRun_FailsWhenSiteDataViolatesContract(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteYAML):                   "courses:\n  ssyb:\n    start_text: Em definição.\n    unexpected: value\n",
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "allowed:\n  - courses.*.start_text\n",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}ok{{end}}`,
	})

	outDir := t.TempDir()
	err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected build run to fail when site data violates contract")
	}
	if !strings.Contains(err.Error(), "dangling site data path not declared in contract: courses.ssyb.unexpected") {
		t.Fatalf("expected dangling path error, got %v", err)
	}
}

func TestRun_FailsWhenContractDeclaresUnusedTemplatePath(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS): mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteYAML): `courses:
  ssyb:
    investment:
      total: R$ 100,00
      installments_text: Em até 2 parcelas
`,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML): `allowed:
  - courses.*.investment.total
  - courses.*.investment.installments_text
`,
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}{{required (dig .SiteData "courses" "ssyb" "investment" "total") "missing total"}}{{end}}`,
	})

	err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, t.TempDir()}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected build run to fail for unused contract path")
	}
	if !strings.Contains(err.Error(), "allowed contract path not used by templates: courses.*.investment.installments_text") {
		t.Fatalf("expected unused contract path error, got %v", err)
	}
}

func TestRun_InternalPageExcludedFromOutput(t *testing.T) {
	siteYAML := `pages:
  agenda:
    title: Agenda interna
    internal: true
`
	tmpl := `{{define "page"}}{{required (dig .SiteData "pages" "agenda" "title") "missing pages.agenda.title"}}{{end}}`

	cases := []struct {
		name     string
		contract string
	}{
		{
			name:     "internal flag without contract path",
			contract: "allowed:\n  - pages.*.title\n",
		},
		{
			name:     "internal flag with contract pattern",
			contract: "allowed:\n  - pages.*.title\n  - pages.*.internal\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			websiteRoot := newTestWebsiteRoot(t)
			testutil.WriteFiles(t, map[string]string{
				filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
				filepath.Join(websiteRoot, "src", "data", fileSiteYAML):                   siteYAML,
				filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           tc.contract,
				filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): tmpl,
			})

			outDir := t.TempDir()
			if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
				t.Fatalf(buildRunFailed, err)
			}
			if _, err := os.Stat(filepath.Join(outDir, fileAgendaHTML)); !os.IsNotExist(err) {
				t.Fatalf("expected internal page output to be absent, got err=%v", err)
			}
		})
	}
}

func TestRun_SiteDataOverrideWinsAndWarns(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	websiteRoot := newTestWebsiteRoot(t)
	overridePath := filepath.Join(t.TempDir(), fileSiteYAML)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS): mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteYAML):         "courses:\n  agenda:\n    title: Local\n",
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML): "",
		overridePath: "courses:\n  agenda:\n    title: External\n",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}{{required (dig .SiteData "courses" "agenda" "title") "missing courses.agenda.title"}}{{end}}`,
	})

	outDir := t.TempDir()
	args := []string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-site-data", overridePath,
	}
	if err := Run(args, logger); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	rendered, err := os.ReadFile(filepath.Join(outDir, fileAgendaHTML))
	if err != nil {
		t.Fatalf(readingAgendaFmt, err)
	}
	if !strings.Contains(string(rendered), "External") {
		t.Fatalf("expected external site data to win, got %s", string(rendered))
	}
	if !strings.Contains(logBuf.String(), "site data override supersedes local site data file") {
		t.Fatalf("expected warning about site data override, got logs: %s", logBuf.String())
	}
}

func TestRun_MirrorsExternalAssetsIntoOutput(t *testing.T) {
	server := newMirrorAssetsTestServer(t)
	defer server.Close()

	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           "body { background-image: url('" + server.URL + "/local-bg.png'); }\n",
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}<link rel="stylesheet" href="/css/main.css"><link rel="stylesheet" href="` + server.URL + `/remote.css">{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "index.gohtml"):   `{{define "page"}}<img src="` + server.URL + `/inline-image.png" alt="remote">{{end}}`,
	})

	outDir := t.TempDir()
	args := []string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-mirror-external-assets",
	}
	if err := Run(args, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	indexPath := filepath.Join(outDir, fileIndexHTML)
	indexHTML := string(mustReadFile(t, indexPath))
	assertNoExternalRefs(t, fileIndexHTML, indexHTML, server.URL)
	// head CSS is inlined as a <style> block; the original css file is not copied.
	assertContainsAll(t, fileIndexHTML, indexHTML, []string{`href="/external/`, `src="/external/`, `url("/external/`})
	if _, err := os.Stat(filepath.Join(outDir, "css", fileMainCSS)); !os.IsNotExist(err) {
		t.Fatalf("expected original css not to be copied to dist, got err=%v", err)
	}

	mustStat(t, filepath.Join(outDir, "external"))

	mirroredCSS := readFirstMirroredCSS(t, filepath.Join(outDir, "external"))
	if mirroredCSS == "" {
		t.Fatal("expected mirrored external css file")
	}
	assertNoExternalRefs(t, "mirrored css", mirroredCSS, server.URL)
	assertContainsAll(t, "mirrored css", mirroredCSS, []string{`url("/external/`})
}

func TestRun_InlineBodyCSS(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "assets", "css"))
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "assets", "css", "widget.css"):          ".widget { color: blue; }\n",
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>hello</p>{{end}}`,
		// widget.css is in the body → normally deferred; with -inline-body-css it becomes <style>.
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): `{{define "head"}}<link rel="stylesheet" href="/css/main.css">{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"): `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>` +
			`<link rel="stylesheet" href="/css/widget.css">{{template "page" .}}</body></html>{{end}}`,
	})

	outDir := t.TempDir()
	args := []string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-inline-body-css",
	}
	if err := Run(args, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	html := string(mustReadFile(t, filepath.Join(outDir, fileAgendaHTML)))
	// Body CSS should be inlined as a <style> block, not deferred via media="print".
	if strings.Contains(html, `media="print"`) {
		t.Fatalf("expected body CSS inlined, still has deferred media=print pattern")
	}
	if !strings.Contains(html, "color:blue") && !strings.Contains(html, "color: blue") {
		t.Fatalf("expected widget CSS content inlined in HTML")
	}
	// No external CSS files should remain in dist.
	if _, err := os.Stat(filepath.Join(outDir, "css")); !os.IsNotExist(err) {
		t.Fatalf("expected no css/ dir in dist when all CSS is inlined, got err=%v", err)
	}
}

func TestRun_EmbedFonts(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "assets", "fonts"))
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "fonts", "inter.woff2"):       "woff2data",
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           `@font-face { src: url("/fonts/inter.woff2"); }`,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>hello</p>{{end}}`,
	})

	outDir := t.TempDir()
	args := []string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-embed-fonts",
	}
	if err := Run(args, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	html := string(mustReadFile(t, filepath.Join(outDir, fileAgendaHTML)))
	// Font should be embedded as a data URI, not a root-relative path.
	if strings.Contains(html, `url("/fonts/inter.woff2")`) {
		t.Fatalf("expected font embedded as data URI, still has path reference")
	}
	if !strings.Contains(html, "data:font/woff2;base64,") {
		t.Fatalf("expected base64 font data URI in HTML, got: %s", html)
	}
	// Font file itself should not be in dist (it's embedded).
	if _, err := os.Stat(filepath.Join(outDir, "fonts", "inter.woff2")); !os.IsNotExist(err) {
		t.Fatalf("expected font file not copied to dist when embedded, got err=%v", err)
	}
}

func TestRun_OriginalCSSNotCopiedToDist(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>hello</p>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	// Original (non-fingerprinted) CSS must not exist in dist.
	if _, err := os.Stat(filepath.Join(outDir, "css", fileMainCSS)); !os.IsNotExist(err) {
		t.Fatalf("expected original css not to be in dist, got err=%v", err)
	}
	// CSS from head is inlined; the page should have a <style> block.
	html := string(mustReadFile(t, filepath.Join(outDir, fileAgendaHTML)))
	if !strings.Contains(html, "<style>") {
		t.Fatalf("expected inlined <style> block, got: %s", html)
	}
}

func TestRun_LDDirStillCopied(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "assets", "ld"))
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "assets", "ld", "org.json"):             `{"@context":"https://schema.org","@type":"Organization"}`,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>hello</p>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	mustStat(t, filepath.Join(outDir, "ld", "org.json"))
}

func newMirrorAssetsTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	baseURL := new(string)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/remote.css" {
			w.Header().Set(httpContentType, "text/css")
			_, _ = io.WriteString(w, "@font-face { src: url('/font.woff2'); } .hero { background-image: url('"+*baseURL+"/bg.png'); }")
			return
		}
		if r.URL.Path == "/font.woff2" {
			w.Header().Set(httpContentType, "font/woff2")
			_, _ = w.Write([]byte("woff2-data"))
			return
		}
		if r.URL.Path == "/bg.png" || r.URL.Path == "/inline-image.png" || r.URL.Path == "/local-bg.png" {
			w.Header().Set(httpContentType, "image/png")
			_, _ = w.Write([]byte("png-data"))
			return
		}
		http.NotFound(w, r)
	})

	server := httptest.NewServer(handler)
	*baseURL = server.URL
	return server
}

func newTestWebsiteRoot(t *testing.T) string {
	t.Helper()

	websiteRoot := t.TempDir()
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "assets", "css"))
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "data"))
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "templates", "layout"))
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "templates", "partials"))
	testutil.MustMkdirAll(t, filepath.Join(websiteRoot, "src", "templates", "pages"))
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"):   stdLayoutTmpl,
		filepath.Join(websiteRoot, "src", "templates", "partials", "head.gohtml"): stdHeadTmpl,
	})
	return websiteRoot
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return raw
}

// buildDevDataPayload should stay consistent with sectionTable: a section's
// items only appear in the -dev-data payload while sectionEnabled agrees that
// section is on.
func TestBuildDevDataPayload_IncludesListingsWhenSectionEnabled(t *testing.T) {
	opts := buildOptions{devData: true, contentSource: "mock"}
	content := &optionalContent{
		listings: []listings.Listing{{ID: "l1", Title: "Listing One"}},
	}

	got := buildDevDataPayload(opts, map[string]any{}, content)
	if !strings.Contains(got, `"listings":[{"id":"l1","langs":null}]`) {
		t.Fatalf("expected listings entry in dev data payload, got %s", got)
	}
}

func TestBuildDevDataPayload_OmitsListingsWhenSectionDisabled(t *testing.T) {
	opts := buildOptions{devData: true, contentSource: "mock"}
	siteData := map[string]any{"sections": map[string]any{"listings": false}}
	content := &optionalContent{
		listings: []listings.Listing{{ID: "l1", Title: "Listing One"}},
	}

	got := buildDevDataPayload(opts, siteData, content)
	if strings.Contains(got, "l1") {
		t.Fatalf("expected listings to be omitted from dev data payload when section is disabled, got %s", got)
	}
	if !strings.Contains(got, `"listings":null`) {
		t.Fatalf(`expected "listings":null in dev data payload when section is disabled, got %s`, got)
	}
}

func TestBuildDevDataPayload_EmptyWhenDevDataFlagOff(t *testing.T) {
	got := buildDevDataPayload(buildOptions{devData: false}, map[string]any{}, &optionalContent{})
	if got != "" {
		t.Fatalf("expected empty payload when -dev-data is off, got %s", got)
	}
}

func mustStat(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s: %v", path, err)
	}
}

func assertNoExternalRefs(t *testing.T, label, content, external string) {
	t.Helper()
	if strings.Contains(content, external) {
		t.Fatalf("expected %s to avoid external references, got %s", label, content)
	}
}

func assertContainsAll(t *testing.T, label, content string, expected []string) {
	t.Helper()
	for _, e := range expected {
		if !strings.Contains(content, e) {
			t.Fatalf("expected %s to contain %q, got %s", label, e, content)
		}
	}
}

func readFirstMirroredCSS(t *testing.T, externalDir string) string {
	t.Helper()

	var mirroredCSS string
	err := filepath.WalkDir(externalDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".css" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mirroredCSS = string(content)
		return filepath.SkipAll
	})
	if err != nil {
		t.Fatalf("walking mirrored assets: %v", err)
	}
	return mirroredCSS
}

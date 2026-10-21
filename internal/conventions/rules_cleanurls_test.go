package conventions

import "testing"

func TestNoHTMLExtensionURLs(t *testing.T) {
	cases := []struct {
		name     string
		html     string
		wantHits int
	}{
		// The case the compiler's own transform deliberately cannot reach, and
		// the reason this rule exists: the path is already clean, the page
		// reference hides in a query value.
		{
			name:     "query value carrying a page reference",
			html:     `<a href="/login?next=account.html">Sign in</a>`,
			wantHits: 1,
		},
		// A single-quoted attribute is out of the transform's scope, so the gate
		// is the only thing that sees it.
		{
			name:     "single-quoted attribute",
			html:     `<a href='login.html'>in</a>`,
			wantHits: 1,
		},
		{
			name:     "plain flat href",
			html:     `<a href="login.html">in</a>`,
			wantHits: 1,
		},
		{
			name:     "form action",
			html:     `<form action="subscribe.html" method="post"></form>`,
			wantHits: 1,
		},
		{
			name:     "area href",
			html:     `<area href="map.html" shape="rect">`,
			wantHits: 1,
		},
		{
			name:     "fragment on a flat href",
			html:     `<a href="about.html#team">t</a>`,
			wantHits: 1,
		},

		// One finding per distinct destination, so a shared nav repeated on
		// sixty pages does not produce sixty identical lines.
		{
			name:     "duplicate destinations collapse",
			html:     `<a href="a.html">1</a><a href="a.html">2</a><a href="a.html">3</a>`,
			wantHits: 1,
		},
		{
			name:     "distinct destinations each report",
			html:     `<a href="a.html">1</a><a href="b.html">2</a>`,
			wantHits: 2,
		},

		// Clean — the shape the compiler now emits.
		{"extension-free", `<a href="/en/login">in</a>`, 0},
		{"directory form", `<a href="/en/blog/">b</a>`, 0},
		{"site root", `<a href="/">home</a>`, 0},

		// Not page URLs.
		{"external http", `<a href="http://x.test/a.html">x</a>`, 0},
		{"external https", `<a href="https://x.test/a.html">x</a>`, 0},
		{"protocol relative", `<a href="//cdn.test/a.html">x</a>`, 0},
		{"mailto", `<a href="mailto:a@b.test">m</a>`, 0},
		{"tel", `<a href="tel:+5551">t</a>`, 0},
		{"javascript", `<a href="javascript:void(0)">j</a>`, 0},
		{"fragment only", `<a href="#main">skip</a>`, 0},
		{"empty href", `<a href="">e</a>`, 0},

		// Asset references are owned by fingerprintLocalAssets and are never
		// page URLs — matching them would make this rule fight the compiler.
		{"stylesheet link is not navigational", `<link rel="stylesheet" href="/css/app.html">`, 0},
		{"img src is not navigational", `<img src="a.html">`, 0},
		{"script src is not navigational", `<script src="/js/app.html"></script>`, 0},

		// A non-.html extension is somebody's download, not a page.
		{"pdf", `<a href="/en/report.pdf">r</a>`, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ruleNoHTMLExtensionURLs.Scan("index.html", tc.html)
			if len(got) != tc.wantHits {
				t.Fatalf("got %d finding(s), want %d\nhtml: %s\nfindings: %+v",
					len(got), tc.wantHits, tc.html, got)
			}
		})
	}
}

func TestNoHTMLExtensionURLsIsRegistered(t *testing.T) {
	// A rule missing from the registry is silently unusable: conventions.yaml
	// naming it would be a config error rather than enabling it.
	if _, ok := registry[ruleNameNoHTMLExtensionURLs]; !ok {
		t.Fatalf("%s must be in the registry", ruleNameNoHTMLExtensionURLs)
	}
	for _, n := range RuleNames() {
		if n == ruleNameNoHTMLExtensionURLs {
			return
		}
	}
	t.Fatalf("%s must appear in RuleNames()", ruleNameNoHTMLExtensionURLs)
}

func TestNoHTMLExtensionURLsReadsBuiltOutput(t *testing.T) {
	// DomainOutput, not DomainSource: a template partial is not a page, and the
	// query-value case only exists after data has been rendered into the href.
	if ruleNoHTMLExtensionURLs.Domain != DomainOutput {
		t.Fatalf("domain = %v, want DomainOutput", ruleNoHTMLExtensionURLs.Domain)
	}
	if !ruleNoHTMLExtensionURLs.Selects("index.html") {
		t.Fatal("must select built HTML")
	}
	if ruleNoHTMLExtensionURLs.Selects("src/assets/css/app.css") {
		t.Fatal("must not select CSS")
	}
}

func TestNoHTMLExtensionURLsScansSitemap(t *testing.T) {
	// The real forma case: pages were already extension-free while a COMMITTED
	// sitemap still advertised the flat URLs, and nothing looked at it.
	sitemap := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://forma.ffreis.com/brochures.html</loc></url>
  <url><loc>https://forma.ffreis.com/bundles.html</loc></url>
  <url><loc>https://forma.ffreis.com/catalogs</loc></url>
  <url><loc>https://forma.ffreis.com/</loc></url>
</urlset>`

	got := ruleNoHTMLExtensionURLs.Scan("sitemap.xml", sitemap)
	if len(got) != 2 {
		t.Fatalf("got %d finding(s), want 2: %+v", len(got), got)
	}
	// Line numbers must point at the offending entry, not at the file start.
	if got[0].Line != 3 {
		t.Errorf("first finding line = %d, want 3", got[0].Line)
	}
	if got[1].Line != 4 {
		t.Errorf("second finding line = %d, want 4", got[1].Line)
	}
}

func TestNoHTMLExtensionURLsCleanSitemapPasses(t *testing.T) {
	sitemap := `<urlset><url><loc>https://x.test/</loc></url>` +
		`<url><loc>https://x.test/login</loc></url>` +
		`<url><loc>https://x.test/blog/</loc></url></urlset>`
	if got := ruleNoHTMLExtensionURLs.Scan("sitemap.xml", sitemap); len(got) != 0 {
		t.Fatalf("extension-free sitemap must pass, got: %+v", got)
	}
}

func TestNoHTMLExtensionURLsSelectsSitemapAndHTMLOnly(t *testing.T) {
	for _, rel := range []string{"index.html", "login/index.html", "sitemap.xml", "dist/sitemap.xml"} {
		if !ruleNoHTMLExtensionURLs.Selects(rel) {
			t.Errorf("must select %q", rel)
		}
	}
	for _, rel := range []string{"css/app.css", "js/app.js", "robots.txt", "manifest.json"} {
		if ruleNoHTMLExtensionURLs.Selects(rel) {
			t.Errorf("must not select %q", rel)
		}
	}
}

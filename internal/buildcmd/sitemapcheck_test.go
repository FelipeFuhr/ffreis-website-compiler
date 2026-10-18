package buildcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// formaSitemapExcerpt is the real shape of ffreis-forma's committed
// sitemap.xml, which its /en/ deployment served verbatim: every <loc>
// unprefixed, while the pages live under /en/.
const formaSitemapExcerpt = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://forma.ffreis.com/</loc></url>
  <url><loc>https://forma.ffreis.com/brochures.html</loc></url>
  <url><loc>https://forma.ffreis.com/contact.html</loc></url>
</urlset>`

func assetsWithSitemap(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, sitemapXML), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCheckSitemapBasePath_CatchesTheLiveFormaMismatch(t *testing.T) {
	dir := assetsWithSitemap(t, formaSitemapExcerpt)
	err := checkSitemapBasePath(dir, buildOptions{basePath: "/en"})
	if err == nil {
		t.Fatal("expected the unprefixed sitemap to fail an /en build")
	}
	for _, want := range []string{"3 URL(s)", "/en", "brochures.html", "canonicals"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

func TestCheckSitemapBasePath_PassesWhenPrefixed(t *testing.T) {
	body := strings.ReplaceAll(formaSitemapExcerpt, "ffreis.com/", "ffreis.com/en/")
	if err := checkSitemapBasePath(assetsWithSitemap(t, body), buildOptions{basePath: "/en"}); err != nil {
		t.Fatalf("a correctly prefixed sitemap must pass: %v", err)
	}
}

func TestCheckSitemapBasePath_RootDeploymentIsNotJudged(t *testing.T) {
	// At the distribution root every root-absolute URL is correct by
	// construction. Running there would fail every site in the fleet.
	for _, base := range []string{"", "/"} {
		if err := checkSitemapBasePath(assetsWithSitemap(t, formaSitemapExcerpt), buildOptions{basePath: base}); err != nil {
			t.Errorf("basePath %q must be a no-op, got: %v", base, err)
		}
	}
}

func TestCheckSitemapBasePath_NoCommittedSitemapIsFine(t *testing.T) {
	// The compiler's own -sitemap-base-url generation is prefixed correctly by
	// construction, so a site with no committed file has nothing to check.
	if err := checkSitemapBasePath(t.TempDir(), buildOptions{basePath: "/en"}); err != nil {
		t.Fatalf("a missing sitemap must not fail the build: %v", err)
	}
}

func TestCheckSitemapBasePath_LeavesAnotherHostAlone(t *testing.T) {
	// A sitemap may legitimately reference a sibling property. Guessing
	// otherwise is how a check becomes something sites switch off.
	body := `<urlset><url><loc>https://elsewhere.example/page.html</loc></url>` +
		`<url><loc>https://forma.ffreis.com/en/ok.html</loc></url></urlset>`
	opts := buildOptions{basePath: "/en", sitemapBaseURL: "https://forma.ffreis.com"}
	if err := checkSitemapBasePath(assetsWithSitemap(t, body), opts); err != nil {
		t.Fatalf("an off-host loc must not be judged when the own host is known: %v", err)
	}
}

func TestCheckSitemapBasePath_WithoutAnOwnHostEveryAbsoluteLocIsJudged(t *testing.T) {
	// -sitemap-base-url is not always passed. With no way to tell the site's own
	// property from another, the strict reading is the right default for a check
	// that fails the build — and it is what caught forma, whose build passes no
	// sitemap base URL at all.
	body := `<urlset><url><loc>https://elsewhere.example/page.html</loc></url></urlset>`
	if err := checkSitemapBasePath(assetsWithSitemap(t, body), buildOptions{basePath: "/en"}); err == nil {
		t.Fatal("with no own host known, an absolute loc outside the base path must be reported")
	}
}

func TestCheckSitemapBasePath_JudgesRootAbsoluteLocs(t *testing.T) {
	body := `<urlset><url><loc>/brochures.html</loc></url></urlset>`
	err := checkSitemapBasePath(assetsWithSitemap(t, body), buildOptions{basePath: "/en"})
	if err == nil || !strings.Contains(err.Error(), "/brochures.html") {
		t.Fatalf("a root-absolute loc outside the base path must fail: %v", err)
	}
}

func TestCheckSitemapBasePath_BareOriginCountsAsOutside(t *testing.T) {
	// https://host (no path) is the distribution root, which an /en deployment
	// does not serve.
	body := `<urlset><url><loc>https://forma.ffreis.com</loc></url></urlset>`
	if err := checkSitemapBasePath(assetsWithSitemap(t, body), buildOptions{basePath: "/en"}); err == nil {
		t.Fatal("a bare origin must be reported for a prefixed deployment")
	}
}

func TestCheckSitemapBasePath_ReportsEveryOffenderOnceAndCapsTheList(t *testing.T) {
	var b strings.Builder
	b.WriteString("<urlset>")
	for i := 0; i < 12; i++ {
		b.WriteString("<url><loc>https://forma.ffreis.com/p")
		b.WriteString(string(rune('a' + i)))
		b.WriteString(".html</loc></url>")
	}
	b.WriteString("</urlset>")
	err := checkSitemapBasePath(assetsWithSitemap(t, b.String()), buildOptions{basePath: "/en"})
	if err == nil {
		t.Fatal("expected a failure")
	}
	if !strings.Contains(err.Error(), "12 URL(s)") {
		t.Errorf("expected the full count, got: %v", err)
	}
	if !strings.Contains(err.Error(), "and 7 more") {
		t.Errorf("expected the list capped with a remainder, got: %v", err)
	}
}

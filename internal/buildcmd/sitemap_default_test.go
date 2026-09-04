package buildcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/testutil"
)

// removeDefaultSitemapConfig deletes the sitemap.yaml newTestWebsiteRoot
// writes by default, so a test can exercise a website root with no sitemap
// config at all — the precondition for testing the default-on fallback
// chain (explicit -sitemap-base-url, then site data's base_url) and the
// prod-build fail-closed guard, both of which only kick in once no config
// file is found.
func removeDefaultSitemapConfig(t *testing.T, websiteRoot string) {
	t.Helper()
	if err := os.Remove(filepath.Join(websiteRoot, "sitemap.yaml")); err != nil {
		t.Fatalf("removing default test sitemap.yaml: %v", err)
	}
}

// TestRun_DefaultOnSitemap_DerivesBaseURLFromSiteData proves sitemap
// generation is now default-on: with no sitemap.yaml config and no
// -sitemap-base-url flag, a sitemap.xml is still emitted, using base_url
// from site data (the same field already read by posts.go's RSS feed link
// and hreflang.go's canonical URLs) as the fallback base URL.
func TestRun_DefaultOnSitemap_DerivesBaseURLFromSiteData(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	removeDefaultSitemapConfig(t, websiteRoot)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteYAML):                   "base_url: https://example.com\n",
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>ok</p>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	sitemapRaw := string(mustReadFile(t, filepath.Join(outDir, "sitemap.xml")))
	if !strings.Contains(sitemapRaw, "<loc>https://example.com/agenda.html</loc>") {
		t.Fatalf("expected default-on sitemap derived from site data base_url to include the agenda page, got %s", sitemapRaw)
	}
}

// TestRun_ProdBuildFailsWithoutDerivableBaseURL proves the previously-silent
// "no sitemap at all" outcome is now a hard build failure on a real
// (non-mock) build: no sitemap.yaml, no -sitemap-base-url, and no base_url
// in site data leaves nothing for the default-on fallback chain to resolve.
func TestRun_ProdBuildFailsWithoutDerivableBaseURL(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	removeDefaultSitemapConfig(t, websiteRoot)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>ok</p>{{end}}`,
	})

	outDir := t.TempDir()
	err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected a prod build with no derivable sitemap base URL to fail, got nil error")
	}
	if !strings.Contains(err.Error(), "sitemap") {
		t.Fatalf("expected a sitemap-related error, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outDir, "sitemap.xml")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no sitemap.xml to be written when the build fails, got err=%v", statErr)
	}
}

// TestRun_MockBuildSucceedsWithoutSitemapWhenNoBaseURLDerivable is the
// contrast case for TestRun_ProdBuildFailsWithoutDerivableBaseURL: the same
// missing-base-URL scenario on a mock/dev build (-content-source mock) is a
// legitimate no-sitemap outcome, so the build must still succeed.
func TestRun_MockBuildSucceedsWithoutSitemapWhenNoBaseURLDerivable(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	removeDefaultSitemapConfig(t, websiteRoot)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>ok</p>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-content-source", "mock",
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	if _, err := os.Stat(filepath.Join(outDir, "sitemap.xml")); !os.IsNotExist(err) {
		t.Fatalf("expected no sitemap.xml for a mock build with no derivable base URL, got err=%v", err)
	}
}

// TestRun_RobotsTxtGetsSitemapDirectiveWhenSitemapGenerated proves robots.txt
// in the output gets a "Sitemap: <base_url>/sitemap.xml" line whenever a
// sitemap was actually generated this build.
func TestRun_RobotsTxtGetsSitemapDirectiveWhenSitemapGenerated(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	removeDefaultSitemapConfig(t, websiteRoot)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "assets", "robots.txt"):                 "User-agent: *\nAllow: /\n",
		filepath.Join(websiteRoot, "src", "data", fileSiteYAML):                   "base_url: https://example.com\n",
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>ok</p>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	robots := string(mustReadFile(t, filepath.Join(outDir, "robots.txt")))
	const wantDirective = "Sitemap: https://example.com/sitemap.xml"
	if !strings.Contains(robots, wantDirective) {
		t.Fatalf("expected robots.txt to declare the generated sitemap, got %q", robots)
	}
	if strings.Count(robots, wantDirective) != 1 {
		t.Fatalf("expected exactly one Sitemap directive, got %q", robots)
	}
	if !strings.Contains(robots, "User-agent: *\nAllow: /") {
		t.Fatalf("expected the original robots.txt content to be preserved, got %q", robots)
	}
}

// TestRun_RobotsTxtSitemapDirectiveNotDuplicatedWhenAlreadyPresent proves the
// injection is idempotent: a source robots.txt that already declares the
// exact Sitemap directive verbatim must not end up with a duplicate.
func TestRun_RobotsTxtSitemapDirectiveNotDuplicatedWhenAlreadyPresent(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	removeDefaultSitemapConfig(t, websiteRoot)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "assets", "robots.txt"):                 "User-agent: *\nAllow: /\nSitemap: https://example.com/sitemap.xml\n",
		filepath.Join(websiteRoot, "src", "data", fileSiteYAML):                   "base_url: https://example.com\n",
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>ok</p>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, websiteRoot, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	robots := string(mustReadFile(t, filepath.Join(outDir, "robots.txt")))
	if got := strings.Count(robots, "Sitemap: https://example.com/sitemap.xml"); got != 1 {
		t.Fatalf("expected the pre-existing Sitemap directive not to be duplicated (count=%d), got %q", got, robots)
	}
}

// TestRun_RobotsTxtLeftAloneWhenSitemapGenerationSkipped proves robots.txt is
// passed through byte-for-byte when sitemap generation is legitimately
// skipped (a mock build with no derivable base URL) — no Sitemap directive
// is invented for a build that shipped no sitemap.xml.
func TestRun_RobotsTxtLeftAloneWhenSitemapGenerationSkipped(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	removeDefaultSitemapConfig(t, websiteRoot)
	const robotsBody = "User-agent: *\nAllow: /\n"
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):           mainCSSContent,
		filepath.Join(websiteRoot, "src", "assets", "robots.txt"):                 robotsBody,
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):           "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML): `{{define "page"}}<p>ok</p>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-content-source", "mock",
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	robots := string(mustReadFile(t, filepath.Join(outDir, "robots.txt")))
	if robots != robotsBody {
		t.Fatalf("expected robots.txt to pass through unchanged when sitemap generation is skipped, got %q, want %q", robots, robotsBody)
	}
}

// TestRun_DefaultOnSitemap_ExcludesDisabledSectionPages proves the
// default-on sitemap path (base URL derived from site data, no sitemap.yaml
// config) still excludes a disabled section's pages, by going through the
// same filterInternalPages pruning generateSitemapFromPages already relies
// on for every sitemap path — not a new, parallel section check.
func TestRun_DefaultOnSitemap_ExcludesDisabledSectionPages(t *testing.T) {
	websiteRoot := newTestWebsiteRoot(t)
	removeDefaultSitemapConfig(t, websiteRoot)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "assets", "css", fileMainCSS):            mainCSSContent,
		filepath.Join(websiteRoot, "src", "data", fileSiteYAML):                    "base_url: https://example.com\n",
		filepath.Join(websiteRoot, "src", "data", fileSiteContractYAML):            "",
		filepath.Join(websiteRoot, "src", "templates", "pages", fileAgendaGoHTML):  `{{define "page"}}<p>ok</p>{{end}}`,
		filepath.Join(websiteRoot, "src", "templates", "pages", "projects.gohtml"): `{{define "page"}}<p>projects</p>{{end}}`,
	})

	outDir := t.TempDir()
	if err := Run([]string{
		flagWebsiteRoot, websiteRoot,
		flagOut, outDir,
		"-disable-sections", "projects",
	}, testutil.DiscardLogger()); err != nil {
		t.Fatalf(buildRunFailed, err)
	}

	sitemapRaw := string(mustReadFile(t, filepath.Join(outDir, "sitemap.xml")))
	if strings.Contains(sitemapRaw, "/projects.html") {
		t.Fatalf("expected disabled projects section to be excluded from the default-on sitemap, got %s", sitemapRaw)
	}
	if !strings.Contains(sitemapRaw, "/agenda.html") {
		t.Fatalf("expected the enabled agenda page to remain in the default-on sitemap, got %s", sitemapRaw)
	}
}

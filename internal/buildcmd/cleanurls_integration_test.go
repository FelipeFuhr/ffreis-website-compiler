package buildcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/testutil"
)

// These exercise Run end to end rather than the transform in isolation. The unit
// tests in cleanurlhrefs_test.go prove the rewrite; these prove it reaches the
// written page, in the order the real pipeline applies it, and that the gates
// downstream of it (linkcheck, sitemap) agree with the result.
//
// That distinction is not academic here: the first version of this work passed
// its unit tests while the built output still carried .html in five pages and a
// sitemap, and only a real build revealed it.

// writeTwoPageSite lays out a site whose pages link to each other with the flat,
// data-authored spelling that forma uses — relative `other.html`, root-absolute
// `/other.html`, a fragment, and a query.
func writeTwoPageSite(t *testing.T) string {
	t.Helper()
	root := newTestWebsiteRoot(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(root, "src", "assets", "css", fileMainCSS): mainCSSContent,
		filepath.Join(root, "src", "data", fileSiteContractYAML): "",
		filepath.Join(root, "src", "templates", "pages", "agenda.gohtml"): `{{define "page"}}` +
			`<a id="rel" href="contact.html">rel</a>` +
			`<a id="abs" href="/contact.html">abs</a>` +
			`<a id="frag" href="contact.html#form">frag</a>` +
			`<a id="query" href="contact.html?src=nav">query</a>` +
			`<a id="home" href="index.html">home</a>` +
			`<a id="ext" href="https://example.test/a.html">ext</a>` +
			`{{end}}`,
		filepath.Join(root, "src", "templates", "pages", "contact.gohtml"): `{{define "page"}}` +
			`<a id="back" href="agenda.html">back</a>` +
			`<form id="f" action="contact.html" method="post"></form>` +
			`{{end}}`,
		// An index page is required, not incidental: `href="index.html"` rewrites
		// to `/`, and linkcheck resolves that against the written output. Without
		// one the build correctly fails with `"/" not found` — which is how the
		// first version of this fixture was caught.
		filepath.Join(root, "src", "templates", "pages", "index.gohtml"): `{{define "page"}}` +
			`<a id="to-agenda" href="agenda.html">agenda</a>` +
			`{{end}}`,
	})
	return root
}

func TestRun_FlatAuthoredHrefsBecomeExtensionFreeInTheWrittenPage(t *testing.T) {
	root := writeTwoPageSite(t)
	outDir := t.TempDir()

	if err := Run([]string{flagWebsiteRoot, root, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	agenda := string(mustReadFile(t, filepath.Join(outDir, "agenda", "index.html")))

	for _, want := range []string{
		`href="/contact"`,         // relative, resolved against the deployment root
		`href="/contact#form"`,    // fragment preserved
		`href="/contact?src=nav"`, // query preserved
		`href="/"`,                // index.html → directory form
	} {
		if !strings.Contains(agenda, want) {
			t.Errorf("expected %s in the written page", want)
		}
	}

	// The external link must survive untouched — rewriting it would send a
	// visitor to this site's own /a instead of the other origin's page.
	if !strings.Contains(agenda, `href="https://example.test/a.html"`) {
		t.Error("external .html href must be left alone")
	}

	// And nothing navigational may still carry the extension.
	if strings.Contains(agenda, `href="contact.html"`) || strings.Contains(agenda, `href="/contact.html"`) {
		t.Errorf("a flat href survived into the output:\n%s", agenda)
	}
}

func TestRun_FormActionIsRewrittenToo(t *testing.T) {
	root := writeTwoPageSite(t)
	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, root, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	contact := string(mustReadFile(t, filepath.Join(outDir, "contact", "index.html")))
	if !strings.Contains(contact, `action="/contact"`) {
		t.Errorf("form action must be rewritten:\n%s", contact)
	}
}

// The regression that matters most: a site authored entirely in the flat style
// must BUILD, because linkcheck runs over the written output. Before the
// transform this exact shape produced broken links and failed the build — which
// is what made extension-free URLs un-adoptable.
func TestRun_FlatAuthoredSiteStillPassesLinkCheck(t *testing.T) {
	root := writeTwoPageSite(t)
	outDir := t.TempDir()
	if err := Run([]string{flagWebsiteRoot, root, flagOut, outDir}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("a site authored with flat hrefs must build: %v", err)
	}
	// Both pages exist as directory indexes, and neither flat file was written.
	for _, page := range []string{"agenda", "contact"} {
		if _, err := os.Stat(filepath.Join(outDir, page, "index.html")); err != nil {
			t.Errorf("expected %s/index.html: %v", page, err)
		}
		if _, err := os.Stat(filepath.Join(outDir, page+".html")); !os.IsNotExist(err) {
			t.Errorf("%s.html must not be written, got err=%v", page, err)
		}
	}
}

func TestRun_GeneratedSitemapIsExtensionFree(t *testing.T) {
	root := writeTwoPageSite(t)
	outDir := t.TempDir()
	args := []string{flagWebsiteRoot, root, flagOut, outDir, "-sitemap-base-url", "https://example.test"}
	if err := Run(args, testutil.DiscardLogger()); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	sitemap := string(mustReadFile(t, filepath.Join(outDir, "sitemap.xml")))
	if strings.Contains(sitemap, ".html</loc>") {
		t.Errorf("sitemap must not advertise .html URLs:\n%s", sitemap)
	}
	for _, want := range []string{"<loc>", "/agenda</loc>", "/contact</loc>"} {
		if !strings.Contains(sitemap, want) {
			t.Errorf("expected %q in sitemap:\n%s", want, sitemap)
		}
	}
}

// The deployer passes -clean-urls unconditionally, so accepting it is a
// compatibility requirement, not a nicety. If this test fails, every deploy
// fails the moment a site pins this compiler.
func TestRun_AcceptsDeprecatedCleanURLsFlag(t *testing.T) {
	root := writeTwoPageSite(t)
	outDir := t.TempDir()
	err := Run([]string{flagWebsiteRoot, root, flagOut, outDir, "-clean-urls"}, testutil.DiscardLogger())
	if err != nil {
		t.Fatalf("-clean-urls must still be accepted (the deployer passes it): %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outDir, "agenda", "index.html")); statErr != nil {
		t.Errorf("output must be unchanged by the deprecated flag: %v", statErr)
	}
}

// Asking for the removed behaviour must fail loudly. A silent no-op would leave
// the caller believing it got flat output.
func TestRun_RejectsCleanURLsFalse(t *testing.T) {
	root := writeTwoPageSite(t)
	outDir := t.TempDir()
	err := Run([]string{flagWebsiteRoot, root, flagOut, outDir, "-clean-urls=false"}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("-clean-urls=false must be rejected, not silently ignored")
	}
	if !strings.Contains(err.Error(), "no longer supported") {
		t.Errorf("error should explain the removal, got: %v", err)
	}
}

package buildcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── injectLangSwitcherHrefs ─────────────────────────────────────────────────

// The switcher anchor as the header template renders it: a static root href
// that this transform rewrites to the parallel page in the sibling language.
func switcherHTML(hrefs ...string) string {
	var sb strings.Builder
	for _, href := range hrefs {
		sb.WriteString(`<a class="lang-flag" href="` + href + `">flag</a>`)
	}
	return sb.String()
}

func TestInjectLangSwitcherHrefs_PointsAtTheParallelPageInTheSiblingLanguage(t *testing.T) {
	got := injectLangSwitcherHrefs(switcherHTML("/pt/"), hreflangSite(), "about", true)

	if !strings.Contains(got, `href="/pt/sobre/"`) {
		t.Errorf("got %q, want the PT slug from slug_map", got)
	}
}

func TestInjectLangSwitcherHrefs_FallsBackToThePageNameWithoutASlugMapEntry(t *testing.T) {
	got := injectLangSwitcherHrefs(switcherHTML("/pt/"), hreflangSite(), "pricing", true)

	if !strings.Contains(got, `href="/pt/pricing/"`) {
		t.Errorf("got %q, want the untranslated page name", got)
	}
}

// The active flag is the page you are already on; rewriting it would send a
// reader in a circle.
func TestInjectLangSwitcherHrefs_LeavesTheActiveLanguageAlone(t *testing.T) {
	got := injectLangSwitcherHrefs(switcherHTML("/pt/", "/en/"), hreflangSite(), "about", true)

	if !strings.Contains(got, `href="/en/"`) {
		t.Errorf("the active EN link was rewritten; got %q", got)
	}
}

func TestInjectLangSwitcherHrefs_IndexPointsAtTheLanguageRoot(t *testing.T) {
	got := injectLangSwitcherHrefs(switcherHTML("/pt/"), hreflangSite(), "index", true)

	if !strings.Contains(got, `href="/pt/"`) {
		t.Errorf("got %q, want the bare language root for the index page", got)
	}
	if strings.Contains(got, "/pt/index") {
		t.Errorf("got %q, want no index segment", got)
	}
}

func TestInjectLangSwitcherHrefs_NonCleanURLsGetAnHTMLExtension(t *testing.T) {
	got := injectLangSwitcherHrefs(switcherHTML("/pt/"), hreflangSite(), "about", false)

	if !strings.Contains(got, `href="/pt/sobre.html"`) {
		t.Errorf("got %q, want a .html sibling URL", got)
	}
}

func TestInjectLangSwitcherHrefs_LeavesHTMLUnchangedWhenThereIsNothingToRewrite(t *testing.T) {
	html := switcherHTML("/pt/")

	for _, tc := range []struct {
		name     string
		siteData map[string]any
	}{
		{"no ui section", map[string]any{}},
		{"no nav section", map[string]any{"ui": map[string]any{}}},
		{"no lang_links", map[string]any{"ui": map[string]any{"nav": map[string]any{}}}},
		{"lang_links is not a list", map[string]any{"ui": map[string]any{"nav": map[string]any{"lang_links": "pt"}}}},
		{"entry has an empty href", map[string]any{"ui": map[string]any{"nav": map[string]any{
			"lang_links": []any{map[string]any{"lang": "pt", "href": ""}},
		}}}},
		{"entry is not a map", map[string]any{"ui": map[string]any{"nav": map[string]any{
			"lang_links": []any{"pt"},
		}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := injectLangSwitcherHrefs(html, tc.siteData, "about", true); got != html {
				t.Errorf("html was modified; want it untouched:\n%s", got)
			}
		})
	}
}

// Only the language-switcher anchors carry class="lang-flag"; an ordinary link
// to the same href must not be rewritten.
func TestInjectLangSwitcherHrefs_OnlyRewritesLangFlagAnchors(t *testing.T) {
	html := `<a href="/pt/">ordinary link</a>` + switcherHTML("/pt/")

	got := injectLangSwitcherHrefs(html, hreflangSite(), "about", true)

	if !strings.Contains(got, `<a href="/pt/">ordinary link</a>`) {
		t.Errorf("an ordinary link to the same href was rewritten; got %q", got)
	}
	if !strings.Contains(got, `class="lang-flag" href="/pt/sobre/"`) {
		t.Errorf("the lang-flag anchor was not rewritten; got %q", got)
	}
}

// ── writeRedirectStub ───────────────────────────────────────────────────────

func TestWriteRedirectStub_WritesAllThreeRedirectMechanisms(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "en", "blog", "only-in-pt")
	target := "https://example.com/pt/blog/so-em-pt/"

	if err := writeRedirectStub(outDir, target); err != nil {
		t.Fatalf("writeRedirectStub: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(outDir, "index.html"))
	if err != nil {
		t.Fatalf("reading stub: %v", err)
	}
	html := string(raw)

	for _, want := range []string{
		`<link rel="canonical" href="` + target + `">`,
		`<meta http-equiv="refresh" content="0;url=` + target + `">`,
		`window.location.replace("` + target + `")`,
		`<a href="` + target + `">`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("stub missing %q; got:\n%s", want, html)
		}
	}
}

// The stub exists so a URL resolves at all — it must create the whole path.
func TestWriteRedirectStub_CreatesMissingParentDirectories(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "deeply", "nested", "path")

	if err := writeRedirectStub(outDir, "https://example.com/"); err != nil {
		t.Fatalf("writeRedirectStub: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "index.html")); err != nil {
		t.Fatalf("stub not written: %v", err)
	}
}

// A canonical pointing at the stub itself would tell search engines to index
// the redirect page rather than the real content.
func TestWriteRedirectStub_CanonicalPointsAtTheTargetNotTheStub(t *testing.T) {
	outDir := t.TempDir()
	target := "https://example.com/pt/real/"

	if err := writeRedirectStub(outDir, target); err != nil {
		t.Fatalf("writeRedirectStub: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(outDir, "index.html"))
	if err != nil {
		t.Fatalf("reading stub: %v", err)
	}
	if !strings.Contains(string(raw), `rel="canonical" href="`+target+`"`) {
		t.Errorf("canonical does not point at the target:\n%s", raw)
	}
}

func TestWriteRedirectStub_ErrorsWhenTheDirectoryCannotBeCreated(t *testing.T) {
	// A file where a directory needs to be makes MkdirAll fail.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := writeRedirectStub(filepath.Join(blocker, "child"), "https://example.com/")
	if err == nil {
		t.Fatal("expected an error when the stub directory cannot be created")
	}
	if !strings.Contains(err.Error(), "creating redirect stub dir") {
		t.Errorf("err = %v, want it to name what it was trying to do", err)
	}
}

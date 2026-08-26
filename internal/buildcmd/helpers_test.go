package buildcmd

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/courses"
	"ffreis-website-compiler/internal/posts"
	"ffreis-website-compiler/internal/testutil"
)

// ── extensionFromContentType ────────────────────────────────────────────────

// Mirrored assets are saved under an extension inferred from the response's
// Content-Type. Getting it wrong means a CSS file saved as .bin, which the
// browser then refuses to apply.
func TestExtensionFromContentType(t *testing.T) {
	for _, tc := range []struct {
		name        string
		contentType string
		want        string
	}{
		{"css", "text/css", ".css"},
		{"css with charset parameter", "text/css; charset=utf-8", ".css"},
		{"css with surrounding whitespace", "  text/css  ", ".css"},
		{"javascript", "application/javascript", ".js"},
		{"legacy javascript", "text/javascript", ".js"},
		{"woff2", "font/woff2", ".woff2"},
		{"woff", "font/woff", ".woff"},
		{"svg", "image/svg+xml", ".svg"},
		{"empty yields nothing", "", ""},
		{"parameters only yields nothing", "; charset=utf-8", ""},
		{"unknown type yields nothing", "application/x-made-up", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := extensionFromContentType(tc.contentType); got != tc.want {
				t.Errorf("extensionFromContentType(%q) = %q, want %q", tc.contentType, got, tc.want)
			}
		})
	}
}

// The system mime table may map a type to several extensions; whichever is
// chosen must at least be a usable dotted extension.
func TestExtensionFromContentType_ReturnsADottedExtensionOrNothing(t *testing.T) {
	for _, contentType := range []string{"text/css", "font/ttf", "image/x-icon", "image/png"} {
		got := extensionFromContentType(contentType)
		if got != "" && !strings.HasPrefix(got, ".") {
			t.Errorf("extensionFromContentType(%q) = %q, want a leading dot", contentType, got)
		}
	}
}

// ── resolveSchemeRelativeURL ────────────────────────────────────────────────

func TestResolveSchemeRelativeURL_InheritsTheBaseScheme(t *testing.T) {
	for _, tc := range []struct {
		name string
		base string
		want string
	}{
		{"https base", "https://example.com/page", "https://cdn.test/a.css"},
		{"http base", "http://example.com/page", "http://cdn.test/a.css"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, err := url.Parse(tc.base)
			if err != nil {
				t.Fatalf("parsing base: %v", err)
			}
			if got := resolveSchemeRelativeURL(base, "//cdn.test/a.css"); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Defaulting to https rather than http matters: a mirrored fetch over http
// could be intercepted, and the page that references it is served over https.
func TestResolveSchemeRelativeURL_DefaultsToHTTPS(t *testing.T) {
	for _, tc := range []struct {
		name string
		base *url.URL
	}{
		{"nil base", nil},
		{"base with no scheme", &url.URL{Host: "example.com"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveSchemeRelativeURL(tc.base, "//cdn.test/a.css"); got != "https://cdn.test/a.css" {
				t.Errorf("got %q, want an https URL", got)
			}
		})
	}
}

// ── hintedExtFromPreload ────────────────────────────────────────────────────

// A <link rel="preload"> carries no filename, so the extension has to come from
// its `as` attribute.
func TestHintedExtFromPreload(t *testing.T) {
	for _, tc := range []struct {
		name string
		tag  string
		want string
	}{
		{"stylesheet preload", `<link rel="preload" as="style" href="//x/a">`, ".css"},
		{"script preload", `<link rel="preload" as="script" href="//x/a">`, ".js"},
		{"font preload defaults to woff2", `<link rel="preload" as="font" href="//x/a">`, ".woff2"},
		{"font preload honours an explicit type", `<link rel="preload" as="font" type="font/woff" href="//x/a">`, ".woff"},
		{"font preload with an unknown type yields nothing", `<link rel="preload" as="font" type="font/made-up" href="//x/a">`, ""},
		{"image preload yields nothing", `<link rel="preload" as="image" href="//x/a">`, ""},
		{"uppercase as attribute", `<link rel="preload" as="STYLE" href="//x/a">`, ".css"},
		{"no as attribute", `<link rel="preload" href="//x/a">`, ""},
		{"unknown as value", `<link rel="preload" as="worker" href="//x/a">`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hintedExtFromPreload(tc.tag); got != tc.want {
				t.Errorf("hintedExtFromPreload(%q) = %q, want %q", tc.tag, got, tc.want)
			}
		})
	}
}

// ── shortHash ───────────────────────────────────────────────────────────────

func TestShortHash_IsDeterministicAndDistinguishing(t *testing.T) {
	a := shortHash("https://cdn.test/a.css")
	b := shortHash("https://cdn.test/b.css")

	if a != shortHash("https://cdn.test/a.css") {
		t.Error("shortHash is not deterministic; mirrored filenames would churn every build")
	}
	if a == b {
		t.Error("distinct URLs produced the same hash")
	}
	if len(a) != 12 {
		t.Errorf("hash length = %d, want 12 hex characters (6 bytes)", len(a))
	}
	if strings.Trim(a, "0123456789abcdef") != "" {
		t.Errorf("hash %q contains non-hex characters; it is used as a filename", a)
	}
}

func TestShortHash_HandlesAnEmptyString(t *testing.T) {
	if got := shortHash(""); len(got) != 12 {
		t.Errorf("shortHash(\"\") = %q, want a 12-character hash", got)
	}
}

// ── injectCoursesHomeCarousel ───────────────────────────────────────────────

func courseList(n int) []courses.Course {
	out := make([]courses.Course, n)
	for i := range out {
		out[i] = courses.Course{Title: string(rune('A' + i)), Description: "d", Order: i + 1}
	}
	return out
}

func TestInjectCoursesHomeCarousel_LimitsToThePreviewCount(t *testing.T) {
	siteData := map[string]any{"pages": map[string]any{"index": map[string]any{}}}

	injectCoursesHomeCarousel(siteData, courseList(5), 3)

	index := siteData["pages"].(map[string]any)["index"].(map[string]any)
	items, ok := index["courses_carousel_items"].([]any)
	if !ok {
		t.Fatalf("courses_carousel_items = %T, want []any", index["courses_carousel_items"])
	}
	if len(items) != 3 {
		t.Errorf("got %d carousel items, want 3", len(items))
	}
}

func TestInjectCoursesHomeCarousel_KeepsEverythingWhenUnderTheLimit(t *testing.T) {
	for _, n := range []int{0, -1, 10} {
		siteData := map[string]any{"pages": map[string]any{"index": map[string]any{}}}
		injectCoursesHomeCarousel(siteData, courseList(2), n)

		index := siteData["pages"].(map[string]any)["index"].(map[string]any)
		if items := index["courses_carousel_items"].([]any); len(items) != 2 {
			t.Errorf("n=%d gave %d items, want both", n, len(items))
		}
	}
}

func TestInjectCoursesHomeCarousel_CreatesTheIndexPageDataWhenAbsent(t *testing.T) {
	siteData := map[string]any{"pages": map[string]any{}}

	injectCoursesHomeCarousel(siteData, courseList(1), 5)

	index, ok := siteData["pages"].(map[string]any)["index"].(map[string]any)
	if !ok {
		t.Fatal("pages.index was not created")
	}
	if _, present := index["courses_carousel_items"]; !present {
		t.Error("carousel items were not injected into the created index data")
	}
}

// Site data without a pages map is not an error — the site simply has no home
// page block to fill.
func TestInjectCoursesHomeCarousel_NoOpWithoutAPagesMap(t *testing.T) {
	siteData := map[string]any{}

	injectCoursesHomeCarousel(siteData, courseList(1), 5)

	if _, present := siteData["pages"]; present {
		t.Error("a pages map was invented; want a silent no-op")
	}
}

// ── writePostLangStub ───────────────────────────────────────────────────────

func stubSiteData() map[string]any {
	return map[string]any{
		"language_default": "en",
		"language_variants": []any{
			map[string]any{"hreflang": "en", "path": "/en"},
			map[string]any{"hreflang": "pt", "path": "/pt"},
		},
	}
}

func TestWritePostLangStub_RedirectsToALanguageThatHasThePost(t *testing.T) {
	outDir := t.TempDir()
	post := posts.Post{Meta: posts.PostMeta{Slug: "only-in-pt", AvailableLanguages: []string{"pt"}}}

	err := writePostLangStub(testutil.DiscardLogger(), buildOptions{outDir: outDir}, post, stubSiteData(), "en")
	if err != nil {
		t.Fatalf("writePostLangStub: %v", err)
	}

	raw, readErr := os.ReadFile(filepath.Join(outDir, "blog", "only-in-pt", "index.html"))
	if readErr != nil {
		t.Fatalf("stub not written: %v", readErr)
	}
	if !strings.Contains(string(raw), "/pt/blog/only-in-pt/") {
		t.Errorf("stub does not point at the PT post:\n%s", raw)
	}
}

// A stub with no usable target must still resolve to something — a dead URL is
// a CloudFront error page for the visitor.
func TestWritePostLangStub_FallsBackToTheLanguageHomeWithNoUsableTarget(t *testing.T) {
	outDir := t.TempDir()
	post := posts.Post{Meta: posts.PostMeta{Slug: "orphan", AvailableLanguages: []string{"de"}}}

	err := writePostLangStub(testutil.DiscardLogger(), buildOptions{outDir: outDir}, post, stubSiteData(), "en")
	if err != nil {
		t.Fatalf("writePostLangStub: %v", err)
	}

	raw, readErr := os.ReadFile(filepath.Join(outDir, "blog", "orphan", "index.html"))
	if readErr != nil {
		t.Fatalf("stub not written: %v", readErr)
	}
	if !strings.Contains(string(raw), `href="/en/"`) {
		t.Errorf("stub should fall back to the current-language home:\n%s", raw)
	}
}

func TestWritePostLangStub_ReportsAnUnwritableOutputDirectory(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	post := posts.Post{Meta: posts.PostMeta{Slug: "s", AvailableLanguages: []string{"pt"}}}

	err := writePostLangStub(testutil.DiscardLogger(), buildOptions{outDir: blocker}, post, stubSiteData(), "en")
	if err == nil || !strings.Contains(err.Error(), "writing redirect stub for post") {
		t.Fatalf("err = %v, want the failing post named", err)
	}
}

package buildcmd

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ── fixtures ────────────────────────────────────────────────────────────────

// hreflangSite returns site data describing an EN-default site with a PT
// variant, including a slug map so the PT page has a translated URL.
func hreflangSite() map[string]any {
	return map[string]any{
		"base_url":         "https://example.com/",
		"html_lang":        "en",
		"language_default": "en",
		"language_variants": []any{
			map[string]any{"hreflang": "en", "path": "/en"},
			map[string]any{"hreflang": "pt", "path": "/pt"},
		},
		"ui": map[string]any{
			"nav": map[string]any{
				"lang_links": []any{
					map[string]any{
						"lang":     "pt",
						"href":     "/pt/",
						"active":   false,
						"slug_map": map[string]any{"about": "sobre", "contact": "contato"},
					},
					map[string]any{"lang": "en", "href": "/en/", "active": true},
				},
			},
		},
	}
}

var alternateRE = regexp.MustCompile(`<link rel="alternate" hreflang="([^"]+)" href="([^"]+)">`)

// alternates parses the emitted links into hreflang → href.
func alternates(html string) map[string]string {
	out := map[string]string{}
	for _, m := range alternateRE.FindAllStringSubmatch(html, -1) {
		out[m[1]] = m[2]
	}
	return out
}

const headHTML = `<html><head><title>t</title></head><body></body></html>`

// ── injectHreflangAlternates ────────────────────────────────────────────────

func TestInjectHreflangAlternates_EmitsOneLinkPerVariantPlusXDefault(t *testing.T) {
	got := alternates(injectHreflangAlternates(headHTML, hreflangSite(), "about", true))

	want := map[string]string{
		"en":        "https://example.com/en/about/",
		"pt":        "https://example.com/pt/sobre/",
		"x-default": "https://example.com/en/about/",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d alternates %v, want %d", len(got), got, len(want))
	}
	for lang, href := range want {
		if got[lang] != href {
			t.Errorf("hreflang %q = %q, want %q", lang, got[lang], href)
		}
	}
}

// The current page's own language uses the slug already resolved for it, not a
// slug-map lookup — otherwise a missing self-entry would rename the page.
func TestInjectHreflangAlternates_CurrentLanguageUsesTheCurrentSlug(t *testing.T) {
	site := hreflangSite()
	// The EN lang_links entry has no slug_map at all.
	got := alternates(injectHreflangAlternates(headHTML, site, "contact", true))

	if got["en"] != "https://example.com/en/contact/" {
		t.Errorf("en = %q, want the current page's own slug", got["en"])
	}
	if got["pt"] != "https://example.com/pt/contato/" {
		t.Errorf("pt = %q, want the translated slug", got["pt"])
	}
}

func TestInjectHreflangAlternates_FallsBackToThePageNameWhenNotInTheSlugMap(t *testing.T) {
	got := alternates(injectHreflangAlternates(headHTML, hreflangSite(), "pricing", true))

	if got["pt"] != "https://example.com/pt/pricing/" {
		t.Errorf("pt = %q, want the untranslated page name as the slug", got["pt"])
	}
}

func TestInjectHreflangAlternates_NonCleanURLsGetAnHTMLExtension(t *testing.T) {
	got := alternates(injectHreflangAlternates(headHTML, hreflangSite(), "about", false))

	if got["en"] != "https://example.com/en/about.html" {
		t.Errorf("en = %q, want a .html URL", got["en"])
	}
	if got["pt"] != "https://example.com/pt/sobre.html" {
		t.Errorf("pt = %q, want a .html URL", got["pt"])
	}
}

// The index page is the language root, so it must not gain an "/index/" segment.
func TestInjectHreflangAlternates_IndexResolvesToTheLanguageRoot(t *testing.T) {
	for _, cleanURLs := range []bool{true, false} {
		got := alternates(injectHreflangAlternates(headHTML, hreflangSite(), "index", cleanURLs))
		if got["en"] != "https://example.com/en/" {
			t.Errorf("cleanURLs=%v: en = %q, want the bare language root", cleanURLs, got["en"])
		}
	}
}

func TestInjectHreflangAlternates_LinksLandInsideTheHead(t *testing.T) {
	out := injectHreflangAlternates(headHTML, hreflangSite(), "about", true)

	headEnd := strings.Index(out, "</head>")
	firstLink := strings.Index(out, `<link rel="alternate"`)
	if firstLink == -1 || headEnd == -1 || firstLink > headEnd {
		t.Errorf("alternates must be injected before </head>; got:\n%s", out)
	}
}

func TestInjectHreflangAlternates_ReturnsHTMLUnchangedWhenThereIsNothingToEmit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"no base_url", func(s map[string]any) { delete(s, "base_url") }},
		{"empty base_url", func(s map[string]any) { s["base_url"] = "" }},
		{"no language_variants", func(s map[string]any) { delete(s, "language_variants") }},
		{"language_variants is not a list", func(s map[string]any) { s["language_variants"] = "en,pt" }},
		{"language_variants is empty", func(s map[string]any) { s["language_variants"] = []any{} }},
		{"variants missing hreflang or path", func(s map[string]any) {
			s["language_variants"] = []any{
				map[string]any{"path": "/en"},
				map[string]any{"hreflang": "pt"},
				"not-a-map",
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			site := hreflangSite()
			tc.mutate(site)
			if got := injectHreflangAlternates(headHTML, site, "about", true); got != headHTML {
				t.Errorf("html was modified; want it returned untouched:\n%s", got)
			}
		})
	}
}

// A default that matches no variant means no x-default link — better than
// pointing it at an arbitrary language.
func TestInjectHreflangAlternates_NoXDefaultWhenTheDefaultMatchesNoVariant(t *testing.T) {
	site := hreflangSite()
	site["language_default"] = "de"

	got := alternates(injectHreflangAlternates(headHTML, site, "about", true))
	if _, present := got["x-default"]; present {
		t.Errorf("x-default = %q, want none when language_default matches no variant", got["x-default"])
	}
	if len(got) != 2 {
		t.Errorf("got %v, want the two real variants to still be emitted", got)
	}
}

func TestInjectHreflangAlternates_TrailingSlashOnBaseURLIsNotDoubled(t *testing.T) {
	site := hreflangSite()
	site["base_url"] = "https://example.com///"

	got := alternates(injectHreflangAlternates(headHTML, site, "about", true))
	if strings.Contains(got["en"], "com//") {
		t.Errorf("en = %q, want no doubled slash after the host", got["en"])
	}
}

// ── buildAlternateURL ───────────────────────────────────────────────────────

func TestBuildAlternateURL(t *testing.T) {
	for _, tc := range []struct {
		name      string
		slug      string
		cleanURLs bool
		want      string
	}{
		{"clean url", "about", true, "https://x.test/en/about/"},
		{"extension url", "about", false, "https://x.test/en/about.html"},
		{"index is the language root, clean", "index", true, "https://x.test/en/"},
		{"index is the language root, extension", "index", false, "https://x.test/en/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildAlternateURL("https://x.test", "/en", tc.slug, tc.cleanURLs); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// ── buildLangLinksSlugMap ───────────────────────────────────────────────────

func TestBuildLangLinksSlugMap_IndexesSlugsByLanguage(t *testing.T) {
	maps := buildLangLinksSlugMap(hreflangSite())

	if len(maps) != 1 {
		t.Fatalf("got %d language maps %v, want only the one with a slug_map", len(maps), maps)
	}
	if maps["pt"]["about"] != "sobre" {
		t.Errorf("pt/about = %q, want sobre", maps["pt"]["about"])
	}
}

func TestBuildLangLinksSlugMap_SkipsUnusableEntries(t *testing.T) {
	site := map[string]any{
		"ui": map[string]any{"nav": map[string]any{"lang_links": []any{
			map[string]any{"slug_map": map[string]any{"a": "b"}},                // no lang
			map[string]any{"lang": "de"},                                        // no slug_map
			map[string]any{"lang": "es", "slug_map": map[string]any{}},          // empty slug_map
			map[string]any{"lang": "fr", "slug_map": map[string]any{"a": 42}},   // non-string slug
			map[string]any{"lang": "it", "slug_map": map[string]any{"a": "aa"}}, // usable
			"not-a-map",
		}}},
	}

	maps := buildLangLinksSlugMap(site)

	var langs []string
	for lang := range maps {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	if len(langs) != 2 || langs[0] != "fr" || langs[1] != "it" {
		t.Fatalf("languages = %v, want [fr it] (entries with a non-empty slug_map)", langs)
	}
	if len(maps["fr"]) != 0 {
		t.Errorf("fr map = %v, want the non-string slug dropped", maps["fr"])
	}
	if maps["it"]["a"] != "aa" {
		t.Errorf("it/a = %q, want aa", maps["it"]["a"])
	}
}

func TestBuildLangLinksSlugMap_EmptyWhenNavIsAbsent(t *testing.T) {
	if maps := buildLangLinksSlugMap(map[string]any{}); len(maps) != 0 {
		t.Errorf("got %v, want an empty map", maps)
	}
}

// ── resolveSlugForLang / variantSlug ────────────────────────────────────────

func TestResolveSlugForLang_FallsBackToThePageName(t *testing.T) {
	maps := map[string]map[string]string{"pt": {"about": "sobre", "empty": ""}}

	for _, tc := range []struct{ name, lang, page, want string }{
		{"translated slug", "pt", "about", "sobre"},
		{"page absent from the map", "pt", "pricing", "pricing"},
		{"language absent entirely", "de", "about", "about"},
		{"empty slug is ignored", "pt", "empty", "empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveSlugForLang(maps, tc.lang, tc.page); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestVariantSlug_UsesTheCurrentSlugForTheCurrentLanguage(t *testing.T) {
	maps := map[string]map[string]string{
		"en": {"about": "should-not-be-used"},
		"pt": {"about": "sobre"},
	}

	if got := variantSlug("en", "en", "resolved-current", maps, "about"); got != "resolved-current" {
		t.Errorf("current language slug = %q, want the already-resolved current slug", got)
	}
	if got := variantSlug("pt", "en", "resolved-current", maps, "about"); got != "sobre" {
		t.Errorf("other language slug = %q, want the slug-map lookup", got)
	}
}

// ── extractLangVariants ─────────────────────────────────────────────────────

func TestExtractLangVariants_ReturnsVariantsAndResolvesTheDefault(t *testing.T) {
	variants, defaultHreflang := extractLangVariants(hreflangSite())

	if len(variants) != 2 {
		t.Fatalf("got %d variants %v, want 2", len(variants), variants)
	}
	if variants[0].hreflang != "en" || variants[0].path != "/en" {
		t.Errorf("first variant = %+v, want en at /en", variants[0])
	}
	if defaultHreflang != "en" {
		t.Errorf("default hreflang = %q, want en (language_default matched by path prefix)", defaultHreflang)
	}
}

func TestExtractLangVariants_IgnoresMalformedEntries(t *testing.T) {
	site := map[string]any{
		"language_default": "pt",
		"language_variants": []any{
			"not-a-map",
			map[string]any{"hreflang": "", "path": "/en"},
			map[string]any{"hreflang": "pt", "path": ""},
			map[string]any{"hreflang": "pt-BR", "path": "/pt"},
		},
	}

	variants, defaultHreflang := extractLangVariants(site)

	if len(variants) != 1 || variants[0].hreflang != "pt-BR" {
		t.Fatalf("variants = %+v, want only the complete entry", variants)
	}
	// The default is matched on the URL prefix, so "pt" resolves to pt-BR.
	if defaultHreflang != "pt-BR" {
		t.Errorf("default hreflang = %q, want pt-BR matched via its /pt path", defaultHreflang)
	}
}

func TestExtractLangVariants_AbsentKeyYieldsNothing(t *testing.T) {
	variants, defaultHreflang := extractLangVariants(map[string]any{})
	if variants != nil || defaultHreflang != "" {
		t.Errorf("got (%v, %q), want (nil, \"\")", variants, defaultHreflang)
	}
}

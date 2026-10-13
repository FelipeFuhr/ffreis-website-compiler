package buildcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPruneCSSForPage_disabled_returnsUnchanged(t *testing.T) {
	css := `.foo { color: red; }`
	got := pruneCSSForPage(css, `<div></div>`, "", cssPruneOptions{enabled: false})
	if got != css {
		t.Errorf("disabled prune should return css unchanged, got %q", got)
	}
}

func TestPruneCSSForPage_dropsRuleNotPresentOnPage(t *testing.T) {
	css := `.used { color: red; } .unused { color: blue; }`
	html := `<div class="used"></div>`
	got := pruneCSSForPage(css, html, "", cssPruneOptions{enabled: true})
	if !strings.Contains(got, ".used") {
		t.Errorf("expected .used to survive, got %q", got)
	}
	if strings.Contains(got, ".unused") {
		t.Errorf("expected .unused to be pruned, got %q", got)
	}
}

func TestPruneCSSForPage_keepsRuleViaSafelist(t *testing.T) {
	css := `.hh-modal.active { display: flex; }`
	html := `<div class="hh-modal"></div>` // .active only ever added by JS
	opts := cssPruneOptions{enabled: true, safelist: []string{".active"}}
	got := pruneCSSForPage(css, html, "", opts)
	if !strings.Contains(got, ".active") {
		t.Errorf("safelisted selector should survive, got %q", got)
	}
}

func TestPruneCSSForPage_dropsDynamicClassWithoutSafelist(t *testing.T) {
	css := `.hh-modal.active { display: flex; }`
	html := `<div class="hh-modal"></div>`
	got := pruneCSSForPage(css, html, "", cssPruneOptions{enabled: true}) // no safelist
	if strings.Contains(got, ".active") {
		t.Errorf("without a safelist entry, a selector requiring an absent class should be pruned, got %q", got)
	}
}

func TestPruneCSSForPage_keepsRootHtmlBodyUniversal(t *testing.T) {
	css := `:root { --x: 1; } html.no-js { display: none; } body { margin: 0; } * { box-sizing: border-box; }`
	html := `<div></div>`
	got := pruneCSSForPage(css, html, "", cssPruneOptions{enabled: true})
	for _, want := range []string{":root", "html.no-js", "body {", "* {"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q to always survive, got %q", want, got)
		}
	}
}

func TestPruneCSSForPage_keepsKeyframesAndFontFaceVerbatim(t *testing.T) {
	css := `@keyframes spin { 0% { transform: rotate(0); } 100% { transform: rotate(360deg); } } @font-face { font-family: "X"; src: url("x.woff2"); }`
	html := `<div></div>` // nothing in the page references .spin or anything
	got := pruneCSSForPage(css, html, "", cssPruneOptions{enabled: true})
	if !strings.Contains(got, "@keyframes spin") {
		t.Errorf("expected @keyframes to survive verbatim, got %q", got)
	}
	if !strings.Contains(got, "@font-face") {
		t.Errorf("expected @font-face to survive verbatim, got %q", got)
	}
}

func TestPruneCSSForPage_mediaQuery_prunesInnerRulesIndependently(t *testing.T) {
	css := `@media (max-width: 600px) { .used { color: red; } .unused { color: blue; } }`
	html := `<div class="used"></div>`
	got := pruneCSSForPage(css, html, "", cssPruneOptions{enabled: true})
	if !strings.Contains(got, "@media") || !strings.Contains(got, ".used") {
		t.Errorf("expected @media wrapper with .used to survive, got %q", got)
	}
	if strings.Contains(got, ".unused") {
		t.Errorf("expected .unused inside @media to be pruned, got %q", got)
	}
}

func TestPruneCSSForPage_mediaQuery_droppedEntirelyWhenAllInnerRulesPruned(t *testing.T) {
	css := `@media (max-width: 600px) { .unused { color: blue; } }`
	html := `<div></div>`
	got := pruneCSSForPage(css, html, "", cssPruneOptions{enabled: true})
	if strings.Contains(got, "@media") {
		t.Errorf("expected the whole @media block to be dropped when nothing inside survives, got %q", got)
	}
}

func TestPruneCSSForPage_tagSelectorsAndAttributeSelectors(t *testing.T) {
	css := `button { color: red; } a { color: blue; } [hidden] { display: none; } [data-tone] { color: green; }`
	html := `<button>x</button><div hidden></div>`
	got := pruneCSSForPage(css, html, "", cssPruneOptions{enabled: true})
	if !strings.Contains(got, "button {") {
		t.Errorf("expected button selector (tag present) to survive, got %q", got)
	}
	if strings.Contains(got, "a {") {
		t.Errorf("expected a selector (tag absent) to be pruned, got %q", got)
	}
	if !strings.Contains(got, "[hidden]") {
		t.Errorf("expected [hidden] (attribute present) to survive, got %q", got)
	}
	if strings.Contains(got, "[data-tone]") {
		t.Errorf("expected [data-tone] (attribute absent) to be pruned, got %q", got)
	}
}

func TestPruneCSSForPage_pseudoClassesIgnoredForMatching(t *testing.T) {
	css := `.hh-nav a:hover { color: gold; }`
	html := `<nav class="hh-nav"><a href="#">x</a></nav>`
	got := pruneCSSForPage(css, html, "", cssPruneOptions{enabled: true})
	if !strings.Contains(got, ":hover") {
		t.Errorf("expected :hover rule to survive since .hh-nav and a both exist, got %q", got)
	}
}

func TestPruneCSSForPage_bareWholeSelectorPseudo_keptSafe(t *testing.T) {
	// A selector that's nothing but a pseudo-class once stripped has no
	// extractable tokens at all — must err toward keeping it.
	css := `:hover { outline: none; }`
	html := `<div></div>`
	got := pruneCSSForPage(css, html, "", cssPruneOptions{enabled: true})
	if !strings.Contains(got, ":hover") {
		t.Errorf("expected bare pseudo-class selector to be kept (no tokens to judge), got %q", got)
	}
}

func TestPruneCSSForPage_commentsAndStringsDontConfuseBraceCounting(t *testing.T) {
	css := `.used { content: "a { b } c"; } /* comment with { and } chars */ .also-used::after { content: "}"; }`
	html := `<div class="used also-used"></div>`
	got := pruneCSSForPage(css, html, "", cssPruneOptions{enabled: true})
	if !strings.Contains(got, `content: "a { b } c"`) {
		t.Errorf("expected string literal with braces to survive intact, got %q", got)
	}
	if !strings.Contains(got, `content: "}"`) {
		t.Errorf("expected string literal with a closing brace to survive intact, got %q", got)
	}
}

func TestPruneCSSForPage_commaSeparatedSelectorList_keepsOnlyMatchingParts(t *testing.T) {
	css := `.used, .unused { color: red; }`
	html := `<div class="used"></div>`
	got := pruneCSSForPage(css, html, "", cssPruneOptions{enabled: true})
	if !strings.Contains(got, ".used") {
		t.Errorf("expected .used to survive, got %q", got)
	}
	if strings.Contains(got, ".unused") {
		t.Errorf("expected .unused to be dropped from the selector list, got %q", got)
	}
}

func TestPruneCSSForPage_notPseudoWithCommaArgs_notSplitAsSelectorList(t *testing.T) {
	// :not(.a, .b) must not be split into two top-level selectors on the
	// comma inside the parens.
	css := `.used:not(.a, .b) { color: red; }`
	html := `<div class="used"></div>`
	got := pruneCSSForPage(css, html, "", cssPruneOptions{enabled: true})
	if !strings.Contains(got, ":not(.a, .b)") {
		t.Errorf("expected :not(...) with internal comma to survive intact as one selector, got %q", got)
	}
}

func TestExtractHTMLTokens(t *testing.T) {
	html := `<div class="foo bar" id="baz" hidden data-tone="hot"><span>x</span></div>`
	tokens := extractHTMLTokens(html)
	for _, c := range []string{"foo", "bar"} {
		if !tokens.classes[c] {
			t.Errorf("expected class %q to be extracted", c)
		}
	}
	if !tokens.ids["baz"] {
		t.Error("expected id baz to be extracted")
	}
	if !tokens.tags["div"] || !tokens.tags["span"] {
		t.Error("expected tags div and span to be extracted")
	}
	if !tokens.attrs["hidden"] || !tokens.attrs["data-tone"] {
		t.Error("expected attrs hidden and data-tone to be extracted")
	}
}

func TestSplitSelectorList_respectsParenDepth(t *testing.T) {
	got := splitSelectorList(".a:not(.b, .c), .d")
	want := []string{".a:not(.b, .c)", ".d"}
	if len(got) != len(want) {
		t.Fatalf("got %d parts, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("part %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPruneCSSForPage_keepsClassOnlyPresentInInlineScript(t *testing.T) {
	css := `.card { color: red; }`
	html := `<div id="grid"></div><script>document.getElementById('grid').innerHTML = '<article class="card">x</article>';</script>`
	got := pruneCSSForPage(css, html, "", cssPruneOptions{enabled: true})
	if !strings.Contains(got, ".card") {
		t.Errorf("expected .card to survive via inline <script> scan, got %q", got)
	}
}

func TestPruneCSSForPage_keepsClassOnlyPresentInExternalScriptFile(t *testing.T) {
	dir := t.TempDir()
	jsPath := filepath.Join(dir, "render.js")
	if err := os.WriteFile(jsPath, []byte(`root.innerHTML = games.map(g => `+"`"+`<article class="hh-game-card" data-tone="${g.tone}"><span class="hh-status">x</span></article>`+"`"+`).join('');`), 0o644); err != nil {
		t.Fatal(err)
	}
	css := `.hh-game-card { color: red; } .hh-status[data-tone] { color: blue; } .never-used { color: green; }`
	html := `<div id="grid"></div><script src="/render.js"></script>`
	got := pruneCSSForPage(css, html, dir, cssPruneOptions{enabled: true})
	if !strings.Contains(got, ".hh-game-card") {
		t.Errorf("expected .hh-game-card to survive via external script scan, got %q", got)
	}
	if !strings.Contains(got, ".hh-status[data-tone]") {
		t.Errorf("expected .hh-status[data-tone] to survive via external script scan (hyphenated attr), got %q", got)
	}
	if strings.Contains(got, ".never-used") {
		t.Errorf("expected .never-used (absent from HTML and JS) to still be pruned, got %q", got)
	}
}

func TestPruneCSSForPage_externalScriptFileMissing_doesNotError(t *testing.T) {
	css := `.foo { color: red; }`
	html := `<script src="/does-not-exist.js"></script>`
	got := pruneCSSForPage(css, html, t.TempDir(), cssPruneOptions{enabled: true})
	if strings.Contains(got, ".foo") {
		t.Errorf("expected .foo to be pruned when the referenced script can't be read (no crash either), got %q", got)
	}
}

func TestPruneCSSForPage_externalScript_skipsRemoteSrc(t *testing.T) {
	css := `.foo { color: red; }`
	html := `<script src="https://cdn.example.com/lib.js"></script>`
	got := pruneCSSForPage(css, html, t.TempDir(), cssPruneOptions{enabled: true})
	if strings.Contains(got, ".foo") {
		t.Errorf("expected .foo to be pruned — remote scripts aren't fetched/scanned, got %q", got)
	}
}

func TestPruneCSSForPage_keepsTagOnlyPresentInScript(t *testing.T) {
	css := `textarea { border: none; }`
	html := `<div id="root"></div><script>root.innerHTML = '<textarea name="x"></textarea>';</script>`
	got := pruneCSSForPage(css, html, "", cssPruneOptions{enabled: true})
	if !strings.Contains(got, "textarea {") {
		t.Errorf("expected bare tag selector to survive via script tag-name scan, got %q", got)
	}
}

func TestPruneCSSForPage_keepsClassEmbeddedInTernaryExpression(t *testing.T) {
	css := `.fallback { color: gray; }`
	html := `<div id="root"></div><script>root.innerHTML = '<div class="logo ' + (hasLogo ? '' : 'fallback') + '">x</div>';</script>`
	got := pruneCSSForPage(css, html, "", cssPruneOptions{enabled: true})
	if !strings.Contains(got, ".fallback") {
		t.Errorf("expected .fallback to survive via quoted-identifier fallback extraction, got %q", got)
	}
}

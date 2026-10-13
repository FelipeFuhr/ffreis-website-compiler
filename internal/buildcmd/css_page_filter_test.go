package buildcmd

import (
	"strings"
	"testing"
)

// ── collectUsedTokens ──────────────────────────────────────────────────────

func TestCollectUsedTokens_ExtractsClassesIDsAndTags(t *testing.T) {
	html := `<html><body><div class="card featured" id="main"><nav></nav></div></body></html>`
	got := collectUsedTokens(html)
	for _, want := range []string{".card", ".featured", "#main", "div", "nav", "body", "html"} {
		if !got[want] {
			t.Errorf("expected token %q to be collected, got set: %v", want, got)
		}
	}
	if got[".nope"] {
		t.Error("did not expect an unreferenced class to be present")
	}
}

// ── filterCSSForPageUsage: basic keep/drop ─────────────────────────────────

func TestFilterCSSForPageUsage_DropsRuleForAbsentClass(t *testing.T) {
	css := `.pricing-card{color:red}`
	html := `<div class="hero"></div>`
	got := filterCSSForPageUsage(css, html)
	if strings.Contains(got, "pricing-card") {
		t.Errorf("expected .pricing-card rule dropped, got: %q", got)
	}
}

func TestFilterCSSForPageUsage_KeepsRuleForPresentClass(t *testing.T) {
	css := `.hero{color:red}`
	html := `<div class="hero"></div>`
	got := filterCSSForPageUsage(css, html)
	if !strings.Contains(got, ".hero") || !strings.Contains(got, "color:red") {
		t.Errorf("expected .hero rule kept intact, got: %q", got)
	}
}

func TestFilterCSSForPageUsage_RequiresAllTokensInACompoundOrDescendantSelector(t *testing.T) {
	// ".a .b" (descendant combinator) needs BOTH tokens present anywhere on the
	// page before it's considered reachable at all; the page here has ".b" but
	// never ".a", so the rule can never match on this page.
	css := `.a .b{color:red}`
	html := `<div class="b"></div>`
	got := filterCSSForPageUsage(css, html)
	if strings.Contains(got, "color:red") {
		t.Errorf("expected rule dropped when one of two required tokens is absent, got: %q", got)
	}
}

func TestFilterCSSForPageUsage_KeepsRuleWhenAllCompoundTokensPresent(t *testing.T) {
	css := `.a .b{color:red}`
	html := `<div class="a"><span class="b"></span></div>`
	got := filterCSSForPageUsage(css, html)
	if !strings.Contains(got, "color:red") {
		t.Errorf("expected rule kept when both tokens present anywhere on page, got: %q", got)
	}
}

// ── comma-separated selector lists: OR semantics ───────────────────────────

func TestFilterCSSForPageUsage_KeepsRuleIfAnyCommaSeparatedSelectorMatches(t *testing.T) {
	css := `.absent-one, .present, .absent-two{color:red}`
	html := `<div class="present"></div>`
	got := filterCSSForPageUsage(css, html)
	if !strings.Contains(got, "color:red") {
		t.Errorf("expected rule kept: one of three comma-separated selectors matched, got: %q", got)
	}
}

func TestFilterCSSForPageUsage_DropsRuleIfNoCommaSeparatedSelectorMatches(t *testing.T) {
	css := `.absent-one, .absent-two{color:red}`
	html := `<div class="present"></div>`
	got := filterCSSForPageUsage(css, html)
	if strings.Contains(got, "color:red") {
		t.Errorf("expected rule dropped: no comma-separated selector matched, got: %q", got)
	}
}

// ── always-kept selectors / at-rules ────────────────────────────────────────

func TestFilterCSSForPageUsage_KeepsRootAndUniversalSelectorsUnconditionally(t *testing.T) {
	css := `:root{--brand:#000}
* {box-sizing:border-box}
html{margin:0}
body{padding:0}`
	// A realistic full-page document (every real rendered page has <html>/
	// <body> tags) with no class/id referencing anything above.
	html := `<html><body><div></div></body></html>`
	got := filterCSSForPageUsage(css, html)
	for _, want := range []string{"--brand", "box-sizing", "margin:0", "padding:0"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected foundational rule containing %q to be kept, got: %q", want, got)
		}
	}
}

func TestFilterCSSForPageUsage_KeepsFontFaceAndKeyframesWholesale(t *testing.T) {
	css := `@font-face{font-family:"X";src:url(/f.woff2)}
@keyframes spin{from{transform:rotate(0)}to{transform:rotate(360deg)}}
.unused{animation:spin 1s}`
	html := `<div class="used-only"></div>`
	got := filterCSSForPageUsage(css, html)
	if !strings.Contains(got, "@font-face") {
		t.Errorf("expected @font-face kept wholesale, got: %q", got)
	}
	if !strings.Contains(got, "@keyframes spin") || !strings.Contains(got, "rotate(360deg)") {
		t.Errorf("expected @keyframes kept wholesale, got: %q", got)
	}
	if strings.Contains(got, ".unused") {
		t.Errorf("expected .unused rule itself still dropped despite referencing the kept keyframes, got: %q", got)
	}
}

// ── @media / @supports / @container recursion ──────────────────────────────

func TestFilterCSSForPageUsage_RecursesIntoMediaQueryKeepingOnlyUsedRules(t *testing.T) {
	css := `@media (max-width: 48rem) {
  .present { display: none }
  .absent { display: block }
}`
	html := `<div class="present"></div>`
	got := filterCSSForPageUsage(css, html)
	if !strings.Contains(got, "@media") || !strings.Contains(got, ".present") {
		t.Errorf("expected @media kept with .present rule inside, got: %q", got)
	}
	if strings.Contains(got, ".absent") {
		t.Errorf("expected .absent rule dropped from inside @media, got: %q", got)
	}
}

func TestFilterCSSForPageUsage_DropsMediaQueryEntirelyWhenNothingInsideSurvives(t *testing.T) {
	css := `@media (max-width: 48rem) { .absent { display: block } }`
	html := `<div class="present"></div>`
	got := filterCSSForPageUsage(css, html)
	if strings.TrimSpace(got) != "" {
		t.Errorf("expected empty output when nothing inside @media survives filtering, got: %q", got)
	}
}

// ── deliberately-conservative gaps: attribute selectors & pseudo-class args ─

func TestFilterCSSForPageUsage_NeverDropsRuleGatedOnlyByAttributeSelector(t *testing.T) {
	css := `[data-widget]{color:red}`
	html := `<div class="unrelated"></div>`
	got := filterCSSForPageUsage(css, html)
	if !strings.Contains(got, "color:red") {
		t.Errorf("expected attribute-selector-only rule always kept (conservative gap), got: %q", got)
	}
}

func TestFilterCSSForPageUsage_NeverDropsRuleUsingNotWithAnAbsentClassArgument(t *testing.T) {
	// ".card:not(.hidden)" must not require ".hidden" to be present — :not()'s
	// argument is a negation, not a requirement.
	css := `.card:not(.hidden){color:red}`
	html := `<div class="card"></div>`
	got := filterCSSForPageUsage(css, html)
	if !strings.Contains(got, "color:red") {
		t.Errorf("expected .card:not(.hidden) kept since .card is present, got: %q", got)
	}
}

// ── pseudo-classes/elements contribute no extra requirement ────────────────

func TestFilterCSSForPageUsage_PseudoClassAloneRequiresOnlyItsBaseSelector(t *testing.T) {
	css := `.button:hover{color:blue}
.button::before{content:""}`
	html := `<button class="button"></button>`
	got := filterCSSForPageUsage(css, html)
	if !strings.Contains(got, ":hover") || !strings.Contains(got, "::before") {
		t.Errorf("expected pseudo-class/element rules kept since base class .button is present, got: %q", got)
	}
}

// ── comments and declarations are preserved verbatim on kept rules ─────────

func TestFilterCSSForPageUsage_PreservesCommentsAndDeclarationsOnKeptRules(t *testing.T) {
	css := `/* card styling */
.card { color: red; /* inline note */ background: url(/x.png) }`
	html := `<div class="card"></div>`
	got := filterCSSForPageUsage(css, html)
	if !strings.Contains(got, "/* card styling */") {
		t.Errorf("expected preceding comment preserved, got: %q", got)
	}
	if !strings.Contains(got, "url(/x.png)") {
		t.Errorf("expected url() content preserved untouched, got: %q", got)
	}
}

// ── integration: the opt-in flag through transformStylesheets ─────────────

func TestTransformStylesheets_SplitCSSPerPage_DropsUnusedComponentCSS(t *testing.T) {
	dir := makeStylesheetDir(t, map[string]string{
		"css/main.css": ".hero{color:red}.pricing-card{color:blue}",
	})
	html := `<html><head><link rel="stylesheet" href="/css/main.css"></head><body><div class="hero"></div></body></html>`
	got, err := transformStylesheets(html, dir, false, false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "color:red") {
		t.Errorf("expected .hero rule kept (class present in page), got: %q", got)
	}
	if strings.Contains(got, "color:blue") {
		t.Errorf("expected .pricing-card rule dropped (class absent from page), got: %q", got)
	}
}

func TestTransformStylesheets_SplitCSSPerPageFalse_KeepsEverything(t *testing.T) {
	dir := makeStylesheetDir(t, map[string]string{
		"css/main.css": ".hero{color:red}.pricing-card{color:blue}",
	})
	html := `<html><head><link rel="stylesheet" href="/css/main.css"></head><body><div class="hero"></div></body></html>`
	got, err := transformStylesheets(html, dir, false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "color:red") || !strings.Contains(got, "color:blue") {
		t.Errorf("expected default (flag off) behavior unchanged: both rules kept, got: %q", got)
	}
}

func TestTransformStylesheets_SplitCSSPerPage_MatchesAgainstWholePageNotJustHeadFragment(t *testing.T) {
	// The class used by .footer-only only appears in the BODY, not the head
	// fragment being transformed when the head <link> is processed. The
	// filter must still see it, since it's given the whole original doc.
	dir := makeStylesheetDir(t, map[string]string{
		"css/main.css": ".footer-only{color:green}",
	})
	html := `<html><head><link rel="stylesheet" href="/css/main.css"></head>` +
		`<body><footer class="footer-only"></footer></body></html>`
	got, err := transformStylesheets(html, dir, false, false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "color:green") {
		t.Errorf("expected .footer-only rule kept since the class appears in the body, got: %q", got)
	}
}

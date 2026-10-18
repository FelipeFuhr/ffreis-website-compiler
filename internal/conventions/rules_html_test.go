package conventions

import (
	"strings"
	"testing"
)

// builtSite runs one output rule over a dist/ tree and returns its errors. It
// is the shape every test in this file uses, so a rule is always exercised
// through Run — the same path the command takes — rather than by calling its
// Scan function directly.
func builtSite(t *testing.T, rule string, files map[string]string) []Finding {
	t.Helper()
	withSource := map[string]string{"src/assets/css/a.css": ".a{margin:1rem}"}
	for k, v := range files {
		withSource[k] = v
	}
	root := siteWith(t, withSource)
	res, err := Run(root, enabled(rule), []string{"dist"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res.Errors
}

func page(body string) string {
	return `<!doctype html><html lang="en"><head><title>T</title></head><body>` + body + `</body></html>`
}

func wantNoFindings(t *testing.T, got []Finding) {
	t.Helper()
	if len(got) != 0 {
		t.Fatalf("expected a clean pass, got: %v", got)
	}
}

func wantOneFinding(t *testing.T, got []Finding, mustMention string) Finding {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("expected exactly one finding, got %d: %v", len(got), got)
	}
	if mustMention != "" && !strings.Contains(got[0].Detail, mustMention) {
		t.Errorf("finding should mention %q, got %q", mustMention, got[0].Detail)
	}
	return got[0]
}

// ── R2 landmarks ──────────────────────────────────────────────────────────

func TestSingleMainLandmark_MissingAndDuplicated(t *testing.T) {
	wantNoFindings(t, builtSite(t, "single-main-landmark", map[string]string{
		"dist/index.html": page(`<main>content</main>`),
	}))

	wantOneFinding(t, builtSite(t, "single-main-landmark", map[string]string{
		"dist/index.html": page(`<div>content</div>`),
	}), "no <main>")

	got := builtSite(t, "single-main-landmark", map[string]string{
		"dist/index.html": page("<main>a</main>\n<main>b</main>"),
	})
	f := wantOneFinding(t, got, "second <main>")
	if f.Line == 0 {
		t.Error("a duplicate landmark finding must carry the line of the duplicate")
	}
}

func TestChromeLandmarks_ReportsEachMissingLandmarkSeparately(t *testing.T) {
	wantNoFindings(t, builtSite(t, "page-chrome-landmarks", map[string]string{
		"dist/index.html": page(`<nav>n</nav><main>m</main><footer>f</footer>`),
	}))

	got := builtSite(t, "page-chrome-landmarks", map[string]string{
		"dist/index.html": page(`<main>m</main>`),
	})
	if len(got) != 2 {
		t.Fatalf("expected a finding for nav AND for footer, got: %v", got)
	}
}

func TestChromeLandmarks_ChromelessPageCanBeExcepted(t *testing.T) {
	// ffreis-editify's login, register and app-shell pages ship no chrome on
	// purpose. The rule has to be adoptable by a site like that without being
	// weakened for the pages that DO have chrome.
	root := siteWith(t, map[string]string{
		"src/assets/css/a.css": ".a{margin:1rem}",
		"dist/index.html":      page(`<nav>n</nav><main>m</main><footer>f</footer>`),
		"dist/login.html":      page(`<main>sign in</main>`),
	})
	cfg := Config{Rules: map[string]RuleConfig{"page-chrome-landmarks": {
		Enabled:    true,
		Exceptions: []Exception{{Path: "dist/login.html", Reason: "auth screen ships no site chrome by design"}},
	}}}
	res, err := Run(root, cfg, []string{"dist"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("expected the exception to cover login.html only, got: %v", res.Errors)
	}
	if len(res.StaleExceptions) != 0 {
		t.Errorf("exception matched a real file but was reported stale: %v", res.StaleExceptions)
	}
	if res.ScannedFiles["page-chrome-landmarks"] != 1 {
		t.Errorf("expected index.html still scanned, got %d files", res.ScannedFiles["page-chrome-landmarks"])
	}
}

// ── R3 forms ──────────────────────────────────────────────────────────────

func TestLabeledFormControls_AcceptsEveryRealLabellingMechanism(t *testing.T) {
	// The wrapping form is the one a regex cannot see, and it is the form
	// ffreis-forma uses for 83 of its 94 labels. Getting it wrong would make the
	// rule unusable on the first site that adopts it.
	wantNoFindings(t, builtSite(t, "labeled-form-controls", map[string]string{
		"dist/index.html": page(`
			<label for="a">Email</label><input id="a" type="email">
			<label>Phone <input type="tel"></label>
			<input type="text" aria-label="Search">
			<span id="lbl">City</span><input type="text" aria-labelledby="lbl">
			<label>Size <select><option>S</option></select></label>
			<input type="hidden" name="csrf" value="x">
			<input type="submit" value="Go">
		`),
	}))
}

func TestLabeledFormControls_PlaceholderIsNotALabel(t *testing.T) {
	got := builtSite(t, "labeled-form-controls", map[string]string{
		"dist/index.html": page(`<input type="search" id="headerSearch" placeholder="Search catalogues">`),
	})
	f := wantOneFinding(t, got, "placeholder")
	if !strings.Contains(f.Detail, "Search catalogues") {
		t.Errorf("the finding should quote the placeholder so it can be found in the template: %q", f.Detail)
	}
	if !strings.Contains(f.Detail, "headerSearch") {
		t.Errorf("the finding should name the control: %q", f.Detail)
	}
}

func TestLabeledFormControls_DanglingReferencesAreReported(t *testing.T) {
	// Both directions look correct in review and name nothing at runtime.
	got := builtSite(t, "labeled-form-controls", map[string]string{
		"dist/index.html": page(`<input type="text" aria-labelledby="gone">`),
	})
	wantOneFinding(t, got, "no element on the page has that id")

	got = builtSite(t, "labeled-form-controls", map[string]string{
		"dist/index.html": page(`<label for="gone">X</label><input type="text" aria-label="ok">`),
	})
	wantOneFinding(t, got, "points at an id that does not exist")
}

func TestFormAutocomplete_FlagsIdentityFieldsOnly(t *testing.T) {
	wantNoFindings(t, builtSite(t, "form-autocomplete", map[string]string{
		"dist/index.html": page(`
			<label>Email <input type="email" autocomplete="email"></label>
			<label>Search <input type="search"></label>
			<label>Quantity <input type="number" name="qty"></label>
			<label>Colour <input type="color" name="accent"></label>
		`),
	}))

	got := builtSite(t, "form-autocomplete", map[string]string{
		"dist/index.html": page(`<label>Email <input type="email" name="email"></label>`),
	})
	wantOneFinding(t, got, `autocomplete="email"`)
}

func TestFormAutocomplete_RecognisesFieldsByName(t *testing.T) {
	got := builtSite(t, "form-autocomplete", map[string]string{
		"dist/index.html": page(`
			<label>Name <input type="text" name="name"></label>
			<label>Postcode <input type="text" name="postcode"></label>
		`),
	})
	if len(got) != 2 {
		t.Fatalf("expected both name and postcode flagged, got: %v", got)
	}
	joined := got[0].Detail + got[1].Detail
	for _, want := range []string{`"name"`, `"postal-code"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected a finding naming %s, got: %v", want, got)
		}
	}
}

func TestFormAutocomplete_AnyNonEmptyTokenSatisfiesTheRule(t *testing.T) {
	// Judging whether the token is the RIGHT one is left to review on purpose: a
	// gate that guesses "email" where the author meant "username" cries wolf,
	// and a gate that cries wolf gets deleted.
	wantNoFindings(t, builtSite(t, "form-autocomplete", map[string]string{
		"dist/index.html": page(`<label>Login <input type="email" autocomplete="username"></label>`),
	}))
}

func TestFormErrorWiring_RequiredControlNeedsADescription(t *testing.T) {
	wantNoFindings(t, builtSite(t, "form-error-wiring", map[string]string{
		"dist/index.html": page(`
			<label>Email <input type="email" required aria-describedby="email-err"></label>
			<p id="email-err">We only use this to reply.</p>
			<label>Nickname <input type="text"></label>
		`),
	}))

	got := builtSite(t, "form-error-wiring", map[string]string{
		"dist/index.html": page(`<label>Email <input type="email" required></label>`),
	})
	wantOneFinding(t, got, "no aria-describedby")
}

func TestFormErrorWiring_AcceptsBothBooleanSpellings(t *testing.T) {
	// The compiler emits boolean attributes bare since the markup-gate fix, and
	// sites still have `required=""` in older built output. Both must count.
	for _, spelling := range []string{`required`, `required=""`, `aria-required="true"`} {
		got := builtSite(t, "form-error-wiring", map[string]string{
			"dist/index.html": page(`<label>E <input type="email" ` + spelling + `></label>`),
		})
		if len(got) != 1 {
			t.Errorf("%s was not recognised as required: %v", spelling, got)
		}
	}
}

func TestFormErrorWiring_DanglingDescribedByIsReported(t *testing.T) {
	got := builtSite(t, "form-error-wiring", map[string]string{
		"dist/index.html": page(`<label>E <input type="email" aria-describedby="gone"></label>`),
	})
	wantOneFinding(t, got, "nothing is announced")
}

func TestFormErrorWiring_DoesNotRequireAStaticAriaInvalid(t *testing.T) {
	// Deliberate: aria-invalid is a runtime state. A page shipping
	// aria-invalid="false" on every field has added markup, not accessibility,
	// and a gate that demanded it would be teaching the wrong thing.
	wantNoFindings(t, builtSite(t, "form-error-wiring", map[string]string{
		"dist/index.html": page(`
			<label>E <input type="email" required aria-describedby="e"></label><p id="e">hint</p>
		`),
	}))
}

// ── R1 LCP image ──────────────────────────────────────────────────────────

func TestLCPImageNotLazy_OnlyTheFirstImageMatters(t *testing.T) {
	got := builtSite(t, "lcp-image-not-lazy", map[string]string{
		"dist/index.html": page(`<img src="hero.webp" alt="Hero" loading="lazy">`),
	})
	wantOneFinding(t, got, "Largest Contentful Paint")

	// A lazy image further down the page is correct and must not be flagged.
	wantNoFindings(t, builtSite(t, "lcp-image-not-lazy", map[string]string{
		"dist/index.html": page(`<img src="hero.webp" alt="Hero" fetchpriority="high">` +
			`<img src="b.webp" alt="B" loading="lazy">`),
	}))

	// A page with no images has nothing to say about an LCP image.
	wantNoFindings(t, builtSite(t, "lcp-image-not-lazy", map[string]string{
		"dist/index.html": page(`<main>text only</main>`),
	}))
}

// ── R6 / R8 first-loaded stylesheet ───────────────────────────────────────

const goodReset = `*,*::before,*::after{box-sizing:border-box}` +
	`body{margin:0;font-family:"DM Sans",sans-serif}` +
	`img,picture,video{display:block;max-width:100%}` +
	`input,button,select,textarea{font:inherit}`

func TestCSSResetPresent_AcceptsAMinifiedInlineReset(t *testing.T) {
	// Real built pages are minified, so every check has to be
	// whitespace-insensitive: `max-width:100%` and `max-width: 100%` are the
	// same declaration.
	wantNoFindings(t, builtSite(t, "css-reset-present", map[string]string{
		"dist/index.html": `<html><head><style>` + goodReset + `</style></head><body>x</body></html>`,
	}))
}

func TestCSSResetPresent_NamesTheMissingPiece(t *testing.T) {
	withoutMedia := strings.Replace(goodReset, `img,picture,video{display:block;max-width:100%}`, "", 1)
	got := builtSite(t, "css-reset-present", map[string]string{
		"dist/index.html": `<html><head><style>` + withoutMedia + `</style></head><body>x</body></html>`,
	})
	wantOneFinding(t, got, "responsive media defaults")
}

func TestCSSResetPresent_ReadsTheFirstLinkedStylesheet(t *testing.T) {
	// The reset normally lives in a file the page only names, which is the whole
	// reason a rule may read the tree.
	wantNoFindings(t, builtSite(t, "css-reset-present", map[string]string{
		"dist/index.html":  `<html><head><link rel="stylesheet" href="css/app.css"></head><body>x</body></html>`,
		"dist/css/app.css": goodReset,
	}))

	got := builtSite(t, "css-reset-present", map[string]string{
		"dist/index.html":  `<html><head><link rel="stylesheet" href="/css/app.css"></head><body>x</body></html>`,
		"dist/css/app.css": `body{margin:0}`,
	})
	if len(got) != 3 {
		t.Fatalf("expected the three missing pieces of a root-absolute stylesheet, got: %v", got)
	}
	if !strings.Contains(got[0].Detail, "/css/app.css") {
		t.Errorf("the finding should name the stylesheet it read: %q", got[0].Detail)
	}
}

func TestCSSResetPresent_SkipsAnExternalStylesheetAndUsesTheFirstLocalOne(t *testing.T) {
	// Every site here links Google Fonts before its own CSS. Treating that as
	// "the first stylesheet" would fail every page in the fleet on a sheet we
	// cannot read and do not control.
	wantNoFindings(t, builtSite(t, "css-reset-present", map[string]string{
		"dist/index.html": `<html><head>` +
			`<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=DM+Sans">` +
			`<style>` + goodReset + `</style></head><body>x</body></html>`,
	}))
}

func TestCSSResetPresent_PageWithNoLocalCSSIsReported(t *testing.T) {
	got := builtSite(t, "css-reset-present", map[string]string{
		"dist/index.html": `<html><head><title>T</title></head><body>x</body></html>`,
	})
	wantOneFinding(t, got, "loads no local stylesheet")
}

func TestCSSResetPresent_DoesNotReadOutsideTheScanRoot(t *testing.T) {
	// An href that climbs out of the tree must not be followed, and must not
	// crash: it is simply not readable, which the rule reports honestly.
	got := builtSite(t, "css-reset-present", map[string]string{
		"dist/index.html": `<html><head><link rel="stylesheet" href="../../etc/passwd"></head><body>x</body></html>`,
	})
	wantOneFinding(t, got, "loads no local stylesheet")
}

func TestFontFamilyDeclared_OnlyCountsBodyHtmlOrRoot(t *testing.T) {
	wantNoFindings(t, builtSite(t, "font-family-declared", map[string]string{
		"dist/index.html": `<html><head><style>body{font-family:"DM Sans",sans-serif}</style></head><body>x</body></html>`,
	}))

	// A family set only on a component leaves everything else on the browser
	// default — and `.body-copy` must not be mistaken for `body`.
	got := builtSite(t, "font-family-declared", map[string]string{
		"dist/index.html": `<html><head><style>.body-copy{font-family:serif}</style></head><body>x</body></html>`,
	})
	wantOneFinding(t, got, "no font-family on body")
}

func TestFontFamilyDeclared_IgnoresAFontFaceBlock(t *testing.T) {
	// @font-face{font-family:X} declares a face, not the page's font.
	got := builtSite(t, "font-family-declared", map[string]string{
		"dist/index.html": `<html><head><style>@font-face{font-family:"X";src:url(x.woff2)}` +
			`body{color:#111}</style></head><body>x</body></html>`,
	})
	wantOneFinding(t, got, "no font-family on body")
}

// ── R10 SEO head ──────────────────────────────────────────────────────────

func TestSEOMetaPresent_AllThreeRequired(t *testing.T) {
	complete := `<html><head><title>Forma</title>` +
		`<meta name="description" content="Printed catalogues.">` +
		`<link rel="canonical" href="https://forma.example/">` +
		`</head><body>x</body></html>`
	wantNoFindings(t, builtSite(t, "seo-meta-present", map[string]string{"dist/index.html": complete}))

	for _, tc := range []struct{ name, html, want string }{
		{"empty title", strings.Replace(complete, "<title>Forma</title>", "<title> </title>", 1), "<title>"},
		{"no description", strings.Replace(complete, `<meta name="description" content="Printed catalogues.">`, "", 1), "description"},
		{"empty description", strings.Replace(complete, `content="Printed catalogues."`, `content=""`, 1), "description"},
		{"no canonical", strings.Replace(complete, `<link rel="canonical" href="https://forma.example/">`, "", 1), "canonical"},
	} {
		got := builtSite(t, "seo-meta-present", map[string]string{"dist/index.html": tc.html})
		if len(got) != 1 || !strings.Contains(got[0].Detail, tc.want) {
			t.Errorf("%s: expected one finding mentioning %q, got: %v", tc.name, tc.want, got)
		}
	}
}

func TestSEOMetaPresent_ReadsMetaRegardlessOfAttributeOrder(t *testing.T) {
	// The compiler emits attributes alphabetically, so `content` precedes `name`.
	// A regex that assumed property-then-content would report every compiler-built
	// page as missing its description.
	wantNoFindings(t, builtSite(t, "seo-meta-present", map[string]string{
		"dist/index.html": `<html><head><title>T</title>` +
			`<meta content="Printed catalogues." name="description"/>` +
			`<link href="https://x.example/" rel="canonical"/></head><body>x</body></html>`,
	}))
}

func TestSocialPreviewImage_PresenceOnly(t *testing.T) {
	wantNoFindings(t, builtSite(t, "social-preview-image", map[string]string{
		"dist/index.html": `<html><head><meta content="https://x.example/og.png" property="og:image"/>` +
			`</head><body>x</body></html>`,
	}))

	got := builtSite(t, "social-preview-image", map[string]string{
		"dist/index.html": page(`<main>x</main>`),
	})
	wantOneFinding(t, got, "src/assets/img/og-image.png")
}

// ── engine behaviour the new rules depend on ──────────────────────────────

func TestRun_OutputRuleWithoutADistRootIsAnError(t *testing.T) {
	// The single most common way a gate in this fleet has turned out to be
	// measuring nothing: enabled, green, and never handed a built site.
	root := siteWith(t, map[string]string{"src/assets/css/a.css": ".a{margin:1rem}"})
	res, err := Run(root, enabled("single-main-landmark"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := wantOneFinding(t, res.Errors, "-dist")
	if f.File != ConfigFileName {
		t.Errorf("a misconfiguration should be reported against %s, got %q", ConfigFileName, f.File)
	}
}

func TestRuleDoc_EveryRegisteredRuleIsDocumented(t *testing.T) {
	// The doc is what a site sees in `-list` and what makes a finding fixable
	// without reading compiler source. A rule added without one is a rule nobody
	// can adopt.
	for _, name := range RuleNames() {
		doc := RuleDoc(name)
		if len(strings.TrimSpace(doc)) < 80 {
			t.Errorf("rule %q has no usable Doc (%d chars)", name, len(doc))
		}
		if registry[name].Scan == nil && registry[name].ScanTree == nil {
			t.Errorf("rule %q registers no scan function", name)
		}
		if registry[name].Selects == nil {
			t.Errorf("rule %q registers no file filter", name)
		}
	}
}

func TestLabeledFormControls_SpamHoneypotIsNotAViolation(t *testing.T) {
	// Found by running the rule against petlook-website: its
	// name="contactHoneypot" field is aria-hidden + tabindex="-1" + hidden by
	// CSS. It is correctly removed from the accessibility tree, so demanding an
	// accessible name is incoherent — and a false positive like this is one whose
	// natural fix is to stop enabling the rule.
	wantNoFindings(t, builtSite(t, "labeled-form-controls", map[string]string{
		"dist/index.html": page(`<form><input type="text" name="contactHoneypot" ` +
			`class="form-honeypot" tabindex="-1" autocomplete="off" aria-hidden="true"></form>`),
	}))
}

func TestLabeledFormControls_AriaHiddenOnAFocusableControlIsItsOwnDefect(t *testing.T) {
	// Not excused, because ARIA forbids it: the control stays in the tab order,
	// so a keyboard user reaches a field their screen reader will not describe.
	got := builtSite(t, "labeled-form-controls", map[string]string{
		"dist/index.html": page(`<input type="text" name="trap" aria-hidden="true">`),
	})
	wantOneFinding(t, got, "still focusable")
}

func TestFormRules_AllSkipAnIntentionallyHiddenControl(t *testing.T) {
	// A honeypot must not be asked for an autocomplete token or an error
	// description either; all three form rules share one exclusion.
	honeypot := page(`<input type="email" name="email" required tabindex="-1" aria-hidden="true">`)
	for _, rule := range []string{"labeled-form-controls", "form-autocomplete", "form-error-wiring"} {
		if got := builtSite(t, rule, map[string]string{"dist/index.html": honeypot}); len(got) != 0 {
			t.Errorf("%s flagged an aria-hidden, unfocusable control: %v", rule, got)
		}
	}
}

func TestCollectControls_SkipsAnUnrenderedControl(t *testing.T) {
	wantNoFindings(t, builtSite(t, "labeled-form-controls", map[string]string{
		"dist/index.html": page(`<input type="text" name="x" hidden>`),
	}))
}

// ── css-reset-present: corrections forced by running it on the real fleet ──

func TestCSSResetPresent_ScansEveryStyleBlockNotJustTheFirst(t *testing.T) {
	// The compiler's own position-based transform splits head CSS into a small
	// critical <style> block plus the rest, which is the fleet default. On
	// ffreis-website the first block is 297 bytes — box-sizing, margin and the
	// body font — while the media and form-control resets live one block later.
	// Reading only the first source reported three missing pieces that were all
	// present, on essentially every compiler-built site.
	critical := `*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}body{font-family:"Inter",sans-serif}`
	rest := `img,picture,video{display:block;max-width:100%}input,button,select,textarea{font:inherit}`
	wantNoFindings(t, builtSite(t, "css-reset-present", map[string]string{
		"dist/index.html": `<html><head><style>` + critical + `</style>` +
			`<style>` + rest + `</style></head><body>x</body></html>`,
	}))
}

func TestCSSResetPresent_AcceptsAUniversalMarginReset(t *testing.T) {
	// `*,*::before,*::after{margin:0}` is the modern idiom and is STRONGER than a
	// body-only reset. Demanding a `body` selector specifically reported
	// ffreis-website, petlook-website and flemming-website as missing a reset
	// they all have.
	css := `*,*::before,*::after{box-sizing:border-box;margin:0}` +
		`img,picture,video{display:block;max-width:100%}input,button,select,textarea{font:inherit}`
	wantNoFindings(t, builtSite(t, "css-reset-present", map[string]string{
		"dist/index.html": `<html><head><style>` + css + `</style></head><body>x</body></html>`,
	}))
}

func TestCSSResetPresent_MediaDefaultsMayLiveInSeparateRules(t *testing.T) {
	// Requiring both declarations in the SAME rule reported ffreis-forma's
	// `img,svg,video,canvas{max-width:100%}` as having no media reset at all,
	// when what it actually lacked was display:block — a true finding described
	// wrongly, which sends the reader looking for the wrong thing.
	css := `*{box-sizing:border-box;margin:0}` +
		`img,svg,video,canvas{max-width:100%}` +
		`img,picture,video{display:block}` +
		`input,button,select,textarea{font:inherit}`
	wantNoFindings(t, builtSite(t, "css-reset-present", map[string]string{
		"dist/index.html": `<html><head><style>` + css + `</style></head><body>x</body></html>`,
	}))
}

func TestCSSResetPresent_StillCatchesTheMeyerResetsRealGaps(t *testing.T) {
	// Loosening must not make the rule vacuous. flemming-website's Eric Meyer
	// reset sets `font: inherit` on a long element list that includes img and
	// video but NOT input/button/select/textarea, and sets no max-width or
	// display on media. Both gaps are real and must still be reported.
	meyer := `html,body,div,span,em,img,ins,kbd,q,s,samp,small,strike,strong,video` +
		`{margin:0;padding:0;border:0;font:inherit;vertical-align:baseline}` +
		`*{box-sizing:border-box}`
	got := builtSite(t, "css-reset-present", map[string]string{
		"dist/index.html": `<html><head><style>` + meyer + `</style></head><body>x</body></html>`,
	})
	if len(got) != 2 {
		t.Fatalf("expected the media and form-control gaps still reported, got: %v", got)
	}
	joined := got[0].Detail + got[1].Detail
	for _, want := range []string{"responsive media defaults", "form controls inheriting typography"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q still reported, got: %v", want, got)
		}
	}
}

func TestCSSResetPresent_AUniversalFontResetCountsForFormControls(t *testing.T) {
	css := `*{box-sizing:border-box;margin:0;font:inherit}img,picture,video{display:block;max-width:100%}`
	wantNoFindings(t, builtSite(t, "css-reset-present", map[string]string{
		"dist/index.html": `<html><head><style>` + css + `</style></head><body>x</body></html>`,
	}))
}

func TestFontFamilyDeclared_ScansEveryStyleBlock(t *testing.T) {
	wantNoFindings(t, builtSite(t, "font-family-declared", map[string]string{
		"dist/index.html": `<html><head><style>*{box-sizing:border-box}</style>` +
			`<style>body{font-family:"Inter",sans-serif}</style></head><body>x</body></html>`,
	}))
}

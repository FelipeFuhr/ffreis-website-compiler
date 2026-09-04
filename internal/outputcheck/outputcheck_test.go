package outputcheck_test

import (
	"os"
	"path/filepath"
	"testing"

	"ffreis-website-compiler/internal/outputcheck"
)

func targets() []outputcheck.Target {
	return []outputcheck.Target{
		{Route: "/", Lang: "en", HTML: `<html lang="en"><main id="main"><a href="/en/about/">About</a></main></html>`},
		{Route: "/about/", Lang: "en", HTML: `<html lang="en"><main id="main">About us</main></html>`},
		{Route: "/checkout/", Lang: "en", HTML: `<html lang="en"><main id="main" data-commerce-checkout></main></html>`},
	}
}

// --- Selector: decides WHERE a check applies, knowing nothing about WHAT ----

func TestSelectorMatchesEverythingByDefault(t *testing.T) {
	var sel outputcheck.Selector
	for _, tgt := range targets() {
		if !sel.Matches(tgt) {
			t.Fatalf("empty selector must match %s", tgt.Route)
		}
	}
}

func TestSelectorScopesByRoute(t *testing.T) {
	sel := outputcheck.Selector{Routes: []string{"/checkout/"}}
	if sel.Matches(outputcheck.Target{Route: "/"}) {
		t.Fatal("selector must not match an unlisted route")
	}
	if !sel.Matches(outputcheck.Target{Route: "/checkout/"}) {
		t.Fatal("selector must match its listed route")
	}
}

func TestSelectorScopesByLanguage(t *testing.T) {
	// The property that stops an English assertion running against the pt tree.
	sel := outputcheck.Selector{Langs: []string{"en"}}
	if sel.Matches(outputcheck.Target{Route: "/", Lang: "pt"}) {
		t.Fatal("a language-scoped selector must not match another language")
	}
	if !sel.Matches(outputcheck.Target{Route: "/", Lang: "en"}) {
		t.Fatal("a language-scoped selector must match its own language")
	}
}

func TestSelectorExcludesNegatedRoutes(t *testing.T) {
	sel := outputcheck.Selector{Routes: []string{"*"}, ExcludeRoutes: []string{"/admin/"}}
	if sel.Matches(outputcheck.Target{Route: "/admin/"}) {
		t.Fatal("an excluded route must not match")
	}
	if !sel.Matches(outputcheck.Target{Route: "/"}) {
		t.Fatal("a non-excluded route must still match")
	}
}

// --- Registry: the extension point. Adding a check must not touch the runner --

func TestRegistryBuildsARegisteredCheck(t *testing.T) {
	reg := outputcheck.NewRegistry()
	reg.Register("always_fails", func(_ map[string]any) (outputcheck.Check, error) {
		return outputcheck.CheckFunc{
			CheckName: "always_fails",
			Fn: func(_ outputcheck.Target) []string {
				return []string{"boom"}
			},
		}, nil
	})

	check, err := reg.Build("always_fails", nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := check.Check(outputcheck.Target{}); len(got) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(got))
	}
}

func TestRegistryRejectsAnUnknownType(t *testing.T) {
	reg := outputcheck.NewRegistry()
	if _, err := reg.Build("nope", nil); err == nil {
		t.Fatal("an unknown check type must be an error, not a silent skip")
	}
}

func TestBuiltinsAreRegistered(t *testing.T) {
	reg := outputcheck.DefaultRegistry()
	for _, name := range []string{"contains", "not_contains", "matches", "not_matches"} {
		if _, err := reg.Build(name, map[string]any{"value": "x", "pattern": "x"}); err != nil {
			t.Fatalf("builtin %q must be registered: %v", name, err)
		}
	}
}

// --- Built-in checks: each cohesive, each independent -----------------------

func TestMatchesCheckUsesRegex(t *testing.T) {
	reg := outputcheck.DefaultRegistry()
	check, err := reg.Build("matches", map[string]any{"pattern": `<main\s+id="main"`})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := check.Check(targets()[0]); len(got) != 0 {
		t.Fatalf("expected match, got failures %v", got)
	}
	if got := check.Check(outputcheck.Target{HTML: "<main>"}); len(got) == 0 {
		t.Fatal("expected a failure when the pattern is absent")
	}
}

func TestNotMatchesCheckRejectsForbiddenMarkup(t *testing.T) {
	reg := outputcheck.DefaultRegistry()
	check, _ := reg.Build("not_matches", map[string]any{"pattern": `(?i)name="(cardnumber|cvv)"`})
	safe := outputcheck.Target{HTML: `<input name="email">`}
	unsafe := outputcheck.Target{HTML: `<input name="CardNumber">`}
	if got := check.Check(safe); len(got) != 0 {
		t.Fatalf("safe markup must pass, got %v", got)
	}
	if got := check.Check(unsafe); len(got) == 0 {
		t.Fatal("card-data markup must fail")
	}
}

func TestAnInvalidRegexIsRejectedAtBuildTime(t *testing.T) {
	reg := outputcheck.DefaultRegistry()
	if _, err := reg.Build("matches", map[string]any{"pattern": "([unclosed"}); err == nil {
		t.Fatal("a bad pattern must fail at build time, not at run time")
	}
}

// --- Corpus checks: assertions that need the whole site ---------------------

// writeCompiledOutput lays out a tiny compiled-output tree (files keyed by
// their path relative to the root, e.g. "site.webmanifest" or
// "images/logo.png") and returns the root plus a Target list carrying that
// root, mimicking what outputtestcmd's real loader hands the engine.
func writeCompiledOutput(t *testing.T, files map[string]string) (root string, targets []outputcheck.Target) {
	t.Helper()
	root = t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	return root, []outputcheck.Target{{Route: "/", Lang: "en", Root: root}}
}

func TestJSONAssetsExistIsRegistered(t *testing.T) {
	reg := outputcheck.DefaultRegistry()
	if _, err := reg.BuildCorpus("json_assets_exist", map[string]any{"path": "x", "field": "y"}); err != nil {
		t.Fatalf("json_assets_exist must be registered: %v", err)
	}
}

func TestJSONAssetsExistCatchesAGenuinelyMissingAsset(t *testing.T) {
	// The icon file was never fingerprinted/copied to dist — the manifest
	// references a path that simply does not exist in the compiled output.
	root, targets := writeCompiledOutput(t, map[string]string{
		"site.webmanifest": `{"icons":[{"src":"/images/logo-256.png","sizes":"256x256"}]}`,
	})
	reg := outputcheck.DefaultRegistry()
	check, err := reg.BuildCorpus("json_assets_exist", map[string]any{
		"path": "site.webmanifest", "field": "icons[].src",
	})
	if err != nil {
		t.Fatalf("BuildCorpus: %v", err)
	}
	findings := check.CheckAll(targets)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for the missing icon, got %d: %v", len(findings), findings)
	}
	if findings[0].Message == "" || findings[0].Check != "json_assets_exist" {
		t.Errorf("finding lacks context: %+v", findings[0])
	}
	_ = root
}

func TestJSONAssetsExistPassesWhenEveryAssetResolves(t *testing.T) {
	root, targets := writeCompiledOutput(t, map[string]string{
		"site.webmanifest":    `{"icons":[{"src":"/images/logo-256.png","sizes":"256x256"}]}`,
		"images/logo-256.png": "pngdata",
	})
	reg := outputcheck.DefaultRegistry()
	check, err := reg.BuildCorpus("json_assets_exist", map[string]any{
		"path": "site.webmanifest", "field": "icons[].src",
	})
	if err != nil {
		t.Fatalf("BuildCorpus: %v", err)
	}
	if findings := check.CheckAll(targets); len(findings) != 0 {
		t.Fatalf("expected no findings when every referenced asset exists, got %v", findings)
	}
	_ = root
}

func TestJSONAssetsExistCatchesTheJSONFileItselfMissing(t *testing.T) {
	root, targets := writeCompiledOutput(t, map[string]string{
		"index.html": "<html></html>",
	})
	reg := outputcheck.DefaultRegistry()
	check, err := reg.BuildCorpus("json_assets_exist", map[string]any{
		"path": "manifest.json", "field": "icons[].src",
	})
	if err != nil {
		t.Fatalf("BuildCorpus: %v", err)
	}
	findings := check.CheckAll(targets)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding when the JSON file itself is absent, got %d: %v", len(findings), findings)
	}
	_ = root
}

func TestJSONAssetsExistToleratesInvalidJSON(t *testing.T) {
	root, targets := writeCompiledOutput(t, map[string]string{
		"site.webmanifest": `{not valid json`,
	})
	reg := outputcheck.DefaultRegistry()
	check, err := reg.BuildCorpus("json_assets_exist", map[string]any{
		"path": "site.webmanifest", "field": "icons[].src",
	})
	if err != nil {
		t.Fatalf("BuildCorpus: %v", err)
	}
	if findings := check.CheckAll(targets); len(findings) != 1 {
		t.Fatalf("expected exactly 1 finding reporting invalid JSON, got %d: %v", len(findings), findings)
	}
	_ = root
}

func TestJSONAssetsExistNoIconsKeyPasses(t *testing.T) {
	// Not every site declares icons; an absent key must not be a false failure.
	root, targets := writeCompiledOutput(t, map[string]string{
		"site.webmanifest": `{"name":"Example"}`,
	})
	reg := outputcheck.DefaultRegistry()
	check, err := reg.BuildCorpus("json_assets_exist", map[string]any{
		"path": "site.webmanifest", "field": "icons[].src",
	})
	if err != nil {
		t.Fatalf("BuildCorpus: %v", err)
	}
	if findings := check.CheckAll(targets); len(findings) != 0 {
		t.Fatalf("expected no findings when the field path is absent, got %v", findings)
	}
	_ = root
}

func TestJSONAssetsExistSkipsExternalRefs(t *testing.T) {
	root, targets := writeCompiledOutput(t, map[string]string{
		"site.webmanifest": `{"icons":[{"src":"https://cdn.example.com/icon.png"}]}`,
	})
	reg := outputcheck.DefaultRegistry()
	check, err := reg.BuildCorpus("json_assets_exist", map[string]any{
		"path": "site.webmanifest", "field": "icons[].src",
	})
	if err != nil {
		t.Fatalf("BuildCorpus: %v", err)
	}
	if findings := check.CheckAll(targets); len(findings) != 0 {
		t.Fatalf("expected external refs to be skipped, got %v", findings)
	}
	_ = root
}

func TestJSONAssetsExistGenericFieldPathNotHardcodedToWebmanifest(t *testing.T) {
	// The check must work for an arbitrary JSON shape/field path, not just
	// the web-app-manifest icons[].src convention.
	root, targets := writeCompiledOutput(t, map[string]string{
		"data/pins.json":   `{"pins":[{"thumb":"/images/pin-a.png"},{"thumb":"/images/pin-b.png"}]}`,
		"images/pin-a.png": "a",
		// pin-b.png intentionally absent.
	})
	reg := outputcheck.DefaultRegistry()
	check, err := reg.BuildCorpus("json_assets_exist", map[string]any{
		"path": "data/pins.json", "field": "pins[].thumb",
	})
	if err != nil {
		t.Fatalf("BuildCorpus: %v", err)
	}
	findings := check.CheckAll(targets)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for the one missing pin thumb, got %d: %v", len(findings), findings)
	}
	if findings[0].Message == "" {
		t.Errorf("finding message must be clear about which path is missing")
	}
	_ = root
}

func TestJSONAssetsExistRejectsMissingConfigAtBuildTime(t *testing.T) {
	reg := outputcheck.DefaultRegistry()
	if _, err := reg.BuildCorpus("json_assets_exist", map[string]any{"field": "icons[].src"}); err == nil {
		t.Fatal("a missing path field must be rejected at build time")
	}
	if _, err := reg.BuildCorpus("json_assets_exist", map[string]any{"path": "x"}); err == nil {
		t.Fatal("a missing field field must be rejected at build time")
	}
}

// --- Suite: composition of selectors and checks -----------------------------

func TestSuiteCollectsEveryFindingRatherThanFailingFast(t *testing.T) {
	reg := outputcheck.DefaultRegistry()
	failing, _ := reg.Build("contains", map[string]any{"value": "NEVER-PRESENT"})

	suite := outputcheck.Suite{
		Rules: []outputcheck.Rule{
			{Name: "everywhere", Checks: []outputcheck.Check{failing}},
		},
	}
	findings := suite.Run(targets())
	if len(findings) != 3 {
		t.Fatalf("expected one finding per target (3), got %d: %v", len(findings), findings)
	}
}

func TestSuiteAppliesARuleOnlyWhereItsSelectorMatches(t *testing.T) {
	reg := outputcheck.DefaultRegistry()
	check, _ := reg.Build("matches", map[string]any{"pattern": "data-commerce-checkout"})

	suite := outputcheck.Suite{
		Rules: []outputcheck.Rule{{
			Name:     "checkout boundary",
			Selector: outputcheck.Selector{Routes: []string{"/checkout/"}},
			Checks:   []outputcheck.Check{check},
		}},
	}
	if findings := suite.Run(targets()); len(findings) != 0 {
		t.Fatalf("the rule must only run on /checkout/, got %v", findings)
	}
}

func TestFindingsCarryEnoughContextToAct(t *testing.T) {
	reg := outputcheck.DefaultRegistry()
	check, _ := reg.Build("contains", map[string]any{"value": "MISSING"})
	suite := outputcheck.Suite{Rules: []outputcheck.Rule{{Name: "rule-a", Checks: []outputcheck.Check{check}}}}

	findings := suite.Run([]outputcheck.Target{{Route: "/x/", Lang: "pt", HTML: "y"}})
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Rule != "rule-a" || f.Route != "/x/" || f.Lang != "pt" || f.Check == "" || f.Message == "" {
		t.Fatalf("finding lacks context: %+v", f)
	}
}

// countingCorpus stands in for any whole-site check; the engine owns the
// dispatch contract, not any particular corpus implementation.
type countingCorpus struct{}

func (countingCorpus) Name() string { return "counting" }

func (countingCorpus) CheckAll(targets []outputcheck.Target) []outputcheck.Finding {
	out := make([]outputcheck.Finding, 0, len(targets))
	for _, t := range targets {
		out = append(out, outputcheck.Finding{Check: "counting", Route: t.Route, Message: "seen"})
	}
	return out
}

func TestSuiteRunsCorpusChecksOverTheWholeSet(t *testing.T) {
	suite := outputcheck.Suite{Corpus: []outputcheck.CorpusCheck{countingCorpus{}}}
	findings := suite.Run([]outputcheck.Target{
		{Route: "/", Lang: "en"},
		{Route: "/b/", Lang: "en"},
	})
	if len(findings) != 2 {
		t.Fatalf("a corpus check must see every target once, got %d", len(findings))
	}
}

// --- Presets: reusable bundles so sites don't copy-paste a baseline ---------

func TestPresetsExposeAnA11yBaseline(t *testing.T) {
	rules, err := outputcheck.Preset("a11y-baseline", outputcheck.DefaultRegistry())
	if err != nil {
		t.Fatalf("Preset: %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("the a11y baseline must contribute at least one rule")
	}
	// A page with no skip link and no main landmark must be caught by it.
	suite := outputcheck.Suite{Rules: rules}
	findings := suite.Run([]outputcheck.Target{{Route: "/", Lang: "en", HTML: "<html><body>bare</body></html>"}})
	if len(findings) == 0 {
		t.Fatal("the a11y baseline must flag a page with no landmark or skip link")
	}
}

func TestAnUnknownPresetIsAnError(t *testing.T) {
	if _, err := outputcheck.Preset("nope", outputcheck.DefaultRegistry()); err == nil {
		t.Fatal("an unknown preset must be an error")
	}
}

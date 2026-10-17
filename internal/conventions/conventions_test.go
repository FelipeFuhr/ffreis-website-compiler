package conventions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// siteWith builds a throwaway website root. files keys are slash-separated
// paths relative to the root.
func siteWith(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func enabled(rule string) Config {
	return Config{Rules: map[string]RuleConfig{rule: {Enabled: true}}}
}

// ── the rule itself ────────────────────────────────────────────────────────

func TestNoPxLiterals_FlagsEveryFormIncludingLeadingDot(t *testing.T) {
	// The leading-dot form is the one a prior fleet regex (`\b\d+(?:\.\d+)?px\b`)
	// reported as "5px" rather than ".5px", and that a later hand-written
	// variant missed entirely. Pin all three spellings.
	root := siteWith(t, map[string]string{
		"src/assets/css/a.css": ".x{margin:12px;border:.5px solid red;padding:0.25px}",
	})
	res, err := Run(root, enabled("no-px-literals"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 3 {
		t.Fatalf("expected 3 findings, got %d: %v", len(res.Errors), res.Errors)
	}
	joined := res.Errors[0].Detail + res.Errors[1].Detail + res.Errors[2].Detail
	for _, want := range []string{`"12px"`, `".5px"`, `"0.25px"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected a finding naming %s, got: %v", want, res.Errors)
		}
	}
}

func TestNoPxLiterals_IgnoresPxInsideIdentifiers(t *testing.T) {
	// "px" inside a name is not a length. Without this the rule flags asset
	// filenames and custom-property names, which would push sites to add
	// exceptions for files that never had a real violation — the fastest way to
	// make the exception list meaningless.
	root := siteWith(t, map[string]string{
		"src/assets/css/a.css": ":root{--step-2px:1rem}\n.i{background:url(icon-20px.svg)}",
	})
	res, err := Run(root, enabled("no-px-literals"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Errorf("expected no findings for px inside identifiers, got: %v", res.Errors)
	}
}

func TestNoPxLiterals_ReportsLineNumbers(t *testing.T) {
	root := siteWith(t, map[string]string{
		"src/assets/css/a.css": "/* one */\n/* two */\n.x{top:3px}\n",
	})
	res, err := Run(root, enabled("no-px-literals"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 1 || res.Errors[0].Line != 3 {
		t.Fatalf("expected one finding on line 3, got: %v", res.Errors)
	}
}

// TestNoPxLiterals_ScansTheWholeTreeNotOneFile is the whole reason this rule
// moved into the compiler.
//
// ffreis-forma's own no-px-literals.test.js read exactly one stylesheet,
// `src/assets/css/styles.css`, while the site had grown 60+ files under
// `css/pages/`. A violation in any of them passed. Absorbing a per-site test
// is only an improvement if the absorbed version is a strict superset, so that
// property gets its own test rather than being asserted in a commit message.
func TestNoPxLiterals_ScansTheWholeTreeNotOneFile(t *testing.T) {
	root := siteWith(t, map[string]string{
		"src/assets/css/styles.css":            ".a{margin:1rem}",
		"src/assets/css/pages/login.css":       ".b{margin:8px}",
		"src/assets/css/pages/_shared/app.css": ".c{gap:4px}",
		"src/assets/js/widget.js":              "el.style.top = '10px';",
	})
	res, err := Run(root, enabled("no-px-literals"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotFiles := map[string]bool{}
	for _, f := range res.Errors {
		gotFiles[f.File] = true
	}
	for _, want := range []string{
		"src/assets/css/pages/login.css",
		"src/assets/css/pages/_shared/app.css",
		"src/assets/js/widget.js",
	} {
		if !gotFiles[want] {
			t.Errorf("%s was not scanned — the rule is not a superset of the per-site test it replaces", want)
		}
	}
	if gotFiles["src/assets/css/styles.css"] {
		t.Errorf("flagged a clean file: %v", res.Errors)
	}
}

func TestRun_ScansTheDistRootForOutputRules(t *testing.T) {
	root := siteWith(t, map[string]string{
		"src/assets/css/a.css": ".a{margin:1rem}",
		"dist/index.html":      `<html><body><p>no main here</p></body></html>`,
	})
	res, err := Run(root, enabled("single-main-landmark"), []string{"dist"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 1 || res.Errors[0].File != "dist/index.html" {
		t.Fatalf("expected one finding in dist/index.html, got: %v", res.Errors)
	}
}

func TestRun_SourceRulesDoNotRescanBuiltOutput(t *testing.T) {
	// A px literal in dist/ is the same literal already reported from the source
	// file it was compiled from, and reporting it twice — once at a path nobody
	// can edit — is how a gate's output becomes something people skim past.
	root := siteWith(t, map[string]string{
		"src/assets/css/a.css": ".a{margin:1rem}",
		"dist/index.html":      `<div style="width:300px"></div>`,
		"dist/app.css":         ".b{top:9px}",
	})
	res, err := Run(root, enabled("no-px-literals"), []string{"dist"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("source rule reached into the built tree: %v", res.Errors)
	}
}

func TestRun_OutputRulesDoNotScanSourceTemplates(t *testing.T) {
	// A partial has no <main> and no <title> by design. An output rule that read
	// src/ would fail every correctly built site, so the domain split is the
	// thing being asserted here, not an implementation detail.
	root := siteWith(t, map[string]string{
		"src/assets/css/a.css":         ".a{margin:1rem}",
		"src/assets/css/fragment.html": `<p>a partial, no landmarks</p>`,
		"dist/index.html":              `<html><body><main>ok</main></body></html>`,
	})
	res, err := Run(root, enabled("single-main-landmark"), []string{"dist"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("output rule scanned source: %v", res.Errors)
	}
	if res.ScannedFiles["single-main-landmark"] != 1 {
		t.Errorf("expected exactly the one built page scanned, got %d", res.ScannedFiles["single-main-landmark"])
	}
}

// ── severity and exceptions ───────────────────────────────────────────────

func TestRun_WarnSeverityDoesNotProduceErrors(t *testing.T) {
	root := siteWith(t, map[string]string{"src/assets/css/a.css": ".a{margin:2px}"})
	cfg := Config{Rules: map[string]RuleConfig{
		"no-px-literals": {Enabled: true, Severity: SeverityWarn},
	}}
	res, err := Run(root, cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 0 || len(res.Warnings) != 1 {
		t.Errorf("expected 0 errors / 1 warning, got %d / %d", len(res.Errors), len(res.Warnings))
	}
}

func TestRun_UnsetSeverityDefaultsToError(t *testing.T) {
	// A rule someone deliberately enabled should bite. Defaulting to warn would
	// let a site "adopt" a rule and get nothing from it.
	root := siteWith(t, map[string]string{"src/assets/css/a.css": ".a{margin:2px}"})
	res, err := Run(root, enabled("no-px-literals"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 1 || len(res.Warnings) != 0 {
		t.Errorf("expected 1 error / 0 warnings, got %d / %d", len(res.Errors), len(res.Warnings))
	}
}

func TestRun_ExceptionExemptsMatchingPaths(t *testing.T) {
	root := siteWith(t, map[string]string{
		"src/assets/css/tokens.css":      ":root{--hairline:1px}",
		"src/assets/css/pages/login.css": ".b{margin:8px}",
	})
	cfg := Config{Rules: map[string]RuleConfig{"no-px-literals": {
		Enabled:    true,
		Exceptions: []Exception{{Path: "src/assets/css/tokens.css", Reason: "hairline border token"}},
	}}}
	res, err := Run(root, cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 1 || res.Errors[0].File != "src/assets/css/pages/login.css" {
		t.Fatalf("expected only the non-exempt file to report, got: %v", res.Errors)
	}
}

func TestMatchPath_SubtreeAndSegmentGlobs(t *testing.T) {
	cases := []struct {
		pattern, rel string
		want         bool
	}{
		{"src/assets/css/tokens.css", "src/assets/css/tokens.css", true},
		{"src/assets/css/*.css", "src/assets/css/tokens.css", true},
		{"src/assets/css/*.css", "src/assets/css/pages/login.css", false},
		{"src/assets/css/**", "src/assets/css/pages/_shared/app.css", true},
		{"src/assets/css/**", "src/assets/js/a.js", false},
		{"**/vendor/**", "src/assets/js/vendor/lib.js", true},
		{"**/vendor/**", "src/assets/js/app.js", false},
	}
	for _, c := range cases {
		if got := matchPath(c.pattern, c.rel); got != c.want {
			t.Errorf("matchPath(%q, %q) = %v, want %v", c.pattern, c.rel, got, c.want)
		}
	}
}

// ── the three anti-decay properties ───────────────────────────────────────

// Property 1: an exception with no reason is REJECTED, not warned about.
func TestLoadConfig_RejectsAnExceptionWithNoReason(t *testing.T) {
	root := siteWith(t, map[string]string{
		"conventions.yaml": "rules:\n  no-px-literals:\n    enabled: true\n    exceptions:\n      - path: src/assets/css/a.css\n",
	})
	_, _, err := LoadConfig(root)
	if err == nil {
		t.Fatal("expected a reasonless exception to be rejected; an exception nobody justified is just a weakened rule")
	}
	if !strings.Contains(err.Error(), "reason is required") {
		t.Errorf("error should name the missing reason, got: %v", err)
	}
}

// Property 2: a rule enabled but matching nothing is an ERROR, not a pass.
// This is the failure mode that has repeatedly made a green gate in this fleet
// mean "nothing ran".
func TestRun_EnabledRuleThatScannedNothingIsAnError(t *testing.T) {
	root := siteWith(t, map[string]string{"README.md": "no assets here"})
	res, err := Run(root, enabled("no-px-literals"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 1 {
		t.Fatalf("expected a vacuous-gate error, got: %v", res.Errors)
	}
	if !strings.Contains(res.Errors[0].Detail, "matched no files") {
		t.Errorf("error should say the rule matched nothing, got: %q", res.Errors[0].Detail)
	}
}

func TestRun_ExceptionSwallowingTheWholeTreeIsCaughtAsVacuous(t *testing.T) {
	// The subtler half of property 2: a blanket exception makes a rule scan
	// zero files, which would otherwise look identical to a clean site.
	root := siteWith(t, map[string]string{"src/assets/css/a.css": ".a{margin:2px}"})
	cfg := Config{Rules: map[string]RuleConfig{"no-px-literals": {
		Enabled:    true,
		Exceptions: []Exception{{Path: "src/assets/css/**", Reason: "legacy, pending migration"}},
	}}}
	res, err := Run(root, cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Detail, "matched no files") {
		t.Fatalf("a blanket exception should surface as a vacuous gate, got: %v", res.Errors)
	}
}

// Property 3: an exception matching nothing is reported, so a baseline can
// actually burn down instead of becoming permanent.
func TestRun_ReportsStaleExceptions(t *testing.T) {
	root := siteWith(t, map[string]string{"src/assets/css/a.css": ".a{margin:1rem}"})
	cfg := Config{Rules: map[string]RuleConfig{"no-px-literals": {
		Enabled: true,
		Exceptions: []Exception{
			{Path: "src/assets/css/deleted-long-ago.css", Reason: "was legacy"},
		},
	}}}
	res, err := Run(root, cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.StaleExceptions) != 1 || !strings.Contains(res.StaleExceptions[0], "deleted-long-ago.css") {
		t.Fatalf("expected the stale exception to be reported, got: %v", res.StaleExceptions)
	}
}

// ── config validation ────────────────────────────────────────────────────

func TestLoadConfig_RejectsUnknownRuleName(t *testing.T) {
	// A typo'd rule name silently disabling the rule is the worst failure a gate
	// can have: the config looks intentional and nothing runs.
	root := siteWith(t, map[string]string{
		"conventions.yaml": "rules:\n  no-px-literalz:\n    enabled: true\n",
	})
	_, _, err := LoadConfig(root)
	if err == nil || !strings.Contains(err.Error(), "unknown rule") {
		t.Fatalf("expected an unknown-rule error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "no-px-literals") {
		t.Errorf("the error should list the known rules so the typo is obvious, got: %v", err)
	}
}

func TestLoadConfig_RejectsUnknownSeverity(t *testing.T) {
	root := siteWith(t, map[string]string{
		"conventions.yaml": "rules:\n  no-px-literals:\n    enabled: true\n    severity: whisper\n",
	})
	if _, _, err := LoadConfig(root); err == nil {
		t.Fatal("expected an unknown severity to be rejected")
	}
}

func TestLoadConfig_MissingFileIsNotAnError(t *testing.T) {
	cfg, path, err := LoadConfig(t.TempDir())
	if err != nil {
		t.Fatalf("a site with no conventions.yaml should not error: %v", err)
	}
	if path != "" {
		t.Errorf("expected an empty path to signal 'no config', got %q", path)
	}
	if len(cfg.Rules) != 0 {
		t.Errorf("expected no rules, got %v", cfg.Rules)
	}
}

func TestSaveConfig_RoundTripsAndKeepsReasons(t *testing.T) {
	root := t.TempDir()
	cfg := Config{Rules: map[string]RuleConfig{"no-px-literals": {
		Enabled:    true,
		Exceptions: []Exception{{Path: "src/assets/css/tokens.css", Reason: "hairline border"}},
	}}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, path, err := LoadConfig(root)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if path == "" {
		t.Fatal("expected the saved config to be found on reload")
	}
	ex := got.Rules["no-px-literals"].Exceptions
	if len(ex) != 1 || ex[0].Reason != "hairline border" {
		t.Errorf("exception reason did not round-trip: %v", ex)
	}
}

func TestRuleNames_AreDiscoverable(t *testing.T) {
	// `-list` is how a site learns what it can adopt, so the registry must not
	// be empty and every entry must carry documentation.
	names := RuleNames()
	if len(names) == 0 {
		t.Fatal("no rules registered")
	}
	for _, n := range names {
		if strings.TrimSpace(RuleDoc(n)) == "" {
			t.Errorf("rule %q has no Doc — it would appear in -list with no explanation", n)
		}
	}
}

// TestRun_FollowsInTreeSymlinks pins behaviour ffreis-forma depends on.
//
// That site aliases shared per-page stylesheets as symlinks into
// css/pages/_shared/ (git mode 120000), and compiler PR #134 exists
// specifically so assetusage recognises them. The scanner reads through
// os.Root for the gosec G122 fix, and a root-scoped handle COULD have refused
// symlinks — which would have silently stopped checking exactly the files the
// per-site test being replaced never checked either. Asserted rather than
// assumed.
func TestRun_FollowsInTreeSymlinks(t *testing.T) {
	root := siteWith(t, map[string]string{
		"src/assets/css/pages/_shared/appshell.css": ".shell{gap:6px}",
	})
	link := filepath.Join(root, "src", "assets", "css", "pages", "account.css")
	if err := os.Symlink(filepath.Join("_shared", "appshell.css"), link); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	res, err := Run(root, enabled("no-px-literals"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	files := map[string]bool{}
	for _, f := range res.Errors {
		files[f.File] = true
	}
	if !files["src/assets/css/pages/account.css"] {
		t.Errorf("symlinked stylesheet was not scanned — forma's css/pages/_shared/ aliases would go unchecked; got %v", res.Errors)
	}
	if !files["src/assets/css/pages/_shared/appshell.css"] {
		t.Errorf("symlink target was not scanned; got %v", res.Errors)
	}
}

func TestRun_ScansAnAbsoluteExtraRootOutsideTheSite(t *testing.T) {
	// `-dist` may point anywhere (CI builds into a temp dir), and the reported
	// path must still be readable rather than an empty prefix.
	root := siteWith(t, map[string]string{"src/assets/css/a.css": ".a{margin:1rem}"})
	outside := siteWith(t, map[string]string{"index.html": `<html><body><p>no main</p></body></html>`})

	res, err := Run(root, enabled("single-main-landmark"), []string{outside})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 1 {
		t.Fatalf("expected one finding from the outside root, got: %v", res.Errors)
	}
	if !strings.HasSuffix(res.Errors[0].File, "index.html") {
		t.Errorf("finding should name the file, got %q", res.Errors[0].File)
	}
}

// ── no-css-grid ───────────────────────────────────────────────────────────

func TestNoCSSGrid_FlagsGridPropertyDeclarations(t *testing.T) {
	root := siteWith(t, map[string]string{
		"src/assets/css/a.css": strings.Join([]string{
			".a{display:grid}",
			".b{display:inline-grid}",
			".c{grid-template-columns:1fr 1fr}",
			".d{grid-column:1 / 3}",
			".e{grid-area:main}",
			".f{grid-gap:1rem}",
		}, "\n"),
	})
	res, err := Run(root, enabled("no-css-grid"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 6 {
		t.Fatalf("expected 6 grid declarations flagged, got %d: %v", len(res.Errors), res.Errors)
	}
}

// TestNoCSSGrid_AllowsGridAsAName is carried over from the per-site test's own
// sanity case, and it is the half that matters.
//
// `.product-grid` is a FLEXBOX container in ffreis-forma. A rule that grepped
// for "grid" would flag it, and the natural response to that false positive is
// to rename the class or weaken the pattern — which is how a rule stops meaning
// anything.
func TestNoCSSGrid_AllowsGridAsAName(t *testing.T) {
	root := siteWith(t, map[string]string{
		"src/assets/css/a.css": ".product-grid{display:flex}\n" +
			".gallery-grid--wide{gap:1rem}\n" +
			"/* the grid of products reflows below 48rem */\n",
	})
	res, err := Run(root, enabled("no-css-grid"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Errorf("flagged \"grid\" used as a name or in prose: %v", res.Errors)
	}
}

func TestNoCSSGrid_IgnoresNonCSSFiles(t *testing.T) {
	// Unlike no-px-literals, this rule is CSS-only: "display: grid" inside a JS
	// string is not a stylesheet declaration, and flagging it would push sites
	// toward exceptions on behaviour files.
	root := siteWith(t, map[string]string{
		"src/assets/css/a.css": ".a{display:flex}",
		"src/assets/js/a.js":   `el.style.display = "grid";`,
	})
	res, err := Run(root, enabled("no-css-grid"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Errorf("expected CSS-only scanning, got: %v", res.Errors)
	}
}

func TestNoCSSGrid_IsNotEnabledByDefault(t *testing.T) {
	// The fleet default is Grid-for-2D / Flexbox-for-1D; this rule exists for
	// sites with a recorded reason to be Grid-free. An empty config must enforce
	// nothing at all.
	root := siteWith(t, map[string]string{"src/assets/css/a.css": ".a{display:grid}"})
	res, err := Run(root, Config{Rules: map[string]RuleConfig{}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) != 0 || len(res.Warnings) != 0 {
		t.Errorf("no rules enabled must enforce nothing, got %v / %v", res.Errors, res.Warnings)
	}
}

// TestNoCSSGrid_CatchesFormsThePerSiteRegexMissed pins the two concrete gaps in
// the hand-written rule this replaces, so absorbing it is a demonstrable
// improvement rather than a lateral move.
//
// Their pattern used `display\s*:\s*grid\b` (no inline-grid) and
// `grid-column\s*:` (no longhand). Both forms are live CSS Grid usage that
// would have passed the per-site test.
func TestNoCSSGrid_CatchesFormsThePerSiteRegexMissed(t *testing.T) {
	for _, decl := range []string{
		".a{display:inline-grid}",
		".a{grid-column-start:2}",
		".a{grid-column-end:4}",
		".a{grid-row-start:1}",
	} {
		root := siteWith(t, map[string]string{"src/assets/css/a.css": decl})
		res, err := Run(root, enabled("no-css-grid"), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res.Errors) == 0 {
			t.Errorf("%s was not flagged — the per-site regex missed this form too", decl)
		}
	}
}

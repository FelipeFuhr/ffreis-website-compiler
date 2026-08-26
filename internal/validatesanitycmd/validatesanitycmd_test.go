package validatesanitycmd

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/sitegen"
	"ffreis-website-compiler/internal/testutil"
)

// ── fixtures ────────────────────────────────────────────────────────────────

const (
	layoutTmpl = `{{define "layout"}}<!doctype html><html><head>{{template "head" .}}</head><body>{{template "page" .}}</body></html>{{end}}`
	headTmpl   = `{{define "head"}}<link rel="stylesheet" href="/css/main.css">{{end}}`
	pageTmpl   = `{{define "page"}}<h1>{{required (dig .SiteData "title") "missing title"}}</h1>{{end}}`
	mainCSS    = "body { color: #000; }\n"
)

// newSite builds the smallest website root that passes validation, so any
// failure in a test is caused by what that test changed.
func newSite(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "assets", "css"))
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "data"))
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "templates", "layout"))
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "templates", "partials"))
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "templates", "pages"))
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(root, "src", "assets", "css", "main.css"):            mainCSS,
		filepath.Join(root, "src", "data", "site.yaml"):                    "title: Probe Site\n",
		filepath.Join(root, "src", "data", "site.contract.yaml"):           "allowed:\n  - title\n",
		filepath.Join(root, "src", "templates", "layout", "base.gohtml"):   layoutTmpl,
		filepath.Join(root, "src", "templates", "partials", "head.gohtml"): headTmpl,
		filepath.Join(root, "src", "templates", "pages", "index.gohtml"):   pageTmpl,
	})
	return root
}

// writeCheck creates an executable sanity check that records that it ran.
func writeCheck(t *testing.T, checksRoot, name, body string, executable bool) string {
	t.Helper()
	testutil.MustMkdirAll(t, checksRoot)
	path := filepath.Join(checksRoot, name)
	mode := os.FileMode(0o600)
	if executable {
		mode = 0o700
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("write check %s: %v", path, err)
	}
	return path
}

// ── Run: the public surface ─────────────────────────────────────────────────

func TestRun_PassesOnAValidSite(t *testing.T) {
	if err := Run([]string{"-website-root", newSite(t)}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("a valid site must pass sanity validation: %v", err)
	}
}

func TestRun_FailsWhenSiteDataViolatesTheContract(t *testing.T) {
	root := newSite(t)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(root, "src", "data", "site.yaml"): "title: Probe Site\nundeclared_key: nope\n",
	})

	err := Run([]string{"-website-root", root}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("site data outside the contract must fail validation")
	}
}

func TestRun_FailsWhenTheWebsiteRootHasNoRecognisableLayout(t *testing.T) {
	err := Run([]string{"-website-root", t.TempDir()}, testutil.DiscardLogger())
	if err == nil || !strings.Contains(err.Error(), "could not resolve website directories") {
		t.Fatalf("err = %v, want an unresolvable-layout error", err)
	}
}

func TestRun_RejectsAnUnknownFlag(t *testing.T) {
	if err := Run([]string{"-not-a-real-flag"}, testutil.DiscardLogger()); err == nil {
		t.Fatal("an unknown flag must be an error, not silently ignored")
	}
}

// A nil logger is the zero value a caller can easily pass; it must not panic.
func TestRun_ToleratesANilLogger(t *testing.T) {
	if err := Run([]string{"-website-root", newSite(t)}, nil); err != nil {
		t.Fatalf("Run with a nil logger: %v", err)
	}
}

func TestRun_RunsSanityDirChecksAndFailsWhenOneDoes(t *testing.T) {
	root := newSite(t)
	checksRoot := filepath.Join(root, "sanity", "checks.d")
	writeCheck(t, checksRoot, "10-fails.sh", "#!/bin/bash\necho 'the failing detail'\nexit 1\n", true)

	err := Run([]string{"-website-root", root}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("a failing sanity check must fail the command")
	}
	if !strings.Contains(err.Error(), "the failing detail") {
		t.Errorf("err = %v, want the check's own output included so the failure is diagnosable", err)
	}
}

func TestRun_SkipsSanityDirChecksWhenDisabled(t *testing.T) {
	root := newSite(t)
	writeCheck(t, filepath.Join(root, "sanity", "checks.d"), "10-fails.sh", "#!/bin/bash\nexit 1\n", true)

	if err := Run([]string{"-website-root", root, "-run-sanity-dir-checks=false"}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("checks must not run when disabled: %v", err)
	}
}

func TestRun_SkipsAssetValidationWhenDisabled(t *testing.T) {
	root := newSite(t)
	// An asset no page references fails the asset check but nothing else.
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(root, "src", "assets", "css", "orphan.css"): "a{}\n",
	})

	if err := Run([]string{"-website-root", root}, testutil.DiscardLogger()); err == nil {
		t.Fatal("an orphaned asset should fail while -check-assets is on (control for the next assertion)")
	}
	if err := Run([]string{"-website-root", root, "-check-assets=false"}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("asset validation must be skippable: %v", err)
	}
}

// ── options ─────────────────────────────────────────────────────────────────

func TestParseValidateSanityOptions_Defaults(t *testing.T) {
	opts, err := parseValidateSanityOptions(nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.websiteRoot != "." {
		t.Errorf("websiteRoot = %q, want %q", opts.websiteRoot, ".")
	}
	if !opts.runSanityDirChecks || !opts.checkAssets {
		t.Errorf("runSanityDirChecks=%v checkAssets=%v, want both on by default",
			opts.runSanityDirChecks, opts.checkAssets)
	}
	if opts.sanityChecksDirName != "checks.d" {
		t.Errorf("sanityChecksDirName = %q, want checks.d", opts.sanityChecksDirName)
	}
}

// Surrounding whitespace in a path is almost always an accident from a shell
// variable, and an untrimmed value silently resolves to the wrong directory.
func TestParseValidateSanityOptions_TrimsPathFlags(t *testing.T) {
	opts, err := parseValidateSanityOptions([]string{
		"-assets-dir", "  /a  ", "-templates-dir", "\t/t\n", "-sanity-dir", " /s ",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.assetsDir != "/a" || opts.templatesDir != "/t" || opts.sanityDir != "/s" {
		t.Errorf("got (%q,%q,%q), want trimmed paths", opts.assetsDir, opts.templatesDir, opts.sanityDir)
	}
}

// ── sanity config ───────────────────────────────────────────────────────────

func TestLoadSanityConfig_DefaultsWhenThereIsNoConfig(t *testing.T) {
	want := sitegen.DefaultSanityConfig()

	for _, tc := range []struct{ name, dir string }{
		{"no sanity dir at all", ""},
		{"sanity dir with no config file", "use-temp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.dir
			if dir == "use-temp" {
				dir = t.TempDir()
			}
			config, source, err := loadSanityConfig(dir)
			if err != nil {
				t.Fatalf("loadSanityConfig: %v", err)
			}
			if source != "" {
				t.Errorf("source = %q, want empty when no config was found", source)
			}
			if config != want {
				t.Errorf("config = %+v, want the defaults %+v", config, want)
			}
		})
	}
}

func TestLoadSanityConfig_ReadsEitherFileExtension(t *testing.T) {
	for _, name := range []string{"sanity.yaml", "sanity.yml"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			testutil.WriteFiles(t, map[string]string{
				filepath.Join(dir, name): "version: 1\nchecks:\n  course_start_matches_first_session: false\n",
			})

			config, source, err := loadSanityConfig(dir)
			if err != nil {
				t.Fatalf("loadSanityConfig: %v", err)
			}
			if source != filepath.Join(dir, name) {
				t.Errorf("source = %q, want the %s that was read", source, name)
			}
			if config.CourseStartMatchesFirstSession {
				t.Error("the config disabled course_start_matches_first_session; it is still on")
			}
		})
	}
}

// An omitted key must keep its default rather than becoming false — the field
// is a *bool precisely so "unset" and "false" stay distinguishable.
func TestLoadSanityConfig_OmittedKeysKeepTheirDefaults(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(dir, "sanity.yaml"): "version: 1\nchecks:\n  course_duration_hours_matches_session_hours: false\n",
	})

	config, _, err := loadSanityConfig(dir)
	if err != nil {
		t.Fatalf("loadSanityConfig: %v", err)
	}
	defaults := sitegen.DefaultSanityConfig()
	if config.CourseStartMatchesFirstSession != defaults.CourseStartMatchesFirstSession {
		t.Error("an unmentioned check was changed; omitted keys must keep their default")
	}
	if config.CourseDurationHoursMatchesSessionHours {
		t.Error("the explicitly disabled check is still on")
	}
}

func TestLoadSanityConfig_MalformedYAMLIsAnError(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(dir, "sanity.yaml"): "checks: [unclosed\n",
	})

	if _, _, err := loadSanityConfig(dir); err == nil {
		t.Fatal("malformed sanity config must be an error, not silently ignored")
	}
}

func TestResolveValidateSanityPaths_SurfacesAMalformedConfig(t *testing.T) {
	root := newSite(t)
	sanityDir := filepath.Join(root, "sanity")
	testutil.MustMkdirAll(t, sanityDir)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(sanityDir, "sanity.yaml"): "checks: [unclosed\n",
	})

	_, _, _, _, _, err := resolveValidateSanityPaths(validateSanityOptions{websiteRoot: root})
	if err == nil || !strings.Contains(err.Error(), "loading sanity config") {
		t.Fatalf("err = %v, want the config failure to be surfaced with context", err)
	}
}

func TestResolveValidateSanityPaths_ExplicitDirsBypassLayoutResolution(t *testing.T) {
	opts := validateSanityOptions{websiteRoot: t.TempDir(), assetsDir: "/explicit/assets", templatesDir: "/explicit/templates"}

	assets, templates, sanityDir, _, _, err := resolveValidateSanityPaths(opts)
	if err != nil {
		t.Fatalf("explicit dirs should not need a resolvable layout: %v", err)
	}
	if assets != "/explicit/assets" || templates != "/explicit/templates" {
		t.Errorf("got (%q, %q), want the explicit paths", assets, templates)
	}
	if sanityDir != "" {
		t.Errorf("sanityDir = %q, want empty when no sanity/ directory exists", sanityDir)
	}
}

func TestResolveValidateSanityPaths_DefaultsToTheSanityDirWhenPresent(t *testing.T) {
	root := newSite(t)
	testutil.MustMkdirAll(t, filepath.Join(root, "sanity"))

	_, _, sanityDir, _, _, err := resolveValidateSanityPaths(validateSanityOptions{websiteRoot: root})
	if err != nil {
		t.Fatalf("resolveValidateSanityPaths: %v", err)
	}
	if sanityDir != filepath.Join(root, "sanity") {
		t.Errorf("sanityDir = %q, want the site's sanity/ directory", sanityDir)
	}
}

// ── which files count as runnable checks ────────────────────────────────────

func TestIsRunnableSanityCheckFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		file string
		mode os.FileMode
		want bool
	}{
		{"executable bit wins regardless of extension", "check", 0o755, true},
		{"executable with no extension", "10-verify", 0o700, true},
		{"non-executable shell script", "check.sh", 0o644, true},
		{"non-executable bash script", "check.bash", 0o644, true},
		{"non-executable python script", "check.py", 0o644, true},
		{"non-executable ruby script", "check.rb", 0o644, true},
		{"uppercase extension still matches", "CHECK.SH", 0o644, true},
		{"plain text is not runnable", "README.md", 0o644, false},
		{"yaml config is not runnable", "sanity.yaml", 0o644, false},
		{"no extension and not executable", "notes", 0o644, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRunnableSanityCheckFile(tc.file, tc.mode); got != tc.want {
				t.Errorf("isRunnableSanityCheckFile(%q, %v) = %v, want %v", tc.file, tc.mode, got, tc.want)
			}
		})
	}
}

func TestSanityCheckCommand_PicksAnInterpreterByExtension(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct{ name, path, wantContains string }{
		{"shell script runs under bash", "/x/check.sh", "bash"},
		{"bash script runs under bash", "/x/check.bash", "bash"},
		{"python script runs under python3", "/x/check.py", "python"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := sanityCheckCommand(ctx, tc.path)
			if err != nil {
				t.Skipf("interpreter not available in this environment: %v", err)
			}
			if !strings.Contains(cmd.Path, tc.wantContains) {
				t.Errorf("cmd.Path = %q, want it to contain %q", cmd.Path, tc.wantContains)
			}
			if cmd.Args[len(cmd.Args)-1] != tc.path {
				t.Errorf("args = %v, want the script path last", cmd.Args)
			}
		})
	}
}

func TestSanityCheckCommand_ExecutesAnUnknownExtensionDirectly(t *testing.T) {
	cmd, err := sanityCheckCommand(context.Background(), "/x/check")
	if err != nil {
		t.Fatalf("sanityCheckCommand: %v", err)
	}
	if cmd.Path != "/x/check" {
		t.Errorf("cmd.Path = %q, want the file executed directly", cmd.Path)
	}
}

// ── directory scanning ──────────────────────────────────────────────────────

func TestReadSortedDirEntries_ReturnsEntriesInNameOrder(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"30-c.sh", "10-a.sh", "20-b.sh"} {
		testutil.WriteFiles(t, map[string]string{filepath.Join(dir, name): "#!/bin/bash\n"})
	}

	entries, err := readSortedDirEntries(dir)
	if err != nil {
		t.Fatalf("readSortedDirEntries: %v", err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	want := []string{"10-a.sh", "20-b.sh", "30-c.sh"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v — the numeric prefix convention relies on sorting", got, want)
		}
	}
}

func TestReadSortedDirEntries_MissingDirectoryIsAnError(t *testing.T) {
	_, err := readSortedDirEntries(filepath.Join(t.TempDir(), "absent"))
	if err == nil || !strings.Contains(err.Error(), "reading sanity checks directory") {
		t.Fatalf("err = %v, want a contextual read error", err)
	}
}

func TestResolveRunnableSanityCheck_SkipsDirectoriesAndNonRunnableFiles(t *testing.T) {
	checksRoot := t.TempDir()
	testutil.MustMkdirAll(t, filepath.Join(checksRoot, "a-subdir"))
	writeCheck(t, checksRoot, "notes.md", "not a check\n", false)
	writeCheck(t, checksRoot, "10-real.sh", "#!/bin/bash\n", true)

	entries, err := readSortedDirEntries(checksRoot)
	if err != nil {
		t.Fatalf("readSortedDirEntries: %v", err)
	}

	var runnable []string
	for _, entry := range entries {
		path, ok, err := resolveRunnableSanityCheck(checksRoot, entry)
		if err != nil {
			t.Fatalf("resolveRunnableSanityCheck(%s): %v", entry.Name(), err)
		}
		if ok {
			runnable = append(runnable, filepath.Base(path))
		}
	}
	if len(runnable) != 1 || runnable[0] != "10-real.sh" {
		t.Errorf("runnable = %v, want only the shell script", runnable)
	}
}

// ── running the checks ──────────────────────────────────────────────────────

func TestRunSanityChecksFromDir_NoOpWhenThereIsNothingToRun(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	for _, tc := range []struct{ name, sanityDir string }{
		{"no sanity dir configured", ""},
		{"sanity dir without a checks.d", root},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := runSanityChecksFromDir(ctx, root, tc.sanityDir, "checks.d", "/compiler", testutil.DiscardLogger())
			if err != nil {
				t.Errorf("expected a silent no-op, got: %v", err)
			}
		})
	}
}

func TestRunSanityChecksFromDir_RunsChecksInNameOrderAndStopsAtTheFirstFailure(t *testing.T) {
	root := t.TempDir()
	sanityDir := filepath.Join(root, "sanity")
	checksRoot := filepath.Join(sanityDir, "checks.d")
	marker := filepath.Join(root, "ran.txt")

	writeCheck(t, checksRoot, "10-first.sh", "#!/bin/bash\necho first >> "+marker+"\n", true)
	writeCheck(t, checksRoot, "20-fails.sh", "#!/bin/bash\necho second >> "+marker+"\nexit 3\n", true)
	writeCheck(t, checksRoot, "30-never.sh", "#!/bin/bash\necho third >> "+marker+"\n", true)

	err := runSanityChecksFromDir(context.Background(), root, sanityDir, "checks.d", "/compiler", testutil.DiscardLogger())
	if err == nil {
		t.Fatal("a failing check must fail the run")
	}

	raw, readErr := os.ReadFile(marker)
	if readErr != nil {
		t.Fatalf("reading marker: %v", readErr)
	}
	ran := strings.Fields(string(raw))
	want := []string{"first", "second"}
	if len(ran) != len(want) {
		t.Fatalf("checks ran = %v, want %v — the run must stop at the first failure", ran, want)
	}
	for i := range want {
		if ran[i] != want[i] {
			t.Errorf("checks ran = %v, want %v (name order)", ran, want)
		}
	}
}

// The contract with a check author is these four variables; a check that reads
// WEBSITE_ROOT should not silently receive an empty string.
func TestRunSanityCheck_ExportsTheDocumentedEnvironment(t *testing.T) {
	root := t.TempDir()
	sanityDir := filepath.Join(root, "sanity")
	checksRoot := filepath.Join(sanityDir, "checks.d")
	out := filepath.Join(root, "env.txt")

	body := "#!/bin/bash\n{\n" +
		"echo \"WEBSITE_ROOT=$WEBSITE_ROOT\"\n" +
		"echo \"SANITY_DIR=$SANITY_DIR\"\n" +
		"echo \"SANITY_CHECKS_DIR=$SANITY_CHECKS_DIR\"\n" +
		"echo \"WEBSITE_COMPILER_EXE=$WEBSITE_COMPILER_EXE\"\n" +
		"echo \"PWD=$PWD\"\n" +
		"} > " + out + "\n"
	checkPath := writeCheck(t, checksRoot, "10-env.sh", body, true)

	if err := runSanityCheck(context.Background(), root, sanityDir, checksRoot, "/path/to/compiler", checkPath, testutil.DiscardLogger()); err != nil {
		t.Fatalf("runSanityCheck: %v", err)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading env dump: %v", err)
	}
	got := string(raw)
	for _, want := range []string{
		"WEBSITE_ROOT=" + root,
		"SANITY_DIR=" + sanityDir,
		"SANITY_CHECKS_DIR=" + checksRoot,
		"WEBSITE_COMPILER_EXE=/path/to/compiler",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in the check's environment; got:\n%s", want, got)
		}
	}
	// Checks are written relative to the website root, so the working directory matters.
	if !strings.Contains(got, "PWD="+root) {
		t.Errorf("check ran in the wrong directory; got:\n%s", got)
	}
}

func TestRunSanityCheck_ReportsAMissingInterpreter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH manipulation differs on windows")
	}
	root := t.TempDir()
	checksRoot := filepath.Join(root, "sanity", "checks.d")
	checkPath := writeCheck(t, checksRoot, "10-ruby.rb", "puts 'hi'\n", false)

	t.Setenv("PATH", t.TempDir()) // an empty PATH: no ruby anywhere
	err := runSanityCheck(context.Background(), root, filepath.Dir(checksRoot), checksRoot, "/compiler", checkPath, testutil.DiscardLogger())
	if err == nil || !strings.Contains(err.Error(), "ruby not found") {
		t.Fatalf("err = %v, want a clear missing-interpreter error", err)
	}
}

package validatesanitycmd

import (
	"bytes"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/testutil"
)

const (
	sanityLayoutTmpl = `{{define "layout"}}{{template "page" .}}{{end}}`
	sanityPageTmpl   = `{{define "page"}}ok{{end}}`
	flagWebsiteRoot  = "-website-root"
)

// setupSanityWebsiteRoot builds the minimal <root>/src/{assets,templates,data}
// layout that resolveValidateSanityPaths needs, with an empty (vacuous)
// site.contract.yaml so contract validation never gets in the way of the
// checks.d fixtures under test.
func setupSanityWebsiteRoot(t *testing.T) string {
	t.Helper()
	websiteRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(websiteRoot, "src", "assets"),
		filepath.Join(websiteRoot, "src", "templates", "layout"),
		filepath.Join(websiteRoot, "src", "templates", "pages"),
		filepath.Join(websiteRoot, "src", "data"),
	} {
		testutil.MustMkdirAll(t, dir)
	}
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "data", "site.contract.yaml"):         "",
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"): sanityLayoutTmpl,
		filepath.Join(websiteRoot, "src", "templates", "pages", "index.gohtml"): sanityPageTmpl,
	})
	return websiteRoot
}

func writeCheckScript(t *testing.T, checksDir, name, body string, executable bool) string {
	t.Helper()
	path := filepath.Join(checksDir, name)
	mode := os.FileMode(0o644)
	if executable {
		mode = 0o755
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("write check script %s: %v", path, err)
	}
	return path
}

func TestRun_NoSanityDirIsANoop(t *testing.T) {
	websiteRoot := setupSanityWebsiteRoot(t)

	if err := Run([]string{flagWebsiteRoot, websiteRoot, "-check-assets=false"}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("expected success with no sanity dir present, got %v", err)
	}
}

func TestRun_PassingCheckScriptSucceeds(t *testing.T) {
	websiteRoot := setupSanityWebsiteRoot(t)
	checksDir := filepath.Join(websiteRoot, "sanity", "checks.d")
	testutil.MustMkdirAll(t, checksDir)
	writeCheckScript(t, checksDir, "01-pass.sh", "#!/bin/bash\nexit 0\n", true)

	if err := Run([]string{flagWebsiteRoot, websiteRoot, "-check-assets=false"}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("expected a passing checks.d script to succeed, got %v", err)
	}
}

func TestRun_FailingCheckScriptPropagatesError(t *testing.T) {
	websiteRoot := setupSanityWebsiteRoot(t)
	checksDir := filepath.Join(websiteRoot, "sanity", "checks.d")
	testutil.MustMkdirAll(t, checksDir)
	failingScript := writeCheckScript(t, checksDir, "01-fail.sh", "#!/bin/bash\necho boom-stdout\nexit 7\n", true)

	err := Run([]string{flagWebsiteRoot, websiteRoot, "-check-assets=false"}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected a failing checks.d script to fail Run")
	}
	if !strings.Contains(err.Error(), "sanity check failed") {
		t.Fatalf("expected 'sanity check failed' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), failingScript) {
		t.Fatalf("expected the failing script path in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "boom-stdout") {
		t.Fatalf("expected combined output surfaced in error, got: %v", err)
	}
}

func TestRun_NonExecutableUnknownExtensionIsSkipped(t *testing.T) {
	websiteRoot := setupSanityWebsiteRoot(t)
	checksDir := filepath.Join(websiteRoot, "sanity", "checks.d")
	testutil.MustMkdirAll(t, checksDir)
	// Neither executable nor a recognized interpreter extension: must be
	// silently skipped, not fed to any interpreter.
	writeCheckScript(t, checksDir, "README.md", "this is not a check\n", false)

	if err := Run([]string{flagWebsiteRoot, websiteRoot, "-check-assets=false"}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("expected non-executable unknown-extension file to be skipped, got %v", err)
	}
}

func TestRun_NonExecutableShellExtensionStillRunsViaInterpreter(t *testing.T) {
	websiteRoot := setupSanityWebsiteRoot(t)
	checksDir := filepath.Join(websiteRoot, "sanity", "checks.d")
	testutil.MustMkdirAll(t, checksDir)
	// No +x bit, but the .sh extension alone is enough to make it runnable via bash.
	writeCheckScript(t, checksDir, "01-interpreted.sh", "#!/bin/bash\nexit 1\n", false)

	err := Run([]string{flagWebsiteRoot, websiteRoot, "-check-assets=false"}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected the non-executable .sh file to still be run (and fail) via bash")
	}
	if !strings.Contains(err.Error(), "sanity check failed") {
		t.Fatalf("expected 'sanity check failed' in error, got: %v", err)
	}
}

func TestRun_ExecutableWithoutRecognizedExtensionRunsDirectly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shebang-based direct execution is not portable to windows")
	}
	websiteRoot := setupSanityWebsiteRoot(t)
	checksDir := filepath.Join(websiteRoot, "sanity", "checks.d")
	testutil.MustMkdirAll(t, checksDir)
	// +x bit set, no recognized extension: exercises the sanityCheckCommand
	// fallback that execs the file directly (relying on its own shebang).
	writeCheckScript(t, checksDir, "check_custom", "#!/bin/bash\nexit 0\n", true)

	if err := Run([]string{flagWebsiteRoot, websiteRoot, "-check-assets=false"}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("expected the executable no-extension check to run directly and pass, got %v", err)
	}
}

func TestRun_CheckScriptStderrIsCapturedInLog(t *testing.T) {
	websiteRoot := setupSanityWebsiteRoot(t)
	checksDir := filepath.Join(websiteRoot, "sanity", "checks.d")
	testutil.MustMkdirAll(t, checksDir)
	writeCheckScript(t, checksDir, "01-warn.sh", "#!/bin/bash\necho warned-on-stderr 1>&2\nexit 0\n", true)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	if err := Run([]string{flagWebsiteRoot, websiteRoot, "-check-assets=false"}, logger); err != nil {
		t.Fatalf("expected a passing script with stderr output to still succeed, got %v", err)
	}
	if !strings.Contains(buf.String(), "warned-on-stderr") {
		t.Fatalf("expected stderr output to be logged, got log: %s", buf.String())
	}
}

func TestRun_SubdirectoriesInsideChecksDAreSkipped(t *testing.T) {
	websiteRoot := setupSanityWebsiteRoot(t)
	checksDir := filepath.Join(websiteRoot, "sanity", "checks.d")
	testutil.MustMkdirAll(t, filepath.Join(checksDir, "helpers"))
	writeCheckScript(t, filepath.Join(checksDir, "helpers"), "would-fail.sh", "#!/bin/bash\nexit 1\n", true)
	writeCheckScript(t, checksDir, "01-pass.sh", "#!/bin/bash\nexit 0\n", true)

	if err := Run([]string{flagWebsiteRoot, websiteRoot, "-check-assets=false"}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("expected the checks.d subdirectory to be skipped (not treated as a check), got %v", err)
	}
}

func TestRun_ChecksRunInSortedOrderWithExpectedEnv(t *testing.T) {
	websiteRoot := setupSanityWebsiteRoot(t)
	sanityDir := filepath.Join(websiteRoot, "sanity")
	checksDir := filepath.Join(sanityDir, "checks.d")
	testutil.MustMkdirAll(t, checksDir)
	orderLog := filepath.Join(websiteRoot, "order.log")

	// Each script appends its own name to order.log, resolved relative to
	// cmd.Dir (websiteRoot), and asserts the env vars the command wires in.
	script := `#!/bin/bash
set -euo pipefail
test -n "$WEBSITE_ROOT"
test -n "$SANITY_DIR"
test -n "$SANITY_CHECKS_DIR"
echo "$1" >> order.log
`
	writeCheckScript(t, checksDir, "10-second.sh", strings.Replace(script, "$1", "second", 1), true)
	writeCheckScript(t, checksDir, "01-first.sh", strings.Replace(script, "$1", "first", 1), true)

	if err := Run([]string{flagWebsiteRoot, websiteRoot, "-check-assets=false"}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("expected sorted checks to pass, got %v", err)
	}

	raw, err := os.ReadFile(orderLog)
	if err != nil {
		t.Fatalf("reading order log: %v", err)
	}
	got := strings.TrimSpace(string(raw))
	if got != "first\nsecond" {
		t.Fatalf("expected checks to run in filename-sorted order (first, second), got: %q", got)
	}
}

func TestRun_RunSanityDirChecksFalseSkipsExecutionEntirely(t *testing.T) {
	websiteRoot := setupSanityWebsiteRoot(t)
	checksDir := filepath.Join(websiteRoot, "sanity", "checks.d")
	testutil.MustMkdirAll(t, checksDir)
	// This script would fail if executed; disabling checks.d execution must
	// bypass it entirely rather than merely tolerating failure.
	writeCheckScript(t, checksDir, "01-would-fail.sh", "#!/bin/bash\nexit 1\n", true)

	err := Run([]string{
		flagWebsiteRoot, websiteRoot,
		"-check-assets=false",
		"-run-sanity-dir-checks=false",
	}, testutil.DiscardLogger())
	if err != nil {
		t.Fatalf("expected -run-sanity-dir-checks=false to skip checks.d entirely, got %v", err)
	}
}

func TestRun_CustomSanityChecksDirName(t *testing.T) {
	websiteRoot := setupSanityWebsiteRoot(t)
	sanityDir := filepath.Join(websiteRoot, "sanity")
	customChecksDir := filepath.Join(sanityDir, "custom-checks")
	testutil.MustMkdirAll(t, customChecksDir)
	writeCheckScript(t, customChecksDir, "01-fail.sh", "#!/bin/bash\nexit 1\n", true)

	// Default "checks.d" dir does not exist, so with the default checks dir
	// name Run must be a no-op; with the override it must pick up and run
	// (and fail on) the script under the custom directory name.
	if err := Run([]string{flagWebsiteRoot, websiteRoot, "-check-assets=false"}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("expected default checks dir name to find nothing under custom-checks, got %v", err)
	}

	err := Run([]string{
		flagWebsiteRoot, websiteRoot,
		"-check-assets=false",
		"-sanity-checks-dir", "custom-checks",
	}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected the custom checks dir override to find and run the failing script")
	}
	if !strings.Contains(err.Error(), "sanity check failed") {
		t.Fatalf("expected 'sanity check failed' in error, got: %v", err)
	}
}

func TestRun_ExplicitSanityDirFlagOverridesDefault(t *testing.T) {
	websiteRoot := setupSanityWebsiteRoot(t)
	altSanityDir := t.TempDir()
	checksDir := filepath.Join(altSanityDir, "checks.d")
	testutil.MustMkdirAll(t, checksDir)
	writeCheckScript(t, checksDir, "01-fail.sh", "#!/bin/bash\nexit 3\n", true)

	err := Run([]string{
		flagWebsiteRoot, websiteRoot,
		"-check-assets=false",
		"-sanity-dir", altSanityDir,
	}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected the explicit -sanity-dir to be honored and its failing check to propagate")
	}
	if !strings.Contains(err.Error(), "sanity check failed") {
		t.Fatalf("expected 'sanity check failed' in error, got: %v", err)
	}
}

func TestRun_SanityConfigDisablesCourseStartCheck(t *testing.T) {
	websiteRoot := setupSanityWebsiteRoot(t)
	// Mismatched start_text vs. the first cronograma session date triggers
	// CourseStartMatchesFirstSession under the default config.
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "data", "site.yaml"): strings.TrimSpace(`
courses:
  demo:
    variants:
      early:
        start_text: "2026-01-11"
        duration_hours: 4
        cronograma:
          sessions:
            - date: "2026-01-10"
              hours: 4
`) + "\n",
	})

	err := Run([]string{flagWebsiteRoot, websiteRoot, "-check-assets=false"}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected the default sanity config to catch the start-date mismatch")
	}
	if !strings.Contains(err.Error(), "does not match the first cronograma session date") {
		t.Fatalf("expected start-date mismatch error, got: %v", err)
	}

	sanityDir := filepath.Join(websiteRoot, "sanity")
	testutil.MustMkdirAll(t, sanityDir)
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(sanityDir, "sanity.yaml"): "version: 1\nchecks:\n  course_start_matches_first_session: false\n",
	})

	if err := Run([]string{flagWebsiteRoot, websiteRoot, "-check-assets=false"}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("expected disabling course_start_matches_first_session via sanity.yaml to suppress the mismatch, got %v", err)
	}
}

func TestRun_PythonCheckScriptRunsViaInterpreter(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available in PATH")
	}
	websiteRoot := setupSanityWebsiteRoot(t)
	checksDir := filepath.Join(websiteRoot, "sanity", "checks.d")
	testutil.MustMkdirAll(t, checksDir)
	writeCheckScript(t, checksDir, "01-check.py", "import sys\nsys.exit(1)\n", true)

	err := Run([]string{flagWebsiteRoot, websiteRoot, "-check-assets=false"}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected the failing .py check to fail via python3")
	}
	if !strings.Contains(err.Error(), "sanity check failed") {
		t.Fatalf("expected 'sanity check failed' in error, got: %v", err)
	}
}

func TestRun_RubyCheckScriptRunsViaInterpreter(t *testing.T) {
	if _, err := exec.LookPath("ruby"); err != nil {
		t.Skip("ruby not available in PATH")
	}
	websiteRoot := setupSanityWebsiteRoot(t)
	checksDir := filepath.Join(websiteRoot, "sanity", "checks.d")
	testutil.MustMkdirAll(t, checksDir)
	writeCheckScript(t, checksDir, "01-check.rb", "exit 0\n", true)

	if err := Run([]string{flagWebsiteRoot, websiteRoot, "-check-assets=false"}, testutil.DiscardLogger()); err != nil {
		t.Fatalf("expected the passing .rb check to succeed via ruby, got %v", err)
	}
}

func TestRun_CheckAssetsDefaultValidatesLocalAssetUsage(t *testing.T) {
	websiteRoot := setupSanityWebsiteRoot(t)
	// Rewrite the page template to reference a stylesheet that doesn't exist,
	// so the default (-check-assets=true) path fails via assetusage.Validate.
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(websiteRoot, "src", "templates", "layout", "base.gohtml"): `{{define "layout"}}<link rel="stylesheet" href="/css/missing.css">{{template "page" .}}{{end}}`,
	})

	err := Run([]string{flagWebsiteRoot, websiteRoot}, testutil.DiscardLogger())
	if err == nil {
		t.Fatal("expected default check-assets=true to fail on a missing/unreachable local asset")
	}
	if !strings.Contains(err.Error(), "validating local css/js asset usage") {
		t.Fatalf("expected asset usage validation error, got: %v", err)
	}
}

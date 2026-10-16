package versioncmd

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it. Run() writes its output directly to os.Stdout
// (matching exportsitedatacmd's JSON-output convention), so testing it
// requires swapping the file descriptor rather than passing an io.Writer.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("reading captured stdout: %v", err)
	}
	return buf.String()
}

// TestNewManifest_JSONShape locks in the manifest's JSON shape: it must
// parse as valid JSON with the keys a feature-detecting caller needs, and
// each list must be populated from a real registry rather than empty or
// invented.
func TestNewManifest_JSONShape(t *testing.T) {
	manifest := NewManifest()

	out, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("manifest did not parse as valid JSON: %v", err)
	}

	for _, key := range []string{"version", "commands", "capabilities", "sections", "template_functions"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("manifest JSON missing expected key %q; got keys: %v", key, decoded)
		}
	}

	if v, _ := decoded["version"].(string); v == "" {
		t.Error("manifest \"version\" is empty")
	}

	// Spot-check specific, real entries a consumer CI script would actually
	// feature-detect for — see internal/buildcmd/options.go for the flags
	// these correspond to. Not exhaustive; just enough to catch the list
	// going empty or the wrong field being wired up.
	assertContains(t, manifest.Commands, "validate-assets")
	assertContains(t, manifest.Commands, "validate-sanity")
	assertContains(t, manifest.Commands, "version")
	assertContains(t, manifest.Capabilities, "content-source-dev")
	assertContains(t, manifest.Capabilities, "listings-detail-pages")
	assertContains(t, manifest.Capabilities, "sitemap-default-on")
	assertContains(t, manifest.Capabilities, "per-page-css")
	assertContains(t, manifest.Sections, "blog")
	assertContains(t, manifest.TemplateFunctions, "required")
}

// TestCommands_MatchCLISwitch is the drift guard for the one list in this
// package that cannot be introspected at runtime: internal/cli/cli.go imports
// versioncmd to wire up "version", so versioncmd importing cli back would be
// an import cycle, leaving `commands` hand-maintained. Rather than trusting a
// "keep this in sync" comment, this parses cli.go's dispatch switch and
// asserts the two agree — so adding a subcommand without updating the
// manifest fails the build instead of silently shipping an inaccurate
// capability manifest to every consumer.
func TestCommands_MatchCLISwitch(t *testing.T) {
	const cliPath = "../cli/cli.go"

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, cliPath, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", cliPath, err)
	}

	fromCLI := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		clause, ok := n.(*ast.CaseClause)
		if !ok || len(clause.List) == 0 {
			return true
		}
		// The canonical command is the first literal in each case clause;
		// any further literals are aliases ("compile" for build, "web" for
		// serve) which the manifest deliberately omits.
		lit, ok := clause.List[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		name, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Fatalf("unquoting case literal %s: %v", lit.Value, err)
		}
		// "help" and its flag spellings are a usage pseudo-command, not a
		// dispatched subcommand.
		if name == "help" || strings.HasPrefix(name, "-") {
			return true
		}
		fromCLI[name] = true
		return true
	})

	if len(fromCLI) == 0 {
		t.Fatalf("parsed no command cases out of %s — the parser, not the manifest, is broken", cliPath)
	}

	inManifest := map[string]bool{}
	for _, c := range commands {
		inManifest[c] = true
	}

	for name := range fromCLI {
		if !inManifest[name] {
			t.Errorf("cli.go dispatches %q but the version manifest's commands list omits it — add it to `commands`", name)
		}
	}
	for name := range inManifest {
		if !fromCLI[name] {
			t.Errorf("version manifest claims command %q but cli.go has no case for it — remove it from `commands`", name)
		}
	}
}

// TestResolvedVersion_HonorsLdflagsOverride proves the version string is
// wired to whatever a release build injects via -ldflags rather than being
// hardcoded: overriding the package var changes what's reported.
func TestResolvedVersion_HonorsLdflagsOverride(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	Version = "9.9.9-test-override"
	if got := resolvedVersion(); got != "9.9.9-test-override" {
		t.Errorf("resolvedVersion() = %q, want the -ldflags-style override", got)
	}

	manifest := NewManifest()
	if manifest.Version != "9.9.9-test-override" {
		t.Errorf("Manifest.Version = %q, want the override to flow through NewManifest", manifest.Version)
	}
}

// TestResolvedVersion_FallbackIsNeverEmpty exercises the no-override path.
// It deliberately does not assert an exact value: the VCS-derived pseudo
// -version depends on the checkout this test runs in (plain clone vs. git
// worktree — see resolvedVersion's doc comment), so the only
// environment-independent guarantee is that it never comes back empty.
func TestResolvedVersion_FallbackIsNeverEmpty(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	Version = ""
	got := resolvedVersion()
	if got == "" {
		t.Error("resolvedVersion() returned an empty string with no -ldflags override set")
	}
}

// TestRun_JSON verifies `version --json` prints exactly one JSON object
// (parseable, on its own) with the manifest's expected keys — the shape a
// consumer CI script would jq/python into for feature detection.
func TestRun_JSON(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })
	Version = "1.2.3-run-json-test"

	out := captureStdout(t, func() {
		if err := Run([]string{"--json"}, nil); err != nil {
			t.Fatalf("Run(--json): %v", err)
		}
	})

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("version --json output did not parse as valid JSON: %v\noutput: %s", err, out)
	}
	for _, key := range []string{"version", "commands", "capabilities", "sections", "template_functions"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("version --json output missing expected key %q", key)
		}
	}
	if v, _ := decoded["version"].(string); v != "1.2.3-run-json-test" {
		t.Errorf("version --json \"version\" = %q, want the wired override to appear verbatim", v)
	}
}

// TestRun_PlainText verifies the non---json default just prints the version
// string, matching the CLI convention other subcommands use for
// human-oriented (non-JSON) output.
func TestRun_PlainText(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })
	Version = "1.2.3-plain-test"

	out := captureStdout(t, func() {
		if err := Run(nil, nil); err != nil {
			t.Fatalf("Run(): %v", err)
		}
	})

	if got := strings.TrimSpace(out); got != "1.2.3-plain-test" {
		t.Errorf("Run() plain output = %q, want just the version string", got)
	}
}

func assertContains(t *testing.T, list []string, want string) {
	t.Helper()
	for _, got := range list {
		if got == want {
			return
		}
	}
	t.Errorf("expected %q in %v", want, list)
}

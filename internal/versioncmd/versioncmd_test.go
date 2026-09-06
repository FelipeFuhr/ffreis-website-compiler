package versioncmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
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
	assertContains(t, manifest.Sections, "blog")
	assertContains(t, manifest.TemplateFunctions, "required")
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

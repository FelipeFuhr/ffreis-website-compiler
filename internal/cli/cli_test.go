package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it. Run dispatches to subcommands that print directly
// to os.Stdout, so observing them means swapping the file descriptor.
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

// withArgs runs fn with os.Args set to the given argv, restoring it after.
// Run reads os.Args directly rather than taking them as a parameter.
func withArgs(t *testing.T, argv []string, fn func()) {
	t.Helper()
	orig := os.Args
	t.Cleanup(func() { os.Args = orig })
	os.Args = argv
	fn()
}

// TestRun_DispatchesVersionJSON exercises the dispatch switch end to end for
// the `version` subcommand. The switch is the one place the capability
// manifest's hand-maintained command list is mirrored (see
// internal/versioncmd's TestCommands_MatchCLISwitch, which parses this file),
// so it is worth proving the wiring actually reaches versioncmd rather than
// only that the two lists agree textually.
func TestRun_DispatchesVersionJSON(t *testing.T) {
	out := captureStdout(t, func() {
		withArgs(t, []string{"website-compiler", "version", "--json"}, func() {
			Run("website-compiler")
		})
	})

	var manifest map[string]any
	if err := json.Unmarshal([]byte(out), &manifest); err != nil {
		t.Fatalf("version --json did not emit parseable JSON through the CLI: %v\noutput: %s", err, out)
	}
	for _, key := range []string{"version", "commands", "capabilities", "sections", "template_functions"} {
		if _, ok := manifest[key]; !ok {
			t.Errorf("manifest emitted via the CLI is missing key %q", key)
		}
	}
}

// TestRun_VersionPlainText covers the non-JSON path through the same dispatch.
func TestRun_VersionPlainText(t *testing.T) {
	out := captureStdout(t, func() {
		withArgs(t, []string{"website-compiler", "version"}, func() {
			Run("website-compiler")
		})
	})

	if strings.TrimSpace(out) == "" {
		t.Error("version printed nothing through the CLI")
	}
	if strings.Contains(out, "{") {
		t.Errorf("version without --json should print a bare version string, got %q", out)
	}
}

// TestRun_HelpListsEveryDispatchedCommand keeps the usage text honest. A
// subcommand that dispatches but is absent from `help` is undiscoverable, and
// the usage block is hand-written, so nothing else catches the omission.
func TestRun_HelpListsEveryDispatchedCommand(t *testing.T) {
	out := captureStdout(t, func() {
		withArgs(t, []string{"website-compiler", "help"}, func() {
			Run("website-compiler")
		})
	})

	for _, cmd := range []string{
		"build",
		"serve",
		"export-site-data",
		"validate-site-data",
		"validate-assets",
		"validate-css",
		"validate-sanity",
		"check-lang-parity",
		"test-output",
		"version",
	} {
		if !strings.Contains(out, cmd) {
			t.Errorf("usage text does not mention the %q command", cmd)
		}
	}
}

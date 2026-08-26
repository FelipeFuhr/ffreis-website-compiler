package cli

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The command table is the compiler's entire public surface. A command that
// silently stops being routed — a renamed case, a dropped alias — is invisible
// until someone's script breaks, so every documented name is pinned here.

func discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
}

// captureOutput redirects stdout and stderr for the duration of fn.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	fn()
	os.Stdout, os.Stderr = origOut, origErr
	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("closing pipe: %v", closeErr)
	}

	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, readErr := r.Read(buf)
		sb.Write(buf[:n])
		if readErr != nil {
			break
		}
	}
	return sb.String()
}

func TestDispatch_NoCommandPrintsUsageAndFails(t *testing.T) {
	var code int
	out := captureOutput(t, func() { code = Dispatch("website-compiler", nil, discard()) })

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(out, "Usage:") {
		t.Errorf("expected usage output, got:\n%s", out)
	}
}

func TestDispatch_HelpPrintsUsageAndSucceeds(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			var code int
			out := captureOutput(t, func() { code = Dispatch("website-compiler", []string{arg}, discard()) })

			if code != 0 {
				t.Errorf("exit code = %d, want 0 — asking for help is not an error", code)
			}
			if !strings.Contains(out, "Commands:") {
				t.Errorf("expected the command list, got:\n%s", out)
			}
		})
	}
}

func TestDispatch_UnknownCommandNamesItAndFails(t *testing.T) {
	var code int
	out := captureOutput(t, func() { code = Dispatch("website-compiler", []string{"buidl"}, discard()) })

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(out, "unknown command: buidl") {
		t.Errorf("expected the mistyped command echoed back, got:\n%s", out)
	}
	if !strings.Contains(out, "Usage:") {
		t.Errorf("expected usage after an unknown command, got:\n%s", out)
	}
}

// Every documented command must be routed. Each is invoked with a flag that
// makes it fail fast — the point is that it REACHED the sub-command, which an
// unrouted name never would (that path prints "unknown command" instead).
func TestDispatch_EveryDocumentedCommandIsRouted(t *testing.T) {
	for _, cmd := range []string{
		"build", "compile",
		"serve", "web",
		"validate-site-data",
		"validate-assets",
		"export-site-data",
		"validate-sanity",
		"check-lang-parity",
		"test-output",
	} {
		t.Run(cmd, func(t *testing.T) {
			out := captureOutput(t, func() {
				Dispatch("website-compiler", []string{cmd, "-definitely-not-a-real-flag"}, discard())
			})
			if strings.Contains(out, "unknown command") {
				t.Errorf("%q is not routed by the command table", cmd)
			}
		})
	}
}

// Aliases must reach the same command, not merely be accepted.
func TestDispatch_AliasesReachTheSameCommand(t *testing.T) {
	for _, pair := range [][2]string{{"build", "compile"}, {"serve", "web"}} {
		t.Run(pair[0]+"/"+pair[1], func(t *testing.T) {
			outA := captureOutput(t, func() {
				Dispatch("x", []string{pair[0], "-definitely-not-a-real-flag"}, discard())
			})
			outB := captureOutput(t, func() {
				Dispatch("x", []string{pair[1], "-definitely-not-a-real-flag"}, discard())
			})
			if outA != outB {
				t.Errorf("%q and %q produced different output; they should be aliases:\n%s\n---\n%s",
					pair[0], pair[1], outA, outB)
			}
		})
	}
}

func TestDispatch_ReturnsOneWhenTheCommandFails(t *testing.T) {
	var code int
	captureOutput(t, func() {
		// An unresolvable website root makes the build fail immediately.
		code = Dispatch("website-compiler", []string{
			"build", "-website-root", filepath.Join(t.TempDir(), "absent"),
		}, discard())
	})
	if code != 1 {
		t.Errorf("exit code = %d, want 1 when the command returns an error", code)
	}
}

func TestPrintUsage_NamesTheProgramAndEveryCommand(t *testing.T) {
	out := captureOutput(t, func() { printUsage("my-program-name") })

	if !strings.Contains(out, "my-program-name") {
		t.Errorf("usage does not name the program:\n%s", out)
	}
	for _, cmd := range []string{
		"build", "serve", "export-site-data", "validate-site-data",
		"validate-assets", "validate-sanity", "check-lang-parity", "test-output",
	} {
		if !strings.Contains(out, cmd) {
			t.Errorf("usage does not document %q:\n%s", cmd, out)
		}
	}
}

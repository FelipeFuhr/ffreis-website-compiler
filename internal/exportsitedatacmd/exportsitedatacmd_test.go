package exportsitedatacmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"ffreis-website-compiler/internal/testutil"
)

// newSiteWithData builds a website root whose templates directory carries the
// given site data, and returns the root.
func newSiteWithData(t *testing.T, siteYAML string) string {
	t.Helper()
	root := t.TempDir()
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "templates"))
	testutil.MustMkdirAll(t, filepath.Join(root, "src", "data"))
	testutil.WriteFiles(t, map[string]string{
		filepath.Join(root, "src", "data", "site.yaml"): siteYAML,
	})
	return root
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what it
// wrote. The command writes the export straight to stdout, so this is the only
// way to observe its output.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	runErr := fn()
	os.Stdout = orig
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
	return sb.String(), runErr
}

func TestRun_ExportsJSONByDefault(t *testing.T) {
	root := newSiteWithData(t, "title: Probe\nnested:\n  count: 3\n")

	out, err := captureStdout(t, func() error {
		return Run([]string{"-website-root", root}, nil)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("default output is not JSON: %v\n%s", err, out)
	}
	if decoded["title"] != "Probe" {
		t.Errorf("title = %v, want Probe", decoded["title"])
	}
	if !strings.Contains(out, "\n  ") {
		t.Error("JSON output should be indented for readability")
	}
}

func TestRun_ExportsYAMLOnRequest(t *testing.T) {
	root := newSiteWithData(t, "title: Probe\n")

	for _, format := range []string{"yaml", "yml", "YAML", "  yaml  "} {
		t.Run(format, func(t *testing.T) {
			out, err := captureStdout(t, func() error {
				return Run([]string{"-website-root", root, "-format", format}, nil)
			})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			var decoded map[string]any
			if err := yaml.Unmarshal([]byte(out), &decoded); err != nil {
				t.Fatalf("output is not YAML: %v\n%s", err, out)
			}
			if decoded["title"] != "Probe" {
				t.Errorf("title = %v, want Probe", decoded["title"])
			}
		})
	}
}

func TestRun_RejectsAnUnsupportedFormat(t *testing.T) {
	root := newSiteWithData(t, "title: Probe\n")

	_, err := captureStdout(t, func() error {
		return Run([]string{"-website-root", root, "-format", "toml"}, nil)
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("err = %v, want an unsupported-format error naming the expected values", err)
	}
}

func TestRun_AcceptsAnExplicitTemplatesDir(t *testing.T) {
	root := newSiteWithData(t, "title: Explicit\n")

	out, err := captureStdout(t, func() error {
		// No -website-root: the explicit templates dir must be enough.
		return Run([]string{"-templates-dir", filepath.Join(root, "src", "templates")}, nil)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "Explicit") {
		t.Errorf("output did not come from the explicit templates dir:\n%s", out)
	}
}

func TestRun_ErrorsWhenTheTemplatesDirCannotBeResolved(t *testing.T) {
	_, err := captureStdout(t, func() error {
		return Run([]string{"-website-root", t.TempDir()}, nil)
	})
	if err == nil || !strings.Contains(err.Error(), "could not resolve templates directory") {
		t.Fatalf("err = %v, want an unresolvable-templates error", err)
	}
}

func TestRun_ErrorsWhenSiteDataIsUnreadable(t *testing.T) {
	root := newSiteWithData(t, "title: [unclosed\n")

	_, err := captureStdout(t, func() error {
		return Run([]string{"-website-root", root}, nil)
	})
	if err == nil || !strings.Contains(err.Error(), "loading site data") {
		t.Fatalf("err = %v, want the load failure surfaced with context", err)
	}
}

func TestRun_RejectsAnUnknownFlag(t *testing.T) {
	_, err := captureStdout(t, func() error {
		return Run([]string{"-not-a-real-flag"}, nil)
	})
	if err == nil {
		t.Fatal("an unknown flag must be an error, not silently ignored")
	}
}

package outputtestcmd

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunChecksWebsiteOwnedCompiledOutput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "tests", "output.yaml"), `version: 1
pages:
  - name: checkout
    path: /checkout/
    contains:
      - Checkout
    not_contains:
      - SECRET_VALUE
  - path: /missing/
    exists: false
`)
	mustWrite(t, filepath.Join(root, "dist", "checkout", "index.html"), "<main>Checkout</main>")

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	if err := Run([]string{"-website-root", root, "-out", filepath.Join(root, "dist")}, logger); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !bytes.Contains(logs.Bytes(), []byte("compiled output tests passed")) {
		t.Fatalf("expected success log, got %s", logs.String())
	}
}

func TestRunFailsForMissingExpectedContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "tests", "output.yaml"), "version: 1\npages:\n  - path: /\n    contains: [expected]\n")
	mustWrite(t, filepath.Join(root, "dist", "index.html"), "actual")
	if err := Run([]string{"-website-root", root, "-out", filepath.Join(root, "dist")}, nil); err == nil {
		t.Fatal("Run() error = nil, want missing content failure")
	}
}

func TestManifestRejectsTraversal(t *testing.T) {
	t.Parallel()
	err := validateManifest(manifest{Version: 1, Pages: []pageTest{{Path: "/../outside/"}}})
	if err == nil {
		t.Fatal("validateManifest() error = nil, want traversal failure")
	}
}

func TestValidateManifestRejectsInvalidDefinitions(t *testing.T) {
	t.Parallel()
	falseValue := false
	cases := []struct {
		name string
		data manifest
	}{
		{name: "unknown version", data: manifest{Version: 2, Pages: []pageTest{{Path: "/"}}}},
		{name: "no pages", data: manifest{Version: 1}},
		{name: "blank path", data: manifest{Version: 1, Pages: []pageTest{{Path: " "}}}},
		{name: "duplicate normalized route", data: manifest{Version: 1, Pages: []pageTest{{Path: "/checkout/"}, {Path: "checkout/index.html"}}}},
		{
			name: "content assertion for absent page",
			data: manifest{Version: 1, Pages: []pageTest{{
				Path: "/gone/", Exists: &falseValue, Contains: []string{"unexpected"},
			}}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateManifest(tc.data); err == nil {
				t.Fatal("validateManifest() error = nil")
			}
		})
	}
}

func TestOutputFilePathAcceptsRoutesAndRejectsEscapes(t *testing.T) {
	t.Parallel()
	valid := map[string]string{
		"/":                    "index.html",
		"checkout/":            filepath.Join("checkout", "index.html"),
		"/checkout/index.html": filepath.Join("checkout", "index.html"),
		"assets/app.js":        filepath.Join("assets", "app.js"),
	}
	for route, want := range valid {
		got, err := outputFilePath(route)
		if err != nil || got != want {
			t.Fatalf("outputFilePath(%q) = %q, %v; want %q, nil", route, got, err, want)
		}
	}
	for _, route := range []string{"", "../secret", "/../secret", "checkout/../../secret"} {
		if _, err := outputFilePath(route); err == nil {
			t.Fatalf("outputFilePath(%q) error = nil", route)
		}
	}
}

func TestRunRejectsUnreadableInputsAndInvalidOutput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := Run([]string{"-website-root", root}, nil); err == nil {
		t.Fatal("Run() error = nil for missing manifest")
	}
	mustWrite(t, filepath.Join(root, "tests", "output.yaml"), "not: [valid")
	if err := Run([]string{"-website-root", root}, nil); err == nil {
		t.Fatal("Run() error = nil for malformed manifest")
	}
	mustWrite(t, filepath.Join(root, "tests", "output.yaml"), "version: 1\npages:\n  - path: /\n")
	mustWrite(t, filepath.Join(root, "not-a-directory"), "file")
	if err := Run([]string{"-website-root", root, "-out", filepath.Join(root, "not-a-directory")}, nil); err == nil {
		t.Fatal("Run() error = nil for output file")
	}
}

func TestRunReportsExistenceAndForbiddenContentFailures(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "tests", "output.yaml"), "version: 1\npages:\n  - path: /missing/\n")
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"-website-root", root, "-out", filepath.Join(root, "dist")}, nil); err == nil || !strings.Contains(err.Error(), "expected file") {
		t.Fatalf("Run() error = %v, want missing output failure", err)
	}
	mustWrite(t, filepath.Join(root, "dist", "index.html"), "forbidden")
	mustWrite(t, filepath.Join(root, "tests", "output.yaml"), "version: 1\npages:\n  - path: /\n    not_contains: [forbidden]\n")
	if err := Run([]string{"-website-root", root, "-out", filepath.Join(root, "dist")}, nil); err == nil || !strings.Contains(err.Error(), "forbidden text") {
		t.Fatalf("Run() error = %v, want forbidden content failure", err)
	}
}

func TestRunRejectsUnexpectedAndDirectoryOutput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist", "section"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "dist", "section", "index.html"), "section")
	mustWrite(t, filepath.Join(root, "tests", "output.yaml"), "version: 1\npages:\n  - path: /section/\n    exists: false\n")
	if err := Run([]string{"-website-root", root, "-out", filepath.Join(root, "dist")}, nil); err == nil || !strings.Contains(err.Error(), "not to exist") {
		t.Fatalf("Run() error = %v, want unexpected file failure", err)
	}
	mustWrite(t, filepath.Join(root, "tests", "output.yaml"), "version: 1\npages:\n  - path: /section\n")
	if err := Run([]string{"-website-root", root, "-out", filepath.Join(root, "dist")}, nil); err == nil || !strings.Contains(err.Error(), "found directory") {
		t.Fatalf("Run() error = %v, want directory failure", err)
	}
}

func TestOptionsAndHelpers(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{"-website-root", "site", "-out", "public", "-manifest", "check.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.websiteRoot != "site" || opts.out != "public" || opts.manifest != "check.yaml" {
		t.Fatalf("parseOptions() = %+v", opts)
	}
	if _, err := parseOptions([]string{"-unknown"}); err == nil {
		t.Fatal("parseOptions() error = nil for unknown flag")
	}
	if got := displayName(pageTest{Path: "/checkout/"}); got != "/checkout/" {
		t.Fatalf("displayName() = %q", got)
	}
	paths := SortedPaths(manifest{Pages: []pageTest{{Path: "/z/"}, {Path: "/a/"}}})
	if strings.Join(paths, ",") != "/a/,/z/" {
		t.Fatalf("SortedPaths() = %v", paths)
	}
}

func TestRunRejectsSymlinkedOutput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "tests", "output.yaml"), "version: 1\npages:\n  - path: /\n")
	mustWrite(t, filepath.Join(root, "outside.html"), "outside")
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside.html"), filepath.Join(root, "dist", "index.html")); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"-website-root", root, "-out", filepath.Join(root, "dist")}, nil); err == nil {
		t.Fatal("Run() error = nil, want symlink failure")
	}
}

func TestRunRejectsOutputThroughSymlinkedDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "tests", "output.yaml"), "version: 1\npages:\n  - path: /checkout/\n")
	mustWrite(t, filepath.Join(root, "outside", "index.html"), "outside")
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(root, "dist", "checkout")); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"-website-root", root, "-out", filepath.Join(root, "dist")}, nil); err == nil {
		t.Fatal("Run() error = nil, want symlinked directory failure")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

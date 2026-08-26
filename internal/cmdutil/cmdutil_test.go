package cmdutil

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/sitegen"
)

func mustMkdirAll(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(parts...)
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	return path
}

func TestResolveWebsitePaths_PrefersTheModernSrcLayout(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, root, "src", "assets")
	mustMkdirAll(t, root, "src", "templates")

	assets, templates, err := ResolveWebsitePaths(root)
	if err != nil {
		t.Fatalf("ResolveWebsitePaths: %v", err)
	}
	if assets != filepath.Join(root, "src", "assets") {
		t.Errorf("assets = %q, want the src/ layout", assets)
	}
	if templates != filepath.Join(root, "src", "templates") {
		t.Errorf("templates = %q, want the src/ layout", templates)
	}
}

func TestResolveWebsitePaths_FallsBackToTheLegacyLayout(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, root, "site")
	mustMkdirAll(t, root, "templates")

	assets, templates, err := ResolveWebsitePaths(root)
	if err != nil {
		t.Fatalf("ResolveWebsitePaths: %v", err)
	}
	if assets != filepath.Join(root, "site") || templates != filepath.Join(root, "templates") {
		t.Errorf("got (%q, %q), want the legacy site/ + templates/ pair", assets, templates)
	}
}

// A site that is halfway through a migration must not resolve to a mixed pair.
func TestResolveWebsitePaths_RequiresBothHalvesOfALayout(t *testing.T) {
	for _, tc := range []struct {
		name string
		dirs [][]string
	}{
		{"src/assets without src/templates", [][]string{{"src", "assets"}}},
		{"src/templates without src/assets", [][]string{{"src", "templates"}}},
		{"legacy site without templates", [][]string{{"site"}}},
		{"legacy templates without site", [][]string{{"templates"}}},
		{"nothing at all", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, parts := range tc.dirs {
				mustMkdirAll(t, append([]string{root}, parts...)...)
			}
			_, _, err := ResolveWebsitePaths(root)
			if err == nil {
				t.Fatal("expected an error rather than a half-resolved layout")
			}
			if !strings.Contains(err.Error(), root) {
				t.Errorf("err = %v, want it to name the root that failed", err)
			}
		})
	}
}

// The modern layout wins even when both are present, so a leftover legacy
// directory does not silently take over after a migration.
func TestResolveWebsitePaths_ModernLayoutWinsOverALeftoverLegacyOne(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, root, "src", "assets")
	mustMkdirAll(t, root, "src", "templates")
	mustMkdirAll(t, root, "site")
	mustMkdirAll(t, root, "templates")

	assets, _, err := ResolveWebsitePaths(root)
	if err != nil {
		t.Fatalf("ResolveWebsitePaths: %v", err)
	}
	if assets != filepath.Join(root, "src", "assets") {
		t.Errorf("assets = %q, want the src/ layout to win", assets)
	}
}

func TestDirExists_DistinguishesDirectoriesFromFilesAndAbsence(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !DirExists(root) {
		t.Error("DirExists(existing dir) = false")
	}
	if DirExists(file) {
		t.Error("DirExists(a file) = true; a file is not a directory")
	}
	if DirExists(filepath.Join(root, "absent")) {
		t.Error("DirExists(missing path) = true")
	}
}

func TestFileExists_DistinguishesFilesFromDirectoriesAndAbsence(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !FileExists(file) {
		t.Error("FileExists(existing file) = false")
	}
	if FileExists(root) {
		t.Error("FileExists(a directory) = true; a directory is not a file")
	}
	if FileExists(filepath.Join(root, "absent")) {
		t.Error("FileExists(missing path) = true")
	}
}

func TestFirstNonEmpty_SkipsEmptyAndWhitespaceOnlyValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		want   string
	}{
		{"first value wins", []string{"a", "b"}, "a"},
		{"empty strings are skipped", []string{"", "", "b"}, "b"},
		{"whitespace-only is treated as empty", []string{"   ", "\t\n", "b"}, "b"},
		{"the chosen value keeps its original spacing", []string{"", " padded "}, " padded "},
		{"all empty yields empty", []string{"", "  "}, ""},
		{"no values yields empty", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FirstNonEmpty(tc.values...); got != tc.want {
				t.Errorf("FirstNonEmpty(%q) = %q, want %q", tc.values, got, tc.want)
			}
		})
	}
}

func TestLogSiteDataOverride_WarnsOnlyWhenAnOverrideShadowedALocalFile(t *testing.T) {
	for _, tc := range []struct {
		name       string
		result     sitegen.SiteDataLoadResult
		wantWarned bool
	}{
		{
			name: "override shadowed a local file",
			result: sitegen.SiteDataLoadResult{
				UsedOverride: true, DefaultPathFound: true,
				Source: "/tmp/override.yaml", DefaultPath: "/site/src/data/site.yaml",
			},
			wantWarned: true,
		},
		{
			name:       "override with no local file to shadow",
			result:     sitegen.SiteDataLoadResult{UsedOverride: true, DefaultPathFound: false},
			wantWarned: false,
		},
		{
			name:       "local file with no override",
			result:     sitegen.SiteDataLoadResult{UsedOverride: false, DefaultPathFound: true},
			wantWarned: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, nil))
			LogSiteDataOverride(logger, tc.result)

			warned := strings.Contains(buf.String(), "site data override supersedes")
			if warned != tc.wantWarned {
				t.Errorf("warned = %v, want %v; log was: %s", warned, tc.wantWarned, buf.String())
			}
			if tc.wantWarned && !strings.Contains(buf.String(), tc.result.Source) {
				t.Errorf("warning should name the override source; log was: %s", buf.String())
			}
		})
	}
}

// Unlike ResolveWebsitePaths, this resolves templates alone — a site with
// templates but no assets directory is legitimate for template-only commands.
func TestResolveTemplatesRoot_ResolvesEitherLayoutIndependently(t *testing.T) {
	for _, tc := range []struct {
		name string
		dirs []string
		want []string
	}{
		{"modern layout", []string{"src", "templates"}, []string{"src", "templates"}},
		{"legacy layout", []string{"templates"}, []string{"templates"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			mustMkdirAll(t, append([]string{root}, tc.dirs...)...)

			got, err := ResolveTemplatesRoot(root)
			if err != nil {
				t.Fatalf("ResolveTemplatesRoot: %v", err)
			}
			if want := filepath.Join(append([]string{root}, tc.want...)...); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestResolveTemplatesRoot_ModernLayoutWinsOverALeftoverLegacyOne(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, root, "src", "templates")
	mustMkdirAll(t, root, "templates")

	got, err := ResolveTemplatesRoot(root)
	if err != nil {
		t.Fatalf("ResolveTemplatesRoot: %v", err)
	}
	if got != filepath.Join(root, "src", "templates") {
		t.Errorf("got %q, want the src/ layout to win", got)
	}
}

func TestResolveTemplatesRoot_ErrorsWhenNeitherLayoutExists(t *testing.T) {
	root := t.TempDir()
	_, err := ResolveTemplatesRoot(root)
	if err == nil {
		t.Fatal("expected an error when no templates directory exists")
	}
	if !strings.Contains(err.Error(), root) {
		t.Errorf("err = %v, want it to name the root that failed", err)
	}
}

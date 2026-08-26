package buildcmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// ffreisRegistry mirrors the shape ffreis-website declares: every known section
// gated, all three off by default because none is production-ready.
const ffreisRegistry = `{
  "version": 1,
  "project": "ffreis-website",
  "flags": [
    {"name": "section_blog",     "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": false},
    {"name": "section_courses",  "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": false},
    {"name": "section_projects", "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": false}
  ]
}`

func registryAt(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "flags"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(root, registryRelPath)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func mustResolve(t *testing.T, registry string, enable, disable []string) []string {
	t.Helper()
	got, err := resolveSectionGates(registry, enable, disable)
	if err != nil {
		t.Fatalf("resolveSectionGates: %v", err)
	}
	return got
}

// The landmine: a prod build whose per-environment section config goes missing
// — dropped in a promote, lost in a branch divergence — must not publish
// not-ready sections. With a registry, no overrides means the declared
// defaults, which are off.
func TestResolveSectionGates_NoOverridesKeepsDeclaredDefaultsOff(t *testing.T) {
	got := mustResolve(t, registryAt(t, ffreisRegistry), nil, nil)
	want := []string{"blog", "courses", "projects"}
	if !slices.Equal(got, want) {
		t.Fatalf("disabled = %v, want %v — a build with no section config must ship nothing not-ready", got, want)
	}
}

// The control for the test above: identical inputs with no registry produce the
// opposite result. This is precisely the old behaviour, and precisely why the
// registry exists — without it, absent config means "publish everything".
func TestResolveSectionGates_LegacyNoRegistryDefaultsEverythingOn(t *testing.T) {
	got := mustResolve(t, "", nil, nil)
	if len(got) != 0 {
		t.Fatalf("disabled = %v, want none: without a registry, absent config enables every section", got)
	}
}

func TestResolveSectionGates_EnableOptsSectionsInPerEnvironment(t *testing.T) {
	got := mustResolve(t, registryAt(t, ffreisRegistry), []string{"blog", "projects"}, nil)
	if !slices.Equal(got, []string{"courses"}) {
		t.Fatalf("disabled = %v, want [courses]", got)
	}
}

// Between two contradictory instructions the one that ships less wins.
func TestResolveSectionGates_DisableBeatsEnable(t *testing.T) {
	got := mustResolve(t, registryAt(t, ffreisRegistry), []string{"blog"}, []string{"blog"})
	if !slices.Contains(got, "blog") {
		t.Fatalf("disabled = %v, want blog disabled when both enabled and disabled", got)
	}
}

func TestResolveSectionGates_LegacyPassesDisableListThrough(t *testing.T) {
	got := mustResolve(t, "", nil, []string{"blog", "courses"})
	if !slices.Equal(got, []string{"blog", "courses"}) {
		t.Fatalf("disabled = %v, want [blog courses]", got)
	}
}

func TestResolveSectionGates_EnableWithoutRegistryIsAnError(t *testing.T) {
	_, err := resolveSectionGates("", []string{"blog"}, nil)
	if err == nil || !strings.Contains(err.Error(), "requires a flag registry") {
		t.Fatalf("err = %v, want a registry-required error", err)
	}
}

// A typo must fail the build, not silently leave the declared default standing.
func TestResolveSectionGates_UnknownSectionNameIsAnError(t *testing.T) {
	for _, tc := range []struct{ name, section string }{
		{"enable", "blogs"},
		{"disable", "projekts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var enable, disable []string
			if tc.name == "enable" {
				enable = []string{tc.section}
			} else {
				disable = []string{tc.section}
			}
			_, err := resolveSectionGates(registryAt(t, ffreisRegistry), enable, disable)
			if err == nil || !strings.Contains(err.Error(), tc.section) {
				t.Fatalf("err = %v, want an error naming %q", err, tc.section)
			}
		})
	}
}

// A registry that forgets a section would let that section fall back to "on".
func TestResolveSectionGates_IncompleteRegistryIsAnError(t *testing.T) {
	incomplete := `{"version": 1, "flags": [
	  {"name": "section_blog", "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": false}
	]}`
	_, err := resolveSectionGates(registryAt(t, incomplete), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "missing gate flags") {
		t.Fatalf("err = %v, want a missing-gate error", err)
	}
}

func TestResolveSectionGates_DanglingGateIsAnError(t *testing.T) {
	dangling := `{"version": 1, "flags": [
	  {"name": "section_blog",     "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": false},
	  {"name": "section_courses",  "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": false},
	  {"name": "section_projects", "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": false},
	  {"name": "section_podcast",  "kind": "gate", "category": "content", "binding": "compile", "effect": "toggle", "default": false}
	]}`
	_, err := resolveSectionGates(registryAt(t, dangling), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "podcast") {
		t.Fatalf("err = %v, want an error naming the dangling gate", err)
	}
}

func TestFindFlagRegistry(t *testing.T) {
	path := registryAt(t, ffreisRegistry)
	root := filepath.Dir(filepath.Dir(path))
	if got := findFlagRegistry(root); got != path {
		t.Errorf("findFlagRegistry = %q, want %q", got, path)
	}
	if got := findFlagRegistry(t.TempDir()); got != "" {
		t.Errorf("findFlagRegistry on a site with no registry = %q, want empty", got)
	}
}

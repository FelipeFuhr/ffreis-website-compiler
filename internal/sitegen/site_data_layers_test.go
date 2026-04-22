package sitegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ffreis-website-compiler/internal/testutil"
)

const (
	siteYAMLFile        = "site.yaml"
	calendarLayerFile   = "20-calendar.yaml"
	writeSiteYAMLFmt    = "write site.yaml: %v"
	unexpectedErrorFmt  = "unexpected error: %v"
	unexpectedLayersFmt = "unexpected layers: %#v"
)

func TestLoadSiteData_BaseOnly(t *testing.T) {
	t.Parallel()

	templatesRoot, dataRoot := newTestSiteRoot(t)
	basePath := filepath.Join(dataRoot, siteYAMLFile)
	if err := os.WriteFile(basePath, []byte("courses:\n  a:\n    stable: true\n"), 0o644); err != nil {
		t.Fatalf(writeSiteYAMLFmt, err)
	}

	result, err := LoadSiteData(templatesRoot, "")
	if err != nil {
		t.Fatalf(unexpectedErrorFmt, err)
	}
	if !result.DefaultPathFound {
		t.Fatalf("expected DefaultPathFound=true, got false")
	}
	if len(result.Layers) != 1 || result.Layers[0] != basePath {
		t.Fatalf(unexpectedLayersFmt, result.Layers)
	}
	courses, ok := result.Data["courses"].(map[string]any)
	if !ok {
		t.Fatalf("missing courses map: %#v", result.Data)
	}
	a, ok := courses["a"].(map[string]any)
	if !ok || a["stable"] != true {
		t.Fatalf("unexpected merged data: %#v", result.Data)
	}
}

func TestLoadSiteData_WithOverlays_MergesDisjointLeaves(t *testing.T) {
	t.Parallel()

	templatesRoot, dataRoot, overlayRoot := newTestOverlayRoot(t)
	basePath := filepath.Join(dataRoot, siteYAMLFile)
	layerPath := filepath.Join(overlayRoot, calendarLayerFile)
	if err := os.WriteFile(basePath, []byte("courses:\n  cqe:\n    variants:\n      online:\n        duration: 88\n"), 0o644); err != nil {
		t.Fatalf(writeSiteYAMLFmt, err)
	}
	if err := os.WriteFile(layerPath, []byte("courses:\n  cqe:\n    variants:\n      online:\n        start_text: \"Em definição.\"\n"), 0o644); err != nil {
		t.Fatalf("write layer: %v", err)
	}

	result, err := LoadSiteData(templatesRoot, "")
	if err != nil {
		t.Fatalf(unexpectedErrorFmt, err)
	}
	if len(result.Layers) != 2 || result.Layers[0] != basePath || result.Layers[1] != layerPath {
		t.Fatalf(unexpectedLayersFmt, result.Layers)
	}

	online := digPathMap(t, result.Data, "courses", "cqe", "variants", "online")
	if online["duration"] != 88 || online["start_text"] != "Em definição." {
		t.Fatalf("unexpected merged online data: %#v", online)
	}
}

func TestLoadSiteData_LeafConflictErrors(t *testing.T) {
	t.Parallel()

	templatesRoot, dataRoot, overlayRoot := newTestOverlayRoot(t)
	basePath := filepath.Join(dataRoot, siteYAMLFile)
	layerPath := filepath.Join(overlayRoot, calendarLayerFile)
	if err := os.WriteFile(basePath, []byte("courses:\n  cqe:\n    variants:\n      online:\n        start_text: \"A\"\n"), 0o644); err != nil {
		t.Fatalf(writeSiteYAMLFmt, err)
	}
	if err := os.WriteFile(layerPath, []byte("courses:\n  cqe:\n    variants:\n      online:\n        start_text: \"B\"\n"), 0o644); err != nil {
		t.Fatalf("write layer: %v", err)
	}

	_, err := LoadSiteData(templatesRoot, "")
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	msg := err.Error()
	for _, token := range []string{
		"courses.cqe.variants.online.start_text",
		basePath,
		layerPath,
	} {
		if !strings.Contains(msg, token) {
			t.Fatalf("expected %q in error, got %v", token, msg)
		}
	}
}

func TestLoadSiteData_OverrideDirectory(t *testing.T) {
	t.Parallel()

	templatesRoot := newTestTemplatesRoot(t)
	overrideDir := filepath.Join(t.TempDir(), "layers")
	testutil.MustMkdirAll(t, overrideDir)

	first := filepath.Join(overrideDir, "10-base.yaml")
	second := filepath.Join(overrideDir, calendarLayerFile)
	if err := os.WriteFile(first, []byte("courses:\n  a:\n    stable: true\n"), 0o644); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := os.WriteFile(second, []byte("courses:\n  a:\n    start_text: \"Em definição.\"\n"), 0o644); err != nil {
		t.Fatalf("write second: %v", err)
	}

	result, err := LoadSiteData(templatesRoot, overrideDir)
	if err != nil {
		t.Fatalf(unexpectedErrorFmt, err)
	}
	if !result.UsedOverride {
		t.Fatalf("expected UsedOverride=true")
	}
	if len(result.Layers) != 2 || result.Layers[0] != first || result.Layers[1] != second {
		t.Fatalf(unexpectedLayersFmt, result.Layers)
	}

	a := digPathMap(t, result.Data, "courses", "a")
	if a["stable"] != true || a["start_text"] != "Em definição." {
		t.Fatalf("unexpected merged data: %#v", a)
	}
}

func TestLoadSiteData_OverrideDirectory_LeafConflictErrors(t *testing.T) {
	t.Parallel()

	templatesRoot := newTestTemplatesRoot(t)
	overrideDir := filepath.Join(t.TempDir(), "layers")
	testutil.MustMkdirAll(t, overrideDir)

	first := filepath.Join(overrideDir, "10-a.yaml")
	second := filepath.Join(overrideDir, "20-b.yaml")
	if err := os.WriteFile(first, []byte("courses:\n  a:\n    start_text: \"A\"\n"), 0o644); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := os.WriteFile(second, []byte("courses:\n  a:\n    start_text: \"B\"\n"), 0o644); err != nil {
		t.Fatalf("write second: %v", err)
	}

	_, err := LoadSiteData(templatesRoot, overrideDir)
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	msg := err.Error()
	for _, token := range []string{
		"courses.a.start_text",
		first,
		second,
	} {
		if !strings.Contains(msg, token) {
			t.Fatalf("expected %q in error, got %v", token, msg)
		}
	}
}

func newTestTemplatesRoot(t *testing.T) string {
	t.Helper()
	websiteRoot := t.TempDir()
	templatesRoot := filepath.Join(websiteRoot, "src", "templates")
	testutil.MustMkdirAll(t, templatesRoot)
	return templatesRoot
}

func newTestSiteRoot(t *testing.T) (templatesRoot, dataRoot string) {
	t.Helper()
	websiteRoot := t.TempDir()
	templatesRoot = filepath.Join(websiteRoot, "src", "templates")
	dataRoot = filepath.Join(websiteRoot, "src", "data")
	testutil.MustMkdirAll(t, templatesRoot)
	testutil.MustMkdirAll(t, dataRoot)
	return templatesRoot, dataRoot
}

func newTestOverlayRoot(t *testing.T) (templatesRoot, dataRoot, overlayRoot string) {
	t.Helper()
	templatesRoot, dataRoot = newTestSiteRoot(t)
	overlayRoot = filepath.Join(dataRoot, "site.d")
	testutil.MustMkdirAll(t, overlayRoot)
	return templatesRoot, dataRoot, overlayRoot
}

func digPathMap(t *testing.T, root map[string]any, keys ...string) map[string]any {
	t.Helper()
	current := any(root)
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("expected map at %q, got %T", strings.Join(keys, "."), current)
		}
		current = asMap[key]
	}
	asMap, ok := current.(map[string]any)
	if !ok {
		t.Fatalf("expected map at %q, got %T", strings.Join(keys, "."), current)
	}
	return asMap
}

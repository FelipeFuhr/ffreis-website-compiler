package buildcmd

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAssetFiles(t *testing.T, assetsRoot string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(assetsRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
}

func TestValidateAssetUsage_FailsOnUnusedAssetByDefault(t *testing.T) {
	assetsRoot := t.TempDir()
	writeAssetFiles(t, assetsRoot, map[string]string{
		"js/examples/demo.js": "console.log('demo');",
	})
	pages := map[string]string{"index": `<html><head></head><body></body></html>`}

	err := validateAssetUsage(discardLogger(), buildOptions{}, assetsRoot, pages)
	if err == nil {
		t.Fatal("expected an error for an unreferenced JS file with no -assets-ignore")
	}
	if !strings.Contains(err.Error(), "js/examples/demo.js") {
		t.Errorf("expected error to name the unused file, got %v", err)
	}
}

func TestValidateAssetUsage_AssetsIgnoreExemptsMatchingFiles(t *testing.T) {
	assetsRoot := t.TempDir()
	writeAssetFiles(t, assetsRoot, map[string]string{
		"js/examples/demo.js": "console.log('demo');",
		"js/ports/auth.js":    "/** @typedef */",
	})
	pages := map[string]string{"index": `<html><head></head><body></body></html>`}

	opts := buildOptions{assetsIgnore: []string{"js/examples/*.js", "js/ports/*.js"}}
	if err := validateAssetUsage(discardLogger(), opts, assetsRoot, pages); err != nil {
		t.Fatalf("expected -assets-ignore to exempt both files, got %v", err)
	}
}

func TestValidateAssetUsage_AssetsIgnoreDoesNotExemptMissingRef(t *testing.T) {
	assetsRoot := t.TempDir()
	// No files written at all — js/main.js is referenced but doesn't exist.
	pages := map[string]string{"index": `<html><head></head><body><script src="js/main.js"></script></body></html>`}

	opts := buildOptions{assetsIgnore: []string{"js/main.js"}}
	err := validateAssetUsage(discardLogger(), opts, assetsRoot, pages)
	if err == nil {
		t.Fatal("expected a missing-reference error even though js/main.js matches -assets-ignore")
	}
	if !strings.Contains(err.Error(), "missing local asset reference") {
		t.Errorf("expected a missing-reference error, got %v", err)
	}
}

func TestValidateAssetUsage_AssetsIgnorePartialMatch_StillFailsOnTheRest(t *testing.T) {
	assetsRoot := t.TempDir()
	writeAssetFiles(t, assetsRoot, map[string]string{
		"js/examples/demo.js":  "console.log('demo');",
		"js/genuinely-dead.js": "console.log('dead');",
	})
	pages := map[string]string{"index": `<html><head></head><body></body></html>`}

	opts := buildOptions{assetsIgnore: []string{"js/examples/*.js"}}
	err := validateAssetUsage(discardLogger(), opts, assetsRoot, pages)
	if err == nil {
		t.Fatal("expected js/genuinely-dead.js (not matched by the ignore pattern) to still fail the build")
	}
	if strings.Contains(err.Error(), "js/examples/demo.js") {
		t.Errorf("expected the ignored file to be absent from the error, got %v", err)
	}
	if !strings.Contains(err.Error(), "js/genuinely-dead.js") {
		t.Errorf("expected the non-ignored file to still be reported, got %v", err)
	}
}

func TestValidateAssetUsage_AssetsIgnoreCombinesWithDisabledSections(t *testing.T) {
	assetsRoot := t.TempDir()
	writeAssetFiles(t, assetsRoot, map[string]string{
		"js/examples/demo.js": "console.log('demo');",
		"js/carousel.js":      "console.log('carousel');",
	})
	pages := map[string]string{"index": `<html><head></head><body></body></html>`}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	opts := buildOptions{
		assetsIgnore:     []string{"js/examples/*.js"},
		disabledSections: []string{"blog"},
	}
	if err := validateAssetUsage(logger, opts, assetsRoot, pages); err != nil {
		t.Fatalf("expected disabledSections to downgrade the remaining unused asset to a warning, got %v", err)
	}
	if !strings.Contains(logBuf.String(), "js/carousel.js") {
		t.Errorf("expected js/carousel.js logged as a tolerated warning, got log: %s", logBuf.String())
	}
}

func TestPartitionIgnoredAssets(t *testing.T) {
	kept, ignored := partitionIgnoredAssets(
		[]string{"js/examples/a.js", "js/examples/b.js", "js/real.js"},
		[]string{"js/examples/*.js"},
	)
	if len(kept) != 1 || kept[0] != "js/real.js" {
		t.Errorf("kept = %v, want [js/real.js]", kept)
	}
	if len(ignored) != 2 {
		t.Errorf("ignored = %v, want 2 entries", ignored)
	}
}

func TestPartitionIgnoredAssets_MalformedPatternMatchesNothing(t *testing.T) {
	kept, ignored := partitionIgnoredAssets(
		[]string{"js/real.js"},
		[]string{"[unclosed"},
	)
	if len(ignored) != 0 {
		t.Errorf("expected a malformed pattern to match nothing, got ignored=%v", ignored)
	}
	if len(kept) != 1 {
		t.Errorf("expected the asset to remain kept, got kept=%v", kept)
	}
}

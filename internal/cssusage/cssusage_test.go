package cssusage

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCSS(t *testing.T, dir, relPath, content string) {
	t.Helper()
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func TestValidate_FlagsRuleWithNoMatchingClassOnPage(t *testing.T) {
	assetsRoot := t.TempDir()
	writeCSS(t, assetsRoot, "css/index.css", `.hero-title { color: red; } .nowhere-used { color: blue; }`)

	result, err := Validate(assetsRoot, map[string]string{
		"index": `<html><head><link rel="stylesheet" href="css/index.css"></head><body><h1 class="hero-title">Hi</h1></body></html>`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("got %#v, want exactly one finding", result.Findings)
	}
	if result.Findings[0].Selector != ".nowhere-used" {
		t.Fatalf("got selector %q, want .nowhere-used", result.Findings[0].Selector)
	}
}

func TestValidate_IgnoresBareTagAndUniversalSelectors(t *testing.T) {
	assetsRoot := t.TempDir()
	writeCSS(t, assetsRoot, "css/index.css", `body { margin: 0; } * { box-sizing: border-box; } a:hover { color: red; }`)

	result, err := Validate(assetsRoot, map[string]string{
		"index": `<html><head><link rel="stylesheet" href="css/index.css"></head><body><p>no classes at all</p></body></html>`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings for tag/universal/pseudo-only selectors, got %#v", result.Findings)
	}
}

func TestValidate_CommaListFlagsOnlyTheUnusedBranch(t *testing.T) {
	assetsRoot := t.TempDir()
	writeCSS(t, assetsRoot, "css/index.css", `.used, .unused { color: red; }`)

	result, err := Validate(assetsRoot, map[string]string{
		"index": `<html><head><link rel="stylesheet" href="css/index.css"></head><body><div class="used"></div></body></html>`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Selector != ".unused" {
		t.Fatalf("got %#v, want exactly one finding for .unused", result.Findings)
	}
}

func TestValidate_NotClauseNeverCountsAsRequiredPresence(t *testing.T) {
	assetsRoot := t.TempDir()
	// .card:not(.featured) matches any .card lacking .featured -- if
	// .featured never appears anywhere, every .card still matches, so this
	// must NOT be flagged (a real rule, actually in effect on the page).
	writeCSS(t, assetsRoot, "css/index.css", `.card:not(.featured) { border: 1px solid; }`)

	result, err := Validate(assetsRoot, map[string]string{
		"index": `<html><head><link rel="stylesheet" href="css/index.css"></head><body><div class="card"></div></body></html>`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected :not() clause to never be treated as a required-present token, got %#v", result.Findings)
	}
}

func TestValidate_RecursesIntoMediaQueriesAgainstSamePage(t *testing.T) {
	assetsRoot := t.TempDir()
	writeCSS(t, assetsRoot, "css/index.css", `@media (min-width: 40rem) { .wide-only { display: flex; } .never-there { display: none; } }`)

	result, err := Validate(assetsRoot, map[string]string{
		"index": `<html><head><link rel="stylesheet" href="css/index.css"></head><body><div class="wide-only"></div></body></html>`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Selector != ".never-there" {
		t.Fatalf("got %#v, want exactly one finding for .never-there inside @media", result.Findings)
	}
}

func TestValidate_SkipsKeyframesAndFontFaceBlocks(t *testing.T) {
	assetsRoot := t.TempDir()
	writeCSS(t, assetsRoot, "css/index.css", `
@keyframes spin { from { transform: rotate(0deg); } 50% { transform: rotate(180deg); } to { transform: rotate(360deg); } }
@font-face { font-family: "Pixel"; src: url("pixel.woff2"); }
`)

	result, err := Validate(assetsRoot, map[string]string{
		"index": `<html><head><link rel="stylesheet" href="css/index.css"></head><body><p>no classes</p></body></html>`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected @keyframes/@font-face bodies to never be treated as selectors, got %#v", result.Findings)
	}
}

func TestValidate_ScopesToEachPagesOwnStylesheetsAndMarkup(t *testing.T) {
	assetsRoot := t.TempDir()
	writeCSS(t, assetsRoot, "css/index.css", `.index-only { color: red; }`)
	writeCSS(t, assetsRoot, "css/promos.css", `.promos-only { color: blue; }`)

	result, err := Validate(assetsRoot, map[string]string{
		"index":  `<html><head><link rel="stylesheet" href="css/index.css"></head><body><div class="index-only"></div></body></html>`,
		"promos": `<html><head><link rel="stylesheet" href="css/promos.css"></head><body><div class="promos-only"></div></body></html>`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected each page to be judged only against its own linked sheet, got %#v", result.Findings)
	}
}

func TestValidate_SkipsMissingStylesheetFile(t *testing.T) {
	assetsRoot := t.TempDir()

	result, err := Validate(assetsRoot, map[string]string{
		"index": `<html><head><link rel="stylesheet" href="css/missing.css"></head><body></body></html>`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected a missing stylesheet to produce no findings (assetusage's job to report), got %#v", result.Findings)
	}
}
